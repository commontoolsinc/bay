package cli

import (
	"fmt"
	"strings"

	gitpkg "github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
	tmuxpkg "github.com/commontoolsinc/bay/internal/tmux"
	"github.com/spf13/cobra"
)

func newStatusLineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status-line <field>",
		Short: "Output workspace info for tmux status line",
		Long: `Output a single field for the current workspace, resolved by tmux window ID.
Designed for use in tmux status-format strings. Outputs empty string if not in a workspace.

Fields: name, branch, pr, status, dock, merged, full`,
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

			winID, err := t.CurrentWindowID()
			if err != nil {
				return nil // not in tmux
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
			case "name":
				out = ws.Name
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
				parts := []string{ws.Name}
				if ws.Worktree != nil && ws.Worktree.Branch != "" {
					branchPart := ws.Worktree.Branch
					if ws.Worktree.PR != "" {
						branchPart += " #" + ws.Worktree.PR
					}
					parts = append(parts, branchPart)
				}
				if ws.Path != "" {
					if dirty, err := g.IsDirty(ws.Path); err == nil && dirty {
						parts = append(parts, "dirty")
					} else if ws.IsMerged() {
						parts = append(parts, "merged")
					}
				} else if ws.Worktree != nil && ws.Worktree.Merged {
					parts = append(parts, "merged")
				}
				out = strings.Join(parts, " | ")
			default:
				return fmt.Errorf("unknown field %q; valid fields: name, branch, pr, status, dock, merged, full", field)
			}

			if out != "" {
				fmt.Print(out)
			}
			return nil
		},
	}

	return cmd
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
