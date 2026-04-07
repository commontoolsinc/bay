package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestUseStrings_NoSelfMarker pins the consistency rule that "self" is
// a runtime magic value, not a syntactic alternative — so the Use line
// shouldn't advertise "|self". The long help text can still mention it.
func TestUseStrings_NoSelfMarker(t *testing.T) {
	root := NewRootCmd("test")
	var bad []string
	for _, c := range allCommands(root) {
		if strings.Contains(c.Use, "|self") {
			bad = append(bad, c.CommandPath()+"  Use="+c.Use)
		}
	}
	for _, b := range bad {
		t.Errorf("usage advertises 'self' in the Use string (move to long help instead): %s", b)
	}
}

// TestPositionalShapes pins the new positional contracts. If anyone
// changes a command's Use line back to a container-positional ([dock],
// [workspace]) or removes the optional [name], this test fails with a
// clear pointer to the contract.
func TestPositionalShapes(t *testing.T) {
	root := NewRootCmd("test")

	cases := []struct {
		path    []string
		wantUse string
		why     string
	}{
		// Create-verbs: positional is the new thing's name.
		{[]string{"ws", "new"}, "new [name]", "ws new positional must be the workspace's display name"},
		{[]string{"surface", "new"}, "new [name]", "surface new positional must be the surface name"},
		{[]string{"shell"}, "shell [name]", "shell positional must be the surface name"},

		// Show: optional positional, defaults to current.
		{[]string{"show"}, "show [name]", "top-level show positional must be optional (defaults to current)"},
		{[]string{"surface", "show"}, "show [name]", "surface show positional must be optional"},
		{[]string{"ws", "show"}, "show [name]", "ws show positional must be optional"},

		// Tree: optional positional, defaults to current.
		{[]string{"dock", "tree"}, "tree [name]", "dock tree positional must be optional (defaults to current dock)"},

		// Close: positional shape, no '|self' marker.
		{[]string{"close"}, "close <name>", "close positional must be name (no '|self' in usage)"},
		{[]string{"surface", "close"}, "close <name>", "surface close positional must be name"},
		{[]string{"ws", "close"}, "close [name]", "ws close has --done so positional is optional"},

		// Rename: two positionals, no '|self' marker in usage.
		{[]string{"rename"}, "rename <old> <new>", "top-level rename — surface form"},
		{[]string{"surface", "rename"}, "rename <old> <new>", "surface rename"},
		{[]string{"ws", "rename"}, "rename <name> <new-name>", "ws rename"},
	}

	for _, c := range cases {
		t.Run(joinPath(c.path), func(t *testing.T) {
			cmd, _, err := root.Find(c.path)
			if err != nil {
				t.Fatalf("could not find %s: %v", joinPath(c.path), err)
			}
			if cmd.Use != c.wantUse {
				t.Errorf("Use mismatch for %s\n  got:  %q\n  want: %q\n  why:  %s",
					joinPath(c.path), cmd.Use, c.wantUse, c.why)
			}
		})
	}
}

// TestWsNew_DockFlagExists pins that ws new accepts --dock now that the
// positional has been repurposed for the workspace name. Without this
// flag the old dock-targeting workflow would have no replacement.
func TestWsNew_DockFlagExists(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"ws", "new"})
	if err != nil {
		t.Fatalf("could not find ws new: %v", err)
	}
	if cmd.Flags().Lookup("dock") == nil {
		t.Error("ws new is missing --dock flag (the replacement for the old positional)")
	}
	// The old --name flag is gone — positional replaces it.
	if cmd.Flags().Lookup("name") != nil {
		t.Error("ws new still has --name flag; positional replaces it")
	}
}

// TestSurfaceNew_WsAndDockFlagsExist pins that surface new accepts --ws
// now that the positional has been repurposed for the surface name.
func TestSurfaceNew_WsAndDockFlagsExist(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"surface", "new"})
	if err != nil {
		t.Fatalf("could not find surface new: %v", err)
	}
	if cmd.Flags().Lookup("ws") == nil {
		t.Error("surface new is missing --ws flag (the replacement for the old positional)")
	}
	if cmd.Flags().Lookup("dock") == nil {
		t.Error("surface new is missing --dock flag")
	}
	if cmd.Flags().Lookup("name") != nil {
		t.Error("surface new still has --name flag; positional replaces it")
	}
}

// TestShell_WsAndDockFlagsExist pins that bay shell accepts --ws now
// that the positional has been repurposed for the shell surface name.
func TestShell_WsAndDockFlagsExist(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"shell"})
	if err != nil {
		t.Fatalf("could not find shell: %v", err)
	}
	if cmd.Flags().Lookup("ws") == nil {
		t.Error("shell is missing --ws flag (the replacement for the old positional)")
	}
	if cmd.Flags().Lookup("dock") == nil {
		t.Error("shell is missing --dock flag")
	}
	if cmd.Flags().Lookup("name") != nil {
		t.Error("shell still has --name flag; positional replaces it")
	}
}

// TestRunSurfaceShow_NoArgsDefaultsToSelf verifies the new no-arg
// behavior on the runSurfaceShow helper. Before this PR, ExactArgs(1)
// rejected this case at the cobra layer; now MaximumNArgs(1) lets it
// through and the helper resolves to the current pane.
func TestRunSurfaceShow_NoArgsDefaultsToSelf(t *testing.T) {
	eng := selfFixture(t)
	if err := runSurfaceShow(eng, nil, "", ""); err != nil {
		t.Errorf("runSurfaceShow with no args should default to current surface: %v", err)
	}
}

func joinPath(parts []string) string {
	return "bay " + strings.Join(parts, " ")
}

// allCommands returns every cobra command reachable from root, including
// root itself, in a flat slice for easy iteration.
func allCommands(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	var visit func(c *cobra.Command)
	visit = func(c *cobra.Command) {
		out = append(out, c)
		for _, child := range c.Commands() {
			visit(child)
		}
	}
	visit(root)
	return out
}
