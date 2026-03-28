// Package engine implements bay's core business logic, coordinating
// config, manifest, tmux, and git operations.
package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

// Engine is the core coordinator for all bay operations.
type Engine struct {
	Config       *config.Config
	ConfigPath   string
	ManifestPath string
	ArchivePath  string
	Tmux         tmux.Interface
	Git          git.Interface
}

// New creates a new Engine.
func New(cfg *config.Config, configPath, manifestPath, archivePath string, t tmux.Interface, g git.Interface) *Engine {
	return &Engine{
		Config:       cfg,
		ConfigPath:   configPath,
		ManifestPath: manifestPath,
		ArchivePath:  archivePath,
		Tmux:         t,
		Git:          g,
	}
}

// LoadManifest loads the manifest, creating an empty one if it doesn't exist.
// NOTE: LoadManifest + saveManifest do NOT hold a lock across the full
// read-modify-write cycle. Use withManifest for operations that must be
// atomic (e.g. WsUpdate, WsRename — called concurrently by agents).
// Operations like WsNew, WsClose, WinOpen are typically user-initiated
// and single-threaded, so the separate load/save is acceptable.
func (e *Engine) LoadManifest() (*manifest.Manifest, error) {
	m, err := manifest.Load(e.ManifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return manifest.New(), nil
		}
		return nil, err
	}
	return m, nil
}

// saveManifest saves the manifest with file locking.
func (e *Engine) saveManifest(m *manifest.Manifest) error {
	return manifest.Save(e.ManifestPath, m)
}

// withManifest loads the manifest, passes it to fn for modification, and saves.
// The entire read-modify-write cycle is done under a file lock.
func (e *Engine) withManifest(fn func(m *manifest.Manifest) error) error {
	return manifest.LockedUpdate(e.ManifestPath, fn)
}

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var validStatuses = map[manifest.WorkspaceStatus]bool{
	manifest.WorkspaceStatusIdle:   true,
	manifest.WorkspaceStatusActive: true,
	manifest.WorkspaceStatusDone:   true,
}

// ValidateStatus checks that a status string is one of idle/active/done.
func ValidateStatus(s string) error {
	if !validStatuses[manifest.WorkspaceStatus(s)] {
		return fmt.Errorf("invalid status %q: must be idle, active, or done", s)
	}
	return nil
}

// ValidateName checks that a name matches the naming constraints.
func ValidateName(name string) error {
	if !validNameRe.MatchString(name) {
		return fmt.Errorf("invalid name %q: must match [a-zA-Z0-9_-]+", name)
	}
	return nil
}

// abbreviateBranch strips common prefixes from branch names for display.
// The result is sanitized to be a valid display name.
func abbreviateBranch(branch string) string {
	prefixes := []string{
		"feature/", "fix/", "chore/", "bugfix/", "hotfix/",
		"release/", "refactor/", "docs/", "test/", "ci/",
	}
	name := branch
	for _, p := range prefixes {
		if strings.HasPrefix(branch, p) {
			name = branch[len(p):]
			break
		}
	}
	// Sanitize: replace invalid characters with hyphens
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := b.String()
	// Trim leading/trailing hyphens and collapse multiple hyphens
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if result == "" {
		return branch // fallback to raw branch if sanitization empties it
	}
	return result
}

const placeholderName = "~"

// ensureSession creates the tmux session if it doesn't exist.
// The default window created by new-session is tagged as a placeholder.
func (e *Engine) ensureSession(name string) error {
	exists, err := e.Tmux.HasSession(name)
	if err != nil {
		return fmt.Errorf("checking tmux session: %w", err)
	}
	if exists {
		return nil
	}
	if err := e.Tmux.NewSession(name); err != nil {
		return fmt.Errorf("creating tmux session: %w", err)
	}
	// Tag the default window as a placeholder
	windows, err := e.Tmux.ListWindows(name)
	if err == nil && len(windows) > 0 {
		_ = e.Tmux.SetWindowOption(windows[0].ID, "@bay-placeholder", "1")
		_ = e.Tmux.RenameWindow(windows[0].ID, placeholderName)
	}
	return nil
}

// cleanPlaceholders removes unused placeholder windows from a session.
// A placeholder is unused if cursor_y <= 1 (no commands have been run).
// Called after creating a real window, so the session won't be empty.
func (e *Engine) cleanPlaceholders(session string) {
	windows, err := e.Tmux.ListWindows(session)
	if err != nil {
		return
	}
	for _, win := range windows {
		val, err := e.Tmux.GetWindowOption(win.ID, "@bay-placeholder")
		if err != nil || val != "1" {
			continue
		}
		// Check if the placeholder has been used
		panes, err := e.Tmux.ListPanes(win.ID)
		if err != nil || len(panes) == 0 {
			_ = e.Tmux.KillWindow(win.ID)
			continue
		}
		cursorY, err := e.Tmux.GetPaneCursorY(panes[0].ID)
		if err != nil {
			continue // can't determine, leave it
		}
		if cursorY <= 1 {
			_ = e.Tmux.KillWindow(win.ID)
		}
		// If cursor has moved, the user is using it — leave it alone
	}
}

