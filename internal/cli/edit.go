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
	var all bool
	var editorFlag, splitDir string
	var window bool

	cmd := &cobra.Command{
		Use:   "edit [workspace]",
		Short: "Open workspace in editor",
		Long: `Open a workspace directory in your editor.

  bay edit                    open current workspace
  bay edit auth-fix           open specific workspace
  bay edit --all              open all workspaces in current dock
  bay edit --editor vim       open with a specific editor this time
  bay edit --split v          open as a vertical split instead of a new window

For editor configuration, see 'bay config editor'.

Editor resolution order:
  1. --editor flag (this invocation only)
  2. [editor].command in config (set via 'bay config editor <name>')
  3. $VISUAL
  4. $EDITOR
  5. Probe: cursor, code, zed, nvim, vim`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			sd := editSplitDir(splitDir, window)

			if all {
				return runEditAll(eng, editorFlag, sd)
			}

			target := "self"
			if len(args) > 0 {
				target = args[0]
			}
			return runEditCreate(eng, target, editorFlag, sd)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "open all active workspaces")
	cmd.Flags().StringVar(&editorFlag, "editor", "", "editor command (overrides config for this invocation)")
	cmd.Flags().StringVar(&splitDir, "split", "", "split direction (h or v) instead of a new window")
	cmd.Flags().BoolVar(&window, "window", false, "open as a new tmux window (default for terminal editors)")

	return cmd
}

// editSplitDir resolves the split direction for editor surfaces.
// Editors default to a new window (empty string) unlike other surfaces
// which default to a vertical split.
func editSplitDir(splitDir string, window bool) string {
	if window || splitDir == "" {
		return ""
	}
	return splitDir
}

// runEditCreate launches the editor on a single workspace as a tracked
// surface. GUI editors are detached and tracked by PID. Terminal editors
// get their own tmux pane via SurfaceAdd.
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
		_, err = launchEditor(editorCmd, isGUI, []string{path})
		if err != nil {
			return fmt.Errorf("launching editor: %w", err)
		}
		// Register the surface without a PID. GUI editor launchers
		// (code, cursor) typically fork and exit, so the launcher PID
		// is useless for liveness tracking. The surface stays until
		// the user closes it with bay close.
		appCmd := editorCmd + " " + path
		if err := eng.SurfaceAddGUI(dockName, wsID, "editor", appCmd, 0); err != nil {
			return fmt.Errorf("registering editor surface: %w", err)
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

// runEditAll launches the editor on every active workspace in the current
// dock. For terminal editors with multiple workspaces, the worktree parent
// directory is opened so the file tree shows all worktrees as subdirectories.
func runEditAll(eng *engine.Engine, editorOverride, splitDir string) error {
	dockName, sessionErr := eng.Tmux.CurrentSession()
	if sessionErr != nil {
		return fmt.Errorf("--all requires being inside a dock (tmux session)")
	}
	paths, err := eng.EditAll(dockName)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no active workspaces in dock %q", dockName)
	}

	editorCmd, isGUI := resolveEditorWithOverride(eng.Config, editorOverride)
	if editorCmd == "" {
		return fmt.Errorf("no editor found; set [editor].command in config, or $VISUAL/$EDITOR")
	}

	// Resolve the workspace to attach the editor surface to.
	wsName := ""
	if dock, ws, err := eng.ResolveSelf(); err == nil && dock == dockName {
		wsName = ws
	}
	if wsName == "" {
		m, err := eng.LoadManifest()
		if err == nil {
			if dock := m.FindDock(dockName); dock != nil && len(dock.Workspaces) > 0 {
				wsName = dock.Workspaces[0].Name
			}
		}
	}
	if wsName == "" {
		return fmt.Errorf("no workspace found in dock %q", dockName)
	}

	if isGUI {
		_, err = launchEditor(editorCmd, isGUI, paths)
		if err != nil {
			return err
		}
		appCmd := editorCmd + " " + strings.Join(paths, " ")
		return eng.SurfaceAddGUI(dockName, wsName, "editor", appCmd, 0)
	}

	// Terminal editor: open on parent dir so all worktrees are visible.
	openPath := paths[0]
	if len(paths) > 1 {
		parentDir, err := eng.EditAllParentDir(dockName)
		if err != nil {
			return fmt.Errorf("cannot determine worktree directory: %w", err)
		}
		openPath = parentDir
	}

	fullCmd := editorCmd + " " + openPath
	return eng.SurfaceAdd(engine.SurfaceAddOptions{
		DockName: dockName,
		WsName:   wsName,
		Type:     manifest.SurfaceTypeEditor,
		Name:     "editor",
		Command:  fullCmd,
		SplitDir: splitDir,
	})
}

// guiEditors are editor commands known to be GUI applications.
var guiEditors = map[string]bool{
	"cursor": true,
	"code":   true,
	"zed":    true,
}

// resolveEditor determines the editor command and whether it's GUI.
func resolveEditorWithOverride(cfg *config.Config, override string) (string, bool) {
	if override != "" {
		base := baseCommand(override)
		return override, guiEditors[base]
	}
	return resolveEditor(cfg)
}

func resolveEditor(cfg *config.Config) (command string, isGUI bool) {
	// 1. Config
	if cfg.DefaultEditor != "" {
		cmd := cfg.DefaultEditor
		gui := guiEditors[baseCommand(cmd)]
		return cmd, gui
	}

	// 2. $VISUAL
	if v := os.Getenv("VISUAL"); v != "" {
		base := baseCommand(v)
		gui := guiEditors[base]
		return v, gui
	}

	// 3. $EDITOR
	if v := os.Getenv("EDITOR"); v != "" {
		base := baseCommand(v)
		gui := guiEditors[base]
		return v, gui
	}

	// 4. Probe
	for _, name := range []string{"cursor", "code", "zed", "nvim", "vim"} {
		if _, err := exec.LookPath(name); err == nil {
			return name, guiEditors[name]
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
