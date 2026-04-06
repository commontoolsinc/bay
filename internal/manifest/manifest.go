// Package manifest tracks all active docks, workspaces, and surfaces.
// The manifest is persisted as JSON at ~/.local/share/bay/manifest.json.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// CurrentVersion is the manifest schema version.
const CurrentVersion = 1

// WorkspaceType constants.
const (
	WorkspaceTypeWorktree WorkspaceType = "worktree"
	WorkspaceTypeExternal WorkspaceType = "external"
)

// WorkspaceStatus constants.
const (
	WorkspaceStatusIdle   WorkspaceStatus = "idle"
	WorkspaceStatusActive WorkspaceStatus = "active"
	WorkspaceStatusDone   WorkspaceStatus = "done"
)

// SurfaceType constants — the semantic role of a surface.
const (
	SurfaceTypeAgent  SurfaceType = "agent"
	SurfaceTypeEditor SurfaceType = "editor"
	SurfaceTypeShell  SurfaceType = "shell"
	SurfaceTypeCmd    SurfaceType = "cmd"
)

// SurfaceBackend constants — how bay interacts with a surface.
const (
	SurfaceBackendTmux SurfaceBackend = "tmux-pane"
	SurfaceBackendGUI  SurfaceBackend = "gui-app"
)

type WorkspaceType string
type WorkspaceStatus string
type SurfaceType string
type SurfaceBackend string

// Manifest is the top-level structure persisted as JSON.
type Manifest struct {
	Version int    `json:"version"`
	Docks   []Dock `json:"docks"`
}

// Dock represents a tmux session and its associated terminal window.
type Dock struct {
	Name       string      `json:"name"`       // unique; matches config key and tmux session name
	Host       *GUIAttrs   `json:"host,omitempty"` // terminal window hosting this dock's tmux session; nil if unmanaged
	Workspaces []Workspace `json:"workspaces"`
}

// Workspace represents a unit of work — typically one branch/PR.
type Workspace struct {
	Name           string          `json:"name"`                      // unique within dock; user-facing, renameable
	Type           WorkspaceType   `json:"type"`                      // "worktree" or "external"
	Path           string          `json:"path,omitempty"`            // absolute path to the working directory
	Status         WorkspaceStatus `json:"status"`                    // "idle", "active", or "done"
	NameOverridden bool            `json:"name_overridden,omitempty"` // true if user explicitly renamed
	LastFocused    int             `json:"last_focused,omitempty"`    // surface ID; 0 = none yet
	Surfaces       []Surface       `json:"surfaces"`
	Worktree       *WorktreeAttrs  `json:"worktree,omitempty"` // type=worktree only
}

// WorktreeAttrs holds git worktree metadata. Only present for worktree workspaces.
type WorktreeAttrs struct {
	Repo   string `json:"repo"`            // repo config key
	Branch string `json:"branch"`
	PR     string `json:"pr,omitempty"`    // PR number (display-only)
}

// Surface is the unit of navigation — anything you can focus and jump to.
type Surface struct {
	ID      int            `json:"id"`      // unique within workspace, stable across renames, never reused
	Name    string         `json:"name"`    // unique within workspace; used for fuzzy nav
	Type    SurfaceType    `json:"type"`    // semantic role: agent, editor, shell, cmd
	Backend SurfaceBackend `json:"backend"` // interaction model: tmux-pane, gui-app

	// Type-specific. Nil when the type doesn't use them.
	Agent   *string `json:"agent,omitempty"`   // type=agent: agent config key
	Command *string `json:"command,omitempty"` // type=cmd: shell command to execute

	// Backend-specific. Exactly one non-nil, matching Backend.
	Tmux *TmuxAttrs `json:"tmux,omitempty"` // backend=tmux-pane
	GUI  *GUIAttrs  `json:"gui,omitempty"`  // backend=gui-app
}

// TmuxAttrs holds state for a tmux-backed surface.
type TmuxAttrs struct {
	// Ephemeral — populated at creation, cleared and repopulated on recover.
	PaneID   string `json:"pane_id,omitempty"`
	WindowID string `json:"window_id,omitempty"`

	// Stable — survives recover.
	LayoutGroup int    `json:"layout_group"`           // per-workspace, starting from 1; same value = same tmux window
	SplitFrom   int    `json:"split_from,omitempty"`   // surface ID; 0 = first pane in layout group
	SplitDir    string `json:"split_dir,omitempty"`    // "h" or "v"; empty for first pane in group
}

// GUIAttrs holds state for a GUI application — either a surface or a dock host.
type GUIAttrs struct {
	AppCommand string `json:"app_command"`          // launch command (e.g. "cursor", "ghostty")
	BundleID   string `json:"bundle_id,omitempty"`  // macOS bundle ID for activation
	PID        int    `json:"pid,omitempty"`         // ephemeral; for liveness probing; 0 = not tracked
}

