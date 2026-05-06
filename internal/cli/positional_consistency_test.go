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
// [bay]) or removes the optional [name], this test fails with a
// clear pointer to the contract.
func TestPositionalShapes(t *testing.T) {
	root := NewRootCmd("test")

	cases := []struct {
		path    []string
		wantUse string
		why     string
	}{
		// Create-verbs: positional is the new thing's name.
		{[]string{"new"}, "new [name]", "bay new positional must be the bay's display name"},
		{[]string{"shell"}, "shell [name]", "shell positional must be the surface name"},

		// Show: optional positional, defaults to current.
		{[]string{"show"}, "show [id]", "top-level show positional must be optional (defaults to current)"},
		{[]string{"surface", "show"}, "show [name]", "surface show positional must be optional"},

		// Tree: optional positional, defaults to current.
		{[]string{"dock", "tree"}, "tree [name]", "dock tree positional must be optional (defaults to current dock)"},

		// Close: positional shape, no '|self' marker.
		{[]string{"close"}, "close [id]", "bay close has --clean so positional is optional"},
		{[]string{"surface", "close"}, "close <name>", "surface close positional must be name"},

		// Rename: first positional optional (defaults to self).
		{[]string{"rename"}, "rename [id] <new-name>", "top-level rename — bay form"},
		{[]string{"surface", "rename"}, "rename [old] <new>", "surface rename"},
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

// TestBayNew_DockFlagExists pins that bay new accepts --dock now that the
// positional has been repurposed for the bay name. Without this
// flag the old dock-targeting workflow would have no replacement.
func TestBayNew_DockFlagExists(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"new"})
	if err != nil {
		t.Fatalf("could not find bay new: %v", err)
	}
	if cmd.Flags().Lookup("dock") == nil {
		t.Error("bay new is missing --dock flag (the replacement for the old positional)")
	}
	// The old --name flag is gone — positional replaces it.
	if cmd.Flags().Lookup("name") != nil {
		t.Error("bay new still has --name flag; positional replaces it")
	}
}

// TestBayNew_BareAgentFlag pins that --agent can be used without a value
// to get the dock's default agent. NoOptDefVal must be set so cobra
// doesn't require an argument.
func TestBayNew_BareAgentFlag(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"new"})
	if err != nil {
		t.Fatalf("could not find bay new: %v", err)
	}
	f := cmd.Flags().Lookup("agent")
	if f == nil {
		t.Fatal("bay new is missing --agent flag")
	}
	if f.NoOptDefVal == "" {
		t.Error("--agent flag must have NoOptDefVal set so bare --agent works")
	}
}

func TestBayClose_BatchFlagsRejectBayID(t *testing.T) {
	for _, flag := range []string{"--done", "--clean", "--all"} {
		t.Run(flag, func(t *testing.T) {
			cmd := newBayCloseCmd()
			cmd.SetArgs([]string{"b1", flag})

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "batch close flags do not take a bay ID") {
				t.Fatalf("bay close b1 %s error = %v, want batch flag/id rejection", flag, err)
			}
		})
	}
}

// TestSurfaceNew_SubcommandsHaveBayAndDockFlags pins that surface new's
// subcommands (shell, agent, cmd) accept --bay and --dock.
func TestSurfaceNew_SubcommandsHaveBayAndDockFlags(t *testing.T) {
	root := NewRootCmd("test")
	for _, kind := range []string{"shell", "agent", "cmd"} {
		cmd, _, err := root.Find([]string{"surface", "new", kind})
		if err != nil {
			t.Fatalf("could not find surface new %s: %v", kind, err)
		}
		if cmd.Flags().Lookup("bay") == nil {
			t.Errorf("surface new %s is missing --bay flag", kind)
		}
		if cmd.Flags().Lookup("dock") == nil {
			t.Errorf("surface new %s is missing --dock flag", kind)
		}
	}
}

// TestShell_BayAndDockFlagsExist pins that bay shell accepts --bay now
// that the positional has been repurposed for the shell surface name.
func TestShell_BayAndDockFlagsExist(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"shell"})
	if err != nil {
		t.Fatalf("could not find shell: %v", err)
	}
	if cmd.Flags().Lookup("bay") == nil {
		t.Error("shell is missing --bay flag (the replacement for the old positional)")
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
