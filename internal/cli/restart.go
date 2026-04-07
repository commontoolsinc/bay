package cli

import (
	"fmt"

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

			if len(args) == 0 && wsFlag == "" && dockFlag == "" {
				return surfaceRestartCurrentPane(eng)
			}
			if len(args) == 0 {
				return fmt.Errorf("--ws/--dock require a surface name")
			}

			dockName, wsName, sName, err := resolveSurfaceArg(eng, args[0], wsFlag, dockFlag)
			if err != nil {
				return err
			}
			return eng.SurfaceRestart(dockName, wsName, sName)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (disambiguates with --dock)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (only valid with --ws or a workspace prefix)")

	return cmd
}
