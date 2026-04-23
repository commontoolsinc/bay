package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
)

func testDocks() []engine.DockInfo {
	return []engine.DockInfo{
		{
			Name: "api",
			Repo: "bay",
			Workspaces: []engine.WorkspaceInfo{
				{
					Name:         "auth-fix",
					Type:         "worktree",
					Path:         "~/projects/bay-wt/auth-fix",
					Branch:       "fix/login",
					Dirty:        true,
					Waiting:      true,
					SurfaceCount: 3,
					Surfaces: []engine.SurfaceInfo{
						{ID: 1, Name: "shell", Type: "shell", Backend: "tmux-pane", Status: "ok"},
						{ID: 2, Name: "agent", Type: "agent", Agent: "codex", Backend: "tmux-pane", Status: "ok"},
						{ID: 3, Name: "tests", Type: "cmd", Command: "npm test --watch=false", Backend: "tmux-pane", Status: "ok"},
					},
					SyncStatus: "ok",
				},
				{
					Name:         "cleanup",
					Type:         "worktree",
					Path:         "~/projects/bay-wt/cleanup",
					Branch:       "cleanup",
					SurfaceCount: 1,
					SyncStatus:   "stale",
					Surfaces: []engine.SurfaceInfo{
						{ID: 1, Name: "shell", Type: "shell", Backend: "tmux-pane", Status: "stale"},
					},
				},
			},
		},
		{
			Name: "web",
			Repo: "bay",
			Workspaces: []engine.WorkspaceInfo{
				{
					Name:         "landing",
					Type:         "worktree",
					Path:         "~/projects/bay-wt-web/landing",
					Branch:       "feature/landing",
					SurfaceCount: 1,
					SyncStatus:   "ok",
				},
			},
		},
		{
			Name: "ops",
			Repo: "other",
			Workspaces: []engine.WorkspaceInfo{
				{
					Name:         "deploy",
					Type:         "external",
					Path:         "~/projects/other/deploy",
					Branch:       "main",
					Pending:      true,
					SurfaceCount: 0,
					SyncStatus:   "missing",
				},
			},
		},
	}
}

func TestBuildListView_DefaultIncludesRepos(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{})
	if len(view.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(view.Repos))
	}
	if view.Focus.Kind != FocusAll {
		t.Fatalf("focus = %q, want %q", view.Focus.Kind, FocusAll)
	}
}

// TestAlignWidth_IgnoresRowsWithoutMeta verifies the load-bearing rule:
// rows with empty meta don't contribute to the alignment width, so a long
// meta-less row doesn't push the meta column out for its meta-having
// siblings. This is the case the FormatListView caller relies on, but
// it's hard to construct via the public DockInfo API — so we exercise
// alignWidth directly.
func TestAlignWidth_IgnoresRowsWithoutMeta(t *testing.T) {
	rows := []alignedRow{
		{prefix: "short", prefixWidth: 5, metaCols: []metaCol{{text: "branch=foo", width: 10}}},
		{prefix: "longer-prefix-with-no-meta", prefixWidth: 26, metaCols: nil},
		{prefix: "medium", prefixWidth: 6, metaCols: []metaCol{{text: "status=active", width: 13}}},
	}
	got := alignWidth(rows)
	if got != 6 {
		t.Errorf("alignWidth = %d, want 6 (longest-with-meta is 'medium' at width 6)", got)
	}

	// Empty input.
	if alignWidth(nil) != 0 {
		t.Errorf("alignWidth(nil) = %d, want 0", alignWidth(nil))
	}

	// All rows meta-less.
	allEmpty := []alignedRow{
		{prefix: "a", prefixWidth: 1, metaCols: nil},
		{prefix: "bb", prefixWidth: 2, metaCols: nil},
	}
	if alignWidth(allEmpty) != 0 {
		t.Errorf("alignWidth(all-meta-less) = %d, want 0", alignWidth(allEmpty))
	}
}