// ensurePlaceholderIfLastWindow checks if killing windowID would leave the
// session empty, and if so, creates a placeholder first.
func (e *Engine) ensurePlaceholderIfLastWindow(session, windowID string) {
	windows, err := e.Tmux.ListWindows(session)
	if err != nil {
		return
	}
	// Count non-placeholder windows (excluding the one about to be killed)
	remaining := 0
	for _, win := range windows {
		if win.ID == windowID {
			continue
		}
		val, _ := e.Tmux.GetWindowOption(win.ID, "@bay-placeholder")
		if val != "1" {
			remaining++
		}
	}
	if remaining > 0 {
		return // other real windows exist
	}
	// This is the last real window — create a placeholder to keep the session alive
	phID, err := e.Tmux.NewWindow(session, placeholderName, "")
	if err != nil {
		return
	}
	_ = e.Tmux.SetWindowOption(phID, "@bay-placeholder", "1")
}

// DockNew creates a new dock configuration and tmux session.
func (e *Engine) DockNew(name, repo, agent, template string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if _, exists := e.Config.Docks[name]; exists {
		return fmt.Errorf("dock %q already exists", name)
	}

	// Validate references
	if repo != "" {
		if _, ok := e.Config.Repos[repo]; !ok {
			return fmt.Errorf("unknown repo %q", repo)
		}
	}
	if agent != "" {
		if _, ok := e.Config.Agents[agent]; !ok {
			return fmt.Errorf("unknown agent %q", agent)
		}
	}

	// Check tmux session doesn't already exist
	exists, err := e.Tmux.HasSession(name)
	if err != nil {
		return fmt.Errorf("checking tmux session: %w", err)
	}
	if exists {
		return fmt.Errorf("tmux session %q already exists and is not a bay dock", name)
	}

	// Create tmux session (default window is tagged as placeholder)
	if err := e.ensureSession(name); err != nil {
		return err
	}

	// Add dock to config and save to disk
	e.Config.Docks[name] = config.DockConfig{
		Repo:                repo,
		Agent:               agent,
		AgentConfigTemplate: template,
	}
	if e.ConfigPath != "" {
		if err := config.Save(e.ConfigPath, e.Config); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}

	// Initialize dock in manifest
	return e.withManifest(func(m *manifest.Manifest) error {
		if m.Docks == nil {
			m.Docks = make(map[string]*manifest.DockState)
		}
		if _, exists := m.Docks[name]; !exists {
			m.Docks[name] = &manifest.DockState{
				Workspaces: make(map[string]*manifest.Workspace),
			}
		}
		return nil
	})
}

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
	if e.ConfigPath != "" {
		if err := config.Save(e.ConfigPath, e.Config); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}
	return nil
}

// RepoRemove removes a repo from the configuration.
// If force is true, also closes and removes any docks that reference this repo.
func (e *Engine) RepoRemove(name string, force bool) error {
	if _, exists := e.Config.Repos[name]; !exists {
		return fmt.Errorf("repo %q not found", name)
	}
	// Find docks that reference this repo
	for dockName, dock := range e.Config.Docks {
		if dock.Repo == name {
			if !force {
				return fmt.Errorf("repo %q is used by dock %q (use --force to also remove the dock)", name, dockName)
			}
			// Force: close the dock's workspaces and remove the dock
			_ = e.DockClose(dockName, true)
			delete(e.Config.Docks, dockName)
		}
	}
	delete(e.Config.Repos, name)
	if e.ConfigPath != "" {
		if err := config.Save(e.ConfigPath, e.Config); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}
	return nil
}

// RepoList returns all configured repos.
func (e *Engine) RepoList() map[string]config.RepoConfig {
	return e.Config.Repos
}

// workspaceAgent returns the effective agent for a workspace:
// the first pane's agent if set, otherwise the dock's default.
func workspaceAgent(ws *manifest.Workspace, dockAgent string) string {
	if len(ws.Windows) > 0 && len(ws.Windows[0].Panes) > 0 {
		if a := ws.Windows[0].Panes[0].Agent; a != "" {
			return a
		}
	}
	return dockAgent
}

// collectWorkspaceAgents returns all unique agent names used across a workspace's panes,
// falling back to the dock default if none found.
func collectWorkspaceAgents(ws *manifest.Workspace, dockAgent string) map[string]bool {
	agents := map[string]bool{}
	for _, win := range ws.Windows {
		for _, pane := range win.Panes {
			if pane.Type == manifest.PaneTypeAgent && pane.Agent != "" {
				agents[pane.Agent] = true
			}
		}
	}
	if len(agents) == 0 && dockAgent != "" {
		agents[dockAgent] = true
	}
	return agents
}

// updateWindowNames sets each window's Name in the manifest and renames in tmux.
func (e *Engine) updateWindowNames(ws *manifest.Workspace, displayName string) {
	for i := range ws.Windows {
		winName := displayName
		if ws.Windows[i].ID > 1 {
			winName = fmt.Sprintf("%s:%d", displayName, ws.Windows[i].ID)
		}
		ws.Windows[i].Name = winName
		if ws.Windows[i].TmuxWindowID != "" {
			_ = e.Tmux.RenameWindow(ws.Windows[i].TmuxWindowID, winName)
		}
	}
}

// DockInfo holds summary information about a dock.
type DockInfo struct {
	Name       string
	Agent      string
	Repo       string
	Workspaces []WorkspaceInfo
}

// WorkspaceInfo holds summary information about a workspace.
type WorkspaceInfo struct {
	ID      string
	Name    string
	Type    string
	Branch  string
	PR      string
	Status  string
	Waiting bool
	Agent   string
}

