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

var (
	cfgPath string
)

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

	root.AddCommand(
		newDockCmd(),
		newRepoCmd(),
		newWsCmd(),
		newWinCmd(),
		newPaneCmd(),
		newGoCmd(),
		newLsCmd(),
		newEditCmd(),
		newShellCmd(),
		newStatusLineCmd(),
		newRecoverCmd(),
		newDoctorCmd(),
		newSetupCmd(),
		newMonitorCmd(),
		newAddPromptCmd(),
		newVersionCmd(version),
	)

	RegisterCompletion(root)

	return root
}