// TestFormatListView_AlignsMetaColumnsAcrossRows verifies that individual
// meta fields (e.g. surfaces=) align column-by-column across sibling rows,
// even when some rows have more fields than others (branch, status, etc.).
func TestFormatListView_AlignsMetaColumnsAcrossRows(t *testing.T) {
	docks := []engine.DockInfo{
		{
			Name: "api",
			Repo: "bay",
			Workspaces: []engine.WorkspaceInfo{
				{Name: "w1", SurfaceCount: 1, SyncStatus: "ok"},
				{Name: "login-bug", Branch: "fix/login-bug", Dirty: true, SurfaceCount: 1, SyncStatus: "ok"},
				{Name: "auth-refactor", Branch: "fix/auth-refactor", Dirty: true, SurfaceCount: 2, SyncStatus: "ok"},
			},
		},
	}
	view := BuildListView(docks, ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false, false))

	// n= should appear at the same column on all three rows.
	var surfCols []int
	for _, line := range strings.Split(out, "\n") {
		if idx := strings.Index(line, "n="); idx >= 0 {
			surfCols = append(surfCols, idx)
		}
	}
	if len(surfCols) != 3 {
		t.Fatalf("expected 3 n= entries, got %d:\n%s", len(surfCols), out)
	}
	for i := 1; i < len(surfCols); i++ {
		if surfCols[i] != surfCols[0] {
			t.Errorf("surfaces= columns not aligned: %v\noutput:\n%s", surfCols, out)
			break
		}
	}
}

// TestFormatListView_AlignsWorkspaceMetaWithinDock verifies that the meta
// column on workspace lines starts at the same screen column for every
// workspace in a dock, regardless of name length. This is the readability
// fix that turns a key=value run-on into a scannable column.
func TestFormatListView_AlignsWorkspaceMetaWithinDock(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false, false))

	var authLine, cleanupLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "ws auth-fix") {
			authLine = line
		}
		if strings.Contains(line, "ws cleanup") {
			cleanupLine = line
		}
	}
	if authLine == "" || cleanupLine == "" {
		t.Fatalf("missing workspace lines:\n%s", out)
	}

	authMetaStart := strings.Index(authLine, "br=")
	cleanupMetaStart := strings.Index(cleanupLine, "br=")
	if authMetaStart < 0 || cleanupMetaStart < 0 {
		t.Fatalf("could not find meta start:\nauth:    %q\ncleanup: %q", authLine, cleanupLine)
	}
	if authMetaStart != cleanupMetaStart {
		t.Errorf("workspace meta columns not aligned:\n  auth-fix br= at column %d\n  cleanup  br= at column %d\n  auth:    %q\n  cleanup: %q",
			authMetaStart, cleanupMetaStart, authLine, cleanupLine)
	}
}

// TestFormatListView_AlignsSurfaceMetaAcrossDock verifies that in tree
// mode, surface meta columns are aligned across ALL workspaces in a dock,
// not just within each workspace. A long surface name in one workspace
// pushes the meta column out for sibling surfaces under other workspaces
// too — the user's eye doesn't need to recalibrate as they scroll.
func TestFormatListView_AlignsSurfaceMetaAcrossDock(t *testing.T) {
	// Two workspaces in the same dock with different longest-surface
	// names: auth-fix has "tests" (5 chars) and shorter; cleanup has
	// only "shell" (5 chars). Both should align to the same dock-wide
	// max so the meta column is stable.
	docks := []engine.DockInfo{
		{
			Name: "api",
			Repo: "bay",
			Workspaces: []engine.WorkspaceInfo{
				{
					Name: "auth-fix",
					Surfaces: []engine.SurfaceInfo{
						{ID: 1, Name: "agent", Type: "agent", Agent: "claude"},
						{ID: 2, Name: "monitor-pane", Type: "cmd", Command: "top"},
					},
				},
				{
					Name: "cleanup",
					Surfaces: []engine.SurfaceInfo{
						{ID: 1, Name: "sh", Type: "shell"},
					},
				},
			},
		},
	}
	view := BuildListView(docks, ListViewOptions{
		Focus:     ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
		Recursive: true,
	})
	out := stripANSI(FormatListView(view, false, false))

	// All three surface lines (across two workspaces) should have
	// "type=" at the same column.
	var typeColumns []int
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "sf ") {
			continue
		}
		idx := strings.Index(line, "ty=")
		if idx < 0 {
			t.Fatalf("surface line missing ty= column: %q", line)
		}
		typeColumns = append(typeColumns, idx)
	}
	if len(typeColumns) != 3 {
		t.Fatalf("expected 3 surface lines, got %d:\n%s", len(typeColumns), out)
	}
	for i := 1; i < len(typeColumns); i++ {
		if typeColumns[i] != typeColumns[0] {
			t.Errorf("surface meta columns not aligned across dock: %v\noutput:\n%s", typeColumns, out)
		}
	}
}

