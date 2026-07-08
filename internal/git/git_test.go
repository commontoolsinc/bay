package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Verify Mock implements Interface at compile time.
var _ Interface = (*Mock)(nil)

func TestMock_DefaultBranch(t *testing.T) {
	m := NewMock()
	m.SetDefaultBranch("/repo", "main")

	branch, err := m.DefaultBranch("/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "main" {
		t.Errorf("expected main, got %q", branch)
	}

	// Unknown repo falls back to "main".
	branch, err = m.DefaultBranch("/other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "main" {
		t.Errorf("expected default main, got %q", branch)
	}
}

func TestMock_CreateAndRemoveWorktree(t *testing.T) {
	m := NewMock()
	m.SetDefaultBranch("/repo", "main")

	// Create a worktree.
	err := m.CreateWorktree("/repo", "/repo-wt/task1", "")
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	// Verify worktree was recorded.
	if !m.HasWorktree("/repo", "/repo-wt/task1") {
		t.Error("expected worktree to be recorded")
	}

	// Verify call was tracked.
	calls := m.Calls("CreateWorktree")
	if len(calls) != 1 {
		t.Fatalf("expected 1 CreateWorktree call, got %d", len(calls))
	}
	if calls[0].Args[0] != "/repo" || calls[0].Args[1] != "/repo-wt/task1" {
		t.Errorf("unexpected call args: %v", calls[0].Args)
	}

	// Remove the worktree.
	err = m.RemoveWorktree("/repo", "/repo-wt/task1", false)
	if err != nil {
		t.Fatalf("RemoveWorktree failed: %v", err)
	}
	if m.HasWorktree("/repo", "/repo-wt/task1") {
		t.Error("expected worktree to be removed")
	}

	// Removing non-existent worktree without force is an error.
	err = m.RemoveWorktree("/repo", "/repo-wt/nonexistent", false)
	if err == nil {
		t.Error("expected error removing non-existent worktree")
	}
}

func TestMock_RemoveWorktree_Force(t *testing.T) {
	m := NewMock()

	// Force-removing a non-existent worktree should not error.
	err := m.RemoveWorktree("/repo", "/repo-wt/nonexistent", true)
	if err != nil {
		t.Fatalf("force remove should not error: %v", err)
	}
}

func TestMock_IsDirty(t *testing.T) {
	m := NewMock()
	m.SetDirty("/repo", true)

	dirty, err := m.IsDirty("/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dirty {
		t.Error("expected dirty=true")
	}

	// Default is not dirty.
	dirty, err = m.IsDirty("/clean-repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dirty {
		t.Error("expected dirty=false for unknown repo")
	}
}

func TestMock_HasUnpushedCommits(t *testing.T) {
	m := NewMock()
	m.SetUnpushed("/repo", true)

	unpushed, err := m.HasUnpushedCommits("/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !unpushed {
		t.Error("expected unpushed=true")
	}

	// Default is no unpushed commits.
	unpushed, err = m.HasUnpushedCommits("/other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unpushed {
		t.Error("expected unpushed=false for unknown repo")
	}
}

func TestMock_LocalHeadInMergedPR(t *testing.T) {
	m := NewMock()
	m.SetLocalHeadInMergedPR("/repo", "123", true)

	landed, err := m.LocalHeadInMergedPR("/repo", "123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !landed {
		t.Error("expected landed=true")
	}

	landed, err = m.LocalHeadInMergedPR("/repo", "124")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if landed {
		t.Error("expected landed=false for unknown PR")
	}
}

func TestReal_HasUnpushedCommits_PushedBranchIsSafe(t *testing.T) {
	repo := newRealGitRepo(t)
	runGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")
	runGit(t, repo, "push", "-u", "origin", "feature")

	unpushed, err := NewReal().HasUnpushedCommits(repo)
	if err != nil {
		t.Fatalf("HasUnpushedCommits failed: %v", err)
	}
	if unpushed {
		t.Fatal("pushed branch should be safe")
	}
}

func TestReal_HasUnpushedCommits_PushedBranchWithoutUpstreamIsSafe(t *testing.T) {
	repo := newRealGitRepo(t)
	runGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")
	runGit(t, repo, "push", "origin", "feature")

	unpushed, err := NewReal().HasUnpushedCommits(repo)
	if err != nil {
		t.Fatalf("HasUnpushedCommits failed: %v", err)
	}
	if unpushed {
		t.Fatal("branch pushed without upstream should be safe")
	}
}

func TestReal_HasUnpushedCommits_LocalOnlyPatchIsUnsafe(t *testing.T) {
	repo := newRealGitRepo(t)
	runGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")

	unpushed, err := NewReal().HasUnpushedCommits(repo)
	if err != nil {
		t.Fatalf("HasUnpushedCommits failed: %v", err)
	}
	if !unpushed {
		t.Fatal("local-only patch should be unsafe")
	}
}

