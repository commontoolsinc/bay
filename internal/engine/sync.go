package engine

import (
	"os"

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

// SyncAll checks git branches and tmux surface state for all workspaces
// and updates the manifest if anything changed.
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
			if e.syncSurfaceTmuxState(ws) {
				changed = true
			}
		}
	}

	if changed {
		_ = e.saveManifest(m)
	}
}

// syncSurfaceTmuxState removes surfaces whose tmux windows no longer exist.
func (e *Engine) syncSurfaceTmuxState(ws *manifest.Workspace) bool {
	changed := false
	var live []manifest.Surface

	for _, s := range ws.Surfaces {
		if s.Tmux == nil || s.Tmux.WindowID == "" {
			live = append(live, s)
			continue
		}

		exists, _ := e.Tmux.WindowExists(s.Tmux.WindowID)
		if exists {
			live = append(live, s)
		} else {
			changed = true
		}
	}

	if changed {
		ws.Surfaces = live
	}
	return changed
}
