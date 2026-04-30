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
	Description  string // short free-form label shown in picker/ls/tree
	Agent        string // agent override
	RequireAgent bool   // fail if no agent can be resolved
	Shell        bool   // open shell instead of agent
	Branch       string // git branch to checkout (creates it if new)
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
	var branchExists bool
	repoName := opts.Repo
	if repoName == "" {
		repoName = dock.Repo
	}

	nameExplicit := opts.Name != ""
	displayName := opts.Name
	if displayName == "" && opts.Branch != "" {
		displayName = uniqueWorkspaceName(dock, nil, abbreviateBranch(opts.Branch))
	}
	// Validate explicit/branch-derived name early. Names matching the
	// reserved ID pattern (^w[1-9]\d*$) are rejected here.
	if displayName != "" {
		if err := ValidateWorkspaceName(displayName); err != nil {
			return nil, err
		}
	}
	desc := strings.TrimSpace(opts.Description)
	if err := ValidateDescription(desc); err != nil {
		return nil, err
	}
	if nameExplicit && dock.FindWorkspace(displayName) != nil {
		return nil, fmt.Errorf("bay name %q already exists in dock %q", displayName, dockName)
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
		if err := os.MkdirAll(wtDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating worktree dir: %w", err)
		}
		// Worktree dirs are sequential (w1, w2, ...) and independent of
		// the workspace name. The name can be renamed or auto-updated as
		// work pivots, but the on-disk directory stays stable.
		wsPath = filepath.Join(wtDir, nextWorkspaceDir(wtDir, dock))

		repoPath := config.ExpandPath(repo.Path)

		// Only fetch when --branch is used so the common case (no branch)
		// stays fast. The detached worktree uses origin/<default> which
		// relies on whatever was last fetched.
		if opts.Branch != "" {
			_ = e.Git.Fetch(repoPath)
			if exists, err := e.Git.BranchExists(repoPath, opts.Branch); err == nil && exists {
				branchExists = true
			}
		}

		wtBranch := ""
		if branchExists {
			wtBranch = opts.Branch
		}
		if err := e.Git.CreateWorktree(repoPath, wsPath, wtBranch); err != nil {
			return nil, fmt.Errorf("creating worktree: %w", err)
		}

		toCopy, err := e.resolveWorktreeInclude(repoPath)
		if err == nil {
			err = copyWorktreeIncludeFiles(repoPath, wsPath, toCopy)
		}
		if err != nil {
			_ = e.Git.RemoveWorktree(repoPath, wsPath, true)
			return nil, err
		}
	}

	// No explicit name and no branch → leave displayName empty. The
	// workspace's ID is the stable handle (set after AddWorkspace);
	// updateWindowNames falls back to the ID for the tab label, and
	// sync fills Name from the branch on first detection.

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

	// Create tmux window and position it at the end of existing workspace
	// windows so new workspaces appear rightmost in the tab bar.
	initialWindowLabel := WorkspaceCompactLabel(&manifest.Workspace{Name: displayName, Path: wsPath})
	windowID, err := e.Tmux.NewWindow(dockName, initialWindowLabel, wsPath)
	if err != nil {
		rollbackWorktree()
		return nil, fmt.Errorf("creating tmux window: %w", err)
	}
	e.cleanPlaceholders(dockName)
	e.positionNewWindow(dockName, windowID, "", m)

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

	surface, err := e.launchSurfaceInTmux(tmuxPaneID, dockName, surfaceType, agentName, "", wsPath, agentArgs, false)
	if err != nil {
		_ = e.Tmux.KillWindow(windowID)
		rollbackWorktree()
		return nil, err
	}
	surface.Name = surfaceName
	surface.Tmux.PaneID = tmuxPaneID
	surface.Tmux.WindowID = windowID
	surface.Tmux.LayoutGroup = 1

	// Build workspace. Name may be empty here (no explicit name, no branch);
	// sync fills it from the branch on first detection and is sticky after,
	// matching the design's "stable yet semantic" goal.
	ws := manifest.Workspace{
		Name:        displayName,
		Type:        wsType,
		Path:        wsPath,
		Description: desc,
		LastActive:  time.Now().Unix(),
		Worktree:    worktreeAttrs,
		Surfaces:    []manifest.Surface{},
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
			return fmt.Errorf("bay name %q already exists in dock %q", displayName, dockName)
		}

		ws.Name = finalName
		if err := dock.AddWorkspace(ws); err != nil {
			return err
		}
		// Fill in the ID for the workspace we just added. Lookups by Name
		// fail when finalName is empty, so resolve by path instead.
		manifest.AssignWorkspaceIDs(dock)
		addedWs := findWorkspaceByPath(dock, wsPath)
		if addedWs == nil {
			return fmt.Errorf("bay at %q not found after creation", wsPath)
		}
		if _, err := addedWs.AddSurface(surface); err != nil {
			_ = dock.RemoveWorkspace(addedWs.ID)
			return err
		}
		return nil
	}); err != nil {
		_ = e.Tmux.KillWindow(windowID)
		rollbackWorktree()
		return nil, err
	}
	if finalName != displayName {
		finalWindowLabel := WorkspaceCompactLabel(&manifest.Workspace{Name: finalName, Path: wsPath})
		_ = e.Tmux.RenameWindow(windowID, finalWindowLabel)
	}

	addedDock, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	dock = addedDock.FindDock(dockName)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	addedWs := findWorkspaceByPath(dock, wsPath)
	if addedWs == nil {
		return nil, fmt.Errorf("bay at %q not found after creation", wsPath)
	}

	// Set up git branch if requested. For existing branches the worktree
	// was already created on the branch; for new branches we create it now.
	if opts.Branch != "" && addedWs.Worktree != nil {
		if !branchExists {
			if err := e.Git.CreateBranch(wsPath, opts.Branch); err != nil {
				return addedWs, fmt.Errorf("bay created but branch creation failed: %w", err)
			}
		}
		if err := e.withManifest(func(m *manifest.Manifest) error {
			dock := m.FindDock(dockName)
			if dock == nil {
				return fmt.Errorf("unknown dock %q", dockName)
			}
			ws := findWorkspaceByPath(dock, wsPath)
			if ws == nil {
				return fmt.Errorf("bay at %q not found in dock %q", wsPath, dockName)
			}

			ws.Worktree.Branch = opts.Branch
			if isPlaceholderName(ws.Name) {
				newName := uniqueWorkspaceName(dock, ws, abbreviateBranch(opts.Branch))
				if newName != ws.Name {
					ws.Name = newName
					e.updateWindowNames(ws, "")
				}
			}
			return nil
		}); err != nil {
			return addedWs, fmt.Errorf("bay created but metadata update failed: %w", err)
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
		return nil, fmt.Errorf("bay at %q not found in dock %q", wsPath, dockName)
	}

	// Re-evaluate tab name lengths now that workspace count changed.
	e.refreshDockWindowNames(updatedDock)

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
func (e *Engine) closeWorkspaceState(dockName, wsID string, force bool) ([]string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	dock := m.FindDock(dockName)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	ws := dock.FindWorkspaceByID(wsID)
	if ws == nil {
		return nil, fmt.Errorf("bay %q not found in dock %q", wsID, dockName)
	}

	// Safety checks for worktree workspaces + determine if the branch
	// is safe to delete after the worktree is removed. The gate is
	// HasUnpushedCommits, not git's merge-into-default check: a
	// pushed-but-unmerged PR branch is safe because the work exists on
	// the remote, and squash-merged/cherry-picked patches are safe
	// because HasUnpushedCommits treats patch-equivalent commits on the
	// default branch as landed.
	branchSafeToDelete := false
	if ws.Type == manifest.WorkspaceTypeWorktree && !force {
		if _, statErr := os.Stat(ws.Path); statErr == nil {
			dirty, err := e.Git.IsDirty(ws.Path)
			if err != nil {
				return nil, fmt.Errorf("checking bay state: %w", err)
			}
			if dirty {
				return nil, fmt.Errorf("bay %q has uncommitted changes (use --force to override)", wsID)
			}

			unpushed, err := e.HasUnlandedCommits(ws)
			if err != nil {
				return nil, fmt.Errorf("bay %q: could not verify push status: %w (use --force to override)", wsID, err)
			}
			if unpushed {
				return nil, fmt.Errorf("bay %q has unlanded commits (use --force to override)", wsID)
			}
			// Safety checks passed → branch work exists remotely or has landed.
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
				unpushed, err := e.HasUnlandedCommits(ws)
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
		return dock.RemoveWorkspace(wsID)
	}); err != nil {
		return nil, err
	}
	return windowIDs, nil
}

// HasUnlandedCommits reports whether committed work in ws is not safely
// recoverable from a remote branch, the default branch, or the workspace's
// merged PR. The PR check handles multi-commit squash merges where per-commit
// patch comparison cannot prove that the old local stack landed.
func (e *Engine) HasUnlandedCommits(ws *manifest.Workspace) (bool, error) {
	if ws == nil || ws.Worktree == nil || ws.Path == "" {
		return false, nil
	}
	unpushed, err := e.Git.HasUnpushedCommits(ws.Path)
	if err != nil {
		if e.localHeadInMergedPR(ws) {
			return false, nil
		}
		return false, err
	}
	if !unpushed {
		return false, nil
	}
	if e.localHeadInMergedPR(ws) {
		return false, nil
	}
	return true, nil
}

func (e *Engine) localHeadInMergedPR(ws *manifest.Workspace) bool {
	if ws == nil || ws.Worktree == nil || ws.Worktree.PR == "" || ws.Path == "" {
		return false
	}
	landed, err := e.Git.LocalHeadInMergedPR(ws.Path, ws.Worktree.PR)
	return err == nil && landed
}

func (e *Engine) WsClose(dockName, wsID string, force bool) error {
	windowIDs, err := e.closeWorkspaceState(dockName, wsID, force)
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

	// Re-evaluate tab name lengths now that workspace count changed.
	e.refreshDockWindowNamesByName(dockName)
	return nil
}

// wsSkipFunc is called for each workspace during batch close. It returns a
// non-empty reason string to skip the workspace, or "" to include it.
type wsSkipFunc func(ws *manifest.Workspace, dockName string) string

// WsCloseClean closes all clean (non-dirty, no unlanded commits) workspaces.
func (e *Engine) WsCloseClean(dockName string, force, dryRun bool, exclude ...string) ([]string, []string, error) {
	return e.wsCloseBatch(dockName, force, dryRun, nil, exclude...)
}

// WsCloseDone closes workspaces that are not dirty AND not pending (have an
// unmerged branch). Only removes workspaces whose work has landed or that
// have no branch at all.
func (e *Engine) WsCloseDone(dockName string, force, dryRun bool, exclude ...string) ([]string, []string, error) {
	return e.wsCloseBatch(dockName, force, dryRun, func(ws *manifest.Workspace, dn string) string {
		if ws.Worktree != nil && ws.Worktree.Branch != "" && !ws.IsMerged() && !e.localHeadInMergedPR(ws) {
			return "pending"
		}
		return ""
	}, exclude...)
}

// wsCloseBatch is the shared implementation for batch-close operations.
// An optional skip function can pre-filter workspaces before the safety
// checks in closeWorkspaceState.
//
// Manifest updates for ALL targets are persisted before any tmux kill,
// so that bay invoked from inside one of the affected panes doesn't
// leave the rest of the targets half-closed when its host pane dies.
func (e *Engine) wsCloseBatch(dockName string, force, dryRun bool, skip wsSkipFunc, exclude ...string) (closed []string, skipped []string, err error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, nil, err
	}

	excludeSet := map[string]bool{}
	for _, name := range exclude {
		excludeSet[name] = true
	}

	type target struct {
		dock string
		id   string
		name string // for error/skip labels only
	}
	var targets []target

	for i := range m.Docks {
		d := &m.Docks[i]
		if dockName != "" && d.Name != dockName {
			continue
		}
		for j := range d.Workspaces {
			ws := &d.Workspaces[j]
			if excludeSet[ws.ID] {
				continue
			}
			if skip != nil {
				if reason := skip(ws, d.Name); reason != "" {
					skipped = append(skipped, d.Name+":"+ws.ID+" ("+reason+")")
					continue
				}
			}
			targets = append(targets, target{dock: d.Name, id: ws.ID, name: ws.Name})
		}
	}

	if len(targets) == 0 {
		if len(skipped) > 0 {
			return nil, skipped, nil
		}
		return nil, nil, fmt.Errorf("no bays found")
	}

	if dryRun {
		// Dry-run: check dirty/unlanded status but don't mutate anything.
		for _, t := range targets {
			label := t.dock + ":" + t.id
			ws := m.FindDock(t.dock).FindWorkspaceByID(t.id)
			if ws != nil && ws.Path != "" && !force {
				if dirty, err := e.Git.IsDirty(ws.Path); err == nil && dirty {
					skipped = append(skipped, label+" (dirty)")
					continue
				}
				if unpushed, err := e.HasUnlandedCommits(ws); err == nil && unpushed {
					skipped = append(skipped, label+" (unlanded)")
					continue
				}
			}
			closed = append(closed, label)
		}
		return closed, skipped, nil
	}

	// First pass: do all manifest mutations, collecting window IDs to kill.
	// closeWorkspaceState performs the dirty/unlanded safety checks, so
	// dirty workspaces are naturally skipped (added to skipped list).
	type pendingKill struct {
		dock      string
		windowIDs []string
	}
	var pending []pendingKill
	for _, t := range targets {
		label := t.dock + ":" + t.id
		ids, closeErr := e.closeWorkspaceState(t.dock, t.id, force)
		if closeErr != nil {
			skipped = append(skipped, label+" ("+closeErr.Error()+")")
			continue
		}
		closed = append(closed, label)
		pending = append(pending, pendingKill{dock: t.dock, windowIDs: ids})
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
func (e *Engine) WsUpdate(dockName, wsID string, branch, pr *string) error {
	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspaceByID(wsID)
		if ws == nil {
			return fmt.Errorf("bay %q not found in dock %q", wsID, dockName)
		}

		ws.LastActive = time.Now().Unix()
		nameChanged := false

		if branch != nil && ws.Worktree != nil {
			ws.Worktree.Branch = *branch
			if isPlaceholderName(ws.Name) {
				ws.Name = uniqueWorkspaceName(dock, ws, abbreviateBranch(*branch))
				nameChanged = true
			}
		}
		if pr != nil && ws.Worktree != nil {
			ws.Worktree.PR = *pr
		}

		if nameChanged {
			e.updateWindowNames(ws, "")
		}

		return nil
	})
}

