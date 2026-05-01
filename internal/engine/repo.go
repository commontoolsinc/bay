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
	// Bay awareness: for each agent with a project_file, ensure it mentions bay.
	seen := map[string]bool{}
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
		if err := ensureBayAwareness(filePath); err != nil {
			return fmt.Errorf("updating %s: %w", info.ProjectFile, err)
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

	// Ensure project files are in .worktreeinclude so they get
	// copied into new worktrees automatically.
	if err := ensureFileContainsLines(wtIncludePath, seen); err != nil {
		return fmt.Errorf("updating .worktreeinclude: %w", err)
	}

	// Gitignore local project files (e.g. CLAUDE.local.md).
	gitignorePath := filepath.Join(repoPath, ".gitignore")
	if err := ensureFileContainsLines(gitignorePath, seen); err != nil {
		return fmt.Errorf("updating .gitignore: %w", err)
	}

	return nil
}

// ensureBayAwareness checks if a project file mentions bay and appends
// an awareness line if not. Creates the file if it doesn't exist.
func ensureBayAwareness(path string) error {
	const marker = "bay agent-guide"

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(data), marker) {
		return nil // already present
	}

	line := "\nThis project uses bay for bay and surface management. Run `bay agent-guide` for commands.\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
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
		wsPath := filepath.Join(wtDir, entry.Name())
		if !e.Git.IsGitRepo(wsPath) {
			continue
		}
		if err := copyWorktreeIncludeFiles(repoPath, wsPath, toCopy); err != nil {
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
