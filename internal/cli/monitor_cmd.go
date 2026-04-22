package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/commontoolsinc/bay/internal/monitor"
	"github.com/spf13/cobra"
)

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Manage the pane monitor",
		// Don't auto-start the monitor when the user is explicitly
		// running monitor subcommands — `monitor stop` auto-starting
		// then stopping is absurd, `monitor status` should be a pure
		// inspector, and `monitor run` is the monitor itself.
		Annotations: map[string]string{noMonitorAutostartAnnotation: "true"},
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
	eng, err := newEngine()
	if err != nil {
		return nil, err
	}
	p := bayPaths()
	cfg := eng.Config
	mon := monitor.NewWithGit(eng.Tmux, eng.Git, p.ManifestFile, p.PatternsFile, p.PIDFile, cfg.Monitor.EffectiveInterval())
	mon.SetEngine(eng)
	return mon, nil
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

			err = mon.Run(ctx)
			// context.Canceled is the normal shutdown path (SIGINT/
			// SIGTERM received). Don't report it as an error — the
			// monitor shares stderr with the terminal, so "Error:
			// context canceled" would confuse users running
			// `bay monitor stop`.
			if err != nil && err == context.Canceled {
				return nil
			}
			return err
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
			fmt.Println(monitorStatusString())
			return nil
		},
	}
}

// monitorStatusString returns the one-line status of the pane monitor.
// Shared by `bay monitor status` and the palette's "Monitor status" entry.
// Any error (config load, status probe) is folded into the string so
// callers can always display it directly.
func monitorStatusString() string {
	mon, err := newMonitorWithConfig()
	if err != nil {
		return fmt.Sprintf("Monitor status unavailable: %v", err)
	}
	running, pid, err := mon.Status()
	if err != nil {
		return fmt.Sprintf("Monitor status unavailable: %v", err)
	}
	if running {
		return fmt.Sprintf("Monitor running (pid %d)", pid)
	}
	return "Monitor not running"
}
