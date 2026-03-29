package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newShellCmd() *cobra.Command {
	var window bool

	cmd := &cobra.Command{
		Use:   "shell [name]",
		Short: "Open a shell pane or window for a workspace",
		Long: `Open a shell for a workspace.

  bay shell             split pane in current window
  bay shell auth-fix    new window for that workspace
  bay shell --window    new window for current workspace`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			if len(args) > 0 {
				// Named workspace: open new window with shell
				dockName, wsID, resolveErr := resolveTarget(eng, args[0])
				if resolveErr != nil {
					return resolveErr
				}
				return eng.WinOpen(dockName, wsID, "", true, "")
			}

			// No args: resolve current workspace
			dockName, wsID, err := eng.ResolveSelf()
			if err != nil {
				return err
			}

			if window {
				// --window: new window for current workspace
				return eng.WinOpen(dockName, wsID, "", true, "")
			}

			// Default: split pane in current window
			ws, err := eng.WsShow(dockName, wsID)
			if err != nil {
				return err
			}

			winIDStr, tmuxErr := eng.Tmux.CurrentWindowID()
			if tmuxErr != nil {
				return fmt.Errorf("cannot determine current tmux window")
			}

			for _, w := range ws.Windows {
				if w.TmuxWindowID == winIDStr {
					return eng.PaneAdd(dockName, wsID, w.ID, "", true, "", "v")
				}
			}

			return fmt.Errorf("current window not found in workspace")
		},
	}

	cmd.Flags().BoolVar(&window, "window", false, "open as new window instead of split pane")

	return cmd
}
