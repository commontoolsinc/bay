package cli

import (
	"github.com/spf13/cobra"
)

func newHomeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "home",
		Short: "Focus the dock checkout shell (or create one)",
		Long: `Focus the most recent home surface in the current dock.
If home has no surfaces, create a shell at the dock's checkout path.

Home is the dock's canonical checkout, not a worktree — its lifecycle is
dock-owned and never deletes the checkout directory.`,
		Args:    cobra.NoArgs,
		GroupID: "navigation",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			dockName, err := resolveCurrentDock(eng)
			if err != nil {
				return err
			}
			return eng.Home(dockName)
		},
	}
}
