package nav

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

// buildTestManifest creates a manifest with two docks and several workspaces.
func buildTestManifest() *manifest.Manifest {
	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "labs",
			Workspaces: []manifest.Workspace{
				{
					Name:     "mem-refactor",
					Status:   manifest.WorkspaceStatusActive,
					Worktree: &manifest.WorktreeAttrs{Repo: "labs", Branch: "feature/refactor-memory-access", PR: "234"},
					Surfaces: []manifest.Surface{
						{ID: 1, Name: "agent", Type: manifest.SurfaceTypeAgent, Backend: manifest.SurfaceBackendTmux, Tmux: &manifest.TmuxAttrs{WindowID: "@1", PaneID: "%1", LayoutGroup: 1}},
						{ID: 2, Name: "shell", Type: manifest.SurfaceTypeShell, Backend: manifest.SurfaceBackendTmux, Tmux: &manifest.TmuxAttrs{WindowID: "@1", PaneID: "%2", LayoutGroup: 1}},
					},
				},
				{
					Name:     "fix-auth",
					Status:   manifest.WorkspaceStatusIdle,
					Worktree: &manifest.WorktreeAttrs{Repo: "labs", Branch: "bugfix/auth-timeout", PR: "567"},
					Surfaces: []manifest.Surface{
						{ID: 1, Name: "agent", Type: manifest.SurfaceTypeAgent, Backend: manifest.SurfaceBackendTmux, Tmux: &manifest.TmuxAttrs{WindowID: "@3", PaneID: "%5", LayoutGroup: 1}},
					},
				},
			},
		},
		{
			Name: "core",
			Workspaces: []manifest.Workspace{
				{
					Name:     "nav-feature",
					Status:   manifest.WorkspaceStatusActive,
					Worktree: &manifest.WorktreeAttrs{Repo: "core", Branch: "feature/nav-support"},
					Surfaces: []manifest.Surface{
						{ID: 1, Name: "agent", Type: manifest.SurfaceTypeAgent, Backend: manifest.SurfaceBackendTmux, Tmux: &manifest.TmuxAttrs{WindowID: "@4", PaneID: "%7", LayoutGroup: 1}},
					},
				},
			},
		},
	}
	return m
}

// buildTestTmux creates a tmux mock with windows and waiting options set.
func buildTestTmux(waitingWindows map[string]bool) *tmux.Mock {
	mock := tmux.NewMock()
	mock.NewSession("bay")
	w1, _ := mock.NewWindow("bay", "main", "/tmp")
	w3, _ := mock.NewWindow("bay", "main", "/tmp")
	w4, _ := mock.NewWindow("bay", "main", "/tmp")

	ids := []string{w1, w3, w4}
	for _, id := range ids {
		if waitingWindows[id] {
			mock.SetWindowOption(id, "@bay-waiting", "1")
		}
	}

	mock.Calls = nil
	return mock
}

func TestCollectEntries(t *testing.T) {
	m := buildTestManifest()
	tc := buildTestTmux(map[string]bool{"@1": true, "@3": true})

	entries := CollectEntries(m, tc)

	// We expect 3 entries: one per workspace.
	// labs has 2 workspaces, core has 1.
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Build a map for easier lookup.
	byName := make(map[string]Entry)
	for _, e := range entries {
		byName[e.WsName] = e
	}

	// Check labs:mem-refactor
	e1 := byName["mem-refactor"]
	if e1.DockName != "labs" {
		t.Errorf("mem-refactor DockName: got %q, want %q", e1.DockName, "labs")
	}
	if e1.Branch != "feature/refactor-memory-access" {
		t.Errorf("mem-refactor Branch: got %q, want %q", e1.Branch, "feature/refactor-memory-access")
	}
	if e1.PR != "234" {
		t.Errorf("mem-refactor PR: got %q, want %q", e1.PR, "234")
	}
	if e1.Status != manifest.WorkspaceStatusActive {
		t.Errorf("mem-refactor Status: got %q, want %q", e1.Status, manifest.WorkspaceStatusActive)
	}
	if e1.SurfaceCount != 2 {
		t.Errorf("mem-refactor SurfaceCount: got %d, want 2", e1.SurfaceCount)
	}

	// Check labs:fix-auth (waiting)
	e2 := byName["fix-auth"]
	if !e2.Waiting {
		t.Error("fix-auth should be waiting (window @3)")
	}
	if e2.WsName != "fix-auth" {
		t.Errorf("fix-auth WsName: got %q, want %q", e2.WsName, "fix-auth")
	}

	// Check core:nav-feature
	e3 := byName["nav-feature"]
	if e3.DockName != "core" {
		t.Errorf("nav-feature DockName: got %q, want %q", e3.DockName, "core")
	}
}

