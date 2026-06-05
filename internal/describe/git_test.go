package describe

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// TestGatherGitSignal_DetachedHeadHasNoBranch exercises the real git path:
// a detached HEAD (the normal state after `bay tidy` and for fresh
// worktrees) must not surface the literal "HEAD" as a branch name.
func TestGatherGitSignal_DetachedHeadHasNoBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// Isolate from the developer's global/system git config (e.g.
		// commit.gpgsign=true would make the commit below fail without a
		// signing key) and any leaked config-path env.
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	git("commit", "--allow-empty", "-q", "-m", "init")
	git("checkout", "-q", "--detach")

	if sig := gatherGitSignal(context.Background(), dir); sig.Branch != "" {
		t.Errorf("detached HEAD should yield empty Branch, got %q", sig.Branch)
	}
}
