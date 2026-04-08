package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/commontoolsinc/bay/internal/config"
	"github.com/spf13/cobra"
)

// newConfigCmd is the parent for everything that touches the bay
// config file. The long help reproduces the config-format docs that
// used to live in `bay help config`, so users can still discover
// the schema via `bay config --help` or `bay help config`.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage bay's config file",
		Long:  configHelpText,
	}

	cmd.AddCommand(
		newConfigEditCmd(),
		newConfigShowCmd(),
		newConfigPathCmd(),
		newConfigEditorCmd(),
	)

	return cmd
}

func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open the bay config file in your editor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			editorCmd, _ := resolveEditor(eng.Config)
			if editorCmd == "" {
				return fmt.Errorf("no editor found; set with 'bay config editor <name>', or via $VISUAL/$EDITOR")
			}
			path := bayPaths().ConfigFile
			args = append(strings.Fields(editorCmd), path)
			c := exec.Command(args[0], args[1:]...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the effective bay config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			out, err := configShowString(eng.Config)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path to the bay config file",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(configPathString(bayPaths().ConfigFile))
		},
	}
}

func newConfigEditorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "editor [name]",
		Short: "Print or set the default editor",
		Long: `With no argument, prints the resolved editor command and type.
With a positional argument, sets the editor command in the config.

  bay config editor             show the current editor
  bay config editor cursor      set the editor to "cursor"
  bay config editor "nvim -u NONE"  set with arguments`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				fmt.Println(configEditorGetString(eng.Config))
				return nil
			}
			if err := runConfigEditorSet(bayPaths().ConfigFile, args[0]); err != nil {
				return err
			}
			fmt.Printf("Editor set to %q\n", args[0])
			return nil
		},
	}
}

// runConfigEditorSet loads the config at configPath, updates the
// editor command, and saves. Used by `bay config editor <name>`.
// Replaces the old `bay edit --set` flag.
func runConfigEditorSet(configPath, name string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg = config.DefaultConfig()
		} else {
			return fmt.Errorf("loading config: %w", err)
		}
	}
	cfg.Editor.Command = name
	if err := config.Save(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	return nil
}

// configEditorGetString returns the human-readable rendering of the
// resolved editor: "<command> (terminal|GUI)" or a no-editor message.
// Format matches the old `bay edit --show` output exactly so anyone
// scripting against it sees the same lines.
func configEditorGetString(cfg *config.Config) string {
	editorCmd, isGUI := resolveEditor(cfg)
	if editorCmd == "" {
		return "No editor configured or detected."
	}
	editorType := "terminal"
	if isGUI {
		editorType = "GUI"
	}
	return fmt.Sprintf("%s (%s)", editorCmd, editorType)
}

// configPathString returns the config file path. Trivial wrapper that
// exists so the cobra Run can stay tiny and the test can target the
// pure-string layer.
func configPathString(p string) string {
	return p
}

// configShowString returns the effective config as a TOML document.
// Re-uses the same encoder bay's config save path uses, so the output
// round-trips back through `bay config edit`.
func configShowString(cfg *config.Config) (string, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return "", fmt.Errorf("encoding config: %w", err)
	}
	return buf.String(), nil
}
