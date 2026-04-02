package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

func testConfig() *config.Config {
	return &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Command: "claude", ConfigFile: "CLAUDE.local.md"},
			"codex":  {Command: "codex", ConfigFile: "AGENTS.local.md"},
		},
		Repos: map[string]config.RepoConfig{
			"bay":   {Path: "~/projects/bay"},
			"other": {Path: "~/projects/other"},
		},
		Docks: map[string]config.DockConfig{
			"api": {Repo: "bay", Agent: "claude"},
			"web": {Repo: "bay", Agent: "codex"},
			"ops": {Repo: "other"},
		},
	}
}

func testDocks() []engine.DockInfo {
	return []engine.DockInfo{
		{
			Name: "api",
			Repo: "bay",
			Workspaces: []engine.WorkspaceInfo{
				{
					ID:      "w1",
					Name:    "auth-fix",
					Type:    "worktree",
					Path:    "~/projects/bay-wt/w1",
					Branch:  "fix/login",
					Status:  "active",
					Waiting: true,
					Windows: []engine.WindowInfo{
						{
							ID:           1,
							Name:         "editor",
							TmuxWindowID: "@12",
							Panes: []engine.PaneInfo{
								{ID: 1, TmuxPaneID: "%21", Type: "shell", Status: "ok"},
								{ID: 2, TmuxPaneID: "%22", Type: "agent", Agent: "codex", Status: "ok"},
							},
							Status: "ok",
						},
						{
							ID:           2,
							Name:         "tests",
							TmuxWindowID: "@13",
							Panes: []engine.PaneInfo{
								{ID: 1, TmuxPaneID: "%23", Type: "cmd", Command: "npm test --watch=false", Status: "ok"},
							},
							Status: "ok",
						},
					},
					SyncStatus: "ok",
				},
				{
					ID:          "w2",
					Name:        "cleanup",
					Type:        "worktree",
					Path:        "~/projects/bay-wt/w2",
					Branch:      "cleanup",
					Status:      "idle",
					WindowCount: 1,
					SyncStatus:  "stale",
					Windows: []engine.WindowInfo{
						{
							ID:           1,
							Name:         "main",
							TmuxWindowID: "@14",
							Status:       "stale",
							Panes: []engine.PaneInfo{
								{ID: 1, TmuxPaneID: "%24", Type: "shell", Status: "stale"},
							},
						},
					},
				},
			},
		},
		{
			Name: "web",
			Repo: "bay",
			Workspaces: []engine.WorkspaceInfo{
				{
					ID:          "w1",
					Name:        "landing",
					Type:        "worktree",
					Path:        "~/projects/bay-wt-web/w1",
					Branch:      "feature/landing",
					Status:      "active",
					WindowCount: 1,
					SyncStatus:  "ok",
				},
			},
		},
		{
			Name: "ops",
			Repo: "other",
			Workspaces: []engine.WorkspaceInfo{
				{
					ID:          "w1",
					Name:        "deploy",
					Type:        "external",
					Path:        "~/projects/other/deploy",
					Branch:      "main",
					Status:      "done",
					WindowCount: 0,
					SyncStatus:  "missing",
				},
			},
		},
	}
}

func TestBuildListView_DefaultIncludesRepos(t *testing.T) {
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{})
	if len(view.Repos) != 2 {
		t.Fatalf("repos = %d, want 2", len(view.Repos))
	}
	if view.Focus.Kind != FocusAll {
		t.Fatalf("focus = %q, want %q", view.Focus.Kind, FocusAll)
	}
}

func TestBuildListView_DockFocusStopsAtWorkspacesByDefault(t *testing.T) {
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false))

	if !strings.Contains(out, "dock api") {
		t.Fatalf("dock focus output missing dock header:\n%s", out)
	}
	if !strings.Contains(out, "workspace auth-fix") || !strings.Contains(out, "workspace cleanup") {
		t.Fatalf("dock focus output missing workspaces:\n%s", out)
	}
	if strings.Contains(out, "pane ") || strings.Contains(out, "window ") {
		t.Fatalf("dock focus default should not recurse into windows/panes:\n%s", out)
	}
}

