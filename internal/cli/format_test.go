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
					ID:     "w1",
					Name:   "auth-fix",
					Type:   "worktree",
					Path:   "~/projects/bay-wt/w1",
					Branch: "fix/login",
					Status: "active",
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

	if !strings.Contains(out, "DOCK api") {
		t.Fatalf("dock focus output missing dock header:\n%s", out)
	}
	if !strings.Contains(out, "WS auth-fix") || !strings.Contains(out, "WS cleanup") {
		t.Fatalf("dock focus output missing workspaces:\n%s", out)
	}
	if strings.Contains(out, "PANE ") || strings.Contains(out, "WIN ") {
		t.Fatalf("dock focus default should not recurse into windows/panes:\n%s", out)
	}
}

func TestBuildListView_WorkspaceFocusShowsFullTree(t *testing.T) {
	view := BuildListView(testConfig(), testDocks(), ListViewOptions{
		Focus: ListFocus{Kind: FocusWorkspace, Repo: "bay", Dock: "api", WorkspaceID: "w1"},
	})
	out := stripANSI(FormatListView(view, false))

	for _, want := range []string{
		"REPO bay",
		"DOCK api",
		"WS auth-fix",
		"WIN 1",
		"PANE 2",
		"kind=agent",
		"agent=codex",
		"kind=cmd",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace focus output missing %q:\n%s", want, out)
		}
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
		"Workspace: auth-fix (w1)",
		"Repo: bay",
		"Dock: api",
		"Default Agent: codex",
		"PANE 2",
		"kind=agent",
		"agent=codex",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workspace show missing %q:\n%s", want, out)
		}
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
}