// WsDescribe sets (or clears, if desc is "") a workspace's description.
// Descriptions appear in the workspace picker and in ls/tree output; they
// have no effect on tmux tab names, which stay short by design.
func (e *Engine) WsDescribe(dockName, wsID, desc string) error {
	desc = strings.TrimSpace(desc)
	if err := ValidateDescription(desc); err != nil {
		return err
	}
	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspaceByID(wsID)
		if ws == nil {
			return fmt.Errorf("bay %q not found in dock %q", wsID, dockName)
		}
		ws.Description = desc
		ws.LastActive = time.Now().Unix()
		return nil
	})
}

// WsRename renames a workspace.
func (e *Engine) WsRename(dockName, wsID, newName string) error {
	if err := ValidateWorkspaceName(newName); err != nil {
		return err
	}

	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspaceByID(wsID)
		if ws == nil {
			return fmt.Errorf("bay %q not found in dock %q", wsID, dockName)
		}

		// Check uniqueness.
		if existing := dock.FindWorkspace(newName); existing != nil {
			return fmt.Errorf("name %q already in use", newName)
		}

		ws.Name = newName
		ws.LastActive = time.Now().Unix()
		e.updateWindowNames(ws, "")

		return nil
	})
}

// WsShow returns detailed information about a workspace.
// This is a read-only manifest query — it does NOT call SyncAll.
// Callers that display data to the user (bay show, bay sf ls)
// should call SyncAll first. Callers that just need workspace state
// for an operation (navigation, close, restart) can skip the sync.
func (e *Engine) WsShow(dockName, wsID string) (*manifest.Workspace, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	ws := dock.FindWorkspaceByID(wsID)
	if ws == nil {
		return nil, fmt.Errorf("bay %q not found in dock %q", wsID, dockName)
	}
	return ws, nil
}

