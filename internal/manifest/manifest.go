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

	"github.com/commontoolsinc/bay/internal/config"
)

// CurrentVersion is the manifest schema version.
const CurrentVersion = 3

// WorkspaceType constants.
const (
	WorkspaceTypeWorktree WorkspaceType = "worktree"
	WorkspaceTypeExternal WorkspaceType = "external"
)

// SurfaceType constants — the semantic role of a surface.
const (
	SurfaceTypeAgent  SurfaceType = "agent"
	SurfaceTypeEditor SurfaceType = "editor"
	SurfaceTypeShell  SurfaceType = "shell"
	SurfaceTypeCmd    SurfaceType = "cmd"
)

// SyncStatus constants — workspace/surface sync health.
type SyncStatus = string

const (
	SyncStatusOK      SyncStatus = "ok"
	SyncStatusStale   SyncStatus = "stale"
	SyncStatusMissing SyncStatus = "missing"
)

// SurfaceBackend constants — how bay interacts with a surface.
const (
	SurfaceBackendTmux SurfaceBackend = "tmux-pane"
	SurfaceBackendGUI  SurfaceBackend = "gui-app"
)

type WorkspaceType string
type SurfaceType string
type SurfaceBackend string

// Repo represents a managed git repository.
type Repo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	WorktreeDir string `json:"worktree_dir,omitempty"`
}

// EffectiveWorktreeDir returns the worktree directory, defaulting to {path}-worktrees.
func (r Repo) EffectiveWorktreeDir() string {
	if r.WorktreeDir != "" {
		return config.ExpandPath(r.WorktreeDir)
	}
	return config.ExpandPath(r.Path) + "-worktrees"
}

// Manifest is the top-level structure persisted as JSON.
type Manifest struct {
	Version int    `json:"version"`
	Repos   []Repo `json:"repos"`
	Docks   []Dock `json:"docks"`
}

// Dock represents a tmux session and its associated terminal window.
type Dock struct {
	Name       string              `json:"name"`                 // unique; matches config key and tmux session name
	Repo       string              `json:"repo,omitempty"`       // default repo for workspaces
	Agent      string              `json:"agent,omitempty"`      // default agent
	AgentArgs  map[string][]string `json:"agent_args,omitempty"` // per-agent args
	Host       *GUIAttrs           `json:"host,omitempty"`       // terminal window hosting this dock's tmux session; nil if unmanaged
	Surfaces   []Surface           `json:"surfaces,omitempty"`   // dock-level surfaces (e.g., dock-scoped editor)
	Workspaces []Workspace         `json:"workspaces"`
}

// Workspace represents a unit of work — typically one branch/PR.
type Workspace struct {
	Name           string         `json:"name"`                      // unique within dock; user-facing, renameable
	Type           WorkspaceType  `json:"type"`                      // "worktree" or "external"
	Path           string         `json:"path,omitempty"`            // absolute path to the working directory
	Description    string         `json:"description,omitempty"`     // short free-form label shown in picker/ls/tree
	NameOverridden bool           `json:"name_overridden,omitempty"` // true if user explicitly renamed
	LastFocused    int            `json:"last_focused,omitempty"`    // surface ID; 0 = none yet
	LastActive     int64          `json:"last_active,omitempty"`     // unix timestamp; updated by bay commands
	Surfaces       []Surface      `json:"surfaces"`
	Worktree       *WorktreeAttrs `json:"worktree,omitempty"` // type=worktree only

	// DeprecatedStatus exists only for v2→v3 manifest migration. Cleared after
	// migration. The "status" JSON tag is reserved by this field.
	DeprecatedStatus string `json:"status,omitempty"`
}

// WorktreeAttrs holds git worktree metadata. Only present for worktree workspaces.
type WorktreeAttrs struct {
	Repo        string `json:"repo"` // repo config key
	Branch      string `json:"branch"`
	PR          string `json:"pr,omitempty"`            // PR number (display-only)
	PRCheckedAt int64  `json:"pr_checked_at,omitempty"` // unix seconds of last gh pr view; 0 = never
	Merged      bool   `json:"merged,omitempty"`        // true when branch has been merged into default
}

// IsMerged reports whether this workspace's branch has been merged into the default branch.
func (ws *Workspace) IsMerged() bool {
	return ws.Worktree != nil && ws.Worktree.Merged
}

// PRCheckTTL is how long a "no PR found" answer stays valid before we re-query
// gh. Without this, opening a PR after the first check would never be picked
// up. 5 minutes balances responsiveness (matching the typical push→create-PR
// workflow) against gh API usage — once a PR is cached, NeedsPRCheck returns
// false and polling stops entirely, so this cost only applies to branches
// that haven't produced a PR yet.
const PRCheckTTL = 300

