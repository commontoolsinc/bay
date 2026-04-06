package cli

import (
	"github.com/spf13/cobra"
)

func newClosePaneCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "close-pane",
		Short:  "Close current surface (or pane if not bay-managed)",
		Hidden: true, // internal command for keybinding
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			// Try to resolve the current surface.
			dockName, wsName, resolveErr := eng.ResolveSelf()
			if resolveErr != nil {
				// Not in a bay workspace — just kill the active tmux pane.
				paneID, err := eng.Tmux.CurrentPaneID()
				if err != nil {
					return err
				}
				return eng.Tmux.KillPane(paneID)
			}

			// Find which surface matches the current pane.
			paneID, err := eng.Tmux.CurrentPaneID()
			if err != nil {
				return err
			}

			ws, err := eng.WsShow(dockName, wsName)
			if err != nil {
				return eng.Tmux.KillPane(paneID)
			}

			for _, s := range ws.Surfaces {
				if s.Tmux != nil && s.Tmux.PaneID == paneID {
					return eng.SurfaceClose(dockName, wsName, s.Name)
				}
			}

			// Pane not tracked in manifest — just kill it.
			return eng.Tmux.KillPane(paneID)
		},
	}
}
