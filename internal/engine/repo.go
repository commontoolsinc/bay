package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
)

// WorktreeAwarenessFile is the file DockInit writes into the dock's
// worktree directory. Claude Code reads CLAUDE.md from every ancestor
// of its working directory, so one file there gives every bay the
// agent-guide pointer with zero footprint inside the worktrees.
const WorktreeAwarenessFile = "CLAUDE.md"

// DockInit performs idempotent bay-awareness setup for a dock:
//   - Writes CLAUDE.md with the agent-guide pointer into the dock's
//     worktree directory, covering all current and future bays.
//   - Appends the pointer to each agent's project_file in the dock
//     checkout, but only when the repo already gitignores that file.
//     Bay never edits .gitignore or leaves files git would report —
//     the pointer serves bay, so it must not dirty the user's repo.
func (e *Engine) DockInit(name string) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}
	dock := m.FindDock(name)
	if dock == nil {
		return fmt.Errorf("dock %q not found", name)
	}
	if dock.Path == "" {
		return nil
	}
	return e.initCheckout(config.ExpandPath(dock.Path), dock.EffectiveWorktreeDir())
}

func (e *Engine) initCheckout(repoPath, wtDir string) error {
	// Written unconditionally: agents that don't read CLAUDE.md just
	// never look at it.
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		return fmt.Errorf("creating worktree dir: %w", err)
	}
	if err := ensureBayAwareness(filepath.Join(wtDir, WorktreeAwarenessFile)); err != nil {
		return fmt.Errorf("updating %s: %w", filepath.Join(wtDir, WorktreeAwarenessFile), err)
	}

	seen := map[string]bool{}
	var skipped []string
	agentNames := make(map[string]struct{}, len(config.KnownAgents)+len(e.Config.Agents))
	for name := range config.KnownAgents {
		agentNames[name] = struct{}{}
	}
	for name := range e.Config.Agents {
		agentNames[name] = struct{}{}
	}
	for name := range agentNames {
		info, _ := e.Config.ResolveAgent(name)
		if info.ProjectFile == "" || seen[info.ProjectFile] {
			continue
		}
		seen[info.ProjectFile] = true
		ignored, err := e.Git.IsIgnored(repoPath, info.ProjectFile)
		if err != nil {
			return fmt.Errorf("checking ignore status of %s: %w", info.ProjectFile, err)
		}
		if !ignored {
			// Creating the file would add untracked dirt (or modify a
			// tracked file). Leave the checkout alone and say so.
			skipped = append(skipped, info.ProjectFile)
			continue
		}
		if err := ensureBayAwareness(filepath.Join(repoPath, info.ProjectFile)); err != nil {
			return fmt.Errorf("updating %s: %w", info.ProjectFile, err)
		}
	}

	if len(skipped) > 0 {
		sort.Strings(skipped)
		fmt.Fprintf(os.Stderr,
			"note: %s not gitignored in %s — skipped, so agents launched in the checkout won't see the bay pointer.\n"+
				"      Add to .gitignore and re-run `bay dock init` to enable it. (Bays get it from the worktree dir regardless.)\n",
			strings.Join(skipped, ", "), repoPath)
	}
	return nil
}

const bayAwarenessBlock = "This project uses bay for workspace management. Run `bay agent-guide` for commands."

const bayAgentGuideRef = "bay agent-guide"

// ensureBayAwareness installs the bay-awareness pointer in an
// instructions file (creating it if missing) unless it's already
// referenced. It only points agents at `bay agent-guide`; it does
// not ask them to maintain the bay description — that's auto-populated
// (see docs/design/auto-descriptions.md).
func ensureBayAwareness(path string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)

	// Idempotent: if the file already points agents at the agent-guide
	// (a prior install or a hand-written mention), leave it alone.
	if strings.Contains(content, bayAgentGuideRef) {
		return nil
	}

	prefix := "\n"
	if len(content) == 0 {
		prefix = ""
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(prefix + bayAwarenessBlock + "\n")
	return err
}

// DockSync copies .worktreeinclude files from the dock checkout into all
// existing worktrees for the given dock.
func (e *Engine) DockSync(name string) (int, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return 0, err
	}
	dock := m.FindDock(name)
	if dock == nil {
		return 0, fmt.Errorf("dock %q not found", name)
	}
	repoPath := config.ExpandPath(dock.Path)
	wtDir := dock.EffectiveWorktreeDir()

	entries, err := os.ReadDir(wtDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading worktree dir: %w", err)
	}

	// Resolve patterns once — the answer depends only on the repo root,
	// and re-running per worktree would fan out git subprocesses N-fold.
	toCopy, err := e.resolveWorktreeInclude(repoPath)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		bayPath := filepath.Join(wtDir, entry.Name())
		if !e.Git.IsGitRepo(bayPath) {
			continue
		}
		if err := copyWorktreeIncludeFiles(repoPath, bayPath, toCopy); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
