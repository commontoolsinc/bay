package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
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
		{"dock only", &engine.Context{Dock: "bay"}, palette.ScopeInDock},
		{"bay", &engine.Context{Dock: "bay", Bay: "auth-fix"}, palette.ScopeInBay},
		{"surface", &engine.Context{Dock: "bay", Bay: "w", Surface: "shell"}, palette.ScopeInBay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detectScope(c.ctx); got != c.want {
				t.Errorf("detectScope(%+v)=%v; want %v", c.ctx, got, c.want)
			}
		})
	}
}

func TestBuildPaletteEntries_HasExpectedEntries(t *testing.T) {
	// Pin the entry count + unique-ID invariant: anything that adds or
	// removes a command should touch this test intentionally.
	env := testPaletteEnv()
	entries := buildPaletteEntries(env, palette.ModeWindow)

	const want = 29
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
		"bay shell --pane":   "M-s",
		"bay shell --window": "M-S",
	})

	win := findEntry(t, buildPaletteEntries(env, palette.ModeWindow), "new-shell")
	pane := findEntry(t, buildPaletteEntries(env, palette.ModePane), "new-shell")

	if win.Hotkey != "M-S" {
		t.Errorf("window-mode new-shell hotkey=%q; want M-S", win.Hotkey)
	}
	if pane.Hotkey != "M-s" {
		t.Errorf("pane-mode new-shell hotkey=%q; want M-s", pane.Hotkey)
	}
}

func TestBuildPaletteEntries_AgentPickEntriesSupportBoundRecents(t *testing.T) {
	env := testPaletteEnv()
	env.Engine.Config = &config.Config{
		Agents: map[string]config.AgentConfig{
			"local": {Command: "local-agent"},
		},
	}
	entries := buildPaletteEntries(env, palette.ModeWindow)

	for _, id := range []string{"new-agent-pick", "new-bay-agent-pick"} {
		entry := findEntry(t, entries, id)
		if entry.ActionWithParam == nil {
			t.Fatalf("%s ActionWithParam is nil; bound recents would reopen the picker", id)
		}
		if entry.ParamValid == nil {
			t.Fatalf("%s ParamValid is nil; stale agent recents would stay visible", id)
		}
		if !entry.ParamValid("codex") {
			t.Fatalf("%s ParamValid(codex)=false; built-in agent should be valid", id)
		}
		if !entry.ParamValid("local") {
			t.Fatalf("%s ParamValid(local)=false; configured agent should be valid", id)
		}
		if entry.ParamValid("retired") {
			t.Fatalf("%s ParamValid(retired)=true; unknown agent should be filtered", id)
		}
	}
}

func TestBuildPaletteEntries_HomeEntriesAreDockScoped(t *testing.T) {
	// Home palette actions must be reachable from any dock surface,
	// even when there is no current bay. ScopeInDock satisfies that
	// (and is also satisfied when ScopeInBay applies).
	env := testPaletteEnv()
	entries := buildPaletteEntries(env, palette.ModeWindow)

	wantIDs := []string{"go-home", "new-home-shell", "edit-home", "new-home-agent", "new-home-agent-pick"}
	for _, id := range wantIDs {
		entry := findEntry(t, entries, id)
		if entry.Needs != palette.ScopeInDock {
			t.Errorf("entry %q Needs=%v; want ScopeInDock so it shows from any dock surface", id, entry.Needs)
		}
	}
}

func TestPaletteGoHome_MaterializesEmptyHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	eng, mockTmux, _, _ := testNavEngine(t)
	mockTmux.Calls = nil

	env := testPaletteEnv()
	env.Engine = eng
	env.Ctx = &engine.Context{Dock: "labs"}

	entry := findEntry(t, buildPaletteEntries(env, palette.ModeWindow), "go-home")
	if _, err := entry.Action(); err != nil {
		t.Fatalf("go-home action: %v", err)
	}

	m, _ := eng.LoadManifest()
	home := m.FindDock("labs").FindBayByID(manifest.HomeBayID)
	if home == nil || home.Type != manifest.BayTypeHome || len(home.Surfaces) != 1 {
		t.Fatalf("home = %+v, want one materialized home surface", home)
	}
	sawNewWindow := false
	for _, call := range mockTmux.Calls {
		if call.Method == "NewWindow" && len(call.Args) >= 2 && call.Args[1] == manifest.HomeBayID {
			sawNewWindow = true
		}
	}
	if !sawNewWindow {
		t.Fatalf("go-home did not create a home window; calls: %+v", mockTmux.Calls)
	}
}

