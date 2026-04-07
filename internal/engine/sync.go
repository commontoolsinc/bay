package engine

import (
	"os"
	"syscall"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

type workspaceSyncUpdate struct {
	dockName       string
	originalName   string
	path           string
	branch         string
	branchChanged  bool
	pr             string
	prBranch       string
	prChanged      bool
	mergedDone     bool
	deadSurfaceIDs map[int]bool
}

// syncWorkspaceGitState checks the actual git branch of a workspace and
// updates the manifest and tmux window names if the branch has changed.
func (e *Engine) syncWorkspaceGitState(dock *manifest.Dock, ws *manifest.Workspace) bool {
	if ws.Path == "" || ws.Worktree == nil {
		return false
	}

	wsPath := config.ExpandPath(ws.Path)
	if _, err := os.Stat(wsPath); err != nil {
		return false
	}

	branch, err := e.Git.CurrentBranch(wsPath)
	if err != nil || branch == "" {
		return false
	}

	if branch == ws.Worktree.Branch {
		return false
	}

	ws.Worktree.Branch = branch

	if ws.Status == manifest.WorkspaceStatusIdle {
		ws.Status = manifest.WorkspaceStatusActive
	}

	if !ws.NameOverridden {
		newName := uniqueWorkspaceName(dock, ws, abbreviateBranch(branch))
		if newName != ws.Name {
			ws.Name = newName
			e.updateWindowNames(ws, ws.Name)
		}
	}

	return true
}

// syncWorkspaceMergeStatus checks whether a workspace's branch has been
// merged into origin/<default>, and if so sets its status to done. Uses
// git merge-base --is-ancestor against already-fetched data — no network
// calls. The monitor daemon is responsible for fetching; this check just
// reads whatever's already local.
//
// Returns true if the status was changed.
func (e *Engine) syncWorkspaceMergeStatus(ws *manifest.Workspace) bool {
	if ws.Worktree == nil || ws.Worktree.Branch == "" || ws.Path == "" {
		return false
	}
	if ws.Status == manifest.WorkspaceStatusDone {
		return false
	}
	wsPath := config.ExpandPath(ws.Path)
	if _, err := os.Stat(wsPath); err != nil {
		return false
	}
	merged, err := e.Git.IsMergedIntoDefault(wsPath, ws.Worktree.Branch)
	if err != nil || !merged {
		return false
	}
	ws.Status = manifest.WorkspaceStatusDone
	return true
}

// syncWorkspacePR looks up the PR number for a workspace with a branch but
// no PR and no PRChecked sentinel. Returns true if the manifest needs saving.
//
// Uses the PRChecked flag to avoid re-hammering workspaces that genuinely
// have no PR: after the first definitive check, PRChecked is set and we
// stop calling gh. Transient errors (gh missing, auth, network) leave
// PRChecked false so we retry on the next display.
func (e *Engine) syncWorkspacePR(ws *manifest.Workspace) bool {
	if ws.Path == "" || ws.Worktree == nil || ws.Worktree.Branch == "" {
		return false
	}
	// Already checked (either PR found, or confirmed no PR)?
	if ws.Worktree.PR != "" || ws.Worktree.PRChecked {
		return false
	}

	wsPath := config.ExpandPath(ws.Path)
	if _, err := os.Stat(wsPath); err != nil {
		return false
	}

	pr, err := e.Git.PRForBranch(wsPath, ws.Worktree.Branch)
	if err != nil {
		// gh unavailable or transient — retry next sync.
		return false
	}
	// Definitive answer: either a PR number, or confirmed no PR.
	ws.Worktree.PR = pr
	ws.Worktree.PRChecked = true
	return true
}

// SyncAll checks git branches, PR numbers, and tmux surface state for all
// workspaces and updates the manifest if anything changed.
func (e *Engine) SyncAll() {
	m, err := e.LoadManifest()
	if err != nil {
		return
	}

	var updates []workspaceSyncUpdate
	for i := range m.Docks {
		dock := &m.Docks[i]
		for j := range dock.Workspaces {
			if update, ok := e.probeWorkspaceSync(dock, &dock.Workspaces[j]); ok {
				updates = append(updates, update)
			}
		}
	}
	if len(updates) == 0 {
		return
	}

	_ = e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		changed := false
		for _, update := range updates {
			if e.applyWorkspaceSyncUpdate(m, update) {
				changed = true
			}
		}
		return changed, nil
	})
}

