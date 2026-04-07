package cli

import (
	"fmt"
	"os"
	"os/exec"
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

	cmd := &cobra.Command{
		Use:   "new [workspace]",
		Short: "Add a surface to a workspace",
		Long: `Add a surface to a workspace.

  bay surface new              split pane in current workspace
  bay surface new --window     new tmux window in current workspace
  bay surface new auth-fix     new surface for that workspace
  bay sf new --agent codex     add an agent surface`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			var dockName, wsName string
			if len(args) > 0 {
				dockName, wsName, err = resolveTarget(eng, args[0])
			} else {
				dockName, wsName, err = eng.ResolveSelf()
			}
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

	cmd.Flags().StringVar(&opts.Agent, "agent", "", "agent type")
	cmd.Flags().BoolVar(&shell, "shell", false, "open a shell")
	cmd.Flags().StringVar(&opts.Command, "cmd", "", "command to run")
	cmd.Flags().StringVar(&opts.SplitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as new tmux window instead of split")
	cmd.Flags().StringVar(&opts.Name, "name", "", "surface name")

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

// runSurfaceNew creates a new surface in (dockName, wsName). Shared by
// `bay sf new` and the top-level `bay new` verbs. The caller is responsible
// for resolving (dockName, wsName) and setting opts.Type.
func runSurfaceNew(eng *engine.Engine, dockName, wsName string, opts surfaceNewOpts) error {
	name := opts.Name
	if name == "" {
		switch opts.Type {
		case manifest.SurfaceTypeAgent:
			name = "agent"
		case manifest.SurfaceTypeCmd:
			name = "cmd"
		default:
			name = "shell"
		}
	}
	return eng.SurfaceAdd(dockName, wsName, opts.Type, name, opts.Agent, opts.Command, opts.SplitDir)
}

func newSurfaceCloseCmd() *cobra.Command {
	var wsFlag, dockFlag string

	cmd := &cobra.Command{
		Use:     "close [name]",
		Aliases: []string{"rm"},
		Short:   "Close a surface (or current tmux pane if not bay-managed)",
		Long: `Close a surface by name, or the current pane if no name given.

  bay sf close monitor              close "monitor" in the current workspace
  bay sf close w1:monitor           close "monitor" in workspace w1
  bay sf close labs:w1:monitor      fully-qualified
  bay sf close monitor --ws w1      same as w1:monitor
  bay sf close                      close the current pane
  bay sf rm shell-2                 same thing with the rm alias`,
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

// runSurfaceClose closes a named surface. Shared by `bay sf close` and the
// top-level `bay close`. Requires an explicit name — `close` is destructive
// and we don't want a bare invocation to silently close the current pane.
// Use `bay close self` to target the current surface.
func runSurfaceClose(eng *engine.Engine, args []string, wsFlag, dockFlag string) error {
	if len(args) == 0 {
		return fmt.Errorf("specify a surface name (or 'self' to close the current surface)")
	}
	dockName, wsName, sName, err := resolveSurfaceArgOrSelf(eng, args[0], wsFlag, dockFlag)
	if err != nil {
		return err
	}
	return eng.SurfaceClose(dockName, wsName, sName)
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
	// Bare invocation: restart the current pane via the self keyword.
	if len(args) == 0 && wsFlag == "" && dockFlag == "" {
		args = []string{"self"}
	}
	if len(args) == 0 {
		return fmt.Errorf("--ws/--dock require a surface name")
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

// runSurfaceShow prints details for a named surface. Shared by `bay sf show`
// and the top-level `bay show`. Accepts the `self` keyword.
func runSurfaceShow(eng *engine.Engine, args []string, wsFlag, dockFlag string) error {
	dockName, wsName, sName, err := resolveSurfaceArgOrSelf(eng, args[0], wsFlag, dockFlag)
	if err != nil {
		return err
	}

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
		Use:     "rename <old> <new>",
		Aliases: []string{"mv"},
		Short:   "Rename a surface",
		Long: `Rename a surface. The <old> name may include a workspace prefix.

  bay sf rename agent agent2              rename in current workspace
  bay sf rename w1:agent agent2           rename agent in workspace w1
  bay sf rename agent agent2 --ws w1      same as w1:agent`,
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

// runSurfaceRename renames a surface. The first arg is the old name (which
// may include a workspace prefix or be the `self` keyword); the second is the
// new name. Shared by `bay sf rename` and the top-level `bay rename`.
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
	entries := nav.CollectSurfaces(ws, eng.Tmux, currentPaneID)

	if len(entries) == 0 {
		fmt.Println("No surfaces in this workspace.")
		return nil
	}

	// Jump to next waiting surface.
	if nextWaiting {
		cur := -1
		for i, e := range entries {
			if e.Current {
				cur = i
				break
			}
		}
		n := len(entries)
		for offset := 1; offset <= n; offset++ {
			idx := (cur + offset) % n
			if entries[idx].Waiting {
				return focusSurface(eng, &entries[idx], dockName, wsName)
			}
		}
		fmt.Println("No waiting surfaces in this workspace.")
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

// surfaceCycle moves to next/prev surface in the current workspace.
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
	entries := nav.CollectSurfaces(ws, eng.Tmux, currentPaneID)

	if len(entries) < 2 {
		return nil // nothing to cycle to
	}

	var target *nav.SurfaceEntry
	if forward {
		target = nav.NextSurface(entries)
	} else {
		target = nav.PrevSurface(entries)
	}
	if target == nil {
		return nil
	}
	return focusSurface(eng, target, dockName, wsName)
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

