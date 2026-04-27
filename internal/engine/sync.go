package engine

import (
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// syncProbeConcurrency caps the number of concurrent probe goroutines in
// SyncAll. Each probe shells out (CurrentBranch, optional PRForBranch via
// gh, IsMergedIntoDefault) — gh is the slowest at ~500ms, so bounding keeps
// display latency reasonable on docks with many fresh workspaces.
const syncProbeConcurrency = 10

// orphanGraceSeconds is how long a newly-emptied workspace lingers
// before auto-close finalizes. The user can cancel by re-adding a
// surface (bay sf new --ws <name>) during this window. Var (not
// const) so tests can set it to 0 to exercise immediate finalization.
var orphanGraceSeconds int64 = 60

type workspaceSyncUpdate struct {
	dockName           string
	originalName       string
	path               string
	branch             string
	branchChanged      bool
	branchDetached     bool
	detachedBranch     string // previous branch name when branchDetached is true
	branchSafeToDelete bool   // detached branch was pushed; safe to delete locally
	pr                 string
	prBranch           string
	prChanged          bool
	merged             bool
	deadSurfaceIDs     map[int]bool
}

// SyncAll checks git branches, PR numbers, merge status, and tmux surface state
// for all workspaces and updates the manifest if anything changed.
//
// The probe phase runs in parallel (bounded to syncProbeConcurrency) so that
// fresh docks with many workspaces don't pay N × ~500ms gh latency on first
// display. Probes only read the manifest, so the parallelism is safe.
func (e *Engine) SyncAll() {
	m, err := e.LoadManifest()
	if err != nil {
		return
	}

	type probeJob struct {
		dock *manifest.Dock
		ws   *manifest.Workspace
	}
	var jobs []probeJob
	for i := range m.Docks {
		dock := &m.Docks[i]
		for j := range dock.Workspaces {
			jobs = append(jobs, probeJob{dock: dock, ws: &dock.Workspaces[j]})
		}
	}
	if len(jobs) == 0 {
		return
	}

	results := make([]workspaceSyncUpdate, len(jobs))
	valid := make([]bool, len(jobs))

	sem := make(chan struct{}, syncProbeConcurrency)
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, job probeJob) {
			defer wg.Done()
			defer func() { <-sem }()
			if update, ok := e.probeWorkspaceSync(job.dock, job.ws); ok {
				results[i] = update
				valid[i] = true
			}
		}(i, job)
	}
	wg.Wait()

	var updates []workspaceSyncUpdate
	for i, ok := range valid {
		if ok {
			updates = append(updates, results[i])
		}
	}
	// Note: no early return on `len(updates) == 0` — the pending-close
	// scan in the closure below must still run to finalize any
	// previously-scheduled auto-closes whose grace windows have
	// elapsed since the last sync.

	var dockNames []string
	// Workspaces whose PendingCloseAt has expired — finalize via
	// WsClose after the lock releases (WsClose acquires its own lock).
	type orphanCandidate struct{ dock, ws string }
	var toFinalize []orphanCandidate

	_ = e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		changed := false
		for _, update := range updates {
			if e.applyWorkspaceSyncUpdate(m, update) {
				changed = true
			}
		}

		// Clean up dead dock-level surfaces.
		for i := range m.Docks {
			dock := &m.Docks[i]
			if len(dock.Surfaces) == 0 {
				continue
			}
			sessionAlive, _ := e.Tmux.HasSession(dock.Name)
			if !sessionAlive {
				continue
			}
			live := dock.Surfaces[:0]
			for _, s := range dock.Surfaces {
				if s.Tmux != nil && s.Tmux.PaneID != "" {
					exists, _ := e.Tmux.PaneExists(s.Tmux.PaneID)
					if !exists {
						changed = true
						continue
					}
				}
				live = append(live, s)
			}
			dock.Surfaces = live
		}

		// Scan for pending-close workspaces: clear the flag if a
		// surface was re-added since scheduling (user saved it);
		// collect the expired ones for post-lock finalization.
		now := time.Now().Unix()
		for di := range m.Docks {
			dock := &m.Docks[di]
			for wi := range dock.Workspaces {
				ws := &dock.Workspaces[wi]
				if ws.PendingCloseAt == 0 {
					continue
				}
				if len(ws.Surfaces) > 0 {
					ws.PendingCloseAt = 0
					changed = true
					continue
				}
				if ws.PendingCloseAt <= now {
					toFinalize = append(toFinalize, orphanCandidate{dock.Name, ws.Name})
				}
			}
		}

		// Capture dock names so we can refresh tab names after the
		// lock is released (tmux RPCs shouldn't block the manifest).
		dockNames = make([]string, len(m.Docks))
		for i := range m.Docks {
			dockNames[i] = m.Docks[i].Name
		}
		return changed, nil
	})

	// Finalize expired pending closes. Best-effort: any failure
	// (gate refused, I/O error) leaves PendingCloseAt set; we clear
	// it on gate failures to stop retrying every sync pass (a
	// permanent orphan is on the user to resolve).
	for _, o := range toFinalize {
		if stillEmpty, err := e.workspaceIsEmpty(o.dock, o.ws); err != nil || !stillEmpty {
			continue
		}
		if err := e.WsClose(o.dock, o.ws, false); err != nil {
			e.clearPendingClose(o.dock, o.ws)
		}
	}

	// Refresh tab name lengths for all docks.
	for _, name := range dockNames {
		e.refreshDockWindowNamesByName(name)
	}
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
			} else if err == nil && branch == "" && ws.Worktree.Branch != "" {
				update.branchDetached = true
				update.detachedBranch = ws.Worktree.Branch
				// Check now (in the probe) so the apply phase doesn't
				// hold the manifest lock during a shell-out.
				if unpushed, upErr := e.HasUnlandedCommits(ws); upErr == nil && !unpushed {
					update.branchSafeToDelete = true
				}
			}

			branchForChecks := ws.Worktree.Branch
			if update.branchChanged {
				branchForChecks = update.branch
			}
			// PR check: a branch change always forces a re-query (the
			// cached PR belongs to the old branch). Otherwise the
			// TTL-based sentinel decides — we re-query after PRCheckTTL
			// even if the previous answer was "no PR", so a PR opened
			// after the first check is eventually picked up.
			now := time.Now().Unix()
			shouldCheckPR := branchForChecks != "" &&
				(update.branchChanged || ws.Worktree.NeedsPRCheck(now))
			if shouldCheckPR {
				pr, err := e.Git.PRForBranch(wsPath, branchForChecks)
				if err == nil {
					update.pr = pr
					update.prBranch = branchForChecks
					update.prChanged = true
				}
			}
			// Re-check merge status on branchChanged even if the stored
			// flag says merged — the flag belongs to the old branch.
			// Apply clears it below; this probe then re-populates it for
			// the new branch when appropriate.
			if branchForChecks != "" && (update.branchChanged || !ws.IsMerged()) {
				merged, err := e.Git.IsMergedIntoDefault(wsPath, branchForChecks)
				if err == nil && merged {
					update.merged = true
				}
			}
		}
	}

	// Only clean up dead surfaces if the dock's tmux session is alive.
	// If the session is gone (reboot, manual kill), the surfaces are
	// needed for `bay recover` to know what to recreate. Stripping
	// them here would leave workspaces with surfaces=0 and recover
	// would report "nothing to recover."
	sessionAlive, _ := e.Tmux.HasSession(dock.Name)
	if sessionAlive {
		update.deadSurfaceIDs = e.deadSurfaceIDs(ws)
	}
	if !update.branchChanged && !update.branchDetached && !update.prChanged && !update.merged && len(update.deadSurfaceIDs) == 0 {
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
		// Cached PR belongs to the old branch — invalidate it. The probe
		// will already have queried gh for the new branch (via the
		// branchChanged bypass) and may set PR below. If the probe's
		// query failed transiently, the cleared state means future syncs
		// treat this as fresh and try again, rather than displaying the
		// wrong PR.
		ws.Worktree.PR = ""
		ws.Worktree.PRCheckedAt = 0
		// Merged belongs to the old branch too. Without this, a worktree
		// that gets reused after its prior branch was merged stays stuck
		// at Merged=true — blocking PR detection and display until the
		// workspace is recreated.
		ws.Worktree.Merged = false
		changed = true
		// Sticky-once-set: only fill Name from the branch when it's a
		// placeholder (empty or auto-assigned w<N>). User-set Names are
		// stable — live branch info is on right-status, so the tab label
		// doesn't need to follow.
		if isPlaceholderName(ws.Name) {
			newName := uniqueWorkspaceName(dock, ws, abbreviateBranch(update.branch))
			if newName != ws.Name {
				ws.Name = newName
				e.updateWindowNames(ws, ws.Name)
				changed = true
			}
		}
	}

	if update.branchDetached && ws.Worktree != nil && ws.Worktree.Branch != "" {
		// Delete the local branch if the probe determined it was pushed.
		// Without this, the branch name is lost once metadata is cleared,
		// and closeWorkspaceState can never clean it up.
		if update.branchSafeToDelete && ws.Worktree.Repo != "" {
			if repo := m.FindRepo(ws.Worktree.Repo); repo != nil {
				repoPath := config.ExpandPath(repo.Path)
				_ = e.Git.DeleteBranch(repoPath, update.detachedBranch)
			}
		}
		ws.Worktree.Branch = ""
		ws.Worktree.PR = ""
		ws.Worktree.PRCheckedAt = 0
		// No branch → no meaningful "merged" status; clear so the flag
		// doesn't resurrect when the worktree gets a new branch later.
		ws.Worktree.Merged = false
		changed = true
		// Name stays sticky: a previously-set Name persists even after the
		// branch is gone. The display may be slightly stale relative to the
		// current worktree state, but stability matters more, and right-status
		// shows the live branch (or its absence). The user can bay rename if
		// they want the label to follow.
	}

	if update.prChanged && ws.Worktree != nil && ws.Worktree.Branch == update.prBranch && ws.Worktree.PR == "" {
		ws.Worktree.PR = update.pr
		// Use time.Now() rather than a probe-time timestamp so concurrent
		// processes can't write a stale value over a fresher one.
		ws.Worktree.PRCheckedAt = time.Now().Unix()
		changed = true
	}

	if update.merged && ws.Worktree != nil && !ws.IsMerged() {
		ws.Worktree.Merged = true
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
			// If the strip emptied the workspace, schedule auto-close
			// after a grace window. The user has that long to re-open
			// a surface (bay sf new --ws <name>) to cancel.
			if len(ws.Surfaces) == 0 && ws.PendingCloseAt == 0 {
				ws.PendingCloseAt = time.Now().Unix() + orphanGraceSeconds
			}
		}
	}

	return changed
}

