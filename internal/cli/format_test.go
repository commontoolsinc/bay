package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
)

func testConfig() *config.Config {
	return &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Command: "claude", ConfigFile: "CLAUDE.local.md"},
		},
		Repos: map[string]config.RepoConfig{
			"myproject": {Path: "~/projects/myproject"},
			"other":     {Path: "~/projects/other"},
		},
		Docks: map[string]config.DockConfig{
			"dev":     {Repo: "myproject", Agent: "claude"},
			"staging": {Repo: "myproject", Agent: "claude"},
			"ops":     {Repo: "other", Agent: "claude"},
		},
	}
}

func testDocks() []engine.DockInfo {
	return []engine.DockInfo{
		{
			Name: "dev", Agent: "claude", Repo: "myproject",
			Workspaces: []engine.WorkspaceInfo{
				{ID: "w1", Name: "auth", Branch: "feature/auth", PR: "42", Status: "active", Agent: "claude"},
				{ID: "w2", Name: "w2", Status: "idle", Agent: "claude"},
			},
		},
		{
			Name: "staging", Agent: "claude", Repo: "myproject",
			Workspaces: []engine.WorkspaceInfo{
				{ID: "w1", Name: "deploy", Branch: "release/v2", PR: "87", Status: "done", Agent: "claude"},
			},
		},
		{
			Name: "ops", Agent: "claude", Repo: "other",
			Workspaces: []engine.WorkspaceInfo{},
		},
	}
}

func TestFormatFullTree(t *testing.T) {
	out := FormatFullTree(testConfig(), testDocks())

	// Should have repo headers
	if !strings.Contains(out, "repo myproject") {
		t.Error("missing repo myproject header")
	}
	if !strings.Contains(out, "repo other") {
		t.Error("missing repo other header")
	}

	// Docks indented under repos
	if !strings.Contains(out, "  dock dev") {
		t.Error("missing dock dev under myproject")
	}
	if !strings.Contains(out, "  dock staging") {
		t.Error("missing dock staging under myproject")
	}
	if !strings.Contains(out, "  dock ops") {
		t.Error("missing dock ops under other")
	}

	// Workspaces indented under docks
	if !strings.Contains(out, "    w1") {
		t.Error("missing workspace w1")
	}
	if !strings.Contains(out, "auth") {
		t.Error("missing workspace name auth")
	}
	if !strings.Contains(out, "#42") {
		t.Error("missing PR #42")
	}

	// Empty dock shows message
	if !strings.Contains(out, "(no workspaces)") {
		t.Error("missing (no workspaces) for empty dock")
	}
}

func TestFormatDockTree(t *testing.T) {
	docks := testDocks()
	out := FormatDockTree(docks[:1]) // just dev

	if !strings.Contains(out, "dock dev (repo=myproject, agent=claude)") {
		t.Errorf("missing dock header, got:\n%s", out)
	}
	if !strings.Contains(out, "  w1") {
		t.Error("missing workspace under dock")
	}
}

func TestFormatDockTree_AllDocks(t *testing.T) {
	out := FormatDockTree(testDocks())

	if strings.Count(out, "dock ") != 3 {
		t.Errorf("expected 3 dock headers, got:\n%s", out)
	}
}

func TestFormatRepoTree(t *testing.T) {
	out := FormatRepoTree(testConfig())

	if !strings.Contains(out, "myproject") {
		t.Error("missing myproject")
	}
	if !strings.Contains(out, "docks: ") {
		t.Error("missing docks line")
	}
	if !strings.Contains(out, "dev") {
		t.Error("missing dev dock reference")
	}
}

func TestFormatSubtreeForRemoval(t *testing.T) {
	cfg := testConfig()
	docks := testDocks()[:2] // dev and staging (both use myproject)

	out := FormatSubtreeForRemoval(cfg, "myproject", docks)

	if !strings.Contains(out, "repo myproject") {
		t.Error("missing repo header")
	}
	if !strings.Contains(out, "dock dev") {
		t.Error("missing dock dev")
	}
	if !strings.Contains(out, "dock staging") {
		t.Error("missing dock staging")
	}
	// Deeper indentation for workspaces
	if !strings.Contains(out, "      w1") {
		t.Error("missing workspace at correct indentation")
	}
}

func TestFormatWorkspaceLine(t *testing.T) {
	ws := engine.WorkspaceInfo{
		ID: "w1", Name: "auth", Branch: "feature/auth",
		PR: "42", Status: "active", Agent: "claude",
	}
	line := formatWorkspaceLine(ws)

	if !strings.Contains(line, "w1") {
		t.Error("missing ID")
	}
	if !strings.Contains(line, "auth") {
		t.Error("missing name")
	}
	if !strings.Contains(line, "#42") {
		t.Error("missing PR")
	}
	if !strings.Contains(line, "claude") {
		t.Error("missing agent")
	}
}

