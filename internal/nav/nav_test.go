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

	// Current is @1 (waiting). Next waiting should be @3.
	next := NextWaiting(entries, "@1")
	if next == nil {
		t.Fatal("expected non-nil result")
	}
	if next.TmuxWindowID != "@3" {
		t.Errorf("expected @3, got %q", next.TmuxWindowID)
	}

	// Current is @3 (waiting). Next waiting should be @4.
	next = NextWaiting(entries, "@3")
	if next == nil {
		t.Fatal("expected non-nil result")
	}
	if next.TmuxWindowID != "@4" {
		t.Errorf("expected @4, got %q", next.TmuxWindowID)
	}

	// Current is @4 (waiting, last). Should wrap around to @1.
	next = NextWaiting(entries, "@4")
	if next == nil {
		t.Fatal("expected non-nil result")
	}
	if next.TmuxWindowID != "@1" {
		t.Errorf("expected @1 (wrap), got %q", next.TmuxWindowID)
	}

	// Current is @2 (not waiting). Next waiting after @2 is @3.
	next = NextWaiting(entries, "@2")
	if next == nil {
		t.Fatal("expected non-nil result")
	}
	if next.TmuxWindowID != "@3" {
		t.Errorf("expected @3, got %q", next.TmuxWindowID)
	}
}

func TestNextWaiting_NoWaiting(t *testing.T) {
	entries := []Entry{
		{WsName: "a", TmuxWindowID: "@1", Waiting: false},
		{WsName: "b", TmuxWindowID: "@2", Waiting: false},
	}

	next := NextWaiting(entries, "@1")
	if next != nil {
		t.Errorf("expected nil when no waiting entries, got %v", next)
	}
}

func TestNextWaiting_UnknownCurrent(t *testing.T) {
	entries := []Entry{
		{WsName: "a", TmuxWindowID: "@1", Waiting: true},
		{WsName: "b", TmuxWindowID: "@2", Waiting: true},
	}

	// Current window not in list; should return first waiting entry.
	next := NextWaiting(entries, "@99")
	if next == nil {
		t.Fatal("expected non-nil result")
	}
	if next.TmuxWindowID != "@1" {
		t.Errorf("expected @1, got %q", next.TmuxWindowID)
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
