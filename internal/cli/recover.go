package cli

import (
	"fmt"
	"strings"

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
				recovered, err := eng.DockRecover(session)
				if err != nil {
					// Not a bay dock — fall through to full recovery
				} else {
					if len(recovered) == 0 {
						fmt.Printf("Dock %q: nothing to recover.\n", session)
					} else {
						fmt.Printf("Dock %q: recovered %s\n", session, strings.Join(recovered, ", "))
					}
					if currentWinID != "" {
						_ = eng.Tmux.SelectWindow(currentWinID)
					}
					startMonitor()
					return nil
				}
			}

			// Outside tmux or not a bay dock: recover all
			results, err := eng.Recover()
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println("Nothing to recover.")
				return nil
			}
			for _, r := range results {
				if len(r.Recovered) == 0 {
					fmt.Printf("Dock %q: nothing to recover.\n", r.Dock)
				} else {
					fmt.Printf("Dock %q: recovered %s\n", r.Dock, strings.Join(r.Recovered, ", "))
				}
			}
			fmt.Println("\nAttach with:")
			for _, r := range results {
				fmt.Printf("  %s\n", r.AttachCmd)
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
