package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

// WsNew creates a new workspace with surfaces.
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

	// Ensure dock exists in manifest.
	dock := m.FindDock(dockName)
	if dock == nil {
		if err := m.AddDock(manifest.Dock{Name: dockName}); err != nil {
			return nil, err
		}
		dock = m.FindDock(dockName)
	}

	// Determine workspace type and path.
	var wsType manifest.WorkspaceType
	var wsPath string
	var worktreeAttrs *manifest.WorktreeAttrs
	repoName := opts.Repo
	if repoName == "" {
		repoName = dockCfg.Repo
	}

	if opts.Dir != "" {
		// External workspace.
		wsType = manifest.WorkspaceTypeExternal
		wsPath = config.ExpandPath(opts.Dir)
		if _, err := os.Stat(wsPath); err != nil {
			return nil, fmt.Errorf("external directory %q: %w", wsPath, err)
		}
	} else {
		// Worktree workspace.
		wsType = manifest.WorkspaceTypeWorktree
		if repoName == "" {
			return nil, fmt.Errorf("dock %q has no default repo; specify --repo or --dir", dockName)
		}
		repoCfg, ok := e.Config.Repos[repoName]
		if !ok {
			return nil, fmt.Errorf("unknown repo %q", repoName)
		}
		worktreeAttrs = &manifest.WorktreeAttrs{Repo: repoName}

		// Use workspace name or a unique timestamp-based name for the worktree directory.
		// Timestamp ensures no collisions even after workspaces are removed.
		wtDir := repoCfg.EffectiveWorktreeDir()
		dirName := opts.Name
		if dirName == "" {
			dirName = "ws-" + strconv.FormatInt(time.Now().UnixMilli(), 36)
		}
		wsPath = filepath.Join(wtDir, dirName)

		if err := os.MkdirAll(wtDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating worktree dir: %w", err)
		}

		repoPath := config.ExpandPath(repoCfg.Path)
		if err := e.Git.CreateWorktree(repoPath, wsPath); err != nil {
			return nil, fmt.Errorf("creating worktree: %w", err)
		}
	}

	// Determine display name.
	displayName := opts.Name
	if displayName == "" {
		if opts.Branch != "" {
			displayName = abbreviateBranch(opts.Branch)
		} else {
			displayName = filepath.Base(wsPath)
		}
	}
	if err := ValidateName(displayName); err != nil {
		return nil, err
	}
	if dock.FindWorkspace(displayName) != nil {
		return nil, fmt.Errorf("workspace name %q already exists in dock %q", displayName, dockName)
	}

	// Determine agent.
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

	// Ensure tmux session exists.
	if err := e.ensureSession(dockName); err != nil {
		rollbackWorktree()
		return nil, err
	}

	// Create tmux window.
	windowID, err := e.Tmux.NewWindow(dockName, displayName, wsPath)
	if err != nil {
		rollbackWorktree()
		return nil, fmt.Errorf("creating tmux window: %w", err)
	}
	_ = e.Tmux.SetWindowOption(windowID, "remain-on-exit", "on")
	e.cleanPlaceholders(dockName)

	// Get the first pane in the new window.
	panes, _ := e.Tmux.ListPanes(windowID)
	var tmuxPaneID string
	if len(panes) > 0 {
		tmuxPaneID = panes[0].ID
	}

	// Launch the initial surface.
	var surfaceType manifest.SurfaceType
	var surfaceName string
	switch {
	case opts.Shell:
		surfaceType = manifest.SurfaceTypeShell
		surfaceName = "shell"
	case agentName != "":
		surfaceType = manifest.SurfaceTypeAgent
		surfaceName = "agent"
	default:
		surfaceType = manifest.SurfaceTypeShell
		surfaceName = "shell"
	}

	surface := e.launchSurfaceInTmux(tmuxPaneID, dockName, surfaceType, agentName, "")
	surface.Name = surfaceName
	surface.Tmux.PaneID = tmuxPaneID
	surface.Tmux.WindowID = windowID
	surface.Tmux.LayoutGroup = 1

	// Build workspace.
	ws := manifest.Workspace{
		Name:       displayName,
		Type:       wsType,
		Path:       wsPath,
		Status:     manifest.WorkspaceStatusIdle,
		LastActive: time.Now().Unix(),
		Worktree:   worktreeAttrs,
		Surfaces:   []manifest.Surface{},
	}

	// Add workspace to dock, then add surface (which assigns the ID).
	if err := dock.AddWorkspace(ws); err != nil {
		_ = e.Tmux.KillWindow(windowID)
		rollbackWorktree()
		return nil, err
	}
	addedWs := dock.FindWorkspace(displayName)
	if _, err := addedWs.AddSurface(surface); err != nil {
		_ = e.Tmux.KillWindow(windowID)
		rollbackWorktree()
		_ = dock.RemoveWorkspace(displayName)
		return nil, err
	}

	// Save manifest.
	if err := e.saveManifest(m); err != nil {
		_ = e.Tmux.KillWindow(windowID)
		rollbackWorktree()
		_ = dock.RemoveWorkspace(displayName)
		return nil, err
	}

	// Create git branch if requested.
	if opts.Branch != "" && addedWs.Worktree != nil {
		if err := e.Git.CreateBranch(wsPath, opts.Branch); err != nil {
			return addedWs, fmt.Errorf("workspace created but branch creation failed: %w", err)
		}
		addedWs.Worktree.Branch = opts.Branch
		if !addedWs.NameOverridden {
			addedWs.Name = abbreviateBranch(opts.Branch)
			e.updateWindowNames(addedWs, addedWs.Name)
		}
		addedWs.Status = manifest.WorkspaceStatusActive
		_ = e.saveManifest(m)
	}

	return addedWs, nil
}

