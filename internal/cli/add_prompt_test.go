package cli

import (
	"testing"
)

// TestAddPrompt_LivesUnderMonitor pins that `bay add-prompt` was
// moved under `bay monitor`. The command's job is to extend the
// pattern file the monitor reads — keeping it under the monitor
// namespace makes it discoverable from `bay monitor --help` and
// removes one top-level command.
func TestAddPrompt_LivesUnderMonitor(t *testing.T) {
	root := NewRootCmd("test")

	cmd, _, err := root.Find([]string{"monitor", "add-prompt"})
	if err != nil {
		t.Fatalf("could not find 'bay monitor add-prompt': %v", err)
	}
	if cmd.Name() != "add-prompt" {
		t.Errorf("Find returned %q, expected the add-prompt leaf", cmd.Name())
	}
}

// TestAddPrompt_NoLongerAtTopLevel pins the removal half: there
// must be no top-level `bay add-prompt` command after the move.
func TestAddPrompt_NoLongerAtTopLevel(t *testing.T) {
	root := NewRootCmd("test")
	for _, c := range root.Commands() {
		if c.Name() == "add-prompt" {
			t.Errorf("top-level 'bay add-prompt' still registered; should be moved under 'bay monitor'")
		}
	}
}

// TestAddPrompt_StartsMonitorEvenUnderOptOutParent pins the
// behavior-preservation guarantee for the move. Today (top-level)
// bay add-prompt auto-starts the monitor via the global auto-start
// hook. After moving under bay monitor (which has the no-autostart
// annotation), the inherited opt-out would skip the auto-start.
//
// The whole point of add-prompt is to extend the monitor's pattern
// set — a new pattern is dead weight until the monitor reads it.
// So `bay monitor add-prompt` is wired into the auto-start path
// the same way the top-level command was: shouldAutostartMonitor
// must return true for it.
func TestAddPrompt_StartsMonitorEvenUnderOptOutParent(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"monitor", "add-prompt"})
	if err != nil {
		t.Fatalf("could not find 'bay monitor add-prompt': %v", err)
	}
	// Defensive: cobra.Find returns the closest match if a leaf
	// isn't present, so verify we got the actual add-prompt leaf
	// before asserting on its annotation behavior.
	if cmd.Name() != "add-prompt" {
		t.Fatalf("Find returned %q, not the add-prompt leaf — TestAddPrompt_LivesUnderMonitor should have caught this first", cmd.Name())
	}
	if !shouldAutostartMonitor(cmd) {
		t.Error("shouldAutostartMonitor returned false for 'bay monitor add-prompt'; the inherited opt-out from the monitor parent must be overridden so the new pattern can take effect")
	}
}