func TestPaletteHomeShell_TargetsHomeNotCurrentBay(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.BayNew(engine.BayNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("BayNew: %v", err)
	}

	env := testPaletteEnv()
	env.Engine = eng
	// Pretend the user is currently inside w1 — Home shell must still
	// target home, not w1.
	env.Ctx = &engine.Context{Dock: "labs", BayID: "w1", Bay: "w1", Surface: "shell"}

	entry := findEntry(t, buildPaletteEntries(env, palette.ModeWindow), "new-home-shell")
	if _, err := entry.Action(); err != nil {
		t.Fatalf("new-home-shell action: %v", err)
	}

	bay, err := eng.BayShow("labs", manifest.HomeBayID)
	if err != nil {
		t.Fatalf("BayShow(home): %v", err)
	}
	if bay.Type != manifest.BayTypeHome || len(bay.Surfaces) != 1 {
		t.Fatalf("home bay = %+v, want one home shell", bay)
	}
	if bay.Surfaces[0].Type != manifest.SurfaceTypeShell {
		t.Errorf("home surface type = %s, want shell", bay.Surfaces[0].Type)
	}
	// w1 must be untouched aside from its initial shell.
	w1, err := eng.BayShow("labs", "w1")
	if err != nil {
		t.Fatalf("BayShow(w1): %v", err)
	}
	if len(w1.Surfaces) != 1 {
		t.Errorf("w1 surfaces=%d, want 1 (home shell must not have added one to w1)", len(w1.Surfaces))
	}
}

func TestPaletteHomeAgent_UsesDockDefaultTargetingHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.BayNew(engine.BayNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("BayNew: %v", err)
	}

	env := testPaletteEnv()
	env.Engine = eng
	env.Ctx = &engine.Context{Dock: "labs", BayID: "w1", Bay: "w1"}

	entry := findEntry(t, buildPaletteEntries(env, palette.ModeWindow), "new-home-agent")
	if _, err := entry.Action(); err != nil {
		t.Fatalf("new-home-agent action: %v", err)
	}

	bay, err := eng.BayShow("labs", manifest.HomeBayID)
	if err != nil {
		t.Fatalf("BayShow(home): %v", err)
	}
	if len(bay.Surfaces) != 1 {
		t.Fatalf("home bay surfaces = %+v, want one agent", bay.Surfaces)
	}
	added := bay.Surfaces[0]
	if added.Type != manifest.SurfaceTypeAgent {
		t.Fatalf("home surface type = %s, want agent", added.Type)
	}
	if added.Agent == nil || *added.Agent != "claude" {
		t.Fatalf("home agent = %v, want dock default claude", added.Agent)
	}
}

func TestPaletteHomeAgentPick_ValidatesAndCreatesInHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	eng, _, _, _ := testNavEngine(t)
	env := testPaletteEnv()
	env.Engine = eng
	env.Ctx = &engine.Context{Dock: "labs"}

	entry := findEntry(t, buildPaletteEntries(env, palette.ModeWindow), "new-home-agent-pick")

	if entry.ParamValid == nil || entry.ActionWithParam == nil {
		t.Fatalf("entry missing ParamValid/ActionWithParam: %+v", entry)
	}
	if !entry.ParamValid("codex") {
		t.Fatalf("ParamValid(codex)=false; built-in agent should be valid")
	}
	if entry.ParamValid("retired") {
		t.Fatalf("ParamValid(retired)=true; unknown agent should be filtered")
	}

	if _, err := entry.ActionWithParam("codex"); err != nil {
		t.Fatalf("ActionWithParam(codex): %v", err)
	}
	bay, err := eng.BayShow("labs", manifest.HomeBayID)
	if err != nil {
		t.Fatalf("BayShow(home): %v", err)
	}
	if len(bay.Surfaces) != 1 {
		t.Fatalf("home surfaces = %+v, want one", bay.Surfaces)
	}
	added := bay.Surfaces[0]
	if added.Type != manifest.SurfaceTypeAgent {
		t.Fatalf("home surface type = %s, want agent", added.Type)
	}
	if added.Agent == nil || *added.Agent != "codex" {
		t.Fatalf("home agent = %v, want codex (chosen by picker)", added.Agent)
	}
}