// --- Constructor ---

// New returns an initialized empty manifest.
func New() *Manifest {
	return &Manifest{
		Version: CurrentVersion,
		Docks:   []Dock{},
	}
}

// --- File I/O ---

// Parse decodes JSON data into a Manifest.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}
	if m.Docks == nil {
		m.Docks = []Dock{}
	}
	return &m, nil
}

// Load reads and parses a manifest file.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	return Parse(data)
}

// Save writes the manifest to a file with file locking.
// Creates a .bak backup of the previous version if one exists.
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

	// Backup existing file before overwriting.
	if _, statErr := os.Stat(path); statErr == nil {
		backupPath := path + ".bak"
		if copyErr := copyFile(path, backupPath); copyErr != nil {
			return fmt.Errorf("creating backup: %w", copyErr)
		}
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}

	// Atomic write: write to temp, then rename.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming manifest: %w", err)
	}
	return nil
}

// LockedUpdate atomically loads the manifest, calls fn for modifications, and saves.
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

	var m *Manifest
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			m = New()
		} else {
			return fmt.Errorf("reading manifest: %w", readErr)
		}
	} else {
		m, err = Parse(data)
		if err != nil {
			return err
		}
	}

	if err := fn(m); err != nil {
		return err
	}

	// Backup existing file before overwriting.
	if readErr == nil {
		backupPath := path + ".bak"
		if copyErr := copyFile(path, backupPath); copyErr != nil {
			return fmt.Errorf("creating backup: %w", copyErr)
		}
	}

	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}

	// Atomic write: write to temp, then rename.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, encoded, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming manifest: %w", err)
	}
	return nil
}

// LoadArchive reads the archive file.
func LoadArchive(path string) (*Manifest, error) {
	return Load(path)
}

// SaveArchive writes the archive file with locking.
func SaveArchive(path string, m *Manifest) error {
	return Save(path, m)
}

// AllWorkspaces returns all workspaces across all docks, sorted by dock then workspace name.
func AllWorkspaces(m *Manifest) []WorkspaceRef {
	var refs []WorkspaceRef
	for i := range m.Docks {
		dock := &m.Docks[i]
		for j := range dock.Workspaces {
			refs = append(refs, WorkspaceRef{
				Dock:      dock.Name,
				Workspace: &dock.Workspaces[j],
			})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Dock != refs[j].Dock {
			return refs[i].Dock < refs[j].Dock
		}
		return refs[i].Workspace.Name < refs[j].Workspace.Name
	})
	return refs
}

// WorkspaceRef is a reference to a workspace within its dock.
type WorkspaceRef struct {
	Dock      string
	Workspace *Workspace
}

// --- Dock operations ---

// FindDock returns a pointer to the dock with the given name, or nil.
func (m *Manifest) FindDock(name string) *Dock {
	for i := range m.Docks {
		if m.Docks[i].Name == name {
			return &m.Docks[i]
		}
	}
	return nil
}

// AddDock adds a dock. Returns an error if a dock with the same name exists.
func (m *Manifest) AddDock(d Dock) error {
	if m.FindDock(d.Name) != nil {
		return fmt.Errorf("dock %q already exists", d.Name)
	}
	if d.Workspaces == nil {
		d.Workspaces = []Workspace{}
	}
	m.Docks = append(m.Docks, d)
	return nil
}

