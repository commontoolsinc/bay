package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/commontoolsinc/bay/internal/manifest"
)

// TestResolveCurrentDock_TmuxSession verifies the primary path: if
// the user is in a tmux pane whose session matches a bay dock, that
// dock is returned.
func TestResolveCurrentDock_TmuxSession(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)
	mockTmux.SetCurrentSession("labs")

	// Simulate being inside tmux.
	t.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")

	dock, err := resolveCurrentDock(eng)
	if err != nil {
		t.Fatalf("resolveCurrentDock: %v", err)
	}
	if dock != "labs" {
		t.Errorf("dock = %q, want labs", dock)
	}
}

// TestResolveCurrentDock_IgnoresTmuxOutsidePane verifies that
// CurrentSession is NOT used when $TMUX is unset (e.g. a shell
// that is not inside tmux).
func TestResolveCurrentDock_IgnoresTmuxOutsidePane(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)
	mockTmux.SetCurrentSession("labs")

	// $TMUX is NOT set — simulates being outside tmux.
	t.Setenv("TMUX", "")

	_, err := resolveCurrentDock(eng)
	if err == nil {
		t.Error("expected error: should not resolve via tmux session when TMUX is unset")
	}
}

// TestResolveCurrentDock_CWDFallback verifies the fallback: if the
// user is NOT in a tmux session but their CWD is inside a known
// checkout, the matching dock is returned.
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

	dock, err := resolveCurrentDock(eng)
	if err != nil {
		t.Fatalf("resolveCurrentDock: %v", err)
	}
	if dock != "labs" {
		t.Errorf("dock = %q, want labs", dock)
	}
}

// TestResolveCurrentDock_NeitherErrors verifies that if neither tmux
// nor CWD resolves to a dock, an error is returned.
func TestResolveCurrentDock_NeitherErrors(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	// Not in tmux, CWD is not a known repo.

	_, err := resolveCurrentDock(eng)
	if err == nil {
		t.Error("expected error when neither tmux nor CWD resolves to a dock")
	}
}

// TestResolveCurrentDock_CWDBeatsTmuxSession is the regression test for
// the bug where `bay new` in one dock's checkout created the bay in the
// dock of the current tmux session instead. Standing in a known
// checkout must win over the ambient session.
func TestResolveCurrentDock_CWDBeatsTmuxSession(t *testing.T) {
	eng, mockTmux, mockGit, dir := testNavEngine(t)

	// Add a second dock "otherdock" with its own checkout, alongside
	// the "labs" dock that testNavEngine creates.
	otherRepo := filepath.Join(dir, "repos", "otherdock")
	if err := os.MkdirAll(filepath.Join(otherRepo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	m, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	m.Docks = append(m.Docks, manifest.Dock{
		Name: "otherdock", Path: otherRepo, Agent: "claude", Bays: []manifest.Bay{},
	})
	if err := manifest.Save(manifestPath, m); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	// Attached to the "labs" tmux session...
	mockTmux.SetCurrentSession("labs")
	t.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")

	// ...but standing in "otherdock"'s checkout. Use the actual path
	// from Getwd after chdir (macOS symlinks /tmp → /private/tmp).
	oldWd, _ := os.Getwd()
	if err := os.Chdir(otherRepo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(oldWd)

	actualCwd, _ := os.Getwd()
	mockGit.SetRepoRoot(actualCwd, actualCwd)

	dock, err := resolveCurrentDock(eng)
	if err != nil {
		t.Fatalf("resolveCurrentDock: %v", err)
	}
	if dock != "otherdock" {
		t.Errorf("dock = %q, want otherdock (CWD must beat tmux session)", dock)
	}
}
