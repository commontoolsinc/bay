package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// closeConfirmWindow is the second-tap deadline. Short enough that two
// presses read as deliberate (Claude Code's ctrl-D-twice timing), long
// enough to forgive a moment of hesitation.
const closeConfirmWindow = 1500 * time.Millisecond

// shouldConfirmLastSurfaceClose scopes the double-tap guard to non-interactive
// invocations, which covers tmux run-shell keybindings such as Option+w.
// Interactive CLI invocations are already deliberate enough to close the last
// surface on the first command.
//
// Package-level var so tests can exercise both paths without depending on the
// test runner's stdin.
var shouldConfirmLastSurfaceClose = func() bool {
	return !stdinIsTTY()
}

// flashFunc displays a transient status-line message. Mirrors
// tmux.Interface.DisplayMessage but narrowed so confirmLastSurfaceClose
// doesn't need the rest of the tmux surface.
type flashFunc func(msg string, durationMs int) error

// confirmLastSurfaceClose decides whether to proceed with closing the last
// surface in a workspace. Returns true if a recent close attempt for the same
// (dock, ws) is on file (the user is double-tapping). Otherwise records a
// fresh attempt, flashes a status message asking for a second tap, and
// returns false.
//
// Package-level var so tests can substitute a deterministic implementation.
var confirmLastSurfaceClose = func(flash flashFunc, dockName, wsName string) bool {
	path := bayPaths().CloseConfirm
	if recordedRecently(path, dockName, wsName, time.Now()) {
		_ = os.Remove(path)
		return true
	}
	if err := writeCloseConfirm(path, dockName, wsName, time.Now()); err != nil {
		// Fail open: without state we can't enforce a second tap. Better
		// to let the close go through than wedge the user with no recovery.
		return true
	}
	// Message duration matches the confirm window so the message vanishing
	// is itself the deadline — no need to say "2s" and risk a stale number.
	_ = flash(
		fmt.Sprintf("press again to close last surface in %q", wsName),
		int(closeConfirmWindow/time.Millisecond),
	)
	return false
}

// recordedRecently reports whether a close-confirm record exists for
// (dock, ws) within closeConfirmWindow of now. Missing or unreadable file,
// mismatched target, or an expired record all return false.
func recordedRecently(path, dockName, wsName string, now time.Time) bool {
	d, w, t, ok := readCloseConfirm(path)
	if !ok {
		return false
	}
	if d != dockName || w != wsName {
		return false
	}
	return now.Sub(t) <= closeConfirmWindow
}

// File format: a single line "<dock>\t<ws>\t<unix-nanos>\n". Tabs are
// disallowed in dock/ws names by ValidateName, so the split is unambiguous.
func readCloseConfirm(path string) (dock, ws string, when time.Time, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", time.Time{}, false
	}
	line := strings.TrimRight(string(data), "\n")
	parts := strings.Split(line, "\t")
	if len(parts) != 3 {
		return "", "", time.Time{}, false
	}
	ns, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", "", time.Time{}, false
	}
	return parts[0], parts[1], time.Unix(0, ns), true
}

func writeCloseConfirm(path, dock, ws string, when time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	line := fmt.Sprintf("%s\t%s\t%d\n", dock, ws, when.UnixNano())
	return os.WriteFile(path, []byte(line), 0o644)
}
