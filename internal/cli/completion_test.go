package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func TestFindCmd(t *testing.T) {
	root := NewRootCmd("test")

	tests := []struct {
		path    string
		wantNil bool
	}{
		{"ws", false},
		{"ws new", false},
		{"ws close", false},
		{"dock", false},
		{"dock new", false},
		{"go", false},
		{"nonexistent", true},
		{"ws nonexistent", true},
	}
	for _, tt := range tests {
		cmd := findCmd(root, tt.path)
		if (cmd == nil) != tt.wantNil {
			t.Errorf("findCmd(%q) nil=%v, want nil=%v", tt.path, cmd == nil, tt.wantNil)
		}
	}
}

func TestGoCmd_HasNextWaitingFlag(t *testing.T) {
	root := NewRootCmd("test")
	cmd := findCmd(root, "go")
	if cmd == nil {
		t.Fatal("go command not found")
	}
	f := cmd.Flags().Lookup("next-waiting")
	if f == nil {
		t.Error("bay go missing --next-waiting flag")
	}
}

func TestSurfaceGoCmd_HasNextWaitingFlag(t *testing.T) {
	root := NewRootCmd("test")
	cmd := findCmd(root, "surface go")
	if cmd == nil {
		t.Fatal("surface go command not found")
	}
	f := cmd.Flags().Lookup("next-waiting")
	if f == nil {
		t.Error("bay surface go missing --next-waiting flag")
	}
}

func TestCompletionsRegistered(t *testing.T) {
	root := NewRootCmd("test")

	// These commands should have ValidArgsFunction set
	withCompletions := []string{
		"ws close", "ws show", "ws update", "ws rename",
		"ws go",
		"dock close", "dock recover",
		"ws new",
	}
	for _, path := range withCompletions {
		cmd := findCmd(root, path)
		if cmd == nil {
			t.Errorf("command %q not found", path)
			continue
		}
		if cmd.ValidArgsFunction == nil {
			t.Errorf("command %q has no ValidArgsFunction", path)
		}
	}
}

func TestWorkspaceCompletions(t *testing.T) {
	// Set up a manifest with test data in a temp dir
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.json")

	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "labs",
			Workspaces: []manifest.Workspace{
				{
					Name:     "mem-refactor",
					Status:   manifest.WorkspaceStatusActive,
					Worktree: &manifest.WorktreeAttrs{Repo: "labs", Branch: "feature/refactor-memory", PR: "234"},
				},
				{
					Name:   "w2",
					Status: manifest.WorkspaceStatusIdle,
				},
			},
		},
	}
	manifest.Save(manifestPath, m)

	// Override the default data dir via env
	origXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", dir)
	defer os.Setenv("XDG_DATA_HOME", origXDG)

	// Copy to bay/manifest.json (where DefaultPaths looks)
	bayDir := filepath.Join(dir, "bay")
	os.MkdirAll(bayDir, 0o755)
	data, _ := os.ReadFile(manifestPath)
	os.WriteFile(filepath.Join(bayDir, "manifest.json"), data, 0o644)

	fn := workspaceCompletions()
	completions, directive := fn(nil, nil, "")

	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %d, want ShellCompDirectiveNoFileComp(%d)", directive, cobra.ShellCompDirectiveNoFileComp)
	}

	// Should contain workspace names, qualified names, and "self"
	hasValue := func(prefix string) bool {
		for _, c := range completions {
			if strings.HasPrefix(c, prefix) {
				return true
			}
		}
		return false
	}

	for _, expected := range []string{"mem-refactor", "w2", "self", "labs:mem-refactor"} {
		if !hasValue(expected) {
			t.Errorf("completions missing %q, got: %v", expected, completions)
		}
	}

	// Branch/PR should NOT be in workspace completions.
	for _, notExpected := range []string{"feature/refactor-memory", "#234"} {
		if hasValue(notExpected) {
			t.Errorf("workspace completions should not include %q (only in goCompletions)", notExpected)
		}
	}
}

func TestDockCompletions(t *testing.T) {
	dir := t.TempDir()

	// Write a config file
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{"claude": {Command: "claude"}},
		Repos:  map[string]config.RepoConfig{"labs": {Path: "/projects/labs"}},
		Docks: map[string]config.DockConfig{
			"dev":      {Repo: "labs", Agent: "claude"},
			"research": {Agent: "claude"},
		},
	}

	origXDG := os.Getenv("XDG_CONFIG_HOME")
	os.Setenv("XDG_CONFIG_HOME", dir)
	defer os.Setenv("XDG_CONFIG_HOME", origXDG)

	bayDir := filepath.Join(dir, "bay")
	os.MkdirAll(bayDir, 0o755)
	config.Save(filepath.Join(bayDir, "config.toml"), cfg)

	fn := dockCompletions()
	completions, _ := fn(nil, nil, "")

	if len(completions) != 2 {
		t.Errorf("expected 2 completions, got %d: %v", len(completions), completions)
	}

	hasValue := func(prefix string) bool {
		for _, c := range completions {
			if strings.HasPrefix(c, prefix) {
				return true
			}
		}
		return false
	}
	if !hasValue("dev") {
		t.Errorf("completions missing 'dev', got: %v", completions)
	}
	if !hasValue("research") {
		t.Errorf("completions missing 'research', got: %v", completions)
	}
}

func TestStatusCompletions(t *testing.T) {
	completions, _ := statusCompletions(nil, nil, "")
	if len(completions) != 3 {
		t.Errorf("expected 3 status completions, got %d", len(completions))
	}
}

func TestSplitCompletions(t *testing.T) {
	completions, _ := splitCompletions(nil, nil, "")
	if len(completions) != 2 {
		t.Errorf("expected 2 split completions, got %d", len(completions))
	}
}

func TestWorkspaceCompletions_SecondArgReturnsNone(t *testing.T) {
	fn := workspaceCompletions()
	completions, _ := fn(nil, []string{"w1"}, "")
	if len(completions) != 0 {
		t.Errorf("expected no completions for second arg, got %d", len(completions))
	}
}