// WsNewOptions are options for creating a new workspace.
type WsNewOptions struct {
	Dock    string // dock name (required)
	Repo    string // repo name override (optional, defaults to dock's repo)
	Dir     string // external directory (makes it external type)
	Name    string // display name override
	Agent   string // agent override
	Shell   bool   // open shell instead of agent
}

// WsNew creates a new workspace.
func (e *Engine) WsNew(opts WsNewOptions) (*manifest.Workspace, error) {
	dockName := opts.Dock
	dockCfg, ok := e.Config.Docks[dockName]
	if !ok {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}

	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	// Ensure dock exists in manifest
	if m.Docks == nil {
		m.Docks = make(map[string]*manifest.DockState)
	}
	dockState, ok := m.Docks[dockName]
	if !ok {
		dockState = &manifest.DockState{
			Workspaces: make(map[string]*manifest.Workspace),
		}
		m.Docks[dockName] = dockState
	}

	// Allocate workspace ID
	wsID := manifest.NextWorkspaceID(dockState)

	// Determine workspace type and path
	var wsType manifest.WorkspaceType
	var wsPath string
	repoName := opts.Repo
	if repoName == "" {
		repoName = dockCfg.Repo
	}

	if opts.Dir != "" {
		// External workspace
		wsType = manifest.WorkspaceTypeExternal
		wsPath = config.ExpandPath(opts.Dir)
		if _, err := os.Stat(wsPath); err != nil {
			return nil, fmt.Errorf("external directory %q: %w", wsPath, err)
		}
	} else {
		// Worktree workspace
		wsType = manifest.WorkspaceTypeWorktree
		if repoName == "" {
			return nil, fmt.Errorf("dock %q has no default repo; specify --repo or --dir", dockName)
		}
		repoCfg, ok := e.Config.Repos[repoName]
		if !ok {
			return nil, fmt.Errorf("unknown repo %q", repoName)
		}
		wtDir := repoCfg.EffectiveWorktreeDir()
		wsPath = filepath.Join(wtDir, wsID)

		// Create worktree directory parent
		if err := os.MkdirAll(wtDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating worktree dir: %w", err)
		}

		// Create git worktree
		repoPath := config.ExpandPath(repoCfg.Path)
		if err := e.Git.CreateWorktree(repoPath, wsPath); err != nil {
			return nil, fmt.Errorf("creating worktree: %w", err)
		}
	}

	// Determine display name before config generation so template gets it
	displayName := opts.Name
	if displayName == "" {
		displayName = wsID // will be updated when branch is set
	}
	if displayName != "" && displayName != wsID {
		if err := ValidateName(displayName); err != nil {
			return nil, err
		}
		// Check uniqueness within dock
		for id, other := range dockState.Workspaces {
			if other.Name == displayName {
				return nil, fmt.Errorf("name %q already in use by %s", displayName, id)
			}
		}
	}

	// Determine agent
	agentName := opts.Agent
	if agentName == "" {
		agentName = dockCfg.Agent
	}

	// rollbackWorktree cleans up a worktree on failure.
	rollbackWorktree := func() {
		if wsType == manifest.WorkspaceTypeWorktree && repoName != "" {
			repoCfg := e.Config.Repos[repoName]
			repoPath := config.ExpandPath(repoCfg.Path)
			_ = e.Git.RemoveWorktree(repoPath, wsPath, true)
		}
	}

	// Generate agent config
	if agentName != "" && !opts.Shell {
		if err := e.generateAgentConfig(dockName, agentName, wsID, displayName, wsPath, wsType, repoName); err != nil {
			rollbackWorktree()
			return nil, fmt.Errorf("generating agent config: %w", err)
		}
	}

	// Ensure tmux session exists (default window tagged as placeholder)
	if err := e.ensureSession(dockName); err != nil {
		rollbackWorktree()
		return nil, err
	}

	// Create tmux window
	windowName := displayName
	windowID, err := e.Tmux.NewWindow(dockName, windowName, wsPath)
	if err != nil {
		rollbackWorktree()
		return nil, fmt.Errorf("creating tmux window: %w", err)
	}

	// Set remain-on-exit
	_ = e.Tmux.SetWindowOption(windowID, "remain-on-exit", "on")

	// Clean up placeholder windows now that a real window exists
	e.cleanPlaceholders(dockName)

	// Build launch command
	var paneType manifest.PaneType
	var paneAgent string
	if opts.Shell {
		paneType = manifest.PaneTypeShell
	} else if agentName != "" {
		paneType = manifest.PaneTypeAgent
		paneAgent = agentName
		cmd := e.buildAgentCommand(agentName, dockCfg)
		panes, err := e.Tmux.ListPanes(windowID)
		if err == nil && len(panes) > 0 {
			_ = e.Tmux.SendKeys(panes[0].ID, cmd)
		}
	} else {
		paneType = manifest.PaneTypeShell
	}

	// Build workspace
	ws := &manifest.Workspace{
		Name:   displayName,
		Type:   wsType,
		Repo:   repoName,
		Path:   wsPath,
		Status: manifest.WorkspaceStatusIdle,
		Windows: []manifest.Window{
			{
				ID:           1,
				TmuxWindowID: windowID,
				Name:         windowName,
				Panes: []manifest.Pane{
					{
						ID:    1,
						Type:  paneType,
						Agent: paneAgent,
					},
				},
			},
		},
	}

	// Save to manifest — if this fails, roll back everything
	dockState.Workspaces[wsID] = ws
	if err := e.saveManifest(m); err != nil {
		_ = e.Tmux.KillWindow(windowID)
		rollbackWorktree()
		delete(dockState.Workspaces, wsID)
		return nil, err
	}

	return ws, nil
}