func TestReal_HasUnpushedCommits_SquashMergedPatchIsSafe(t *testing.T) {
	repo := newRealGitRepo(t)
	runGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")
	runGit(t, repo, "push", "-u", "origin", "feature")
	runGit(t, repo, "push", "origin", "--delete", "feature")

	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "cherry-pick", "--no-commit", "feature")
	runGit(t, repo, "commit", "-m", "squash feature")
	runGit(t, repo, "push", "origin", "main")
	runGit(t, repo, "checkout", "feature")

	unpushed, err := NewReal().HasUnpushedCommits(repo)
	if err != nil {
		t.Fatalf("HasUnpushedCommits failed: %v", err)
	}
	if unpushed {
		t.Fatal("squash-merged patch should be safe")
	}
}

func TestReal_HasUnpushedCommits_UnpushedMergeCommitIsUnsafe(t *testing.T) {
	repo := newRealGitRepo(t)
	runGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")
	runGit(t, repo, "push", "-u", "origin", "feature")
	runGit(t, repo, "push", "origin", "--delete", "feature")

	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "cherry-pick", "--no-commit", "feature")
	runGit(t, repo, "commit", "-m", "squash feature")
	runGit(t, repo, "push", "origin", "main")

	runGit(t, repo, "checkout", "feature")
	runGit(t, repo, "merge", "--no-ff", "--no-commit", "main")
	writeRepoFile(t, repo, "feature.txt", "feature\nmanual merge edit\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "manual merge")

	unpushed, err := NewReal().HasUnpushedCommits(repo)
	if err != nil {
		t.Fatalf("HasUnpushedCommits failed: %v", err)
	}
	if !unpushed {
		t.Fatal("unpushed merge commit should be unsafe")
	}
}

func TestReal_WorktreeMatchesRecoverableRef_AllowsLocalReviewRefTree(t *testing.T) {
	repo := newRealGitRepo(t)

	runGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")
	runGit(t, repo, "update-ref", "refs/bay/review-heads/github/123", "feature")

	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "cherry-pick", "--no-commit", "feature")
	runGit(t, repo, "branch", "-D", "feature")

	matches, ref, err := NewReal().WorktreeMatchesRecoverableRef(repo)
	if err != nil {
		t.Fatalf("WorktreeMatchesRecoverableRef failed: %v", err)
	}
	if !matches {
		t.Fatal("cherry-picked review tree should match local review ref")
	}
	if ref != "refs/bay/review-heads/github/123" {
		t.Fatalf("matched ref = %q, want local review ref", ref)
	}
}

func TestReal_WorktreeMatchesRecoverableRef_BlocksExtraUntrackedFile(t *testing.T) {
	repo := newRealGitRepo(t)

	runGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")
	runGit(t, repo, "update-ref", "refs/bay/review-heads/github/123", "feature")

	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "cherry-pick", "--no-commit", "feature")
	runGit(t, repo, "branch", "-D", "feature")
	writeRepoFile(t, repo, "notes.txt", "local notes\n")

	matches, _, err := NewReal().WorktreeMatchesRecoverableRef(repo)
	if err != nil {
		t.Fatalf("WorktreeMatchesRecoverableRef failed: %v", err)
	}
	if matches {
		t.Fatal("extra untracked file should prevent recoverable-ref match")
	}
}

func TestReal_WorktreeMatchesRecoverableRef_BlocksStagedOnlyUserEdit(t *testing.T) {
	repo := newRealGitRepo(t)

	runGit(t, repo, "checkout", "-b", "feature")
	writeRepoFile(t, repo, "feature.txt", "feature\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")
	runGit(t, repo, "update-ref", "refs/bay/review-heads/github/123", "feature")

	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "cherry-pick", "--no-commit", "feature")
	runGit(t, repo, "branch", "-D", "feature")
	writeRepoFile(t, repo, "README.md", "staged local edit\n")
	runGit(t, repo, "add", "README.md")
	writeRepoFile(t, repo, "README.md", "initial\n")

	matches, _, err := NewReal().WorktreeMatchesRecoverableRef(repo)
	if err != nil {
		t.Fatalf("WorktreeMatchesRecoverableRef failed: %v", err)
	}
	if matches {
		t.Fatal("staged-only user edit should prevent recoverable-ref match")
	}
}

