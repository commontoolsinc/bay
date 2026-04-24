package engine

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

// assertLayout fails the test if the window's tmux layout doesn't match
// want. The layout-string syntax: pane IDs as leaves, "/" for vertical
// splits, "|" for horizontal, parens around nested internal nodes.
// Example: "%1/(%2|%3)" — %1 stacked above a horizontal split of %2 and %3.
func assertLayout(t *testing.T, mock *tmux.Mock, windowID, want string) {
	t.Helper()
	got, err := mock.LayoutString(windowID)
	if err != nil {
		t.Fatalf("LayoutString(%s): %v", windowID, err)
	}
	if got != want {
		t.Errorf("layout mismatch for window %s\n  got:  %s\n  want: %s", windowID, got, want)
	}
}

// addShell is a tiny helper for the layout-sensitive tests below: adds
// a shell surface with the given name and split direction, and returns
// the new surface's pane ID for use in expected layout strings.
func addShell(t *testing.T, eng *Engine, ws, name, splitDir string) string {
	t.Helper()
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs", WsName: ws,
		Type: manifest.SurfaceTypeShell, Name: name, SplitDir: splitDir,
	}); err != nil {
		t.Fatalf("SurfaceAdd %s: %v", name, err)
	}
	wsView, _ := eng.WsShow("labs", ws)
	return wsView.FindSurface(name).Tmux.PaneID
}

// TestSurfaceRestore_LayoutCloseLastRestoresAtEnd is the regression test
// for "delete last of three, restore goes in middle." With layout-string
// assertions the failure mode is unambiguous.
func TestSurfaceRestore_LayoutCloseLastRestoresAtEnd(t *testing.T) {
	eng, _ := testEngine(t)
	mock := eng.Tmux.(*tmux.Mock)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	wsView, _ := eng.WsShow("labs", "w1")
	rootPane := wsView.Surfaces[0].Tmux.PaneID
	winID := wsView.Surfaces[0].Tmux.WindowID

	middle := addShell(t, eng, "w1", "middle", "v")
	bottom := addShell(t, eng, "w1", "bottom", "v")

	// Three vertical panes — but tmux's binary-split model nests:
	// new splits replace the target leaf with a {root, new} split.
	assertLayout(t, mock, winID, rootPane+"/("+middle+"/"+bottom+")")

	if err := eng.SurfaceClose("labs", "w1", "bottom", false); err != nil {
		t.Fatalf("SurfaceClose bottom: %v", err)
	}
	// After collapsing the singleton split, layout is the simple stack.
	assertLayout(t, mock, winID, rootPane+"/"+middle)

	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("SurfaceRestore: %v", err)
	}
	// The restored pane lands as a sibling of the last surface (middle),
	// nested in middle's region — visually appended to the bottom.
	wsView, _ = eng.WsShow("labs", "w1")
	restored := wsView.FindSurface("bottom").Tmux.PaneID
	assertLayout(t, mock, winID, rootPane+"/("+middle+"/"+restored+")")
}

// TestSurfaceRestore_LayoutRootPaneLandsAtRoot exercises the
// `split-window -fb` path: closing the root pane and restoring it must
// place the new pane at the root level of the window's layout, wrapping
// the surviving siblings.
func TestSurfaceRestore_LayoutRootPaneLandsAtRoot(t *testing.T) {
	eng, _ := testEngine(t)
	mock := eng.Tmux.(*tmux.Mock)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	wsView, _ := eng.WsShow("labs", "w1")
	rootName := wsView.Surfaces[0].Name
	winID := wsView.Surfaces[0].Tmux.WindowID

	middle := addShell(t, eng, "w1", "middle", "v")
	bottom := addShell(t, eng, "w1", "bottom", "v")

	if err := eng.SurfaceClose("labs", "w1", rootName, false); err != nil {
		t.Fatalf("SurfaceClose root: %v", err)
	}
	// After root is gone, the surviving inner split collapses out of the
	// root spot — layout is just the middle/bottom pair.
	assertLayout(t, mock, winID, middle+"/"+bottom)

	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("SurfaceRestore: %v", err)
	}
	// The restored root must wrap the surviving panes, not nest inside
	// one of them. With -fb the layout becomes {restored, prev_layout}.
	wsView, _ = eng.WsShow("labs", "w1")
	restored := wsView.FindSurface(rootName).Tmux.PaneID
	assertLayout(t, mock, winID, restored+"/("+middle+"/"+bottom+")")
}

