package cli

import (
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func newShellCmd() *cobra.Command {
	var window, pane bool
	var splitDir string
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:   "shell [name]",
		Short: "Open a shell surface in a workspace",
		Long: `Add a shell surface to a workspace. The positional is the new
shell's display name; --ws/--dock select the target workspace.

  bay shell                    shell as a split pane (default)
  bay shell logs               new shell named "logs"
  bay shell --window           shell in a new tmux window
  bay shell --split h          horizontal split
  bay shell --ws auth-fix      target a different workspace`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			surfaceName := "shell"
			if len(args) > 0 {
				if err := validateSurfaceName(args[0]); err != nil {
					return err
				}
				surfaceName = args[0]
			}

			dockName, wsName, err := resolveSurfaceWorkspace(eng, wsFlag, dockFlag)
			if err != nil {
				return err
			}

			sd := resolveSplit(splitDir, window, pane)

			return eng.SurfaceAdd(engine.SurfaceAddOptions{
				DockName: dockName,
				WsName:   wsName,
				Type:     manifest.SurfaceTypeShell,
				Name:     surfaceName,
				SplitDir: sd,
			})
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (defaults to current)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (with --ws to disambiguate)")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&pane, "pane", false, "split into current window (default; shorthand for --split v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as a new tmux window")

	return cmd
}
