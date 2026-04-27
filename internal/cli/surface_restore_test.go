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

// TestRunSurfaceRestore_SuccessEmitsNoToast regression-tests the opposite
// of the "empty queue needs feedback" test: on successful restore we
// must NOT emit a tmux display-message. The restored pane appearing is
// the user-visible feedback, and a status-line message here correlates
// with a visible stall in the new pane's content rendering.
func TestRunSurfaceRestore_SuccessEmitsNoToast(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)
	mockTmux.SetCurrentSession("labs")
	t.Setenv("TMUX", "/tmp/tmux-501/default,12345,0")

	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs"}); err != nil {
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

	if len(mockTmux.DisplayMessages()) != before {
		t.Errorf("successful restore must not emit a tmux display-message; new messages: %v",
			mockTmux.DisplayMessages()[before:])
	}
}
