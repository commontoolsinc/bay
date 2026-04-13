package cli

import (
	"os"
	"testing"
)

// TestResolveCurrentDock_TmuxSession verifies the primary path: if
// the tmux session is a bay dock AND CWD is inside the dock's repo,
// that dock is returned.
func TestResolveCurrentDock_TmuxSession(t *testing.T) {
	eng, mockTmux, _, dir := testNavEngine(t)
	mockTmux.SetCurrentSession("labs")

	// CWD must be inside the dock's repo for the tmux path to match.
	repoDir := dir + "/repos/labs"
	oldWd, _ := os.Getwd()
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(oldWd)

	dock, repo, err := resolveCurrentDock(eng)
	if err != nil {
		t.Fatalf("resolveCurrentDock: %v", err)
	}
	if dock != "labs" {
		t.Errorf("dock = %q, want labs", dock)
	}
	if repo != "labs" {
		t.Errorf("repo = %q, want labs", repo)
	}
}

// TestResolveCurrentDock_IgnoresTmuxWhenCWDMismatches verifies that
// the tmux session is NOT used when CWD is outside the dock's repo
// (e.g. inherited $TMUX from a different project).
func TestResolveCurrentDock_IgnoresTmuxWhenCWDMismatches(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)
	mockTmux.SetCurrentSession("labs")

	// CWD is NOT inside the dock's repo (default test CWD).

	_, _, err := resolveCurrentDock(eng)
	if err == nil {
		t.Error("expected error: should not resolve via tmux session when CWD is outside dock's repo")
	}
}

// TestResolveCurrentDock_CWDFallback verifies the fallback: if the
// user is NOT in a tmux session but their CWD is inside a known
// repo, the matching dock is returned.
func TestResolveCurrentDock_CWDFallback(t *testing.T) {
	eng, _, mockGit, dir := testNavEngine(t)
	// Not in tmux — no SetCurrentSession call.

	// Simulate CWD inside the repo directory. Use the actual path
	// from Getwd after chdir — on macOS, TempDir paths may differ
	// from what Getwd returns due to symlinks (/tmp → /private/tmp).
	repoDir := dir + "/repos/labs"
	oldWd, _ := os.Getwd()
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(oldWd)

	actualCwd, _ := os.Getwd()
	mockGit.SetRepoRoot(actualCwd, actualCwd)

	dock, repo, err := resolveCurrentDock(eng)
	if err != nil {
		t.Fatalf("resolveCurrentDock: %v", err)
	}
	if dock != "labs" {
		t.Errorf("dock = %q, want labs", dock)
	}
	if repo != "labs" {
		t.Errorf("repo = %q, want labs", repo)
	}
}

// TestResolveCurrentDock_NeitherErrors verifies that if neither tmux
// nor CWD resolves to a dock, an error is returned.
func TestResolveCurrentDock_NeitherErrors(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	// Not in tmux, CWD is not a known repo.

	_, _, err := resolveCurrentDock(eng)
	if err == nil {
		t.Error("expected error when neither tmux nor CWD resolves to a dock")
	}
}
