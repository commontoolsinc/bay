package tmux

import (
	"fmt"
	"sort"
)

// Call records a method call on the mock.
type Call struct {
	Method string
	Args   []string
}

// mockWindow holds internal mock state for a window.
type mockWindow struct {
	id      string
	name    string
	session string
	index   int
	options map[string]string
	panes   []string // pane IDs belonging to this window
}

// mockPane holds internal mock state for a pane.
type mockPane struct {
	id       string
	windowID string
	pid      int
	active   bool
	capture  string // content returned by CapturePane
	cursorY  int
}

// Mock is a test double that implements Interface using in-memory state.
type Mock struct {
	Calls []Call

	sessions map[string]bool
	windows  map[string]*mockWindow  // keyed by window ID
	panes    map[string]*mockPane    // keyed by pane ID

	windowCounter int
	paneCounter   int
	pidCounter    int

	currentSession  string
	currentWindowID string
	currentSessionSet  bool
	currentWindowIDSet bool
}

// NewMock creates a new Mock with initialized state.
func NewMock() *Mock {
	return &Mock{
		sessions: make(map[string]bool),
		windows:  make(map[string]*mockWindow),
		panes:    make(map[string]*mockPane),
	}
}

func (m *Mock) record(method string, args ...string) {
	m.Calls = append(m.Calls, Call{Method: method, Args: args})
}

func (m *Mock) nextWindowID() string {
	m.windowCounter++
	return fmt.Sprintf("@%d", m.windowCounter)
}

func (m *Mock) nextPaneID() string {
	m.paneCounter++
	return fmt.Sprintf("%%%d", m.paneCounter)
}

func (m *Mock) nextPID() int {
	m.pidCounter++
	return 10000 + m.pidCounter
}

// --- Sessions ---

func (m *Mock) HasSession(name string) (bool, error) {
	m.record("HasSession", name)
	return m.sessions[name], nil
}

func (m *Mock) NewSession(name string) error {
	m.record("NewSession", name)
	if m.sessions[name] {
		return fmt.Errorf("session %q already exists", name)
	}
	m.sessions[name] = true
	// Real tmux creates a default window when a session is created.
	// Replicate that behavior so placeholder logic works correctly.
	m.NewWindow(name, "", "")
	return nil
}

func (m *Mock) KillSession(name string) error {
	m.record("KillSession", name)
	if !m.sessions[name] {
		return fmt.Errorf("session %q not found", name)
	}
	// Remove all windows (and their panes) belonging to this session.
	for wid, w := range m.windows {
		if w.session == name {
			for _, pid := range w.panes {
				delete(m.panes, pid)
			}
			delete(m.windows, wid)
		}
	}
	delete(m.sessions, name)
	return nil
}

