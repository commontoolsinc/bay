// Package manifest tracks all active workspaces, windows, and panes.
// The manifest lives at ~/.local/share/bay/manifest.toml.
package manifest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
)

// Workspace type constants.
const (
	WorkspaceTypeWorktree WorkspaceType = "worktree"
	WorkspaceTypeExternal WorkspaceType = "external"
)

// Workspace status constants.
const (
	WorkspaceStatusIdle   WorkspaceStatus = "idle"
	WorkspaceStatusActive WorkspaceStatus = "active"
	WorkspaceStatusDone   WorkspaceStatus = "done"
)

// Pane type constants.
const (
	PaneTypeAgent PaneType = "agent"
	PaneTypeShell PaneType = "shell"
	PaneTypeCmd   PaneType = "cmd"
)

// WorkspaceType is the type of a workspace ("worktree" or "external").
type WorkspaceType string

// WorkspaceStatus is the status of a workspace ("idle", "active", or "done").
type WorkspaceStatus string

// PaneType is the type of a pane ("agent", "shell", or "cmd").
type PaneType string

// Manifest is the top-level structure persisted to manifest.toml.
type Manifest struct {
	Docks map[string]*DockState `toml:"docks"`
}

// DockState holds all workspaces within a dock.
type DockState struct {
	Workspaces map[string]*Workspace `toml:"workspaces"`
}

// Workspace represents a managed working directory with metadata.
type Workspace struct {
	Name           string          `toml:"name"`
	Type           WorkspaceType   `toml:"type"`
	Repo           string          `toml:"repo,omitempty"`
	Path           string          `toml:"path,omitempty"`
	Branch         string          `toml:"branch,omitempty"`
	PR             string          `toml:"pr,omitempty"`
	Status         WorkspaceStatus `toml:"status"`
	NameOverridden bool            `toml:"name_overridden,omitempty"`
	Windows        []Window        `toml:"windows,omitempty"`
}

// LoadArchive reads the archive file. Returns os.IsNotExist-compatible error if missing.
func LoadArchive(path string) (*Manifest, error) {
	return Load(path)
}

// SaveArchive writes the archive file with locking.
func SaveArchive(path string, m *Manifest) error {
	return Save(path, m)
}

// Window represents a tmux window attached to a workspace.
type Window struct {
	ID           int    `toml:"id" json:"id"`
	TmuxWindowID string `toml:"tmux_window_id,omitempty" json:"tmux_window_id,omitempty"`
	Name         string `toml:"name" json:"name"`
	Panes        []Pane `toml:"panes,omitempty" json:"panes,omitempty"`
}

// Pane represents a tmux pane within a window.
type Pane struct {
	ID        int      `toml:"id" json:"id"`
	Type      PaneType `toml:"type" json:"type"`
	Agent     string   `toml:"agent,omitempty" json:"agent,omitempty"`
	Command   string   `toml:"command,omitempty" json:"command,omitempty"`
	SplitFrom int      `toml:"split_from,omitempty" json:"split_from,omitempty"`
	SplitDir  string   `toml:"split_dir,omitempty" json:"split_dir,omitempty"`
}

// New returns an initialized empty manifest.
func New() *Manifest {
	return &Manifest{
		Docks: make(map[string]*DockState),
	}
}

// Parse decodes TOML data into a Manifest.
func Parse(data string) (*Manifest, error) {
	var m Manifest
	if _, err := toml.Decode(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if m.Docks == nil {
		m.Docks = make(map[string]*DockState)
	}
	for _, dock := range m.Docks {
		if dock.Workspaces == nil {
			dock.Workspaces = make(map[string]*Workspace)
		}
	}
	return &m, nil
}

// Load reads and parses a manifest file.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	return Parse(string(data))
}

// Save writes the manifest to a file with file locking.
func Save(path string, m *Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating manifest dir: %w", err)
	}

	lockPath := path + ".lock"
	unlock, err := lockFile(lockPath)
	if err != nil {
		return fmt.Errorf("acquiring manifest lock: %w", err)
	}
	defer unlock()

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

// LockedUpdate atomically loads the manifest, calls fn for modifications, and saves.
// The entire read-modify-write cycle is protected by a file lock.
func LockedUpdate(path string, fn func(m *Manifest) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating manifest dir: %w", err)
	}

	lockPath := path + ".lock"
	unlock, err := lockFile(lockPath)
	if err != nil {
		return fmt.Errorf("acquiring manifest lock: %w", err)
	}
	defer unlock()

	// Load (without lock since we already hold it)
	var m *Manifest
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			m = New()
		} else {
			return fmt.Errorf("reading manifest: %w", readErr)
		}
	} else {
		m, err = Parse(string(data))
		if err != nil {
			return err
		}
	}

	// Modify
	if err := fn(m); err != nil {
		return err
	}

	// Save (without lock since we already hold it)
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(m); err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

