// Package cli implements the bay CLI using cobra.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	gitpkg "github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/picker"
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
  bay new shell           create a shell surface
  bay new agent claude    create an agent surface
  bay go [query]          jump to a surface
  bay ls                  see everything
  bay close [name]        close a surface (or current pane)
  bay restart             restart current surface
  bay edit                open editor

Run 'bay help' for all commands, or 'bay help <command>' for details.
`

// NewRootCmd creates the root bay command.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "bay",
		Short:         "Multi-session workspace management for tmux and git worktrees",
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprint(cmd.OutOrStdout(), bayHelpTemplate)
		},
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path (default ~/.config/bay/config.toml)")

	// Define command groups
	root.AddGroup(
		&cobra.Group{ID: "workspace", Title: "Workspaces:"},
		&cobra.Group{ID: "surface", Title: "Surfaces:"},
		&cobra.Group{ID: "navigation", Title: "Navigation:"},
		&cobra.Group{ID: "infra", Title: "Infrastructure:"},
		&cobra.Group{ID: "other", Title: "Other:"},
	)

	// Workspace commands
	wsCmd := newWsCmd()
	wsCmd.GroupID = "workspace"

	// Surface commands (namespace + verbs + utility)
	surfaceCmd := newSurfaceCmd()
	surfaceCmd.GroupID = "surface"
	shellCmd := newShellCmd()
	shellCmd.GroupID = "surface"
	editCmd := newEditCmd()
	editCmd.GroupID = "surface"
	restartCmd := newRestartCmd()
	restartCmd.GroupID = "surface"
	topNewCmd := newTopNewCmd()
	topNewCmd.GroupID = "surface"
	topCloseCmd := newTopCloseCmd()
	topCloseCmd.GroupID = "surface"
	topShowCmd := newTopShowCmd()
	topShowCmd.GroupID = "surface"
	topRenameCmd := newTopRenameCmd()
	topRenameCmd.GroupID = "surface"

	// Navigation commands
	goCmd := newGoCmd()
	goCmd.GroupID = "navigation"
	lsCmd := newLsCmd()
	lsCmd.GroupID = "navigation"
	treeCmd := newTreeCmd()
	treeCmd.GroupID = "navigation"
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

	// Other commands.
	versionCmd := newVersionCmd(version)
	versionCmd.GroupID = "other"

	configCmd := newConfigCmd()
	configCmd.GroupID = "infra"

	root.AddCommand(
		wsCmd,
		surfaceCmd,
		shellCmd,
		editCmd,
		restartCmd,
		topNewCmd,
		topCloseCmd,
		topShowCmd,
		topRenameCmd,
		goCmd,
		lsCmd,
		treeCmd,
		pwdCmd,
		statusLineCmd,
		dockCmd,
		repoCmd,
		configCmd,
		setupCmd,
		recoverCmd,
		doctorCmd,
		monitorCmd,
		versionCmd,
		newAgentGuideCmd(), // hidden, for skill
	)

	RegisterCompletion(root)

	// Auto-start the background monitor on every command unless the
	// command (or one of its ancestors) opts out via the no-autostart
	// annotation. See monitor_autostart.go.
	installMonitorAutostart(root)

	return root
}
