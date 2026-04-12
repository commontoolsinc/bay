package cli

import (
	"github.com/spf13/cobra"
)

func newGoCmd() *cobra.Command {
	var nextWaiting bool
	var pick bool

	cmd := &cobra.Command{
		Use:   "go [query]",
		Short: "Navigate to a surface in the current workspace",
		Long: `Fuzzy find and switch to a surface within the current workspace.
This is an alias for "bay surface go".

  bay go                  pick from surfaces in current workspace
  bay go shell            jump to the shell surface
  bay go --next-waiting   jump to next waiting surface`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			if pick {
				return surfaceGoPick(eng)
			}
			return surfaceGo(eng, args, nextWaiting)
		},
	}

	cmd.Flags().BoolVar(&nextWaiting, "next-waiting", false, "jump to next waiting surface")
	cmd.Flags().BoolVar(&pick, "pick", false, "open picker in a popup (used by keybindings)")
	cmd.Flags().MarkHidden("pick")

	return cmd
}
