package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List all workspaces across all docks",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			docks, err := eng.List()
			if err != nil {
				return err
			}

			for _, d := range docks {
				fmt.Printf("Dock: %s\n", d.Name)
				if len(d.Workspaces) == 0 {
					fmt.Println("  (no workspaces)")
					continue
				}
				for _, ws := range d.Workspaces {
					waiting := ""
					if ws.Waiting {
						waiting = " \u23f3"
					}
					pr := ""
					if ws.PR != "" {
						pr = "#" + ws.PR
					}
					branch := ws.Branch
					if branch == "" {
						branch = "\u2014"
					}
					agent := ws.Agent
					if agent == "" {
						agent = "\u2014"
					}
					fmt.Printf("  %-5s %-20s %-35s %-6s %-8s %-8s%s\n",
						ws.ID, ws.Name, branch, pr, ws.Status, agent, waiting)
				}
			}
			return nil
		},
	}
}