// generateAgentConfig writes the agent's config file into the workspace CWD.
// wsRepo is the workspace's actual repo name (may differ from the dock's default).
func (e *Engine) generateAgentConfig(dockName, agentName, wsID, wsName, wsPath string, wsType manifest.WorkspaceType, wsRepo string) error {
	agentCfg, ok := e.Config.Agents[agentName]
	if !ok {
		return fmt.Errorf("unknown agent %q", agentName)
	}

	dockCfg := e.Config.Docks[dockName]

	// Determine the effective repo for gitignore checks and template vars.
	// Use the workspace's repo if set, otherwise the dock's default.
	effectiveRepo := wsRepo
	if effectiveRepo == "" {
		effectiveRepo = dockCfg.Repo
	}

	// Check gitignore against the effective repo, not the workspace CWD.
	// The workspace CWD (a worktree) may not have its own .gitignore;
	// the check should be against the repo root.
	if agentCfg.ConfigFile != "" && effectiveRepo != "" {
		repoCfg, ok := e.Config.Repos[effectiveRepo]
		if ok {
			repoPath := config.ExpandPath(repoCfg.Path)
			ignored, err := e.Git.IsIgnored(repoPath, agentCfg.ConfigFile)
			if err != nil {
				// If not a git repo, skip gitignore check
			} else if !ignored {
				return fmt.Errorf(
					"%s is not in .gitignore for repo %q — add it first or run: bay setup",
					agentCfg.ConfigFile, effectiveRepo,
				)
			}
		}
	}

	// If no template, nothing to write
	if dockCfg.AgentConfigTemplate == "" {
		return nil
	}

	// Load template
	templatePath := config.ExpandPath(dockCfg.AgentConfigTemplate)
	tmplData, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("reading template %s: %w", templatePath, err)
	}

	// Variable substitution
	content := string(tmplData)
	if wsName == "" {
		wsName = wsID
	}

	// Resolve repo paths from the effective repo, not the dock default
	dockRepo := ""
	dockWorktreeDir := ""
	if effectiveRepo != "" {
		repoCfg, hasRepo := e.Config.Repos[effectiveRepo]
		if hasRepo {
			dockRepo = config.ExpandPath(repoCfg.Path)
			dockWorktreeDir = repoCfg.EffectiveWorktreeDir()
		}
	}

	replacements := map[string]string{
		"{workspace_id}":      wsID,
		"{workspace_name}":    wsName,
		"{workspace_type}":    string(wsType),
		"{dock}":              dockName,
		"{workspace_path}":    wsPath,
		"{dock_repo}":         dockRepo,
		"{dock_worktree_dir}": dockWorktreeDir,
	}
	for k, v := range replacements {
		content = strings.ReplaceAll(content, k, v)
	}

	// Write config file
	configPath := filepath.Join(wsPath, agentCfg.ConfigFile)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// buildAgentCommand assembles the agent launch command.
func (e *Engine) buildAgentCommand(agentName string, dockCfg config.DockConfig) string {
	agentCfg := e.Config.Agents[agentName]
	parts := []string{agentCfg.Command}
	parts = append(parts, dockCfg.AgentArgs...)
	return strings.Join(parts, " ")
}

// WsClose closes a workspace and all its windows/panes.
func (e *Engine) WsClose(dockName, wsID string, force bool) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[dockName]
	if !ok {
		return fmt.Errorf("unknown dock %q", dockName)
	}

	ws, ok := dockState.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}

	// Safety checks for worktree workspaces
	if ws.Type == manifest.WorkspaceTypeWorktree && !force {
		// Only check if path exists — if deleted externally, nothing to protect
		if _, statErr := os.Stat(ws.Path); statErr == nil {
			dirty, err := e.Git.IsDirty(ws.Path)
			if err != nil {
				return fmt.Errorf("checking workspace state: %w", err)
			}
			if dirty {
				return fmt.Errorf("workspace %s has uncommitted changes (use --force to override)", wsID)
			}

			unpushed, err := e.Git.HasUnpushedCommits(ws.Path)
			if err != nil {
				// Could not determine push status — refuse to be safe
				return fmt.Errorf("workspace %s: could not verify push status: %w (use --force to override)", wsID, err)
			}
			if unpushed {
				return fmt.Errorf("workspace %s has unpushed commits (use --force to override)", wsID)
			}
		}
	}

	// Close all tmux windows. For each, check if it's the last real window
	// in the session and create a placeholder to keep the session alive.
	for _, win := range ws.Windows {
		if win.TmuxWindowID != "" {
			e.ensurePlaceholderIfLastWindow(dockName, win.TmuxWindowID)
			_ = e.Tmux.KillWindow(win.TmuxWindowID)
		}
	}

	// Clean up agent config files — use the actual agent from the workspace's panes
	e.cleanupAgentConfig(dockName, ws)

	// Remove worktree if applicable
	if ws.Type == manifest.WorkspaceTypeWorktree && ws.Repo != "" {
		repoCfg, ok := e.Config.Repos[ws.Repo]
		if ok {
			repoPath := config.ExpandPath(repoCfg.Path)
			if err := e.Git.RemoveWorktree(repoPath, ws.Path, force); err != nil {
				if !force {
					return fmt.Errorf("removing worktree: %w", err)
				}
			}
			// Clean up the worktree parent directory if it's now empty.
			// os.Remove only succeeds on empty directories.
			wtDir := repoCfg.EffectiveWorktreeDir()
			_ = os.Remove(wtDir)
		}
	}

	// Move to archive
	archive, err := manifest.LoadArchive(e.ArchivePath)
	if err != nil {
		archive = manifest.New()
	}
	if archive.Docks == nil {
		archive.Docks = make(map[string]*manifest.DockState)
	}
	if _, ok := archive.Docks[dockName]; !ok {
		archive.Docks[dockName] = &manifest.DockState{
			Workspaces: make(map[string]*manifest.Workspace),
		}
	}
	archive.Docks[dockName].Workspaces[wsID] = ws
	_ = manifest.SaveArchive(e.ArchivePath, archive)

	// Remove from manifest
	delete(dockState.Workspaces, wsID)
	return e.saveManifest(m)
}

