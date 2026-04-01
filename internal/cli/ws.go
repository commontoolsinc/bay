package cli

import (
	"encoding/json"
	"fmt"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/spf13/cobra"
)

func newWsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ws",
		Aliases: []string{"workspace"},
		Short:   "Manage workspaces",
	}

	cmd.AddCommand(
		newWsNewCmd(),
		newWsCloseCmd(),
		newWsShowCmd(),
		newWsUpdateCmd(),
		newWsRenameCmd(),
	)

	return cmd
}

func newWsNewCmd() *cobra.Command {
	var opts engine.WsNewOptions
	var shell bool

	cmd := &cobra.Command{
		Use:   "new [dock]",
		Short: "Create a new workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			if len(args) > 0 {
				opts.Dock = args[0]
			} else {
				// Infer dock from current tmux session
				dock, tmuxErr := eng.Tmux.CurrentSession()
				if tmuxErr != nil {
					return fmt.Errorf("not in a tmux session; specify dock name explicitly")
				}
				if _, ok := eng.Config.Docks[dock]; !ok {
					return fmt.Errorf("current tmux session %q is not a bay dock; specify dock name explicitly", dock)
				}
				opts.Dock = dock
			}

			// Shell-first default: if neither --agent nor --shell was
			// explicitly passed, default to shell mode.
			if !cmd.Flags().Changed("agent") && !cmd.Flags().Changed("shell") {
				opts.Shell = true
			} else {
				opts.Shell = shell
			}

			// Bare --agent (no value): use dock's default agent.
			// --agent <name>: use that specific agent.
			// Both cases: opts.Agent is already set by the flag binding.

			ws, err := eng.WsNew(opts)
			if err != nil {
				return err
			}
			fmt.Printf("Workspace %s created in dock %s (path: %s)\n",
				ws.Name, opts.Dock, ws.Path)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Repo, "repo", "", "repo name")
	cmd.Flags().StringVar(&opts.Dir, "dir", "", "external directory (creates external workspace)")
	cmd.Flags().StringVar(&opts.Name, "name", "", "display name")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "agent type (bare --agent uses dock default)")
	cmd.Flags().BoolVar(&shell, "shell", false, "open shell instead of agent")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "create and checkout a git branch in the worktree")
	cmd.Flags().Lookup("agent").NoOptDefVal = ""

	return cmd
}

func newWsCloseCmd() *cobra.Command {
	var force, done bool

	cmd := &cobra.Command{
		Use:   "close [name|self]",
		Short: "Close a workspace and all its windows",
		Long: `Close a workspace and all its windows.

  bay ws close w1          close a specific workspace
  bay ws close self        close the current workspace
  bay ws close --done      close all workspaces with status "done"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			if done {
				// Batch close all done workspaces
				dockName := ""
				sess, tmuxErr := eng.Tmux.CurrentSession()
				if tmuxErr == nil {
					if _, ok := eng.Config.Docks[sess]; ok {
						dockName = sess
					}
				}

				closed, skipped, closeErr := eng.WsCloseByStatus(dockName, "done", force)
				for _, c := range closed {
					fmt.Printf("Closed %s\n", c)
				}
				for _, s := range skipped {
					fmt.Printf("Skipped %s\n", s)
				}
				if len(skipped) > 0 && !force {
					fmt.Println("\nUse --force to close dirty workspaces.")
				}
				return closeErr
			}

			if len(args) == 0 {
				return fmt.Errorf("specify a workspace to close (bay ws close <name>) or use --done to close all finished workspaces")
			}

			dockName, wsID, err := resolveTarget(eng, args[0])
			if err != nil {
				return err
			}

			return eng.WsClose(dockName, wsID, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force close even if dirty")
	cmd.Flags().BoolVar(&done, "done", false, "close all workspaces with status done")

	return cmd
}

func newWsShowCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "show [name|self]",
		Short: "Show workspace details (default: current)",
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
			dockName, wsID, err := resolveTarget(eng, target)
			if err != nil {
				return err
			}

			ws, err := eng.WsShow(dockName, wsID)
			if err != nil {
				return err
			}
			wsInfo, err := eng.WorkspaceInfo(dockName, wsID)
			if err != nil {
				return err
			}
			wsInfo.DefaultAgent = ws.AgentOverride
			if wsInfo.DefaultAgent == "" {
				wsInfo.DefaultAgent = eng.Config.Docks[dockName].Agent
			}

			if jsonOutput {
				out := map[string]interface{}{
					"id":            wsID,
					"name":          wsInfo.Name,
					"repo":          ws.Repo,
					"dock":          dockName,
					"type":          wsInfo.Type,
					"path":          wsInfo.Path,
					"branch":        wsInfo.Branch,
					"pr":            wsInfo.PR,
					"status":        wsInfo.Status,
					"sync_status":   wsInfo.SyncStatus,
					"default_agent": wsInfo.DefaultAgent,
					"windows":       wsInfo.Windows,
				}
				data, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			fmt.Print(FormatWorkspaceShow(ws.Repo, dockName, wsInfo, false))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

func newWsUpdateCmd() *cobra.Command {
	var branch, pr, status string

	cmd := &cobra.Command{
		Use:   "update <name|self>",
		Short: "Update workspace metadata (at least one of --branch, --pr, or --status is required)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("branch") && !cmd.Flags().Changed("pr") && !cmd.Flags().Changed("status") {
				return fmt.Errorf("at least one of --branch, --pr, or --status is required")
			}

			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsID, err := resolveTarget(eng, args[0])
			if err != nil {
				return err
			}

			var bp, pp, sp *string
			if cmd.Flags().Changed("branch") {
				bp = &branch
			}
			if cmd.Flags().Changed("pr") {
				pp = &pr
			}
			if cmd.Flags().Changed("status") {
				sp = &status
			}

			return eng.WsUpdate(dockName, wsID, bp, pp, sp)
		},
	}

	cmd.Flags().StringVar(&branch, "branch", "", "branch name")
	cmd.Flags().StringVar(&pr, "pr", "", "PR number")
	cmd.Flags().StringVar(&status, "status", "", "status (idle|active|done)")

	return cmd
}

func newWsRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <name|self> <new-name>",
		Short: "Rename a workspace display name",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsID, err := resolveTarget(eng, args[0])
			if err != nil {
				return err
			}

			return eng.WsRename(dockName, wsID, args[1])
		},
	}
}

// resolveTarget resolves "self" or a workspace query.
func resolveTarget(eng *engine.Engine, target string) (string, string, error) {
	if target == "self" {
		return eng.ResolveSelf()
	}
	return eng.ResolveWorkspace(target)
}