// lockFile acquires an exclusive file lock. Returns an unlock function.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// NextWorkspaceID returns the next workspace ID (w{max+1}) for the given dock.
func NextWorkspaceID(dock *DockState) string {
	max := 0
	for id := range dock.Workspaces {
		if n, err := parseWorkspaceNum(id); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("w%d", max+1)
}

// NextWindowID returns the next window ID (max+1) for the given workspace.
func NextWindowID(ws *Workspace) int {
	max := 0
	for _, w := range ws.Windows {
		if w.ID > max {
			max = w.ID
		}
	}
	return max + 1
}

// NextPaneID returns the next pane ID (max+1) for the given window.
func NextPaneID(win *Window) int {
	max := 0
	for _, p := range win.Panes {
		if p.ID > max {
			max = p.ID
		}
	}
	return max + 1
}

// parseWorkspaceNum extracts the numeric part from a workspace ID like "w3".
func parseWorkspaceNum(id string) (int, error) {
	if !strings.HasPrefix(id, "w") {
		return 0, fmt.Errorf("invalid workspace id: %q", id)
	}
	return strconv.Atoi(id[1:])
}

// ensureDock ensures the dock exists in the manifest and returns it.
func (m *Manifest) ensureDock(dock string) *DockState {
	d, ok := m.Docks[dock]
	if !ok {
		d = &DockState{Workspaces: make(map[string]*Workspace)}
		m.Docks[dock] = d
	}
	return d
}

// AddWorkspace adds a workspace to the given dock with an auto-assigned ID.
// Returns the assigned workspace ID.
func (m *Manifest) AddWorkspace(dock string, ws *Workspace) (string, error) {
	d := m.ensureDock(dock)
	id := NextWorkspaceID(d)
	d.Workspaces[id] = ws
	return id, nil
}

// RemoveWorkspace removes a workspace from the given dock.
func (m *Manifest) RemoveWorkspace(dock, id string) error {
	d, ok := m.Docks[dock]
	if !ok {
		return fmt.Errorf("dock %q not found", dock)
	}
	if _, ok := d.Workspaces[id]; !ok {
		return fmt.Errorf("workspace %q not found in dock %q", id, dock)
	}
	delete(d.Workspaces, id)
	return nil
}

// GetWorkspace retrieves a workspace by "dock:id" or bare "wN" (if unambiguous).
func (m *Manifest) GetWorkspace(query string) (ws *Workspace, dock string, id string, err error) {
	if parts := strings.SplitN(query, ":", 2); len(parts) == 2 {
		dock, id = parts[0], parts[1]
		d, ok := m.Docks[dock]
		if !ok {
			return nil, "", "", fmt.Errorf("dock %q not found", dock)
		}
		ws, ok = d.Workspaces[id]
		if !ok {
			return nil, "", "", fmt.Errorf("workspace %q not found in dock %q", id, dock)
		}
		return ws, dock, id, nil
	}

	// Bare ID: search all docks.
	id = query
	var matches []struct {
		dock string
		ws   *Workspace
	}
	for dockName, d := range m.Docks {
		if w, ok := d.Workspaces[id]; ok {
			matches = append(matches, struct {
				dock string
				ws   *Workspace
			}{dockName, w})
		}
	}
	switch len(matches) {
	case 0:
		return nil, "", "", fmt.Errorf("workspace %q not found", query)
	case 1:
		return matches[0].ws, matches[0].dock, id, nil
	default:
		var docks []string
		for _, match := range matches {
			docks = append(docks, match.dock)
		}
		return nil, "", "", fmt.Errorf("workspace %q is ambiguous; found in docks: %s", id, strings.Join(docks, ", "))
	}
}

// GetWorkspaceByName retrieves a workspace by display name. Error if ambiguous.
func (m *Manifest) GetWorkspaceByName(name string) (ws *Workspace, dock string, id string, err error) {
	var matches []struct {
		dock string
		id   string
		ws   *Workspace
	}
	for dockName, d := range m.Docks {
		for wsID, w := range d.Workspaces {
			if w.Name == name {
				matches = append(matches, struct {
					dock string
					id   string
					ws   *Workspace
				}{dockName, wsID, w})
			}
		}
	}
	switch len(matches) {
	case 0:
		return nil, "", "", fmt.Errorf("workspace with name %q not found", name)
	case 1:
		return matches[0].ws, matches[0].dock, matches[0].id, nil
	default:
		var locs []string
		for _, match := range matches {
			locs = append(locs, match.dock+":"+match.id)
		}
		return nil, "", "", fmt.Errorf("workspace name %q is ambiguous; found at: %s", name, strings.Join(locs, ", "))
	}
}

// ResolveWorkspace resolves a query that may be "dock:id", bare "wN", or a name.
// Returns error if ambiguous or not found.
func (m *Manifest) ResolveWorkspace(query string) (ws *Workspace, dock string, id string, err error) {
	// Try dock:id first (contains colon).
	if strings.Contains(query, ":") {
		return m.GetWorkspace(query)
	}

	// Try as bare workspace ID (wN pattern).
	if _, parseErr := parseWorkspaceNum(query); parseErr == nil {
		return m.GetWorkspace(query)
	}

	// Try as name.
	return m.GetWorkspaceByName(query)
}

// findWindow returns a pointer to the window with the given ID, or nil.
func findWindow(ws *Workspace, windowID int) (int, *Window) {
	for i := range ws.Windows {
		if ws.Windows[i].ID == windowID {
			return i, &ws.Windows[i]
		}
	}
	return -1, nil
}

// AddWindow adds a window to the specified workspace. The window's ID is auto-assigned.
// Returns the assigned window ID.
func (m *Manifest) AddWindow(dock, wsID string, win Window) (int, error) {
	d, ok := m.Docks[dock]
	if !ok {
		return 0, fmt.Errorf("dock %q not found", dock)
	}
	ws, ok := d.Workspaces[wsID]
	if !ok {
		return 0, fmt.Errorf("workspace %q not found in dock %q", wsID, dock)
	}
	win.ID = NextWindowID(ws)
	ws.Windows = append(ws.Windows, win)
	return win.ID, nil
}

// RemoveWindow removes a window by ID from the specified workspace.
func (m *Manifest) RemoveWindow(dock, wsID string, windowID int) error {
	d, ok := m.Docks[dock]
	if !ok {
		return fmt.Errorf("dock %q not found", dock)
	}
	ws, ok := d.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %q not found in dock %q", wsID, dock)
	}
	idx, _ := findWindow(ws, windowID)
	if idx < 0 {
		return fmt.Errorf("window %d not found in workspace %q", windowID, wsID)
	}
	ws.Windows = append(ws.Windows[:idx], ws.Windows[idx+1:]...)
	return nil
}

