package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// RepoAdd adds a repo to the configuration.
// If cloneURL is non-empty and the path does not exist, the repo is cloned first.
// If force is false and the path is not a git repo, an error is returned.
func (e *Engine) RepoAdd(name, path, worktreeDir, cloneURL string, force bool) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if _, exists := e.Config.Repos[name]; exists {
		return fmt.Errorf("repo %q already exists", name)
	}

	// Normalize to absolute path for config storage
	normalizedPath := config.NormalizePath(path)
	expandedPath := config.ExpandPath(normalizedPath)

	if cloneURL != "" {
		// Clone mode: destination must NOT exist
		if _, err := os.Stat(expandedPath); err == nil {
			return fmt.Errorf("directory %q already exists — cannot clone into an existing directory", expandedPath)
		}
		if err := e.Git.Clone(cloneURL, expandedPath); err != nil {
			return fmt.Errorf("cloning %s: %w", cloneURL, err)
		}
	} else {
		// Local mode: path must exist
		info, err := os.Stat(expandedPath)
		if err != nil {
			return fmt.Errorf("path %q: %w", expandedPath, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("path %q is not a directory", expandedPath)
		}
		// Verify it's a git repo
		if !force && !e.Git.IsGitRepo(expandedPath) {
			return fmt.Errorf("path %q is not a git repository (use --force to add anyway)", expandedPath)
		}
	}

	// Normalize worktree dir too if provided
	if worktreeDir != "" {
		worktreeDir = config.NormalizePath(worktreeDir)
	}

	e.Config.Repos[name] = config.RepoConfig{
		Path:        normalizedPath,
		WorktreeDir: worktreeDir,
	}
	if e.configPath != "" {
		if err := config.Save(e.configPath, e.Config); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}
	return nil
}

// RepoInUseError is returned when a repo cannot be removed because docks reference it.
// The AffectedDocks field contains DockInfo for each dock that would be removed.
type RepoInUseError struct {
	RepoName      string
	AffectedDocks []DockInfo
}

func (e *RepoInUseError) Error() string {
	return fmt.Sprintf("repo %q is in use by %d dock(s)", e.RepoName, len(e.AffectedDocks))
}

// RepoRemove removes a repo from the configuration.
// If force is true, also closes and removes any docks that reference this repo.
// Returns *RepoInUseError if the repo is in use and force is false.
func (e *Engine) RepoRemove(name string, force bool) error {
	if _, exists := e.Config.Repos[name]; !exists {
		return fmt.Errorf("repo %q not found", name)
	}

	// Find all docks that reference this repo
	var affectedDockNames []string
	for dockName, dock := range e.Config.Docks {
		if dock.Repo == name {
			affectedDockNames = append(affectedDockNames, dockName)
		}
	}

	if len(affectedDockNames) > 0 && !force {
		// Build DockInfo for affected docks so the CLI can format them
		docks, _ := e.List()
		var affected []DockInfo
		for _, d := range docks {
			for _, dockName := range affectedDockNames {
				if d.Name == dockName {
					affected = append(affected, d)
				}
			}
		}
		return &RepoInUseError{RepoName: name, AffectedDocks: affected}
	}

	// Force path: close all workspaces in affected docks.
	// DockClose handles workspace iteration, manifest saves, and
	// session kills. But killing a session we're inside would
	// terminate us before we save config. So we split the work:
	// 1. Close workspaces (DockCloseWorkspaces — no session kill)
	// 2. Save config
	// 3. Kill sessions

	for _, dockName := range affectedDockNames {
		e.DockCloseWorkspaces(dockName, true)
		delete(e.Config.Docks, dockName)
	}

	delete(e.Config.Repos, name)
	if e.configPath != "" {
		if err := config.Save(e.configPath, e.Config); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}

	if len(affectedDockNames) > 0 {
		if err := e.withManifest(func(m *manifest.Manifest) error {
			for _, dockName := range affectedDockNames {
				_ = m.RemoveDock(dockName)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("saving manifest: %w", err)
		}
	}

	// Now kill the tmux sessions (safe — config is saved)
	for _, dockName := range affectedDockNames {
		_ = e.Tmux.KillSession(dockName)
	}

	return nil
}

// RepoInit performs idempotent project setup for a repo:
// - Appends bay awareness line to each agent's project_file if missing.
// - Creates .worktreeinclude if it doesn't exist.
func (e *Engine) RepoInit(name string) error {
	repoCfg, ok := e.Config.Repos[name]
	if !ok {
		return fmt.Errorf("repo %q not found", name)
	}
	repoPath := config.ExpandPath(repoCfg.Path)

	// Bay awareness: for each agent with a project_file, ensure it mentions bay.
	for _, agent := range e.Config.Agents {
		if agent.ProjectFile == "" {
			continue
		}
		filePath := filepath.Join(repoPath, agent.ProjectFile)
		if err := ensureBayAwareness(filePath); err != nil {
			return fmt.Errorf("updating %s: %w", agent.ProjectFile, err)
		}
	}

	// .worktreeinclude: create with a comment if it doesn't exist.
	wtIncludePath := filepath.Join(repoPath, ".worktreeinclude")
	if _, err := os.Stat(wtIncludePath); os.IsNotExist(err) {
		content := "# Files to copy into new worktrees (gitignore pattern syntax).\n# Example: .env\n"
		if err := os.WriteFile(wtIncludePath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("creating .worktreeinclude: %w", err)
		}
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

	line := "\nThis project uses bay for workspace management. Run `bay agent-guide` for commands.\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

// RepoList returns all configured repos.
func (e *Engine) RepoList() map[string]config.RepoConfig {
	return e.Config.Repos
}
