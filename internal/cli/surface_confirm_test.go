package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// noFlash is a flashFunc that drops every message — used by tests that
// don't care about message text.
func noFlash(string, int) error { return nil }

// --- file-format helpers ---

func TestCloseConfirm_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close-confirm")
	now := time.Now()
	if err := writeCloseConfirm(path, "labs", "w1", now); err != nil {
		t.Fatalf("write: %v", err)
	}
	d, w, when, ok := readCloseConfirm(path)
	if !ok {
		t.Fatalf("read: !ok")
	}
	if d != "labs" || w != "w1" {
		t.Errorf("got dock=%q ws=%q, want labs/w1", d, w)
	}
	if !when.Equal(now) {
		t.Errorf("timestamp mismatch: got %v, want %v", when, now)
	}
}

func TestRecordedRecently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close-confirm")
	now := time.Now()
	_ = writeCloseConfirm(path, "labs", "w1", now)

	cases := []struct {
		name string
		dock string
		ws   string
		when time.Time
		want bool
	}{
		{"same target, within window", "labs", "w1", now.Add(closeConfirmWindow / 2), true},
		{"same target, at edge", "labs", "w1", now.Add(closeConfirmWindow), true},
		{"same target, expired", "labs", "w1", now.Add(closeConfirmWindow + time.Millisecond), false},
		{"different ws", "labs", "w2", now, false},
		{"different dock", "other", "w1", now, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := recordedRecently(path, c.dock, c.ws, c.when); got != c.want {
				t.Errorf("recordedRecently = %v, want %v", got, c.want)
			}
		})
	}
}

func TestRecordedRecently_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist")
	if recordedRecently(path, "labs", "w1", time.Now()) {
		t.Error("recordedRecently should be false for missing file")
	}
}

// --- runSurfaceClose wiring ---

// withConfirmStubs replaces both confirmation hooks with deterministic stubs
// for the duration of a test. lastSurfaceCalls and agentCalls receive every
// invocation so tests can assert call counts and arguments.
func withConfirmStubs(t *testing.T, lastSurface, agent func(name string) bool) (lastCalls, agentCalls *[]string) {
	t.Helper()
	lc := []string{}
	ac := []string{}

	origLast := confirmLastSurfaceClose
	origAgent := confirmAgentClose
	origShould := shouldConfirmLastSurfaceClose
	t.Cleanup(func() {
		confirmLastSurfaceClose = origLast
		confirmAgentClose = origAgent
		shouldConfirmLastSurfaceClose = origShould
	})

	shouldConfirmLastSurfaceClose = func() bool { return true }
	confirmLastSurfaceClose = func(_ flashFunc, dock, ws string) bool {
		lc = append(lc, dock+":"+ws)
		if lastSurface == nil {
			return true
		}
		return lastSurface(dock + ":" + ws)
	}
	confirmAgentClose = func(name string) bool {
		ac = append(ac, name)
		if agent == nil {
			return true
		}
		return agent(name)
	}
	return &lc, &ac
}

