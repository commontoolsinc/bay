package cli

import (
	"path/filepath"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
)

// --- Hard break: bay edit --set and --show are gone ---

func TestBayEdit_NoLongerHasSetFlag(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"edit"})
	if err != nil {
		t.Fatalf("could not find edit: %v", err)
	}
	if cmd.Flags().Lookup("set") != nil {
		t.Error("bay edit still has --set flag; it should have moved to bay config editor <name>")
	}
}

func TestBayEdit_NoLongerHasShowFlag(t *testing.T) {
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"edit"})
	if err != nil {
		t.Fatalf("could not find edit: %v", err)
	}
	if cmd.Flags().Lookup("show") != nil {
		t.Error("bay edit still has --show flag; it should have moved to bay config editor")
	}
}

func TestBayEdit_StillHasAllFlag(t *testing.T) {
	// Sanity: --all is the one bay edit flag we're NOT removing.
	root := NewRootCmd("test")
	cmd, _, err := root.Find([]string{"edit"})
	if err != nil {
		t.Fatalf("could not find edit: %v", err)
	}
	if cmd.Flags().Lookup("all") == nil {
		t.Error("bay edit lost --all flag; this PR was not supposed to touch it")
	}
}

// --- New bay config namespace ---

func TestBayConfigCommand_HasSubcommands(t *testing.T) {
	// Pin the four expected child commands. If a future change
	// adds/removes one, this test fails with a clear pointer.
	root := NewRootCmd("test")
	for _, path := range [][]string{
		{"config", "edit"},
		{"config", "show"},
		{"config", "path"},
		{"config", "editor"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil {
			t.Errorf("missing command: bay %s (%v)", joinPath(path), err)
			continue
		}
		// cobra.Find can return the parent if a subcommand isn't found.
		// Verify we got the actual leaf, not the parent falling through.
		if cmd.Name() != path[len(path)-1] {
			t.Errorf("bay %s resolved to %q, not the expected leaf", joinPath(path), cmd.Name())
		}
	}
}

func TestBayConfigEditor_NoArgPrintsResolvedEditor(t *testing.T) {
	// Tests the helper that backs `bay config editor` (no arg).
	// Format must match the old `bay edit --show` output:
	// "<command> (<terminal|GUI>)" or "No editor configured or detected."
	cfg := &config.Config{Editor: config.EditorConfig{Command: "nvim"}}
	got := configEditorGetString(cfg)
	if got != "nvim (terminal)" {
		t.Errorf("configEditorGetString = %q, want %q", got, "nvim (terminal)")
	}
}

func TestBayConfigEditor_NoArgWithGUIEditor(t *testing.T) {
	cfg := &config.Config{Editor: config.EditorConfig{Command: "cursor"}}
	got := configEditorGetString(cfg)
	if got != "cursor (GUI)" {
		t.Errorf("configEditorGetString = %q, want %q", got, "cursor (GUI)")
	}
}

func TestBayConfigEditor_NoArgNoEditor(t *testing.T) {
	// No config + no env vars + no probe match → "no editor"
	// message. The probe scans real PATH so we can't fully isolate;
	// instead we verify the helper returns SOMETHING when an editor
	// IS resolved, and trust resolveEditor's existing test coverage
	// for the no-editor branch.
	cfg := &config.Config{}
	got := configEditorGetString(cfg)
	if got == "" {
		t.Error("configEditorGetString should never return an empty string; either an editor or the 'no editor' message")
	}
}

func TestBayConfigEditor_WithArgPersistsEditor(t *testing.T) {
	// Run the set helper and verify the config is persisted.
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{},
		Docks:  map[string]config.DockConfig{},
	}
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("seeding config: %v", err)
	}

	if err := runConfigEditorSet(configPath, "cursor"); err != nil {
		t.Fatalf("runConfigEditorSet: %v", err)
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if loaded.Editor.Command != "cursor" {
		t.Errorf("persisted editor = %q, want cursor", loaded.Editor.Command)
	}
}

func TestBayConfigPath_PrintsConfigPath(t *testing.T) {
	// The path-printer takes the path as an argument and writes it
	// to a writer; that's the surface that's testable in isolation.
	got := configPathString("/some/path/config.toml")
	if got != "/some/path/config.toml" {
		t.Errorf("configPathString = %q, want literal path", got)
	}
}

func TestBayConfigShow_OutputContainsExpectedFields(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Command: "claude"},
		},
		Editor: config.EditorConfig{Command: "nvim"},
		Monitor: config.MonitorConfig{
			IntervalSeconds: 5,
		},
	}
	out, err := configShowString(cfg)
	if err != nil {
		t.Fatalf("configShowString: %v", err)
	}
	// Check that the rendered output mentions each section we care
	// about. We don't pin the exact format because TOML re-rendering
	// can vary; just verify the surface mentions agents, editor,
	// monitor.
	for _, want := range []string{"claude", "nvim", "5"} {
		if !contains(out, want) {
			t.Errorf("config show output missing %q; got:\n%s", want, out)
		}
	}
}
