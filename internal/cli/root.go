// Package cli implements the bay CLI using cobra.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/picker"
	gitpkg "github.com/commontoolsinc/bay/internal/git"
	tmuxpkg "github.com/commontoolsinc/bay/internal/tmux"
	"github.com/spf13/cobra"
)

// cfgPath is set by cobra's --config persistent flag binding.
// Package-level var is required by cobra's StringVar API.
var cfgPath string

// defaultPicker is the picker implementation used by navigation commands.
// Can be replaced for testing or to swap in an alternative (e.g. fzf).
var defaultPicker picker.Interface = &picker.Builtin{}

// bayPaths returns the standard paths, respecting the --config flag.
func bayPaths() config.Paths {
	p := config.DefaultPaths()
	if cfgPath != "" {
		p.ConfigFile = cfgPath
	}
	return p
}

// newEngine creates an Engine from the loaded config and real implementations.
// If no config file exists, returns an engine with an empty default config.
func newEngine() (*engine.Engine, error) {
	p := bayPaths()
	cfg, err := config.Load(p.ConfigFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg = config.DefaultConfig()
		} else {
			return nil, fmt.Errorf("loading config: %w", err)
		}
	}
	errs := cfg.Validate()
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "config warning: %s\n", e)
		}
	}

	t := tmuxpkg.NewReal()
	g := gitpkg.NewReal()

	return engine.New(cfg, p.ConfigFile, p.ManifestFile, p.ArchiveFile, t, g), nil
}

const bayHelpTemplate = `Bay — workspace management for tmux and git worktrees.

Quick start:
  bay ws new              create a workspace
  bay go [query]          jump to a surface
  bay ws go [query]       jump to a workspace
  bay ls                  see everything
  bay edit                open editor
  bay shell               open shell
  bay restart             restart current surface

Run 'bay help <command>' for details on any command.
Run 'bay help --all' for a complete command list.
`

// NewRootCmd creates the root bay command.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "bay",
		Short:         "Multi-session workspace management for tmux and git worktrees",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	var helpAll bool
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if helpAll {
			// Full cobra help.
			cmd.SetHelpTemplate(cmd.UsageTemplate())
			cmd.Help()
		} else if cmd == root {
			// Focused help for root command.
			fmt.Fprint(cmd.OutOrStdout(), bayHelpTemplate)
		} else {
			// Default help for subcommands.
			cmd.Help()
		}
	})
	root.Flags().BoolVar(&helpAll, "all", false, "show all commands")
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path (default ~/.config/bay/config.toml)")

	// Define command groups
	root.AddGroup(
		&cobra.Group{ID: "workspace", Title: "Workspaces:"},
		&cobra.Group{ID: "navigation", Title: "Navigation:"},
		&cobra.Group{ID: "infra", Title: "Infrastructure:"},
		&cobra.Group{ID: "other", Title: "Other:"},
	)

	// Workspace commands
	wsCmd := newWsCmd()
	wsCmd.GroupID = "workspace"
	surfaceCmd := newSurfaceCmd()
	surfaceCmd.GroupID = "workspace"
	shellCmd := newShellCmd()
	shellCmd.GroupID = "workspace"
	editCmd := newEditCmd()
	editCmd.GroupID = "workspace"
	restartCmd := newRestartCmd()
	restartCmd.GroupID = "workspace"

	// Navigation commands
	goCmd := newGoCmd()
	goCmd.GroupID = "navigation"
	lsCmd := newLsCmd()
	lsCmd.GroupID = "navigation"
	pwdCmd := newPwdCmd()
	pwdCmd.GroupID = "navigation"
	statusLineCmd := newStatusLineCmd()
	statusLineCmd.GroupID = "navigation"

	// Infrastructure commands
	dockCmd := newDockCmd()
	dockCmd.GroupID = "infra"
	repoCmd := newRepoCmd()
	repoCmd.GroupID = "infra"
	setupCmd := newSetupCmd()
	setupCmd.GroupID = "infra"
	recoverCmd := newRecoverCmd()
	recoverCmd.GroupID = "infra"
	doctorCmd := newDoctorCmd()
	doctorCmd.GroupID = "infra"
	monitorCmd := newMonitorCmd()
	monitorCmd.GroupID = "infra"

	// Other commands
	addPromptCmd := newAddPromptCmd()
	addPromptCmd.GroupID = "other"
	versionCmd := newVersionCmd(version)
	versionCmd.GroupID = "other"

	root.AddCommand(
		wsCmd,
		surfaceCmd,
		shellCmd,
		editCmd,
		restartCmd,
		goCmd,
		lsCmd,
		pwdCmd,
		statusLineCmd,
		dockCmd,
		repoCmd,
		setupCmd,
		recoverCmd,
		doctorCmd,
		monitorCmd,
		addPromptCmd,
		versionCmd,
		newAgentGuideCmd(), // hidden, for skill
	)

	RegisterCompletion(root)

	return root
}
