// Package nav provides fuzzy workspace navigation for "bay go".
package nav

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mpsalisbury/bay/internal/manifest"
	"github.com/mpsalisbury/bay/internal/tmux"
)

// Entry represents a single navigable target (one tmux window within a workspace).
type Entry struct {
	DockName     string
	WsID         string
	WsName       string
	Branch       string
	PR           string
	Status       manifest.WorkspaceStatus
	TmuxWindowID string
	Waiting      bool
}

// CollectEntries builds a list of all navigation entries from the manifest.
// Each workspace window becomes one entry. The waiting status is determined
// by querying the tmux @bay-waiting window option.
func CollectEntries(m *manifest.Manifest, tc tmux.Interface) []Entry {
	var entries []Entry

	// Sort dock names for deterministic output.
	dockNames := make([]string, 0, len(m.Docks))
	for name := range m.Docks {
		dockNames = append(dockNames, name)
	}
	sort.Strings(dockNames)

	for _, dockName := range dockNames {
		dock := m.Docks[dockName]

		// Sort workspace IDs for deterministic output.
		wsIDs := make([]string, 0, len(dock.Workspaces))
		for id := range dock.Workspaces {
			wsIDs = append(wsIDs, id)
		}
		sort.Strings(wsIDs)

		for _, wsID := range wsIDs {
			ws := dock.Workspaces[wsID]
			for _, win := range ws.Windows {
				waiting := false
				if win.TmuxWindowID != "" {
					val, err := tc.GetWindowOption(win.TmuxWindowID, "@bay-waiting")
					if err == nil && val == "1" {
						waiting = true
					}
				}
				entries = append(entries, Entry{
					DockName:     dockName,
					WsID:         wsID,
					WsName:       ws.Name,
					Branch:       ws.Branch,
					PR:           ws.PR,
					Status:       ws.Status,
					TmuxWindowID: win.TmuxWindowID,
					Waiting:      waiting,
				})
			}
		}
	}
	return entries
}

// FuzzyMatch filters entries by case-insensitive substring match against
// WsName, Branch, PR, and DockName. An empty query returns all entries.
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

// NextWaiting returns the next waiting entry after the given current window,
// cycling through the list. Returns nil if no entries are waiting.
func NextWaiting(entries []Entry, currentWindowID string) *Entry {
	waiting := FilterWaiting(entries)
	if len(waiting) == 0 {
		return nil
	}

	// Find current position among all entries.
	currentIdx := -1
	for i, e := range entries {
		if e.TmuxWindowID == currentWindowID {
			currentIdx = i
			break
		}
	}

	// If current not found, return the first waiting entry.
	if currentIdx == -1 {
		return &waiting[0]
	}

	// Scan forward from current+1, wrapping around.
	n := len(entries)
	for offset := 1; offset <= n; offset++ {
		idx := (currentIdx + offset) % n
		if entries[idx].Waiting {
			e := entries[idx]
			return &e
		}
	}

	return nil
}

// FormatEntry formats a single entry for picker display.
func FormatEntry(e Entry) string {
	pr := ""
	if e.PR != "" {
		pr = "#" + e.PR
	}
	waiting := ""
	if e.Waiting {
		waiting = "  WAITING"
	}
	return fmt.Sprintf("%s  %s  %s  %s  %s  %s%s",
		e.DockName, e.WsID, e.WsName, e.Branch, pr, string(e.Status), waiting)
}

// FormatEntries formats all entries with aligned columns.
func FormatEntries(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}

	// Collect column values.
	type row struct {
		dock    string
		wsID    string
		wsName  string
		branch  string
		pr      string
		status  string
		waiting string
	}

	rows := make([]row, len(entries))
	maxDock, maxWsID, maxWsName, maxBranch, maxPR, maxStatus := 0, 0, 0, 0, 0, 0

	for i, e := range entries {
		pr := ""
		if e.PR != "" {
			pr = "#" + e.PR
		}
		waiting := ""
		if e.Waiting {
			waiting = "WAITING"
		}
		r := row{
			dock:    e.DockName,
			wsID:    e.WsID,
			wsName:  e.WsName,
			branch:  e.Branch,
			pr:      pr,
			status:  string(e.Status),
			waiting: waiting,
		}
		rows[i] = r

		if len(r.dock) > maxDock {
			maxDock = len(r.dock)
		}
		if len(r.wsID) > maxWsID {
			maxWsID = len(r.wsID)
		}
		if len(r.wsName) > maxWsName {
			maxWsName = len(r.wsName)
		}
		if len(r.branch) > maxBranch {
			maxBranch = len(r.branch)
		}
		if len(r.pr) > maxPR {
			maxPR = len(r.pr)
		}
		if len(r.status) > maxStatus {
			maxStatus = len(r.status)
		}
	}

	var sb strings.Builder
	for _, r := range rows {
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s",
			maxDock, r.dock,
			maxWsID, r.wsID,
			maxWsName, r.wsName,
			maxBranch, r.branch,
			maxPR, r.pr,
			maxStatus, r.status,
		)
		if r.waiting != "" {
			line += "  " + r.waiting
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return sb.String()
}
