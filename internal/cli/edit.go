package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func newEditCmd() *cobra.Command {
	var dockScope, wsScope bool
	var editorFlag, splitDir string
	var window, pane bool

	cmd := &cobra.Command{
		Use:   "edit [workspace]",
		Short: "Open editor on workspace or dock",
		Long: `Open your editor on the workspace or dock directory. Default scope
is dock (one editor for all workspaces). Use --ws for per-workspace.

  bay edit                    dock editor (default, all workspaces)
  bay edit --ws               workspace editor (current workspace only)
  bay edit --ws auth-fix      specific workspace
  bay edit --editor vim       use a specific editor this time
  bay edit --pane             split pane instead of new window

Editor resolution order:
  1. --editor flag (this invocation only)
  2. default_editor in config (set via 'bay config editor <name>')
  3. $VISUAL
  4. $EDITOR
  5. Probe: cursor, code, zed, nvim, vim`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			// Determine scope: --ws or --dock (default: dock).
			if wsScope || len(args) > 0 {
				sd := resolveSplit(splitDir, window, pane)
				target := "self"
				if len(args) > 0 {
					target = args[0]
				}
				return runEditCreate(eng, target, editorFlag, sd)
			}
			return runEditDock(eng, editorFlag)
		},
	}

	cmd.Flags().BoolVar(&dockScope, "dock", false, "dock-scoped editor (default)")
	cmd.Flags().BoolVar(&wsScope, "ws", false, "workspace-scoped editor")
	cmd.Flags().StringVar(&editorFlag, "editor", "", "editor command (overrides config for this invocation)")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v)")
	cmd.Flags().BoolVar(&pane, "pane", false, "split into current window (shorthand for --split v)")
	cmd.Flags().BoolVar(&window, "window", false, "open as a new tmux window")

	return cmd
}

// resolveSplit resolves the split direction. Default is a new window
// (empty string). --pane forces a vertical split. --split overrides
// the direction explicitly.
func resolveSplit(splitDir string, window, pane bool) string {
	if splitDir != "" {
		return splitDir
	}
	if pane {
		return "v"
	}
	return "" // window (default)
}

// runEditDock launches or focuses a dock-level editor that covers all
// workspaces. For terminal editors, creates a tracked surface in a tmux
// window at index 0. For GUI editors, launches fire-and-forget.
func runEditDock(eng *engine.Engine, editorOverride string) error {
	dockName, err := eng.Tmux.CurrentSession()
	if err != nil {
		return fmt.Errorf("not in a tmux session")
	}

	editorCmd, isGUI := resolveEditorWithOverride(eng.Config, editorOverride)
	if editorCmd == "" {
		return fmt.Errorf("no editor found; set [editor].command in config, or $VISUAL/$EDITOR")
	}

	// Determine the edit path — worktree parent dir so all workspaces are visible.
	editPath, err := eng.EditAllParentDir(dockName)
	if err != nil {
		// Fallback: use the first workspace path.
		paths, pathErr := eng.EditAll(dockName)
		if pathErr != nil || len(paths) == 0 {
			return fmt.Errorf("no workspaces to edit in dock %q", dockName)
		}
		editPath = paths[0]
	}

	if isGUI {
		// Fire and forget — re-running focuses the existing window.
		_, err = launchEditor(editorCmd, isGUI, []string{editPath})
		return err
	}

	return eng.DockEditorAdd(dockName, editorCmd, editPath)
}

// runEditCreate launches the editor on a single workspace.
// GUI editors launch and return (fire-and-forget). Terminal editors
// create a tracked surface in their own tmux pane via SurfaceAdd.
// Shared by `bay edit` and `bay new edit`.
func runEditCreate(eng *engine.Engine, target, editorOverride, splitDir string) error {
	dockName, wsID, err := resolveTarget(eng, target)
	if err != nil {
		return err
	}
	path, err := eng.Edit(dockName, wsID)
	if err != nil {
		return err
	}

	editorCmd, isGUI := resolveEditorWithOverride(eng.Config, editorOverride)
	if editorCmd == "" {
		return fmt.Errorf("no editor found; set [editor].command in config, or $VISUAL/$EDITOR")
	}

	if isGUI {
		// Fire and forget — GUI editors manage their own windows.
		_, err = launchEditor(editorCmd, isGUI, []string{path})
		if err != nil {
			return fmt.Errorf("launching editor: %w", err)
		}
		return nil
	}

	// Terminal editor: create a pane surface running the editor.
	fullCmd := editorCmd + " " + path
	return eng.SurfaceAdd(engine.SurfaceAddOptions{
		DockName: dockName,
		WsName:   wsID,
		Type:     manifest.SurfaceTypeEditor,
		Name:     "editor",
		Command:  fullCmd,
		SplitDir: splitDir,
	})
}

// knownEditors maps CLI command names to whether they're GUI apps.
var knownEditors = map[string]bool{
	"cursor": true,
	"code":   true,
	"zed":    true,
	"nvim":   false,
	"vim":    false,
}

// resolveEditor determines the editor command and whether it's GUI.
func resolveEditorWithOverride(cfg *config.Config, override string) (string, bool) {
	if override != "" {
		return override, knownEditors[baseCommand(override)]
	}
	return resolveEditor(cfg)
}

func resolveEditor(cfg *config.Config) (command string, isGUI bool) {
	lookup := func(cmd string) (string, bool) {
		return cmd, knownEditors[baseCommand(cmd)]
	}

	if cfg.DefaultEditor != "" {
		return lookup(cfg.DefaultEditor)
	}
	if v := os.Getenv("VISUAL"); v != "" {
		return lookup(v)
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return lookup(v)
	}
	for _, name := range []string{"cursor", "code", "zed", "nvim", "vim"} {
		if _, err := exec.LookPath(name); err == nil {
			return lookup(name)
		}
	}
	return "", false
}

// baseCommand extracts the base command name from a potentially full path or
// command with args (e.g. "/usr/bin/code" -> "code", "nvim -u NONE" -> "nvim").
func baseCommand(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return cmd
	}
	// Take the last path component
	base := parts[0]
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	return base
}

// launchEditor launches the editor with the given paths.
// Returns the PID for GUI editors (0 for terminal editors).
func launchEditor(editorCmd string, isGUI bool, paths []string) (int, error) {
	args := append(strings.Fields(editorCmd), paths...)
	c := exec.Command(args[0], args[1:]...)

	if isGUI {
		// GUI editors: detach, don't wait
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Start(); err != nil {
			return 0, err
		}
		return c.Process.Pid, nil
	}

	// Terminal editors: attach to terminal
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return 0, c.Run()
}