func TestBuildListView_DockFocusStopsAtWorkspacesByDefault(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false, false))

	if !strings.Contains(out, "dk api") {
		t.Fatalf("dock focus output missing dock header:\n%s", out)
	}
	if !strings.Contains(out, "ws auth-fix") || !strings.Contains(out, "ws cleanup") {
		t.Fatalf("dock focus output missing workspaces:\n%s", out)
	}
	if strings.Contains(out, "sf ") {
		t.Fatalf("dock focus default should not recurse into surfaces:\n%s", out)
	}
}

func TestBuildListView_WorkspaceFocusShowsFullTree(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "auth-fix"},
	})
	out := stripANSI(FormatListView(view, false, false))

	for _, want := range []string{
		"rp bay",
		"dk api",
		"ws auth-fix",
		"sf agent",
		"ty=agent",
		"ag=codex",
		"ty=cmd",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace focus output missing %q:\n%s", want, out)
		}
	}
}

func TestBuildListView_WorkspaceFocusRestrictsToDock(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "auth-fix"},
	})
	out := stripANSI(FormatListView(view, false, false))

	if strings.Contains(out, "ws landing") {
		t.Fatalf("workspace focus should not include workspaces from other docks:\n%s", out)
	}
}

func TestFormatListView_SuppressesSyncOKAndShowsStale(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false, false))

	if strings.Contains(out, "sy=ok") {
		t.Fatalf("sy=ok should be suppressed:\n%s", out)
	}
	if !strings.Contains(out, "sy=stale") {
		t.Fatalf("stale sync state should be shown:\n%s", out)
	}
}

func TestFormatWorkspaceShow_RendersMultiLineDescription(t *testing.T) {
	ws := &engine.WorkspaceInfo{
		Name:        "auth-fix",
		Description: "Login flow fixes\n\nhit rebase conflict on helper.ts\ntests green except auth_test.go",
		Type:        "worktree",
		Path:        "~/x",
		SyncStatus:  "ok",
	}
	out := stripANSI(FormatWorkspaceShow("bay", "api", ws, false))
	// First line is shown inline with the description label; each body
	// line is on its own row indented to the value column.
	for _, want := range []string{
		"description Login flow fixes",
		"hit rebase conflict on helper.ts",
		"tests green except auth_test.go",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace show missing %q:\n%s", want, out)
		}
	}
}

func TestFormatWorkspaceShow_IncludesDefaultAgentAndSurfaces(t *testing.T) {
	ws := &engine.WorkspaceInfo{
		Name:         "auth-fix",
		Type:         "worktree",
		Path:         "~/projects/bay-wt/auth-fix",
		Branch:       "fix/login",
		Dirty:        true,
		DefaultAgent: "codex",
		SyncStatus:   "ok",
		Surfaces: []engine.SurfaceInfo{
			{ID: 1, Name: "shell", Type: "shell", Status: "ok"},
			{ID: 2, Name: "agent", Type: "agent", Agent: "codex", Status: "ok"},
		},
	}

	out := stripANSI(FormatWorkspaceShow("bay", "api", ws, false))
	for _, want := range []string{
		"workspace auth-fix",
		"repo bay",
		"dock api",
		"default agent codex",
		"surface agent",
		"ty=agent",
		"ag=codex",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace show missing %q:\n%s", want, out)
		}
	}
}

func TestLabelValueFormatsHumanReadableLabels(t *testing.T) {
	if got := stripANSI(labelValue("repo", "bay")); got != "repo bay" {
		t.Fatalf("labelValue() = %q, want %q", got, "repo bay")
	}
}