// clearPendingClose resets PendingCloseAt on a workspace to 0. Used
// after a finalize attempt fails so the orphan doesn't get retried
// on every sync pass; the user must resolve the underlying issue
// (dirty, unlanded) and close manually.
func (e *Engine) clearPendingClose(dockName, wsName string) {
	_ = e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return nil
		}
		ws := dock.FindWorkspace(wsName)
		if ws == nil {
			return nil
		}
		ws.PendingCloseAt = 0
		return nil
	})
}

// workspaceIsEmpty returns true if the workspace exists and has zero
// surfaces at this moment. Used by the orphan auto-close guard to
// skip workspaces that gained a surface concurrently with sync.
func (e *Engine) workspaceIsEmpty(dockName, wsName string) (bool, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return false, err
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return false, nil
	}
	ws := dock.FindWorkspace(wsName)
	if ws == nil {
		return false, nil
	}
	return len(ws.Surfaces) == 0, nil
}

func (e *Engine) deadSurfaceIDs(ws *manifest.Workspace) map[int]bool {
	dead := map[int]bool{}
	for _, s := range ws.Surfaces {
		if s.Tmux != nil && s.Tmux.PaneID != "" {
			exists, _ := e.Tmux.PaneExists(s.Tmux.PaneID)
			if !exists {
				dead[s.ID] = true
			}
		}
	}
	return dead
}

// processAlive checks if a process with the given PID is still running.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
