package cli

import (
	"fmt"

	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func newPaneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pane",
		Short: "Manage panes (add surfaces to current window)",
	}

	cmd.AddCommand(newPaneAddCmd())

	return cmd
}

func newPaneAddCmd() *cobra.Command {
	var agent, cmdStr, splitDir, name string
	var shell bool

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a surface to the current workspace (split from current pane)",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsName, err := eng.ResolveSelf()
			if err != nil {
				return err
			}

			// Determine surface type and name.
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

			if splitDir == "" {
				splitDir = "v"
			}

			if err := eng.SurfaceAdd(dockName, wsName, surfaceType, surfaceName, agent, cmdStr, splitDir); err != nil {
				return fmt.Errorf("adding surface: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&agent, "agent", "", "agent type")
	cmd.Flags().BoolVar(&shell, "shell", false, "open a shell")
	cmd.Flags().StringVar(&cmdStr, "cmd", "", "command to run")
	cmd.Flags().StringVar(&splitDir, "split", "v", "split direction (h or v)")
	cmd.Flags().StringVar(&name, "name", "", "surface name")

	return cmd
}
