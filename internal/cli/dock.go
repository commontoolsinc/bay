package cli

import (
	"fmt"
	"strings"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func newDockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dock",
		Aliases: []string{"dk"},
		Short:   "Manage docks (tmux session groups)",
	}

	cmd.AddCommand(
		newDockNewCmd(),
		newDockLsCmd(),
		newDockShowCmd(),
		newDockTreeCmd(),
		newDockRenameCmd(),
		newDockCloseCmd(),
		newDockRecoverCmd(),
	)

	return cmd
}

func newDockNewCmd() *cobra.Command {
	var repo, agent, terminal string

	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new dock",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			if err := eng.DockNew(args[0], repo, agent, terminal); err != nil {
				return err
			}
			fmt.Printf("Dock %q created.\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&repo, "repo", "", "default repo for workspaces")
	cmd.Flags().StringVar(&agent, "agent", "", "default agent type")
	cmd.Flags().StringVar(&terminal, "terminal", "", "host terminal app (e.g. ghostty, iterm2)")

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
		Use:     "show <name>",
		Aliases: []string{"cat"},
		Short:   "Show dock details",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			name := args[0]

			m, _ := eng.LoadManifest()
			if m == nil {
				return fmt.Errorf("dock %q not found", name)
			}
			mDock := m.FindDock(name)
			if mDock == nil {
				return fmt.Errorf("dock %q not found", name)
			}

			fmt.Printf("Dock: %s\n", name)
			if mDock.Repo != "" {
				fmt.Printf("  repo:     %s\n", mDock.Repo)
			}
			if agent := eng.Config.ResolvedDockAgent(name, mDock.Agent); agent != "" {
				fmt.Printf("  agent:    %s\n", agent)
			}
			if agentArgs := eng.Config.ResolvedDockAgentArgs(name, mDock.AgentArgs); len(agentArgs) > 0 {
				fmt.Printf("  agent_args: %s\n", strings.Join(agentArgs, " "))
			}
			if terminal := eng.Config.ResolvedDockTerminal(name); terminal != "" {
				fmt.Printf("  terminal: %s\n", terminal)
			}

			// Session status
			exists, _ := eng.Tmux.HasSession(name)
			if exists {
				fmt.Println("  session:  running")
			} else {
				fmt.Println("  session:  not running")
			}

			// Workspace summary
			idle, active, done := 0, 0, 0
			for _, ws := range mDock.Workspaces {
				switch ws.Status {
				case manifest.WorkspaceStatusIdle:
					idle++
				case manifest.WorkspaceStatusActive:
					active++
				case manifest.WorkspaceStatusDone:
					done++
				}
			}
			total := len(mDock.Workspaces)
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

			return nil
		},
	}
}

func newDockTreeCmd() *cobra.Command {
	var longOutput bool

	cmd := &cobra.Command{
		Use:   "tree [name]",
		Short: "Tree view for a dock (default: current)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			var dockName string
			if len(args) > 0 {
				dockName = args[0]
			} else {
				sess, tmuxErr := eng.Tmux.CurrentSession()
				if tmuxErr != nil {
					return fmt.Errorf("not in a tmux session — pass a dock name")
				}
				dockName = sess
			}

			docks, err := eng.List()
			if err != nil {
				return err
			}

			repo := ""
			m, _ := eng.LoadManifest()
			if m != nil {
				if dock := m.FindDock(dockName); dock != nil {
					repo = dock.Repo
				} else {
					return fmt.Errorf("dock %q not found", dockName)
				}
			}

			view := BuildListView(docks, ListViewOptions{
				Focus:     ListFocus{Kind: FocusDock, Repo: repo, Dock: dockName},
				Recursive: true,
			})

			fmt.Print(FormatListView(view, longOutput, false))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "show extended details such as tmux IDs")

	return cmd
}

func newDockRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rename [old] <new>",
		Aliases: []string{"mv"},
		Short:   "Rename a dock (defaults to current)",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			var oldName, newName string
			if len(args) == 1 {
				dockName, _, err := eng.ResolveSelf()
				if err != nil {
					return fmt.Errorf("cannot determine current dock: %w", err)
				}
				oldName = dockName
				newName = args[0]
			} else {
				oldName = args[0]
				newName = args[1]
			}
			return eng.DockRename(oldName, newName)
		},
	}
}

func newDockCloseCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "close <name>",
		Aliases: []string{"rm"},
		Short:   "Close all workspaces in a dock",
		Args:    cobra.ExactArgs(1),
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
			result, err := eng.DockRecover(args[0])
			if err != nil {
				return err
			}
			if len(result.Recovered) == 0 {
				fmt.Printf("Dock %q: nothing to recover.\n", args[0])
			} else {
				fmt.Printf("Dock %q: recovered %s\n", args[0], strings.Join(result.Recovered, ", "))
			}
			printRecoveryWarnings(result.Warnings)
			fmt.Printf("Attach with: tmux attach -t %s\n", args[0])
			return nil
		},
	}
}
