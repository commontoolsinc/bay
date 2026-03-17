package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPaneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pane",
		Short: "Manage panes",
	}

	cmd.AddCommand(newPaneAddCmd())

	return cmd
}

func newPaneAddCmd() *cobra.Command {
	var agent, cmdStr, splitDir string
	var shell bool

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a pane to the current window",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsID, err := eng.ResolveSelf()
			if err != nil {
				return err
			}

			// Find current window
			ws, err := eng.WsShow(dockName, wsID)
			if err != nil {
				return err
			}

			winIDStr, _ := eng.Tmux.CurrentWindowID()
			for _, w := range ws.Windows {
				if w.TmuxWindowID == winIDStr {
					return eng.PaneAdd(dockName, wsID, w.ID, agent, shell, cmdStr, splitDir)
				}
			}

			return fmt.Errorf("current window not found in workspace")
		},
	}

	cmd.Flags().StringVar(&agent, "agent", "", "agent type")
	cmd.Flags().BoolVar(&shell, "shell", false, "open a shell")
	cmd.Flags().StringVar(&cmdStr, "cmd", "", "command to run")
	cmd.Flags().StringVar(&splitDir, "split", "v", "split direction (h or v)")

	return cmd
}