// MarkPRCheckStale resets PRCheckedAt for a workspace so the monitor's
// next sync tick re-queries gh, bypassing the TTL. Used as a
// user-interest signal: when someone reads workspace info and the PR
// is missing, that's a signal they expect to see one soon, so push
// bay to re-check before the 5-minute TTL elapses.
//
// No-op when the PR is already populated or the branch is empty — those
// states have no useful signal to act on.
func (e *Engine) MarkPRCheckStale(dockName, wsID string) error {
	return e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		dock := m.FindDock(dockName)
		if dock == nil {
			return false, nil
		}
		ws := dock.FindWorkspaceByID(wsID)
		if ws == nil {
			return false, nil
		}
		return clearPRCheckedAt(ws), nil
	})
}

// MarkAllPRChecksStale resets PRCheckedAt for every workspace that has a
// branch but no PR, in a single locked manifest update. Used by `bay tree`
// to nudge the monitor without paying N file-lock cycles.
func (e *Engine) MarkAllPRChecksStale() error {
	return e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		changed := false
		for i := range m.Docks {
			dock := &m.Docks[i]
			for j := range dock.Workspaces {
				if clearPRCheckedAt(&dock.Workspaces[j]) {
					changed = true
				}
			}
		}
		return changed, nil
	})
}