func TestFormatListRows_DenormalizesSurfaceRows(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus:     ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "auth-fix"},
		Recursive: true,
	})
	rows := ListRows(view)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	s := string(data)
	for _, want := range []string{`"surface_type":"agent"`, `"surface_agent":"codex"`, `"surface_type":"cmd"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("rows JSON missing %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, `"workspace_waiting":true`) {
		t.Fatalf("rows JSON missing workspace waiting flag:\n%s", s)
	}
}

func TestBuildListView_FocusRepoFiltersRepos(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusRepo, Repo: "bay"},
	})
	out := stripANSI(FormatListView(view, false, false))

	if !strings.Contains(out, "rp bay") {
		t.Fatalf("repo-focused output missing target repo:\n%s", out)
	}
	if strings.Contains(out, "rp other") {
		t.Fatalf("repo-focused output should not include other repos:\n%s", out)
	}
}

func TestFormatListView_ShowsWaitingIndicator(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false, false))

	if !strings.Contains(out, "\u23f3") {
		t.Fatalf("expected waiting indicator in output:\n%s", out)
	}
}

func TestFormatListView_HighlightsCurrentContext(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	view.CurrentDock = "api"
	view.CurrentWs = "auth-fix"

	out := stripANSI(FormatListView(view, false, false))

	if !strings.Contains(out, "dk api *") {
		t.Fatalf("current dock should have * marker:\n%s", out)
	}
	if !strings.Contains(out, "ws auth-fix *") {
		t.Fatalf("current workspace should have * marker:\n%s", out)
	}
	// Non-current workspace should NOT have marker.
	if strings.Contains(out, "ws cleanup *") {
		t.Fatalf("non-current workspace should not have * marker:\n%s", out)
	}
}

func TestFormatListView_NoHighlightWithoutContext(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	// No CurrentDock/CurrentWs set.

	out := stripANSI(FormatListView(view, false, false))

	if strings.Contains(out, " *") {
		t.Fatalf("should have no * markers without current context:\n%s", out)
	}
}

func TestDetectStdoutWidth_ColumnsOverride(t *testing.T) {
	t.Setenv("COLUMNS", "200")
	if got := detectStdoutWidth(); got != 200 {
		t.Errorf("detectStdoutWidth() with COLUMNS=200 = %d, want 200", got)
	}

	t.Setenv("COLUMNS", "0")
	// COLUMNS=0 is ignored; real tty width (or 0 if piped) wins.
	if got := detectStdoutWidth(); got < 0 {
		t.Errorf("detectStdoutWidth() with COLUMNS=0 = %d, want >=0 (fallback)", got)
	}

	t.Setenv("COLUMNS", "garbage")
	if got := detectStdoutWidth(); got < 0 {
		t.Errorf("detectStdoutWidth() with COLUMNS=garbage = %d, want >=0 (fallback)", got)
	}
}

func TestDescStrMaxForWidth(t *testing.T) {
	// Long mode: overhead is 6 (1 separator + 2 quotes + 3 "ds=").
	tests := []struct {
		name         string
		termWidth    int
		nonDescWidth int
		short        bool
		want         int
	}{
		{"piped returns full cap", 0, 100, false, listDescPipedStrMax},
		{"narrow terminal floors at min", 60, 80, false, listDescMinStrLen},
		{"medium terminal gets moderate budget", 120, 60, false, 54},
		{"wide terminal caps at max", 400, 60, false, listDescPipedStrMax},
		{"exactly min available", 80, 59, false, listDescMinStrLen},
		{"just above min", 100, 70, false, 24},
		// Short mode: overhead is 3 (separator + quotes only, no "ds=").
		{"short mode gets extra 3 chars", 120, 60, true, 57},
	}
	for _, tc := range tests {
		got := descStrMaxForWidth(tc.termWidth, tc.nonDescWidth, tc.short)
		if got != tc.want {
			t.Errorf("%s: descStrMaxForWidth(%d, %d, short=%v) = %d, want %d",
				tc.name, tc.termWidth, tc.nonDescWidth, tc.short, got, tc.want)
		}
	}
}

func TestFormatDockTree_IncludesNoRepoDocks(t *testing.T) {
	docks := []engine.DockInfo{
		{
			Name: "tools",
			Workspaces: []engine.WorkspaceInfo{
				{Name: "scratch", Branch: "notes", SurfaceCount: 1, SyncStatus: "ok"},
			},
		},
	}

	out := stripANSI(FormatListView(BuildListView(docks, ListViewOptions{}), false, false))
	if !strings.Contains(out, "rp (no repo)") {
		t.Fatalf("missing synthetic no-repo container:\n%s", out)
	}
	if !strings.Contains(out, "dk tools") {
		t.Fatalf("missing no-repo dock:\n%s", out)
	}
}
