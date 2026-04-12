package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/nav"
	"github.com/commontoolsinc/bay/internal/picker"
	"github.com/spf13/cobra"
)

func newSurfaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "surface",
		Aliases: []string{"sf"},
		Short:   "Manage surfaces (agent, shell, cmd panes within a workspace)",
	}

	cmd.AddCommand(
		newSurfaceNewCmd(),
		newSurfaceCloseCmd(),
		newSurfaceRestartCmd(),
		newSurfaceLsCmd(),
		newSurfaceShowCmd(),
		newSurfaceRenameCmd(),
		newSurfaceGoCmd(),
		newSurfaceNextCmd(),
		newSurfacePrevCmd(),
	)

	return cmd
}

func newSurfaceNewCmd() *cobra.Command {
	var opts surfaceNewOpts
	var shell, window bool
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:   "new [name]",
		Short: "Add a surface to a workspace",
		Long: `Add a surface to a workspace. The positional is the new surface's
display name; --ws/--dock select the target workspace.

  bay surface new                          split a shell into the current workspace
  bay surface new logs                     new surface named "logs"
  bay surface new --window                 new tmux window instead of split
  bay surface new --ws auth-fix            target a different workspace
  bay sf new --agent codex                 add an agent surface`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			if len(args) > 0 {
				if err := validateSurfaceName(args[0]); err != nil {
					return err
				}
				opts.Name = args[0]
			}

			dockName, wsName, err := resolveSurfaceWorkspace(eng, wsFlag, dockFlag)
			if err != nil {
				return err
			}

			// Derive surface type from the flags. Default to shell.
			switch {
			case shell:
				opts.Type = manifest.SurfaceTypeShell
			case opts.Command != "":
				opts.Type = manifest.SurfaceTypeCmd
			case opts.Agent != "":
				opts.Type = manifest.SurfaceTypeAgent
			default:
				opts.Type = manifest.SurfaceTypeShell
			}

			opts.SplitDir = surfaceSplitDir(opts.SplitDir, window)

			return runSurfaceNew(eng, dockName, wsName, opts)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (defaults to current)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (with --ws to disambiguate)")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "agent type")
	cmd.Flags().BoolVar(&shell, "shell", false, "open a shell")
	cmd.Flags().StringVar(&opts.Command, "cmd", "", "command to run")
	cmd.Flags().StringVar(&opts.SplitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as new tmux window instead of split")

	return cmd
}

// surfaceNewOpts collects the options accepted by all surface-creation
// commands (sf new + the top-level bay new). Type is required; Name defaults
// to a per-type label. SplitDir is "h", "v", or "" (new tmux window).
type surfaceNewOpts struct {
	Type     manifest.SurfaceType
	Agent    string // for SurfaceTypeAgent
	Command  string // for SurfaceTypeCmd
	Name     string // surface display name; defaults derived from Type
	SplitDir string
}

// surfaceSplitDir resolves the final tmux split direction. --window forces a
// new window (empty string). Otherwise, an empty splitDir defaults to "v".
func surfaceSplitDir(splitDir string, window bool) string {
	if window {
		return ""
	}
	if splitDir == "" {
		return "v"
	}
	return splitDir
}

// validateSurfaceName rejects names containing a colon. Users who type
// `bay new shell w1:logs` expecting colon-path semantics would otherwise
// land a surface literally named "w1:logs" in the current workspace.
func validateSurfaceName(name string) error {
	if strings.Contains(name, ":") {
		return fmt.Errorf("surface name %q cannot contain ':' (use --ws/--dock to specify a workspace)", name)
	}
	return nil
}

// defaultCmdName derives a surface name from a command string.
// Uses the first word, and if it looks like a path, just the base name.
// Falls back to "cmd" if the command is empty.
func defaultCmdName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "cmd"
	}
	return filepath.Base(fields[0])
}

// runSurfaceNew creates a new surface in (dockName, wsName). Shared by
// `bay sf new` and the top-level `bay new` verbs. The caller is responsible
// for resolving (dockName, wsName) and setting opts.Type.
func runSurfaceNew(eng *engine.Engine, dockName, wsName string, opts surfaceNewOpts) error {
	if err := validateSurfaceName(opts.Name); err != nil {
		return err
	}

	name := opts.Name
	if name == "" {
		switch opts.Type {
		case manifest.SurfaceTypeAgent:
			name = "agent"
		case manifest.SurfaceTypeCmd:
			name = defaultCmdName(opts.Command)
		default:
			name = "shell"
		}
	}
	return eng.SurfaceAdd(engine.SurfaceAddOptions{
		DockName: dockName,
		WsName:   wsName,
		Type:     opts.Type,
		Name:     name,
		Agent:    opts.Agent,
		Command:  opts.Command,
		SplitDir: opts.SplitDir,
	})
}

