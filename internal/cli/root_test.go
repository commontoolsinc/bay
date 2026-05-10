package cli

import (
	"testing"
)

func TestNewRootCmd(t *testing.T) {
	root := NewRootCmd("test")
	if root.Use != "bay" {
		t.Errorf("root command use = %q, want bay", root.Use)
	}

	// Verify all subcommands are registered (including hidden ones).
	// add-prompt was removed entirely — see TestAddPrompt_RemovedEntirely.
	expected := map[string]bool{
		"dock": false, "surface": false,
		"new": false, "close": false, "clean-review": false, "show": false, "rename": false, "describe": false,
		"home": false, "go": false, "ls": false, "prepare": false, "next": false, "prev": false, "tree": false, "pwd": false,
		"recover": false, "doctor": false, "setup": false, "monitor": false, "version": false,
		"shell": false, "edit": false, "status-line": false,
		"agent-guide": false,
		"palette":     false, "prepare-worker": false,
	}
	for _, cmd := range root.Commands() {
		if _, ok := expected[cmd.Name()]; ok {
			expected[cmd.Name()] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("subcommand %q not registered", name)
		}
	}

	// Verify close-pane is hidden
	for _, cmd := range root.Commands() {
		if cmd.Name() == "close-pane" {
			if !cmd.Hidden {
				t.Error("close-pane should be hidden")
			}
			break
		}
	}

	// Palette is hidden — it's only invoked via tmux keybinding, not typed.
	for _, cmd := range root.Commands() {
		if cmd.Name() == "palette" && !cmd.Hidden {
			t.Error("palette should be hidden")
		}
	}

	// clean-review is a targeted maintenance command, not a core command
	// shown in the main help.
	for _, cmd := range root.Commands() {
		if cmd.Name() == "clean-review" && !cmd.Hidden {
			t.Error("clean-review should be hidden")
		}
	}

	for _, cmd := range root.Commands() {
		if cmd.Name() == "prepare-worker" && !cmd.Hidden {
			t.Error("prepare-worker should be hidden")
		}
	}
}

func TestNewRootCmd_OldCommandsRemoved(t *testing.T) {
	root := NewRootCmd("test")

	removed := []string{"win", "pane", "close-pane", "workspace", "ws"}
	for _, cmd := range root.Commands() {
		for _, name := range removed {
			if cmd.Name() == name {
				t.Errorf("old command %q should be removed", name)
			}
		}
	}
}

func TestPaletteCommandDefaultsToPaneMode(t *testing.T) {
	cmd := newPaletteCmd()
	flag := cmd.Flags().Lookup("split")
	if flag == nil {
		t.Fatal("palette command missing --split flag")
	}
	if flag.DefValue != "pane" {
		t.Fatalf("palette --split default=%q; want pane", flag.DefValue)
	}
}

func TestSurfaceSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	sf, _, err := root.Find([]string{"surface"})
	if err != nil {
		t.Fatalf("finding surface: %v", err)
	}

	expected := []string{"new", "close", "ls", "show", "rename", "go", "next", "prev"}
	found := map[string]bool{}
	for _, cmd := range sf.Commands() {
		found[cmd.Name()] = true
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("surface subcommand %q not found", name)
		}
	}
}

func TestSurfaceAlias(t *testing.T) {
	root := NewRootCmd("test")

	// "sf" should resolve to the surface command
	cmd, _, err := root.Find([]string{"sf"})
	if err != nil {
		t.Fatalf("finding sf alias: %v", err)
	}
	if cmd.Name() != "surface" {
		t.Errorf("sf should resolve to surface, got %q", cmd.Name())
	}
}

func TestRepoCommandRemoved(t *testing.T) {
	root := NewRootCmd("test")
	for _, cmd := range root.Commands() {
		if cmd.Name() == "repo" {
			t.Fatal("repo command should not be registered")
		}
	}
}

func TestDockSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	dock, _, err := root.Find([]string{"dock"})
	if err != nil {
		t.Fatalf("finding dock: %v", err)
	}

	expected := []string{"new", "ls", "show", "tree", "rename", "close", "recover", "sync"}
	found := map[string]bool{}
	for _, cmd := range dock.Commands() {
		found[cmd.Name()] = true
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("dock subcommand %q not found", name)
		}
	}
}

func TestTreeCommandExists(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"tree"})
	if err != nil {
		t.Fatalf("finding tree: %v", err)
	}
	if cmd.Name() != "tree" {
		t.Errorf("expected tree, got %q", cmd.Name())
	}
}

func TestBayTopLevelSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	expected := []string{"new", "close", "show", "rename", "describe", "ls", "go", "next", "prev"}
	for _, name := range expected {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd == nil || cmd.Name() != name {
			t.Errorf("top-level bay command %q not found", name)
		}
	}
}
