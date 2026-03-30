package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// WsNewOptions are options for creating a new workspace.
type WsNewOptions struct {
	Dock   string // dock name (required)
	Repo   string // repo name override (optional, defaults to dock's repo)
	Dir    string // external directory (makes it external type)
	Name   string // display name override
	Agent  string // agent override
	Shell  bool   // open shell instead of agent
	Branch string // create and checkout this git branch
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

	// Generate agent config if an agent is being launched.
	// For shell mode, skip — the user isn't using an agent yet. If they
	// later add an agent pane, config will be generated at that point.
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

	// Launch into the first pane
	panes, _ := e.Tmux.ListPanes(windowID)
	var tmuxPaneID string
	if len(panes) > 0 {
		tmuxPaneID = panes[0].ID
	}
	paneType, paneAgent, _ := e.launchPaneInTmux(tmuxPaneID, dockName, agentName, opts.Shell, "")

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

	// Create git branch if requested
	if opts.Branch != "" {
		if err := e.Git.CreateBranch(wsPath, opts.Branch); err != nil {
			return ws, fmt.Errorf("workspace created but branch creation failed: %w", err)
		}
		// Update workspace metadata with branch info
		branch := opts.Branch
		if updateErr := e.WsUpdate(dockName, wsID, &branch, nil, nil); updateErr != nil {
			return ws, fmt.Errorf("workspace created but metadata update failed: %w", updateErr)
		}
		// Reload workspace to reflect updates
		ws, _ = e.WsShow(dockName, wsID)
	}

	return ws, nil
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
	archive, err := manifest.LoadArchive(e.archivePath)
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
	_ = manifest.SaveArchive(e.archivePath, archive)

	// Remove from manifest
	delete(dockState.Workspaces, wsID)
	return e.saveManifest(m)
}

// WsCloseByStatus closes all workspaces with the given status across the specified
// dock (or all docks if dockName is empty). Clean workspaces are closed; dirty ones
// are skipped and reported. Returns lists of closed and skipped workspace identifiers.
func (e *Engine) WsCloseByStatus(dockName, status string, force bool) (closed []string, skipped []string, err error) {
	if err := ValidateStatus(status); err != nil {
		return nil, nil, err
	}

	m, err := e.LoadManifest()
	if err != nil {
		return nil, nil, err
	}

	type target struct {
		dock string
		id   string
	}
	var targets []target

	for dn, ds := range m.Docks {
		if dockName != "" && dn != dockName {
			continue
		}
		for wsID, ws := range ds.Workspaces {
			if string(ws.Status) == status {
				targets = append(targets, target{dock: dn, id: wsID})
			}
		}
	}

	if len(targets) == 0 {
		return nil, nil, fmt.Errorf("no workspaces with status %q found", status)
	}

	for _, t := range targets {
		label := t.dock + ":" + t.id
		if closeErr := e.WsClose(t.dock, t.id, force); closeErr != nil {
			skipped = append(skipped, label+" ("+closeErr.Error()+")")
		} else {
			closed = append(closed, label)
		}
	}
	return closed, skipped, nil
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

// ResolveByWindowID finds the workspace that owns the given tmux window ID.
// This is faster than ResolveSelf (no CWD stat calls) and works reliably in
// tmux status-line contexts where CWD may not be set.
func (e *Engine) ResolveByWindowID(tmuxWindowID string) (dockName, wsID string, ws *manifest.Workspace, err error) {
	m, err := e.LoadManifest()
	if err != nil {
		return "", "", nil, err
	}
	for dn, dockState := range m.Docks {
		for wid, w := range dockState.Workspaces {
			for _, win := range w.Windows {
				if win.TmuxWindowID == tmuxWindowID {
					return dn, wid, w, nil
				}
			}
		}
	}
	return "", "", nil, fmt.Errorf("no workspace found for tmux window %s", tmuxWindowID)
}