func clearPRCheckedAt(ws *manifest.Workspace) bool {
	if ws.Worktree == nil {
		return false
	}
	if ws.Worktree.Branch == "" || ws.Worktree.PR != "" {
		return false
	}
	if ws.Worktree.PRCheckedAt == 0 {
		return false
	}
	ws.Worktree.PRCheckedAt = 0
	return true
}

// ResolveWorkspace resolves a workspace query to (dockName, wsID).
// Accepts: "dock:id" or bare ID. Names are not accepted; if the query
// matches a Name, the returned error includes a "did you mean" hint
// at the canonical ID.
func (e *Engine) ResolveWorkspace(query string) (string, string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return "", "", err
	}
	ws, dock, resolveErr := m.ResolveWorkspace(query)
	if resolveErr != nil {
		return "", "", resolveErr
	}
	return dock.Name, ws.ID, nil
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
					return dock.Name, ws.ID, nil
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
						return dock.Name, ws.ID, nil
					}
				}
			}
		}
	}

	return "", "", fmt.Errorf("not in a bay")
}

// ResolveByWindowID finds the workspace that owns the given tmux window ID.
func (e *Engine) ResolveByWindowID(tmuxWindowID string) (dockName, wsID string, ws *manifest.Workspace, err error) {
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
					return dock.Name, w.ID, w, nil
				}
			}
		}
	}
	return "", "", nil, fmt.Errorf("no bay found for tmux window %s", tmuxWindowID)
}

