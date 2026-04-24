package cli

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// TestRunSurfaceRestore_EmptyQueueEmitsTmuxToast regresses the first
// local-testing bug: Option+W → Option+Z on an empty queue printed to
// stderr only, which is invisible from tmux run-shell. The fix routes
// the message through tmux display-message, which stays visible.
func TestRunSurfaceRestore_EmptyQueueEmitsTmuxToast(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)
	mockTmux.SetCurrentSession("labs")
	t.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")

	if err := runSurfaceRestore(eng, false); err != nil {
		t.Fatalf("runSurfaceRestore: %v", err)
	}

	msgs := mockTmux.DisplayMessages()
	if len(msgs) == 0 {
		t.Fatal("expected a tmux display-message on empty queue; got none")
	}
	if !strings.Contains(msgs[len(msgs)-1], "Nothing to restore") {
		t.Errorf("last tmux message = %q; want substring %q", msgs[len(msgs)-1], "Nothing to restore")
	}
}

// TestRunSurfaceRestore_SuccessEmitsTmuxToast confirms Option+Z gives the
// user visual confirmation of what was restored. Without this, a user
// mashing Option+Z on what they think is an empty queue can silently pop
// surprise entries — which happened during manual testing.
func TestRunSurfaceRestore_SuccessEmitsTmuxToast(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)
	mockTmux.SetCurrentSession("labs")
	t.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")

	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "logs", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}
	if err := eng.SurfaceClose("labs", "w1", "logs", false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}

	before := len(mockTmux.DisplayMessages())
	if err := runSurfaceRestore(eng, false); err != nil {
		t.Fatalf("runSurfaceRestore: %v", err)
	}

	msgs := mockTmux.DisplayMessages()
	if len(msgs) <= before {
		t.Fatal("expected a tmux display-message on successful restore; got none")
	}
	last := msgs[len(msgs)-1]
	if !strings.Contains(last, "Restored") || !strings.Contains(last, "logs") {
		t.Errorf("last tmux message = %q; want substrings %q and %q", last, "Restored", "logs")
	}
}
