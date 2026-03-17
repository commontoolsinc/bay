package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRecoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recover",
		Short: "Reconstruct all tmux state after reboot",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			cmds, err := eng.Recover()
			if err != nil {
				return err
			}
			if len(cmds) == 0 {
				fmt.Println("Nothing to recover.")
				return nil
			}
			fmt.Println("Recovered. Attach with:")
			for _, c := range cmds {
				fmt.Printf("  %s\n", c)
			}

			// Start monitor if not running
			mon, monErr := newMonitorWithConfig()
			if monErr == nil {
				running, _, _ := mon.Status()
				if !running {
					if startErr := mon.Start(); startErr != nil {
						fmt.Printf("Warning: could not start monitor: %v\n", startErr)
					} else {
						fmt.Println("Monitor started.")
					}
				}
			}

			return nil
		},
	}
}
