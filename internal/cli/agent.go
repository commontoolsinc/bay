package cli

import (
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func newAgentCmd() *cobra.Command {
	var window, pane bool
	var splitDir string
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:   "agent [type] [name]",
		Short: "Open an agent surface in a workspace",
		Long: `Launch an agent in a workspace. The agent type is optional — if
omitted, uses the dock's default agent.

  bay agent                    dock's default agent as a split pane
  bay agent claude             specific agent
  bay agent codex my-codex     specific agent with custom name
  bay agent --window           as a new tmux window
  bay agent --ws auth-fix      target a different workspace`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			opts := surfaceNewOpts{
				Type:     manifest.SurfaceTypeAgent,
				SplitDir: resolveSplit(splitDir, window, pane),
			}
			if len(args) > 0 {
				opts.Agent = args[0]
			}
			if len(args) > 1 {
				if err := validateSurfaceName(args[1]); err != nil {
					return err
				}
				opts.Name = args[1]
			}

			dockName, wsName, err := resolveSurfaceWorkspace(eng, wsFlag, dockFlag)
			if err != nil {
				return err
			}

			return runSurfaceNew(eng, dockName, wsName, opts)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (defaults to current)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (with --ws to disambiguate)")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&pane, "pane", false, "split into current window (default; shorthand for --split v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as a new tmux window")

	return cmd
}
