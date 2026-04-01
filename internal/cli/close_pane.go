package cli

import (
	"github.com/commontoolsinc/bay/internal/engine"
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

			// Resolve workspace context for manifest updates
			dockName, wsID, resolveErr := eng.ResolveSelf()

			if len(panes) > 1 {
				// Multiple panes: kill the active one and update manifest
				for _, p := range panes {
					if p.Active {
						_ = eng.Tmux.KillPane(p.ID)
						// Remove the pane from the manifest if we know the workspace
						if resolveErr == nil {
							syncPaneCount(eng, dockName, wsID, winID)
						}
						return nil
					}
				}
				// Fallback: kill last pane
				_ = eng.Tmux.KillPane(panes[len(panes)-1].ID)
				if resolveErr == nil {
					syncPaneCount(eng, dockName, wsID, winID)
				}
				return nil
			}

			// Single pane: close the window via bay
			if resolveErr == nil {
				ws, err := eng.WsShow(dockName, wsID)
				if err == nil {
					for _, w := range ws.Windows {
						if w.TmuxWindowID == winID {
							return eng.WinClose(dockName, wsID, w.ID)
						}
					}
				}
			}

			// Not a bay window — just kill the tmux pane
			if len(panes) > 0 {
				return eng.Tmux.KillPane(panes[0].ID)
			}
			return nil
		},
	}
}

// syncPaneCount reconciles the manifest's pane list with the actual
// tmux pane count after a pane is closed.
func syncPaneCount(eng *engine.Engine, dockName, wsID, tmuxWinID string) {
	ws, err := eng.WsShow(dockName, wsID)
	if err != nil {
		return
	}

	for _, w := range ws.Windows {
		if w.TmuxWindowID == tmuxWinID {
			// Count actual tmux panes
			tmuxPanes, err := eng.Tmux.ListPanes(tmuxWinID)
			if err != nil {
				return
			}
			// If manifest has more panes than tmux, trim from the end
			if len(w.Panes) > len(tmuxPanes) {
				eng.SyncManifestPanes(dockName, wsID, w.ID, len(tmuxPanes))
			}
			return
		}
	}
}
