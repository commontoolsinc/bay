package cli

import (
	"encoding/json"
	"fmt"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/spf13/cobra"
)

func newLsCmd() *cobra.Command {
	var jsonOutput bool
	var dirtyOnly bool

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List all repos, docks, and workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			docks, err := eng.List()
			if err != nil {
				return err
			}

			if dirtyOnly {
				docks = filterDirtyWorkspaces(eng, docks)
			}

			if jsonOutput {
				if docks == nil {
					docks = []engine.DockInfo{}
				}
				data, err := json.MarshalIndent(docks, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			fmt.Print(FormatFullTree(eng.Config, docks))

			// Print advice for stale or missing workspaces
			hasStale, hasMissing := false, false
			for _, d := range docks {
				for _, ws := range d.Workspaces {
					if ws.Stale {
						hasStale = true
					}
					if ws.Missing {
						hasMissing = true
					}
				}
			}
			if hasStale {
				fmt.Println("\n[stale] = tmux window was closed. Run 'bay recover' to recreate, or 'bay ws close <name>' to remove.")
			}
			if hasMissing {
				fmt.Println("\n[missing] = worktree directory was deleted. Run 'bay ws close <name> --force' to clean up.")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&dirtyOnly, "dirty", false, "only show dirty workspaces")

	return cmd
}

// filterDirtyWorkspaces filters dock info to only include workspaces with
// uncommitted changes.
func filterDirtyWorkspaces(eng *engine.Engine, docks []engine.DockInfo) []engine.DockInfo {
	var result []engine.DockInfo
	for _, d := range docks {
		filtered := engine.DockInfo{
			Name:  d.Name,
			Agent: d.Agent,
			Repo:  d.Repo,
		}
		for _, ws := range d.Workspaces {
			if ws.Path == "" {
				continue
			}
			path := config.ExpandPath(ws.Path)
			dirty, err := eng.Git.IsDirty(path)
			if err != nil {
				continue
			}
			if dirty {
				filtered.Workspaces = append(filtered.Workspaces, ws)
			}
		}
		if len(filtered.Workspaces) > 0 {
			result = append(result, filtered)
		}
	}
	return result
}
