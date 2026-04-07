package engine

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// seedWorktreeWorkspace adds a worktree-type workspace with a branch set to
// the manifest, ensures its path exists on disk (so the sync doesn't bail
// early on os.Stat), and returns the workspace path.
func seedWorktreeWorkspace(t *testing.T, eng *Engine, dockName, wsName, branch string) string {
	t.Helper()
	wsPath := filepath.Join(t.TempDir(), wsName)
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		t.Fatalf("mkdir ws path: %v", err)
	}
	err := eng.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		dock.Workspaces = append(dock.Workspaces, manifest.Workspace{
			Name: wsName,
			Type: manifest.WorkspaceTypeWorktree,
			Path: wsPath,
			Worktree: &manifest.WorktreeAttrs{
				Repo:   "labs",
				Branch: branch,
			},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	return wsPath
}

// --- syncWorkspacePR ---

func TestSyncAll_PopulatesPRWhenFound(t *testing.T) {
	eng, _ := testEngine(t)
	wsPath := seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/login")

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetPR(wsPath, "feature/login", "42")

	eng.SyncAll()

	m, _ := eng.LoadManifest()
	ws := m.FindDock("labs").FindWorkspace("w1")
	if ws.Worktree.PR != "42" {
		t.Errorf("PR = %q, want 42", ws.Worktree.PR)
	}
	if ws.Worktree.PRCheckedAt == 0 {
		t.Error("PRCheckedAt should be set after successful lookup")
	}
}

func TestSyncAll_MarksCheckedWhenNoPRFound(t *testing.T) {
	eng, _ := testEngine(t)
	_ = seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/no-pr")

	// Mock returns "" (no PR) with nil error — the definitive "no PR" case.
	eng.SyncAll()

	m, _ := eng.LoadManifest()
	ws := m.FindDock("labs").FindWorkspace("w1")
	if ws.Worktree.PR != "" {
		t.Errorf("PR = %q, want empty", ws.Worktree.PR)
	}
	if ws.Worktree.PRCheckedAt == 0 {
		t.Error("PRCheckedAt should be set after definitive 'no PR' answer")
	}
}

func TestSyncAll_DoesNotRecheckRecentlyCheckedWorkspace(t *testing.T) {
	eng, _ := testEngine(t)
	_ = seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/no-pr")

	// First sync — sets PRCheckedAt with empty PR.
	eng.SyncAll()

	mockGit := eng.Git.(*git.Mock)
	before := len(mockGit.Calls("PRForBranch"))

	// Second sync — should NOT call PRForBranch again for this workspace.
	eng.SyncAll()

	if after := len(mockGit.Calls("PRForBranch")); after != before {
		t.Errorf("second SyncAll should not call PRForBranch on already-checked workspace: before=%d, after=%d", before, after)
	}
}

func TestSyncAll_DoesNotRecheckWorkspaceWithPRSet(t *testing.T) {
	eng, _ := testEngine(t)
	wsPath := seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/login")

	// Pre-set PR on the workspace (simulates an existing manifest).
	_ = eng.withManifest(func(m *manifest.Manifest) error {
		ws := m.FindDock("labs").FindWorkspace("w1")
		ws.Worktree.PR = "99"
		return nil
	})

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetPR(wsPath, "feature/login", "42") // different from cached
	before := len(mockGit.Calls("PRForBranch"))

	eng.SyncAll()

	if after := len(mockGit.Calls("PRForBranch")); after != before {
		t.Errorf("SyncAll should not re-query PR when one is already cached: before=%d, after=%d", before, after)
	}

	m, _ := eng.LoadManifest()
	ws := m.FindDock("labs").FindWorkspace("w1")
	if ws.Worktree.PR != "99" {
		t.Errorf("PR = %q, want 99 (cached value should stick)", ws.Worktree.PR)
	}
}

func TestSyncAll_RechecksAfterTTLExpires(t *testing.T) {
	// The "stale no PR" bug: after a definitive "no PR" answer, opening a PR
	// later should be picked up. We simulate this by aging the PRCheckedAt
	// timestamp past PRCheckTTL.
	eng, _ := testEngine(t)
	wsPath := seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/late-pr")

	// First sync — mock has no PR for the branch, so the result is empty.
	eng.SyncAll()

	m, _ := eng.LoadManifest()
	ws := m.FindDock("labs").FindWorkspace("w1")
	if ws.Worktree.PR != "" {
		t.Fatalf("PR = %q, want empty after first sync", ws.Worktree.PR)
	}
	if ws.Worktree.PRCheckedAt == 0 {
		t.Fatal("PRCheckedAt should be set after first sync")
	}

	// Now: a PR is opened, and the cached PRCheckedAt is aged past the TTL.
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetPR(wsPath, "feature/late-pr", "501")
	_ = eng.withManifest(func(m *manifest.Manifest) error {
		ws := m.FindDock("labs").FindWorkspace("w1")
		ws.Worktree.PRCheckedAt = 1 // ancient
		return nil
	})

	eng.SyncAll()

	m, _ = eng.LoadManifest()
	ws = m.FindDock("labs").FindWorkspace("w1")
	if ws.Worktree.PR != "501" {
		t.Errorf("PR = %q, want 501 after TTL expiry + re-sync", ws.Worktree.PR)
	}
}

func TestSyncAll_BranchChangeInvalidatesCachedPR(t *testing.T) {
	// Regression: when a workspace's branch changes (detected by sync from
	// the actual git state), the cached PR — which belongs to the OLD
	// branch — must be invalidated and re-queried for the new branch.
	// Otherwise users see the wrong PR after switching branches.
	eng, _ := testEngine(t)
	wsPath := seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/old")

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetPR(wsPath, "feature/old", "100")

	// First sync — caches PR=100 for the old branch.
	eng.SyncAll()
	m, _ := eng.LoadManifest()
	ws := m.FindDock("labs").FindWorkspace("w1")
	if ws.Worktree.PR != "100" {
		t.Fatalf("first sync: PR = %q, want 100", ws.Worktree.PR)
	}

	// Now: branch changes on disk to feature/new. The new branch has its
	// own PR #200. Crucially, PRCheckedAt is recent — the old TTL-based
	// logic would have skipped the re-query.
	mockGit.SetBranch(wsPath, "feature/new")
	mockGit.SetPR(wsPath, "feature/new", "200")

	eng.SyncAll()

	m, _ = eng.LoadManifest()
	// Workspace got renamed by abbreviateBranch.
	ws = m.FindDock("labs").FindWorkspace("new")
	if ws == nil {
		t.Fatalf("workspace 'new' missing after rename")
	}
	if ws.Worktree.Branch != "feature/new" {
		t.Errorf("Branch = %q, want feature/new", ws.Worktree.Branch)
	}
	if ws.Worktree.PR != "200" {
		t.Errorf("PR = %q, want 200 (new branch's PR, not the cached old one)", ws.Worktree.PR)
	}
}

func TestSyncAll_BranchChangeClearsCacheWhenGHFails(t *testing.T) {
	// Belt-and-braces: even if the new gh query fails (no PR found), the
	// cached PR for the old branch must NOT remain. Otherwise users see
	// the old PR next to the new branch — strictly worse than no PR at all.
	eng, _ := testEngine(t)
	wsPath := seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/old")

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetPR(wsPath, "feature/old", "100")

	// First sync — caches PR=100.
	eng.SyncAll()

	// Branch changes; new branch has no PR (mock default).
	mockGit.SetBranch(wsPath, "feature/new")

	eng.SyncAll()

	m, _ := eng.LoadManifest()
	ws := m.FindDock("labs").FindWorkspace("new")
	if ws == nil {
		t.Fatalf("workspace 'new' missing after rename")
	}
	if ws.Worktree.PR != "" {
		t.Errorf("PR = %q, want empty (new branch has no PR; old cached value should not persist)", ws.Worktree.PR)
	}
}

func TestSyncAll_ParallelChecksMultipleWorkspaces(t *testing.T) {
	eng, _ := testEngine(t)
	mockGit := eng.Git.(*git.Mock)

	// Create N workspaces with branches and configure mock PR numbers.
	const n = 5
	for i := 0; i < n; i++ {
		wsName := "w" + strconv.Itoa(i+1)
		branch := "feature/branch-" + strconv.Itoa(i+1)
		wsPath := seedWorktreeWorkspace(t, eng, "labs", wsName, branch)
		mockGit.SetPR(wsPath, branch, "10"+strconv.Itoa(i))
	}

	eng.SyncAll()

	// All N workspaces should have PR populated and PRCheckedAt set.
	m, _ := eng.LoadManifest()
	for i := 0; i < n; i++ {
		wsName := "w" + strconv.Itoa(i+1)
		ws := m.FindDock("labs").FindWorkspace(wsName)
		if ws == nil {
			t.Errorf("workspace %q missing", wsName)
			continue
		}
		wantPR := "10" + strconv.Itoa(i)
		if ws.Worktree.PR != wantPR {
			t.Errorf("%s: PR = %q, want %q", wsName, ws.Worktree.PR, wantPR)
		}
		if ws.Worktree.PRCheckedAt == 0 {
			t.Errorf("%s: PRCheckedAt should be set", wsName)
		}
	}

	// Each workspace should have been queried exactly once.
	if got := len(mockGit.Calls("PRForBranch")); got != n {
		t.Errorf("PRForBranch call count = %d, want %d", got, n)
	}
}

// --- syncWorkspaceMergeStatus ---

func TestSyncAll_MarksMergedWorkspaceDone(t *testing.T) {
	eng, _ := testEngine(t)
	wsPath := seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/login")
	// Make sure the workspace is active (not idle).
	_ = eng.withManifest(func(m *manifest.Manifest) error {
		ws := m.FindDock("labs").FindWorkspace("w1")
		ws.Status = manifest.WorkspaceStatusActive
		return nil
	})

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetDefaultBranch(wsPath, "main")
	mockGit.SetMerged(wsPath, "feature/login", true)

	eng.SyncAll()

	m, _ := eng.LoadManifest()
	ws := m.FindDock("labs").FindWorkspace("w1")
	if ws.Status != manifest.WorkspaceStatusDone {
		t.Errorf("Status = %q, want done", ws.Status)
	}
}

func TestSyncAll_DoesNotChangeStatusForUnmergedBranch(t *testing.T) {
	eng, _ := testEngine(t)
	wsPath := seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/login")
	_ = eng.withManifest(func(m *manifest.Manifest) error {
		ws := m.FindDock("labs").FindWorkspace("w1")
		ws.Status = manifest.WorkspaceStatusActive
		return nil
	})

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetDefaultBranch(wsPath, "main")
	// Not merged — mock default is false.

	eng.SyncAll()

	m, _ := eng.LoadManifest()
	ws := m.FindDock("labs").FindWorkspace("w1")
	if ws.Status != manifest.WorkspaceStatusActive {
		t.Errorf("Status = %q, want active", ws.Status)
	}
}

func TestSyncAll_SkipsMergedCheckForAlreadyDone(t *testing.T) {
	eng, _ := testEngine(t)
	wsPath := seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/login")
	_ = eng.withManifest(func(m *manifest.Manifest) error {
		ws := m.FindDock("labs").FindWorkspace("w1")
		ws.Status = manifest.WorkspaceStatusDone
		return nil
	})

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetDefaultBranch(wsPath, "main")
	before := len(mockGit.Calls("IsMergedIntoDefault"))

	eng.SyncAll()

	if after := len(mockGit.Calls("IsMergedIntoDefault")); after != before {
		t.Errorf("SyncAll should skip merge check for done workspace: before=%d, after=%d", before, after)
	}
}

func TestSyncAll_SkipsWorkspaceWithoutBranch(t *testing.T) {
	eng, _ := testEngine(t)
	wsPath := filepath.Join(t.TempDir(), "w1")
	os.MkdirAll(wsPath, 0o755)
	_ = eng.withManifest(func(m *manifest.Manifest) error {
		m.FindDock("labs").Workspaces = append(m.FindDock("labs").Workspaces, manifest.Workspace{
			Name:     "w1",
			Type:     manifest.WorkspaceTypeWorktree,
			Path:     wsPath,
			Worktree: &manifest.WorktreeAttrs{Repo: "labs"}, // no Branch
		})
		return nil
	})

	mockGit := eng.Git.(*git.Mock)
	before := len(mockGit.Calls("PRForBranch"))

	eng.SyncAll()

	if after := len(mockGit.Calls("PRForBranch")); after != before {
		t.Errorf("SyncAll should not query PR for branchless workspace: before=%d, after=%d", before, after)
	}
}
