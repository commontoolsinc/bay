package tmux

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
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
//
// The recorder (Calls + record) is protected by mu so concurrent callers
// from the engine's parallel sync paths don't race on the slice append.
// Tests that read or reset Calls directly should do so sequentially
// (between test phases), not concurrently with engine code.
type Mock struct {
	mu    sync.Mutex
	Calls []Call

	sessions map[string]bool
	windows  map[string]*mockWindow // keyed by window ID
	panes    map[string]*mockPane   // keyed by pane ID

	windowCounter int
	paneCounter   int
	pidCounter    int

	currentSession     string
	currentWindowID    string
	currentPaneID      string
	currentSessionSet  bool
	currentWindowIDSet bool
	currentPaneIDSet   bool

	clientWidth         int
	statusReservedCells int
	displayMessages     []string // recorded DisplayMessage calls in order

	// OnKill is an optional test hook fired at the start of any
	// destructive operation (KillPane / KillWindow / KillSession)
	// before the mock mutates its own state. Tests use it to assert
	// invariants like "the manifest must already be updated by the
	// time bay issues a tmux kill" without instrumenting production
	// code. method is "KillPane" / "KillWindow" / "KillSession".
	OnKill func(method, target string)
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
	m.mu.Lock()
	defer m.mu.Unlock()
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
	if m.OnKill != nil {
		m.OnKill("KillSession", name)
	}
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

func (m *Mock) RenameSession(oldName string, newName string) error {
	m.record("RenameSession", oldName, newName)
	if !m.sessions[oldName] {
		return fmt.Errorf("session %q not found", oldName)
	}
	if m.sessions[newName] {
		return fmt.Errorf("session %q already exists", newName)
	}
	delete(m.sessions, oldName)
	m.sessions[newName] = true
	// Update all windows belonging to this session.
	for _, w := range m.windows {
		if w.session == oldName {
			w.session = newName
		}
	}
	if m.currentSessionSet && m.currentSession == oldName {
		m.currentSession = newName
	}
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
	if m.OnKill != nil {
		m.OnKill("KillWindow", windowID)
	}
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

func (m *Mock) UnsetWindowOption(windowID string, option string) error {
	m.record("UnsetWindowOption", windowID, option)
	w, ok := m.windows[windowID]
	if !ok {
		return fmt.Errorf("window %q not found", windowID)
	}
	delete(w.options, option)
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

func (m *Mock) WaitingWindowIDs(session string) (map[string]bool, error) {
	m.record("WaitingWindowIDs", session)
	if !m.sessions[session] {
		return nil, fmt.Errorf("session %q not found", session)
	}
	result := make(map[string]bool)
	for id, w := range m.windows {
		if w.session == session {
			if val, ok := w.options["@bay-waiting"]; ok && val == "1" {
				result[id] = true
			}
		}
	}
	return result, nil
}

func (m *Mock) BellWindowIDs(session string) (map[string]bool, error) {
	m.record("BellWindowIDs", session)
	if !m.sessions[session] {
		return nil, fmt.Errorf("session %q not found", session)
	}
	result := make(map[string]bool)
	for id, w := range m.windows {
		if w.session == session {
			if val, ok := w.options["@bay-bell"]; ok && val == "1" {
				result[id] = true
			}
		}
	}
	return result, nil
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

func (m *Mock) SplitWindow(targetID string, dir string, cwd string) (string, error) {
	m.record("SplitWindow", targetID, dir, cwd)

	windowID := targetID
	insertAt := -1
	if p, ok := m.panes[targetID]; ok {
		windowID = p.windowID
		if w, ok := m.windows[windowID]; ok {
			for i, paneID := range w.panes {
				if paneID == targetID {
					insertAt = i + 1
					break
				}
			}
		}
	}

	w, ok := m.windows[windowID]
	if !ok {
		return "", fmt.Errorf("target %q not found", targetID)
	}
	paneID := m.nextPaneID()
	p := &mockPane{
		id:       paneID,
		windowID: windowID,
		pid:      m.nextPID(),
		active:   false,
	}
	if insertAt >= 0 && insertAt <= len(w.panes) {
		w.panes = append(w.panes, "")
		copy(w.panes[insertAt+1:], w.panes[insertAt:])
		w.panes[insertAt] = paneID
	} else {
		w.panes = append(w.panes, paneID)
	}
	m.panes[paneID] = p
	return paneID, nil
}

func (m *Mock) SelectPane(paneID string) error {
	m.record("SelectPane", paneID)
	if _, ok := m.panes[paneID]; !ok {
		return fmt.Errorf("pane %q not found", paneID)
	}
	return nil
}

func (m *Mock) KillPane(paneID string) error {
	m.record("KillPane", paneID)
	if m.OnKill != nil {
		m.OnKill("KillPane", paneID)
	}
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

func (m *Mock) PaneExists(paneID string) (bool, error) {
	m.record("PaneExists", paneID)
	_, ok := m.panes[paneID]
	return ok, nil
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

func (m *Mock) CurrentPaneID() (string, error) {
	m.record("CurrentPaneID")
	if !m.currentPaneIDSet {
		return "", fmt.Errorf("not in a tmux pane")
	}
	return m.currentPaneID, nil
}

// --- Client display ---

func (m *Mock) DisplayMessage(msg string, durationMs int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, Call{Method: "DisplayMessage", Args: []string{msg, strconv.Itoa(durationMs)}})
	m.displayMessages = append(m.displayMessages, msg)
	return nil
}

func (m *Mock) FindDockEditorWindow(session string) (string, bool) {
	m.record("FindDockEditorWindow", session)
	return "", false
}

func (m *Mock) MoveWindow(windowID string, targetIndex int) error {
	m.record("MoveWindow", windowID, fmt.Sprintf("%d", targetIndex))
	return nil
}

func (m *Mock) MoveWindowAfter(windowID string, afterWindowID string) error {
	m.record("MoveWindowAfter", windowID, afterWindowID)
	return nil
}

func (m *Mock) DisplayPopup(cmd string) error {
	m.record("DisplayPopup", cmd)
	return nil
}

func (m *Mock) ClientWidth() (int, error) {
	m.record("ClientWidth")
	if m.clientWidth <= 0 {
		return 100, nil
	}
	return m.clientWidth, nil
}

func (m *Mock) StatusReservedCells() (int, error) {
	m.record("StatusReservedCells")
	if m.statusReservedCells <= 0 {
		return 50, nil
	}
	return m.statusReservedCells, nil
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

// SetCurrentPaneID sets the value returned by CurrentPaneID.
func (m *Mock) SetCurrentPaneID(id string) {
	m.currentPaneID = id
	m.currentPaneIDSet = true
}

// SetCaptureContent sets the content that CapturePane will return for a pane.
func (m *Mock) SetCaptureContent(paneID string, content string) {
	if p, ok := m.panes[paneID]; ok {
		p.capture = content
	}
}

// SetClientWidth sets the value returned by ClientWidth.
func (m *Mock) SetClientWidth(w int) {
	m.clientWidth = w
}

// SetStatusReservedCells sets the value returned by StatusReservedCells.
func (m *Mock) SetStatusReservedCells(c int) {
	m.statusReservedCells = c
}

// DisplayMessages returns a copy of the messages recorded via DisplayMessage,
// in call order.
func (m *Mock) DisplayMessages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.displayMessages))
	copy(out, m.displayMessages)
	return out
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
	m.currentPaneIDSet = false
	m.clientWidth = 0
	m.displayMessages = nil
}

// HasSessionCalled returns true if NewSession was called with the given name
// (i.e. the session currently exists).
func (m *Mock) HasSessionCalled(name string) bool {
	return m.sessions[name]
}
