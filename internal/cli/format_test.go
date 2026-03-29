package cli

import (
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
