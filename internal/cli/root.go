// Package cli implements the bay CLI using cobra.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	gitpkg "github.com/commontoolsinc/bay/internal/git"
	tmuxpkg "github.com/commontoolsinc/bay/internal/tmux"
	"github.com/spf13/cobra"
)

// cfgPath is set by cobra's --config persistent flag binding.
// Package-level var is required by cobra's StringVar API.
var cfgPath string

// bayPaths returns the standard paths, respecting the --config flag.
func bayPaths() config.Paths {
	p := config.DefaultPaths()
	if cfgPath != "" {
		p.ConfigFile = cfgPath
	}
	return p
}

// newEngine creates an Engine from the loaded config and real implementations.
func newEngine() (*engine.Engine, error) {
	p := bayPaths()
	cfg, err := config.Load(p.ConfigFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no config file found at %s\nRun 'bay setup' to get started.", p.ConfigFile)
		}
		return nil, fmt.Errorf("loading config: %w", err)
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

// NewRootCmd creates the root bay command.
func NewRootCmd(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "bay",
		Short: "Multi-session workspace management for tmux and git worktrees",
		Long:  "Bay manages concurrent workspaces across tmux windows and git worktrees.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

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
	winCmd := newWinCmd()
	winCmd.GroupID = "workspace"
	paneCmd := newPaneCmd()
	paneCmd.GroupID = "workspace"
	shellCmd := newShellCmd()
	shellCmd.GroupID = "workspace"
	editCmd := newEditCmd()
	editCmd.GroupID = "workspace"

	// Navigation commands
	goCmd := newGoCmd()
	goCmd.GroupID = "navigation"
	lsCmd := newLsCmd()
	lsCmd.GroupID = "navigation"
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
		winCmd,
		paneCmd,
		shellCmd,
		editCmd,
		goCmd,
		lsCmd,
		statusLineCmd,
		dockCmd,
		repoCmd,
		setupCmd,
		recoverCmd,
		doctorCmd,
		monitorCmd,
		addPromptCmd,
		versionCmd,
		newClosePaneCmd(), // hidden, for keybinding
	)

	RegisterCompletion(root)

	return root
}