func (m *Mock) ListSessions() ([]Session, error) {
	m.record("ListSessions")
	var result []Session
	for name := range m.sessions {
		result = append(result, Session{Name: name})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// --- Windows ---

func (m *Mock) NewWindow(session string, name string, cwd string) (string, error) {
	m.record("NewWindow", session, name, cwd)
	if !m.sessions[session] {
		return "", fmt.Errorf("session %q not found", session)
	}
	id := m.nextWindowID()
	index := 0
	for _, w := range m.windows {
		if w.session == session && w.index >= index {
			index = w.index + 1
		}
	}
	w := &mockWindow{
		id:      id,
		name:    name,
		session: session,
		index:   index,
		options: make(map[string]string),
	}
	// Create the initial pane for this window.
	paneID := m.nextPaneID()
	p := &mockPane{
		id:       paneID,
		windowID: id,
		pid:      m.nextPID(),
		active:   true,
	}
	w.panes = []string{paneID}
	m.windows[id] = w
	m.panes[paneID] = p
	return id, nil
}

func (m *Mock) KillWindow(windowID string) error {
	m.record("KillWindow", windowID)
	w, ok := m.windows[windowID]
	if !ok {
		return fmt.Errorf("window %q not found", windowID)
	}
	for _, pid := range w.panes {
		delete(m.panes, pid)
	}
	delete(m.windows, windowID)
	return nil
}

func (m *Mock) RenameWindow(windowID string, name string) error {
	m.record("RenameWindow", windowID, name)
	w, ok := m.windows[windowID]
	if !ok {
		return fmt.Errorf("window %q not found", windowID)
	}
	w.name = name
	return nil
}

func (m *Mock) SetWindowOption(windowID string, option string, value string) error {
	m.record("SetWindowOption", windowID, option, value)
	w, ok := m.windows[windowID]
	if !ok {
		return fmt.Errorf("window %q not found", windowID)
	}
	w.options[option] = value
	return nil
}

func (m *Mock) GetWindowOption(windowID string, option string) (string, error) {
	m.record("GetWindowOption", windowID, option)
	w, ok := m.windows[windowID]
	if !ok {
		return "", fmt.Errorf("window %q not found", windowID)
	}
	val, ok := w.options[option]
	if !ok {
		return "", fmt.Errorf("option %q not set on window %q", option, windowID)
	}
	return val, nil
}

func (m *Mock) ListWindows(session string) ([]Window, error) {
	m.record("ListWindows", session)
	if !m.sessions[session] {
		return nil, fmt.Errorf("session %q not found", session)
	}
	var result []Window
	for _, w := range m.windows {
		if w.session == session {
			result = append(result, Window{
				ID:    w.id,
				Name:  w.name,
				Index: w.index,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Index < result[j].Index
	})
	return result, nil
}

func (m *Mock) WindowExists(windowID string) (bool, error) {
	m.record("WindowExists", windowID)
	_, ok := m.windows[windowID]
	return ok, nil
}

func (m *Mock) FindWindowByName(session string, name string) (string, error) {
	m.record("FindWindowByName", session, name)
	if !m.sessions[session] {
		return "", fmt.Errorf("session %q not found", session)
	}
	for _, w := range m.windows {
		if w.session == session && w.name == name {
			return w.id, nil
		}
	}
	return "", fmt.Errorf("window %q not found in session %q", name, session)
}

func (m *Mock) SelectWindow(windowID string) error {
	m.record("SelectWindow", windowID)
	if _, ok := m.windows[windowID]; !ok {
		return fmt.Errorf("window %q not found", windowID)
	}
	return nil
}

// --- Panes ---

func (m *Mock) SplitWindow(windowID string, dir string, cwd string) (string, error) {
	m.record("SplitWindow", windowID, dir, cwd)
	w, ok := m.windows[windowID]
	if !ok {
		return "", fmt.Errorf("window %q not found", windowID)
	}
	paneID := m.nextPaneID()
	p := &mockPane{
		id:       paneID,
		windowID: windowID,
		pid:      m.nextPID(),
		active:   false,
	}
	w.panes = append(w.panes, paneID)
	m.panes[paneID] = p
	return paneID, nil
}

func (m *Mock) KillPane(paneID string) error {
	m.record("KillPane", paneID)
	p, ok := m.panes[paneID]
	if !ok {
		return fmt.Errorf("pane %q not found", paneID)
	}
	// Remove from parent window's pane list.
	if w, ok := m.windows[p.windowID]; ok {
		for i, pid := range w.panes {
			if pid == paneID {
				w.panes = append(w.panes[:i], w.panes[i+1:]...)
				break
			}
		}
	}
	delete(m.panes, paneID)
	return nil
}

func (m *Mock) SendKeys(paneID string, keys string) error {
	m.record("SendKeys", paneID, keys)
	if _, ok := m.panes[paneID]; !ok {
		return fmt.Errorf("pane %q not found", paneID)
	}
	return nil
}

func (m *Mock) CapturePane(paneID string, lines int) (string, error) {
	m.record("CapturePane", paneID, fmt.Sprintf("%d", lines))
	p, ok := m.panes[paneID]
	if !ok {
		return "", fmt.Errorf("pane %q not found", paneID)
	}
	return p.capture, nil
}

func (m *Mock) RespawnPane(paneID string, cwd string, command string) error {
	m.record("RespawnPane", paneID, cwd, command)
	if _, ok := m.panes[paneID]; !ok {
		return fmt.Errorf("pane %q not found", paneID)
	}
	return nil
}

func (m *Mock) ListPanes(windowID string) ([]Pane, error) {
	m.record("ListPanes", windowID)
	w, ok := m.windows[windowID]
	if !ok {
		return nil, fmt.Errorf("window %q not found", windowID)
	}
	var result []Pane
	for _, pid := range w.panes {
		p := m.panes[pid]
		result = append(result, Pane{
			ID:     p.id,
			PID:    p.pid,
			Active: p.active,
		})
	}
	return result, nil
}

func (m *Mock) GetPanePID(paneID string) (int, error) {
	m.record("GetPanePID", paneID)
	p, ok := m.panes[paneID]
	if !ok {
		return 0, fmt.Errorf("pane %q not found", paneID)
	}
	return p.pid, nil
}

func (m *Mock) GetPaneCursorY(paneID string) (int, error) {
	m.record("GetPaneCursorY", paneID)
	p, ok := m.panes[paneID]
	if !ok {
		return 0, fmt.Errorf("pane %q not found", paneID)
	}
	return p.cursorY, nil
}

// SetPaneCursorY sets the cursor Y position for a pane (test helper).
func (m *Mock) SetPaneCursorY(paneID string, y int) {
	if p, ok := m.panes[paneID]; ok {
		p.cursorY = y
	}
}

// --- Current context ---

func (m *Mock) CurrentSession() (string, error) {
	m.record("CurrentSession")
	if !m.currentSessionSet {
		return "", fmt.Errorf("not in a tmux session")
	}
	return m.currentSession, nil
}

func (m *Mock) CurrentWindowID() (string, error) {
	m.record("CurrentWindowID")
	if !m.currentWindowIDSet {
		return "", fmt.Errorf("not in a tmux window")
	}
	return m.currentWindowID, nil
}

// --- Mock helpers (not part of Interface) ---

// SetCurrentSession sets the value returned by CurrentSession.
func (m *Mock) SetCurrentSession(name string) {
	m.currentSession = name
	m.currentSessionSet = true
}

// SetCurrentWindowID sets the value returned by CurrentWindowID.
func (m *Mock) SetCurrentWindowID(id string) {
	m.currentWindowID = id
	m.currentWindowIDSet = true
}

// SetCaptureContent sets the content that CapturePane will return for a pane.
func (m *Mock) SetCaptureContent(paneID string, content string) {
	if p, ok := m.panes[paneID]; ok {
		p.capture = content
	}
}

// Reset clears all tmux mock state (simulates reboot).
func (m *Mock) Reset() {
	m.Calls = nil
	m.sessions = make(map[string]bool)
	m.windows = make(map[string]*mockWindow)
	m.panes = make(map[string]*mockPane)
	m.windowCounter = 0
	m.paneCounter = 0
	m.pidCounter = 0
	m.currentSessionSet = false
	m.currentWindowIDSet = false
}

// HasSessionCalled returns true if NewSession was called with the given name
// (i.e. the session currently exists).
func (m *Mock) HasSessionCalled(name string) bool {
	return m.sessions[name]
}
