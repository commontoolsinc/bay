package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

func TestSurfaceClose_PushesUndoEntry(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "shell-2", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	before := time.Now().Unix()
	if err := eng.SurfaceClose("labs", "w1", "shell-2", false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}

	entries, err := eng.ListClosedEntries("labs")
	if err != nil {
		t.Fatalf("ListClosedEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 queued entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Kind != manifest.ClosedKindSurface {
		t.Errorf("wrong kind: %q", e.Kind)
	}
	if e.Surface == nil {
		t.Fatal("missing Surface payload")
	}
	if e.Surface.Workspace != "w1" || e.Surface.Name != "shell-2" {
		t.Errorf("wrong payload: %+v", e.Surface)
	}
	if e.Surface.Type != manifest.SurfaceTypeShell {
		t.Errorf("wrong type: %q", e.Surface.Type)
	}
	if e.ClosedAt < before {
		t.Errorf("ClosedAt=%d predates test start %d", e.ClosedAt, before)
	}
}

func TestSurfaceRestore_RoundTripsSurface(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "logs", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}
	if err := eng.SurfaceClose("labs", "w1", "logs", false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if s := ws.FindSurface("logs"); s != nil {
		t.Fatal("surface should be gone before restore")
	}

	entry, err := eng.SurfaceRestore("labs")
	if err != nil {
		t.Fatalf("SurfaceRestore: %v", err)
	}
	if entry == nil || entry.Surface == nil || entry.Surface.Name != "logs" {
		t.Fatalf("SurfaceRestore returned unexpected entry: %+v", entry)
	}

	ws, _ = eng.WsShow("labs", "w1")
	restored := ws.FindSurface("logs")
	if restored == nil {
		t.Fatal("restored surface not found in workspace")
	}
	// A closed split pane must come back as a split pane, not a new
	// tmux window. Regression test for the first local-testing bug: a
	// tiled shell was restored as a full window.
	if restored.Tmux == nil || restored.Tmux.SplitDir != "v" {
		t.Errorf("expected restored surface to preserve SplitDir=v, got %+v", restored.Tmux)
	}

	// Queue should be empty after a successful restore.
	entries, err := eng.ListClosedEntries("labs")
	if err != nil {
		t.Fatalf("ListClosedEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty queue after restore, got %d entries", len(entries))
	}
}

func TestSurfaceRestore_EmptyQueueReturnsNothingToRestore(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	entry, err := eng.SurfaceRestore("labs")
	if !errors.Is(err, ErrNothingToRestore) {
		t.Errorf("expected ErrNothingToRestore, got %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil entry on empty queue, got %+v", entry)
	}
}

func TestSurfaceRestore_DropsStaleEntryWhenWorkspaceGone(t *testing.T) {
	// When the parent workspace has been closed between close and restore
	// (e.g. grace window elapsed under orphan-hygiene), the entry is stale
	// and should be silently dropped.
	defer withZeroGrace()()
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	ws, _ := eng.WsShow("labs", "w1")
	first := ws.Surfaces[0].Name

	if err := eng.SurfaceClose("labs", "w1", first, false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}
	// Zero grace → next sync finalizes the workspace close. Queue entry
	// remains, but workspace is gone.
	eng.SyncAll()

	_, err := eng.SurfaceRestore("labs")
	if !errors.Is(err, ErrNothingToRestore) {
		t.Errorf("expected ErrNothingToRestore for stale entry, got %v", err)
	}

	// Stale entry should have been dropped from the queue.
	entries, _ := eng.ListClosedEntries("labs")
	if len(entries) != 0 {
		t.Errorf("expected stale entry dropped, got %d entries", len(entries))
	}
}

func TestSurfaceRestore_RestoreWithinGraceCancelsPendingClose(t *testing.T) {
	// The core composition with orphan-hygiene: Option+W on the last
	// surface schedules PendingCloseAt; Option+Z within the grace window
	// restores the surface, and SurfaceAdd's cancel path clears the timer.
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	ws, _ := eng.WsShow("labs", "w1")
	first := ws.Surfaces[0].Name

	if err := eng.SurfaceClose("labs", "w1", first, false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}
	ws, _ = eng.WsShow("labs", "w1")
	if ws.PendingCloseAt == 0 {
		t.Fatal("expected PendingCloseAt scheduled after last-surface close")
	}

	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("SurfaceRestore: %v", err)
	}

	ws, _ = eng.WsShow("labs", "w1")
	if ws.PendingCloseAt != 0 {
		t.Errorf("PendingCloseAt not cleared after restore: %d", ws.PendingCloseAt)
	}
	if len(ws.Surfaces) != 1 {
		t.Errorf("expected 1 surface after restore, got %d", len(ws.Surfaces))
	}
}

