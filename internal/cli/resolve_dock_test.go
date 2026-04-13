package cli

import (
	"os"
	"testing"
)

// TestResolveCurrentDock_TmuxSession verifies the primary path: if
// the user is in a tmux pane whose session matches a bay dock, that
// dock is returned.
func TestResolveCurrentDock_TmuxSession(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)
	mockTmux.SetCurrentSession("labs")

	// Simulate being inside a tmux pane.
	t.Setenv("TMUX_PANE", "%0")

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

// TestResolveCurrentDock_IgnoresTmuxOutsidePane verifies that
// CurrentSession is NOT used when TMUX_PANE is unset (e.g. a shell
// that inherited $TMUX but is not inside a tmux pane).
func TestResolveCurrentDock_IgnoresTmuxOutsidePane(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)
	mockTmux.SetCurrentSession("labs")

	// TMUX_PANE is NOT set — simulates being outside tmux.
	t.Setenv("TMUX_PANE", "")

	_, _, err := resolveCurrentDock(eng)
	if err == nil {
		t.Error("expected error: should not resolve via tmux session when TMUX_PANE is unset")
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
