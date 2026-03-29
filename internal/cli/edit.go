package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/spf13/cobra"
)

func newEditCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "edit [name|self]",
		Short: "Open workspace in editor",
		Long: `Open a workspace directory in your editor.

  bay edit           open current workspace
  bay edit auth-fix  open specific workspace
  bay edit --all     open all active workspaces

Editor resolution order:
  1. [editor].command in config
  2. $VISUAL
  3. $EDITOR
  4. Probe: cursor, code, zed, nvim, vim`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			var paths []string

			if all {
				paths, err = eng.EditAll()
				if err != nil {
					return err
				}
				if len(paths) == 0 {
					return fmt.Errorf("no active workspaces")
				}
			} else {
				target := "self"
				if len(args) > 0 {
					target = args[0]
				}
				dockName, wsID, resolveErr := resolveTarget(eng, target)
				if resolveErr != nil {
					return resolveErr
				}
				path, editErr := eng.Edit(dockName, wsID)
				if editErr != nil {
					return editErr
				}
				paths = []string{path}
			}

			editorCmd, isGUI := resolveEditor(eng.Config)
			if editorCmd == "" {
				return fmt.Errorf("no editor found; set [editor].command in config, or $VISUAL/$EDITOR")
			}

			if all && !isGUI && len(paths) > 1 {
				return fmt.Errorf("--all requires a GUI editor (cursor, code, zed); %s is a terminal editor", editorCmd)
			}

			return launchEditor(editorCmd, isGUI, paths)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "open all active workspaces")

	return cmd
}

// guiEditors are editor commands known to be GUI applications.
var guiEditors = map[string]bool{
	"cursor": true,
	"code":   true,
	"zed":    true,
}

// resolveEditor determines the editor command and whether it's GUI.
func resolveEditor(cfg *config.Config) (command string, isGUI bool) {
	// 1. Config
	if cfg.Editor.Command != "" {
		cmd := cfg.Editor.Command
		gui := guiEditors[cmd]
		if cfg.Editor.GUI != nil {
			gui = *cfg.Editor.GUI
		}
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
func launchEditor(editorCmd string, isGUI bool, paths []string) error {
	args := append(strings.Fields(editorCmd), paths...)
	c := exec.Command(args[0], args[1:]...)

	if isGUI {
		// GUI editors: detach, don't wait
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Start()
	}

	// Terminal editors: attach to terminal
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