func TestReal_DiscardWorktreeChanges_RemovesTrackedAndUntrackedChanges(t *testing.T) {
	repo := newRealGitRepo(t)

	writeRepoFile(t, repo, "README.md", "modified\n")
	writeRepoFile(t, repo, "notes.txt", "local notes\n")

	if err := NewReal().DiscardWorktreeChanges(repo); err != nil {
		t.Fatalf("DiscardWorktreeChanges: %v", err)
	}

	dirty, err := NewReal().IsDirty(repo)
	if err != nil {
		t.Fatalf("IsDirty: %v", err)
	}
	if dirty {
		t.Fatal("worktree should be clean after discard")
	}
	if _, err := os.Stat(filepath.Join(repo, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("notes.txt should be removed, stat err=%v", err)
	}
}

func TestMock_CurrentBranch(t *testing.T) {
	m := NewMock()
	m.SetBranch("/repo", "feature-x")

	branch, err := m.CurrentBranch("/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "feature-x" {
		t.Errorf("expected feature-x, got %q", branch)
	}

	// Default is empty (detached HEAD).
	branch, err = m.CurrentBranch("/other")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "" {
		t.Errorf("expected empty string for detached, got %q", branch)
	}
}

func TestMock_IsIgnored(t *testing.T) {
	m := NewMock()
	m.AddIgnored("/repo", ".env")

	ignored, err := m.IsIgnored("/repo", ".env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ignored {
		t.Error("expected .env to be ignored")
	}

	ignored, err = m.IsIgnored("/repo", "main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ignored {
		t.Error("expected main.go to not be ignored")
	}
}

func newRealGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")

	runGit(t, "", "init", "--bare", remote)
	runGit(t, "", "clone", remote, repo)
	runGit(t, repo, "config", "user.email", "bay@example.test")
	runGit(t, repo, "config", "user.name", "Bay Tests")
	runGit(t, repo, "checkout", "-b", "main")
	writeRepoFile(t, repo, "README.md", "initial\n")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "initial")
	runGit(t, repo, "push", "-u", "origin", "main")
	return repo
}

func writeRepoFile(t *testing.T, repo, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
}

func TestMock_CallTracking(t *testing.T) {
	m := NewMock()
	m.SetDirty("/repo", false)

	_, _ = m.IsDirty("/repo")
	_, _ = m.IsDirty("/repo")
	_, _ = m.CurrentBranch("/repo")

	if got := len(m.Calls("IsDirty")); got != 2 {
		t.Errorf("expected 2 IsDirty calls, got %d", got)
	}
	if got := len(m.Calls("CurrentBranch")); got != 1 {
		t.Errorf("expected 1 CurrentBranch call, got %d", got)
	}
	if got := len(m.Calls("CreateWorktree")); got != 0 {
		t.Errorf("expected 0 CreateWorktree calls, got %d", got)
	}
}

func TestMock_CreateBranch(t *testing.T) {
	m := NewMock()

	err := m.CreateBranch("/repo", "feature/new-thing")
	if err != nil {
		t.Fatalf("CreateBranch failed: %v", err)
	}

	// Verify the call was recorded
	calls := m.Calls("CreateBranch")
	if len(calls) != 1 {
		t.Fatalf("expected 1 CreateBranch call, got %d", len(calls))
	}
	if calls[0].Args[0] != "/repo" || calls[0].Args[1] != "feature/new-thing" {
		t.Errorf("unexpected call args: %v", calls[0].Args)
	}

	// Verify the mock updated the branch state
	branch, err := m.CurrentBranch("/repo")
	if err != nil {
		t.Fatalf("CurrentBranch failed: %v", err)
	}
	if branch != "feature/new-thing" {
		t.Errorf("branch = %q, want feature/new-thing", branch)
	}
}

func TestMock_RepoRoot(t *testing.T) {
	m := NewMock()
	m.SetRepoRoot("/projects/myrepo/src", "/projects/myrepo")

	root, err := m.RepoRoot("/projects/myrepo/src")
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	if root != "/projects/myrepo" {
		t.Errorf("root = %q, want /projects/myrepo", root)
	}

	// Unknown path returns error.
	_, err = m.RepoRoot("/unknown")
	if err == nil {
		t.Error("expected error for unknown path")
	}
}

func TestMock_IsMergedIntoDefault(t *testing.T) {
	m := NewMock()
	m.SetMerged("/repo", "feature/done", true)

	merged, err := m.IsMergedIntoDefault("/repo", "feature/done")
	if err != nil {
		t.Fatalf("IsMergedIntoDefault: %v", err)
	}
	if !merged {
		t.Error("expected merged=true")
	}

	// Unset branch defaults to not merged.
	merged, err = m.IsMergedIntoDefault("/repo", "feature/open")
	if err != nil {
		t.Fatalf("IsMergedIntoDefault: %v", err)
	}
	if merged {
		t.Error("expected merged=false for unknown branch")
	}
}

func TestMock_Fetch(t *testing.T) {
	m := NewMock()
	if err := m.Fetch("/repo"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	calls := m.Calls("Fetch")
	if len(calls) != 1 {
		t.Errorf("expected 1 Fetch call, got %d", len(calls))
	}
}

func TestMock_CreateWorktree_DuplicateError(t *testing.T) {
	m := NewMock()
	m.SetDefaultBranch("/repo", "main")

	err := m.CreateWorktree("/repo", "/repo-wt/task1", "")
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	err = m.CreateWorktree("/repo", "/repo-wt/task1", "")
	if err == nil {
		t.Error("expected error creating duplicate worktree")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' in error, got: %v", err)
	}
}
