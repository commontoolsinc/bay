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
	Dock         string // dock name (required)
	Repo         string // repo name override (optional, defaults to dock's repo)
	Dir          string // external directory (makes it external type)
	Name         string // display name override
	Agent        string // agent override
	RequireAgent bool   // fail if no agent can be resolved
	Shell        bool   // open shell instead of agent
	Branch       string // create and checkout this git branch
}

// WsNew creates a new workspace with surfaces.
func (e *Engine) WsNew(opts WsNewOptions) (*manifest.Workspace, error) {
	dockName := opts.Dock

	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	// Ensure dock exists in manifest.
	dock := m.FindDock(dockName)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}

	// Determine workspace type and path.
	var wsType manifest.WorkspaceType
	var wsPath string
	var worktreeAttrs *manifest.WorktreeAttrs
	repoName := opts.Repo
	if repoName == "" {
		repoName = dock.Repo
	}

	nameExplicit := opts.Name != ""
	displayName := opts.Name
	if displayName == "" {
		if opts.Branch != "" {
			displayName = uniqueWorkspaceName(dock, nil, abbreviateBranch(opts.Branch))
		} else {
			displayName = nextWorkspaceName(dock)
		}
	}
	if err := ValidateName(displayName); err != nil {
		return nil, err
	}
	if nameExplicit && dock.FindWorkspace(displayName) != nil {
		return nil, fmt.Errorf("workspace name %q already exists in dock %q", displayName, dockName)
	}

	agentName := ""
	if !opts.Shell {
		agentName, err = e.resolveWorkspaceAgent(dockName, m, opts.Agent, opts.RequireAgent)
		if err != nil {
			return nil, err
		}
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
		repo := m.FindRepo(repoName)
		if repo == nil {
			return nil, fmt.Errorf("unknown repo %q", repoName)
		}
		worktreeAttrs = &manifest.WorktreeAttrs{Repo: repoName}

		wtDir := repo.EffectiveWorktreeDir()
		wsPath = filepath.Join(wtDir, displayName)
		// If the directory already exists (name reused after close), add a timestamp suffix.
		if _, err := os.Stat(wsPath); err == nil {
			wsPath = wsPath + "-" + strconv.FormatInt(time.Now().UnixMilli(), 36)
		}

		if err := os.MkdirAll(wtDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating worktree dir: %w", err)
		}

		repoPath := config.ExpandPath(repo.Path)
		if err := e.Git.CreateWorktree(repoPath, wsPath); err != nil {
			return nil, fmt.Errorf("creating worktree: %w", err)
		}

		// Copy files listed in .worktreeinclude from repo root to worktree.
		copyWorktreeIncludeFiles(repoPath, wsPath)
	}

	// Resolve agent args.
	agentArgs := e.resolvedDockAgentArgs(dockName, m)

	// rollbackWorktree cleans up a worktree on failure.
	rollbackWorktree := func() {
		if wsType == manifest.WorkspaceTypeWorktree && repoName != "" {
			repo := m.FindRepo(repoName)
			if repo != nil {
				repoPath := config.ExpandPath(repo.Path)
				_ = e.Git.RemoveWorktree(repoPath, wsPath, true)
			}
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

	surface, err := e.launchSurfaceInTmux(tmuxPaneID, dockName, surfaceType, agentName, "", agentArgs)
	if err != nil {
		_ = e.Tmux.KillWindow(windowID)
		rollbackWorktree()
		return nil, err
	}
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

	var finalName string
	if err := e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}

		finalName = displayName
		if !nameExplicit {
			finalName = uniqueWorkspaceName(dock, nil, displayName)
		} else if dock.FindWorkspace(displayName) != nil {
			return fmt.Errorf("workspace name %q already exists in dock %q", displayName, dockName)
		}

		ws.Name = finalName
		if err := dock.AddWorkspace(ws); err != nil {
			return err
		}
		addedWs := dock.FindWorkspace(finalName)
		if addedWs == nil {
			return fmt.Errorf("workspace %q not found after creation", finalName)
		}
		if _, err := addedWs.AddSurface(surface); err != nil {
			_ = dock.RemoveWorkspace(finalName)
			return err
		}
		return nil
	}); err != nil {
		_ = e.Tmux.KillWindow(windowID)
		rollbackWorktree()
		return nil, err
	}
	if finalName != displayName {
		_ = e.Tmux.RenameWindow(windowID, finalName)
	}

	addedDock, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	dock = addedDock.FindDock(dockName)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	addedWs := dock.FindWorkspace(finalName)
	if addedWs == nil {
		return nil, fmt.Errorf("workspace %q not found after creation", finalName)
	}

	// Create git branch if requested.
	if opts.Branch != "" && addedWs.Worktree != nil {
		if err := e.Git.CreateBranch(wsPath, opts.Branch); err != nil {
			return addedWs, fmt.Errorf("workspace created but branch creation failed: %w", err)
		}
		if err := e.withManifest(func(m *manifest.Manifest) error {
			dock := m.FindDock(dockName)
			if dock == nil {
				return fmt.Errorf("unknown dock %q", dockName)
			}
			ws := findWorkspaceByPath(dock, wsPath)
			if ws == nil {
				return fmt.Errorf("workspace at %q not found in dock %q", wsPath, dockName)
			}

			ws.Worktree.Branch = opts.Branch
			if !ws.NameOverridden {
				newName := uniqueWorkspaceName(dock, ws, abbreviateBranch(opts.Branch))
				if newName != ws.Name {
					ws.Name = newName
					e.updateWindowNames(ws, ws.Name)
				}
			}
			ws.Status = manifest.WorkspaceStatusActive
			return nil
		}); err != nil {
			return addedWs, fmt.Errorf("workspace created but metadata update failed: %w", err)
		}
	}

	updatedManifest, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	updatedDock := updatedManifest.FindDock(dockName)
	if updatedDock == nil {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	updatedWs := findWorkspaceByPath(updatedDock, wsPath)
	if updatedWs == nil {
		return nil, fmt.Errorf("workspace at %q not found in dock %q", wsPath, dockName)
	}
	return updatedWs, nil
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
		repo := m.FindRepo(ws.Worktree.Repo)
		if repo != nil {
			repoPath := config.ExpandPath(repo.Path)
			if err := e.Git.RemoveWorktree(repoPath, ws.Path, force); err != nil {
				if !force {
					return fmt.Errorf("removing worktree: %w", err)
				}
			}
			wtDir := repo.EffectiveWorktreeDir()
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

	// Remove from manifest atomically in case another command updated the dock.
	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		if err := dock.RemoveWorkspace(wsName); err != nil {
			return err
		}
		return nil
	})
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
				ws.Name = uniqueWorkspaceName(dock, ws, abbreviateBranch(*branch))
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
		ws.LastActive = time.Now().Unix()
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

	// First try: match CWD against workspace paths. IsPathUnder is
	// symlink-safe — needed on macOS where /tmp → /private/tmp etc.
	cwd, cwdErr := os.Getwd()
	if cwdErr == nil {
		for i := range m.Docks {
			dock := &m.Docks[i]
			for j := range dock.Workspaces {
				ws := &dock.Workspaces[j]
				if config.IsPathUnder(cwd, ws.Path) {
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

// SetLastFocused records which surface was last focused in a workspace and
// bumps LastActive. The activity bump signals to the monitor's activity gate
// that this workspace is in active use, so its repo gets fetched on the next
// merge-detection cycle.
func (e *Engine) SetLastFocused(dockName, wsName string, surfaceID int) error {
	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspace(wsName)
		if ws == nil {
			return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
		}
		ws.LastFocused = surfaceID
		ws.LastActive = time.Now().Unix()
		return nil
	})
}

// copyWorktreeIncludeFiles reads .worktreeinclude from the repo root and copies
// matching files into the new worktree. Each line is a filename (not a glob).
// Missing source files and missing .worktreeinclude are silently ignored.
func copyWorktreeIncludeFiles(repoRoot, worktreePath string) {
	includeFile := filepath.Join(repoRoot, ".worktreeinclude")
	data, err := os.ReadFile(includeFile)
	if err != nil {
		return // no .worktreeinclude — nothing to copy
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		src := filepath.Join(repoRoot, line)
		srcData, err := os.ReadFile(src)
		if err != nil {
			continue // source file doesn't exist — skip
		}

		dst := filepath.Join(worktreePath, line)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			continue
		}

		// Preserve the source file's permissions.
		info, err := os.Stat(src)
		if err != nil {
			continue
		}
		_ = os.WriteFile(dst, srcData, info.Mode())
	}
}

// nextWorkspaceName returns the next available sequential name (w1, w2, ...) in a dock.
func nextWorkspaceName(dock *manifest.Dock) string {
	for i := 1; ; i++ {
		name := fmt.Sprintf("w%d", i)
		if dock.FindWorkspace(name) == nil {
			return name
		}
	}
}

func findWorkspaceByPath(dock *manifest.Dock, path string) *manifest.Workspace {
	for i := range dock.Workspaces {
		if dock.Workspaces[i].Path == path {
			return &dock.Workspaces[i]
		}
	}
	return nil
}
