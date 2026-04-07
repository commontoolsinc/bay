package engine

import (
	"os"
	"syscall"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// syncWorkspaceGitState checks the actual git branch of a workspace and
// updates the manifest and tmux window names if the branch has changed.
func (e *Engine) syncWorkspaceGitState(ws *manifest.Workspace) bool {
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
		ws.Name = abbreviateBranch(branch)
		e.updateWindowNames(ws, ws.Name)
	}

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

	changed := false
	for i := range m.Docks {
		dock := &m.Docks[i]
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			if e.syncWorkspaceGitState(ws) {
				changed = true
			}
			if e.syncWorkspacePR(ws) {
				changed = true
			}
			if e.syncSurfaceState(ws) {
				changed = true
			}
		}
	}

	if changed {
		_ = e.saveManifest(m)
	}
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
