package engine

import (
	"os"
	"path/filepath"
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
	if !ws.Worktree.PRChecked {
		t.Error("PRChecked should be true after successful lookup")
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
	if !ws.Worktree.PRChecked {
		t.Error("PRChecked should be true after definitive 'no PR' answer")
	}
}

func TestSyncAll_DoesNotRecheckPRCheckedWorkspace(t *testing.T) {
	eng, _ := testEngine(t)
	_ = seedWorktreeWorkspace(t, eng, "labs", "w1", "feature/no-pr")

	// First sync — marks PRChecked=true with empty PR.
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
