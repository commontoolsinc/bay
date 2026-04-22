package cli

import (
	"fmt"
	"strings"

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

// topNewCmdSpec describes the differences between the shell/agent/cmd
// subcommands of `bay new`. Everything else (flags, RunE shape, workspace
// resolution) is identical and handled by newTopNewSurfaceCmd.
type topNewCmdSpec struct {
	use       string
	short     string
	long      string
	args      cobra.PositionalArgs
	buildOpts func(args []string, splitDir string, window, pane bool) (surfaceNewOpts, error)
}

func newTopNewSurfaceCmd(spec topNewCmdSpec) *cobra.Command {
	var wsFlag, dockFlag, splitDir string
	var window, pane bool

	cmd := &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Long:  spec.long,
		Args:  spec.args,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, err := spec.buildOpts(args, splitDir, window, pane)
			if err != nil {
				return err
			}
			eng, err := newEngine()
			if err != nil {
				return err
			}
			dockName, wsName, err := resolveSurfaceWorkspace(eng, wsFlag, dockFlag)
			if err != nil {
				return err
			}
			return runSurfaceNew(eng, dockName, wsName, opts)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (defaults to current)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (with --ws to disambiguate)")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&pane, "pane", false, "split into current window (shorthand for --split v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as a new tmux window (default)")

	return cmd
}

func newTopNewShellCmd() *cobra.Command {
	return newTopNewSurfaceCmd(topNewCmdSpec{
		use:   "shell [name]",
		short: "Create a shell surface",
		args:  cobra.MaximumNArgs(1),
		buildOpts: func(args []string, splitDir string, window, pane bool) (surfaceNewOpts, error) {
			opts := surfaceNewOpts{
				Type:     manifest.SurfaceTypeShell,
				SplitDir: resolveSplit(splitDir, window, pane),
			}
			if len(args) > 0 {
				if err := validateSurfaceName(args[0]); err != nil {
					return opts, err
				}
				opts.Name = args[0]
			}
			return opts, nil
		},
	})
}

func newTopNewAgentCmd() *cobra.Command {
	return newTopNewSurfaceCmd(topNewCmdSpec{
		use:   "agent [type] [name]",
		short: "Create an agent surface",
		long: `Create an agent surface. The agent type is optional — if omitted,
uses the dock's default agent.

  bay new agent                        dock's default agent
  bay new agent claude                 specific agent
  bay new agent codex codex-debug      specific agent with custom name
  bay new agent --ws auth-fix          in another workspace
  bay new agent --split v              as a vertical split instead of a window`,
		args: cobra.MaximumNArgs(2),
		buildOpts: func(args []string, splitDir string, window, pane bool) (surfaceNewOpts, error) {
			opts := surfaceNewOpts{
				Type:     manifest.SurfaceTypeAgent,
				SplitDir: resolveSplit(splitDir, window, pane),
			}
			if len(args) > 0 {
				opts.Agent = args[0]
			}
			if len(args) > 1 {
				if err := validateSurfaceName(args[1]); err != nil {
					return opts, err
				}
				opts.Name = args[1]
			}
			return opts, nil
		},
	})
}

func newTopNewCmdCmd() *cobra.Command {
	return newTopNewSurfaceCmd(topNewCmdSpec{
		use:   `cmd "<command>" [name]`,
		short: "Create a cmd surface running a command",
		long: `Create a surface that runs an arbitrary command.

  bay new cmd "npm test"
  bay new cmd "npm test" tests
  bay new cmd "tail -f log.txt" --window`,
		args: cobra.RangeArgs(1, 2),
		buildOpts: func(args []string, splitDir string, window, pane bool) (surfaceNewOpts, error) {
			opts := surfaceNewOpts{
				Type:     manifest.SurfaceTypeCmd,
				Command:  args[0],
				SplitDir: resolveSplit(splitDir, window, pane),
			}
			if len(args) > 1 {
				if err := validateSurfaceName(args[1]); err != nil {
					return opts, err
				}
				opts.Name = args[1]
			}
			return opts, nil
		},
	})
}

func newTopNewEditCmd() *cobra.Command {
	var wsFlag, dockFlag, editorFlag, splitDir string
	var window, pane bool

	cmd := &cobra.Command{
		Use:   "edit [workspace]",
		Short: "Launch a workspace-scoped editor surface",
		Long: `Launch the editor on a specific workspace directory (creates a tracked
editor surface). For a dock-scoped editor covering all workspaces,
use 'bay edit' instead.

  bay new edit                       current workspace
  bay new edit auth-fix              specific workspace
  bay new edit --ws auth-fix         same thing with a flag
  bay new edit --editor vim          use a specific editor this time
  bay new edit --pane                open as a split pane`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			target, err := editTargetFromArgs(args, wsFlag, dockFlag)
			if err != nil {
				return err
			}
			return runEditCreate(eng, target, editorFlag, resolveSplit(splitDir, window, pane))
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (defaults to current)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (with --ws to disambiguate)")
	cmd.Flags().StringVar(&editorFlag, "editor", "", "editor command (overrides config for this invocation)")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&pane, "pane", false, "split into current window (shorthand for --split v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as a new tmux window (default)")

	return cmd
}

// editTargetFromArgs builds a resolveTarget-compatible workspace target
// string from a positional arg plus --ws/--dock flags. Positional and flags
// are mutually exclusive (each is a way to name a workspace); --dock without
// --ws (or without a positional) is an error.
func editTargetFromArgs(args []string, wsFlag, dockFlag string) (string, error) {
	if len(args) > 0 {
		if wsFlag != "" || dockFlag != "" {
			return "", fmt.Errorf("cannot combine positional workspace with --ws/--dock")
		}
		return args[0], nil
	}
	if wsFlag == "" {
		if dockFlag != "" {
			return "", fmt.Errorf("--dock requires --ws or a positional workspace")
		}
		return "self", nil
	}
	if dockFlag != "" {
		return dockFlag + ":" + wsFlag, nil
	}
	return wsFlag, nil
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
	return resolveBareWs(eng, wsFlag)
}

// newTopCloseCmd is `bay close` — close a surface (alias: rm, hidden).
func newTopCloseCmd() *cobra.Command {
	var wsFlag, dockFlag string
	var force bool

	cmd := &cobra.Command{
		Use:     "close <name>",
		Aliases: []string{"rm"},
		Short:   "Close a surface",
		Long: `Close a surface by name. Use 'self' to target the current surface.

  bay close monitor              close "monitor" in the current workspace
  bay close w1:monitor           close "monitor" in workspace w1
  bay close labs:w1:monitor      fully-qualified
  bay close monitor --ws w1      same as w1:monitor
  bay close self                 close the current pane's surface
  bay rm shell-2                 same thing with the rm alias

Closing an agent surface prompts for confirmation when stdin is a
terminal — agents carry valuable conversation context. Use --force to
skip the prompt.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return runSurfaceClose(eng, args, wsFlag, dockFlag, force)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (disambiguates with --dock)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (only valid with --ws or a workspace prefix)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip the confirmation prompt for agent surfaces")

	return cmd
}

// newTopShowCmd is `bay show` — print surface details (alias: cat, hidden).
func newTopShowCmd() *cobra.Command {
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:     "show [name]",
		Aliases: []string{"cat"},
		Short:   "Show surface details (default: current)",
		Args:    cobra.MaximumNArgs(1),
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

// newTopRenameCmd is `bay rename` — rename the current tab.
//
// Design note: bay does not have an explicit entity for a tmux
// window. Surfaces are grouped into windows via their LayoutGroup
// field, and the window's tab name is derived from the first
// surface in the group. "bay rename foo" renames the tab, which
// means renaming the workspace (for primary windows) or the
// tab-owning surface (for secondary windows). If this mapping
// proves confusing — e.g., the user expects to rename a specific
// pane rather than the tab — we may need to introduce an explicit
// Window entity with its own name, separate from both workspaces
// and surfaces. The LayoutGroup integer would become a reference
// to a named Window record in the manifest.
func newTopRenameCmd() *cobra.Command {
	var dockFlag string

	cmd := &cobra.Command{
		Use:     "rename [name] <new-name>",
		Aliases: []string{"mv"},
		Short:   "Rename the current tab (workspace or secondary window)",
		Long: `Rename the current tab. With one arg, renames the current tab:
  - In a primary window: renames the workspace.
  - In a secondary window: renames the tab-owning surface.

  bay rename foo                       rename current tab to foo
  bay rename :foo                      same (leading colon is stripped)
  bay rename old-name new-name         rename workspace by name
  bay rename old-name new-name --dock d  rename in a specific dock`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			if len(args) == 1 {
				newName := strings.TrimPrefix(args[0], ":")

				// Determine if we're in a primary or secondary window.
				dockName, wsName, err := eng.ResolveSelf()
				if err != nil {
					return err
				}

				ws, err := eng.WsShow(dockName, wsName)
				if err != nil {
					return err
				}
				paneID, _ := eng.Tmux.CurrentPaneID()
				layoutGroup := 0
				for _, s := range ws.Surfaces {
					if s.Tmux != nil && s.Tmux.PaneID == paneID {
						layoutGroup = s.Tmux.LayoutGroup
						break
					}
				}

				if layoutGroup <= 1 {
					// Primary window — rename the workspace.
					return eng.WsRename(dockName, wsName, newName)
				}

				// Secondary window — rename the tab-owning surface
				// (first surface in this layout group by slice order).
				for _, s := range ws.Surfaces {
					if s.Tmux != nil && s.Tmux.LayoutGroup == layoutGroup {
						return eng.SurfaceRename(dockName, wsName, s.Name, newName)
					}
				}
				return fmt.Errorf("could not determine tab-owning surface")
			}

			// Two args: explicit workspace rename (existing behavior).
			source := args[0]
			newName := args[1]
			dockName, wsID, err := resolveWsArg(eng, source, dockFlag)
			if err != nil {
				return err
			}
			return eng.WsRename(dockName, wsID, newName)
		},
	}

	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (disambiguates a bare workspace name)")

	return cmd
}

func newTopDescribeCmd() *cobra.Command {
	var dockFlag string
	var clear bool

	cmd := &cobra.Command{
		Use:   "describe [name] [<description>]",
		Short: "Set the current workspace's description (alias for `bay ws describe`)",
		Long: `Set a short, free-form description for a workspace. Descriptions appear
in the workspace picker, bay ls, and bay tree; they do not affect tmux
tab names. Target around 40 characters (hard cap 80).

  bay describe "Login flow fixes"       set current workspace's description
  bay describe auth-fix "Login fixes"   set by workspace name
  bay describe --clear                  clear current workspace's description`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDescribe(args, dockFlag, clear)
		},
	}

	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (disambiguates a bare workspace name)")
	cmd.Flags().BoolVar(&clear, "clear", false, "clear the description")

	return cmd
}
