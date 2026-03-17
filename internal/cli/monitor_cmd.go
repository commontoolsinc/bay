package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mpsalisbury/bay/internal/config"
	"github.com/mpsalisbury/bay/internal/monitor"
	tmuxpkg "github.com/mpsalisbury/bay/internal/tmux"
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
	path := cfgPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}

	dataDir := config.DefaultDataDir()
	configDir := config.DefaultConfigDir()
	pidPath := dataDir + "/monitor.pid"
	manifestPath := dataDir + "/manifest.toml"
	patternsPath := configDir + "/bay-prompts.txt"
	interval := cfg.Monitor.EffectiveInterval()

	t := tmuxpkg.NewReal()
	return monitor.New(t, manifestPath, patternsPath, pidPath, interval), nil
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
			if err := monitor.WritePIDFile(config.DefaultDataDir()+"/monitor.pid", os.Getpid()); err != nil {
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