func TestRunSurfaceClose_LastSurface_NonInteractiveFiresDoubleTap(t *testing.T) {
	last, agent := withConfirmStubs(t, func(string) bool { return false }, nil)

	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	// Sole surface in the workspace — closing it should trigger the
	// last-surface confirmation, which our stub declines.
	if err := runSurfaceClose(eng, []string{"w1:shell"}, "", "", false); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	if len(*last) != 1 || (*last)[0] != "labs:w1" {
		t.Errorf("expected one last-surface confirm for labs:w1, got %v", *last)
	}
	if len(*agent) != 0 {
		t.Errorf("agent prompt should not fire for shell surface, got %v", *agent)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 1 {
		t.Errorf("declined confirmation should leave surface intact, got %d surfaces", len(ws.Surfaces))
	}
}

func TestRunSurfaceClose_LastSurface_DirtyShowsRefusalInsteadOfDoubleTap(t *testing.T) {
	last, agent := withConfirmStubs(t,
		func(string) bool {
			t.Fatalf("last-surface confirm should not fire when workspace is dirty")
			return false
		},
		nil,
	)

	eng, mockTmux, mockGit, _ := testNavEngine(t)
	ws, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mockGit.SetDirty(ws.Path, true)

	if err := runSurfaceClose(eng, []string{"w1:shell"}, "", "", false); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	if len(*last) != 0 {
		t.Errorf("last-surface confirm should not fire, got %v", *last)
	}
	if len(*agent) != 0 {
		t.Errorf("agent prompt should not fire for shell surface, got %v", *agent)
	}
	if msgs := mockTmux.DisplayMessages(); len(msgs) != 1 || !strings.Contains(msgs[0], "bay kept (uncommitted changes)") {
		t.Fatalf("expected dirty refusal toast, got %v", msgs)
	}

	got, _ := eng.WsShow("labs", "w1")
	if len(got.Surfaces) != 1 {
		t.Errorf("dirty refusal should leave surface intact, got %d surfaces", len(got.Surfaces))
	}
}

func TestRunSurfaceClose_LastSurface_UnpushedShowsRefusalInsteadOfDoubleTap(t *testing.T) {
	last, agent := withConfirmStubs(t,
		func(string) bool {
			t.Fatalf("last-surface confirm should not fire when workspace has unlanded commits")
			return false
		},
		nil,
	)

	eng, mockTmux, mockGit, _ := testNavEngine(t)
	ws, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mockGit.SetUnpushed(ws.Path, true)

	if err := runSurfaceClose(eng, []string{"w1:shell"}, "", "", false); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	if len(*last) != 0 {
		t.Errorf("last-surface confirm should not fire, got %v", *last)
	}
	if len(*agent) != 0 {
		t.Errorf("agent prompt should not fire for shell surface, got %v", *agent)
	}
	if msgs := mockTmux.DisplayMessages(); len(msgs) != 1 || !strings.Contains(msgs[0], "bay kept (unlanded commits)") {
		t.Fatalf("expected unlanded refusal toast, got %v", msgs)
	}

	got, _ := eng.WsShow("labs", "w1")
	if len(got.Surfaces) != 1 {
		t.Errorf("unlanded refusal should leave surface intact, got %d surfaces", len(got.Surfaces))
	}
}

func TestRunSurfaceClose_LastSurface_NonInteractiveAcceptedCloses(t *testing.T) {
	withConfirmStubs(t, func(string) bool { return true }, nil)

	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	if err := runSurfaceClose(eng, []string{"w1:shell"}, "", "", false); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 0 {
		t.Errorf("accepted confirmation should close surface, got %d surfaces", len(ws.Surfaces))
	}
}

func TestRunSurfaceClose_LastSurface_InteractiveSkipsDoubleTap(t *testing.T) {
	last, agent := withConfirmStubs(t,
		func(string) bool {
			t.Fatalf("last-surface confirm should not fire for interactive CLI close")
			return false
		},
		nil,
	)
	shouldConfirmLastSurfaceClose = func() bool { return false }

	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	if err := runSurfaceClose(eng, []string{"w1:shell"}, "", "", false); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	if len(*last) != 0 {
		t.Errorf("last-surface confirm should not fire for interactive close, got %v", *last)
	}
	if len(*agent) != 0 {
		t.Errorf("agent prompt should not fire for shell surface, got %v", *agent)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 0 {
		t.Errorf("interactive last-surface close should close surface, got %d surfaces", len(ws.Surfaces))
	}
}

func TestRunSurfaceClose_NotLastSurface_SkipsDoubleTap(t *testing.T) {
	last, _ := withConfirmStubs(t, nil, nil)

	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "extra", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	// Two surfaces present — closing one shouldn't trigger last-surface.
	if err := runSurfaceClose(eng, []string{"w1:extra"}, "", "", false); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	if len(*last) != 0 {
		t.Errorf("last-surface confirm should not fire when other surfaces remain, got %v", *last)
	}
}

func TestRunSurfaceClose_LastSurface_ForceBypassesBoth(t *testing.T) {
	last, agent := withConfirmStubs(t,
		func(string) bool {
			t.Fatalf("last-surface confirm should not fire with --force")
			return false
		},
		func(string) bool {
			t.Fatalf("agent confirm should not fire with --force")
			return false
		},
	)

	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	if err := runSurfaceClose(eng, []string{"w1:shell"}, "", "", true); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}
	if len(*last) != 0 || len(*agent) != 0 {
		t.Errorf("--force should skip both confirms, got last=%v agent=%v", *last, *agent)
	}
}

func TestRunSurfaceClose_LastSurfaceAgent_NonInteractiveOnlyDoubleTap(t *testing.T) {
	// When the last surface is an agent, the double-tap supersedes the
	// agent y/N prompt — one confirmation, not two.
	last, agent := withConfirmStubs(t, func(string) bool { return true }, nil)

	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	// Add an agent surface alongside the default shell, then drop the
	// shell so the agent is the sole remaining surface. Closing the shell
	// while the agent exists isn't "last surface," so it goes through
	// without firing either prompt.
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "claude", Agent: "claude", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd agent: %v", err)
	}
	if err := eng.SurfaceClose("labs", "w1", "shell", true); err != nil {
		t.Fatalf("seed close shell: %v", err)
	}

	if err := runSurfaceClose(eng, []string{"w1:claude"}, "", "", false); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	if len(*last) != 1 {
		t.Errorf("expected one last-surface confirm, got %v", *last)
	}
	if len(*agent) != 0 {
		t.Errorf("agent confirm should be suppressed when last-surface fires, got %v", *agent)
	}
}