// WsClose closes a workspace and all its surfaces.
func (e *Engine) WsClose(dockName, wsName string, force bool) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dock := m.FindDock(dockName)
	if dock == nil {
		return fmt.Errorf("unknown dock %q", dockName)
	}

	ws := dock.FindWorkspace(wsName)
	if ws == nil {
		return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
	}

	// Safety checks for worktree workspaces.
	if ws.Type == manifest.WorkspaceTypeWorktree && !force {
		if _, statErr := os.Stat(ws.Path); statErr == nil {
			dirty, err := e.Git.IsDirty(ws.Path)
			if err != nil {
				return fmt.Errorf("checking workspace state: %w", err)
			}
			if dirty {
				return fmt.Errorf("workspace %q has uncommitted changes (use --force to override)", wsName)
			}

			unpushed, err := e.Git.HasUnpushedCommits(ws.Path)
			if err != nil {
				return fmt.Errorf("workspace %q: could not verify push status: %w (use --force to override)", wsName, err)
			}
			if unpushed {
				return fmt.Errorf("workspace %q has unpushed commits (use --force to override)", wsName)
			}
		}
	}

	// Close all tmux windows for this workspace's surfaces.
	closedWindows := map[string]bool{}
	for _, s := range ws.Surfaces {
		if s.Tmux != nil && s.Tmux.WindowID != "" && !closedWindows[s.Tmux.WindowID] {
			e.ensurePlaceholderIfLastWindow(dockName, s.Tmux.WindowID)
			_ = e.Tmux.KillWindow(s.Tmux.WindowID)
			closedWindows[s.Tmux.WindowID] = true
		}
	}

	// Remove worktree if applicable.
	if ws.Type == manifest.WorkspaceTypeWorktree && ws.Worktree != nil && ws.Worktree.Repo != "" {
		repoCfg, ok := e.Config.Repos[ws.Worktree.Repo]
		if ok {
			repoPath := config.ExpandPath(repoCfg.Path)
			if err := e.Git.RemoveWorktree(repoPath, ws.Path, force); err != nil {
				if !force {
					return fmt.Errorf("removing worktree: %w", err)
				}
			}
			wtDir := repoCfg.EffectiveWorktreeDir()
			_ = os.Remove(wtDir)
		}
	}

	// Archive the workspace. Disambiguate name if it already exists
	// in the archive (same workspace closed and recreated before).
	archive, err := manifest.LoadArchive(e.archivePath)
	if err != nil {
		archive = manifest.New()
	}
	archiveDock := archive.FindDock(dockName)
	if archiveDock == nil {
		_ = archive.AddDock(manifest.Dock{Name: dockName})
		archiveDock = archive.FindDock(dockName)
	}
	archived := *ws
	for i := 2; archiveDock.FindWorkspace(archived.Name) != nil; i++ {
		archived.Name = fmt.Sprintf("%s-%d", ws.Name, i)
	}
	_ = archiveDock.AddWorkspace(archived)
	_ = manifest.SaveArchive(e.archivePath, archive)

	// Remove from manifest.
	if err := dock.RemoveWorkspace(wsName); err != nil {
		return err
	}
	return e.saveManifest(m)
}

// WsCloseByStatus closes all workspaces with the given status.
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
		name string
	}
	var targets []target

	for i := range m.Docks {
		d := &m.Docks[i]
		if dockName != "" && d.Name != dockName {
			continue
		}
		for j := range d.Workspaces {
			ws := &d.Workspaces[j]
			if string(ws.Status) == status {
				targets = append(targets, target{dock: d.Name, name: ws.Name})
			}
		}
	}

	if len(targets) == 0 {
		return nil, nil, fmt.Errorf("no workspaces with status %q found", status)
	}

	for _, t := range targets {
		label := t.dock + ":" + t.name
		if closeErr := e.WsClose(t.dock, t.name, force); closeErr != nil {
			skipped = append(skipped, label+" ("+closeErr.Error()+")")
		} else {
			closed = append(closed, label)
		}
	}
	return closed, skipped, nil
}

