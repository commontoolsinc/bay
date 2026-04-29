package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/commontoolsinc/bay/internal/engine"
	gitpkg "github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
	tmuxpkg "github.com/commontoolsinc/bay/internal/tmux"
	"github.com/spf13/cobra"
)

func newStatusLineCmd() *cobra.Command {
	// --width is a string (not int) so unexpanded tmux format vars
	// like "#{status-right-length}" degrade gracefully to the full
	// format instead of surfacing a parse error in the status line.
	var widthStr string

	// --window takes an explicit tmux window ID (e.g. "@7") instead
	// of asking tmux which window is "current". See the long comment
	// in RunE for why this matters for correctness.
	var windowID string

	cmd := &cobra.Command{
		Use:   "status-line <field>",
		Short: "Output workspace info for tmux status line",
		Long: `Output a single field for the current workspace, resolved by tmux window ID.
Designed for use in tmux status-format strings. Outputs empty string if not in a workspace.

Fields: id, name, dir, branch, pr, status, dock, merged, full

The full field outputs label branch #PR status. Use --width to enable
adaptive truncation (pass #{status-right-length} from tmux). Non-numeric
width values are treated as 0 (full format, no truncation).

Pass --window #{window_id} so each window gets its own #() cache entry
in tmux; otherwise the status line can show stale data from a different
window until the next status-interval tick.`,
		Args: cobra.ExactArgs(1),
		// Tmux status-right runs this command on every refresh — do
		// NOT fork the monitor from here.
		Annotations: map[string]string{noMonitorAutostartAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			field := args[0]

			// Fast path: skip newEngine() entirely. status-line
			// only needs a tmux connection (for the current window
			// ID) and the manifest (for workspace data). No config
			// parse, no git interface, no engine construction.
			t := tmuxpkg.NewReal()
			g := gitpkg.NewReal()

			// Resolve the window this status line represents.
			//
			// Prefer --window over asking tmux for the "current"
			// window. Reason: tmux caches #(shell-cmd) output per
			// client, keyed by the literal command string, and only
			// refreshes on status-interval ticks (default 15s).
			// If the config is
			//     set -g status-right '#(bay status-line full)'
			// then every window/session sees the same cached output
			// — whichever window happened to be "current" when the
			// command last ran. Switching windows or docks shows
			// stale info until the next tick.
			//
			// When callers interpolate #{window_id} into the command
			// string:
			//     set -g status-right '#(bay status-line full --window #{window_id})'
			// tmux expands it to a different string per window
			// ("@1", "@2", ...), giving each its own cache entry.
			// The cache miss on a new window triggers a fresh run
			// with the correct --window, and bay reports that
			// window's workspace rather than whichever one tmux
			// reports as "current".
			//
			// Fall back to CurrentWindowID() for backward compat
			// with configs that don't pass --window yet.
			winID := windowID
			if winID == "" {
				var err error
				winID, err = t.CurrentWindowID()
				if err != nil {
					return nil // not in tmux
				}
			}

			p := bayPaths()
			m, err := manifest.Load(p.ManifestFile)
			if err != nil {
				return nil // can't load manifest
			}

			dockName, ws := resolveWindowID(m, winID)
			if ws == nil {
				return nil // not a bay window
			}

			var out string
			switch field {
			case "id":
				out = ws.ID
			case "name":
				out = ws.Name
			case "dir":
				out = engine.WorkspaceDirTag(ws)
			case "branch":
				if ws.Worktree != nil {
					out = ws.Worktree.Branch
				}
			case "pr":
				if ws.Worktree != nil && ws.Worktree.PR != "" {
					out = "#" + ws.Worktree.PR
				}
			case "status":
				if ws.Path != "" {
					if dirty, err := g.IsDirty(ws.Path); err == nil && dirty {
						out = "dirty"
					}
				}
				if out == "" && ws.IsMerged() {
					out = "merged"
				}
			case "dock":
				out = dockName
			case "merged":
				dock := m.FindDock(dockName)
				if dock != nil {
					count := 0
					for _, w := range dock.Workspaces {
						if w.IsMerged() {
							count++
						}
					}
					if count > 0 {
						out = fmt.Sprintf("%d merged", count)
					}
				}
			case "full":
				branch := ""
				pr := ""
				if ws.Worktree != nil {
					branch = ws.Worktree.Branch
					pr = ws.Worktree.PR
				}

				status := ""
				if ws.Path != "" {
					if dirty, err := g.IsDirty(ws.Path); err == nil && dirty {
						status = "dirty"
					} else if ws.IsMerged() {
						status = "merged"
					}
				} else if ws.Worktree != nil && ws.Worktree.Merged {
					status = "merged"
				}

				width, _ := strconv.Atoi(widthStr)
				out = formatStatusLine(ws, branch, pr, status, width)
			default:
				return fmt.Errorf("unknown field %q; valid fields: id, name, dir, branch, pr, status, dock, merged, full", field)
			}

			if out != "" {
				fmt.Print(out)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&widthStr, "width", "", "available width in cells; enables adaptive truncation")
	cmd.Flags().StringVar(&windowID, "window", "", "tmux window ID (pass #{window_id} so each window gets its own #() cache entry)")

	return cmd
}

// formatStatusLine builds the status-line output with optional width-adaptive
// truncation. When width is 0, the full format is returned.
//
// Tiers (tried in order until output fits within width):
//
//	full:             label branch #PR status
//	label-cropped:    w4.auth branch #PR status
//	branch-cropped:   w4.auth bran.. #PR status
//	compact-status:   w4.auth bran.. #PR *
//	minimal:          w4.auth bran.. *
//	label-only:       w4.auth
func formatStatusLine(ws *manifest.Workspace, branch, pr, status string, width int) string {
	label := engine.WorkspaceCompactLabel(ws)
	full := buildStatusLine(label, branch, pr, status)
	if width <= 0 || len(full) <= width {
		return full
	}

	// First crop only the label, preserving branch/PR/status.
	if out, ok := fitByCroppingLabel(ws, branch, pr, status, width); ok {
		return out
	}

	// Then truncate branch metadata. Keep full status first.
	if branch != "" {
		if out, ok := fitWithTruncatedBranch(ws, branch, pr, status, width); ok {
			return out
		}
	}

	shortStatus := abbreviateStatus(status)
	if branch != "" {
		if out, ok := fitWithTruncatedBranch(ws, branch, pr, shortStatus, width); ok {
			return out
		}
		if out, ok := fitWithTruncatedBranch(ws, branch, "", shortStatus, width); ok {
			return out
		}
	}

	if label != "" {
		if out, ok := fitByCroppingLabel(ws, "", "", shortStatus, width); ok {
			return out
		}
		return engine.TruncateWorkspaceCompactLabel(ws, width)
	}
	if shortStatus != "" && len(shortStatus) <= width {
		return shortStatus
	}
	return ""
}

func fitByCroppingLabel(ws *manifest.Workspace, branch, pr, status string, width int) (string, bool) {
	rest := buildStatusLine("", branch, pr, status)
	budget := width
	if rest != "" {
		budget -= len(rest) + 1
	}
	if budget <= 0 {
		return "", false
	}
	if engine.WorkspaceCompactLabel(ws) != "" {
		minBudget := 1
		if dirTag := engine.WorkspaceDirTag(ws); dirTag != "" {
			minBudget = len(dirTag)
		}
		if budget < minBudget {
			return "", false
		}
	}
	label := engine.TruncateWorkspaceCompactLabel(ws, budget)
	out := buildStatusLine(label, branch, pr, status)
	return out, out != "" && len(out) <= width
}

func fitWithTruncatedBranch(ws *manifest.Workspace, branch, pr, status string, width int) (string, bool) {
	label := engine.WorkspaceCompactLabel(ws)
	maxLabelBudget := len(label)
	if maxLabelBudget > width {
		maxLabelBudget = width
	}
	minLabelBudget := 0
	if label != "" {
		minLabelBudget = 1
		if dirTag := engine.WorkspaceDirTag(ws); dirTag != "" && len(dirTag) <= width {
			minLabelBudget = len(dirTag)
		}
	}
	for labelBudget := maxLabelBudget; labelBudget >= minLabelBudget; labelBudget-- {
		truncatedLabel := ""
		if labelBudget > 0 {
			truncatedLabel = engine.TruncateWorkspaceCompactLabel(ws, labelBudget)
		}
		branchBudget := statusLineBranchBudget(width, truncatedLabel, pr, status)
		if branchBudget < 4 {
			continue
		}
		out := buildStatusLine(truncatedLabel, engine.TruncateName(branch, branchBudget), pr, status)
		if len(out) <= width {
			return out, true
		}
	}
	return "", false
}

func statusLineBranchBudget(width int, label, pr, status string) int {
	used := 0
	parts := 1 // branch
	if label != "" {
		used += len(label)
		parts++
	}
	if pr != "" {
		used += len("#") + len(pr)
		parts++
	}
	if status != "" {
		used += len(status)
		parts++
	}
	return width - used - (parts - 1)
}

func buildStatusLine(label, branch, pr, status string) string {
	var parts []string
	if label != "" {
		parts = append(parts, label)
	}
	if branch != "" {
		parts = append(parts, branch)
	}
	if pr != "" {
		parts = append(parts, "#"+pr)
	}
	if status != "" {
		parts = append(parts, status)
	}
	return strings.Join(parts, " ")
}

// abbreviateStatus returns a short status indicator: * for dirty, M for merged.
func abbreviateStatus(status string) string {
	switch status {
	case "dirty":
		return "*"
	case "merged":
		return "M"
	default:
		return ""
	}
}

// resolveWindowID finds the dock and workspace that own a tmux window ID.
// Returns ("", nil) if no match is found.
func resolveWindowID(m *manifest.Manifest, windowID string) (string, *manifest.Workspace) {
	for i := range m.Docks {
		dock := &m.Docks[i]
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			for _, s := range ws.Surfaces {
				if s.Tmux != nil && s.Tmux.WindowID == windowID {
					return dock.Name, ws
				}
			}
		}
	}
	return "", nil
}
