package cli

import (
	"github.com/spf13/cobra"
)

func newRestartCmd() *cobra.Command {
	var surfaceName string

	cmd := &cobra.Command{
		Use:   "restart [workspace]",
		Short: "Restart a surface's process",
		Long: `Restart the current surface (or a named one).
This is a shorthand for "bay surface restart".

  bay restart             restart current surface
  bay restart --surface agent   restart a specific surface`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			target := "self"
			if len(args) > 0 {
				target = args[0]
			}
			dockName, wsName, sName, err := resolveSurfaceTarget(eng, target, surfaceName)
			if err != nil {
				return err
			}

			return eng.SurfaceRestart(dockName, wsName, sName)
		},
	}

	cmd.Flags().StringVar(&surfaceName, "surface", "", "surface name")

	return cmd
}
