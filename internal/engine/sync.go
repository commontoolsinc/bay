package engine

import (
	"os"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// syncWorkspaceGitState checks the actual git branch of a workspace and
// updates the manifest and tmux window names if the branch has changed.
// Returns true if any changes were made.
func (e *Engine) syncWorkspaceGitState(ws *manifest.Workspace) bool {
	if ws.Path == "" {
		return false
	}

	wsPath := config.ExpandPath(ws.Path)
	if _, err := os.Stat(wsPath); err != nil {
		return false // path missing, nothing to sync
	}

	branch, err := e.Git.CurrentBranch(wsPath)
	if err != nil {
		return false
	}

	// No change
	if branch == ws.Branch {
		return false
	}

	// Don't overwrite a manually-set branch with empty (detached HEAD).
	// Only update when git has an actual branch name.
	if branch == "" {
		return false
	}

	// Branch changed — update manifest
	ws.Branch = branch

	// Auto-promote idle → active when a branch appears
	if branch != "" && ws.Status == manifest.WorkspaceStatusIdle {
		ws.Status = manifest.WorkspaceStatusActive
	}

	// Auto-update display name if not manually overridden
	if !ws.NameOverridden && branch != "" {
		ws.Name = abbreviateBranch(branch)
		e.updateWindowNames(ws, ws.Name)
	}

	return true
}

// SyncAllGitState checks git branches for all workspaces and updates
// the manifest if anything changed. Called by List() and WsShow() to
// ensure displayed data is fresh.
func (e *Engine) SyncAllGitState() {
	m, err := e.LoadManifest()
	if err != nil {
		return
	}

	changed := false
	for _, dockState := range m.Docks {
		for _, ws := range dockState.Workspaces {
			if e.syncWorkspaceGitState(ws) {
				changed = true
			}
		}
	}

	if changed {
		_ = e.saveManifest(m)
	}
}

// SyncManifestPanes trims a window's pane list to match the actual tmux pane count.
func (e *Engine) SyncManifestPanes(dockName, wsID string, winID, actualCount int) {
	_ = e.withManifest(func(m *manifest.Manifest) error {
		ds, ok := m.Docks[dockName]
		if !ok {
			return nil
		}
		ws, ok := ds.Workspaces[wsID]
		if !ok {
			return nil
		}
		for i := range ws.Windows {
			if ws.Windows[i].ID == winID {
				if len(ws.Windows[i].Panes) > actualCount {
					ws.Windows[i].Panes = ws.Windows[i].Panes[:actualCount]
				}
				return nil
			}
		}
		return nil
	})
}