func TestSurfaceRestore_PreservesQueueOnAddFailure(t *testing.T) {
	// If SurfaceAdd fails during restore, the entry must stay queued so
	// the user can retry after fixing the cause. We simulate a transient
	// failure by injecting a queue entry for an unknown agent — the
	// workspace exists, so the stale-drop path is not taken, but SurfaceAdd
	// fails in validateAgentName.
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	// Inject a bad entry directly — bypasses SurfaceClose and its own
	// validation, simulating the "config regressed between close and
	// restore" scenario that the design promises to be recoverable from.
	err := eng.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock("labs")
		dock.PushClosedEntry(manifest.ClosedEntry{
			ClosedAt: time.Now().Unix(),
			Kind:     manifest.ClosedKindSurface,
			Surface: &manifest.ClosedSurface{
				Workspace: "w1",
				Name:      "a1",
				Type:      manifest.SurfaceTypeAgent,
				Agent:     "definitely-not-a-real-agent",
			},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("inject: %v", err)
	}

	_, err = eng.SurfaceRestore("labs")
	if err == nil {
		t.Fatal("expected restore to fail on unknown agent")
	}
	if errors.Is(err, ErrNothingToRestore) {
		t.Errorf("unexpected ErrNothingToRestore on real failure: %v", err)
	}

	entries, _ := eng.ListClosedEntries("labs")
	if len(entries) != 1 {
		t.Errorf("expected entry preserved on restore failure, got %d entries", len(entries))
	}
}

// TestSurfaceRestore_RootPaneRejoinsWindow verifies the layout-faithful
// restore of a root pane: with siblings surviving in the same layout
// group, the restored pane re-enters the original tmux window via
// `split-window -fb` instead of creating a new window.
func TestSurfaceRestore_RootPaneRejoinsWindow(t *testing.T) {
	eng, _ := testEngine(t)
	mockTmux := eng.Tmux.(*tmux.Mock)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	// WsNew's initial surface is the root (SplitDir=""). Add two split
	// children so the layout group survives after the root is closed.
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "middle", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd middle: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "bottom", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd bottom: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	rootName := ws.Surfaces[0].Name
	rootLayoutGroup := ws.Surfaces[0].Tmux.LayoutGroup

	if err := eng.SurfaceClose("labs", "w1", rootName, false); err != nil {
		t.Fatalf("SurfaceClose root: %v", err)
	}

	before := len(mockTmux.Calls)
	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("SurfaceRestore: %v", err)
	}

	// Assert SplitWindowBefore was called (not NewWindow) during restore.
	sawSplitBefore := false
	for _, c := range mockTmux.Calls[before:] {
		if c.Method == "SplitWindowBefore" {
			sawSplitBefore = true
			break
		}
		if c.Method == "NewWindow" {
			t.Errorf("restore created a new window; expected SplitWindowBefore. Call: %+v", c)
		}
	}
	if !sawSplitBefore {
		t.Error("expected SplitWindowBefore call during root-pane restore")
	}

	ws, _ = eng.WsShow("labs", "w1")
	restored := ws.FindSurface(rootName)
	if restored == nil {
		t.Fatal("root surface not found after restore")
	}
	if restored.Tmux == nil || restored.Tmux.LayoutGroup != rootLayoutGroup {
		t.Errorf("restored root LayoutGroup=%d; want %d", restored.Tmux.LayoutGroup, rootLayoutGroup)
	}
	if restored.Tmux.SplitDir != "" {
		t.Errorf("restored root SplitDir=%q; want \"\"", restored.Tmux.SplitDir)
	}
	if restored.Tmux.SplitFrom != 0 {
		t.Errorf("restored root SplitFrom=%d; want 0", restored.Tmux.SplitFrom)
	}
}

