package cli

import (
	"fmt"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/spf13/cobra"
)

func newWinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "win",
		Aliases: []string{"window"},
		Short:   "Manage windows",
	}

	cmd.AddCommand(
		newWinOpenCmd(),
		newWinCloseCmd(),
		newWinRestartCmd(),
	)

	return cmd
}

func newWinOpenCmd() *cobra.Command {
	var agent, cmdStr string
	var shell bool

	cmd := &cobra.Command{
		Use:   "open <workspace>",
		Short: "Add a window to an existing workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsID, err := resolveTarget(eng, args[0])
			if err != nil {
				return err
			}

			return eng.WinOpen(dockName, wsID, agent, shell, cmdStr)
		},
	}

	cmd.Flags().StringVar(&agent, "agent", "", "agent type")
	cmd.Flags().BoolVar(&shell, "shell", false, "open a shell")
	cmd.Flags().StringVar(&cmdStr, "cmd", "", "command to run")

	return cmd
}

func newWinCloseCmd() *cobra.Command {
	var winFlag int

	cmd := &cobra.Command{
		Use:   "close [self|workspace]",
		Short: "Close a window (not the workspace)",
		Long: `Close a window. With "self" (default), closes the current tmux window.
With a workspace name/id, closes the primary window (or use --window N).`,
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

			dockName, wsID, winID, err := resolveWindowTarget(eng, target, winFlag)
			if err != nil {
				return err
			}

			return eng.WinClose(dockName, wsID, winID)
		},
	}

	cmd.Flags().IntVar(&winFlag, "window", 0, "window ID within workspace (default: current or primary)")

	return cmd
}

func newWinRestartCmd() *cobra.Command {
	var winFlag int

	cmd := &cobra.Command{
		Use:   "restart [self|workspace]",
		Short: "Kill agent and respawn with fresh config",
		Long: `Restart a window's agent. With "self" (default), restarts the current tmux window.
With a workspace name/id, restarts the primary window (or use --window N).`,
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

			dockName, wsID, winID, err := resolveWindowTarget(eng, target, winFlag)
			if err != nil {
				return err
			}

			return eng.WinRestart(dockName, wsID, winID)
		},
	}

	cmd.Flags().IntVar(&winFlag, "window", 0, "window ID within workspace (default: current or primary)")

	return cmd
}

// resolveWindowTarget resolves a target to (dock, wsID, windowID).
// If target is "self", uses the current tmux window. Otherwise resolves to
// the workspace and uses the explicit --window flag or the primary window.
func resolveWindowTarget(eng *engine.Engine, target string, explicitWinID int) (string, string, int, error) {
	dockName, wsID, err := resolveTarget(eng, target)
	if err != nil {
		return "", "", 0, err
	}

	ws, err := eng.WsShow(dockName, wsID)
	if err != nil {
		return "", "", 0, err
	}
	if len(ws.Windows) == 0 {
		return "", "", 0, fmt.Errorf("no windows in workspace %s", wsID)
	}

	// Explicit --window flag
	if explicitWinID > 0 {
		for _, w := range ws.Windows {
			if w.ID == explicitWinID {
				return dockName, wsID, w.ID, nil
			}
		}
		return "", "", 0, fmt.Errorf("window %d not found in workspace %s", explicitWinID, wsID)
	}

	// "self" — match current tmux window
	if target == "self" {
		winIDStr, tmuxErr := eng.Tmux.CurrentWindowID()
		if tmuxErr == nil {
			for _, w := range ws.Windows {
				if w.TmuxWindowID == winIDStr {
					return dockName, wsID, w.ID, nil
				}
			}
		}
		// If we resolved via CWD but can't match tmux window, fall through to primary
	}

	// Default: primary (first) window
	return dockName, wsID, ws.Windows[0].ID, nil
}

