package engine

import (
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// prSyncConcurrency caps the number of concurrent gh pr view calls during
// the parallel PR sync pass. Each call shells out and takes ~500ms; bounding
// keeps display latency reasonable on docks with many fresh workspaces.
const prSyncConcurrency = 10

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
// no PR. Returns true if the manifest needs saving.
//
// Uses the PRCheckedAt timestamp + PRCheckTTL to avoid hammering gh: after a
// definitive check, we wait PRCheckTTL before re-querying. The TTL ensures
// that a PR opened after the first check is eventually picked up. Transient
// errors (gh missing, auth, network) leave PRCheckedAt unchanged so we retry
// on the next display.
func (e *Engine) syncWorkspacePR(ws *manifest.Workspace) bool {
	if ws.Path == "" || !ws.Worktree.NeedsPRCheck(time.Now().Unix()) {
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
	ws.Worktree.PRCheckedAt = time.Now().Unix()
	return true
}

// SyncAll checks git branches, PR numbers, and tmux surface state for all
// workspaces and updates the manifest if anything changed.
func (e *Engine) SyncAll() {
	m, err := e.LoadManifest()
	if err != nil {
		return
	}

	// Run PR detection in parallel for any workspaces that need it. This
	// pass blocks until all gh calls return; the per-workspace loop below
	// then sees fresh PRCheckedAt timestamps and skips them.
	changed := e.parallelSyncPRs(m)

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
			if e.syncWorkspaceMergeStatus(ws) {
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

// parallelSyncPRs runs gh pr view in parallel for all workspaces in the
// manifest that need PR detection. Bounded by prSyncConcurrency. Returns
// true if any workspace's PR state was updated.
//
// Each goroutine writes to a different *Workspace, so the writes are
// disjoint and need no mutex. The wg.Wait happens-before any subsequent
// reads in SyncAll.
func (e *Engine) parallelSyncPRs(m *manifest.Manifest) bool {
	now := time.Now().Unix()
	var todo []*manifest.Workspace
	for i := range m.Docks {
		dock := &m.Docks[i]
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			if ws.Path == "" || !ws.Worktree.NeedsPRCheck(now) {
				continue
			}
			todo = append(todo, ws)
		}
	}
	if len(todo) == 0 {
		return false
	}

	sem := make(chan struct{}, prSyncConcurrency)
	var wg sync.WaitGroup
	var changed int64

	for _, ws := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func(ws *manifest.Workspace) {
			defer wg.Done()
			defer func() { <-sem }()

			wsPath := config.ExpandPath(ws.Path)
			if _, err := os.Stat(wsPath); err != nil {
				return
			}
			pr, err := e.Git.PRForBranch(wsPath, ws.Worktree.Branch)
			if err != nil {
				// Transient — leave PRCheckedAt unchanged so we retry.
				return
			}
			ws.Worktree.PR = pr
			ws.Worktree.PRCheckedAt = time.Now().Unix()
			atomic.AddInt64(&changed, 1)
		}(ws)
	}
	wg.Wait()

	return changed > 0
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