// cleanupAgentConfig removes bay-generated config files from a workspace.
func (e *Engine) cleanupAgentConfig(dockName string, ws *manifest.Workspace) {
	dockAgent := ""
	if dc, ok := e.Config.Docks[dockName]; ok {
		dockAgent = dc.Agent
	}
	agents := collectWorkspaceAgents(ws, dockAgent)
	// Remove config files for each agent
	for agentName := range agents {
		agentCfg, ok := e.Config.Agents[agentName]
		if !ok || agentCfg.ConfigFile == "" {
			continue
		}
		configPath := filepath.Join(ws.Path, agentCfg.ConfigFile)
		_ = os.Remove(configPath)
	}
}

// WsUpdate updates workspace metadata.
func (e *Engine) WsUpdate(dockName, wsID string, branch, pr, status *string) error {
	return e.withManifest(func(m *manifest.Manifest) error {
		dockState, ok := m.Docks[dockName]
		if !ok {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws, ok := dockState.Workspaces[wsID]
		if !ok {
			return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
		}

		nameChanged := false

		if branch != nil {
			ws.Branch = *branch
			if !ws.NameOverridden {
				ws.Name = abbreviateBranch(*branch)
				nameChanged = true
			}
			if ws.Status == manifest.WorkspaceStatusIdle {
				ws.Status = manifest.WorkspaceStatusActive
			}
		}
		if pr != nil {
			ws.PR = *pr
		}
		if status != nil {
			if err := ValidateStatus(*status); err != nil {
				return err
			}
			ws.Status = manifest.WorkspaceStatus(*status)
		}

		// Update tmux window names if display name changed
		if nameChanged {
			e.updateWindowNames(ws, ws.Name)
		}

		return nil
	})
}

// WsRename renames a workspace display name (overrides auto-abbreviation).
func (e *Engine) WsRename(dockName, wsID, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}

	return e.withManifest(func(m *manifest.Manifest) error {
		dockState, ok := m.Docks[dockName]
		if !ok {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws, ok := dockState.Workspaces[wsID]
		if !ok {
			return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
		}

		// Check uniqueness within dock
		for id, other := range dockState.Workspaces {
			if id != wsID && other.Name == newName {
				return fmt.Errorf("name %q already in use by %s", newName, id)
			}
		}

		ws.Name = newName
		ws.NameOverridden = true
		e.updateWindowNames(ws, newName)

		return nil
	})
}

// WsShow returns detailed information about a workspace.
func (e *Engine) WsShow(dockName, wsID string) (*manifest.Workspace, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	dockState, ok := m.Docks[dockName]
	if !ok {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	ws, ok := dockState.Workspaces[wsID]
	if !ok {
		return nil, fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}
	return ws, nil
}

// ResolveWorkspace resolves a workspace query to (dockName, wsID).
// Accepts: "dock:wN", bare "wN" (if unambiguous), or display name.
func (e *Engine) ResolveWorkspace(query string) (string, string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return "", "", err
	}
	_, dock, id, resolveErr := m.ResolveWorkspace(query)
	return dock, id, resolveErr
}

// ResolveSelf resolves the current workspace from CWD and tmux context.
func (e *Engine) ResolveSelf() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("getting cwd: %w", err)
	}

	m, err := e.LoadManifest()
	if err != nil {
		return "", "", err
	}

	// Match CWD against workspace paths
	for dockName, dockState := range m.Docks {
		for wsID, ws := range dockState.Workspaces {
			wsPath := config.ExpandPath(ws.Path)
			if cwd == wsPath || strings.HasPrefix(cwd, wsPath+"/") {
				return dockName, wsID, nil
			}
		}
	}

	return "", "", fmt.Errorf("current directory %s does not match any workspace", cwd)
}

