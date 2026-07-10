package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
// tutorial's bays to immediately show status=done.
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

// TestReal_DefaultBranch_SlashInBranchName verifies that a default
// branch whose name contains slashes (origin/HEAD ->
// origin/team/feature, as some forks configure) is returned intact.
// Regression: the old parser split on "/" and kept only the last
// component, so "team/feature" became "feature".
func TestReal_DefaultBranch_SlashInBranchName(t *testing.T) {
	clone := setupRemoteAndClone(t)

	gitRun(t, "-C", clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/team/feature")

	r := Real{}
	branch, err := r.DefaultBranch(clone)
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if branch != "team/feature" {
		t.Errorf("DefaultBranch = %q, want team/feature (slash must be preserved)", branch)
	}
}

// TestReal_CreateWorktree_SlashDefaultBranch reproduces the `bay new`
// failure in a repo whose default branch name contains a slash: the
// detached worktree bases on origin/<default>, which must resolve to a
// real ref. Before the DefaultBranch fix this tried origin/feature
// (nonexistent) and git worktree add failed with "invalid reference".
func TestReal_CreateWorktree_SlashDefaultBranch(t *testing.T) {
	clone := setupRemoteAndClone(t)

	// Namespaced default branch on the remote (origin/HEAD ->
	// origin/team/feature).
	gitRun(t, "-C", clone, "checkout", "-b", "team/feature")
	gitRun(t, "-C", clone, "push", "-u", "origin", "team/feature")
	gitRun(t, "-C", clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/team/feature")

	wt := filepath.Join(t.TempDir(), "wt")
	r := Real{}
	if err := r.CreateWorktree(clone, wt, ""); err != nil {
		t.Fatalf("CreateWorktree with slash default branch: %v", err)
	}
}

// TestReal_ExpandExcludes verifies that gitignore-syntax patterns in an
// exclude file match both tracked and untracked files, and that the two
// buckets are returned separately.
func TestReal_ExpandExcludes(t *testing.T) {
	clone := setupRemoteAndClone(t)

	// Tracked file: config.toml — matches "*.toml".
	writeFile(t, filepath.Join(clone, "config.toml"), "x")
	gitRun(t, "-C", clone, "add", "config.toml")
	gitRun(t, "-C", clone, "commit", "-m", "add config")

	// Untracked files.
	writeFile(t, filepath.Join(clone, "local.env"), "SECRET=1")      // matches *.env
	writeFile(t, filepath.Join(clone, "keep.md"), "docs")            // no match
	writeFile(t, filepath.Join(clone, "sub/nested.env"), "NESTED=1") // matches *.env recursively

	// Exclude file with gitignore syntax.
	writeFile(t, filepath.Join(clone, ".wti"), "*.env\n*.toml\n")

	r := Real{}
	tracked, untracked, err := r.ExpandExcludes(clone, ".wti")
	if err != nil {
		t.Fatalf("ExpandExcludes: %v", err)
	}

	if !slices.Contains(tracked, "config.toml") {
		t.Errorf("tracked = %v, want to contain config.toml", tracked)
	}
	if !slices.Contains(untracked, "local.env") {
		t.Errorf("untracked = %v, want to contain local.env", untracked)
	}
	if !slices.Contains(untracked, "sub/nested.env") {
		t.Errorf("untracked = %v, want to contain sub/nested.env (recursive)", untracked)
	}
	if slices.Contains(untracked, "keep.md") {
		t.Errorf("untracked = %v, should not contain keep.md (no pattern match)", untracked)
	}
	if slices.Contains(untracked, ".wti") {
		t.Errorf("untracked = %v, should not contain the exclude file itself", untracked)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
