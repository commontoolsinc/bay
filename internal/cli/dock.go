package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dock",
		Short: "Manage docks (tmux session groups)",
	}

	cmd.AddCommand(
		newDockNewCmd(),
		newDockLsCmd(),
		newDockCloseCmd(),
		newDockRecoverCmd(),
	)

	return cmd
}

func newDockNewCmd() *cobra.Command {
	var repo, agent, template string

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new dock",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			if err := eng.DockNew(args[0], repo, agent, template); err != nil {
				return err
			}
			fmt.Printf("Dock %q created.\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "default repo for workspaces")
	cmd.Flags().StringVar(&agent, "agent", "", "default agent type")
	cmd.Flags().StringVar(&template, "template", "", "agent config template path")

	return cmd
}

func newDockLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List docks and their workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			docks, err := eng.List()
			if err != nil {
				return err
			}
			for _, d := range docks {
				fmt.Printf("Dock: %s (agent=%s, repo=%s)\n", d.Name, d.Agent, d.Repo)
				for _, ws := range d.Workspaces {
					fmt.Printf("  %-5s %-20s %-35s %-6s %s\n",
						ws.ID, ws.Name, ws.Branch, ws.Status, ws.PR)
				}
			}
			return nil
		},
	}
}

func newDockCloseCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "close <name>",
		Short: "Close all workspaces in a dock",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return eng.DockClose(args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force close even if workspaces are dirty")

	return cmd
}

func newDockRecoverCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recover <name>",
		Short: "Recover a single dock",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			attachCmd, err := eng.DockRecover(args[0])
			if err != nil {
				return err
			}
			fmt.Println("Recovered. Attach with:")
			fmt.Printf("  %s\n", attachCmd)
			return nil
		},
	}
}