// TestSurfaceRestore_LayoutStackedRestoreReproducesOriginal confirms the
// LIFO close-then-restore composition: closing every pane and restoring
// in order rebuilds the original layout structure.
func TestSurfaceRestore_LayoutStackedRestoreReproducesOriginal(t *testing.T) {
	eng, _ := testEngine(t)
	mock := eng.Tmux.(*tmux.Mock)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	wsView, _ := eng.WsShow("labs", "w1")
	rootName := wsView.Surfaces[0].Name

	addShell(t, eng, "w1", "middle", "v")
	addShell(t, eng, "w1", "bottom", "v")

	// Close every pane in order: bottom, middle, root. Closing the last
	// surface kills the tmux window, so the post-restore window is a
	// freshly created one — captured below from the first restore.
	for _, name := range []string{"bottom", "middle", rootName} {
		if err := eng.SurfaceClose("labs", "w1", name, false); err != nil {
			t.Fatalf("SurfaceClose %s: %v", name, err)
		}
	}

	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("restore root: %v", err)
	}
	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("restore middle: %v", err)
	}
	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("restore bottom: %v", err)
	}

	wsView, _ = eng.WsShow("labs", "w1")
	rootPane := wsView.FindSurface(rootName).Tmux.PaneID
	middlePane := wsView.FindSurface("middle").Tmux.PaneID
	bottomPane := wsView.FindSurface("bottom").Tmux.PaneID
	// Look up the current window ID — the original was killed when its
	// last surface closed.
	winID := wsView.FindSurface(rootName).Tmux.WindowID

	got, err := mock.LayoutString(winID)
	if err != nil {
		t.Fatalf("LayoutString: %v", err)
	}
	// Strip parens for an order-only check, since the binary nesting
	// shape is an implementation detail of the restore sequence.
	flat := strings.NewReplacer("(", "", ")", "").Replace(got)
	want := rootPane + "/" + middlePane + "/" + bottomPane
	if flat != want {
		t.Errorf("stacked-restore visual order wrong\n  got (flattened): %s\n  raw:             %s\n  want:            %s", flat, got, want)
	}
}

// TestSurfaceRestore_LayoutMixedAxisStaysIntact ensures the layout-tree
// mock correctly handles a mixed-axis layout: a horizontal split on top
// with a vertical split inside its right half. Closing and restoring
// either pane should preserve nesting structure.
func TestSurfaceRestore_LayoutMixedAxisStaysIntact(t *testing.T) {
	eng, _ := testEngine(t)
	mock := eng.Tmux.(*tmux.Mock)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	wsView, _ := eng.WsShow("labs", "w1")
	rootPane := wsView.Surfaces[0].Tmux.PaneID
	winID := wsView.Surfaces[0].Tmux.WindowID

	right := addShell(t, eng, "w1", "right", "h")
	rightBottom := addShell(t, eng, "w1", "right-bottom", "v")

	// Split shape: root | (right / right-bottom).
	// Splits-against-last logic puts right-bottom under "right" since
	// "right" is the most-recently-added surface in layout group 1.
	assertLayout(t, mock, winID, rootPane+"|("+right+"/"+rightBottom+")")

	// Close the bottom of the right column, restore. It should land
	// back as a vertical split with "right" — no axis confusion.
	if err := eng.SurfaceClose("labs", "w1", "right-bottom", false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}
	assertLayout(t, mock, winID, rootPane+"|"+right)

	if _, err := eng.SurfaceRestore("labs"); err != nil {
		t.Fatalf("SurfaceRestore: %v", err)
	}
	wsView, _ = eng.WsShow("labs", "w1")
	restored := wsView.FindSurface("right-bottom").Tmux.PaneID
	assertLayout(t, mock, winID, rootPane+"|("+right+"/"+restored+")")
}