// AddPane adds a pane to the specified window. The pane's ID is auto-assigned.
// Returns the assigned pane ID.
func (m *Manifest) AddPane(dock, wsID string, windowID int, pane Pane) (int, error) {
	d, ok := m.Docks[dock]
	if !ok {
		return 0, fmt.Errorf("dock %q not found", dock)
	}
	ws, ok := d.Workspaces[wsID]
	if !ok {
		return 0, fmt.Errorf("workspace %q not found in dock %q", wsID, dock)
	}
	idx, win := findWindow(ws, windowID)
	if idx < 0 {
		return 0, fmt.Errorf("window %d not found in workspace %q", windowID, wsID)
	}
	pane.ID = NextPaneID(win)
	win.Panes = append(win.Panes, pane)
	// Write back since we're working with a copy via findWindow pointer into the slice element.
	ws.Windows[idx] = *win
	return pane.ID, nil
}

// RemovePane removes a pane by ID from the specified window.
func (m *Manifest) RemovePane(dock, wsID string, windowID, paneID int) error {
	d, ok := m.Docks[dock]
	if !ok {
		return fmt.Errorf("dock %q not found", dock)
	}
	ws, ok := d.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %q not found in dock %q", wsID, dock)
	}
	idx, win := findWindow(ws, windowID)
	if idx < 0 {
		return fmt.Errorf("window %d not found in workspace %q", windowID, wsID)
	}
	found := false
	panes := win.Panes[:0]
	for _, p := range win.Panes {
		if p.ID == paneID {
			found = true
			continue
		}
		panes = append(panes, p)
	}
	if !found {
		return fmt.Errorf("pane %d not found in window %d", paneID, windowID)
	}
	win.Panes = panes
	ws.Windows[idx] = *win
	return nil
}


// WorkspaceRef is a reference to a workspace within its dock.
type WorkspaceRef struct {
	Dock      string
	ID        string
	Workspace *Workspace
}

// AllWorkspaces returns all workspaces across all docks, sorted by dock then ID.
func AllWorkspaces(m *Manifest) []WorkspaceRef {
	var refs []WorkspaceRef
	for dockName, dock := range m.Docks {
		for wsID, ws := range dock.Workspaces {
			refs = append(refs, WorkspaceRef{
				Dock:      dockName,
				ID:        wsID,
				Workspace: ws,
			})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Dock != refs[j].Dock {
			return refs[i].Dock < refs[j].Dock
		}
		return refs[i].ID < refs[j].ID
	})
	return refs
}
