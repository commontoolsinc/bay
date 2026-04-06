package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTreeCmd() *cobra.Command {
	var longOutput bool

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show full hierarchy (repos, docks, workspaces, surfaces)",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			docks, err := eng.List()
			if err != nil {
				return err
			}

			view := BuildListView(eng.Config, docks, ListViewOptions{
				Focus:     ListFocus{Kind: FocusAll},
				Recursive: true,
			})

			fmt.Print(FormatListView(view, longOutput))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "show extended details such as tmux IDs")

	return cmd
}
