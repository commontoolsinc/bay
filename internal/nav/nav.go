// Package nav provides fuzzy workspace navigation for "bay go" and "bay ws go".
package nav

import (
	"fmt"
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
	Status   manifest.WorkspaceStatus
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

		for i := range dock.Workspaces {
			ws := &dock.Workspaces[i]

			branch := ""
			pr := ""
			if ws.Worktree != nil {
				branch = ws.Worktree.Branch
				pr = ws.Worktree.PR
			}

			// Find the first tmux window ID and check waiting status.
			var tmuxWindowID string
			waiting := false
			for _, s := range ws.Surfaces {
				if s.Tmux != nil && s.Tmux.WindowID != "" {
					if tmuxWindowID == "" {
						tmuxWindowID = s.Tmux.WindowID
					}
					val, err := tc.GetWindowOption(s.Tmux.WindowID, "@bay-waiting")
					if err == nil && val == "1" {
						waiting = true
					}
				}
			}

			entries = append(entries, Entry{
				DockName:     dockName,
				WsName:       ws.Name,
				Branch:       branch,
				PR:           pr,
				Status:       ws.Status,
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

// NextWaiting returns the next waiting entry after the given current window.
func NextWaiting(entries []Entry, currentWindowID string) *Entry {
	waiting := FilterWaiting(entries)
	if len(waiting) == 0 {
		return nil
	}

	currentIdx := -1
	for i, e := range entries {
		if e.TmuxWindowID == currentWindowID {
			currentIdx = i
			break
		}
	}

	if currentIdx == -1 {
		return &waiting[0]
	}

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
	return fmt.Sprintf("%s  %s  %s  %s  %s%s",
		e.DockName, e.WsName, e.Branch, pr, string(e.Status), waiting)
}

// FormatEntries formats all entries with aligned columns.
func FormatEntries(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}

	type row struct {
		dock    string
		wsName  string
		branch  string
		pr      string
		status  string
		waiting string
	}

	rows := make([]row, len(entries))
	maxDock, maxWsName, maxBranch, maxPR, maxStatus := 0, 0, 0, 0, 0

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
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s",
			maxDock, r.dock,
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
