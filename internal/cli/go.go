package cli

import (
	"github.com/spf13/cobra"
)

func newGoCmd() *cobra.Command {
	var index int

	cmd := &cobra.Command{
		Use:   "go [query]",
		Short: "Navigate to a surface in the current workspace",
		Long: `Fuzzy find and switch to a surface within the current workspace.
This is an alias for "bay surface go".

  bay go             pick from surfaces in current workspace
  bay go shell       jump to the shell surface
  bay go --index 2   jump to surface #2`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return surfaceGo(eng, args, index)
		},
	}

	cmd.Flags().IntVar(&index, "index", 0, "jump to surface by 1-based index")

	return cmd
}
