//go:build integration

// Integration tests for tmux primitives against a real tmux server. These
// pin the Mock's modeled behavior to ground truth — for any sequence the
// mock claims produces a particular pane order, the same sequence on real
// tmux is asserted to produce the same order.
//
// These tests start a tmux server on an isolated socket (`-L bay-test-...`)
// so they don't disturb the user's running tmux. They're skipped by
// default; run them explicitly:
//
//   go test -tags=integration ./internal/tmux/
//
// Requires tmux to be installed and on PATH.

package tmux

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// integrationServer wraps a tmux server running on a unique socket, so
// the test's commands can't touch the user's regular tmux state.
type integrationServer struct {
	socket string
	t      *testing.T
}

// startTmux starts a new tmux server on a unique socket and registers a
// cleanup hook that kills it when the test ends, even on failure. The
// socket name embeds t.Name() so concurrent tests (under -parallel)
// can't collide.
func startTmux(t *testing.T) *integrationServer {
	t.Helper()
	socket := fmt.Sprintf("bay-%s-%d", sanitizeSocketName(t.Name()), time.Now().UnixNano())
	s := &integrationServer{socket: socket, t: t}
	t.Cleanup(s.killServer)
	return s
}

// sanitizeSocketName replaces characters tmux's `-L` doesn't accept in a
// socket name (slashes, colons) with underscores.
func sanitizeSocketName(name string) string {
	return strings.NewReplacer("/", "_", ":", "_").Replace(name)
}

// setupSession is the shared prologue for the integration tests below:
// starts a private tmux server, creates a session, and returns the
// initial window ID and root pane ID.
func setupSession(t *testing.T) (s *integrationServer, winID, rootPaneID string) {
	t.Helper()
	s = startTmux(t)
	s.newDetachedSession("test")
	winID = s.firstWindowID("test")
	rootPaneID = s.listPaneIDs(winID)[0]
	return
}

