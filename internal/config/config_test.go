package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_FullConfig(t *testing.T) {
	data := `
[agents.claude]
command = "claude"
config_file = "CLAUDE.local.md"

[agents.codex]
command = "codex"
config_file = "AGENTS.local.md"

[repos.labs]
path = "~/projects/labs"
worktree_dir = "~/projects/labs-worktrees"

[repos.ct-server]
path = "~/projects/ct-server"

[docks.labs]
repo = "labs"
agent = "claude"
agent_args = ["--add-dir", "~/crew/projects/assistant"]
agent_config_template = "~/.config/bay/templates/labs.md"

[docks.research]
repo = "labs"
agent = "claude"

[monitor]
interval_seconds = 3

[keybinding]
add_prompt = "P"
next_waiting = "w"
`
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Agents
	if len(cfg.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(cfg.Agents))
	}
	if cfg.Agents["claude"].Command != "claude" {
		t.Errorf("claude command = %q", cfg.Agents["claude"].Command)
	}
	if cfg.Agents["claude"].ConfigFile != "CLAUDE.local.md" {
		t.Errorf("claude config_file = %q", cfg.Agents["claude"].ConfigFile)
	}

	// Repos
	if len(cfg.Repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(cfg.Repos))
	}
	if cfg.Repos["labs"].WorktreeDir != "~/projects/labs-worktrees" {
		t.Errorf("labs worktree_dir = %q", cfg.Repos["labs"].WorktreeDir)
	}

	// Docks
	if len(cfg.Docks) != 2 {
		t.Errorf("expected 2 docks, got %d", len(cfg.Docks))
	}
	if cfg.Docks["labs"].Repo != "labs" {
		t.Errorf("labs dock repo = %q", cfg.Docks["labs"].Repo)
	}
	if len(cfg.Docks["labs"].AgentArgs) != 2 {
		t.Errorf("labs dock agent_args len = %d", len(cfg.Docks["labs"].AgentArgs))
	}

	// Monitor
	if cfg.Monitor.IntervalSeconds != 3 {
		t.Errorf("monitor interval = %d", cfg.Monitor.IntervalSeconds)
	}

	// Keybinding
	if cfg.Keybind.AddPrompt != "P" {
		t.Errorf("keybinding add_prompt = %q", cfg.Keybind.AddPrompt)
	}
}

func TestParse_EmptyConfig(t *testing.T) {
	cfg, err := Parse("")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Agents == nil || cfg.Repos == nil || cfg.Docks == nil {
		t.Error("maps should be initialized, not nil")
	}
}

func TestParse_InvalidTOML(t *testing.T) {
	_, err := Parse("[invalid toml = =")
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestRepoConfig_EffectiveWorktreeDir(t *testing.T) {
	// Explicit worktree_dir
	r := RepoConfig{Path: "/projects/labs", WorktreeDir: "/custom/worktrees"}
	if got := r.EffectiveWorktreeDir(); got != "/custom/worktrees" {
		t.Errorf("expected /custom/worktrees, got %q", got)
	}

	// Default: path + "-worktrees"
	r2 := RepoConfig{Path: "/projects/labs"}
	if got := r2.EffectiveWorktreeDir(); got != "/projects/labs-worktrees" {
		t.Errorf("expected /projects/labs-worktrees, got %q", got)
	}
}

func TestMonitorConfig_EffectiveInterval(t *testing.T) {
	m := MonitorConfig{IntervalSeconds: 5}
	if got := m.EffectiveInterval(); got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
	m2 := MonitorConfig{}
	if got := m2.EffectiveInterval(); got != 3 {
		t.Errorf("expected default 3, got %d", got)
	}
}

func TestValidate_UnknownRepo(t *testing.T) {
	cfg := &Config{
		Agents: map[string]AgentConfig{"claude": {Command: "claude"}},
		Repos:  map[string]RepoConfig{},
		Docks:  map[string]DockConfig{"labs": {Repo: "nonexistent", Agent: "claude"}},
	}
	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidate_UnknownAgent(t *testing.T) {
	cfg := &Config{
		Agents: map[string]AgentConfig{},
		Repos:  map[string]RepoConfig{},
		Docks:  map[string]DockConfig{"labs": {Agent: "nonexistent"}},
	}
	errs := cfg.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{
		Agents: map[string]AgentConfig{
			"claude": {Command: "claude", ConfigFile: "CLAUDE.local.md"},
		},
		Repos: map[string]RepoConfig{
			"labs": {Path: "/projects/labs"},
		},
		Docks: map[string]DockConfig{
			"labs": {Repo: "labs", Agent: "claude"},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Agents["claude"].Command != "claude" {
		t.Errorf("loaded agent command = %q", loaded.Agents["claude"].Command)
	}
	if loaded.Repos["labs"].Path != "/projects/labs" {
		t.Errorf("loaded repo path = %q", loaded.Repos["labs"].Path)
	}
	if loaded.Docks["labs"].Repo != "labs" {
		t.Errorf("loaded dock repo = %q", loaded.Docks["labs"].Repo)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.toml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		input string
		want  string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"/abs/path", "/abs/path"},
		{"relative", "relative"},
	}
	for _, tt := range tests {
		got := ExpandPath(tt.input)
		if got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