// TestSurfaceRestore_SplitChildRejoinsWindow verifies that a closed split
// pane comes back as a split in its original layout group (not a new
// window) when a sibling survives.
func TestSurfaceRestore_SplitChildRejoinsWindow(t *testing.T) {
	eng, _ := testEngine(t)
	mockTmux := eng.Tmux.(*tmux.Mock)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "split", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	splitLayoutGroup := ws.FindSurface("split").Tmux.LayoutGroup

	if err := eng.SurfaceClose("labs", "w1", "split", false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}

	before := len(mockTmux.Calls)
	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("SurfaceRestore: %v", err)
	}

	// Regular SplitWindow (not SplitWindowBefore, not NewWindow) is the
	// correct call for a split-child restore.
	sawSplit := false
	for _, c := range mockTmux.Calls[before:] {
		if c.Method == "NewWindow" {
			t.Errorf("restore created a new window for a split child; expected SplitWindow. Call: %+v", c)
		}
		if c.Method == "SplitWindow" {
			sawSplit = true
		}
	}
	if !sawSplit {
		t.Error("expected SplitWindow call during split-child restore")
	}

	ws, _ = eng.WsShow("labs", "w1")
	restored := ws.FindSurface("split")
	if restored == nil || restored.Tmux == nil {
		t.Fatal("split surface not found after restore")
	}
	if restored.Tmux.LayoutGroup != splitLayoutGroup {
		t.Errorf("restored split LayoutGroup=%d; want %d", restored.Tmux.LayoutGroup, splitLayoutGroup)
	}
	if restored.Tmux.SplitDir != "v" {
		t.Errorf("restored split SplitDir=%q; want \"v\"", restored.Tmux.SplitDir)
	}
}

// TestSurfaceRestore_LastPaneRestoresAtEnd regresses the "delete last of
// three, restore, it appears in the middle instead of at the end" bug:
// restore splits against the *last* surviving surface in the layout
// group, not the first, so the re-created pane appends to the tmux
// pane stack rather than landing below the root.
func TestSurfaceRestore_LastPaneRestoresAtEnd(t *testing.T) {
	eng, _ := testEngine(t)
	mockTmux := eng.Tmux.(*tmux.Mock)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "middle", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd middle: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "bottom", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd bottom: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	windowID := ws.FindSurface("middle").Tmux.WindowID
	middlePaneID := ws.FindSurface("middle").Tmux.PaneID

	if err := eng.SurfaceClose("labs", "w1", "bottom", false); err != nil {
		t.Fatalf("SurfaceClose bottom: %v", err)
	}
	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("SurfaceRestore: %v", err)
	}

	// The mock models split-window target-insert order: splitting against
	// target T inserts the new pane immediately after T in the window's
	// pane list. Asserting the new pane lands after "middle" (not between
	// the root and "middle") confirms we split against the last surface,
	// not the first.
	panes, _ := mockTmux.ListPanes(windowID)
	if len(panes) != 3 {
		t.Fatalf("expected 3 panes after restore, got %d", len(panes))
	}
	middleIdx := -1
	for i, p := range panes {
		if p.ID == middlePaneID {
			middleIdx = i
			break
		}
	}
	if middleIdx < 0 {
		t.Fatal("middle pane not found after restore")
	}
	if middleIdx != 1 {
		t.Errorf("middle pane at index %d; expected 1 (between root and restored)", middleIdx)
	}
	// Restored "bottom" must be the last pane in the window.
	restored := ws.FindSurface("bottom")
	if restored == nil {
		t.Fatal("restored bottom surface not found")
	}
	ws, _ = eng.WsShow("labs", "w1")
	restored = ws.FindSurface("bottom")
	if panes[len(panes)-1].ID != restored.Tmux.PaneID {
		t.Errorf("restored bottom is not the last pane; pane order: %v, bottom PaneID: %s",
			panesToIDs(panes), restored.Tmux.PaneID)
	}
}

