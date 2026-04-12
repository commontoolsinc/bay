package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_FullConfig(t *testing.T) {
	data := `
[agents.claude]
command = "claude"
args = ["--dangerously-skip-permissions"]
resume_args = "--continue"
project_file = "CLAUDE.md"

[agents.codex]
command = "codex"

[docks.labs]
agent = "claude"
terminal = "ghostty"

[docks.labs.agent_args]
claude = ["--add-dir", "~/crew/projects/assistant"]

[docks.research]
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
	if len(cfg.Agents["claude"].Args) != 1 || cfg.Agents["claude"].Args[0] != "--dangerously-skip-permissions" {
		t.Errorf("claude args = %v, want [--dangerously-skip-permissions]", cfg.Agents["claude"].Args)
	}

	// Docks
	if len(cfg.Docks) != 2 {
		t.Errorf("expected 2 docks, got %d", len(cfg.Docks))
	}
	if len(cfg.Docks["labs"].AgentArgs["claude"]) != 2 {
		t.Errorf("labs dock agent_args[claude] len = %d", len(cfg.Docks["labs"].AgentArgs["claude"]))
	}
	if cfg.Docks["labs"].Terminal != "ghostty" {
		t.Errorf("labs dock terminal = %q, want ghostty", cfg.Docks["labs"].Terminal)
	}

	// Monitor
	if cfg.Monitor.IntervalSeconds != 3 {
		t.Errorf("monitor interval = %d", cfg.Monitor.IntervalSeconds)
	}

}

func TestDefaultConfig_IsEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DefaultAgent != "" || cfg.DefaultEditor != "" {
		t.Error("DefaultConfig should have no defaults set")
	}
}

func TestEffectiveInterval_DefaultsTo3(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Monitor.EffectiveInterval() != 3 {
		t.Errorf("EffectiveInterval() = %d, want 3", cfg.Monitor.EffectiveInterval())
	}
}

func TestParse_EmptyConfig(t *testing.T) {
	cfg, err := Parse("")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.Agents == nil || cfg.Docks == nil {
		t.Error("maps should be initialized, not nil")
	}
}

func TestParse_InvalidTOML(t *testing.T) {
	_, err := Parse("[invalid toml = =")
	if err == nil {
		t.Error("expected error for invalid TOML")
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

func TestValidate_UnknownAgent(t *testing.T) {
	cfg := &Config{
		Agents: map[string]AgentConfig{},
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
		Docks: map[string]DockConfig{
			"labs": {Agent: "claude", Terminal: "ghostty"},
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
	if loaded.Docks["labs"].Terminal != "ghostty" {
		t.Errorf("loaded dock terminal = %q", loaded.Docks["labs"].Terminal)
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

func TestCanonicalPath_ResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// The canonical form of the symlink path should equal the canonical
	// form of the target. We canonicalize the target too because the
	// temp dir itself may live under a symlinked prefix (macOS uses
	// /var → /private/var, /tmp → /private/tmp).
	wantTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("eval target: %v", err)
	}
	got := CanonicalPath(link)
	if got != wantTarget {
		t.Errorf("CanonicalPath(link) = %q, want %q", got, wantTarget)
	}
}

func TestCanonicalPath_NonexistentReturnsExpanded(t *testing.T) {
	// EvalSymlinks fails on nonexistent paths; CanonicalPath should
	// fall back to the ExpandPath result.
	got := CanonicalPath("~/this/does/not/exist/anywhere")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "this/does/not/exist/anywhere")
	if got != want {
		t.Errorf("CanonicalPath(nonexistent) = %q, want %q", got, want)
	}
}

func TestIsPathUnder(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent")
	child := filepath.Join(parent, "child")
	sibling := filepath.Join(dir, "sibling")
	// parentx is a real sibling directory whose name happens to share
	// parent's prefix. Both must exist on disk so EvalSymlinks succeeds
	// for both — otherwise the prefix-but-not-boundary test passes for
	// the wrong reason (different symlink-resolved roots) on macOS.
	parentX := parent + "x"
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}
	if err := os.MkdirAll(parentX, 0o755); err != nil {
		t.Fatalf("mkdir parentX: %v", err)
	}

	cases := []struct {
		name   string
		child  string
		parent string
		want   bool
	}{
		{"exact match", parent, parent, true},
		{"nested child", child, parent, true},
		{"sibling not under", sibling, parent, false},
		{"prefix string but not directory boundary", parentX, parent, false},
		{"both empty returns false", "", "", false},
		{"empty child returns false", "", parent, false},
		{"empty parent returns false", parent, "", false},
		{"filesystem root parent matches absolute child", "/etc", "/", true},
		{"filesystem root parent matches itself", "/", "/", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsPathUnder(tc.child, tc.parent)
			if got != tc.want {
				t.Errorf("IsPathUnder(%q, %q) = %v, want %v", tc.child, tc.parent, got, tc.want)
			}
		})
	}
}

func TestIsPathUnder_ResolvesSymlinkOnEitherSide(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	realChild := filepath.Join(realDir, "child")
	if err := os.MkdirAll(realChild, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	linkToReal := filepath.Join(dir, "link")
	if err := os.Symlink(realDir, linkToReal); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Symlink on the parent side: child path resolves to a location
	// inside the real dir, parent is the symlink.
	if !IsPathUnder(realChild, linkToReal) {
		t.Error("realChild should be under linkToReal (symlink-resolved)")
	}
	// Symlink on the child side: child uses the symlink prefix, parent
	// is the real path.
	if !IsPathUnder(filepath.Join(linkToReal, "child"), realDir) {
		t.Error("linkToReal/child should be under realDir (symlink-resolved)")
	}
}

func TestNormalizePath(t *testing.T) {
	home, _ := os.UserHomeDir()

	// Absolute path under home -> ~/...
	got := NormalizePath(filepath.Join(home, "projects", "foo"))
	if got != "~/projects/foo" {
		t.Errorf("NormalizePath(home/projects/foo) = %q, want ~/projects/foo", got)
	}

	// Already ~/...  -> stays ~/...
	got = NormalizePath("~/projects/bar")
	if got != "~/projects/bar" {
		t.Errorf("NormalizePath(~/projects/bar) = %q, want ~/projects/bar", got)
	}

	// Absolute path outside home -> stays absolute
	got = NormalizePath("/tmp/repo")
	if got != "/tmp/repo" {
		t.Errorf("NormalizePath(/tmp/repo) = %q, want /tmp/repo", got)
	}

	// Relative path -> resolved to absolute
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
	if !strings.HasPrefix(p.ManifestFile, p.DataDir+string(filepath.Separator)) {
		t.Errorf("ManifestFile %q not under DataDir %q", p.ManifestFile, p.DataDir)
	}
	// ConfigFile should be under ConfigDir
	if !strings.HasPrefix(p.ConfigFile, p.ConfigDir+string(filepath.Separator)) {
		t.Errorf("ConfigFile %q not under ConfigDir %q", p.ConfigFile, p.ConfigDir)
	}
}

func TestResolvedDockAgent(t *testing.T) {
	cfg := &Config{
		Agents: map[string]AgentConfig{"claude": {Command: "claude"}},
		Docks:  map[string]DockConfig{"labs": {Agent: "claude"}},
	}

	// Config override wins
	if got := cfg.ResolvedDockAgent("labs", "codex"); got != "claude" {
		t.Errorf("expected config override 'claude', got %q", got)
	}

	// Falls through to manifest default
	if got := cfg.ResolvedDockAgent("other", "codex"); got != "codex" {
		t.Errorf("expected manifest default 'codex', got %q", got)
	}

	// No config override, no manifest default — falls through to PATH probe.
	// Result is environment-dependent; just verify it doesn't error.
	cfg.ResolvedDockAgent("other", "")
}

func TestResolvedAgentArgs(t *testing.T) {
	cfg := &Config{
		Agents: map[string]AgentConfig{
			"claude": {Command: "claude", Args: []string{"--global-flag"}},
			"codex":  {Command: "codex"},
		},
		Docks: map[string]DockConfig{
			"labs": {AgentArgs: map[string][]string{
				"claude": {"--dock-flag"},
			}},
		},
	}

	// Per-dock override wins
	if got := cfg.ResolvedAgentArgs("labs", "claude", nil); len(got) != 1 || got[0] != "--dock-flag" {
		t.Errorf("expected dock override [--dock-flag], got %v", got)
	}

	// Falls through to manifest default
	manifest := map[string][]string{"claude": {"--manifest-flag"}}
	if got := cfg.ResolvedAgentArgs("other", "claude", manifest); len(got) != 1 || got[0] != "--manifest-flag" {
		t.Errorf("expected manifest default [--manifest-flag], got %v", got)
	}

	// Falls through to agent-level args
	if got := cfg.ResolvedAgentArgs("other", "claude", nil); len(got) != 1 || got[0] != "--global-flag" {
		t.Errorf("expected agent args [--global-flag], got %v", got)
	}

	// No args at any level
	if got := cfg.ResolvedAgentArgs("other", "codex", nil); len(got) != 0 {
		t.Errorf("expected no args, got %v", got)
	}
}