// WsUpdate updates workspace metadata (branch, PR, status).
func (e *Engine) WsUpdate(dockName, wsName string, branch, pr, status *string) error {
	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspace(wsName)
		if ws == nil {
			return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
		}

		ws.LastActive = time.Now().Unix()
		nameChanged := false

		if branch != nil && ws.Worktree != nil {
			ws.Worktree.Branch = *branch
			if !ws.NameOverridden {
				ws.Name = abbreviateBranch(*branch)
				nameChanged = true
			}
			if ws.Status == manifest.WorkspaceStatusIdle {
				ws.Status = manifest.WorkspaceStatusActive
			}
		}
		if pr != nil && ws.Worktree != nil {
			ws.Worktree.PR = *pr
		}
		if status != nil {
			if err := ValidateStatus(*status); err != nil {
				return err
			}
			ws.Status = manifest.WorkspaceStatus(*status)
		}

		if nameChanged {
			e.updateWindowNames(ws, ws.Name)
		}

		return nil
	})
}

// WsRename renames a workspace.
func (e *Engine) WsRename(dockName, wsName, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}

	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspace(wsName)
		if ws == nil {
			return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
		}

		// Check uniqueness.
		if existing := dock.FindWorkspace(newName); existing != nil {
			return fmt.Errorf("name %q already in use", newName)
		}

		ws.Name = newName
		ws.NameOverridden = true
		e.updateWindowNames(ws, newName)

		return nil
	})
}

// WsShow returns detailed information about a workspace.
func (e *Engine) WsShow(dockName, wsName string) (*manifest.Workspace, error) {
	e.SyncAll()

	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	ws := dock.FindWorkspace(wsName)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
	}
	return ws, nil
}

// ResolveWorkspace resolves a workspace query to (dockName, wsName).
// Accepts: "dock:name" or bare display name.
func (e *Engine) ResolveWorkspace(query string) (string, string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return "", "", err
	}
	ws, dock, resolveErr := m.ResolveWorkspace(query)
	if resolveErr != nil {
		return "", "", resolveErr
	}
	return dock.Name, ws.Name, nil
}

// ResolveSelf resolves the current workspace from CWD and tmux context.
func (e *Engine) ResolveSelf() (string, string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return "", "", err
	}

	// First try: match CWD against workspace paths.
	cwd, cwdErr := os.Getwd()
	if cwdErr == nil {
		for i := range m.Docks {
			dock := &m.Docks[i]
			for j := range dock.Workspaces {
				ws := &dock.Workspaces[j]
				wsPath := config.ExpandPath(ws.Path)
				if cwd == wsPath || strings.HasPrefix(cwd, wsPath+"/") {
					return dock.Name, ws.Name, nil
				}
			}
		}
	}

	// Fallback: match current tmux window ID against surfaces.
	winID, tmuxErr := e.Tmux.CurrentWindowID()
	if tmuxErr == nil {
		for i := range m.Docks {
			dock := &m.Docks[i]
			for j := range dock.Workspaces {
				ws := &dock.Workspaces[j]
				for _, s := range ws.Surfaces {
					if s.Tmux != nil && s.Tmux.WindowID == winID {
						return dock.Name, ws.Name, nil
					}
				}
			}
		}
	}

	return "", "", fmt.Errorf("not in a bay workspace")
}

// ResolveByWindowID finds the workspace that owns the given tmux window ID.
func (e *Engine) ResolveByWindowID(tmuxWindowID string) (dockName, wsName string, ws *manifest.Workspace, err error) {
	m, err := e.LoadManifest()
	if err != nil {
		return "", "", nil, err
	}
	for i := range m.Docks {
		dock := &m.Docks[i]
		for j := range dock.Workspaces {
			w := &dock.Workspaces[j]
			for _, s := range w.Surfaces {
				if s.Tmux != nil && s.Tmux.WindowID == tmuxWindowID {
					return dock.Name, w.Name, w, nil
				}
			}
		}
	}
	return "", "", nil, fmt.Errorf("no workspace found for tmux window %s", tmuxWindowID)
}

// updateWindowNames renames all tmux windows for a workspace's surfaces.
func (e *Engine) updateWindowNames(ws *manifest.Workspace, name string) {
	seen := map[string]bool{}
	for _, s := range ws.Surfaces {
		if s.Tmux != nil && s.Tmux.WindowID != "" && !seen[s.Tmux.WindowID] {
			_ = e.Tmux.RenameWindow(s.Tmux.WindowID, name)
			seen[s.Tmux.WindowID] = true
		}
	}
}

// collectWorkspaceAgents returns a set of agent names used by a workspace's surfaces.
func collectWorkspaceAgents(ws *manifest.Workspace, defaultAgent string) map[string]bool {
	agents := map[string]bool{}
	if defaultAgent != "" {
		agents[defaultAgent] = true
	}
	for _, s := range ws.Surfaces {
		if s.Agent != nil && *s.Agent != "" {
			agents[*s.Agent] = true
		}
	}
	return agents
}
