package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRecoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recover",
		Short: "Reconstruct tmux state after reboot",
		Long: `Reconstruct tmux state from the manifest.

Inside a dock: recovers just that dock.
Outside tmux: recovers all docks.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			// Save current window so we can restore focus after recovery
			currentWinID, _ := eng.Tmux.CurrentWindowID()

			// Inside a dock: recover just that dock
			if session, sessionErr := eng.Tmux.CurrentSession(); sessionErr == nil {
				attachCmd, err := eng.DockRecover(session)
				if err != nil {
					// Not a bay dock — fall through to full recovery
					if currentWinID != "" {
						_ = eng.Tmux.SelectWindow(currentWinID)
					}
				} else {
					fmt.Printf("Recovered dock %q.\n", session)
					fmt.Printf("  %s\n", attachCmd)
					if currentWinID != "" {
						_ = eng.Tmux.SelectWindow(currentWinID)
					}
					startMonitor()
					return nil
				}
			}

			// Outside tmux or not a bay dock: recover all
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

			// Restore focus if we were in tmux
			if currentWinID != "" {
				_ = eng.Tmux.SelectWindow(currentWinID)
			}

			startMonitor()
			return nil
		},
	}
}

func startMonitor() {
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
}
