package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

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
					Status:       "active",
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
					Status:       "idle",
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
					Status:       "active",
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
					Status:       "done",
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

// TestFormatListView_AlignsWorkspaceMetaWithinDock verifies that the meta
// column on workspace lines starts at the same screen column for every
// workspace in a dock, regardless of name length. This is the readability
// fix that turns a key=value run-on into a scannable column.
func TestFormatListView_AlignsWorkspaceMetaWithinDock(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false))

	var authLine, cleanupLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "workspace auth-fix") {
			authLine = line
		}
		if strings.Contains(line, "workspace cleanup") {
			cleanupLine = line
		}
	}
	if authLine == "" || cleanupLine == "" {
		t.Fatalf("missing workspace lines:\n%s", out)
	}

	authMetaStart := strings.Index(authLine, "branch=")
	cleanupMetaStart := strings.Index(cleanupLine, "branch=")
	if authMetaStart < 0 || cleanupMetaStart < 0 {
		t.Fatalf("could not find meta start:\nauth:    %q\ncleanup: %q", authLine, cleanupLine)
	}
	if authMetaStart != cleanupMetaStart {
		t.Errorf("workspace meta columns not aligned:\n  auth-fix branch= at column %d\n  cleanup  branch= at column %d\n  auth:    %q\n  cleanup: %q",
			authMetaStart, cleanupMetaStart, authLine, cleanupLine)
	}
}

// TestFormatListView_AlignsSurfaceMetaWithinWorkspace is the surface-level
// counterpart: in tree mode, all surface lines under a workspace have their
// meta column aligned to each other.
func TestFormatListView_AlignsSurfaceMetaWithinWorkspace(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "auth-fix"},
	})
	out := stripANSI(FormatListView(view, false))

	// auth-fix has surfaces "shell", "agent", "tests" — different lengths.
	var shellLine, agentLine, testsLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "surface shell") {
			shellLine = line
		}
		if strings.Contains(line, "surface agent") {
			agentLine = line
		}
		if strings.Contains(line, "surface tests") {
			testsLine = line
		}
	}
	if shellLine == "" || agentLine == "" || testsLine == "" {
		t.Fatalf("missing surface lines:\n%s", out)
	}

	shellMeta := strings.Index(shellLine, "type=")
	agentMeta := strings.Index(agentLine, "type=")
	testsMeta := strings.Index(testsLine, "type=")
	if shellMeta != agentMeta || agentMeta != testsMeta {
		t.Errorf("surface meta columns not aligned: shell=%d agent=%d tests=%d\n  %q\n  %q\n  %q",
			shellMeta, agentMeta, testsMeta, shellLine, agentLine, testsLine)
	}
}

func TestBuildListView_DockFocusStopsAtWorkspacesByDefault(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false))

	if !strings.Contains(out, "dock api") {
		t.Fatalf("dock focus output missing dock header:\n%s", out)
	}
	if !strings.Contains(out, "workspace auth-fix") || !strings.Contains(out, "workspace cleanup") {
		t.Fatalf("dock focus output missing workspaces:\n%s", out)
	}
	if strings.Contains(out, "surface ") {
		t.Fatalf("dock focus default should not recurse into surfaces:\n%s", out)
	}
}

func TestBuildListView_WorkspaceFocusShowsFullTree(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "auth-fix"},
	})
	out := stripANSI(FormatListView(view, false))

	for _, want := range []string{
		"repo bay",
		"dock api",
		"workspace auth-fix",
		"surface agent",
		"type=agent",
		"agent=codex",
		"type=cmd",
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
	out := stripANSI(FormatListView(view, false))

	if strings.Contains(out, "workspace landing") {
		t.Fatalf("workspace focus should not include workspaces from other docks:\n%s", out)
	}
}

func TestFormatListView_SuppressesSyncOKAndShowsStale(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false))

	if strings.Contains(out, "sync=ok") {
		t.Fatalf("sync=ok should be suppressed:\n%s", out)
	}
	if !strings.Contains(out, "sync=stale") {
		t.Fatalf("stale sync state should be shown:\n%s", out)
	}
}

func TestFormatWorkspaceShow_IncludesDefaultAgentAndSurfaces(t *testing.T) {
	ws := &engine.WorkspaceInfo{
		Name:         "auth-fix",
		Type:         "worktree",
		Path:         "~/projects/bay-wt/auth-fix",
		Branch:       "fix/login",
		Status:       "active",
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
		"type=agent",
		"agent=codex",
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
	out := stripANSI(FormatListView(view, false))

	if !strings.Contains(out, "repo bay") {
		t.Fatalf("repo-focused output missing target repo:\n%s", out)
	}
	if strings.Contains(out, "repo other") {
		t.Fatalf("repo-focused output should not include other repos:\n%s", out)
	}
}

func TestFormatListView_ShowsWaitingIndicator(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false))

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

	out := stripANSI(FormatListView(view, false))

	if !strings.Contains(out, "dock api *") {
		t.Fatalf("current dock should have * marker:\n%s", out)
	}
	if !strings.Contains(out, "workspace auth-fix *") {
		t.Fatalf("current workspace should have * marker:\n%s", out)
	}
	// Non-current workspace should NOT have marker.
	if strings.Contains(out, "workspace cleanup *") {
		t.Fatalf("non-current workspace should not have * marker:\n%s", out)
	}
}

func TestFormatListView_NoHighlightWithoutContext(t *testing.T) {
	view := BuildListView(testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	// No CurrentDock/CurrentWs set.

	out := stripANSI(FormatListView(view, false))

	if strings.Contains(out, " *") {
		t.Fatalf("should have no * markers without current context:\n%s", out)
	}
}

func TestFormatDockTree_IncludesNoRepoDocks(t *testing.T) {
	docks := []engine.DockInfo{
		{
			Name: "tools",
			Workspaces: []engine.WorkspaceInfo{
				{Name: "scratch", Branch: "notes", Status: "active", SurfaceCount: 1, SyncStatus: "ok"},
			},
		},
	}

	out := stripANSI(FormatListView(BuildListView(docks, ListViewOptions{}), false))
	if !strings.Contains(out, "repo (no repo)") {
		t.Fatalf("missing synthetic no-repo container:\n%s", out)
	}
	if !strings.Contains(out, "dock tools") {
		t.Fatalf("missing no-repo dock:\n%s", out)
	}
}