// NeedsPRCheck reports whether this worktree should have its PR number
// re-queried via gh. Returns true when:
//   - the worktree has a branch but no PR cached, AND
//   - we've never checked OR the last check is older than PRCheckTTL
//
// Callers must additionally verify the workspace path is non-empty.
func (w *WorktreeAttrs) NeedsPRCheck(now int64) bool {
	if w == nil || w.Branch == "" || w.PR != "" {
		return false
	}
	if w.PRCheckedAt > 0 && now-w.PRCheckedAt < PRCheckTTL {
		return false
	}
	return true
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
	LayoutGroup int    `json:"layout_group"`         // per-workspace, starting from 1; same value = same tmux window
	SplitFrom   int    `json:"split_from,omitempty"` // surface ID; 0 = first pane in layout group
	SplitDir    string `json:"split_dir,omitempty"`  // "h" or "v"; empty for first pane in group
}

// GUIAttrs holds state for a GUI application — either a surface or a dock host.
type GUIAttrs struct {
	AppCommand string `json:"app_command"`         // launch command (e.g. "cursor", "ghostty")
	BundleID   string `json:"bundle_id,omitempty"` // macOS bundle ID for activation
	PID        int    `json:"pid,omitempty"`       // ephemeral; for liveness probing; 0 = not tracked
}

// --- Constructor ---

// New returns an initialized empty manifest.
func New() *Manifest {
	return &Manifest{
		Version: CurrentVersion,
		Repos:   []Repo{},
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
	if m.Repos == nil {
		m.Repos = []Repo{}
	}
	if m.Docks == nil {
		m.Docks = []Dock{}
	}

	// Migrate v2 → v3: convert status=done to Worktree.Merged=true.
	if m.Version < 3 {
		for i := range m.Docks {
			for j := range m.Docks[i].Workspaces {
				ws := &m.Docks[i].Workspaces[j]
				if ws.DeprecatedStatus == "done" && ws.Worktree != nil {
					ws.Worktree.Merged = true
				}
				ws.DeprecatedStatus = ""
			}
		}
		m.Version = CurrentVersion
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
// Save writes the manifest to disk. Before overwriting, backs up the
// current version to the backups/ directory (at most once per minute).
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

	// Rolling backup before overwriting.
	BackupIfNeeded(path)

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
	return LockedUpdateMaybe(path, func(m *Manifest) (bool, error) {
		if err := fn(m); err != nil {
			return false, err
		}
		return true, nil
	})
}

// LockedUpdateMaybe atomically loads the manifest, calls fn, and saves only if fn reports changes.
func LockedUpdateMaybe(path string, fn func(m *Manifest) (bool, error)) error {
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

	changed, err := fn(m)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	// Rolling backup before overwriting.
	if readErr == nil {
		BackupIfNeeded(path)
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

// --- Repo operations ---

// FindRepo returns a pointer to the repo with the given name, or nil.
func (m *Manifest) FindRepo(name string) *Repo {
	for i := range m.Repos {
		if m.Repos[i].Name == name {
			return &m.Repos[i]
		}
	}
	return nil
}

// AddRepo adds a repo. Returns an error if a repo with the same name exists.
func (m *Manifest) AddRepo(r Repo) error {
	if m.FindRepo(r.Name) != nil {
		return fmt.Errorf("repo %q already exists", r.Name)
	}
	m.Repos = append(m.Repos, r)
	return nil
}

// RemoveRepo removes a repo by name.
func (m *Manifest) RemoveRepo(name string) error {
	for i := range m.Repos {
		if m.Repos[i].Name == name {
			m.Repos = append(m.Repos[:i], m.Repos[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("repo %q not found", name)
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

// FindDockSurface returns a pointer to a dock-level surface by name.
func (d *Dock) FindDockSurface(name string) *Surface {
	for i := range d.Surfaces {
		if d.Surfaces[i].Name == name {
			return &d.Surfaces[i]
		}
	}
	return nil
}

// AddDockSurface adds a dock-level surface with an auto-assigned ID.
func (d *Dock) AddDockSurface(s Surface) (int, error) {
	if d.FindDockSurface(s.Name) != nil {
		return 0, fmt.Errorf("dock surface %q already exists in dock %q", s.Name, d.Name)
	}
	max := 0
	for _, s := range d.Surfaces {
		if s.ID > max {
			max = s.ID
		}
	}
	s.ID = max + 1
	d.Surfaces = append(d.Surfaces, s)
	return s.ID, nil
}

// RemoveDockSurface removes a dock-level surface by name.
func (d *Dock) RemoveDockSurface(name string) error {
	for i := range d.Surfaces {
		if d.Surfaces[i].Name == name {
			d.Surfaces = append(d.Surfaces[:i], d.Surfaces[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("dock surface %q not found in dock %q", name, d.Name)
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