func TestCollectEntries_EmptyManifest(t *testing.T) {
	m := manifest.New()
	tc := tmux.NewMock()

	entries := CollectEntries(m, tc)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestFuzzyMatch_NoQuery(t *testing.T) {
	entries := []Entry{
		{WsName: "a", DockName: "d1"},
		{WsName: "b", DockName: "d2"},
	}
	result := FuzzyMatch(entries, "")
	if len(result) != 2 {
		t.Fatalf("empty query should return all entries; got %d", len(result))
	}
}

func TestFuzzyMatch_ByName(t *testing.T) {
	entries := []Entry{
		{WsName: "mem-refactor", Branch: "feature/mem", PR: "100", DockName: "labs"},
		{WsName: "fix-auth", Branch: "bugfix/auth", PR: "200", DockName: "core"},
		{WsName: "nav-feature", Branch: "feature/nav", PR: "300", DockName: "labs"},
	}

	result := FuzzyMatch(entries, "mem")
	if len(result) != 1 {
		t.Fatalf("expected 1 match for 'mem', got %d", len(result))
	}
	if result[0].WsName != "mem-refactor" {
		t.Errorf("expected mem-refactor, got %q", result[0].WsName)
	}
}

func TestFuzzyMatch_ByBranch(t *testing.T) {
	entries := []Entry{
		{WsName: "mem-refactor", Branch: "feature/refactor-memory-access", PR: "100", DockName: "labs"},
		{WsName: "fix-auth", Branch: "bugfix/auth-timeout", PR: "200", DockName: "core"},
	}

	result := FuzzyMatch(entries, "auth-timeout")
	if len(result) != 1 {
		t.Fatalf("expected 1 match for 'auth-timeout', got %d", len(result))
	}
	if result[0].WsName != "fix-auth" {
		t.Errorf("expected fix-auth, got %q", result[0].WsName)
	}
}

func TestFuzzyMatch_ByPR(t *testing.T) {
	entries := []Entry{
		{WsName: "mem-refactor", Branch: "feature/mem", PR: "234", DockName: "labs"},
		{WsName: "fix-auth", Branch: "bugfix/auth", PR: "567", DockName: "core"},
	}

	result := FuzzyMatch(entries, "234")
	if len(result) != 1 {
		t.Fatalf("expected 1 match for '234', got %d", len(result))
	}
	if result[0].PR != "234" {
		t.Errorf("expected PR 234, got %q", result[0].PR)
	}
}

func TestFuzzyMatch_ByDockName(t *testing.T) {
	entries := []Entry{
		{WsName: "mem-refactor", Branch: "feature/mem", PR: "234", DockName: "labs"},
		{WsName: "fix-auth", Branch: "bugfix/auth", PR: "567", DockName: "core"},
	}

	result := FuzzyMatch(entries, "core")
	if len(result) != 1 {
		t.Fatalf("expected 1 match for 'core', got %d", len(result))
	}
	if result[0].DockName != "core" {
		t.Errorf("expected dock core, got %q", result[0].DockName)
	}
}

func TestFuzzyMatch_CaseInsensitive(t *testing.T) {
	entries := []Entry{
		{WsName: "Mem-Refactor", Branch: "Feature/MEM", PR: "234", DockName: "Labs"},
	}

	result := FuzzyMatch(entries, "mem")
	if len(result) != 1 {
		t.Fatalf("case-insensitive match should find 1 entry, got %d", len(result))
	}
}

func TestFuzzyMatch_MultipleMatches(t *testing.T) {
	entries := []Entry{
		{WsName: "feature-a", Branch: "feature/a", PR: "100", DockName: "labs"},
		{WsName: "feature-b", Branch: "feature/b", PR: "200", DockName: "labs"},
		{WsName: "bugfix-c", Branch: "bugfix/c", PR: "300", DockName: "core"},
	}

	result := FuzzyMatch(entries, "feature")
	if len(result) != 2 {
		t.Fatalf("expected 2 matches for 'feature', got %d", len(result))
	}
}

func TestFuzzyMatch_NoMatch(t *testing.T) {
	entries := []Entry{
		{WsName: "mem-refactor", Branch: "feature/mem", PR: "234", DockName: "labs"},
	}

	result := FuzzyMatch(entries, "zzz-nonexistent")
	if len(result) != 0 {
		t.Fatalf("expected 0 matches for 'zzz-nonexistent', got %d", len(result))
	}
}

func TestFilterWaiting(t *testing.T) {
	entries := []Entry{
		{WsName: "a", Waiting: true},
		{WsName: "b", Waiting: false},
		{WsName: "c", Waiting: true},
		{WsName: "d", Waiting: false},
	}

	result := FilterWaiting(entries)
	if len(result) != 2 {
		t.Fatalf("expected 2 waiting entries, got %d", len(result))
	}
	for _, e := range result {
		if !e.Waiting {
			t.Errorf("entry %q should be waiting", e.WsName)
		}
	}
}

func TestFilterWaiting_NoneWaiting(t *testing.T) {
	entries := []Entry{
		{WsName: "a", Waiting: false},
		{WsName: "b", Waiting: false},
	}

	result := FilterWaiting(entries)
	if len(result) != 0 {
		t.Fatalf("expected 0 waiting entries, got %d", len(result))
	}
}

func TestNextWaiting_CyclesCorrectly(t *testing.T) {
	entries := []Entry{
		{WsName: "a", TmuxWindowID: "@1", Waiting: true},
		{WsName: "b", TmuxWindowID: "@2", Waiting: false},
		{WsName: "c", TmuxWindowID: "@3", Waiting: true},
		{WsName: "d", TmuxWindowID: "@4", Waiting: true},
	}

	cases := []struct {
		current string
		wantWin string
		wantIdx int
	}{
		{"@1", "@3", 2}, // current is @1 (waiting); next waiting is @3
		{"@3", "@4", 3}, // current is @3 (waiting); next waiting is @4
		{"@4", "@1", 0}, // current is @4 (last waiting); wraps to @1
		{"@2", "@3", 2}, // current is @2 (not waiting); next waiting is @3
	}
	for _, tc := range cases {
		next, idx := NextWaiting(entries, tc.current)
		if next == nil {
			t.Fatalf("current=%q: expected non-nil", tc.current)
		}
		if next.TmuxWindowID != tc.wantWin || idx != tc.wantIdx {
			t.Errorf("current=%q: got (%q, %d), want (%q, %d)",
				tc.current, next.TmuxWindowID, idx, tc.wantWin, tc.wantIdx)
		}
	}
}

func TestNextWaiting_NoWaiting(t *testing.T) {
	entries := []Entry{
		{WsName: "a", TmuxWindowID: "@1", Waiting: false},
		{WsName: "b", TmuxWindowID: "@2", Waiting: false},
	}

	next, idx := NextWaiting(entries, "@1")
	if next != nil || idx != -1 {
		t.Errorf("expected (nil, -1), got (%v, %d)", next, idx)
	}
}

func TestNextWaiting_UnknownCurrent(t *testing.T) {
	entries := []Entry{
		{WsName: "a", TmuxWindowID: "@1", Waiting: true},
		{WsName: "b", TmuxWindowID: "@2", Waiting: true},
	}

	// Current window not in list; should return first waiting entry.
	next, idx := NextWaiting(entries, "@99")
	if next == nil {
		t.Fatal("expected non-nil result")
	}
	if next.TmuxWindowID != "@1" || idx != 0 {
		t.Errorf("expected (@1, 0), got (%q, %d)", next.TmuxWindowID, idx)
	}
}

func TestFormatEntry(t *testing.T) {
	e := Entry{
		DockName:     "labs",
		WsName:       "mem-refactor",
		Branch:       "feature/refactor-memory-access",
		PR:           "234",
		Status:       manifest.WorkspaceStatusActive,
		TmuxWindowID: "@1",
		Waiting:      false,
	}

	s := FormatEntry(e)

	for _, want := range []string{"labs", "mem-refactor", "feature/refactor-memory-access", "#234", "active"} {
		if !strings.Contains(s, want) {
			t.Errorf("FormatEntry missing %q in output: %q", want, s)
		}
	}
}

func TestFormatEntry_NoPR(t *testing.T) {
	e := Entry{
		DockName:     "core",
		WsName:       "nav-feature",
		Branch:       "feature/nav",
		PR:           "",
		Status:       manifest.WorkspaceStatusIdle,
		TmuxWindowID: "@4",
		Waiting:      false,
	}

	s := FormatEntry(e)
	if strings.Contains(s, "#") {
		t.Errorf("FormatEntry should not include '#' when PR is empty: %q", s)
	}
}

func TestFormatEntry_Waiting(t *testing.T) {
	e := Entry{
		DockName:     "labs",
		WsName:       "fix-auth",
		Branch:       "bugfix/auth",
		PR:           "567",
		Status:       manifest.WorkspaceStatusIdle,
		TmuxWindowID: "@3",
		Waiting:      true,
	}

	s := FormatEntry(e)
	if !strings.Contains(s, "WAITING") {
		t.Errorf("FormatEntry should include WAITING indicator: %q", s)
	}
}

func TestFormatEntries(t *testing.T) {
	entries := []Entry{
		{DockName: "labs", WsName: "mem-refactor", Branch: "feature/refactor-memory-access", PR: "234", Status: manifest.WorkspaceStatusActive, TmuxWindowID: "@1"},
		{DockName: "core", WsName: "nav", Branch: "feature/nav", PR: "", Status: manifest.WorkspaceStatusIdle, TmuxWindowID: "@4"},
	}

	s := FormatEntries(entries)

	// Should contain both entries.
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), s)
	}

	// Both lines should have the same length (column-aligned).
	if len(lines[0]) != len(lines[1]) {
		t.Errorf("lines should be aligned to same length:\n  %q (%d)\n  %q (%d)",
			lines[0], len(lines[0]), lines[1], len(lines[1]))
	}
}

