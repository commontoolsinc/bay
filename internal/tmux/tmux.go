// Package tmux provides an interface for tmux operations needed by bay.
package tmux

// Interface defines tmux operations that bay needs.
type Interface interface {
	// Sessions
	HasSession(name string) (bool, error)
	NewSession(name string) error
	KillSession(name string) error
	RenameSession(oldName string, newName string) error
	ListSessions() ([]Session, error)

	// Windows
	NewWindow(session string, name string, cwd string) (string, error) // returns tmux window ID like "@4"
	KillWindow(windowID string) error
	RenameWindow(windowID string, name string) error
	SetWindowOption(windowID string, option string, value string) error
	GetWindowOption(windowID string, option string) (string, error)
	ListWindows(session string) ([]Window, error)
	WindowExists(windowID string) (bool, error)
	FindWindowByName(session string, name string) (string, error) // returns window ID
	SelectWindow(windowID string) error

	// Panes
	SelectPane(paneID string) error
	SplitWindow(targetID string, dir string, cwd string) (string, error) // target may be a window or pane ID; returns pane ID
	KillPane(paneID string) error
	SendKeys(paneID string, keys string) error
	CapturePane(paneID string, lines int) (string, error)
	RespawnPane(paneID string, cwd string, command string) error
	ListPanes(windowID string) ([]Pane, error)
	GetPanePID(paneID string) (int, error)

	// Pane state
	PaneExists(paneID string) (bool, error)
	GetPaneCursorY(paneID string) (int, error)

	// Current context
	CurrentSession() (string, error)
	CurrentWindowID() (string, error)
	CurrentPaneID() (string, error)

	// Client display
	DisplayMessage(msg string) error
	ClientWidth() (int, error)
}

// Session represents a tmux session.
type Session struct {
	Name string
}

// Window represents a tmux window.
type Window struct {
	ID    string // tmux unique ID like "@4"
	Name  string
	Index int
}

// Pane represents a tmux pane.
type Pane struct {
	ID     string // tmux pane ID like "%5"
	PID    int
	Active bool
}
