package cli

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/palette"
)

func TestBuildAgentPickerItems_FullListAlwaysContainsAllAgents(t *testing.T) {
	mru := []string{"gemini"}
	available := []string{"claude", "codex", "gemini"}

	_, labels := buildAgentPickerItems(mru, available)

	// Expected layout: mru first (gemini), separator, then full sorted list.
	want := []string{"gemini", "──────", "claude", "codex", "gemini"}
	if len(labels) != len(want) {
		t.Fatalf("labels=%v; want %v", labels, want)
	}
	for i, w := range want {
		if labels[i] != w {
			t.Errorf("labels[%d]=%q; want %q", i, labels[i], w)
		}
	}
}

func TestBuildAgentPickerItems_NoMRUOmitsSeparator(t *testing.T) {
	_, labels := buildAgentPickerItems(nil, []string{"claude", "codex"})
	want := []string{"claude", "codex"}
	if len(labels) != len(want) {
		t.Fatalf("labels=%v; want %v (no separator when MRU empty)", labels, want)
	}
	for i, w := range want {
		if labels[i] != w {
			t.Errorf("labels[%d]=%q; want %q", i, labels[i], w)
		}
	}
}

func TestBuildAgentPickerItems_SeparatorValueIsSentinel(t *testing.T) {
	items, _ := buildAgentPickerItems([]string{"codex"}, []string{"claude", "codex"})
	// items[1] must be the separator and have Value < 0 so picker.Pick
	// returning that value is treated as cancellation.
	if !strings.HasPrefix(items[1].Display, "─") {
		t.Fatalf("items[1]=%+v; want separator", items[1])
	}
	if items[1].Value >= 0 {
		t.Errorf("separator Value=%d; must be negative", items[1].Value)
	}
}

func TestBuildAgentPickerItems_LabelsIndexedByItemValue(t *testing.T) {
	// Real items have Value == their index in labels (except the separator).
	// This test pins that invariant — paletteAgentPick relies on
	// labels[sel] to resolve the chosen agent name.
	items, labels := buildAgentPickerItems([]string{"codex"}, []string{"claude", "codex", "gemini"})
	for _, it := range items {
		if it.Value < 0 {
			continue // separator
		}
		if it.Value >= len(labels) {
			t.Errorf("item Value=%d out of range (len=%d)", it.Value, len(labels))
			continue
		}
		if labels[it.Value] != it.Display {
			t.Errorf("labels[%d]=%q does not match item.Display=%q", it.Value, labels[it.Value], it.Display)
		}
	}
}

func TestDetectScope(t *testing.T) {
	cases := []struct {
		name string
		ctx  *engine.Context
		want palette.Scope
	}{
		{"nil ctx", nil, palette.ScopeAnywhere},
		{"empty ctx", &engine.Context{}, palette.ScopeAnywhere},
		{"repo only", &engine.Context{Repo: "bay"}, palette.ScopeAnywhere},
		{"dock only", &engine.Context{Dock: "bay"}, palette.ScopeInDock},
		{"workspace", &engine.Context{Dock: "bay", Workspace: "auth-fix"}, palette.ScopeInWorkspace},
		{"surface", &engine.Context{Dock: "bay", Workspace: "w", Surface: "shell"}, palette.ScopeInWorkspace},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detectScope(c.ctx); got != c.want {
				t.Errorf("detectScope(%+v)=%v; want %v", c.ctx, got, c.want)
			}
		})
	}
}

func TestBuildPaletteEntries_HasExpected24Entries(t *testing.T) {
	// Pin the entry count + unique-ID invariant. The design locks in 24
	// v1 entries; anything that adds or removes a command should touch
	// this test intentionally.
	env := testPaletteEnv()
	entries := buildPaletteEntries(env, palette.ModeWindow)

	const want = 24
	if len(entries) != want {
		t.Errorf("buildPaletteEntries returned %d entries; want %d", len(entries), want)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.ID == "" {
			t.Errorf("entry %q has empty ID", e.Title)
		}
		if seen[e.ID] {
			t.Errorf("duplicate ID %q", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestBuildPaletteEntries_ModeSensitiveHotkeysFlip(t *testing.T) {
	// Install a fake hotkey map so we can assert that the "New shell"
	// entry's Hotkey swaps between window/pane modes as the user Tabs.
	env := testPaletteEnv()
	env.Hotkeys = fakeHotkeys(map[string]string{
		"bay shell --window": "M-s",
		"bay shell --pane":   "M-S",
	})

	win := findEntry(t, buildPaletteEntries(env, palette.ModeWindow), "new-shell")
	pane := findEntry(t, buildPaletteEntries(env, palette.ModePane), "new-shell")

	if win.Hotkey != "M-s" {
		t.Errorf("window-mode new-shell hotkey=%q; want M-s", win.Hotkey)
	}
	if pane.Hotkey != "M-S" {
		t.Errorf("pane-mode new-shell hotkey=%q; want M-S", pane.Hotkey)
	}
}

// testPaletteEnv returns a minimal env suitable for exercising the
// build-entry logic. Engine is non-nil (nil would NPE in closures) but
// actions aren't invoked in these tests — we only check metadata.
func testPaletteEnv() *paletteEnv {
	return &paletteEnv{
		Engine:  &engine.Engine{},
		Ctx:     &engine.Context{Dock: "testdock", Workspace: "testws", Surface: "testsf"},
		Scope:   palette.ScopeInWorkspace,
		Recents: palette.LoadRecents(""),
		Hotkeys: fakeHotkeys(nil),
	}
}

// fakeHotkeys builds a palette.Hotkeys directly from a lookup map,
// bypassing the tmux list-keys subprocess.
func fakeHotkeys(m map[string]string) *palette.Hotkeys {
	// Synthesize a tmux list-keys block so the parser sees what we want.
	// Tests that don't care about hotkeys can pass nil → empty block.
	var b strings.Builder
	for cmd, key := range m {
		b.WriteString("bind-key -T root ")
		b.WriteString(key)
		b.WriteString(" run-shell '")
		b.WriteString(cmd)
		b.WriteString(" || true'\n")
	}
	return palette.ParseHotkeysForTest(b.String())
}

func findEntry(t *testing.T, entries []palette.Entry, id string) palette.Entry {
	t.Helper()
	for _, e := range entries {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("entry %q not found", id)
	return palette.Entry{}
}
