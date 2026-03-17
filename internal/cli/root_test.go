package cli

import (
	"testing"
)

func TestNewRootCmd(t *testing.T) {
	root := NewRootCmd("test")
	if root.Use != "bay" {
		t.Errorf("root command use = %q, want bay", root.Use)
	}

	// Verify all subcommands are registered
	expected := map[string]bool{
		"dock": false, "ws": false, "win": false, "pane": false,
		"go": false, "ls": false, "recover": false, "doctor": false,
		"setup": false, "monitor": false, "add-prompt": false, "version": false,
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
}

func TestDockSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	dock, _, err := root.Find([]string{"dock"})
	if err != nil {
		t.Fatalf("finding dock: %v", err)
	}

	expected := []string{"new", "ls", "close", "recover"}
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

func TestWsSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	ws, _, err := root.Find([]string{"ws"})
	if err != nil {
		t.Fatalf("finding ws: %v", err)
	}

	expected := []string{"new", "close", "show", "update", "rename"}
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

func TestWinSubcommands(t *testing.T) {
	root := NewRootCmd("test")
	win, _, err := root.Find([]string{"win"})
	if err != nil {
		t.Fatalf("finding win: %v", err)
	}

	expected := []string{"open", "close", "restart"}
	found := map[string]bool{}
	for _, cmd := range win.Commands() {
		found[cmd.Name()] = true
	}
	for _, name := range expected {
		if !found[name] {
			t.Errorf("win subcommand %q not found", name)
		}
	}
}