// WinOpen opens a new window for an existing workspace.
func (e *Engine) WinOpen(dockName, wsID string, agent string, shell bool, cmd string) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[dockName]
	if !ok {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws, ok := dockState.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}

	// Determine window ID
	winID := manifest.NextWindowID(ws)

	// Window name: workspace name + :N for non-primary windows
	winName := ws.Name
	if winID > 1 {
		winName = fmt.Sprintf("%s:%d", ws.Name, winID)
	}

	// Create tmux window
	tmuxWinID, err := e.Tmux.NewWindow(dockName, winName, ws.Path)
	if err != nil {
		return fmt.Errorf("creating tmux window: %w", err)
	}

	// Set remain-on-exit
	_ = e.Tmux.SetWindowOption(tmuxWinID, "remain-on-exit", "on")

	// Clean up placeholder windows now that a real window exists
	e.cleanPlaceholders(dockName)

	// Determine the effective agent and generate its config if needed
	effectiveAgent := agent
	if effectiveAgent == "" && !shell && cmd == "" {
		effectiveAgent = e.Config.Docks[dockName].Agent
	}
	if effectiveAgent != "" && !shell {
		// Generate agent config file for this agent type (may differ from workspace creation agent)
		_ = e.generateAgentConfig(dockName, effectiveAgent, wsID, ws.Name, ws.Path, ws.Type, ws.Repo)
	}

	// Determine pane type and launch
	dockCfg := e.Config.Docks[dockName]
	var paneType manifest.PaneType
	var paneAgent string
	var paneCmd string
	switch {
	case shell:
		paneType = manifest.PaneTypeShell
	case cmd != "":
		paneType = manifest.PaneTypeCmd
		paneCmd = cmd
		panes, err := e.Tmux.ListPanes(tmuxWinID)
		if err == nil && len(panes) > 0 {
			_ = e.Tmux.SendKeys(panes[0].ID, cmd)
		}
	case effectiveAgent != "":
		paneType = manifest.PaneTypeAgent
		paneAgent = effectiveAgent
		agentCmd := e.buildAgentCommand(effectiveAgent, dockCfg)
		panes, err := e.Tmux.ListPanes(tmuxWinID)
		if err == nil && len(panes) > 0 {
			_ = e.Tmux.SendKeys(panes[0].ID, agentCmd)
		}
	default:
		paneType = manifest.PaneTypeShell
	}

	// Add window to manifest
	win := manifest.Window{
		ID:           winID,
		TmuxWindowID: tmuxWinID,
		Name:         winName,
		Panes: []manifest.Pane{
			{
				ID:      1,
				Type:    paneType,
				Agent:   paneAgent,
				Command: paneCmd,
			},
		},
	}
	ws.Windows = append(ws.Windows, win)

	return e.saveManifest(m)
}

// WinClose closes a window (not the workspace).
func (e *Engine) WinClose(dockName, wsID string, winID int) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[dockName]
	if !ok {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws, ok := dockState.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}

	found := false
	for i, win := range ws.Windows {
		if win.ID == winID {
			if win.TmuxWindowID != "" {
				e.ensurePlaceholderIfLastWindow(dockName, win.TmuxWindowID)
				_ = e.Tmux.KillWindow(win.TmuxWindowID)
			}
			ws.Windows = append(ws.Windows[:i], ws.Windows[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("window %d not found in workspace %s", winID, wsID)
	}

	return e.saveManifest(m)
}

// WinRestart kills the agent in a window and respawns it with fresh config.
// Each pane is respawned according to its recorded type (agent/cmd/shell).
func (e *Engine) WinRestart(dockName, wsID string, winID int) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[dockName]
	if !ok {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws, ok := dockState.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}

	var win *manifest.Window
	for i := range ws.Windows {
		if ws.Windows[i].ID == winID {
			win = &ws.Windows[i]
			break
		}
	}
	if win == nil {
		return fmt.Errorf("window %d not found", winID)
	}

	dockCfg := e.Config.Docks[dockName]

	// Regenerate agent config for each agent type used in this window
	for _, pane := range win.Panes {
		if pane.Type == manifest.PaneTypeAgent && pane.Agent != "" {
			_ = e.generateAgentConfig(dockName, pane.Agent, wsID, ws.Name, ws.Path, ws.Type, ws.Repo)
		}
	}

	// Respawn each pane according to its recorded type
	if win.TmuxWindowID == "" {
		return nil
	}
	tmuxPanes, err := e.Tmux.ListPanes(win.TmuxWindowID)
	if err != nil {
		return nil
	}

	for i, pane := range win.Panes {
		if i >= len(tmuxPanes) {
			break
		}
		tmuxPaneID := tmuxPanes[i].ID

		// respawn-pane -k kills the live process and respawns atomically.
		// No separate SIGTERM needed.
		var respawnCmd string
		switch pane.Type {
		case manifest.PaneTypeAgent:
			agentName := pane.Agent
			if agentName == "" {
				agentName = dockCfg.Agent
			}
			if agentName != "" {
				if _, ok := e.Config.Agents[agentName]; ok {
					respawnCmd = e.buildAgentCommand(agentName, dockCfg)
				}
			}
		case manifest.PaneTypeCmd:
			if pane.Command != "" {
				respawnCmd = pane.Command
			}
		case manifest.PaneTypeShell:
			// Empty command = respawn-pane omits it, giving a default shell
		}
		_ = e.Tmux.RespawnPane(tmuxPaneID, ws.Path, respawnCmd)
	}

	return nil
}

// PaneAdd adds a pane to the current window.
func (e *Engine) PaneAdd(dockName, wsID string, winID int, agent string, shell bool, cmd string, splitDir string) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[dockName]
	if !ok {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws, ok := dockState.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}

	var win *manifest.Window
	for i := range ws.Windows {
		if ws.Windows[i].ID == winID {
			win = &ws.Windows[i]
			break
		}
	}
	if win == nil {
		return fmt.Errorf("window %d not found", winID)
	}

	if splitDir == "" {
		splitDir = "v"
	}

	// Determine the actual split parent: the currently active tmux pane.
	// Match it against manifest panes by position in the tmux pane list.
	splitFrom := 0
	tmuxPanesBefore, err := e.Tmux.ListPanes(win.TmuxWindowID)
	if err == nil {
		for tmuxIdx, tp := range tmuxPanesBefore {
			if tp.Active && tmuxIdx < len(win.Panes) {
				splitFrom = win.Panes[tmuxIdx].ID
				break
			}
		}
		// Fallback: if no active match, use last manifest pane
		if splitFrom == 0 && len(win.Panes) > 0 {
			splitFrom = win.Panes[len(win.Panes)-1].ID
		}
	}

	// Split the window
	newPaneID, err := e.Tmux.SplitWindow(win.TmuxWindowID, splitDir, ws.Path)
	if err != nil {
		return fmt.Errorf("splitting window: %w", err)
	}

	// Determine pane type
	paneID := manifest.NextPaneID(win)
	var paneType manifest.PaneType
	var paneAgent string
	var paneCmd string
	switch {
	case shell:
		paneType = manifest.PaneTypeShell
	case cmd != "":
		paneType = manifest.PaneTypeCmd
		paneCmd = cmd
		_ = e.Tmux.SendKeys(newPaneID, cmd)
	case agent != "":
		paneType = manifest.PaneTypeAgent
		paneAgent = agent
		dockCfg := e.Config.Docks[dockName]
		agentCmd := e.buildAgentCommand(agent, dockCfg)
		_ = e.Tmux.SendKeys(newPaneID, agentCmd)
	default:
		paneType = manifest.PaneTypeShell
	}

	pane := manifest.Pane{
		ID:        paneID,
		Type:      paneType,
		Agent:     paneAgent,
		Command:   paneCmd,
		SplitFrom: splitFrom,
		SplitDir:  splitDir,
	}
	win.Panes = append(win.Panes, pane)

	return e.saveManifest(m)
}

