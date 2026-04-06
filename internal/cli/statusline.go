package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newStatusLineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status-line <field>",
		Short: "Output workspace info for tmux status line",
		Long: `Output a single field for the current workspace, resolved by tmux window ID.
Designed for use in tmux status-format strings. Outputs empty string if not in a workspace.

Fields: name, branch, pr, status, dock, full`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			field := args[0]

			eng, err := newEngine()
			if err != nil {
				return nil // silent in status line context
			}

			winID, err := eng.Tmux.CurrentWindowID()
			if err != nil {
				return nil // not in tmux
			}

			dockName, _, ws, err := eng.ResolveByWindowID(winID)
			if err != nil {
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
				out = string(ws.Status)
			case "dock":
				out = dockName
			case "full":
				parts := []string{ws.Name}
				if ws.Worktree != nil && ws.Worktree.Branch != "" {
					branchPart := ws.Worktree.Branch
					if ws.Worktree.PR != "" {
						branchPart += " #" + ws.Worktree.PR
					}
					parts = append(parts, branchPart)
				}
				if ws.Status != "" {
					parts = append(parts, string(ws.Status))
				}
				out = strings.Join(parts, " | ")
			default:
				return fmt.Errorf("unknown field %q; valid fields: name, branch, pr, status, dock, full", field)
			}

			if out != "" {
				fmt.Print(out)
			}
			return nil
		},
	}

	return cmd
}