// WorkspaceDirTag returns the path-derived display tag for a workspace.
// For worktree workspaces this is usually the stable worktree directory
// basename (w1, w2, ...).
func WorkspaceDirTag(ws *manifest.Workspace) string {
	if ws == nil || ws.Path == "" {
		return ""
	}
	clean := filepath.Clean(ws.Path)
	base := filepath.Base(clean)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

// WorkspaceCompactLabel returns the ordinary tmux/status display label for a
// workspace, preserving the path-derived dir tag when the workspace has a
// distinct semantic name.
func WorkspaceCompactLabel(ws *manifest.Workspace) string {
	if ws == nil {
		return ""
	}
	dirTag := WorkspaceDirTag(ws)
	if dirTag == "" {
		return ws.Name
	}
	if ws.Name == "" || ws.Name == dirTag {
		return dirTag
	}
	return dirTag + "." + ws.Name
}

// TruncateWorkspaceCompactLabel crops a compact workspace label to maxLen
// bytes, preserving the dir tag before the semantic name.
func TruncateWorkspaceCompactLabel(ws *manifest.Workspace, maxLen int) string {
	label := WorkspaceCompactLabel(ws)
	if maxLen <= 0 || label == "" {
		return ""
	}
	if len(label) <= maxLen {
		return label
	}

	dirTag := WorkspaceDirTag(ws)
	if dirTag == "" || ws == nil || ws.Name == "" || ws.Name == dirTag {
		return TruncateName(label, maxLen)
	}

	prefix := dirTag + "."
	if maxLen >= len(prefix)+2 {
		nameBudget := maxLen - len(prefix)
		if len(ws.Name) <= nameBudget {
			return prefix + ws.Name
		}
		return prefix + ws.Name[:nameBudget]
	}
	if len(dirTag) <= maxLen {
		return dirTag
	}
	return TruncateName(dirTag, maxLen)
}

func workspaceWindowLabel(ws *manifest.Workspace) string {
	label := WorkspaceCompactLabel(ws)
	if label != "" {
		return label
	}
	if ws != nil {
		return ws.ID
	}
	return ""
}

// updateWindowNames renames all tmux windows for a workspace's surfaces.
// Primary windows (layout group 1) get the workspace compact label; secondary
// windows get ":surfacename" where surfacename is the first surface in the
// group.
//
// When primaryLabel is empty and no compact label is available, falls back to
// the workspace's ID so the tab still has a stable label.
func (e *Engine) updateWindowNames(ws *manifest.Workspace, primaryLabel string) {
	if primaryLabel == "" {
		primaryLabel = workspaceWindowLabel(ws)
	}
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
				_ = e.Tmux.RenameWindow(s.Tmux.WindowID, primaryLabel)
			} else if surfName := firstInGroup[s.Tmux.LayoutGroup]; surfName != "" {
				_ = e.Tmux.RenameWindow(s.Tmux.WindowID, ":"+surfName)
			}
			seen[s.Tmux.WindowID] = true
		}
	}
}

