package cli

import (
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func newShellCmd() *cobra.Command {
	var window bool
	var name string

	cmd := &cobra.Command{
		Use:   "shell [workspace]",
		Short: "Open a shell surface for a workspace",
		Long: `Open a shell for a workspace.

  bay shell             split in current workspace
  bay shell auth-fix    new surface for that workspace
  bay shell --window    new tmux window for current workspace`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			var dockName, wsName string
			if len(args) > 0 {
				dockName, wsName, err = resolveTarget(eng, args[0])
			} else {
				dockName, wsName, err = eng.ResolveSelf()
			}
			if err != nil {
				return err
			}

			surfaceName := name
			if surfaceName == "" {
				surfaceName = "shell"
			}

			splitDir := "v"
			if window {
				splitDir = "" // empty = new window
			}

			return eng.SurfaceAdd(dockName, wsName, manifest.SurfaceTypeShell, surfaceName, "", "", splitDir)
		},
	}

	cmd.Flags().BoolVar(&window, "window", false, "open as new tmux window instead of split")
	cmd.Flags().StringVar(&name, "name", "", "surface name")

	return cmd
}