func TestBuildListView_WorkspaceFocusShowsFullTree(t *testing.T) {
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "w1"},
	})
	out := stripANSI(FormatListView(view, false))

	for _, want := range []string{
		"repo bay",
		"dock api",
		"workspace auth-fix",
		"window 1",
		"pane 2",
		"kind=agent",
		"agent=codex",
		"kind=cmd",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace focus output missing %q:\n%s", want, out)
		}
	}
}

func TestBuildListView_WorkspaceFocusRestrictsToDock(t *testing.T) {
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "w1"},
	})
	out := stripANSI(FormatListView(view, false))

	if strings.Contains(out, "workspace landing") {
		t.Fatalf("workspace focus should not include matching workspace ids from other docks:\n%s", out)
	}
}

func TestFormatListView_LongShowsTmuxIDs(t *testing.T) {
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "w1"},
	})
	out := stripANSI(FormatListView(view, true))

	for _, want := range []string{"tmux=@12", "tmux=%22"} {
		if !strings.Contains(out, want) {
			t.Fatalf("long output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatListView_SuppressesSyncOKAndShowsStale(t *testing.T) {
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{
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

func TestFormatWorkspaceShow_IncludesDefaultAgentAndPanes(t *testing.T) {
	ws := &engine.WorkspaceInfo{
		ID:           "w1",
		Name:         "auth-fix",
		Type:         "worktree",
		Path:         "~/projects/bay-wt/w1",
		Branch:       "fix/login",
		Status:       "active",
		DefaultAgent: "codex",
		SyncStatus:   "ok",
		Windows: []engine.WindowInfo{
			{
				ID:     1,
				Name:   "editor",
				Status: "ok",
				Panes: []engine.PaneInfo{
					{ID: 1, Type: "shell", Status: "ok"},
					{ID: 2, Type: "agent", Agent: "codex", Status: "ok"},
				},
			},
		},
	}

	out := stripANSI(FormatWorkspaceShow("bay", "api", ws, false))
	for _, want := range []string{
		"workspace auth-fix (w1)",
		"repo bay",
		"dock api",
		"default agent codex",
		"pane 2",
		"kind=agent",
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

func TestFormatListRows_DenormalizesPaneRows(t *testing.T) {
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{
		Focus:     ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "w1"},
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
	for _, want := range []string{`"pane_type":"agent"`, `"pane_agent":"codex"`, `"window_id":2`} {
		if !strings.Contains(s, want) {
			t.Fatalf("rows JSON missing %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, `"workspace_waiting":true`) {
		t.Fatalf("rows JSON missing workspace waiting flag:\n%s", s)
	}
}

func TestBuildListView_FocusRepoFiltersRepos(t *testing.T) {
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{
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
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusDock, Repo: "bay", Dock: "api"},
	})
	out := stripANSI(FormatListView(view, false))

	if !strings.Contains(out, "⏳") {
		t.Fatalf("expected waiting indicator in output:\n%s", out)
	}
}

func TestFormatDockTree_IncludesNoRepoDocks(t *testing.T) {
	cfg := &config.Config{
		Repos: map[string]config.RepoConfig{
			"bay": {Path: "~/projects/bay"},
		},
		Docks: map[string]config.DockConfig{
			"tools": {},
		},
	}
	docks := []engine.DockInfo{
		{
			Name: "tools",
			Workspaces: []engine.WorkspaceInfo{
				{ID: "w1", Name: "scratch", Branch: "notes", Status: "active", WindowCount: 1, SyncStatus: "ok"},
			},
		},
	}

	out := stripANSI(FormatListView(BuildListView(cfg, docks, ListViewOptions{}), false))
	if !strings.Contains(out, "repo (no repo)") {
		t.Fatalf("missing synthetic no-repo container:\n%s", out)
	}
	if !strings.Contains(out, "dock tools") {
		t.Fatalf("missing no-repo dock:\n%s", out)
	}
}
