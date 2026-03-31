package cli

import (
	"github.com/spf13/cobra"
)

func newClosePaneCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "close-pane",
		Short:  "Close current pane (or window if only one pane)",
		Hidden: true, // internal command for keybinding
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			winID, err := eng.Tmux.CurrentWindowID()
			if err != nil {
				return err
			}

			panes, err := eng.Tmux.ListPanes(winID)
			if err != nil {
				return err
			}

			if len(panes) > 1 {
				// Multiple panes: kill the active one
				for _, p := range panes {
					if p.Active {
						return eng.Tmux.KillPane(p.ID)
					}
				}
				// Fallback: kill last pane
				return eng.Tmux.KillPane(panes[len(panes)-1].ID)
			}

			// Single pane: close the window via bay
			dockName, wsID, err := eng.ResolveSelf()
			if err != nil {
				// Not a bay window — just kill the tmux pane
				if len(panes) > 0 {
					return eng.Tmux.KillPane(panes[0].ID)
				}
				return nil
			}

			ws, err := eng.WsShow(dockName, wsID)
			if err != nil {
				return err
			}

			for _, w := range ws.Windows {
				if w.TmuxWindowID == winID {
					return eng.WinClose(dockName, wsID, w.ID)
				}
			}

			// Fallback
			if len(panes) > 0 {
				return eng.Tmux.KillPane(panes[0].ID)
			}
			return nil
		},
	}
}
