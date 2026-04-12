package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTreeCmd() *cobra.Command {
	var longOutput bool
	var shortOutput bool

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

			view := BuildListView(docks, ListViewOptions{
				Focus:     ListFocus{Kind: FocusAll},
				Recursive: true,
			})

			view.SetCurrentContext(eng)

			fmt.Print(FormatListView(view, longOutput, shortOutput))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "show extended details such as tmux IDs")
	cmd.Flags().BoolVarP(&shortOutput, "short", "s", false, "compact output without labels or key names")

	return cmd
}
