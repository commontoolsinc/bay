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
	var all, showEditor bool
	var setEditor string

	cmd := &cobra.Command{
		Use:   "edit [name|self]",
		Short: "Open workspace in editor",
		Long: `Open a workspace directory in your editor.

  bay edit              open current workspace
  bay edit auth-fix     open specific workspace
  bay edit --all        open all workspaces in current dock
  bay edit --set cursor set your default editor
  bay edit --show       show which editor would be used

Editor resolution order:
  1. [editor].command in config (set with --set)
  2. $VISUAL
  3. $EDITOR
  4. Probe: cursor, code, zed, nvim, vim`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			// Handle --set: save editor preference and return
			if setEditor != "" {
				if err := eng.SetEditor(setEditor); err != nil {
					return err
				}
				fmt.Printf("Editor set to %q\n", setEditor)
				return nil
			}

			// Handle --show: print resolved editor and exit
			if showEditor {
				editorCmd, isGUI := resolveEditor(eng.Config)
				if editorCmd == "" {
					fmt.Println("No editor configured or detected.")
				} else {
					editorType := "terminal"
					if isGUI {
						editorType = "GUI"
					}
					fmt.Printf("%s (%s)\n", editorCmd, editorType)
				}
				return nil
			}

			var paths []string

			if all {
				// Scope to current dock
				dockName, sessionErr := eng.Tmux.CurrentSession()
				if sessionErr != nil {
					return fmt.Errorf("--all requires being inside a dock (tmux session)")
				}
				paths, err = eng.EditAll(dockName)
				if err != nil {
					return err
				}
				if len(paths) == 0 {
					return fmt.Errorf("no active workspaces in dock %q", dockName)
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

			// For --all with terminal editors (vim, nvim), open the worktree
			// parent directory instead of individual paths. The file tree
			// shows all worktrees as subdirectories.
			if all && !isGUI && len(paths) > 1 {
				dockName, _ := eng.Tmux.CurrentSession()
				parentDir, err := eng.EditAllParentDir(dockName)
				if err != nil {
					return fmt.Errorf("cannot determine worktree directory: %w", err)
				}
				paths = []string{parentDir}
			}

			return launchEditor(editorCmd, isGUI, paths)
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "open all active workspaces")
	cmd.Flags().StringVar(&setEditor, "set", "", "set default editor (e.g., cursor, code, nvim)")
	cmd.Flags().BoolVar(&showEditor, "show", false, "show which editor would be used")

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