func TestFormatEntries_Empty(t *testing.T) {
	s := FormatEntries(nil)
	if s != "" {
		t.Errorf("expected empty string for nil entries, got %q", s)
	}
}

// --- Surface-level navigation tests ---

func buildSurfaceWorkspace() *manifest.Workspace {
	agent := "claude"
	cmd := "npm test"
	return &manifest.Workspace{
		Name: "auth-fix",
		Path: "/projects/auth-fix",
		Surfaces: []manifest.Surface{
			{ID: 1, Name: "agent", Type: manifest.SurfaceTypeAgent, Backend: manifest.SurfaceBackendTmux, Agent: &agent, Tmux: &manifest.TmuxAttrs{PaneID: "%1", WindowID: "@1", LayoutGroup: 1}},
			{ID: 2, Name: "shell", Type: manifest.SurfaceTypeShell, Backend: manifest.SurfaceBackendTmux, Tmux: &manifest.TmuxAttrs{PaneID: "%2", WindowID: "@1", LayoutGroup: 1}},
			{ID: 3, Name: "tests", Type: manifest.SurfaceTypeCmd, Backend: manifest.SurfaceBackendTmux, Command: &cmd, Tmux: &manifest.TmuxAttrs{PaneID: "%3", WindowID: "@2", LayoutGroup: 2}},
			{ID: 4, Name: "editor", Type: manifest.SurfaceTypeEditor, Backend: manifest.SurfaceBackendGUI, GUI: &manifest.GUIAttrs{AppCommand: "cursor", PID: 99999}},
		},
	}
}