func (s *integrationServer) tmuxCmd(args ...string) (string, error) {
	full := append([]string{"-L", s.socket}, args...)
	out, err := exec.Command("tmux", full...).CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("tmux %s: %w (%s)",
			strings.Join(full, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *integrationServer) mustCmd(args ...string) string {
	s.t.Helper()
	out, err := s.tmuxCmd(args...)
	if err != nil {
		s.t.Fatalf("%v", err)
	}
	return out
}

func (s *integrationServer) killServer() {
	_ = exec.Command("tmux", "-L", s.socket, "kill-server").Run()
}

func (s *integrationServer) newDetachedSession(name string) {
	// -d: detached. -x/-y: pin a known terminal size so split-window
	// success doesn't depend on the test runner's TERM.
	s.mustCmd("new-session", "-d", "-s", name, "-x", "200", "-y", "60")
}

// firstWindowID returns the ID of the active window in the session.
func (s *integrationServer) firstWindowID(session string) string {
	return s.mustCmd("display-message", "-t", session, "-p", "#{window_id}")
}

// listPaneIDs returns pane IDs in tmux's natural depth-first order.
func (s *integrationServer) listPaneIDs(target string) []string {
	out := s.mustCmd("list-panes", "-t", target, "-F", "#{pane_id}")
	return strings.Split(out, "\n")
}

// split runs split-window and returns the new pane ID.
func (s *integrationServer) split(target, dir string, before bool) string {
	args := []string{"split-window"}
	if before {
		args = append(args, "-fb")
	}
	flag := "-v"
	if dir == "h" {
		flag = "-h"
	}
	args = append(args, "-t", target, flag, "-c", "/tmp", "-P", "-F", "#{pane_id}")
	return s.mustCmd(args...)
}

// killPane kills a pane by ID. Mirror of the SplitWindow helper for
// callsite symmetry — keeps tmux command details out of test bodies.
func (s *integrationServer) killPane(paneID string) {
	s.mustCmd("kill-pane", "-t", paneID)
}

// paneGeometry returns (left, top, width, height) for a pane.
func (s *integrationServer) paneGeometry(paneID string) (left, top, width, height int) {
	out := s.mustCmd("display-message", "-t", paneID, "-p",
		"#{pane_left} #{pane_top} #{pane_width} #{pane_height}")
	if _, err := fmt.Sscanf(out, "%d %d %d %d", &left, &top, &width, &height); err != nil {
		s.t.Fatalf("parsing geometry %q: %v", out, err)
	}
	return
}

// --- Tests ---

// TestIntegration_SplitNestsBinaryTree mirrors TestMockLayout_SplitNestsBinaryTree.
// Two successive splits against the previous pane should yield three panes
// in their creation order in real tmux too.
func TestIntegration_SplitNestsBinaryTree(t *testing.T) {
	s, win, root := setupSession(t)
	a := s.split(root, "v", false)
	b := s.split(a, "v", false)

	got := s.listPaneIDs(win)
	want := []string{root, a, b}
	if !slices.Equal(got, want) {
		t.Errorf("pane order = %v, want %v", got, want)
	}
}

// TestIntegration_SplitBeforeWrapsRoot mirrors TestMockLayout_SplitBeforeWrapsRoot.
// `split-window -fb` should insert the new pane at the top/left of the
// window, ahead of any existing panes.
func TestIntegration_SplitBeforeWrapsRoot(t *testing.T) {
	s, win, root := setupSession(t)
	b := s.split(root, "v", false)
	prepended := s.split(root, "v", true)

	got := s.listPaneIDs(win)
	want := []string{prepended, root, b}
	if !slices.Equal(got, want) {
		t.Errorf("pane order = %v, want %v", got, want)
	}

	// The prepended pane must occupy the topmost row in the window.
	_, prependedTop, _, _ := s.paneGeometry(prepended)
	_, rootTop, _, _ := s.paneGeometry(root)
	if prependedTop >= rootTop {
		t.Errorf("prepended pane top=%d not above root top=%d", prependedTop, rootTop)
	}
}

// TestIntegration_KillPaneCollapsesBinarySplit mirrors
// TestMockLayout_KillPaneCollapsesBinarySplit. After killing one of two
// sibling panes, the survivor should be the only pane left in the window
// (tmux's binary-split tree collapses).
func TestIntegration_KillPaneCollapsesBinarySplit(t *testing.T) {
	s, win, root := setupSession(t)
	other := s.split(root, "v", false)
	s.killPane(other)

	got := s.listPaneIDs(win)
	want := []string{root}
	if !slices.Equal(got, want) {
		t.Errorf("after kill, pane order = %v, want %v", got, want)
	}
}

// TestIntegration_MixedAxisLayout exercises the same shape as
// TestSurfaceRestore_LayoutMixedAxisStaysIntact: a horizontal split on the
// outer axis with a vertical split inside the right half.
func TestIntegration_MixedAxisLayout(t *testing.T) {
	s, win, root := setupSession(t)
	right := s.split(root, "h", false)
	rightBottom := s.split(right, "v", false)

	got := s.listPaneIDs(win)
	want := []string{root, right, rightBottom}
	if !slices.Equal(got, want) {
		t.Errorf("pane order = %v, want %v", got, want)
	}

	// Geometry sanity: root and right share the same row (horizontal
	// split); right and rightBottom share the same column (vertical
	// split inside right's region).
	rootLeft, rootTop, _, _ := s.paneGeometry(root)
	rightLeft, rightTop, _, _ := s.paneGeometry(right)
	rightBottomLeft, rightBottomTop, _, _ := s.paneGeometry(rightBottom)

	if rootTop != rightTop {
		t.Errorf("root and right not on same row: root_top=%d right_top=%d", rootTop, rightTop)
	}
	if rootLeft >= rightLeft {
		t.Errorf("root_left=%d not left of right_left=%d", rootLeft, rightLeft)
	}
	if rightLeft != rightBottomLeft {
		t.Errorf("right and rightBottom not in same column: right_left=%d rightBottom_left=%d",
			rightLeft, rightBottomLeft)
	}
	if rightTop >= rightBottomTop {
		t.Errorf("right_top=%d not above rightBottom_top=%d", rightTop, rightBottomTop)
	}
}

// TestIntegration_StackedCloseRestoreSequence simulates the user-reported
// "delete last of three, restore appends" scenario at the tmux primitive
// level. Build [root, middle, bottom], close bottom, re-add by splitting
// against middle (matching SurfaceAdd's `lastSurfaceInLayoutGroup` behavior),
// and verify the new pane lands at the visual end.
func TestIntegration_StackedCloseRestoreSequence(t *testing.T) {
	s, win, root := setupSession(t)
	middle := s.split(root, "v", false)
	bottom := s.split(middle, "v", false)

	got := s.listPaneIDs(win)
	want := []string{root, middle, bottom}
	if !slices.Equal(got, want) {
		t.Fatalf("initial pane order = %v, want %v", got, want)
	}

	// Close the last pane, then re-add by splitting against the now-last
	// surviving sibling — this is what bay's restore does.
	s.killPane(bottom)
	got = s.listPaneIDs(win)
	if !slices.Equal(got, []string{root, middle}) {
		t.Fatalf("after kill, pane order = %v, want %v", got, []string{root, middle})
	}

	restored := s.split(middle, "v", false)
	got = s.listPaneIDs(win)
	want = []string{root, middle, restored}
	if !slices.Equal(got, want) {
		t.Errorf("after restore, pane order = %v, want %v", got, want)
	}
}
