package cli

import (
	"fmt"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func newWinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "win",
		Aliases: []string{"window"},
		Short:   "Manage surfaces (legacy alias for surface operations)",
	}

	cmd.AddCommand(
		newWinOpenCmd(),
		newWinCloseCmd(),
		newWinRestartCmd(),
	)

	return cmd
}

func newWinOpenCmd() *cobra.Command {
	var agent, cmdStr, name string
	var shell bool

	cmd := &cobra.Command{
		Use:   "open [workspace]",
		Short: "Add a surface to a workspace in a new window",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			target := "self"
			if len(args) > 0 {
				target = args[0]
			}
			dockName, wsName, err := resolveTarget(eng, target)
			if err != nil {
				return err
			}

			var surfaceType manifest.SurfaceType
			surfaceName := name
			switch {
			case shell:
				surfaceType = manifest.SurfaceTypeShell
				if surfaceName == "" {
					surfaceName = "shell"
				}
			case cmdStr != "":
				surfaceType = manifest.SurfaceTypeCmd
				if surfaceName == "" {
					surfaceName = "cmd"
				}
			case agent != "":
				surfaceType = manifest.SurfaceTypeAgent
				if surfaceName == "" {
					surfaceName = "agent"
				}
			default:
				surfaceType = manifest.SurfaceTypeShell
				if surfaceName == "" {
					surfaceName = "shell"
				}
			}

			// Empty splitDir = new window (not a split)
			return eng.SurfaceAdd(dockName, wsName, surfaceType, surfaceName, agent, cmdStr, "")
		},
	}

	cmd.Flags().StringVar(&agent, "agent", "", "agent type")
	cmd.Flags().BoolVar(&shell, "shell", false, "open a shell")
	cmd.Flags().StringVar(&cmdStr, "cmd", "", "command to run")
	cmd.Flags().StringVar(&name, "name", "", "surface name")

	return cmd
}

func newWinCloseCmd() *cobra.Command {
	var surfaceName string

	cmd := &cobra.Command{
		Use:   "close [self|workspace]",
		Short: "Close a surface",
		Args:  cobra.MaximumNArgs(1),
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

			return eng.SurfaceClose(dockName, wsName, sName)
		},
	}

	cmd.Flags().StringVar(&surfaceName, "surface", "", "surface name")

	return cmd
}

func newWinRestartCmd() *cobra.Command {
	var surfaceName string

	cmd := &cobra.Command{
		Use:   "restart [self|workspace]",
		Short: "Restart a surface's process",
		Args:  cobra.MaximumNArgs(1),
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

// resolveSurfaceTarget resolves a target to (dock, workspace, surface name).
func resolveSurfaceTarget(eng *engine.Engine, target, explicitSurface string) (string, string, string, error) {
	dockName, wsName, err := resolveTarget(eng, target)
	if err != nil {
		return "", "", "", err
	}

	ws, err := eng.WsShow(dockName, wsName)
	if err != nil {
		return "", "", "", err
	}
	if len(ws.Surfaces) == 0 {
		return "", "", "", fmt.Errorf("no surfaces in workspace %q", wsName)
	}

	if explicitSurface != "" {
		if ws.FindSurface(explicitSurface) == nil {
			return "", "", "", fmt.Errorf("surface %q not found in workspace %q", explicitSurface, wsName)
		}
		return dockName, wsName, explicitSurface, nil
	}

	// Match current tmux pane.
	if target == "self" {
		paneID, tmuxErr := eng.Tmux.CurrentPaneID()
		if tmuxErr == nil {
			for _, s := range ws.Surfaces {
				if s.Tmux != nil && s.Tmux.PaneID == paneID {
					return dockName, wsName, s.Name, nil
				}
			}
		}
	}

	// Default: first surface.
	return dockName, wsName, ws.Surfaces[0].Name, nil
}