// Tab-name truncation tuning.
const (
	// Fallback cells reserved for status-left + status-right when tmux can't
	// report the actual values. Real tmux reports status-left-length +
	// status-right-length, which is far more accurate than a fixed estimate.
	tabStatusBarOverheadFallback = 50
	// Per-tab overhead for window index, colon separator, and one space
	// between tabs — e.g. "1:foo " is 2 chars of overhead beyond the name.
	// Set slightly higher to leave a little safety margin.
	tabPerTabOverhead = 4
	// Floor on truncation — below this, names become unreadable.
	tabMinNameLen = 3
	// Ceiling — longer names are rare and waste status-bar space.
	tabMaxNameLen = 20
)

// refreshDockWindowNames recomputes the max tab name length for a dock based
// on the terminal width, status-left/right lengths, and workspace count, then
// renames all windows. This keeps tab names maximally informative without
// overflowing the status bar.
//
// If no tmux client is attached (ClientWidth errors or returns <= 0), the
// full name is used without truncation — we'd rather over-run the status bar
// when the user next attaches than clobber all names to 3 chars in CI / bay
// invocations from outside tmux.
func (e *Engine) refreshDockWindowNames(dock *manifest.Dock) {
	clientWidth, err := e.Tmux.ClientWidth()
	truncate := err == nil && clientWidth > 0
	maxLen := 0
	if truncate {
		reserved, rerr := e.Tmux.StatusReservedCells()
		if rerr != nil || reserved <= 0 {
			reserved = tabStatusBarOverheadFallback
		}
		maxLen = maxTabNameLen(clientWidth, reserved, len(dock.Workspaces))
	}
	for i := range dock.Workspaces {
		ws := &dock.Workspaces[i]
		name := WorkspaceCompactLabel(ws)
		if truncate {
			name = TruncateWorkspaceCompactLabel(ws, maxLen)
		}
		e.updateWindowNames(ws, name)
	}
}

// refreshDockWindowNamesByName loads the manifest, finds the named dock, and
// refreshes its tab names. Silently no-ops on errors (tab-name refresh is
// decorative — don't fail the calling operation for it).
func (e *Engine) refreshDockWindowNamesByName(dockName string) {
	m, err := e.LoadManifest()
	if err != nil {
		return
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return
	}
	e.refreshDockWindowNames(dock)
}

