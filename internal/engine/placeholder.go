package engine

import "fmt"

const placeholderName = "~"

// ensureSession creates the tmux session if it doesn't exist.
// The default window created by new-session is tagged as a placeholder.
func (e *Engine) ensureSession(name string) error {
	exists, err := e.Tmux.HasSession(name)
	if err != nil {
		return fmt.Errorf("checking tmux session: %w", err)
	}
	if exists {
		return nil
	}
	if err := e.Tmux.NewSession(name); err != nil {
		return fmt.Errorf("creating tmux session: %w", err)
	}
	// Tag the default window as a placeholder
	windows, err := e.Tmux.ListWindows(name)
	if err == nil && len(windows) > 0 {
		_ = e.Tmux.SetWindowOption(windows[0].ID, "@bay-placeholder", "1")
		_ = e.Tmux.RenameWindow(windows[0].ID, placeholderName)
	}
	return nil
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