// DockClose closes all workspaces in a dock.
func (e *Engine) DockClose(name string, force bool) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[name]
	if !ok {
		return fmt.Errorf("unknown dock %q", name)
	}

	// Collect workspace IDs first to avoid stale-map iteration
	var wsIDs []string
	for wsID := range dockState.Workspaces {
		wsIDs = append(wsIDs, wsID)
	}

	// Close each workspace
	for _, wsID := range wsIDs {
		if err := e.WsClose(name, wsID, force); err != nil {
			if !force {
				return fmt.Errorf("workspace %s: %w", wsID, err)
			}
		}
	}

	return nil
}

// DockRecover recovers a single dock by name.
func (e *Engine) DockRecover(name string) (string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return "", err
	}

	dockState, ok := m.Docks[name]
	if !ok {
		return "", fmt.Errorf("unknown dock %q in manifest", name)
	}
	dockCfg, hasCfg := e.Config.Docks[name]

	if err := e.ensureSession(name); err != nil {
		return "", err
	}

	e.recoverDockWorkspaces(name, dockState, dockCfg, hasCfg)
	e.cleanPlaceholders(name)

	if err := e.saveManifest(m); err != nil {
		return "", err
	}

	return fmt.Sprintf("tmux attach -t %s", name), nil
}

// List returns all workspaces across all docks with waiting status.
// Used by both `bay ls` and `bay dock ls`.
func (e *Engine) List() ([]DockInfo, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	var docks []DockInfo
	for name, dockCfg := range e.Config.Docks {
		info := DockInfo{
			Name:  name,
			Agent: dockCfg.Agent,
			Repo:  dockCfg.Repo,
		}
		if ds, ok := m.Docks[name]; ok {
			for id, ws := range ds.Workspaces {
				wsInfo := WorkspaceInfo{
					ID:     id,
					Name:   ws.Name,
					Type:   string(ws.Type),
					Branch: ws.Branch,
					PR:     ws.PR,
					Status: string(ws.Status),
					Agent:  workspaceAgent(ws, dockCfg.Agent),
				}
				// Check waiting status from tmux
				for _, win := range ws.Windows {
					if win.TmuxWindowID != "" {
						val, err := e.Tmux.GetWindowOption(win.TmuxWindowID, "@bay-waiting")
						if err == nil && val == "1" {
							wsInfo.Waiting = true
						}
					}
				}
				info.Workspaces = append(info.Workspaces, wsInfo)
			}
		}
		docks = append(docks, info)
	}
	return docks, nil
}

// Recover reconstructs all tmux state from the manifest after reboot.
func (e *Engine) Recover() ([]string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	var attachCmds []string

	for dockName, dockState := range m.Docks {
		dockCfg, hasCfg := e.Config.Docks[dockName]

		if err := e.ensureSession(dockName); err != nil {
			return nil, err
		}

		e.recoverDockWorkspaces(dockName, dockState, dockCfg, hasCfg)
		e.cleanPlaceholders(dockName)

		attachCmds = append(attachCmds, fmt.Sprintf("tmux attach -t %s", dockName))
	}

	// Save updated manifest (with new tmux IDs)
	if err := e.saveManifest(m); err != nil {
		return nil, err
	}

	return attachCmds, nil
}

