package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestAddPrompt_RemovedEntirely pins that bay add-prompt is gone
// from the entire CLI surface. The command was a friction-saver for
// an obscure workflow (capture a current pane line as a detection
// pattern), but the captured line always needed manual editing to
// be useful as a regex — so the workflow really was: run the
// command, then open the patterns file anyway. Cutting the command
// in favor of "edit ~/.config/bay/waiting-patterns.txt directly" removes
// ~80 lines of code, the structural force-autostart annotation, and
// one rarely-discovered surface.
//
// This test walks every command in the cobra tree (including hidden
// children) and fails if any leaf is named "add-prompt".
func TestAddPrompt_RemovedEntirely(t *testing.T) {
	root := NewRootCmd("test")
	var found []string
	var visit func(c *cobra.Command)
	visit = func(c *cobra.Command) {
		if c.Name() == "add-prompt" {
			found = append(found, c.CommandPath())
		}
		for _, child := range c.Commands() {
			visit(child)
		}
	}
	visit(root)
	for _, p := range found {
		t.Errorf("add-prompt command still exists at %q; should be removed entirely", p)
	}
}