func TestCollectSurfaces_IncludesGUI(t *testing.T) {
	ws := buildSurfaceWorkspace()
	mock := tmux.NewMock()

	entries := CollectSurfaces(ws, mock, "")

	if len(entries) != 4 {
		t.Fatalf("expected 4 surface entries (3 tmux + 1 gui), got %d", len(entries))
	}

	gui := entries[3]
	if gui.Name != "editor" || gui.Type != "editor" {
		t.Errorf("gui entry: name=%q type=%q", gui.Name, gui.Type)
	}
	if gui.AppCommand != "cursor" {
		t.Errorf("gui entry: app_command=%q, want cursor", gui.AppCommand)
	}
	if gui.PaneID != "" || gui.WindowID != "" {
		t.Error("gui entry should have no tmux IDs")
	}
}

func TestCollectSurfaces(t *testing.T) {
	ws := buildSurfaceWorkspace()
	mock := tmux.NewMock()

	entries := CollectSurfaces(ws, mock, "%2")

	if len(entries) != 4 {
		t.Fatalf("expected 4 surface entries, got %d", len(entries))
	}
	if entries[0].Name != "agent" || entries[0].Type != "agent" {
		t.Errorf("entry 0: name=%q type=%q", entries[0].Name, entries[0].Type)
	}
	if entries[1].Name != "shell" || !entries[1].Current {
		t.Errorf("entry 1: name=%q current=%v, want shell/true", entries[1].Name, entries[1].Current)
	}
	if entries[2].Name != "tests" || entries[2].Type != "cmd" {
		t.Errorf("entry 2: name=%q type=%q", entries[2].Name, entries[2].Type)
	}
}

