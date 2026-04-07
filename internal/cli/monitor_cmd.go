package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"syscall"

	"github.com/commontoolsinc/bay/internal/config"
	gitpkg "github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/monitor"
	tmuxpkg "github.com/commontoolsinc/bay/internal/tmux"
	"github.com/spf13/cobra"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Manage the pane monitor",
	}

	cmd.AddCommand(
		newMonitorStartCmd(),
		newMonitorStopCmd(),
		newMonitorStatusCmd(),
		newMonitorRunCmd(),
	)

	return cmd
}

func newMonitorWithConfig() (*monitor.Monitor, error) {
	p := bayPaths()
	cfg, err := config.Load(p.ConfigFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			cfg = config.DefaultConfig()
		} else {
			return nil, err
		}
	}

	t := tmuxpkg.NewReal()
	g := gitpkg.NewReal()
	return monitor.NewWithGit(t, g, p.ManifestFile, p.PatternsFile, p.PIDFile, cfg.Monitor.EffectiveInterval()), nil
}

func newMonitorStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the pane monitor as a background process",
		RunE: func(cmd *cobra.Command, args []string) error {
			mon, err := newMonitorWithConfig()
			if err != nil {
				return err
			}
			return mon.Start()
		},
	}
}

func newMonitorRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "run",
		Short:  "Run the monitor in the foreground (used by 'start')",
		Hidden: true, // internal subcommand
		RunE: func(cmd *cobra.Command, args []string) error {
			mon, err := newMonitorWithConfig()
			if err != nil {
				return err
			}

			// Write our own PID
			if err := monitor.WritePIDFile(bayPaths().PIDFile, os.Getpid()); err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle signals for clean shutdown
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
			go func() {
				<-sigCh
				cancel()
			}()

			return mon.Run(ctx)
		},
	}
}

func newMonitorStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the pane monitor",
		RunE: func(cmd *cobra.Command, args []string) error {
			mon, err := newMonitorWithConfig()
			if err != nil {
				return err
			}
			return mon.Stop()
		},
	}
}

func newMonitorStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check if the monitor is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			mon, err := newMonitorWithConfig()
			if err != nil {
				return err
			}
			running, pid, err := mon.Status()
			if err != nil {
				return err
			}
			if running {
				fmt.Printf("Monitor running (pid %d)\n", pid)
			} else {
				fmt.Println("Monitor not running")
			}
			return nil
		},
	}
}
