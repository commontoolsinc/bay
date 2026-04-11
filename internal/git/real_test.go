package git

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRun is a test helper that runs a git command and fails the test
// if it errors.
func gitRun(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", args[0], out, err)
	}
}

// setupRemoteAndClone creates a bare remote + clone with one initial
// commit on main. Returns the clone path.
func setupRemoteAndClone(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	clone := filepath.Join(dir, "clone")

	gitRun(t, "init", "--bare", remote)
	gitRun(t, "clone", remote, clone)
	// CI runners may not have a git identity configured.
	gitRun(t, "-C", clone, "config", "user.email", "test@test.com")
	gitRun(t, "-C", clone, "config", "user.name", "Test")
	gitRun(t, "-C", clone, "commit", "--allow-empty", "-m", "init")
	gitRun(t, "-C", clone, "push")

	return clone
}

// TestReal_IsMergedIntoDefault_FreshBranchNotMerged verifies that a
// brand-new branch with no commits above the default branch is NOT
// detected as merged. This is the false-positive that caused the
// tutorial's workspaces to immediately show status=done.
func TestReal_IsMergedIntoDefault_FreshBranchNotMerged(t *testing.T) {
	clone := setupRemoteAndClone(t)

	gitRun(t, "-C", clone, "checkout", "-b", "fix/fresh")

	r := Real{}
	merged, err := r.IsMergedIntoDefault(clone, "fix/fresh")
	if err != nil {
		t.Fatalf("IsMergedIntoDefault: %v", err)
	}
	if merged {
		t.Error("fresh branch with no diverging commits should NOT be detected as merged")
	}
}

// TestReal_IsMergedIntoDefault_MergedBranchDetected verifies that a
// branch with actual commits that's been merged into the default
// branch IS detected as merged.
func TestReal_IsMergedIntoDefault_MergedBranchDetected(t *testing.T) {
	clone := setupRemoteAndClone(t)

	// Detect the default branch name (main vs master) — varies by
	// git version and config.
	r := Real{}
	defaultBranch, err := r.DefaultBranch(clone)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}

	gitRun(t, "-C", clone, "checkout", "-b", "fix/done")
	gitRun(t, "-C", clone, "commit", "--allow-empty", "-m", "work on fix")
	gitRun(t, "-C", clone, "push", "-u", "origin", "fix/done")
	gitRun(t, "-C", clone, "checkout", defaultBranch)
	gitRun(t, "-C", clone, "merge", "fix/done")
	gitRun(t, "-C", clone, "push")

	merged, err := r.IsMergedIntoDefault(clone, "fix/done")
	if err != nil {
		t.Fatalf("IsMergedIntoDefault: %v", err)
	}
	if !merged {
		t.Error("branch with commits that was merged into default should be detected as merged")
	}
}

// TestReal_IsMergedIntoDefault_UnmergedBranchNotDetected verifies
// that a branch with commits that hasn't been merged is NOT detected.
func TestReal_IsMergedIntoDefault_UnmergedBranchNotDetected(t *testing.T) {
	clone := setupRemoteAndClone(t)

	gitRun(t, "-C", clone, "checkout", "-b", "fix/wip")
	gitRun(t, "-C", clone, "commit", "--allow-empty", "-m", "wip commit")

	r := Real{}
	merged, err := r.IsMergedIntoDefault(clone, "fix/wip")
	if err != nil {
		t.Fatalf("IsMergedIntoDefault: %v", err)
	}
	if merged {
		t.Error("unmerged branch with commits should NOT be detected as merged")
	}
}
