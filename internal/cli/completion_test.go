package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	// These commands should have ValidArgsFunction set on their
	// positional. Commands whose positional is a free-form name (the
	// create-verbs ws new / surface new / shell) intentionally have
	// no ValidArgsFunction — see TestCreateVerbs_PositionalsAreFreeText.
	withCompletions := []string{
		"ws close", "ws show", "ws rename",
		"ws go",
		"dock close", "dock recover", "dock tree",
		// surface verbs (sf X form) — close/restart/show/rename target
		// existing surfaces by name.
		"surface close", "surface restart", "surface show", "surface rename",
		// top-level surface verbs
		"close", "show", "rename", "restart",
		// bay new subcommands with positional completions
		"new agent", "new edit",
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

// TestCreateVerbs_PositionalsAreFreeText pins the contract that
// ws new / surface new / shell have NO positional completion. Their
// positional names a brand-new thing the user is about to create —
// completing it from existing names would be misleading and would
// invite the same kind of dock-name-vs-workspace-name confusion this
// PR exists to fix.
func TestCreateVerbs_PositionalsAreFreeText(t *testing.T) {
	root := NewRootCmd("test")
	for _, path := range []string{"ws new", "surface new", "shell"} {
		cmd := findCmd(root, path)
		if cmd == nil {
			t.Errorf("command %q not found", path)
			continue
		}
		if cmd.ValidArgsFunction != nil {
			t.Errorf("command %q has a positional ValidArgsFunction; create-verb positionals must be free text", path)
		}
	}
}

func TestSurfaceCommands_UseSurfaceCompletions(t *testing.T) {
	// surface close/restart/show/rename and the top-level close/show/rename/restart
	// must use sfCompl, not wsCompl. Set up a manifest with one workspace and
	// one surface, and verify the completer returns the surface name.
	dir := t.TempDir()

	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "labs",
			Workspaces: []manifest.Workspace{
				{
					Name: "w1",
					Surfaces: []manifest.Surface{
						{Name: "agent", Type: manifest.SurfaceTypeAgent},
					},
				},
			},
		},
	}

	origXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", dir)
	defer os.Setenv("XDG_DATA_HOME", origXDG)
	bayDir := filepath.Join(dir, "bay")
	os.MkdirAll(bayDir, 0o755)
	manifest.Save(filepath.Join(bayDir, "manifest.json"), m)

	root := NewRootCmd("test")

	for _, path := range []string{
		"surface close", "surface restart", "surface show", "surface rename",
		"close", "show", "rename", "restart",
	} {
		cmd := findCmd(root, path)
		if cmd == nil || cmd.ValidArgsFunction == nil {
			t.Errorf("command %q has no ValidArgsFunction", path)
			continue
		}
		completions, _ := cmd.ValidArgsFunction(cmd, nil, "")
		// Surface completer emits qualified forms only — verify the agent
		// surface appears as w1:agent and labs:w1:agent.
		hasAgent := false
		for _, c := range completions {
			if strings.HasPrefix(c, "w1:agent\t") || strings.HasPrefix(c, "labs:w1:agent\t") {
				hasAgent = true
				break
			}
		}
		if !hasAgent {
			t.Errorf("command %q completions missing 'agent' surface, got: %v", path, completions)
		}
	}
}

func TestSurfaceCompletions(t *testing.T) {
	dir := t.TempDir()

	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "labs",
			Workspaces: []manifest.Workspace{
				{
					Name: "w1",
					Surfaces: []manifest.Surface{
						{Name: "agent", Type: manifest.SurfaceTypeAgent},
						{Name: "shell", Type: manifest.SurfaceTypeShell},
					},
				},
			},
		},
	}

	origXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", dir)
	defer os.Setenv("XDG_DATA_HOME", origXDG)
	bayDir := filepath.Join(dir, "bay")
	os.MkdirAll(bayDir, 0o755)
	manifest.Save(filepath.Join(bayDir, "manifest.json"), m)

	fn := surfaceCompletions()
	completions, directive := fn(nil, nil, "")

	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %d, want NoFileComp", directive)
	}

	hasValue := func(prefix string) bool {
		for _, c := range completions {
			if strings.HasPrefix(c, prefix) {
				return true
			}
		}
		return false
	}

	// Both qualified forms for each surface, plus the self keyword. Bare
	// names ("agent", "shell") are deliberately omitted — they collide
	// across workspaces and would mislead users.
	for _, expected := range []string{
		"self",
		"w1:agent", "w1:shell",
		"labs:w1:agent", "labs:w1:shell",
	} {
		if !hasValue(expected) {
			t.Errorf("surfaceCompletions missing %q, got: %v", expected, completions)
		}
	}

	// Bare surface names must NOT appear — verify by checking that no entry
	// is exactly "agent" or "shell" (no colon prefix) or starts with
	// "agent\t" / "shell\t".
	for _, c := range completions {
		if c == "agent" || c == "shell" ||
			strings.HasPrefix(c, "agent\t") || strings.HasPrefix(c, "shell\t") {
			t.Errorf("surfaceCompletions should not include bare name %q", c)
		}
	}
}

