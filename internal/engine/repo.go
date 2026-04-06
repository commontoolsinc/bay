package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// RepoAdd adds a repo to the manifest.
// If cloneURL is non-empty and the path does not exist, the repo is cloned first.
// If force is false and the path is not a git repo, an error is returned.
func (e *Engine) RepoAdd(name, path, worktreeDir, cloneURL string, force bool) error {
	if err := ValidateName(name); err != nil {
		return err
	}

	// Check for duplicate name in manifest.
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}
	if m.FindRepo(name) != nil {
		return fmt.Errorf("repo %q already exists", name)
	}

	// Normalize to absolute path for storage
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

	return e.withManifest(func(m *manifest.Manifest) error {
		return m.AddRepo(manifest.Repo{
			Name:        name,
			Path:        normalizedPath,
			WorktreeDir: worktreeDir,
		})
	})
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

// RepoRemove removes a repo from the manifest.
// If force is true, also closes and removes any docks that reference this repo.
// Returns *RepoInUseError if the repo is in use and force is false.
func (e *Engine) RepoRemove(name string, force bool) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}
	if m.FindRepo(name) == nil {
		return fmt.Errorf("repo %q not found", name)
	}

	// Find all docks that reference this repo
	var affectedDockNames []string
	for i := range m.Docks {
		if m.Docks[i].Repo == name {
			affectedDockNames = append(affectedDockNames, m.Docks[i].Name)
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
	for _, dockName := range affectedDockNames {
		e.DockCloseWorkspaces(dockName, true)
	}

	// Remove repo and affected docks from manifest.
	if err := e.withManifest(func(m *manifest.Manifest) error {
		for _, dockName := range affectedDockNames {
			_ = m.RemoveDock(dockName)
		}
		return m.RemoveRepo(name)
	}); err != nil {
		return fmt.Errorf("saving manifest: %w", err)
	}

	// Remove config overrides for affected docks.
	for _, dockName := range affectedDockNames {
		delete(e.Config.Docks, dockName)
	}
	if len(affectedDockNames) > 0 && e.configPath != "" {
		_ = config.Save(e.configPath, e.Config)
	}

	// Now kill the tmux sessions (safe — manifest is saved)
	for _, dockName := range affectedDockNames {
		_ = e.Tmux.KillSession(dockName)
	}

	return nil
}

// RepoInit performs idempotent project setup for a repo:
// - Appends bay awareness line to each agent's project_file if missing.
// - Creates .worktreeinclude if it doesn't exist.
func (e *Engine) RepoInit(name string) error {
	repo, err := e.findRepo(name)
	if err != nil {
		return err
	}
	repoPath := config.ExpandPath(repo.Path)

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

// RepoList returns all repos from the manifest.
func (e *Engine) RepoList() ([]manifest.Repo, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	return m.Repos, nil
}