func TestRunSurfaceClose_LastSurfaceAgent_InteractivePromptsAgent(t *testing.T) {
	last, agent := withConfirmStubs(t,
		func(string) bool {
			t.Fatalf("last-surface confirm should not fire for interactive CLI close")
			return false
		},
		func(string) bool { return true },
	)
	shouldConfirmLastSurfaceClose = func() bool { return false }

	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "claude", Agent: "claude", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd agent: %v", err)
	}
	if err := eng.SurfaceClose("labs", "w1", "shell", true); err != nil {
		t.Fatalf("seed close shell: %v", err)
	}

	if err := runSurfaceClose(eng, []string{"w1:claude"}, "", "", false); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	if len(*last) != 0 {
		t.Errorf("last-surface confirm should not fire for interactive close, got %v", *last)
	}
	if len(*agent) != 1 || (*agent)[0] != "claude" {
		t.Errorf("interactive last-agent close should prompt once, got %v", *agent)
	}
}

func TestConfirmLastSurfaceClose_DefaultBehavior(t *testing.T) {
	// End-to-end of the default impl: first call returns false and writes
	// state + flashes a message; second call within the window returns
	// true and clears the file. Uses a temp DataDir via XDG_DATA_HOME so
	// we don't touch the user's real bay data.
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	var msgs []string
	flash := func(msg string, _ int) error {
		msgs = append(msgs, msg)
		return nil
	}

	if confirmLastSurfaceClose(flash, "labs", "w1") {
		t.Fatal("first call should require a second tap")
	}
	if len(msgs) != 1 {
		t.Errorf("first call should flash a message, got %d", len(msgs))
	}

	if !confirmLastSurfaceClose(flash, "labs", "w1") {
		t.Fatal("second call within window should accept")
	}

	// Third call after a successful confirm starts fresh — file was cleared.
	if confirmLastSurfaceClose(flash, "labs", "w1") {
		t.Error("third call (after consume) should require a fresh tap")
	}
}

func TestConfirmLastSurfaceClose_DifferentWorkspace_DoesNotCarry(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if confirmLastSurfaceClose(noFlash, "labs", "w1") {
		t.Fatal("first call on w1 should require second tap")
	}
	// Pressing again on a DIFFERENT workspace should not consume w1's
	// pending confirmation — each workspace stands alone.
	if confirmLastSurfaceClose(noFlash, "labs", "w2") {
		t.Error("close on w2 should not be accepted by w1's pending tap")
	}
}
