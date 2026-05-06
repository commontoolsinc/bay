package cli

import "github.com/spf13/cobra"

func newAgentCmd() *cobra.Command {
	return newTopNewSurfaceCmd(topNewCmdSpec{
		use:   "agent [type] [name]",
		short: "Open an agent surface in a bay",
		long: `Launch an agent in a bay. The agent type is optional — if
omitted, uses the dock's default agent.

  bay agent                    dock's default agent as a split pane
  bay agent claude             specific agent
  bay agent codex my-codex     specific agent with custom name
  bay agent --window           as a new tmux window
  bay agent --bay b1           target a different bay`,
		args:      cobra.MaximumNArgs(2),
		buildOpts: agentSurfaceOpts,
	})
}
