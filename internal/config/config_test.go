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
resume_args = "--continue"
project_file = "CLAUDE.md"

[agents.codex]
command = "codex"

[repos.labs]
path = "~/projects/labs"
worktree_dir = "~/projects/labs-worktrees"

[repos.ct-server]
path = "~/projects/ct-server"

[docks.labs]
repo = "labs"
agent = "claude"
agent_args = ["--add-dir", "~/crew/projects/assistant"]
terminal = "ghostty"
template = "default"

[docks.research]
repo = "labs"
agent = "claude"

[monitor]
interval_seconds = 3
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
	if cfg.Agents["claude"].ResumeArgs != "--continue" {
		t.Errorf("claude resume_args = %q, want --continue", cfg.Agents["claude"].ResumeArgs)
	}
	if cfg.Agents["claude"].ProjectFile != "CLAUDE.md" {
		t.Errorf("claude project_file = %q, want CLAUDE.md", cfg.Agents["claude"].ProjectFile)
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
	if cfg.Docks["labs"].Terminal != "ghostty" {
		t.Errorf("labs dock terminal = %q, want ghostty", cfg.Docks["labs"].Terminal)
	}
	if cfg.Docks["labs"].Template != "default" {
		t.Errorf("labs dock template = %q, want default", cfg.Docks["labs"].Template)
	}

	// Monitor
	if cfg.Monitor.IntervalSeconds != 3 {
		t.Errorf("monitor interval = %d", cfg.Monitor.IntervalSeconds)
	}

}

func TestDefaultConfig_HasInitializedMaps(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Agents == nil {
		t.Error("Agents map should be initialized")
	}
	if cfg.Repos == nil {
		t.Error("Repos map should be initialized")
	}
	if cfg.Docks == nil {
		t.Error("Docks map should be initialized")
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
			"claude": {Command: "claude", ResumeArgs: "--continue", ProjectFile: "CLAUDE.md"},
		},
		Repos: map[string]RepoConfig{
			"labs": {Path: "/projects/labs"},
		},
		Docks: map[string]DockConfig{
			"labs": {Repo: "labs", Agent: "claude", Terminal: "ghostty", Template: "default"},
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
	if loaded.Agents["claude"].ResumeArgs != "--continue" {
		t.Errorf("loaded agent resume_args = %q", loaded.Agents["claude"].ResumeArgs)
	}
	if loaded.Agents["claude"].ProjectFile != "CLAUDE.md" {
		t.Errorf("loaded agent project_file = %q", loaded.Agents["claude"].ProjectFile)
	}
	if loaded.Repos["labs"].Path != "/projects/labs" {
		t.Errorf("loaded repo path = %q", loaded.Repos["labs"].Path)
	}
	if loaded.Docks["labs"].Repo != "labs" {
		t.Errorf("loaded dock repo = %q", loaded.Docks["labs"].Repo)
	}
	if loaded.Docks["labs"].Terminal != "ghostty" {
		t.Errorf("loaded dock terminal = %q", loaded.Docks["labs"].Terminal)
	}
	if loaded.Docks["labs"].Template != "default" {
		t.Errorf("loaded dock template = %q", loaded.Docks["labs"].Template)
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

func TestNormalizePath(t *testing.T) {
	home, _ := os.UserHomeDir()

	// Absolute path under home → ~/...
	got := NormalizePath(filepath.Join(home, "projects", "foo"))
	if got != "~/projects/foo" {
		t.Errorf("NormalizePath(home/projects/foo) = %q, want ~/projects/foo", got)
	}

	// Already ~/...  → stays ~/...
	got = NormalizePath("~/projects/bar")
	if got != "~/projects/bar" {
		t.Errorf("NormalizePath(~/projects/bar) = %q, want ~/projects/bar", got)
	}

	// Absolute path outside home → stays absolute
	got = NormalizePath("/tmp/repo")
	if got != "/tmp/repo" {
		t.Errorf("NormalizePath(/tmp/repo) = %q, want /tmp/repo", got)
	}

	// Relative path → resolved to absolute
	got = NormalizePath("relative/path")
	if !filepath.IsAbs(ExpandPath(got)) {
		t.Errorf("NormalizePath(relative/path) = %q, should resolve to absolute", got)
	}
}

func TestDefaultPaths(t *testing.T) {
	p := DefaultPaths()

	if p.ConfigDir == "" {
		t.Error("ConfigDir is empty")
	}
	if p.DataDir == "" {
		t.Error("DataDir is empty")
	}
	if p.ConfigFile == "" {
		t.Error("ConfigFile is empty")
	}
	if p.ManifestFile == "" {
		t.Error("ManifestFile is empty")
	}
	if p.ArchiveFile == "" {
		t.Error("ArchiveFile is empty")
	}
	if p.PatternsFile == "" {
		t.Error("PatternsFile is empty")
	}
	if p.PIDFile == "" {
		t.Error("PIDFile is empty")
	}
	// ManifestFile should be under DataDir
	if !filepath.HasPrefix(p.ManifestFile, p.DataDir) {
		t.Errorf("ManifestFile %q not under DataDir %q", p.ManifestFile, p.DataDir)
	}
	// ConfigFile should be under ConfigDir
	if !filepath.HasPrefix(p.ConfigFile, p.ConfigDir) {
		t.Errorf("ConfigFile %q not under ConfigDir %q", p.ConfigFile, p.ConfigDir)
	}
}