func newSurfaceCloseCmd() *cobra.Command {
	var wsFlag, dockFlag string
	var force bool

	cmd := &cobra.Command{
		Use:     "close <name>",
		Aliases: []string{"rm"},
		Short:   "Close a surface",
		Long: `Close a surface by name. Use 'self' to target the current surface.

  bay sf close monitor              close "monitor" in the current workspace
  bay sf close w1:monitor           close "monitor" in workspace w1
  bay sf close labs:w1:monitor      fully-qualified
  bay sf close monitor --ws w1      same as w1:monitor
  bay sf close self                 close the current pane's surface
  bay sf rm shell-2                 same thing with the rm alias

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

// runSurfaceClose closes a named surface. Shared by `bay sf close` and the
// top-level `bay close`. Requires an explicit name — `close` is destructive
// and we don't want a bare invocation to silently close the current pane.
// Use `bay close self` to target the current surface.
//
// Agent surfaces prompt for confirmation when stdin is a TTY (unless force
// is true). Other surface types close without prompting.
func runSurfaceClose(eng *engine.Engine, args []string, wsFlag, dockFlag string, force bool) error {
	if len(args) == 0 {
		if wsFlag != "" || dockFlag != "" {
			return fmt.Errorf("--ws/--dock require a surface name")
		}
		return fmt.Errorf("specify a surface name (or 'self' to close the current surface)")
	}
	dockName, wsName, sName, err := resolveSurfaceArgOrSelf(eng, args[0], wsFlag, dockFlag)
	if err != nil {
		return err
	}

	// Confirmation prompt for agent surfaces. Skipped entirely when --force
	// is set; otherwise we look up the surface type and ask
	// confirmAgentClose (which handles the TTY check and prompt itself,
	// or returns the test stub's answer).
	if !force {
		ws, err := eng.WsShow(dockName, wsName)
		if err != nil {
			return err
		}
		s := ws.FindSurface(sName)
		if s != nil && s.Type == manifest.SurfaceTypeAgent {
			if !confirmAgentClose(s.Name) {
				fmt.Fprintln(os.Stderr, "not closing.")
				return nil // user declined; not an error
			}
		}
	}

	return eng.SurfaceClose(dockName, wsName, sName, force)
}

func newSurfaceRestartCmd() *cobra.Command {
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:   "restart [name]",
		Short: "Restart a surface's process",
		Long: `Restart a surface by name, or the current surface if no name given.

  bay sf restart agent              restart "agent" in the current workspace
  bay sf restart w1:agent           restart "agent" in workspace w1
  bay sf restart agent --ws w1      same as w1:agent
  bay sf restart                    restart current surface`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return runSurfaceRestart(eng, args, wsFlag, dockFlag)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (disambiguates with --dock)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (only valid with --ws or a workspace prefix)")

	return cmd
}

// runSurfaceRestart restarts a named surface, or the current pane's surface
// if no name was given. Shared by `bay sf restart`, `bay restart`, and the
// top-level surface verbs. Restart is non-destructive, so the bare no-arg
// form is preserved (unlike `close`).
func runSurfaceRestart(eng *engine.Engine, args []string, wsFlag, dockFlag string) error {
	if len(args) == 0 {
		if wsFlag != "" || dockFlag != "" {
			return fmt.Errorf("--ws/--dock require a surface name")
		}
		// Bare invocation: restart the current pane via the self keyword.
		args = []string{"self"}
	}
	dockName, wsName, sName, err := resolveSurfaceArgOrSelf(eng, args[0], wsFlag, dockFlag)
	if err != nil {
		return err
	}
	return eng.SurfaceRestart(dockName, wsName, sName)
}

func newSurfaceGoCmd() *cobra.Command {
	var index int
	var nextWaiting bool

	cmd := &cobra.Command{
		Use:   "go [query]",
		Short: "Navigate to a surface within the current workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return surfaceGo(eng, args, index, nextWaiting)
		},
	}

	cmd.Flags().IntVar(&index, "index", 0, "jump to surface by 1-based index")
	cmd.Flags().BoolVar(&nextWaiting, "next-waiting", false, "jump to next waiting surface")

	return cmd
}

func newSurfaceNextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Switch to the next surface in the current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return surfaceCycle(eng, true)
		},
	}
}

func newSurfacePrevCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prev",
		Short: "Switch to the previous surface in the current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return surfaceCycle(eng, false)
		},
	}
}

func newSurfaceLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List surfaces in the current workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsName, err := eng.ResolveSelf()
			if err != nil {
				return fmt.Errorf("not in a bay workspace")
			}

			eng.SyncAll()
			ws, err := eng.WsShow(dockName, wsName)
			if err != nil {
				return err
			}

			if len(ws.Surfaces) == 0 {
				fmt.Println("No surfaces in this workspace.")
				return nil
			}

			for _, s := range ws.Surfaces {
				line := fmt.Sprintf("%-12s %s", s.Name, string(s.Type))
				if s.Agent != nil && *s.Agent != "" {
					line += fmt.Sprintf("  agent=%s", *s.Agent)
				}
				if s.Command != nil && *s.Command != "" {
					line += fmt.Sprintf("  cmd=%s", *s.Command)
				}
				fmt.Println(line)
			}
			return nil
		},
	}
}

func newSurfaceShowCmd() *cobra.Command {
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

// runSurfaceShow prints details for a named surface. Shared by `bay sf show`
// and the top-level `bay show`. Accepts the `self` keyword, and a bare
// no-arg invocation defaults to the current pane's surface.
func runSurfaceShow(eng *engine.Engine, args []string, wsFlag, dockFlag string) error {
	target := "self"
	if len(args) > 0 {
		target = args[0]
	}
	dockName, wsName, sName, err := resolveSurfaceArgOrSelf(eng, target, wsFlag, dockFlag)
	if err != nil {
		return err
	}

	eng.SyncAll()
	ws, err := eng.WsShow(dockName, wsName)
	if err != nil {
		return err
	}

	s := ws.FindSurface(sName)
	if s == nil {
		return fmt.Errorf("surface %q not found in workspace %q", sName, wsName)
	}

	fmt.Printf("Surface: %s\n", s.Name)
	fmt.Printf("  type:    %s\n", s.Type)
	fmt.Printf("  backend: %s\n", s.Backend)
	if s.Tmux != nil {
		if s.Tmux.WindowID != "" {
			fmt.Printf("  window:  %s\n", s.Tmux.WindowID)
		}
		if s.Tmux.PaneID != "" {
			fmt.Printf("  pane:    %s\n", s.Tmux.PaneID)
		}
	}
	if s.Agent != nil && *s.Agent != "" {
		fmt.Printf("  agent:   %s\n", *s.Agent)
	}
	if s.Command != nil && *s.Command != "" {
		fmt.Printf("  command: %s\n", *s.Command)
	}
	return nil
}

func newSurfaceRenameCmd() *cobra.Command {
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:     "rename [old] <new>",
		Aliases: []string{"mv"},
		Short:   "Rename a surface (defaults to current)",
		Long: `Rename a surface. With one arg, renames the current surface.

  bay sf rename agent2                    rename current surface
  bay sf rename agent agent2              rename in current workspace
  bay sf rename w1:agent agent2           rename agent in workspace w1
  bay sf rename agent agent2 --ws w1      same as w1:agent`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				args = []string{"self", args[0]}
			}
			return runSurfaceRename(eng, args, wsFlag, dockFlag)
		},
	}

	cmd.Flags().StringVar(&wsFlag, "ws", "", "workspace name (disambiguates with --dock)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (only valid with --ws or a workspace prefix)")

	return cmd
}

// runSurfaceRename renames a surface. The first arg is the old name (which
// may include a workspace prefix or be the `self` keyword); the second is the
// new name. Shared by `bay sf rename` and the top-level `bay rename`.
//
// Precondition: args must contain exactly two elements. Cobra's ExactArgs(2)
// enforces this in production; callers from tests should pass the same.
func runSurfaceRename(eng *engine.Engine, args []string, wsFlag, dockFlag string) error {
	dockName, wsName, oldName, err := resolveSurfaceArgOrSelf(eng, args[0], wsFlag, dockFlag)
	if err != nil {
		return err
	}
	return eng.SurfaceRename(dockName, wsName, oldName, args[1])
}

// surfaceGo implements the surface picker / direct jump logic.
func surfaceGo(eng *engine.Engine, args []string, index int, nextWaiting bool) error {
	dockName, wsName, err := eng.ResolveSelf()
	if err != nil {
		return fmt.Errorf("not in a bay workspace")
	}

	ws, err := eng.WsShow(dockName, wsName)
	if err != nil {
		return err
	}

	currentPaneID, _ := eng.Tmux.CurrentPaneID()
	waitingWindows, _ := eng.Tmux.WaitingWindowIDs(dockName)
	entries := nav.CollectSurfaces(ws, currentPaneID, waitingWindows)

	if len(entries) == 0 {
		fmt.Println("No surfaces in this workspace.")
		return nil
	}

	if nextWaiting {
		target, _ := nav.NextWaitingSurface(entries)
		if target == nil {
			fmt.Println("No waiting surfaces in this workspace.")
			return nil
		}
		if err := focusSurface(eng, target, dockName, wsName); err != nil {
			return err
		}
		return nil
	}

	// Direct jump by index.
	if index > 0 {
		target := nav.SurfaceByIndex(entries, index)
		if target == nil {
			return fmt.Errorf("no surface at index %d (workspace has %d surfaces)", index, len(entries))
		}
		return focusSurface(eng, target, dockName, wsName)
	}

	// Filter by query.
	query := ""
	if len(args) > 0 {
		query = args[0]
	}
	if query != "" {
		entries = filterSurfaceEntries(entries, query)
	}

	switch len(entries) {
	case 0:
		fmt.Println("No matching surfaces.")
		return nil
	case 1:
		return focusSurface(eng, &entries[0], dockName, wsName)
	default:
		return pickSurface(eng, entries, dockName, wsName)
	}
}

// surfaceCycle moves to next/prev surface in the current workspace and
// flashes the new position via the cycling indicator.
func surfaceCycle(eng *engine.Engine, forward bool) error {
	dockName, wsName, err := eng.ResolveSelf()
	if err != nil {
		return fmt.Errorf("not in a bay workspace")
	}

	ws, err := eng.WsShow(dockName, wsName)
	if err != nil {
		return err
	}

	currentPaneID, _ := eng.Tmux.CurrentPaneID()
	waitingWindows, _ := eng.Tmux.WaitingWindowIDs(dockName)
	entries := nav.CollectSurfaces(ws, currentPaneID, waitingWindows)

	if len(entries) < 2 {
		return nil
	}

	var target *nav.SurfaceEntry
	if forward {
		target, _ = nav.NextSurface(entries)
	} else {
		target, _ = nav.PrevSurface(entries)
	}
	if err := focusSurface(eng, target, dockName, wsName); err != nil {
		return err
	}
	return nil
}

// focusSurface switches focus to a surface and records it as last-focused.
func focusSurface(eng *engine.Engine, entry *nav.SurfaceEntry, dockName, wsName string) error {
	// GUI surface: launch the editor CLI to activate its window.
	if entry.AppCommand != "" {
		_ = eng.SetLastFocused(dockName, wsName, entry.ID)
		return focusGUISurface(entry)
	}
	// Tmux surface: select window + pane.
	if entry.WindowID != "" {
		if err := eng.Tmux.SelectWindow(entry.WindowID); err != nil {
			return err
		}
	}
	if entry.PaneID != "" {
		if err := eng.Tmux.SelectPane(entry.PaneID); err != nil {
			return err
		}
	}
	_ = eng.SetLastFocused(dockName, wsName, entry.ID)
	return nil
}

// focusGUISurface activates a GUI application by re-running its CLI command.
// Editors like cursor and code reuse their existing window when launched
// on an already-open path.
func focusGUISurface(entry *nav.SurfaceEntry) error {
	args := strings.Fields(entry.AppCommand)
	if len(args) == 0 {
		return nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

// pickSurface shows the built-in picker for surface selection.
func pickSurface(eng *engine.Engine, entries []nav.SurfaceEntry, dockName, wsName string) error {
	items := make([]picker.Item, len(entries))
	for i, e := range entries {
		items[i] = picker.Item{
			Display: formatSurfaceEntry(e),
			Value:   i,
		}
	}

	selected, err := defaultPicker.Pick(items, picker.Options{Prompt: "surface> "})
	if err != nil || selected < 0 {
		return nil // cancelled
	}
	return focusSurface(eng, &entries[selected], dockName, wsName)
}

// formatSurfaceEntry formats a surface entry for picker display.
func formatSurfaceEntry(e nav.SurfaceEntry) string {
	s := fmt.Sprintf("%-10s %s", e.Name, e.Type)
	if e.Current {
		s += "  *"
	}
	if e.Waiting {
		s += "  WAITING"
	}
	return s
}

// filterSurfaceEntries filters surfaces by name or type substring.
func filterSurfaceEntries(entries []nav.SurfaceEntry, query string) []nav.SurfaceEntry {
	q := strings.ToLower(query)
	var result []nav.SurfaceEntry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Type), q) {
			result = append(result, e)
		}
	}
	return result
}