// maxTabNameLen computes the maximum tab name length given the terminal width,
// the cells reserved for status-left + status-right, and the workspace count.
func maxTabNameLen(clientWidth, reservedCells, wsCount int) int {
	if wsCount <= 0 {
		return tabMaxNameLen
	}
	available := clientWidth - reservedCells
	if available < 0 {
		available = 0
	}
	maxLen := available/wsCount - tabPerTabOverhead
	if maxLen < tabMinNameLen {
		maxLen = tabMinNameLen
	}
	if maxLen > tabMaxNameLen {
		maxLen = tabMaxNameLen
	}
	return maxLen
}

// TruncateName truncates a display name to maxLen, appending ".." if shortened.
// Used by status-line formatting (where byte-length math matters because the
// output is composed by overhead-tracking code expecting ASCII).
func TruncateName(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 2 {
		return s[:maxLen]
	}
	return s[:maxLen-2] + ".."
}

// truncTabEllipsis is the single-cell, single-rune ellipsis used in tab names.
// Reclaims a cell of name space vs. ".." — important when budgets are tight.
const truncTabEllipsis = "…"

// TruncateTabName is like TruncateName but uses a single-cell "…" ellipsis
// and counts in runes. Intended for tmux tab names where tight budgets make
// every cell count and tmux renders cell-based.
func TruncateTabName(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-1]) + truncTabEllipsis
}