func TestSurfaceCompletions_NoSelfAfterColon(t *testing.T) {
	// When the user has typed a colon, 'self' is not a meaningful completion
	// (qualified self is always literal, not a keyword).
	dir := t.TempDir()
	m := manifest.New()
	m.Docks = []manifest.Dock{{Name: "labs", Workspaces: []manifest.Workspace{{Name: "w1"}}}}

	origXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", dir)
	defer os.Setenv("XDG_DATA_HOME", origXDG)
	bayDir := filepath.Join(dir, "bay")
	os.MkdirAll(bayDir, 0o755)
	manifest.Save(filepath.Join(bayDir, "manifest.json"), m)

	fn := surfaceCompletions()
	completions, _ := fn(nil, nil, "w1:")

	for _, c := range completions {
		if strings.HasPrefix(c, "self\t") || c == "self" {
			t.Errorf("surfaceCompletions should not include 'self' after a colon, got: %v", completions)
		}
	}
}

func TestSurfaceCompletions_SecondArgReturnsNone(t *testing.T) {
	fn := surfaceCompletions()
	completions, _ := fn(nil, []string{"agent"}, "")
	if len(completions) != 0 {
		t.Errorf("expected no completions for second arg, got %d", len(completions))
	}
}

func TestWorkspaceFlagCompletions(t *testing.T) {
	dir := t.TempDir()
	m := manifest.New()
	m.Docks = []manifest.Dock{
		{Name: "labs", Workspaces: []manifest.Workspace{{Name: "w1"}, {Name: "w2"}}},
	}
	origXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", dir)
	defer os.Setenv("XDG_DATA_HOME", origXDG)
	bayDir := filepath.Join(dir, "bay")
	os.MkdirAll(bayDir, 0o755)
	manifest.Save(filepath.Join(bayDir, "manifest.json"), m)

	fn := workspaceFlagCompletions()
	// Pass non-empty args — flag completers must NOT short-circuit on args.
	completions, directive := fn(nil, []string{"some-positional"}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("directive = %d, want NoFileComp", directive)
	}
	if len(completions) == 0 {
		t.Error("workspaceFlagCompletions returned no completions even with non-empty args")
	}
	// Should contain w1 and w2.
	hasValue := func(prefix string) bool {
		for _, c := range completions {
			if strings.HasPrefix(c, prefix) {
				return true
			}
		}
		return false
	}
	for _, expected := range []string{"w1", "w2", "labs:w1", "labs:w2"} {
		if !hasValue(expected) {
			t.Errorf("workspaceFlagCompletions missing %q, got: %v", expected, completions)
		}
	}
}

func TestDockFlagCompletions(t *testing.T) {
	dir := t.TempDir()
	m := manifest.New()
	m.Docks = []manifest.Dock{
		{Name: "labs", Repo: "labs"},
		{Name: "research"},
	}
	origXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", dir)
	defer os.Setenv("XDG_DATA_HOME", origXDG)
	bayDir := filepath.Join(dir, "bay")
	os.MkdirAll(bayDir, 0o755)
	manifest.Save(filepath.Join(bayDir, "manifest.json"), m)

	fn := dockFlagCompletions()
	// Pass non-empty args — flag completers must NOT short-circuit on args.
	completions, _ := fn(nil, []string{"some-positional"}, "")
	if len(completions) != 2 {
		t.Errorf("expected 2 dock completions, got %d: %v", len(completions), completions)
	}
}

func TestFlagCompletions_WsAndDockOnSurfaceVerbs(t *testing.T) {
	// Verify --ws and --dock have completion functions registered on
	// the surface verb commands. surface new and shell are in this
	// list now that their positional is the surface name and the
	// workspace target moved to --ws (PR #112).
	root := NewRootCmd("test")
	for _, path := range []string{
		"surface new",
		"surface close", "surface restart", "surface show", "surface rename",
		"close", "show", "rename", "restart",
		"shell",
		"new shell", "new agent", "new cmd",
	} {
		cmd := findCmd(root, path)
		if cmd == nil {
			t.Errorf("command %q not found", path)
			continue
		}
		// Cobra exposes flag completions via its internal map. We can't
		// inspect them directly, so just verify the flags exist (a missing
		// flag would mean the wiring loop silently no-op'd on this command).
		if cmd.Flags().Lookup("ws") == nil {
			t.Errorf("command %q missing --ws flag", path)
		}
		if cmd.Flags().Lookup("dock") == nil {
			t.Errorf("command %q missing --dock flag", path)
		}
	}
}

// TestFlagCompletions_DockOnWsNew pins that bay ws new exposes a
// --dock flag (the replacement for the old positional). The flag
// completion is registered against the same dockFlagCompletions
// helper used elsewhere — there's no public way to inspect cobra's
// flag-completion map, so this test only verifies the flag exists.
func TestFlagCompletions_DockOnWsNew(t *testing.T) {
	root := NewRootCmd("test")
	cmd := findCmd(root, "ws new")
	if cmd == nil {
		t.Fatal("ws new command not found")
	}
	if cmd.Flags().Lookup("dock") == nil {
		t.Error("ws new missing --dock flag")
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

	// Write a manifest file with docks
	m := manifest.New()
	m.Docks = []manifest.Dock{
		{Name: "dev", Repo: "labs", Agent: "claude", Workspaces: []manifest.Workspace{}},
		{Name: "research", Agent: "claude", Workspaces: []manifest.Workspace{}},
	}

	origXDG := os.Getenv("XDG_DATA_HOME")
	os.Setenv("XDG_DATA_HOME", dir)
	defer os.Setenv("XDG_DATA_HOME", origXDG)

	bayDir := filepath.Join(dir, "bay")
	os.MkdirAll(bayDir, 0o755)
	manifest.Save(filepath.Join(bayDir, "manifest.json"), m)

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
