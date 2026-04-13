package engine

import (
	"fmt"
	"os"
	"path/filepath"
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
		// If the directory already exists (e.g. old worktree renamed to
		// a branch but directory kept), advance to the next sequential
		// name for both workspace and directory.
		if _, err := os.Stat(wsPath); err == nil {
			displayName = nextAvailableWorkspaceName(dock, wtDir)
			wsPath = filepath.Join(wtDir, displayName)
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
	agentArgs := e.resolvedAgentArgs(dockName, agentName, m)

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

	surface, err := e.launchSurfaceInTmux(tmuxPaneID, dockName, surfaceType, agentName, "", wsPath, agentArgs)
	if err != nil {
		_ = e.Tmux.KillWindow(windowID)
		rollbackWorktree()
		return nil, err
	}
	surface.Name = surfaceName
	surface.Tmux.PaneID = tmuxPaneID
	surface.Tmux.WindowID = windowID
	surface.Tmux.LayoutGroup = 1

	// Build workspace. NameOverridden is set when the user passed an
	// explicit name, so the post-branch auto-rename block below (and
	// the SyncAll branch detector) won't silently overwrite the
	// user's choice with a branch-derived name.
	ws := manifest.Workspace{
		Name:           displayName,
		NameOverridden: nameExplicit,
		Type:           wsType,
		Path:           wsPath,
		LastActive:     time.Now().Unix(),
		Worktree:       worktreeAttrs,
		Surfaces:       []manifest.Surface{},
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

// closeWorkspaceState does the safety checks, worktree removal, archive,
// and manifest update for a workspace — but NOT the destructive tmux
// kill. Returns the tmux window IDs the caller should kill afterward.
//
// This split exists so callers can defer all destructive tmux work until
// every manifest update has been persisted. When bay is invoked from
// inside a pane being killed, tmux SIGHUPs bay shortly after the kill
// command is issued; if the manifest update were still pending at that
// moment, the user-visible state would be left inconsistent. By doing
// the manifest update first, the worst case is that bay dies between
// the manifest write and the kill — leaving the manifest correct and
// the pane briefly orphaned (next bay invocation will skip it).
//
// Callers: WsClose (kills the returned IDs immediately), DockClose
// (collects across all workspaces and uses KillSession at the end),
// RepoRemove (collects across all docks), WsCloseByStatus (collects
// across all targets).
func (e *Engine) closeWorkspaceState(dockName, wsName string, force bool) ([]string, error) {
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

	// Safety checks for worktree workspaces + determine if the branch
	// is safe to delete after the worktree is removed. The gate is
	// HasUnpushedCommits, not git's merge-into-default check — a
	// pushed-but-unmerged PR branch is safe to delete locally because
	// the work exists on the remote.
	branchSafeToDelete := false
	if ws.Type == manifest.WorkspaceTypeWorktree && !force {
		if _, statErr := os.Stat(ws.Path); statErr == nil {
			dirty, err := e.Git.IsDirty(ws.Path)
			if err != nil {
				return nil, fmt.Errorf("checking workspace state: %w", err)
			}
			if dirty {
				return nil, fmt.Errorf("workspace %q has uncommitted changes (use --force to override)", wsName)
			}

			unpushed, err := e.Git.HasUnpushedCommits(ws.Path)
			if err != nil {
				return nil, fmt.Errorf("workspace %q: could not verify push status: %w (use --force to override)", wsName, err)
			}
			if unpushed {
				return nil, fmt.Errorf("workspace %q has unpushed commits (use --force to override)", wsName)
			}
			// Safety checks passed → branch is pushed.
			if ws.Worktree != nil && ws.Worktree.Branch != "" {
				branchSafeToDelete = true
			}
		}
	} else if force && ws.Type == manifest.WorkspaceTypeWorktree {
		// Force close: still check if the branch is pushed (best-effort)
		// so we can clean it up. Don't block the close if the check
		// fails — just skip the branch delete.
		if ws.Worktree != nil && ws.Worktree.Branch != "" {
			if _, statErr := os.Stat(ws.Path); statErr == nil {
				unpushed, err := e.Git.HasUnpushedCommits(ws.Path)
				if err == nil && !unpushed {
					branchSafeToDelete = true
				}
			}
		}
	}

	// Collect window IDs (deduped) the caller should kill once all
	// manifest state is persisted.
	seen := map[string]bool{}
	var windowIDs []string
	for _, s := range ws.Surfaces {
		if s.Tmux != nil && s.Tmux.WindowID != "" && !seen[s.Tmux.WindowID] {
			seen[s.Tmux.WindowID] = true
			windowIDs = append(windowIDs, s.Tmux.WindowID)
		}
	}

	// Remove worktree from disk. This is destructive of the worktree
	// directory (and may invalidate the shell's CWD), but does not
	// kill bay's process.
	if ws.Type == manifest.WorkspaceTypeWorktree && ws.Worktree != nil && ws.Worktree.Repo != "" {
		repo := m.FindRepo(ws.Worktree.Repo)
		if repo != nil {
			repoPath := config.ExpandPath(repo.Path)
			if err := e.Git.RemoveWorktree(repoPath, ws.Path, force); err != nil {
				if !force {
					return nil, fmt.Errorf("removing worktree: %w", err)
				}
			}
			// Delete the local branch now that the worktree is gone.
			// git refuses to delete a branch checked out in a worktree,
			// so this must come after RemoveWorktree.
			if branchSafeToDelete {
				_ = e.Git.DeleteBranch(repoPath, ws.Worktree.Branch)
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
	if err := e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		return dock.RemoveWorkspace(wsName)
	}); err != nil {
		return nil, err
	}
	return windowIDs, nil
}

func (e *Engine) WsClose(dockName, wsName string, force bool) error {
	windowIDs, err := e.closeWorkspaceState(dockName, wsName, force)
	if err != nil {
		return err
	}
	// Manifest is saved. Now kill the windows — bay may die mid-call if
	// it's running in one of these panes, but the user-visible state is
	// already correct.
	for _, id := range windowIDs {
		e.ensurePlaceholderIfLastWindow(dockName, id)
		_ = e.Tmux.KillWindow(id)
	}
	return nil
}

// WsCloseClean closes all clean (non-dirty, no unpushed commits) workspaces.
//
// Manifest updates for ALL targets are persisted before any tmux kill,
// so that bay invoked from inside one of the affected panes doesn't
// leave the rest of the targets half-closed when its host pane dies.
func (e *Engine) WsCloseClean(dockName string, force bool) (closed []string, skipped []string, err error) {
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
			targets = append(targets, target{dock: d.Name, name: ws.Name})
		}
	}

	if len(targets) == 0 {
		return nil, nil, fmt.Errorf("no workspaces found")
	}

	// First pass: do all manifest mutations, collecting window IDs to kill.
	// closeWorkspaceState performs the dirty/unpushed safety checks, so
	// dirty workspaces are naturally skipped (added to skipped list).
	type pendingKill struct {
		dock      string
		windowIDs []string
	}
	var pending []pendingKill
	for _, t := range targets {
		label := t.dock + ":" + t.name
		ids, closeErr := e.closeWorkspaceState(t.dock, t.name, force)
		if closeErr != nil {
			skipped = append(skipped, label+" ("+closeErr.Error()+")")
			continue
		}
		closed = append(closed, label)
		pending = append(pending, pendingKill{dock: t.dock, windowIDs: ids})
	}

	if len(closed) == 0 {
		return nil, skipped, fmt.Errorf("no clean workspaces found")
	}

	// Second pass: kill tmux windows. Safe to die at any point — every
	// closed-workspace's manifest entry is already persisted.
	for _, p := range pending {
		for _, id := range p.windowIDs {
			e.ensurePlaceholderIfLastWindow(p.dock, id)
			_ = e.Tmux.KillWindow(id)
		}
	}
	return closed, skipped, nil
}

// WsUpdate updates workspace metadata (branch, PR).
func (e *Engine) WsUpdate(dockName, wsName string, branch, pr *string) error {
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
		}
		if pr != nil && ws.Worktree != nil {
			ws.Worktree.PR = *pr
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
// This is a read-only manifest query — it does NOT call SyncAll.
// Callers that display data to the user (bay ws show, bay sf ls)
// should call SyncAll first. Callers that just need workspace state
// for an operation (navigation, close, restart) can skip the sync.
func (e *Engine) WsShow(dockName, wsName string) (*manifest.Workspace, error) {
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
// Primary windows (layout group 1) get the workspace name; secondary windows
// get ":surfacename" where surfacename is the first surface in the group.
func (e *Engine) updateWindowNames(ws *manifest.Workspace, wsName string) {
	// Build a map of layout group → first surface name (by slice order).
	firstInGroup := map[int]string{}
	for _, s := range ws.Surfaces {
		if s.Tmux != nil && s.Tmux.LayoutGroup > 0 {
			if _, ok := firstInGroup[s.Tmux.LayoutGroup]; !ok {
				firstInGroup[s.Tmux.LayoutGroup] = s.Name
			}
		}
	}

	seen := map[string]bool{}
	for _, s := range ws.Surfaces {
		if s.Tmux != nil && s.Tmux.WindowID != "" && !seen[s.Tmux.WindowID] {
			if s.Tmux.LayoutGroup <= 1 {
				_ = e.Tmux.RenameWindow(s.Tmux.WindowID, wsName)
			} else if surfName := firstInGroup[s.Tmux.LayoutGroup]; surfName != "" {
				_ = e.Tmux.RenameWindow(s.Tmux.WindowID, ":"+surfName)
			}
			seen[s.Tmux.WindowID] = true
		}
	}
}

// SetLastFocused records which surface was last focused in a workspace and
// bumps LastActive. The activity bump signals to the monitor's activity gate
// that this workspace is in active use, so its repo gets fetched on the next
// merge-detection cycle.
func (e *Engine) SetLastFocused(dockName, wsName string, surfaceID int) error {
	return e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		dock := m.FindDock(dockName)
		if dock == nil {
			return false, fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspace(wsName)
		if ws == nil {
			return false, fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
		}
		now := time.Now().Unix()
		if ws.LastFocused == surfaceID && ws.LastActive == now {
			return false, nil // no change
		}
		ws.LastFocused = surfaceID
		ws.LastActive = now
		return true, nil
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

// nextWorkspaceName returns the next available sequential name (w1, w2, ...)
// that is not already used as a workspace name in the dock.
func nextWorkspaceName(dock *manifest.Dock) string {
	for i := 1; ; i++ {
		name := fmt.Sprintf("w%d", i)
		if dock.FindWorkspace(name) == nil {
			return name
		}
	}
}

// nextAvailableWorkspaceName returns the next sequential name (w1, w2, ...)
// that is not used as a workspace name in the dock AND does not exist as a
// directory under wtDir.
func nextAvailableWorkspaceName(dock *manifest.Dock, wtDir string) string {
	for i := 1; ; i++ {
		name := fmt.Sprintf("w%d", i)
		if dock.FindWorkspace(name) != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(wtDir, name)); err == nil {
			continue
		}
		return name
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
