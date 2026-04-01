package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const agentGuideContent = `# Bay — Workspace Management

Bay manages concurrent git worktrees and tmux windows. Each workspace
is an isolated copy of a repo with its own branch, tmux window, and
shell. Bay handles creation, navigation, recovery after reboot, and
cleanup.

## Mental model

repo (local git checkout)
  → dock (tmux session grouping workspaces for a project)
    → workspace (git worktree + tmux window)
      → window (tmux tab, multiple per workspace)
        → pane (split within a window)

## Key commands

### Create and manage workspaces

    bay ws new [dock]                  # new worktree + shell (default)
    bay ws new [dock] --agent          # new worktree + dock's default agent
    bay ws new [dock] --agent codex    # new worktree + specific agent
    bay ws new [dock] --branch <name>  # new worktree + create git branch
    bay ws close <name>               # close workspace (safety checks)
    bay ws close --done               # close all done workspaces
    bay ws show [name]                # workspace details (default: current)
    bay ws rename <name> <new>        # rename display name

### Windows, panes, and shells

    bay win open [workspace]          # new window (default: current workspace)
    bay win close [self|workspace]    # close a window
    bay shell                         # split shell pane in current window
    bay shell <name>                  # new shell window for workspace
    bay edit                          # open current workspace in editor
    bay edit --all                    # open all workspaces in editor

### Navigation

    bay ls                            # list all repos/docks/workspaces
    bay go                            # fuzzy-pick any window (fzf)
    bay go --next-waiting             # jump to next waiting agent

### Metadata

Bay auto-detects git branches. You only need to manually set:

    bay ws update <name> --pr <number>    # PR number (not in git)
    bay ws update <name> --status done    # mark work complete

### Infrastructure

    bay repo add <name> <path>        # register a local repo
    bay repo add <name> <path> --url <git-url>  # clone + register
    bay dock new <name> --repo <r>    # create a dock
    bay recover                       # rebuild tmux state after reboot
    bay doctor                        # health checks

### Cleanup

    bay ws close <name> --force       # skip safety checks
    bay dock close <name>             # close all workspaces in dock
    bay repo remove <name> --force    # remove repo + docks + workspaces

## Workspace agents

When an agent is launched in a workspace (via --agent), bay automatically
injects instructions into the agent's config file. The agent does not
need to be taught about bay separately.

## Machine-readable output

    bay ls --json                     # JSON array of dock objects
    bay ws show <name> --json         # JSON workspace with windows/panes
`

func newAgentGuideCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "agent-guide",
		Short:  "Print bay instructions for agents",
		Hidden: true, // used by skills, not direct user invocation
		Args:   cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(agentGuideContent)
		},
	}
}
