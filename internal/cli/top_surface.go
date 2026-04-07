package cli

import (
	"fmt"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

// Top-level surface verbs. These are surface-biased shortcuts for the most
// common operations: `bay new`, `bay close`, `bay show`, `bay rename`. Each
// delegates to the same runSurfaceX helper that backs `bay sf X`, so the two
// forms always behave identically.
//
// Hidden Unix-style aliases (rm, cat, mv) ride along on close/show/rename for
// muscle memory but do not appear in `bay --help`.

// newTopNewCmd is `bay new` — a parent command that dispatches on positional
// kind to one of: shell, agent, cmd, edit.
func newTopNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new <kind>",
		Short: "Create a surface (shell, agent, cmd, edit)",
		Long: `Create a new surface in the current (or specified) workspace.

  bay new shell                       split a shell into the current workspace
  bay new shell logs --window         new tmux window named "logs"
  bay new agent claude                start a Claude agent surface
  bay new agent codex --ws auth-fix   start codex in another workspace
  bay new cmd "npm test" tests        run a command as a tracked surface
  bay new edit                        open the editor on the current workspace
  bay new edit auth-fix               open the editor on a specific workspace`,
	}

	cmd.AddCommand(
		newTopNewShellCmd(),
		newTopNewAgentCmd(),
		newTopNewCmdCmd(),
		newTopNewEditCmd(),
	)

	return cmd
}

func newTopNewShellCmd() *cobra.Command {
	var wsFlag, dockFlag, splitDir string
	var window bool

	cmd := &cobra.Command{
		Use:   "shell [name]",
		Short: "Create a shell surface",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			dockName, wsName, err := resolveSurfaceWorkspace(eng, wsFlag, dockFlag)
			if err != nil {
				return err
			}
			opts := surfaceNewOpts{
				Type:     manifest.SurfaceTypeShell,
				SplitDir: surfaceSplitDir(splitDir, window),
			}
			if len(args) > 0 {
				opts.Name = args[0]
			}
			return runSurfaceNew(eng, dockName, wsName, opts)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (defaults to current)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (with --ws to disambiguate)")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as a new tmux window instead of a split")

	return cmd
}

func newTopNewAgentCmd() *cobra.Command {
	var wsFlag, dockFlag, splitDir string
	var window bool

	cmd := &cobra.Command{
		Use:   "agent <agent> [name]",
		Short: "Create an agent surface",
		Long: `Create an agent surface running the named agent.

  bay new agent claude
  bay new agent codex codex-debug
  bay new agent claude --ws auth-fix --window`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			dockName, wsName, err := resolveSurfaceWorkspace(eng, wsFlag, dockFlag)
			if err != nil {
				return err
			}
			opts := surfaceNewOpts{
				Type:     manifest.SurfaceTypeAgent,
				Agent:    args[0],
				SplitDir: surfaceSplitDir(splitDir, window),
			}
			if len(args) > 1 {
				opts.Name = args[1]
			}
			return runSurfaceNew(eng, dockName, wsName, opts)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (defaults to current)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (with --ws to disambiguate)")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as a new tmux window instead of a split")

	return cmd
}

func newTopNewCmdCmd() *cobra.Command {
	var wsFlag, dockFlag, splitDir string
	var window bool

	cmd := &cobra.Command{
		Use:   `cmd "<command>" [name]`,
		Short: "Create a cmd surface running a command",
		Long: `Create a surface that runs an arbitrary command.

  bay new cmd "npm test"
  bay new cmd "npm test" tests
  bay new cmd "tail -f log.txt" --window`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			dockName, wsName, err := resolveSurfaceWorkspace(eng, wsFlag, dockFlag)
			if err != nil {
				return err
			}
			opts := surfaceNewOpts{
				Type:     manifest.SurfaceTypeCmd,
				Command:  args[0],
				SplitDir: surfaceSplitDir(splitDir, window),
			}
			if len(args) > 1 {
				opts.Name = args[1]
			}
			return runSurfaceNew(eng, dockName, wsName, opts)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (defaults to current)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (with --ws to disambiguate)")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as a new tmux window instead of a split")

	return cmd
}

func newTopNewEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit [workspace]",
		Short: "Launch the editor on a workspace (creates an editor surface)",
		Long: `Launch the editor on a workspace and register a tracked editor surface
so it appears in 'bay go'. With no positional, opens the current workspace.

This is equivalent to bare 'bay edit [workspace]'. Use 'bay edit' if you need
the --all, --set, or --show utility flags.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			target := "self"
			if len(args) > 0 {
				target = args[0]
			}
			return runEditCreate(eng, target)
		},
	}
	return cmd
}

// resolveSurfaceWorkspace resolves the (dock, ws) for a surface-creation
// command. With no flags it uses the current workspace; with --ws it looks up
// the workspace by name (with --dock to disambiguate).
func resolveSurfaceWorkspace(eng *engine.Engine, wsFlag, dockFlag string) (string, string, error) {
	if wsFlag == "" && dockFlag == "" {
		return eng.ResolveSelf()
	}
	if wsFlag == "" {
		return "", "", fmt.Errorf("--dock requires --ws")
	}
	if dockFlag != "" {
		return eng.ResolveWorkspace(dockFlag + ":" + wsFlag)
	}
	return eng.ResolveWorkspace(wsFlag)
}

// newTopCloseCmd is `bay close` — close a surface (alias: rm, hidden).
func newTopCloseCmd() *cobra.Command {
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:     "close [name]",
		Aliases: []string{"rm"},
		Short:   "Close a surface (or current tmux pane if not bay-managed)",
		Long: `Close a surface by name, or the current pane if no name given.

  bay close monitor              close "monitor" in the current workspace
  bay close w1:monitor           close "monitor" in workspace w1
  bay close labs:w1:monitor      fully-qualified
  bay close monitor --ws w1      same as w1:monitor
  bay close                      close the current pane
  bay rm shell-2                 same thing with the rm alias`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return runSurfaceClose(eng, args, wsFlag, dockFlag)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (disambiguates with --dock)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (only valid with --ws or a workspace prefix)")

	return cmd
}

// newTopShowCmd is `bay show` — print surface details (alias: cat, hidden).
func newTopShowCmd() *cobra.Command {
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:     "show <name>",
		Aliases: []string{"cat"},
		Short:   "Show surface details",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return runSurfaceShow(eng, args, wsFlag, dockFlag)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (disambiguates with --dock)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (only valid with --ws or a workspace prefix)")

	return cmd
}

// newTopRenameCmd is `bay rename` — rename a surface (alias: mv, hidden).
func newTopRenameCmd() *cobra.Command {
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:     "rename <old> <new>",
		Aliases: []string{"mv"},
		Short:   "Rename a surface",
		Long: `Rename a surface. The <old> name may include a workspace prefix.

  bay rename agent agent2              rename in current workspace
  bay rename w1:agent agent2           rename agent in workspace w1
  bay rename agent agent2 --ws w1      same as w1:agent`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return runSurfaceRename(eng, args, wsFlag, dockFlag)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (disambiguates with --dock)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (only valid with --ws or a workspace prefix)")

	return cmd
}
