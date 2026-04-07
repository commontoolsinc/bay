package cli

import (
	"github.com/spf13/cobra"
)

func newRestartCmd() *cobra.Command {
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:   "restart [name]",
		Short: "Restart a surface's process",
		Long: `Restart a surface by name, or the current surface if no name given.
This is a shorthand for "bay surface restart".

  bay restart agent              restart the agent surface
  bay restart w1:agent           restart agent in workspace w1
  bay restart agent --ws w1      same as w1:agent
  bay restart                    restart current surface`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return runSurfaceRestart(eng, args, wsFlag, dockFlag)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (disambiguates with --dock)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (only valid with --ws or a workspace prefix)")

	return cmd
}