func TestFormatWorkspaceLine_EmptyFields(t *testing.T) {
	ws := engine.WorkspaceInfo{
		ID: "w1", Name: "w1", Status: "idle",
	}
	line := formatWorkspaceLine(ws)

	// Empty branch and agent should show em-dash
	if strings.Count(line, "\u2014") != 2 {
		t.Errorf("expected 2 em-dashes for empty branch and agent, got: %q", line)
	}
}

func TestFormatWorkspaceLine_Waiting(t *testing.T) {
	ws := engine.WorkspaceInfo{
		ID: "w1", Name: "test", Status: "active", Waiting: true,
	}
	line := formatWorkspaceLine(ws)

	if !strings.Contains(line, "\u23f3") {
		t.Error("missing waiting indicator")
	}
}

func TestFormatFullTree_ColumnHeaders(t *testing.T) {
	out := FormatFullTree(testConfig(), testDocks())

	// Column headers should appear in the output
	for _, header := range []string{"ID", "NAME", "BRANCH", "PR", "STATUS", "AGENT"} {
		if !strings.Contains(out, header) {
			t.Errorf("FormatFullTree missing column header %q in output:\n%s", header, out)
		}
	}
}

func TestFormatDockTree_ColumnHeaders(t *testing.T) {
	out := FormatDockTree(testDocks()[:1]) // just dev, which has workspaces

	// Column headers should appear in the output
	for _, header := range []string{"ID", "NAME", "BRANCH", "PR", "STATUS", "AGENT"} {
		if !strings.Contains(out, header) {
			t.Errorf("FormatDockTree missing column header %q in output:\n%s", header, out)
		}
	}
}

func TestFormatWorkspaceLine_Stale(t *testing.T) {
	ws := engine.WorkspaceInfo{
		ID: "w1", Name: "stale-ws", Branch: "feature/stale",
		Status: "active", Agent: "claude", Stale: true,
	}
	line := formatWorkspaceLine(ws)

	if !strings.Contains(line, "[stale]") {
		t.Errorf("expected [stale] indicator in output, got: %q", line)
	}
}

func TestFormatWorkspaceLine_Missing(t *testing.T) {
	ws := engine.WorkspaceInfo{
		ID: "w1", Name: "missing-ws", Branch: "feature/missing",
		Status: "active", Agent: "claude", Missing: true,
	}
	line := formatWorkspaceLine(ws)

	if !strings.Contains(line, "[missing]") {
		t.Errorf("expected [missing] indicator in output, got: %q", line)
	}
}

func TestFormatWorkspaceLine_StaleAndMissing(t *testing.T) {
	ws := engine.WorkspaceInfo{
		ID: "w1", Name: "bad-ws", Status: "active",
		Agent: "claude", Missing: true, Stale: true,
	}
	line := formatWorkspaceLine(ws)

	if !strings.Contains(line, "[missing]") {
		t.Errorf("expected [missing] indicator, got: %q", line)
	}
	if !strings.Contains(line, "[stale]") {
		t.Errorf("expected [stale] indicator, got: %q", line)
	}
}

func TestDockInfo_JSONTags(t *testing.T) {
	docks := testDocks()
	data, err := json.Marshal(docks)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	s := string(data)

	// Verify JSON keys use snake_case from tags, not Go field names
	if !strings.Contains(s, `"name"`) {
		t.Error("JSON missing 'name' key")
	}
	if !strings.Contains(s, `"workspaces"`) {
		t.Error("JSON missing 'workspaces' key")
	}
	if !strings.Contains(s, `"id"`) {
		t.Error("JSON missing 'id' key for workspace")
	}

	// Verify omitempty works — empty PR should not appear
	if strings.Contains(s, `"pr":""`) {
		t.Error("empty PR should be omitted from JSON")
	}
}

func TestDockInfo_JSONRoundTrip(t *testing.T) {
	docks := testDocks()
	data, err := json.MarshalIndent(docks, "", "  ")
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed []engine.DockInfo
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(parsed) != len(docks) {
		t.Fatalf("expected %d docks, got %d", len(docks), len(parsed))
	}
	// Find dev dock
	for _, d := range parsed {
		if d.Name == "dev" {
			if len(d.Workspaces) != 2 {
				t.Errorf("dev dock: expected 2 workspaces, got %d", len(d.Workspaces))
			}
			for _, ws := range d.Workspaces {
				if ws.ID == "w1" && ws.Name != "auth" {
					t.Errorf("w1 name = %q, want auth", ws.Name)
				}
			}
			return
		}
	}
	t.Error("dev dock not found in parsed JSON")
}