// SetLastFocused records which surface was last focused in a workspace and
// bumps LastActive. The activity bump signals to the monitor's activity gate
// that this workspace is in active use, so its repo gets fetched on the next
// merge-detection cycle.
func (e *Engine) SetLastFocused(dockName, wsID string, surfaceID int) error {
	return e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		dock := m.FindDock(dockName)
		if dock == nil {
			return false, fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspaceByID(wsID)
		if ws == nil {
			return false, fmt.Errorf("bay %q not found in dock %q", wsID, dockName)
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

const worktreeIncludeName = ".worktreeinclude"

// resolveWorktreeInclude expands .worktreeinclude patterns, prints refusals
// to stderr (tracked matches, or matches not covered by .gitignore), and
// returns the repo-root-relative paths that should be copied.
//
// A missing .worktreeinclude yields an empty list with no error. This step
// depends only on repoRoot, so callers syncing many worktrees should call
// it once and pass the result into each copyWorktreeIncludeFiles call.
func (e *Engine) resolveWorktreeInclude(repoRoot string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(repoRoot, worktreeIncludeName)); os.IsNotExist(err) {
		return nil, nil
	}

	tracked, untracked, err := e.Git.ExpandExcludes(repoRoot, worktreeIncludeName)
	if err != nil {
		return nil, fmt.Errorf("expanding %s: %w", worktreeIncludeName, err)
	}

	var notIgnored, toCopy []string
	for _, rel := range untracked {
		ignored, err := e.Git.IsIgnored(repoRoot, rel)
		if err != nil {
			return nil, fmt.Errorf("checking ignore status of %s: %w", rel, err)
		}
		if !ignored {
			notIgnored = append(notIgnored, rel)
			continue
		}
		toCopy = append(toCopy, rel)
	}

	reportRefusal("refusing to copy tracked file(s) — remove the pattern, or untrack and .gitignore", tracked)
	reportRefusal("refusing to copy file(s) not covered by .gitignore — add them to .gitignore first, or remove the pattern", notIgnored)

	return toCopy, nil
}

func reportRefusal(reason string, files []string) {
	if len(files) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s:\n", worktreeIncludeName, reason)
	for _, f := range files {
		fmt.Fprintf(os.Stderr, "  - %s\n", f)
	}
}

// copyWorktreeIncludeFiles copies files (repo-root-relative paths produced
// by resolveWorktreeInclude) from repoRoot into worktreePath, preserving
// file mode.
func copyWorktreeIncludeFiles(repoRoot, worktreePath string, files []string) error {
	for _, rel := range files {
		src := filepath.Join(repoRoot, rel)
		info, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		srcData, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		dst := filepath.Join(worktreePath, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, srcData, info.Mode()); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
	}
	return nil
}

// nextWorkspaceDir returns the next sequential directory basename (w1, w2,
// ...) that is unclaimed — neither present on disk under wtDir nor recorded
// as the basename of any workspace's Path in the dock. Worktree directory
// names are intentionally decoupled from workspace names so that renaming
// or repurposing a workspace doesn't leave a stale dirname on disk.
func nextWorkspaceDir(wtDir string, dock *manifest.Dock) string {
	claimed := map[string]bool{}
	if dock != nil {
		for _, ws := range dock.Workspaces {
			if ws.Path != "" {
				claimed[filepath.Base(ws.Path)] = true
			}
		}
	}
	for i := 1; ; i++ {
		name := fmt.Sprintf("w%d", i)
		if claimed[name] {
			continue
		}
		if _, err := os.Stat(filepath.Join(wtDir, name)); err == nil {
			continue
		}
		return name
	}
}

// positionNewWindow moves a newly created window so that tmux tab order
// matches manifest order. For a new workspace (wsID==""), the window goes
// after the last window of the last existing workspace. For a new surface in
// an existing workspace, it goes after the last window of that workspace.
// If m is nil the manifest is loaded; callers with a pre-loaded manifest
// can pass it to avoid a second read.
func (e *Engine) positionNewWindow(dockName, windowID, wsID string, m *manifest.Manifest) {
	if m == nil {
		var err error
		m, err = e.LoadManifest()
		if err != nil {
			return
		}
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return
	}

	var afterID string
	if wsID == "" {
		// New workspace: goes after the last window of the last existing workspace.
		for i := len(dock.Workspaces) - 1; i >= 0; i-- {
			if id := lastWindowIDInWorkspace(dock, dock.Workspaces[i].ID); id != "" {
				afterID = id
				break
			}
		}
	} else {
		// New surface: goes after the last window of this workspace,
		// falling back to the last window of the previous workspace.
		afterID = lastWindowIDInWorkspace(dock, wsID)
		if afterID == "" {
			afterID = lastWindowIDBeforeWorkspace(dock, wsID)
		}
	}

	if afterID != "" && afterID != windowID {
		_ = e.Tmux.MoveWindowAfter(windowID, afterID)
	}
}

// lastWindowIDBeforeWorkspace returns the tmux window ID that a new window
// for the given workspace should be placed after, based on manifest order.
// It walks backwards through workspaces (and surfaces within the target
// workspace) to find the nearest existing window. Returns "" if none found.
func lastWindowIDBeforeWorkspace(dock *manifest.Dock, wsID string) string {
	wsIdx := -1
	for i, ws := range dock.Workspaces {
		if ws.ID == wsID {
			wsIdx = i
			break
		}
	}
	if wsIdx == -1 {
		return ""
	}
	// Walk backwards from the previous workspace to find any existing window.
	for i := wsIdx - 1; i >= 0; i-- {
		for j := len(dock.Workspaces[i].Surfaces) - 1; j >= 0; j-- {
			s := dock.Workspaces[i].Surfaces[j]
			if s.Tmux != nil && s.Tmux.WindowID != "" {
				return s.Tmux.WindowID
			}
		}
	}
	return ""
}

// lastWindowIDInWorkspace returns the last tmux window ID among the surfaces
// of the given workspace. Returns "" if the workspace has no windows.
func lastWindowIDInWorkspace(dock *manifest.Dock, wsID string) string {
	ws := dock.FindWorkspaceByID(wsID)
	if ws == nil {
		return ""
	}
	for i := len(ws.Surfaces) - 1; i >= 0; i-- {
		if ws.Surfaces[i].Tmux != nil && ws.Surfaces[i].Tmux.WindowID != "" {
			return ws.Surfaces[i].Tmux.WindowID
		}
	}
	return ""
}

func findWorkspaceByPath(dock *manifest.Dock, path string) *manifest.Workspace {
	for i := range dock.Workspaces {
		if dock.Workspaces[i].Path == path {
			return &dock.Workspaces[i]
		}
	}
	return nil
}
