// Package nav provides fuzzy workspace navigation for "bay go" and "bay ws go".
package nav

import (
	"sort"
	"strings"

	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

// Entry represents a navigable workspace.
type Entry struct {
	DockName string
	WsName   string
	Branch   string
	PR       string
	Pending  bool
	Waiting  bool
	// TmuxWindowID of any surface in this workspace (for focusing).
	TmuxWindowID string
	SurfaceCount int
}

// CollectEntries builds a list of all navigation entries from the manifest.
// Each workspace becomes one entry.
func CollectEntries(m *manifest.Manifest, tc tmux.Interface) []Entry {
	var entries []Entry

	// Sort docks for deterministic output.
	docks := make([]string, 0, len(m.Docks))
	for i := range m.Docks {
		docks = append(docks, m.Docks[i].Name)
	}
	sort.Strings(docks)

	for _, dockName := range docks {
		dock := m.FindDock(dockName)
		if dock == nil {
			continue
		}

		// Batch: get all waiting windows for this dock in one tmux
		// call instead of one GetWindowOption per surface.
		waitingWindows, _ := tc.WaitingWindowIDs(dockName)

		for i := range dock.Workspaces {
			ws := &dock.Workspaces[i]

			branch := ""
			pr := ""
			if ws.Worktree != nil {
				branch = ws.Worktree.Branch
				pr = ws.Worktree.PR
			}

			var tmuxWindowID string
			waiting := false
			for _, s := range ws.Surfaces {
				if s.Tmux != nil && s.Tmux.WindowID != "" {
					if tmuxWindowID == "" {
						tmuxWindowID = s.Tmux.WindowID
					}
					if waitingWindows[s.Tmux.WindowID] {
						waiting = true
						// Prefer the waiting window so next-waiting
						// jumps directly to it, not a sibling.
						tmuxWindowID = s.Tmux.WindowID
					}
				}
			}

			entries = append(entries, Entry{
				DockName:     dockName,
				WsName:       ws.Name,
				Branch:       branch,
				PR:           pr,
				Pending:      ws.Worktree != nil && ws.Worktree.Branch != "" && !ws.IsMerged(),
				TmuxWindowID: tmuxWindowID,
				Waiting:      waiting,
				SurfaceCount: len(ws.Surfaces),
			})
		}
	}
	return entries
}

// FuzzyMatch filters entries by case-insensitive substring match.
func FuzzyMatch(entries []Entry, query string) []Entry {
	if query == "" {
		return entries
	}
	q := strings.ToLower(query)
	var matched []Entry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.WsName), q) ||
			strings.Contains(strings.ToLower(e.Branch), q) ||
			strings.Contains(strings.ToLower(e.PR), q) ||
			strings.Contains(strings.ToLower(e.DockName), q) {
			matched = append(matched, e)
		}
	}
	return matched
}

// FilterWaiting returns only entries with Waiting=true.
func FilterWaiting(entries []Entry) []Entry {
	var waiting []Entry
	for _, e := range entries {
		if e.Waiting {
			waiting = append(waiting, e)
		}
	}
	return waiting
}

// NextWaiting returns the next waiting entry after the given current window
// (and its index in entries). Returns (nil, -1) when no entry is waiting.
func NextWaiting(entries []Entry, currentWindowID string) (*Entry, int) {
	currentIdx := -1
	for i, e := range entries {
		if e.TmuxWindowID == currentWindowID {
			currentIdx = i
			break
		}
	}

	n := len(entries)
	if currentIdx == -1 {
		// No matching current window: return the first waiting entry.
		for i, e := range entries {
			if e.Waiting {
				return &entries[i], i
			}
		}
		return nil, -1
	}

	for offset := 1; offset <= n; offset++ {
		idx := (currentIdx + offset) % n
		if entries[idx].Waiting {
			return &entries[idx], idx
		}
	}
	return nil, -1
}

// --- Surface-level navigation (intra-workspace) ---

// SurfaceEntry represents a navigable surface within a workspace.
type SurfaceEntry struct {
	ID       int
	Name     string
	Type     string // "agent", "editor", "shell", "cmd"
	PaneID   string // tmux pane ID
	WindowID string // tmux window ID
	Waiting  bool
	Current  bool // true if this is the currently focused surface
}

// CollectSurfaces builds a list of surface entries for a workspace.
// waitingWindows is a pre-computed set of window IDs with @bay-waiting=1,
// obtained from a single tc.WaitingWindowIDs call by the caller.
func CollectSurfaces(ws *manifest.Workspace, currentPaneID string, waitingWindows map[string]bool) []SurfaceEntry {
	var entries []SurfaceEntry
	for _, s := range ws.Surfaces {
		e := SurfaceEntry{
			ID:   s.ID,
			Name: s.Name,
			Type: string(s.Type),
		}
		if s.Tmux != nil {
			e.PaneID = s.Tmux.PaneID
			e.WindowID = s.Tmux.WindowID
			if e.PaneID == currentPaneID && currentPaneID != "" {
				e.Current = true
			}
			if waitingWindows[e.WindowID] {
				e.Waiting = true
			}
		}
		entries = append(entries, e)
	}
	return entries
}

// NextSurface returns the next surface after the current one (and its index),
// wrapping around. If no surface is marked current, returns the first entry.
// Returns (nil, -1) on an empty list.
func NextSurface(entries []SurfaceEntry) (*SurfaceEntry, int) {
	if len(entries) == 0 {
		return nil, -1
	}
	cur := currentSurfaceIndex(entries)
	next := (cur + 1) % len(entries)
	return &entries[next], next
}

// PrevSurface returns the previous surface before the current one (and its
// index), wrapping around. If no surface is marked current, returns the last
// entry. Returns (nil, -1) on an empty list.
func PrevSurface(entries []SurfaceEntry) (*SurfaceEntry, int) {
	if len(entries) == 0 {
		return nil, -1
	}
	cur := currentSurfaceIndex(entries)
	if cur == -1 {
		last := len(entries) - 1
		return &entries[last], last
	}
	prev := (cur - 1 + len(entries)) % len(entries)
	return &entries[prev], prev
}

// currentSurfaceIndex returns the index of the entry marked Current, or -1.
func currentSurfaceIndex(entries []SurfaceEntry) int {
	for i, e := range entries {
		if e.Current {
			return i
		}
	}
	return -1
}

// NextWaitingSurface returns the next surface marked Waiting after the
// current one (and its index), wrapping around. If no surface is current,
// returns the first waiting entry. Returns (nil, -1) when none are waiting.
func NextWaitingSurface(entries []SurfaceEntry) (*SurfaceEntry, int) {
	cur := currentSurfaceIndex(entries)
	n := len(entries)
	if cur == -1 {
		for i, e := range entries {
			if e.Waiting {
				return &entries[i], i
			}
		}
		return nil, -1
	}
	for offset := 1; offset <= n; offset++ {
		idx := (cur + offset) % n
		if entries[idx].Waiting {
			return &entries[idx], idx
		}
	}
	return nil, -1
}