// RemoveDock removes a dock by name.
func (m *Manifest) RemoveDock(name string) error {
	for i := range m.Docks {
		if m.Docks[i].Name == name {
			m.Docks = append(m.Docks[:i], m.Docks[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("dock %q not found", name)
}

// --- Workspace operations ---

// FindWorkspace returns a pointer to the workspace with the given name, or nil.
func (d *Dock) FindWorkspace(name string) *Workspace {
	for i := range d.Workspaces {
		if d.Workspaces[i].Name == name {
			return &d.Workspaces[i]
		}
	}
	return nil
}

// AddWorkspace adds a workspace. Returns an error if the name is taken.
func (d *Dock) AddWorkspace(ws Workspace) error {
	if d.FindWorkspace(ws.Name) != nil {
		return fmt.Errorf("workspace %q already exists in dock %q", ws.Name, d.Name)
	}
	if ws.Surfaces == nil {
		ws.Surfaces = []Surface{}
	}
	d.Workspaces = append(d.Workspaces, ws)
	return nil
}

// RemoveWorkspace removes a workspace by name.
func (d *Dock) RemoveWorkspace(name string) error {
	for i := range d.Workspaces {
		if d.Workspaces[i].Name == name {
			d.Workspaces = append(d.Workspaces[:i], d.Workspaces[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("workspace %q not found in dock %q", name, d.Name)
}

// --- Surface operations ---

// NextSurfaceID returns the next surface ID (max+1) for the workspace.
func (ws *Workspace) NextSurfaceID() int {
	max := 0
	for _, s := range ws.Surfaces {
		if s.ID > max {
			max = s.ID
		}
	}
	return max + 1
}

// FindSurface returns a pointer to the surface with the given name, or nil.
func (ws *Workspace) FindSurface(name string) *Surface {
	for i := range ws.Surfaces {
		if ws.Surfaces[i].Name == name {
			return &ws.Surfaces[i]
		}
	}
	return nil
}

// FindSurfaceByID returns a pointer to the surface with the given ID, or nil.
func (ws *Workspace) FindSurfaceByID(id int) *Surface {
	for i := range ws.Surfaces {
		if ws.Surfaces[i].ID == id {
			return &ws.Surfaces[i]
		}
	}
	return nil
}

// AddSurface adds a surface with an auto-assigned ID. Returns the assigned ID.
func (ws *Workspace) AddSurface(s Surface) (int, error) {
	if ws.FindSurface(s.Name) != nil {
		return 0, fmt.Errorf("surface %q already exists in workspace %q", s.Name, ws.Name)
	}
	s.ID = ws.NextSurfaceID()
	ws.Surfaces = append(ws.Surfaces, s)
	return s.ID, nil
}

// RemoveSurface removes a surface by name.
func (ws *Workspace) RemoveSurface(name string) error {
	for i := range ws.Surfaces {
		if ws.Surfaces[i].Name == name {
			ws.Surfaces = append(ws.Surfaces[:i], ws.Surfaces[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("surface %q not found in workspace %q", name, ws.Name)
}

// --- Workspace resolution ---

// ResolveWorkspace resolves a query that may be "dock:name" or a bare name.
// Returns pointers to the workspace and its parent dock.
func (m *Manifest) ResolveWorkspace(query string) (*Workspace, *Dock, error) {
	// Try "dock:name" format.
	if parts := strings.SplitN(query, ":", 2); len(parts) == 2 {
		d := m.FindDock(parts[0])
		if d == nil {
			return nil, nil, fmt.Errorf("dock %q not found", parts[0])
		}
		ws := d.FindWorkspace(parts[1])
		if ws == nil {
			return nil, nil, fmt.Errorf("workspace %q not found in dock %q", parts[1], parts[0])
		}
		return ws, d, nil
	}

	// Bare name: search all docks.
	var matches []struct {
		ws   *Workspace
		dock *Dock
	}
	for i := range m.Docks {
		if ws := m.Docks[i].FindWorkspace(query); ws != nil {
			matches = append(matches, struct {
				ws   *Workspace
				dock *Dock
			}{ws, &m.Docks[i]})
		}
	}

	switch len(matches) {
	case 0:
		return nil, nil, fmt.Errorf("workspace %q not found", query)
	case 1:
		return matches[0].ws, matches[0].dock, nil
	default:
		var docks []string
		for _, match := range matches {
			docks = append(docks, match.dock.Name)
		}
		return nil, nil, fmt.Errorf("workspace %q is ambiguous; found in docks: %s", query, strings.Join(docks, ", "))
	}
}

// --- Validation ---

// Validate checks the surface for structural errors.
func (s *Surface) Validate() []string {
	var errs []string
	if s.Backend == SurfaceBackendTmux && s.Tmux == nil {
		errs = append(errs, fmt.Sprintf("surface %q: backend is %q but tmux attrs is nil", s.Name, s.Backend))
	}
	if s.Backend == SurfaceBackendGUI && s.GUI == nil {
		errs = append(errs, fmt.Sprintf("surface %q: backend is %q but gui attrs is nil", s.Name, s.Backend))
	}
	if s.Tmux != nil && s.GUI != nil {
		errs = append(errs, fmt.Sprintf("surface %q: both tmux and gui attrs are set", s.Name))
	}
	if s.Type == SurfaceTypeAgent && s.Agent == nil {
		errs = append(errs, fmt.Sprintf("surface %q: type is %q but agent is nil", s.Name, s.Type))
	}
	if s.Type == SurfaceTypeCmd && s.Command == nil {
		errs = append(errs, fmt.Sprintf("surface %q: type is %q but command is nil", s.Name, s.Type))
	}
	if s.Type != SurfaceTypeAgent && s.Agent != nil {
		errs = append(errs, fmt.Sprintf("surface %q: type is %q but agent is set", s.Name, s.Type))
	}
	if s.Type != SurfaceTypeCmd && s.Command != nil {
		errs = append(errs, fmt.Sprintf("surface %q: type is %q but command is set", s.Name, s.Type))
	}
	return errs
}

// --- Private helpers ---

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

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
