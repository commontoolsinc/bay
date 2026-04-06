package cli

import (
	"testing"
)

func TestNewRootCmd(t *testing.T) {
	root := NewRootCmd("test")
	if root.Use != "bay" {
		t.Errorf("root command use = %q, want bay", root.Use)
	}

	// Verify all subcommands are registered (including hidden ones)
	expected := map[string]bool{
		"dock": false, "repo": false, "ws": false, "surface": false,
		"go": false, "ls": false, "tree": false, "pwd": false, "recover": false, "doctor": false,
		"setup": false, "monitor": false, "add-prompt": false, "version": false,
		"shell": false, "edit": false, "restart": false, "status-line": false,
		"agent-guide": false,
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
}

func TestNewRootCmd_OldCommandsRemoved(t *testing.T) {
	root := NewRootCmd("test")

	removed := []string{"win", "pane", "close-pane"}
	for _, cmd := range root.Commands() {
		for _, name := range removed {
			if cmd.Name() == name {
				t.Errorf("old command %q should be removed", name)
			}
		}
	}
}

func TestRestartCommandExists(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"restart"})
	if err != nil {
		t.Fatalf("finding restart: %v", err)
	}
	if cmd.Name() != "restart" {
		t.Errorf("expected restart, got %q", cmd.Name())
	}
}

func TestSurfaceSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	sf, _, err := root.Find([]string{"surface"})
	if err != nil {
		t.Fatalf("finding surface: %v", err)
	}

	expected := []string{"new", "close", "restart", "ls", "show", "rename", "go", "next", "prev"}
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

func TestRepoSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	repo, _, err := root.Find([]string{"repo"})
	if err != nil {
		t.Fatalf("finding repo: %v", err)
	}

	expected := []string{"add", "ls", "show", "remove", "init"}
	found := map[string]bool{}
	for _, cmd := range repo.Commands() {
		found[cmd.Name()] = true
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("repo subcommand %q not found", name)
		}
	}
}

func TestDockSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	dock, _, err := root.Find([]string{"dock"})
	if err != nil {
		t.Fatalf("finding dock: %v", err)
	}

	expected := []string{"new", "ls", "show", "tree", "rename", "close", "recover"}
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

func TestWsSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	ws, _, err := root.Find([]string{"ws"})
	if err != nil {
		t.Fatalf("finding ws: %v", err)
	}

	expected := []string{"new", "close", "show", "update", "rename", "ls", "tree", "go", "next", "prev"}
	found := map[string]bool{}
	for _, cmd := range ws.Commands() {
		found[cmd.Name()] = true
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("ws subcommand %q not found", name)
		}
	}
}
