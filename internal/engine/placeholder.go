package engine

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/commontoolsinc/bay/internal/manifest"
)

const placeholderName = "~"

// sessionIDOption is the tmux session-scoped user option that bay sets
// on every session it manages. Its value is the same UUID stored in
// the matching Dock.SessionID. The two together let SyncAll detect a
// tmux server restart that left a same-named session in its wake.
const sessionIDOption = "@bay-session-id"

// newSessionID generates a fresh session identity. var (not const-fn)
// so tests can swap in deterministic IDs.
var newSessionID = func() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// verifySessionOwnership reports whether the tmux session for dock
// exists and, if so, whether it's the one bay last set up.
//
// `exists` is false when no tmux session of dock.Name is found —
// today's "session fully gone" branch. Callers should preserve state.
//
// `owned` is true only when the session's @bay-session-id marker and
// dock.SessionID agree (or both empty for legacy/pre-rollout). Any
// mismatch — foreign session resurrection, in-flight recover that
// hasn't persisted its new SessionID yet — produces owned=false so
// pane-id-based cleanup is suppressed.
func (e *Engine) verifySessionOwnership(dock *manifest.Dock) (exists, owned bool) {
	exists, _ = e.Tmux.HasSession(dock.Name)
	if !exists {
		return false, false
	}
	marker, _ := e.Tmux.GetSessionOption(dock.Name, sessionIDOption)
	switch {
	case marker == "" && dock.SessionID == "":
		// Legacy state — neither side has been tagged. Behave as today
		// (owned, run cleanup) so existing live docks don't regress.
		return true, true
	case marker == dock.SessionID:
		return true, true
	default:
		// (empty marker, set manifest) — session was recreated outside bay
		// (set marker, empty manifest)  — recover in flight, or partial failure
		// (set, set, mismatched)        — session was recreated since bay last ran
		return true, false
	}
}

// ensureSession ensures a tmux session named `name` exists and carries
// a known @bay-session-id marker. It does NOT persist the manifest —
// the caller writes the returned session ID at the moment that's
// race-safe for its flow (see docs/design/session-identity.md).
//
// expectedSessionID is the manifest's stored Dock.SessionID for this
// dock (empty for fresh docks). Behavior matrix:
//
//   - no session                        → create + tag with new UUID
//   - session, marker empty             → tag with new UUID (claim)
//   - session, marker matches expected  → no-op, return existing
//   - session, marker mismatched        → re-tag with new UUID (claim)
//   - session, marker set, expected ""  → adopt the existing marker
//
// Returns the UUID that the session's marker now holds.
func (e *Engine) ensureSession(name, expectedSessionID string) (string, error) {
	exists, err := e.Tmux.HasSession(name)
	if err != nil {
		return "", fmt.Errorf("checking tmux session: %w", err)
	}
	if !exists {
		if err := e.Tmux.NewSession(name); err != nil {
			return "", fmt.Errorf("creating tmux session: %w", err)
		}
		// Tag the default window as a placeholder.
		windows, err := e.Tmux.ListWindows(name)
		if err == nil && len(windows) > 0 {
			_ = e.Tmux.SetWindowOption(windows[0].ID, "@bay-placeholder", "1")
			_ = e.Tmux.RenameWindow(windows[0].ID, placeholderName)
		}
		return e.tagSession(name)
	}

	marker, _ := e.Tmux.GetSessionOption(name, sessionIDOption)
	switch {
	case marker != "" && marker == expectedSessionID:
		// Steady state — leave both sides alone.
		return marker, nil
	case marker != "" && expectedSessionID == "":
		// Tmux is tagged but the manifest hasn't recorded a SessionID
		// yet. Adopt the existing marker rather than churning a new
		// one (covers the post-failure recovery path).
		return marker, nil
	default:
		// (empty, empty)        → first-time claim for a legacy session
		// (empty, set)          → session was recreated without bay's marker
		// (set, set, different) → session was recreated since bay last ran
		return e.tagSession(name)
	}
}

// tagSession sets a fresh @bay-session-id on the named session and
// returns the UUID it set. Called from every ensureSession branch
// that needs to (re-)tag.
func (e *Engine) tagSession(name string) (string, error) {
	id := newSessionID()
	if err := e.Tmux.SetSessionOption(name, sessionIDOption, id); err != nil {
		return "", fmt.Errorf("tagging tmux session: %w", err)
	}
	return id, nil
}

// cleanPlaceholders removes unused placeholder windows from a session.
// A placeholder is unused if cursor_y <= 1 (no commands have been run).
// Called after creating a real window, so the session won't be empty.
func (e *Engine) cleanPlaceholders(session string) {
	windows, err := e.Tmux.ListWindows(session)
	if err != nil {
		return
	}
	for _, win := range windows {
		val, err := e.Tmux.GetWindowOption(win.ID, "@bay-placeholder")
		if err != nil || val != "1" {
			continue
		}
		// Check if the placeholder has been used
		panes, err := e.Tmux.ListPanes(win.ID)
		if err != nil || len(panes) == 0 {
			_ = e.Tmux.KillWindow(win.ID)
			continue
		}
		cursorY, err := e.Tmux.GetPaneCursorY(panes[0].ID)
		if err != nil {
			continue // can't determine, leave it
		}
		if cursorY <= 1 {
			_ = e.Tmux.KillWindow(win.ID)
		}
		// If cursor has moved, the user is using it — leave it alone
	}
}

// ensurePlaceholderIfLastWindow checks if killing windowID would leave the
// session empty, and if so, creates a placeholder first.
func (e *Engine) ensurePlaceholderIfLastWindow(session, windowID string) {
	windows, err := e.Tmux.ListWindows(session)
	if err != nil {
		return
	}
	// Count non-placeholder windows (excluding the one about to be killed)
	remaining := 0
	for _, win := range windows {
		if win.ID == windowID {
			continue
		}
		val, _ := e.Tmux.GetWindowOption(win.ID, "@bay-placeholder")
		if val != "1" {
			remaining++
		}
	}
	if remaining > 0 {
		return // other real windows exist
	}
	// This is the last real window — create a placeholder to keep the session alive
	phID, err := e.Tmux.NewWindow(session, placeholderName, "")
	if err != nil {
		return
	}
	_ = e.Tmux.SetWindowOption(phID, "@bay-placeholder", "1")
}
