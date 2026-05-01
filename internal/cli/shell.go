package cli

import "github.com/spf13/cobra"

func newShellCmd() *cobra.Command {
	return newTopNewSurfaceCmd(topNewCmdSpec{
		use:   "shell [name]",
		short: "Open a shell surface in a bay",
		long: `Add a shell surface to a bay. The positional is the new
shell's display name; --bay/--dock select the target bay.

  bay shell                    shell as a split pane (default)
  bay shell logs               new shell named "logs"
  bay shell --window           shell in a new tmux window
  bay shell --split h          horizontal split
  bay shell --bay w1           target a different bay`,
		args:      cobra.MaximumNArgs(1),
		buildOpts: shellSurfaceOpts,
	})
}
