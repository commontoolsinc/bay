package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/tmux"
)

func testListEngine(t *testing.T) (*engine.Engine, string) {
	t.Helper()
	dir := t.TempDir()

	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Command: "claude", ConfigFile: "CLAUDE.local.md"},
		},
		Repos: map[string]config.RepoConfig{
			"labs": {Path: filepath.Join(dir, "repos", "labs")},
		},
		Docks: map[string]config.DockConfig{
			"labs": {Repo: "labs", Agent: "claude"},
			"web":  {Repo: "labs", Agent: "claude"},
		},
	}

	repoDir := filepath.Join(dir, "repos", "labs")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	mockTmux := tmux.NewMock()
	mockGit := git.NewMock()
	mockGit.SetGlobalIgnored(true)

	return engine.New(
		cfg,
		filepath.Join(dir, "config.toml"),
		filepath.Join(dir, "manifest.toml"),
		filepath.Join(dir, "archive.toml"),
		mockTmux,
		mockGit,
	), dir
}

func TestResolveListFocus_DirtySkipsContextualFocus(t *testing.T) {
	eng, _ := testListEngine(t)

	ws, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.Chdir(ws.Path); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir("/")
	})

	focus := resolveListFocus(eng, true)
	if focus.Kind != FocusAll {
		t.Fatalf("dirty listings should ignore contextual focus, got %#v", focus)
	}
}

func TestInferListFocus_WorkspaceContext(t *testing.T) {
	eng, _ := testListEngine(t)

	ws, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.Chdir(ws.Path); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir("/")
	})

	focus := inferListFocus(eng)
	if focus.Kind != FocusWorkspace || focus.Dock != "labs" || focus.Repo != "labs" || focus.WorkspaceID != "w1" {
		t.Fatalf("unexpected focus: %#v", focus)
	}
}

func TestInferListFocus_RepoContext(t *testing.T) {
	eng, dir := testListEngine(t)

	repoPath := filepath.Join(dir, "repos", "labs")
	if err := os.Chdir(repoPath); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir("/")
	})

	focus := inferListFocus(eng)
	if focus.Kind != FocusRepo || focus.Repo != "labs" {
		t.Fatalf("unexpected focus: %#v", focus)
	}
}
