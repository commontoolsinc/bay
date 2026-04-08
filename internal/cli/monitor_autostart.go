package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// noMonitorAutostartAnnotation, when set on a cobra command, tells the
// root command's PersistentPreRunE not to auto-start the monitor for
// that command. Used for high-frequency or read-only commands like
// status-line, completion, monitor *, version, agent-guide.
const noMonitorAutostartAnnotation = "bay-no-monitor-autostart"

// forceMonitorAutostartAnnotation overrides an inherited
// no-autostart annotation. When walking from the invoked command up
// to the root, the FIRST annotation hit wins — so a child command
// can opt back IN to auto-start even when its parent (or any
// ancestor) opts out. Used by `bay monitor add-prompt`, which lives
// under the no-autostart `monitor` parent but actively wants the
// monitor running so the new pattern takes effect.
const forceMonitorAutostartAnnotation = "bay-force-monitor-autostart"

// ensureMonitorFn is the function the auto-start hook calls. It's a
// package var so tests can swap it without forking a real process.
// The default implementation reads the PID file, checks the process,
// and forks the monitor if it's not running. Failures are silent —
// the auto-start path runs in front of every CLI command, and a noisy
// warning per invocation would be worse than the broken-monitor case
// (which `bay doctor` reports anyway).
var ensureMonitorFn = ensureMonitor

func ensureMonitor() {
	mon, err := newMonitorWithConfig()
	if err != nil {
		return
	}
	running, _, _ := mon.Status()
	if running {
		return
	}
	_ = mon.Start()
}

// shouldAutostartMonitor walks from the invoked command up toward
// the root and returns based on the FIRST annotation it finds:
//
//   - forceMonitorAutostartAnnotation=true → return true (opt back in)
//   - noMonitorAutostartAnnotation=true    → return false (opt out)
//   - neither                              → continue walking
//
// Walking leaf-to-root means the closer-to-leaf annotation wins,
// so a child like `bay monitor add-prompt` can override its parent's
// inherited opt-out.
//
// Cobra's hidden internal commands (__complete, __completeNoDesc, help)
// are also excluded — they run on every shell tab-completion request
// or help invocation and should never fork a daemon.
func shouldAutostartMonitor(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[forceMonitorAutostartAnnotation] == "true" {
			return true
		}
		if c.Annotations[noMonitorAutostartAnnotation] == "true" {
			return false
		}
		name := c.Name()
		if name == "help" || strings.HasPrefix(name, "__") {
			return false
		}
	}
	return true
}

// installMonitorAutostart wires a PersistentPreRunE on root that calls
// ensureMonitorFn before any non-opt-out command's RunE. If a command
// already has its own PersistentPreRunE, this preserves it.
func installMonitorAutostart(root *cobra.Command) {
	prev := root.PersistentPreRunE
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if shouldAutostartMonitor(cmd) {
			ensureMonitorFn()
		}
		if prev != nil {
			return prev(cmd, args)
		}
		return nil
	}
}