func (e *Engine) probeWorkspaceSync(dock *manifest.Dock, ws *manifest.Workspace) (workspaceSyncUpdate, bool) {
	update := workspaceSyncUpdate{
		dockName:     dock.Name,
		originalName: ws.Name,
		path:         ws.Path,
	}

	if ws.Path != "" && ws.Worktree != nil {
		wsPath := config.ExpandPath(ws.Path)
		if _, err := os.Stat(wsPath); err == nil {
			branch, err := e.Git.CurrentBranch(wsPath)
			if err == nil && branch != "" && branch != ws.Worktree.Branch {
				update.branch = branch
				update.branchChanged = true
			}

			branchForChecks := ws.Worktree.Branch
			if update.branchChanged {
				branchForChecks = update.branch
			}
			if branchForChecks != "" && ws.Worktree.PR == "" && !ws.Worktree.PRChecked {
				pr, err := e.Git.PRForBranch(wsPath, branchForChecks)
				if err == nil {
					update.pr = pr
					update.prBranch = branchForChecks
					update.prChanged = true
				}
			}
			if branchForChecks != "" && ws.Status != manifest.WorkspaceStatusDone {
				merged, err := e.Git.IsMergedIntoDefault(wsPath, branchForChecks)
				if err == nil && merged {
					update.mergedDone = true
				}
			}
		}
	}

	update.deadSurfaceIDs = e.deadSurfaceIDs(ws)
	if !update.branchChanged && !update.prChanged && !update.mergedDone && len(update.deadSurfaceIDs) == 0 {
		return workspaceSyncUpdate{}, false
	}
	return update, true
}

func (e *Engine) applyWorkspaceSyncUpdate(m *manifest.Manifest, update workspaceSyncUpdate) bool {
	dock := m.FindDock(update.dockName)
	if dock == nil {
		return false
	}

	ws := findWorkspaceByPath(dock, update.path)
	if ws == nil {
		ws = dock.FindWorkspace(update.originalName)
	}
	if ws == nil {
		return false
	}

	changed := false
	if update.branchChanged && ws.Worktree != nil && ws.Worktree.Branch != update.branch {
		ws.Worktree.Branch = update.branch
		changed = true
		if ws.Status == manifest.WorkspaceStatusIdle {
			ws.Status = manifest.WorkspaceStatusActive
			changed = true
		}
		if !ws.NameOverridden {
			newName := uniqueWorkspaceName(dock, ws, abbreviateBranch(update.branch))
			if newName != ws.Name {
				ws.Name = newName
				e.updateWindowNames(ws, ws.Name)
				changed = true
			}
		}
	}

	if update.prChanged && ws.Worktree != nil && ws.Worktree.Branch == update.prBranch && ws.Worktree.PR == "" && !ws.Worktree.PRChecked {
		ws.Worktree.PR = update.pr
		ws.Worktree.PRChecked = true
		changed = true
	}

	if update.mergedDone && ws.Status != manifest.WorkspaceStatusDone {
		ws.Status = manifest.WorkspaceStatusDone
		changed = true
	}

	if len(update.deadSurfaceIDs) > 0 {
		live := ws.Surfaces[:0]
		removed := false
		for _, s := range ws.Surfaces {
			if update.deadSurfaceIDs[s.ID] {
				removed = true
				continue
			}
			live = append(live, s)
		}
		if removed {
			ws.Surfaces = live
			changed = true
		}
	}

	return changed
}

func (e *Engine) deadSurfaceIDs(ws *manifest.Workspace) map[int]bool {
	dead := map[int]bool{}
	for _, s := range ws.Surfaces {
		switch {
		case s.Tmux != nil && s.Tmux.PaneID != "":
			exists, _ := e.Tmux.PaneExists(s.Tmux.PaneID)
			if !exists {
				dead[s.ID] = true
			}
		case s.GUI != nil && s.GUI.PID > 0:
			if !processAlive(s.GUI.PID) {
				dead[s.ID] = true
			}
		case s.GUI != nil:
			dead[s.ID] = true
		}
	}
	return dead
}

// syncSurfaceState removes dead surfaces — tmux surfaces whose windows
// no longer exist, and GUI surfaces whose processes have exited.
func (e *Engine) syncSurfaceState(ws *manifest.Workspace) bool {
	changed := false
	var live []manifest.Surface

	for _, s := range ws.Surfaces {
		switch {
		case s.Tmux != nil && s.Tmux.PaneID != "":
			exists, _ := e.Tmux.PaneExists(s.Tmux.PaneID)
			if exists {
				live = append(live, s)
			} else {
				changed = true
			}

		case s.GUI != nil && s.GUI.PID > 0:
			if processAlive(s.GUI.PID) {
				live = append(live, s)
			} else {
				changed = true
			}

		default:
			// No tmux window and no trackable PID — remove.
			// This covers GUI surfaces with PID 0 (untracked).
			if s.GUI != nil {
				changed = true
			} else {
				live = append(live, s)
			}
		}
	}

	if changed {
		ws.Surfaces = live
	}
	return changed
}

// processAlive checks if a process with the given PID is still running.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
