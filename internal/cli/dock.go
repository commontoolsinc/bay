package cli

import (
	"fmt"
	"strings"

	"github.com/commontoolsinc/bay/internal/engine"
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
		newDockShowCmd(),
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

			// If inside a dock, show only that dock
			currentSession, sessionErr := eng.Tmux.CurrentSession()
			if sessionErr == nil {
				for _, d := range docks {
					if d.Name == currentSession {
						fmt.Print(FormatDockTree([]engine.DockInfo{d}))
						return nil
					}
				}
			}

			// Otherwise show all docks
			fmt.Print(FormatDockTree(docks))
			return nil
		},
	}
}

func newDockShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show dock details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			name := args[0]
			dock, ok := eng.Config.Docks[name]
			if !ok {
				return fmt.Errorf("dock %q not found", name)
			}

			fmt.Printf("Dock: %s\n", name)
			if dock.Repo != "" {
				fmt.Printf("  repo:     %s\n", dock.Repo)
			}
			if dock.Agent != "" {
				fmt.Printf("  agent:    %s\n", dock.Agent)
			}
			if len(dock.AgentArgs) > 0 {
				fmt.Printf("  agent_args: %s\n", strings.Join(dock.AgentArgs, " "))
			}
			if dock.AgentConfigTemplate != "" {
				fmt.Printf("  template: %s\n", dock.AgentConfigTemplate)
			}

			// Session status
			exists, _ := eng.Tmux.HasSession(name)
			if exists {
				fmt.Println("  session:  running")
			} else {
				fmt.Println("  session:  not running")
			}

			// Workspace summary
			m, _ := eng.LoadManifest()
			if m != nil {
				if ds, ok := m.Docks[name]; ok {
					idle, active, done := 0, 0, 0
					for _, ws := range ds.Workspaces {
						switch ws.Status {
						case "idle":
							idle++
						case "active":
							active++
						case "done":
							done++
						}
					}
					total := len(ds.Workspaces)
					fmt.Printf("  workspaces: %d", total)
					if total > 0 {
						var parts []string
						if active > 0 {
							parts = append(parts, fmt.Sprintf("%d active", active))
						}
						if idle > 0 {
							parts = append(parts, fmt.Sprintf("%d idle", idle))
						}
						if done > 0 {
							parts = append(parts, fmt.Sprintf("%d done", done))
						}
						fmt.Printf(" (%s)", strings.Join(parts, ", "))
					}
					fmt.Println()
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
			recovered, err := eng.DockRecover(args[0])
			if err != nil {
				return err
			}
			if len(recovered) == 0 {
				fmt.Printf("Dock %q: nothing to recover.\n", args[0])
			} else {
				fmt.Printf("Dock %q: recovered %s\n", args[0], strings.Join(recovered, ", "))
			}
			fmt.Printf("Attach with: tmux attach -t %s\n", args[0])
			return nil
		},
	}
}