// recoverDockWorkspaces handles recovery for all workspaces in a dock.
func (e *Engine) recoverDockWorkspaces(dockName string, dockState *manifest.DockState, dockCfg config.DockConfig, hasCfg bool) {
	for wsID, ws := range dockState.Workspaces {
		// Verify workspace path exists
		if _, err := os.Stat(ws.Path); err != nil {
			continue
		}

		// Regenerate agent config for each agent type used in this workspace.
		dockAgent := ""
		if hasCfg {
			dockAgent = dockCfg.Agent
		}
		for agentName := range collectWorkspaceAgents(ws, dockAgent) {
			_ = e.generateAgentConfig(dockName, agentName, wsID, ws.Name, ws.Path, ws.Type, ws.Repo)
		}

		for i, win := range ws.Windows {
			// Check if window still exists
			windowExists := false
			if win.TmuxWindowID != "" {
				windowExists, _ = e.Tmux.WindowExists(win.TmuxWindowID)
			}

			// Try finding by name as fallback
			if !windowExists {
				foundID, err := e.Tmux.FindWindowByName(dockName, win.Name)
				if err == nil && foundID != "" {
					ws.Windows[i].TmuxWindowID = foundID
					windowExists = true
				}
			}

			if windowExists {
				// Window exists — reconcile panes. Check if any
				// manifest panes are missing and recreate them.
				e.reconcileWindowPanes(ws.Windows[i].TmuxWindowID, win, ws.Path, dockCfg, hasCfg)
			} else {
				// Create new window
				newID, err := e.Tmux.NewWindow(dockName, win.Name, ws.Path)
				if err != nil {
					continue
				}
				ws.Windows[i].TmuxWindowID = newID
				_ = e.Tmux.SetWindowOption(newID, "remain-on-exit", "on")

				// Recreate all panes — these are freshly created, always launch
				for j, pane := range win.Panes {
					if j == 0 {
						// First pane already exists in new window
						e.recoverPaneLaunch(pane, newID, "", dockCfg, hasCfg, true)
						continue
					}
					dir := pane.SplitDir
					if dir == "" {
						dir = "v"
					}
					newPaneID, err := e.Tmux.SplitWindow(newID, dir, ws.Path)
					if err != nil {
						continue
					}
					e.recoverPaneLaunch(pane, "", newPaneID, dockCfg, hasCfg, true)
				}
			}
		}
	}
}

// reconcileWindowPanes checks an existing window's panes against the manifest
// and repairs missing ones. Panes with live foreground processes are skipped.
func (e *Engine) reconcileWindowPanes(tmuxWindowID string, win manifest.Window, wsPath string, dockCfg config.DockConfig, hasCfg bool) {
	tmuxPanes, err := e.Tmux.ListPanes(tmuxWindowID)
	if err != nil {
		return
	}

	manifestPaneCount := len(win.Panes)
	tmuxPaneCount := len(tmuxPanes)

	// If tmux has at least as many panes as the manifest, assume they're present.
	// If tmux has fewer, recreate the missing ones.
	if tmuxPaneCount >= manifestPaneCount {
		return
	}

	// Recreate missing panes (those beyond what tmux currently has)
	for j := tmuxPaneCount; j < manifestPaneCount; j++ {
		pane := win.Panes[j]
		dir := pane.SplitDir
		if dir == "" {
			dir = "v"
		}
		newPaneID, err := e.Tmux.SplitWindow(tmuxWindowID, dir, wsPath)
		if err != nil {
			continue
		}
		e.recoverPaneLaunch(pane, "", newPaneID, dockCfg, hasCfg, true)
	}
}

// recoverPaneLaunch sends the appropriate launch command to a recovered pane.
// When newlyCreated is false (pane found in an existing window), skips panes
// that have a live foreground process to avoid corrupting running agents.
// When newlyCreated is true (pane just created by bay), always launches.
func (e *Engine) recoverPaneLaunch(pane manifest.Pane, windowID string, tmuxPaneID string, dockCfg config.DockConfig, hasCfg bool, newlyCreated bool) {
	// Determine the target pane ID
	var targetPaneID string
	if tmuxPaneID != "" {
		targetPaneID = tmuxPaneID
	} else if windowID != "" {
		// First pane: look it up from the window
		panes, err := e.Tmux.ListPanes(windowID)
		if err != nil || len(panes) == 0 {
			return
		}
		targetPaneID = panes[0].ID
	} else {
		return
	}

	// For panes in existing windows (not freshly created by recovery),
	// skip launch if a foreground process is already running.
	// Freshly created panes always have a shell PID, which is expected —
	// we need to launch into them.
	if !newlyCreated {
		pid, err := e.Tmux.GetPanePID(targetPaneID)
		if err == nil && pid > 0 {
			if proc, findErr := os.FindProcess(pid); findErr == nil {
				if proc.Signal(syscall.Signal(0)) == nil {
					return // pane has a live process, skip
				}
			}
		}
	}

	switch pane.Type {
	case manifest.PaneTypeAgent:
		agentName := pane.Agent
		if agentName == "" && hasCfg {
			agentName = dockCfg.Agent
		}
		if agentName == "" {
			return
		}
		if _, ok := e.Config.Agents[agentName]; !ok {
			return
		}
		agentCmd := e.buildAgentCommand(agentName, dockCfg)
		_ = e.Tmux.SendKeys(targetPaneID, agentCmd)
	case manifest.PaneTypeCmd:
		if pane.Command != "" {
			_ = e.Tmux.SendKeys(targetPaneID, pane.Command)
		}
	case manifest.PaneTypeShell:
		// Shell panes need no command
	}
}