func TestPaletteHomeEditor_TargetsHomePath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	eng, _, _, dir := testNavEngine(t)
	// Force a known terminal editor so runEditCreate creates a tracked
	// surface (GUI editors fire-and-forget and don't persist a surface).
	eng.Config.DefaultEditor = "vim"

	env := testPaletteEnv()
	env.Engine = eng
	env.Ctx = &engine.Context{Dock: "labs"}

	entry := findEntry(t, buildPaletteEntries(env, palette.ModeWindow), "edit-home")
	if _, err := entry.Action(); err != nil {
		t.Fatalf("edit-home action: %v", err)
	}

	bay, err := eng.BayShow("labs", manifest.HomeBayID)
	if err != nil {
		t.Fatalf("BayShow(home): %v", err)
	}
	if len(bay.Surfaces) != 1 {
		t.Fatalf("home surfaces = %+v, want one editor", bay.Surfaces)
	}
	added := bay.Surfaces[0]
	if added.Type != manifest.SurfaceTypeEditor {
		t.Fatalf("home surface type = %s, want editor", added.Type)
	}
	expectedPath := filepath.Join(dir, "repos", "labs")
	if added.Command == nil || !strings.Contains(*added.Command, expectedPath) {
		t.Fatalf("editor command = %v, want one targeting dock path %q", added.Command, expectedPath)
	}
}

func TestPaletteCloseSurface_LastHomeUsesDoubleTapConfirmation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	eng, mockTmux, _, _ := testNavEngine(t)
	if err := eng.Home("labs"); err != nil {
		t.Fatalf("Home: %v", err)
	}
	m, _ := eng.LoadManifest()
	home := m.FindDock("labs").FindBayByID(manifest.HomeBayID)
	winID := home.Surfaces[0].Tmux.WindowID

	env := testPaletteEnv()
	env.Engine = eng
	env.Ctx = &engine.Context{
		Dock:    "labs",
		BayID:   manifest.HomeBayID,
		Bay:     manifest.HomeBayID,
		Surface: "shell",
	}
	entry := findEntry(t, buildPaletteEntries(env, palette.ModeWindow), "close-surface")

	if _, err := entry.Action(); err != nil {
		t.Fatalf("close-surface first: %v", err)
	}
	if exists, _ := mockTmux.WindowExists(winID); !exists {
		t.Fatalf("first palette close killed home window %s", winID)
	}
	msgs := mockTmux.DisplayMessages()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "dismiss dock") {
		t.Fatalf("first palette close messages = %v, want dismissal guidance", msgs)
	}

	if _, err := entry.Action(); err != nil {
		t.Fatalf("close-surface second: %v", err)
	}
	if has, _ := mockTmux.HasSession("labs"); has {
		t.Fatal("tmux session still exists after confirmed palette close")
	}
	m2, _ := eng.LoadManifest()
	if home := m2.FindDock("labs").FindBayByID(manifest.HomeBayID); home != nil {
		t.Fatalf("home persisted after confirmed palette close: %+v", home)
	}
}

func TestPaletteCloseBay_LastHomeUsesDoubleTapConfirmation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	eng, mockTmux, _, _ := testNavEngine(t)
	if err := eng.Home("labs"); err != nil {
		t.Fatalf("Home: %v", err)
	}
	m, _ := eng.LoadManifest()
	home := m.FindDock("labs").FindBayByID(manifest.HomeBayID)
	winID := home.Surfaces[0].Tmux.WindowID

	env := testPaletteEnv()
	env.Engine = eng
	env.Ctx = &engine.Context{
		Dock:  "labs",
		BayID: manifest.HomeBayID,
		Bay:   manifest.HomeBayID,
	}
	entry := findEntry(t, buildPaletteEntries(env, palette.ModeWindow), "close-bay")

	if _, err := entry.Action(); err != nil {
		t.Fatalf("close-bay first: %v", err)
	}
	if exists, _ := mockTmux.WindowExists(winID); !exists {
		t.Fatalf("first palette close-bay killed home window %s", winID)
	}
	msgs := mockTmux.DisplayMessages()
	if len(msgs) != 1 || !strings.Contains(msgs[0], "dismiss dock") {
		t.Fatalf("first palette close-bay messages = %v, want dismissal guidance", msgs)
	}

	if _, err := entry.Action(); err != nil {
		t.Fatalf("close-bay second: %v", err)
	}
	if has, _ := mockTmux.HasSession("labs"); has {
		t.Fatal("tmux session still exists after confirmed palette close-bay")
	}
	m2, _ := eng.LoadManifest()
	if home := m2.FindDock("labs").FindBayByID(manifest.HomeBayID); home != nil {
		t.Fatalf("home persisted after confirmed palette close-bay: %+v", home)
	}
}

// testPaletteEnv returns a minimal env suitable for exercising the
// build-entry logic. Engine is non-nil (nil would NPE in closures) but
// actions aren't invoked in these tests — we only check metadata.
func testPaletteEnv() *paletteEnv {
	return &paletteEnv{
		Engine:  &engine.Engine{},
		Ctx:     &engine.Context{Dock: "testdock", Bay: "testbay", Surface: "testsf"},
		Scope:   palette.ScopeInBay,
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
