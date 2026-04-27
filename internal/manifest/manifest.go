// Package manifest tracks all active docks, workspaces, and surfaces.
// The manifest is persisted as JSON at ~/.local/share/bay/manifest.json.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/commontoolsinc/bay/internal/config"
)

// CurrentVersion is the manifest schema version.
const CurrentVersion = 4

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
	Name          string              `json:"name"`                 // unique; matches config key and tmux session name
	Repo          string              `json:"repo,omitempty"`       // default repo for workspaces
	Agent         string              `json:"agent,omitempty"`      // default agent
	AgentArgs     map[string][]string `json:"agent_args,omitempty"` // per-agent args
	Host          *GUIAttrs           `json:"host,omitempty"`       // terminal window hosting this dock's tmux session; nil if unmanaged
	Surfaces      []Surface           `json:"surfaces,omitempty"`   // dock-level surfaces (e.g., dock-scoped editor)
	Workspaces    []Workspace         `json:"workspaces"`
	ClosedEntries []ClosedEntry       `json:"closed_entries,omitempty"` // undo-close queue (see docs/design/undo-close.md)
}

// Workspace represents a unit of work — typically one branch/PR.
type Workspace struct {
	ID             string         `json:"id"`                         // stable handle (^w[1-9]\d*$); set at creation, never changes; unique within dock
	Name           string         `json:"name"`                       // user-facing display label, renameable; not a CLI key. Empty until set explicitly or filled from a branch.
	Type           WorkspaceType  `json:"type"`                       // "worktree" or "external"
	Path           string         `json:"path,omitempty"`             // absolute path to the working directory
	Description    string         `json:"description,omitempty"`      // short free-form label shown in picker/ls/tree
	LastFocused    int            `json:"last_focused,omitempty"`     // surface ID; 0 = none yet
	LastActive     int64          `json:"last_active,omitempty"`      // unix timestamp; updated by bay commands
	PendingCloseAt int64          `json:"pending_close_at,omitempty"` // unix ts; non-zero = scheduled for auto-close at this time unless a surface is re-added first
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

// IsWorkspaceID reports whether s matches the canonical workspace ID
// pattern: lowercase 'w' followed by a positive integer with no leading
// zeros. The pattern is reserved — workspace Names cannot match it,
// which keeps the ID and Name namespaces disjoint.
func IsWorkspaceID(s string) bool {
	_, ok := parseWorkspaceIDNum(s)
	return ok
}

// parseWorkspaceIDNum extracts the integer suffix from a workspace ID.
// Returns (n, true) for valid IDs (n >= 1), (0, false) otherwise.
func parseWorkspaceIDNum(s string) (int, bool) {
	if len(s) < 2 || s[0] != 'w' {
		return 0, false
	}
	rest := s[1:]
	// Reject any non-digit lead — strconv.Atoi accepts +/- prefixes,
	// but those aren't canonical workspace IDs.
	if rest[0] < '0' || rest[0] > '9' {
		return 0, false
	}
	if len(rest) > 1 && rest[0] == '0' { // reject leading zeros (canonical)
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// AssignWorkspaceIDs fills in IDs for any workspace in the dock whose ID
// is currently empty. Two passes after an initial scan:
//
//  1. Claim the path basename as ID if it matches the canonical pattern
//     and isn't already taken in this dock. This preserves continuity
//     for legacy manifests where path basenames are already w1/w2/...
//  2. Assign next-sequential w<N> for any workspace still without an ID,
//     where N is one above the highest used in the dock.
//
// Sequential IDs (pass 2) always fall above claimed-basename IDs (pass 1),
// which keeps externals from stealing low IDs out from under worktrees
// when dock ordering is mixed. The three-pass structure (scan → claim →
// sequential) is required to preserve that invariant: merging scan into
// claim risks one workspace claiming an ID a later workspace already
// holds explicitly, and merging claim into sequential lets a sequential
// fill grab a low ID before another workspace's basename can claim it.
//
// On collisions in pass 1 (two workspaces sharing the same w<N> path
// basename), the workspace appearing first in dock.Workspaces wins; the
// second falls through to pass 2. Iteration order is the manifest's
// stored order, so the result is deterministic across runs.
//
// Idempotent: a workspace with a non-empty ID is left alone, and the
// function returns immediately when no fill is needed.
func AssignWorkspaceIDs(dock *Dock) {
	needsFill := false
	for i := range dock.Workspaces {
		if dock.Workspaces[i].ID == "" {
			needsFill = true
			break
		}
	}
	if !needsFill {
		return
	}

	used := map[string]bool{}
	maxN := 0
	for i := range dock.Workspaces {
		id := dock.Workspaces[i].ID
		if id == "" {
			continue
		}
		used[id] = true
		if n, ok := parseWorkspaceIDNum(id); ok && n > maxN {
			maxN = n
		}
	}
	for i := range dock.Workspaces {
		ws := &dock.Workspaces[i]
		if ws.ID != "" || ws.Path == "" {
			continue
		}
		base := filepath.Base(ws.Path)
		n, ok := parseWorkspaceIDNum(base)
		if !ok || used[base] {
			continue
		}
		ws.ID = base
		used[base] = true
		if n > maxN {
			maxN = n
		}
	}
	for i := range dock.Workspaces {
		ws := &dock.Workspaces[i]
		if ws.ID != "" {
			continue
		}
		maxN++
		ws.ID = fmt.Sprintf("w%d", maxN)
		used[ws.ID] = true
	}
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

// ClosedEntryKind is the kind tag for a ClosedEntry.
type ClosedEntryKind string

const (
	ClosedKindSurface ClosedEntryKind = "surface"
	// ClosedKindWorkspace is reserved for Step 2 of undo-close.
)

// ClosedQueueMax caps the number of undo-close entries retained per dock.
const ClosedQueueMax = 10

// ClosedQueueTTLSeconds is the wall-clock retention window for undo-close
// entries. Entries older than this are pruned on next queue access.
// 1 hour matches the Chrome-style muscle-memory case (accidental closes
// are noticed and reversed within seconds to minutes) without allowing
// stale entries to ambush the user hours later.
const ClosedQueueTTLSeconds = 60 * 60

// ClosedEntry is a single record in the undo-close queue.
type ClosedEntry struct {
	ClosedAt int64           `json:"closed_at"` // unix seconds; used for TTL expiry
	Kind     ClosedEntryKind `json:"kind"`
	Surface  *ClosedSurface  `json:"surface,omitempty"` // Kind == ClosedKindSurface
}

// ClosedSurface records enough state to recreate a closed surface in its
// parent workspace. Transient fields (PaneID, WindowID, ID) are deliberately
// omitted — they're reassigned on restore.
type ClosedSurface struct {
	Workspace   string      `json:"workspace"`              // parent workspace ID at close time (the stable handle)
	Name        string      `json:"name"`                   // user-facing surface name
	Type        SurfaceType `json:"type"`                   // agent / shell / cmd / editor
	Agent       string      `json:"agent,omitempty"`        // type=agent
	Command     string      `json:"command,omitempty"`      // type=cmd or type=editor
	SplitDir    string      `json:"split_dir,omitempty"`    // "" = root pane, "h"/"v" = split
	LayoutGroup int         `json:"layout_group,omitempty"` // original tmux window membership; restore re-joins siblings if any survive
}

// PushClosedEntry appends entry to the dock's undo-close queue, enforcing
// the per-dock ClosedQueueMax cap by dropping the oldest entry when full.
func (d *Dock) PushClosedEntry(entry ClosedEntry) {
	d.ClosedEntries = append(d.ClosedEntries, entry)
	if len(d.ClosedEntries) > ClosedQueueMax {
		d.ClosedEntries = d.ClosedEntries[len(d.ClosedEntries)-ClosedQueueMax:]
	}
}

// PruneClosedEntries drops entries whose ClosedAt is older than
// now - ClosedQueueTTLSeconds. Returns true if anything was removed.
func (d *Dock) PruneClosedEntries(now int64) bool {
	cutoff := now - ClosedQueueTTLSeconds
	before := len(d.ClosedEntries)
	kept := d.ClosedEntries[:0]
	for _, e := range d.ClosedEntries {
		if e.ClosedAt > cutoff {
			kept = append(kept, e)
		}
	}
	d.ClosedEntries = kept
	return len(d.ClosedEntries) != before
}

// PeekClosedEntry prunes stale entries and returns a deep copy of the most
// recent live entry, or nil if the queue is empty. The entry is not removed
// — callers use RemoveClosedEntryAt after confirming the restore succeeded,
// so a failure leaves the entry queued for retry. The deep copy guarantees
// callers can't accidentally mutate queue state through the returned
// Surface pointer.
func (d *Dock) PeekClosedEntry(now int64) *ClosedEntry {
	d.PruneClosedEntries(now)
	if len(d.ClosedEntries) == 0 {
		return nil
	}
	entry := d.ClosedEntries[len(d.ClosedEntries)-1]
	if entry.Surface != nil {
		surfaceCopy := *entry.Surface
		entry.Surface = &surfaceCopy
	}
	return &entry
}

// RemoveClosedEntryAt removes the entry with the given ClosedAt timestamp.
// Matching by timestamp (not index) lets Peek/Remove survive interleaved
// pushes between the two calls — if another close happens during restore,
// we still drop the entry we restored, not the newly-pushed one.
func (d *Dock) RemoveClosedEntryAt(closedAt int64) bool {
	for i := len(d.ClosedEntries) - 1; i >= 0; i-- {
		if d.ClosedEntries[i].ClosedAt == closedAt {
			d.ClosedEntries = append(d.ClosedEntries[:i], d.ClosedEntries[i+1:]...)
			return true
		}
	}
	return false
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

	// Fill in workspace IDs. Runs unconditionally:
	//   - For pre-v4 manifests, workspaces have no ID field set.
	//   - Defensive: if any workspace was created without an ID (e.g.,
	//     during the rollout window before engine wires up ID assignment
	//     at creation), Parse fills it in here so callers can rely on
	//     ws.ID being non-empty.
	for i := range m.Docks {
		AssignWorkspaceIDs(&m.Docks[i])
	}
	if m.Version < 4 {
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

// FindWorkspace returns the first workspace with a matching non-empty Name.
// Empty Names always return nil — multiple workspaces may legitimately have
// no Name set (display falls back to ID), so an empty-string lookup is not
// a useful identification query.
func (d *Dock) FindWorkspace(name string) *Workspace {
	if name == "" {
		return nil
	}
	for i := range d.Workspaces {
		if d.Workspaces[i].Name == name {
			return &d.Workspaces[i]
		}
	}
	return nil
}

// FindWorkspaceByID returns the workspace with the matching ID, or nil.
// IDs are unique within a dock and never reused, so this is the canonical
// lookup once the resolver moves off Name.
func (d *Dock) FindWorkspaceByID(id string) *Workspace {
	if id == "" {
		return nil
	}
	for i := range d.Workspaces {
		if d.Workspaces[i].ID == id {
			return &d.Workspaces[i]
		}
	}
	return nil
}

// AddWorkspace adds a workspace. Returns an error if the workspace's Name
// is non-empty and already taken in the dock. Empty-Name workspaces are
// always allowed; they'll display via their ID until a Name is set.
func (d *Dock) AddWorkspace(ws Workspace) error {
	if ws.Name != "" && d.FindWorkspace(ws.Name) != nil {
		return fmt.Errorf("workspace %q already exists in dock %q", ws.Name, d.Name)
	}
	if ws.Surfaces == nil {
		ws.Surfaces = []Surface{}
	}
	d.Workspaces = append(d.Workspaces, ws)
	return nil
}

// RemoveWorkspace removes a workspace identified by ID. (Names are no
// longer CLI keys; the engine layer passes the ID it received from the
// resolver.)
func (d *Dock) RemoveWorkspace(id string) error {
	for i := range d.Workspaces {
		if d.Workspaces[i].ID == id {
			d.Workspaces = append(d.Workspaces[:i], d.Workspaces[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("workspace %q not found in dock %q", id, d.Name)
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

// ResolveWorkspace resolves a query that may be "dock:id" or a bare ID.
// Returns pointers to the workspace and its parent dock.
//
// Strict resolution: only IDs are accepted as CLI keys. If the query
// looks like a Name (i.e. it doesn't match a known ID), the error
// message hints at the canonical ID for any workspace with a matching
// Name, so users who type a friendly Name see a one-step fix.
func (m *Manifest) ResolveWorkspace(query string) (*Workspace, *Dock, error) {
	// Try "dock:id" format.
	if parts := strings.SplitN(query, ":", 2); len(parts) == 2 {
		d := m.FindDock(parts[0])
		if d == nil {
			return nil, nil, fmt.Errorf("dock %q not found", parts[0])
		}
		ws := d.FindWorkspaceByID(parts[1])
		if ws == nil {
			return nil, nil, workspaceNotFoundError(parts[1], parts[0], dockNameHints(d, parts[1]))
		}
		return ws, d, nil
	}

	// Bare ID: search all docks.
	var matches []struct {
		ws   *Workspace
		dock *Dock
	}
	for i := range m.Docks {
		if ws := m.Docks[i].FindWorkspaceByID(query); ws != nil {
			matches = append(matches, struct {
				ws   *Workspace
				dock *Dock
			}{ws, &m.Docks[i]})
		}
	}

	switch len(matches) {
	case 0:
		return nil, nil, workspaceNotFoundError(query, "", manifestNameHints(m, query))
	case 1:
		return matches[0].ws, matches[0].dock, nil
	default:
		var docks []string
		for _, match := range matches {
			docks = append(docks, match.dock.Name)
		}
		return nil, nil, fmt.Errorf("workspace ID %q is ambiguous; found in docks: %s", query, strings.Join(docks, ", "))
	}
}

// nameHint pairs an ID with its parent dock for "did you mean" output.
type nameHint struct {
	id   string
	dock string
}

// dockNameHints returns IDs of workspaces in d whose Name equals query.
func dockNameHints(d *Dock, query string) []nameHint {
	var hints []nameHint
	for i := range d.Workspaces {
		if d.Workspaces[i].Name == query {
			hints = append(hints, nameHint{id: d.Workspaces[i].ID, dock: d.Name})
		}
	}
	return hints
}

// manifestNameHints returns IDs of workspaces across all docks whose
// Name equals query.
func manifestNameHints(m *Manifest, query string) []nameHint {
	var hints []nameHint
	for i := range m.Docks {
		hints = append(hints, dockNameHints(&m.Docks[i], query)...)
	}
	return hints
}

// workspaceNotFoundError formats a "workspace not found" error,
// optionally adding a "did you mean" pointer at the canonical ID for
// any workspace whose Name matched the query.
func workspaceNotFoundError(query, dockName string, hints []nameHint) error {
	base := fmt.Sprintf("workspace %q not found", query)
	if dockName != "" {
		base = fmt.Sprintf("workspace %q not found in dock %q", query, dockName)
	}
	switch len(hints) {
	case 0:
		return fmt.Errorf("%s", base)
	case 1:
		return fmt.Errorf("%s; did you mean %q? (Names are not CLI keys; use the ID)", base, hints[0].id)
	default:
		parts := make([]string, len(hints))
		for i, h := range hints {
			parts[i] = fmt.Sprintf("%q (%s)", h.id, h.dock)
		}
		return fmt.Errorf("%s; did you mean one of: %s? (Names are not CLI keys; use the ID)", base, strings.Join(parts, ", "))
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