// panesToIDs is a small helper for test failure messages.
func panesToIDs(panes []tmux.Pane) []string {
	ids := make([]string, len(panes))
	for i, p := range panes {
		ids[i] = p.ID
	}
	return ids
}

// TestSurfaceRestore_StackedRestoreRejoinsOriginalWindow is the end-to-end
// scenario that motivated layout-faithful restore: close three panes of
// a split-window, then restore three times. The LIFO queue means each
// restore finds its predecessor still alive, so all three land in the
// same tmux window as splits — no "new windows that never existed."
func TestSurfaceRestore_StackedRestoreRejoinsOriginalWindow(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "middle", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd middle: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "bottom", SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd bottom: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	rootName := ws.Surfaces[0].Name
	originalLayoutGroup := ws.Surfaces[0].Tmux.LayoutGroup

	// Close all three in order: bottom, middle, root.
	for _, name := range []string{"bottom", "middle", rootName} {
		if err := eng.SurfaceClose("labs", "w1", name, false); err != nil {
			t.Fatalf("SurfaceClose %s: %v", name, err)
		}
	}

	// Restore three times — the LIFO order is root, middle, bottom.
	for range 3 {
		if _, err := eng.SurfaceRestore("labs"); err != nil {
			t.Fatalf("SurfaceRestore: %v", err)
		}
	}

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 3 {
		t.Fatalf("expected 3 surfaces after stacked restore, got %d", len(ws.Surfaces))
	}
	// All three must share the originally recorded layout group.
	for _, s := range ws.Surfaces {
		if s.Tmux == nil {
			t.Errorf("surface %q has no Tmux attrs after restore", s.Name)
			continue
		}
		if s.Tmux.LayoutGroup != originalLayoutGroup {
			t.Errorf("surface %q LayoutGroup=%d; want %d (all in original window)",
				s.Name, s.Tmux.LayoutGroup, originalLayoutGroup)
		}
	}
}

// TestSurfaceRestore_FallsBackToNewWindowWhenLayoutGroupGone covers the
// case where every surface in the original layout group has been closed.
// With no sibling to split against, restore creates a new tmux window.
func TestSurfaceRestore_FallsBackToNewWindowWhenLayoutGroupGone(t *testing.T) {
	eng, _ := testEngine(t)
	mockTmux := eng.Tmux.(*tmux.Mock)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	// Add a surface in a new tmux window so the workspace has a second
	// layout group; this gives SurfaceRestore somewhere to land while
	// confirming it DOESN'T mistakenly rejoin the wrong group.
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: "w1",
		Type: manifest.SurfaceTypeShell, Name: "other-window",
	}); err != nil {
		t.Fatalf("SurfaceAdd other-window: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	rootName := ws.Surfaces[0].Name

	// Close the root (layout group 1). Its layout group becomes empty
	// because it had no siblings.
	if err := eng.SurfaceClose("labs", "w1", rootName, false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}

	before := len(mockTmux.Calls)
	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("SurfaceRestore: %v", err)
	}

	sawNewWindow := false
	for _, c := range mockTmux.Calls[before:] {
		if c.Method == "NewWindow" {
			sawNewWindow = true
		}
		if c.Method == "SplitWindowBefore" || c.Method == "SplitWindow" {
			t.Errorf("restore split into an existing window; layout group was gone. Call: %+v", c)
		}
	}
	if !sawNewWindow {
		t.Error("expected NewWindow call when the layout group is gone")
	}
}
