package cli

import (
	"fmt"
	"os"
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
		newSurfaceGoCmd(),
		newSurfaceNextCmd(),
		newSurfacePrevCmd(),
	)

	return cmd
}

func newSurfaceNewCmd() *cobra.Command {
	var agent, cmdStr, name, splitDir string
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

			var surfaceType manifest.SurfaceType
			surfaceName := name
			switch {
			case shell:
				surfaceType = manifest.SurfaceTypeShell
				if surfaceName == "" {
					surfaceName = "shell"
				}
			case cmdStr != "":
				surfaceType = manifest.SurfaceTypeCmd
				if surfaceName == "" {
					surfaceName = "cmd"
				}
			case agent != "":
				surfaceType = manifest.SurfaceTypeAgent
				if surfaceName == "" {
					surfaceName = "agent"
				}
			default:
				surfaceType = manifest.SurfaceTypeShell
				if surfaceName == "" {
					surfaceName = "shell"
				}
			}

			dir := splitDir
			if window {
				dir = "" // empty = new tmux window
			} else if dir == "" {
				dir = "v" // default split direction
			}

			return eng.SurfaceAdd(dockName, wsName, surfaceType, surfaceName, agent, cmdStr, dir)
		},
	}

	cmd.Flags().StringVar(&agent, "agent", "", "agent type")
	cmd.Flags().BoolVar(&shell, "shell", false, "open a shell")
	cmd.Flags().StringVar(&cmdStr, "cmd", "", "command to run")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as new tmux window instead of split")
	cmd.Flags().StringVar(&name, "name", "", "surface name")

	return cmd
}

func newSurfaceCloseCmd() *cobra.Command {
	var surfaceName string

	cmd := &cobra.Command{
		Use:   "close [workspace]",
		Short: "Close a surface",
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
			dockName, wsName, sName, err := resolveSurfaceTarget(eng, target, surfaceName)
			if err != nil {
				return err
			}

			return eng.SurfaceClose(dockName, wsName, sName)
		},
	}

	cmd.Flags().StringVar(&surfaceName, "surface", "", "surface name")

	return cmd
}

func newSurfaceRestartCmd() *cobra.Command {
	var surfaceName string

	cmd := &cobra.Command{
		Use:   "restart [workspace]",
		Short: "Restart a surface's process",
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
			dockName, wsName, sName, err := resolveSurfaceTarget(eng, target, surfaceName)
			if err != nil {
				return err
			}

			return eng.SurfaceRestart(dockName, wsName, sName)
		},
	}

	cmd.Flags().StringVar(&surfaceName, "surface", "", "surface name")

	return cmd
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
				return focusSurface(eng, &entries[idx])
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
		return focusSurface(eng, target)
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
		return focusSurface(eng, &entries[0])
	default:
		return pickSurface(eng, entries)
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
	return focusSurface(eng, target)
}

// focusSurface switches tmux focus to a surface's pane.
func focusSurface(eng *engine.Engine, entry *nav.SurfaceEntry) error {
	if entry.WindowID != "" {
		if err := eng.Tmux.SelectWindow(entry.WindowID); err != nil {
			return err
		}
	}
	if entry.PaneID != "" {
		return eng.Tmux.SelectPane(entry.PaneID)
	}
	return nil
}

// pickSurface shows the built-in picker for surface selection.
func pickSurface(eng *engine.Engine, entries []nav.SurfaceEntry) error {
	items := make([]picker.Item, len(entries))
	for i, e := range entries {
		items[i] = picker.Item{
			Display: formatSurfaceEntry(e),
			Value:   i,
		}
	}

	selected, err := picker.Run(items, picker.Options{Prompt: "surface> "}, os.Stdin, os.Stdout)
	if err != nil || selected < 0 {
		return nil // cancelled
	}
	return focusSurface(eng, &entries[selected])
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

// resolveSurfaceTarget resolves a target to (dock, workspace, surface name).
func resolveSurfaceTarget(eng *engine.Engine, target, explicitSurface string) (string, string, string, error) {
	dockName, wsName, err := resolveTarget(eng, target)
	if err != nil {
		return "", "", "", err
	}

	ws, err := eng.WsShow(dockName, wsName)
	if err != nil {
		return "", "", "", err
	}
	if len(ws.Surfaces) == 0 {
		return "", "", "", fmt.Errorf("no surfaces in workspace %q", wsName)
	}

	if explicitSurface != "" {
		if ws.FindSurface(explicitSurface) == nil {
			return "", "", "", fmt.Errorf("surface %q not found in workspace %q", explicitSurface, wsName)
		}
		return dockName, wsName, explicitSurface, nil
	}

	// Match current tmux pane.
	if target == "self" {
		paneID, tmuxErr := eng.Tmux.CurrentPaneID()
		if tmuxErr == nil {
			for _, s := range ws.Surfaces {
				if s.Tmux != nil && s.Tmux.PaneID == paneID {
					return dockName, wsName, s.Name, nil
				}
			}
		}
	}

	// Default: first surface.
	return dockName, wsName, ws.Surfaces[0].Name, nil
}