func TestCollectSurfaces_NoCurrent(t *testing.T) {
	ws := buildSurfaceWorkspace()
	mock := tmux.NewMock()

	entries := CollectSurfaces(ws, mock, "%999")

	for _, e := range entries {
		if e.Current {
			t.Errorf("no entry should be current when pane ID doesn't match, got %q", e.Name)
		}
	}
}

func TestNextSurface(t *testing.T) {
	ws := buildSurfaceWorkspace()
	mock := tmux.NewMock()
	entries := CollectSurfaces(ws, mock, "%1") // current = agent (index 0)

	next, idx := NextSurface(entries)
	if next == nil || next.Name != "shell" || idx != 1 {
		t.Errorf("next after agent: got (%v, %d), want (shell, 1)", next, idx)
	}
}

func TestNextSurface_WrapsAround(t *testing.T) {
	ws := buildSurfaceWorkspace()
	mock := tmux.NewMock()
	// Current = editor (index 3, last). Next should wrap to agent (index 0).
	// Editor is GUI so we mark current by absence — set a non-matching pane
	// and manually mark the entry.
	entries := CollectSurfaces(ws, mock, "")
	entries[3].Current = true

	next, idx := NextSurface(entries)
	if next == nil || next.Name != "agent" || idx != 0 {
		t.Errorf("next after last: got (%v, %d), want (agent, 0)", next, idx)
	}
}

func TestPrevSurface(t *testing.T) {
	ws := buildSurfaceWorkspace()
	mock := tmux.NewMock()
	entries := CollectSurfaces(ws, mock, "%2") // current = shell (index 1)

	prev, idx := PrevSurface(entries)
	if prev == nil || prev.Name != "agent" || idx != 0 {
		t.Errorf("prev before shell: got (%v, %d), want (agent, 0)", prev, idx)
	}
}

func TestPrevSurface_WrapsAround(t *testing.T) {
	ws := buildSurfaceWorkspace()
	mock := tmux.NewMock()
	entries := CollectSurfaces(ws, mock, "%1") // current = agent (index 0, first)

	prev, idx := PrevSurface(entries)
	if prev == nil || prev.Name != "editor" || idx != 3 {
		t.Errorf("prev before first: got (%v, %d), want (editor, 3)", prev, idx)
	}
}

func TestSurfaceByIndex(t *testing.T) {
	ws := buildSurfaceWorkspace()
	mock := tmux.NewMock()
	entries := CollectSurfaces(ws, mock, "")

	s := SurfaceByIndex(entries, 1)
	if s == nil || s.Name != "agent" {
		t.Errorf("index 1 should be agent, got %v", s)
	}
	s = SurfaceByIndex(entries, 3)
	if s == nil || s.Name != "tests" {
		t.Errorf("index 3 should be tests, got %v", s)
	}
	s = SurfaceByIndex(entries, 0)
	if s != nil {
		t.Error("index 0 should be nil (1-based)")
	}
	s = SurfaceByIndex(entries, 4)
	if s == nil || s.Name != "editor" {
		t.Errorf("index 4 should be editor, got %v", s)
	}
	s = SurfaceByIndex(entries, 5)
	if s != nil {
		t.Error("index 5 should be nil (out of range)")
	}
}

func TestNextSurface_NoCurrent(t *testing.T) {
	ws := buildSurfaceWorkspace()
	mock := tmux.NewMock()
	entries := CollectSurfaces(ws, mock, "%999") // no match

	next, idx := NextSurface(entries)
	if next == nil || next.Name != "agent" || idx != 0 {
		t.Errorf("with no current: got (%v, %d), want (agent, 0)", next, idx)
	}
}
