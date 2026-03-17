package cli

import (
	"fmt"

	"github.com/mpsalisbury/bay/internal/engine"
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
				opts.Dock = dock
			}
			opts.Shell = shell

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
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "agent type override")
	cmd.Flags().BoolVar(&shell, "shell", false, "open shell instead of agent")

	return cmd
}

func newWsCloseCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "close <name|self>",
		Short: "Close a workspace and all its windows",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsID, err := resolveTarget(eng, args[0])
			if err != nil {
				return err
			}

			return eng.WsClose(dockName, wsID, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force close even if dirty")

	return cmd
}

func newWsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name|self>",
		Short: "Show workspace details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsID, err := resolveTarget(eng, args[0])
			if err != nil {
				return err
			}

			ws, err := eng.WsShow(dockName, wsID)
			if err != nil {
				return err
			}

			fmt.Printf("Workspace: %s (%s)\n", ws.Name, wsID)
			fmt.Printf("  Dock:   %s\n", dockName)
			fmt.Printf("  Type:   %s\n", ws.Type)
			fmt.Printf("  Path:   %s\n", ws.Path)
			fmt.Printf("  Branch: %s\n", ws.Branch)
			fmt.Printf("  PR:     %s\n", ws.PR)
			fmt.Printf("  Status: %s\n", ws.Status)
			fmt.Printf("  Windows: %d\n", len(ws.Windows))
			for _, w := range ws.Windows {
				fmt.Printf("    [%d] %s (tmux: %s, panes: %d)\n",
					w.ID, w.Name, w.TmuxWindowID, len(w.Panes))
			}
			return nil
		},
	}
}

func newWsUpdateCmd() *cobra.Command {
	var branch, pr, status string

	cmd := &cobra.Command{
		Use:   "update <name|self>",
		Short: "Update workspace metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
