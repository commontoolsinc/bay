package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
)

// DockInit performs idempotent project setup for a dock checkout:
// - Appends bay awareness line to each agent's project_file if missing.
// - Creates .worktreeinclude if it doesn't exist, and adds project files to it.
// - Adds project files to .gitignore.
func (e *Engine) DockInit(name string) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}
	dock := m.FindDock(name)
	if dock == nil {
		return fmt.Errorf("dock %q not found", name)
	}
	repoPath := config.ExpandPath(dock.Path)
	return e.initCheckout(repoPath)
}

func (e *Engine) initCheckout(repoPath string) error {
	// `toGitignore` is for files we just created. Pre-existing project
	// files are left out — the user is presumed to have their gitignore
	// the way they want it.
	// `toSync` is for .worktreeinclude. A file only goes in if it's
	// either created (we'll gitignore it) or already gitignored. Adding
	// a non-ignored pre-existing file would make `bay dock sync` refuse
	// to copy it (worktreeinclude requires gitignored matches).
	seen := map[string]bool{}
	toGitignore := map[string]bool{}
	toSync := map[string]bool{}
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
		filePath := filepath.Join(repoPath, info.ProjectFile)
		wasCreated, err := ensureBayAwareness(filePath)
		if err != nil {
			return fmt.Errorf("updating %s: %w", info.ProjectFile, err)
		}
		if wasCreated {
			toGitignore[info.ProjectFile] = true
			toSync[info.ProjectFile] = true
			continue
		}
		ignored, err := e.Git.IsIgnored(repoPath, info.ProjectFile)
		if err != nil {
			return fmt.Errorf("checking ignore status of %s: %w", info.ProjectFile, err)
		}
		if ignored {
			toSync[info.ProjectFile] = true
		}
	}

	// .worktreeinclude: create with a comment if it doesn't exist.
	wtIncludePath := filepath.Join(repoPath, ".worktreeinclude")
	if _, err := os.Stat(wtIncludePath); os.IsNotExist(err) {
		content := "# Gitignore-syntax patterns copied into new worktrees.\n" +
			"# Every match must also be covered by .gitignore.\n" +
			"# Example: .env\n"
		if err := os.WriteFile(wtIncludePath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("creating .worktreeinclude: %w", err)
		}
	}

	if len(toSync) > 0 {
		if err := ensureFileContainsLines(wtIncludePath, toSync); err != nil {
			return fmt.Errorf("updating .worktreeinclude: %w", err)
		}
	}

	if len(toGitignore) > 0 {
		gitignorePath := filepath.Join(repoPath, ".gitignore")
		if err := ensureFileContainsLines(gitignorePath, toGitignore); err != nil {
			return fmt.Errorf("updating .gitignore: %w", err)
		}
	}

	return nil
}

const bayAwarenessBlock = "This project uses bay for workspace management. Run `bay agent-guide` for commands.\n" +
	"\n" +
	"**At session start:** check the workspace description with `bay describe`. " +
	"If empty, set one with `bay describe \"<short goal>\"`. Update the body via " +
	"`bay describe --edit` when the situation meaningfully changes."

const (
	bayAwarenessMarker      = "**At session start:** check the workspace description"
	bayAwarenessLegacyStart = "This project uses bay"
	bayAgentGuideRef        = "bay agent-guide"
)

// ensureBayAwareness installs (or upgrades) the bay-awareness block in a
// project file. Returns true if the file did not exist before this call.
func ensureBayAwareness(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	created := os.IsNotExist(err)
	content := string(data)

	if strings.Contains(content, bayAwarenessMarker) {
		return false, nil
	}
	// Any single line that looks like a previously-installed pointer
	// (starts with "This project uses bay" and references the agent-guide
	// command) gets upgraded in place. Covers every phrasing bay has ever
	// auto-installed plus the doc paraphrase users may have hand-pasted.
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, bayAwarenessLegacyStart) && strings.Contains(trimmed, bayAgentGuideRef) {
			lines[i] = bayAwarenessBlock
			return false, os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}
	// Some other mention of `bay agent-guide` (e.g., a code sample or
	// hand-written prose) — leave the file alone rather than double up.
	if strings.Contains(content, bayAgentGuideRef) {
		return false, nil
	}

	prefix := "\n"
	if len(content) == 0 {
		prefix = ""
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(prefix + bayAwarenessBlock + "\n"); err != nil {
		return false, err
	}
	return created, nil
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

// ensureFileContainsLines appends entries from the map to the file if they
// don't already appear as whole lines. Creates the file if it doesn't exist.
func ensureFileContainsLines(path string, entries map[string]bool) error {
	data, _ := os.ReadFile(path)
	existing := make(map[string]bool)
	for line := range strings.SplitSeq(string(data), "\n") {
		existing[strings.TrimSpace(line)] = true
	}

	var toAdd []string
	for entry := range entries {
		if !existing[entry] {
			toAdd = append(toAdd, entry)
		}
	}
	if len(toAdd) == 0 {
		return nil
	}
	sort.Strings(toAdd)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	content := string(data)
	if len(content) > 0 && content[len(content)-1] != '\n' {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}
	for _, entry := range toAdd {
		if _, err := f.WriteString(entry + "\n"); err != nil {
			return err
		}
	}
	return nil
}
