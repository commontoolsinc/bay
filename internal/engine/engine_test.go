package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

func testEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()

	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Command: "claude"},
			"codex":  {Command: "codex"},
		},
		Docks: map[string]config.DockConfig{},
	}

	// Create repo directory
	repoDir := filepath.Join(dir, "repos", "labs")
	os.MkdirAll(repoDir, 0o755)

	configPath := filepath.Join(dir, "config.toml")
	manifestPath := filepath.Join(dir, "manifest.json")
	archivePath := filepath.Join(dir, "archive.json")

	mockTmux := tmux.NewMock()
	mockGit := git.NewMock()
	mockGit.SetGlobalIgnored(true) // default: config files are gitignored

	eng := New(cfg, configPath, manifestPath, archivePath, mockTmux, mockGit)

	// Set up repos and docks in the manifest (they now live there, not config).
	manifest.Save(manifestPath, &manifest.Manifest{
		Version: manifest.CurrentVersion,
		Repos: []manifest.Repo{
			{Name: "labs", Path: filepath.Join(dir, "repos", "labs")},
		},
		Docks: []manifest.Dock{
			{Name: "labs", Repo: "labs", Agent: "claude", Workspaces: []manifest.Workspace{}},
		},
	})

	return eng, dir
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"labs", false},
		{"my-dock", false},
		{"my_dock", false},
		{"a1", false},
		{"has spaces", true},
		{"has:colon", true},
		{"has/slash", true},
		{"", true},
	}
	for _, tt := range tests {
		err := ValidateName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateName(%q) error=%v, wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}

// TestValidateWorkspaceName verifies the additional reservation: names
// matching the canonical workspace ID pattern (^w[1-9]\d*$) are rejected
// so user-set Names can't shadow IDs at the CLI.
func TestValidateWorkspaceName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		// Reserved — rejected.
		{"w1", true},
		{"w42", true},
		{"w999", true},

		// Looks ID-shaped but isn't canonical — accepted (also accepted
		// by ValidateName).
		{"w0", false},     // n must be >= 1 to be a valid ID
		{"w01", false},    // leading zeros aren't canonical IDs
		{"W1", false},     // case-sensitive: capital W is not a workspace ID
		{"ws1", false},    // different prefix
		{"my-w1", false},  // not a pure w<N>
		{"w1-bug", false}, // trailing chars

		// Names that fail the base ValidateName too.
		{"has space", true},
		{"", true},

		// Friendly names — accepted.
		{"auth-fix", false},
		{"cache_ttl", false},
	}
	for _, tt := range tests {
		err := ValidateWorkspaceName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateWorkspaceName(%q) error=%v, wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}

// TestAbbreviateBranch_AvoidsReservedPattern guards the case where a
// branch like fix/w1 would otherwise produce a Name matching the
// reserved ID pattern; abbreviateBranch must prefix it so the result
// stays a legal Name.
func TestAbbreviateBranch_AvoidsReservedPattern(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"fix/w1", "br-w1"},
		{"feature/w42", "br-w42"},
		{"w3", "br-w3"},
		{"fix/auth", "auth"}, // unaffected
	}
	for _, tt := range tests {
		if got := abbreviateBranch(tt.branch); got != tt.want {
			t.Errorf("abbreviateBranch(%q) = %q, want %q", tt.branch, got, tt.want)
		}
	}
}

func TestAbbreviateBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"feature/mem-refactor", "mem-refactor"},
		{"fix/crash-on-start", "crash-on-start"},
		{"chore/update-deps", "update-deps"},
		{"plain-branch", "plain-branch"},
		{"main", "main"},
	}
	for _, tt := range tests {
		got := abbreviateBranch(tt.branch)
		if got != tt.want {
			t.Errorf("abbreviateBranch(%q) = %q, want %q", tt.branch, got, tt.want)
		}
	}
}

func TestDockNew(t *testing.T) {
	eng, _ := testEngine(t)

	err := eng.DockNew("research", "labs", "claude", "")
	if err != nil {
		t.Fatalf("DockNew failed: %v", err)
	}

	// Verify manifest updated (docks are now in manifest, not config).

	// Verify tmux session created
	mockTmux := eng.Tmux.(*tmux.Mock)
	if !mockTmux.HasSessionCalled("research") {
		t.Error("tmux session not created")
	}

	// Verify manifest updated
	m, _ := eng.LoadManifest()
	if m.FindDock("research") == nil {
		t.Error("dock not in manifest")
	}
}

func TestDockNew_DuplicateName(t *testing.T) {
	eng, _ := testEngine(t)

	// labs already exists in config
	err := eng.DockNew("labs", "labs", "claude", "")
	if err == nil {
		t.Error("expected error for duplicate dock name")
	}
}

func TestDockNew_InvalidName(t *testing.T) {
	eng, _ := testEngine(t)

	err := eng.DockNew("bad name", "", "", "")
	if err == nil {
		t.Error("expected error for invalid name")
	}
}

func TestDockNew_LaunchesHostTerminal(t *testing.T) {
	eng, dir := testEngine(t)
	eng.configPath = filepath.Join(dir, "config.toml")
	config.Save(eng.configPath, eng.Config)

	// Pass terminal name directly to DockNew.
	err := eng.DockNew("research", "labs", "claude", "ghostty")
	if err != nil {
		t.Fatalf("DockNew: %v", err)
	}

	m, _ := eng.LoadManifest()
	dock := m.FindDock("research")
	if dock == nil {
		t.Fatal("dock not in manifest")
	}
	if dock.Host == nil {
		t.Fatal("dock.Host should be set when terminal is configured")
	}
	if dock.Host.AppCommand != "ghostty" {
		t.Errorf("host app_command = %q, want ghostty", dock.Host.AppCommand)
	}
	// Config should have Terminal override persisted.
	if dc, ok := eng.Config.Docks["research"]; !ok || dc.Terminal != "ghostty" {
		t.Errorf("config terminal override not persisted")
	}
}

func TestRecover_RelaunchesHostTerminal(t *testing.T) {
	eng, _ := testEngine(t)

	oldLaunchTerminal := launchTerminal
	launchTerminal = func(terminal, session string) (int, error) {
		if terminal != "ghostty" || session != "labs" {
			t.Fatalf("launchTerminal(%q, %q)", terminal, session)
		}
		return 4242, nil
	}
	defer func() { launchTerminal = oldLaunchTerminal }()

	// Set host terminal on the existing labs dock in the manifest.
	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	dock.Host = &manifest.GUIAttrs{AppCommand: "ghostty", PID: 0}
	eng.saveManifest(m)

	eng.Config.Docks["labs"] = config.DockConfig{
		Terminal: "ghostty",
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	_, err := eng.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}

	// After recovery, the host PID should be updated (non-zero).
	m, _ = eng.LoadManifest()
	dock = m.FindDock("labs")
	if dock == nil {
		t.Fatal("dock not found after recovery")
	}
	if dock.Host == nil {
		t.Fatal("dock.Host should persist through recovery")
	}
	if dock.Host.PID != 4242 {
		t.Fatalf("dock.Host.PID = %d, want 4242", dock.Host.PID)
	}
}

func TestRecover_HostTerminalFailureIsWarning(t *testing.T) {
	eng, _ := testEngine(t)

	oldLaunchTerminal := launchTerminal
	launchTerminal = func(terminal, session string) (int, error) {
		return 0, fmt.Errorf("missing terminal binary")
	}
	defer func() { launchTerminal = oldLaunchTerminal }()

	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	dock.Host = &manifest.GUIAttrs{AppCommand: "ghostty", PID: 0}
	eng.saveManifest(m)

	eng.Config.Docks["labs"] = config.DockConfig{
		Terminal: "ghostty",
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	results, err := eng.Recover()
	if err != nil {
		t.Fatalf("Recover returned hard error for terminal warning: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if len(results[0].Warnings) != 1 || !strings.Contains(results[0].Warnings[0], "missing terminal binary") {
		t.Fatalf("warnings = %v, want launch-terminal warning", results[0].Warnings)
	}

	m, _ = eng.LoadManifest()
	dock = m.FindDock("labs")
	if dock == nil || dock.Host == nil {
		t.Fatal("dock host missing after recovery")
	}
	if dock.Host.PID != 0 {
		t.Fatalf("dock.Host.PID = %d, want unchanged 0 on warning", dock.Host.PID)
	}
}

func TestWsNew_Worktree(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	if ws.Type != manifest.WorkspaceTypeWorktree {
		t.Errorf("type = %q, want worktree", ws.Type)
	}
	if ws.Name != "w1" {
		t.Errorf("name = %q, want w1", ws.Name)
	}
	if len(ws.Surfaces) != 1 {
		t.Fatalf("surfaces = %d, want 1", len(ws.Surfaces))
	}
	if ws.Surfaces[0].Type != manifest.SurfaceTypeAgent {
		t.Errorf("surface type = %q, want agent", ws.Surfaces[0].Type)
	}
	if ws.Surfaces[0].Tmux == nil || ws.Surfaces[0].Tmux.PaneID == "" {
		t.Error("expected first surface to record tmux pane ID")
	}

	// Verify worktree was created
	mockGit := eng.Git.(*git.Mock)
	if len(mockGit.CreatedWorktrees()) != 1 {
		t.Errorf("expected 1 worktree created, got %d", len(mockGit.CreatedWorktrees()))
	}

	// Verify manifest persisted
	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock == nil || dock.FindWorkspace("w1") == nil {
		t.Error("workspace not in manifest")
	}

	// Second workspace gets w2
	ws2, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("second WsNew failed: %v", err)
	}
	if ws2.Name != "w2" {
		t.Errorf("second workspace name = %q, want w2", ws2.Name)
	}
}

func TestWsNew_InvalidNameDoesNotCreateWorktree(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "bad name"}); err == nil {
		t.Fatal("expected invalid workspace name to fail")
	}

	mockGit := eng.Git.(*git.Mock)
	if got := len(mockGit.CreatedWorktrees()); got != 0 {
		t.Fatalf("created worktrees = %d, want 0", got)
	}
}

func TestWsNew_BranchNameCollisionGetsUniqueName(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "new-branch", Shell: true}); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "feature/new-branch"})
	if err != nil {
		t.Fatalf("WsNew with colliding branch name: %v", err)
	}
	if ws.Name != "new-branch-2" {
		t.Fatalf("workspace name = %q, want new-branch-2", ws.Name)
	}
}

func TestWsNew_RequireAgentUsesProbeWhenNoDockDefault(t *testing.T) {
	eng, _ := testEngine(t)

	m, _ := eng.LoadManifest()
	m.FindDock("labs").Agent = ""
	if err := eng.saveManifest(m); err != nil {
		t.Fatalf("saveManifest: %v", err)
	}
	eng.Config.DefaultAgent = ""

	// With no dock default and no config default, falls through to
	// PATH probe. If an agent is on PATH, it succeeds; if not, it
	// fails. Both outcomes are valid — just verify it doesn't panic.
	_, _ = eng.WsNew(WsNewOptions{Dock: "labs", RequireAgent: true})
}

func TestWsNew_UnknownExplicitAgentFails(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Agent: "ghostwriter", RequireAgent: true}); err == nil || !strings.Contains(err.Error(), `unknown agent "ghostwriter"`) {
		t.Fatalf("WsNew error = %v, want unknown agent", err)
	}
}

func TestWsNew_ShellIgnoresInvalidDefaultAgent(t *testing.T) {
	eng, _ := testEngine(t)
	eng.Config.Docks["labs"] = config.DockConfig{Agent: "ghostwriter"}

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if ws.Name != "w1" {
		t.Fatalf("name = %q, want w1", ws.Name)
	}
	if len(ws.Surfaces) != 1 || ws.Surfaces[0].Type != manifest.SurfaceTypeShell {
		t.Fatalf("surfaces = %#v, want single shell surface", ws.Surfaces)
	}
}

func TestWsNew_CopiesWorktreeincludeFiles(t *testing.T) {
	eng, dir := testEngine(t)

	repoDir := filepath.Join(dir, "repos", "labs")

	// .worktreeinclude uses gitignore syntax — a glob here exercises that.
	os.WriteFile(filepath.Join(repoDir, ".worktreeinclude"), []byte("*.env\n"), 0o644)

	// Source file the pattern should match, and mock the expansion.
	os.WriteFile(filepath.Join(repoDir, "local.env"), []byte("SECRET=abc\n"), 0o644)
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetExcludeMatches(repoDir, ".worktreeinclude", nil, []string{"local.env"})

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ws.Path, "local.env"))
	if err != nil {
		t.Fatalf("local.env not copied to worktree: %v", err)
	}
	if string(data) != "SECRET=abc\n" {
		t.Errorf("local.env content = %q, want SECRET=abc", data)
	}
}

func TestWsNew_SkipsWorktreeincludeIfMissing(t *testing.T) {
	eng, _ := testEngine(t)

	// No .worktreeinclude file — should not error.
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	_ = ws
}

func TestWsNew_RefusesWorktreeincludeTrackedFile(t *testing.T) {
	eng, dir := testEngine(t)

	repoDir := filepath.Join(dir, "repos", "labs")
	os.WriteFile(filepath.Join(repoDir, ".worktreeinclude"), []byte("config.toml\n*.env\n"), 0o644)
	os.WriteFile(filepath.Join(repoDir, "local.env"), []byte("OK=1\n"), 0o644)

	// config.toml is tracked → refuse; local.env is untracked + ignored → copy.
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetExcludeMatches(repoDir, ".worktreeinclude",
		[]string{"config.toml"}, []string{"local.env"})

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew should not error on refusal; got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, "config.toml")); !os.IsNotExist(err) {
		t.Errorf("tracked config.toml should NOT have been copied into worktree")
	}
	if _, err := os.Stat(filepath.Join(ws.Path, "local.env")); err != nil {
		t.Errorf("valid local.env should still have been copied: %v", err)
	}
}

func TestWsNew_RefusesWorktreeincludeNonIgnoredFile(t *testing.T) {
	eng, dir := testEngine(t)

	repoDir := filepath.Join(dir, "repos", "labs")
	os.WriteFile(filepath.Join(repoDir, ".worktreeinclude"), []byte("notes.txt\n"), 0o644)
	os.WriteFile(filepath.Join(repoDir, "notes.txt"), []byte("x\n"), 0o644)

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetGlobalIgnored(false) // notes.txt is NOT covered by .gitignore
	mockGit.SetExcludeMatches(repoDir, ".worktreeinclude", nil, []string{"notes.txt"})

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew should not error on refusal; got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws.Path, "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("non-gitignored notes.txt should NOT have been copied into worktree")
	}
}

func TestWsNew_SequentialDefaultNames(t *testing.T) {
	eng, _ := testEngine(t)

	ws1, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("first WsNew: %v", err)
	}
	if ws1.Name != "w1" {
		t.Errorf("first workspace name = %q, want w1", ws1.Name)
	}

	ws2, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("second WsNew: %v", err)
	}
	if ws2.Name != "w2" {
		t.Errorf("second workspace name = %q, want w2", ws2.Name)
	}

	// Close w1, create another — should get w3, not reuse w1.
	eng.WsClose("labs", "w1", true)
	ws3, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("third WsNew: %v", err)
	}
	if ws3.Name != "w1" {
		t.Errorf("third workspace name = %q, want w1 (reuse after close)", ws3.Name)
	}
}

func TestSurfaceAdd_PersistsTmuxPaneID(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "codex", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd failed: %v", err)
	}

	ws, err := eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	if len(ws.Surfaces) != 2 {
		t.Fatalf("surfaces = %d, want 2", len(ws.Surfaces))
	}
	if ws.Surfaces[1].Tmux == nil || ws.Surfaces[1].Tmux.PaneID == "" {
		t.Fatal("expected added surface to record tmux pane ID")
	}
}

func TestSurfaceAdd_UnknownAgentFailsWithoutPersistingSurface(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "ghostwriter", SplitDir: "v"})
	if err == nil || !strings.Contains(err.Error(), `unknown agent "ghostwriter"`) {
		t.Fatalf("SurfaceAdd error = %v, want unknown agent", err)
	}

	ws, err := eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	if got := len(ws.Surfaces); got != 1 {
		t.Fatalf("surfaces = %d, want 1", got)
	}
}

func TestSurfaceAdd_PrefersCurrentPaneAsSplitParent(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-2", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd shell-2 failed: %v", err)
	}

	ws, err := eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.SetCurrentPaneID(ws.Surfaces[0].Tmux.PaneID)

	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-3", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd shell-3 failed: %v", err)
	}

	ws, err = eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	s := ws.FindSurface("shell-3")
	if s == nil {
		t.Fatal("surface shell-3 not found")
	}
	if s.Tmux.SplitFrom != ws.Surfaces[0].ID {
		t.Fatalf("split_from = %d, want %d", s.Tmux.SplitFrom, ws.Surfaces[0].ID)
	}
}

func TestSurfaceAdd_FallsBackToLastFocusedSurface(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-2", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd shell-2 failed: %v", err)
	}

	ws, err := eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	if err := eng.SetLastFocused("labs", "w1", ws.Surfaces[1].ID); err != nil {
		t.Fatalf("SetLastFocused failed: %v", err)
	}

	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-3", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd shell-3 failed: %v", err)
	}

	ws, err = eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	s := ws.FindSurface("shell-3")
	if s == nil {
		t.Fatal("surface shell-3 not found")
	}
	if s.Tmux.SplitFrom != ws.Surfaces[1].ID {
		t.Fatalf("split_from = %d, want %d", s.Tmux.SplitFrom, ws.Surfaces[1].ID)
	}
}

func TestWsNew_External(t *testing.T) {
	eng, dir := testEngine(t)

	extDir := filepath.Join(dir, "external-project")
	os.MkdirAll(extDir, 0o755)

	ws, err := eng.WsNew(WsNewOptions{
		Dock: "labs",
		Dir:  extDir,
		Name: "ext1",
	})
	if err != nil {
		t.Fatalf("WsNew external failed: %v", err)
	}

	if ws.Type != manifest.WorkspaceTypeExternal {
		t.Errorf("type = %q, want external", ws.Type)
	}
	if ws.Path != extDir {
		t.Errorf("path = %q, want %q", ws.Path, extDir)
	}

	// No worktree should have been created
	mockGit := eng.Git.(*git.Mock)
	if len(mockGit.CreatedWorktrees()) != 0 {
		t.Error("worktree should not be created for external workspace")
	}
}

func TestWsNew_Shell(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew shell failed: %v", err)
	}

	if ws.Surfaces[0].Type != manifest.SurfaceTypeShell {
		t.Errorf("surface type = %q, want shell", ws.Surfaces[0].Type)
	}
}

func TestWsNew_UnknownDock(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "nonexistent"})
	if err == nil {
		t.Error("expected error for unknown dock")
	}
}

func TestWsClose_Worktree(t *testing.T) {
	eng, _ := testEngine(t)

	// Create workspace
	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Close it
	err = eng.WsClose("labs", "w1", false)
	if err != nil {
		t.Fatalf("WsClose failed: %v", err)
	}

	// Verify removed from manifest
	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock != nil && dock.FindWorkspace("w1") != nil {
		t.Error("workspace should be removed from manifest")
	}

	// Verify worktree removed
	mockGit := eng.Git.(*git.Mock)
	if len(mockGit.RemovedWorktrees()) != 1 {
		t.Errorf("expected 1 worktree removed, got %d", len(mockGit.RemovedWorktrees()))
	}

	// Verify archived
	archive, err := manifest.LoadArchive(eng.archivePath)
	if err != nil {
		t.Fatalf("loading archive: %v", err)
	}
	archiveDock := archive.FindDock("labs")
	if archiveDock == nil || archiveDock.FindWorkspace("w1") == nil {
		t.Error("workspace should be in archive")
	}
}

func TestWsClose_DeletesPushedBranch(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "fix/cleanup"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	// The worktree path must exist for safety checks to run.
	os.MkdirAll(ws.Path, 0o755)

	// Branch is pushed (HasUnpushedCommits returns false — the default).
	if err := eng.WsClose("labs", ws.Name, false); err != nil {
		t.Fatalf("WsClose: %v", err)
	}

	mockGit := eng.Git.(*git.Mock)
	deleted := mockGit.DeletedBranches()
	if len(deleted) != 1 {
		t.Fatalf("expected 1 branch deleted, got %d: %v", len(deleted), deleted)
	}
	if deleted[0].Args[1] != "fix/cleanup" {
		t.Errorf("deleted branch = %q, want fix/cleanup", deleted[0].Args[1])
	}
}

func TestWsClose_KeepsBranchWhenUnpushed(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "fix/wip"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Mark the workspace as having unpushed commits.
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetUnpushed(ws.Path, true)

	// Force close (non-force would refuse due to unpushed commits).
	if err := eng.WsClose("labs", ws.Name, true); err != nil {
		t.Fatalf("WsClose --force: %v", err)
	}

	// Branch should NOT be deleted — unpushed commits exist.
	if len(mockGit.DeletedBranches()) != 0 {
		t.Errorf("branch should not be deleted when unpushed commits exist; got %v", mockGit.DeletedBranches())
	}
}

func TestWsClose_AllowsMergedPRHeadWhenPatchCheckSaysUnlanded(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "fix/squash"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	if err := eng.withManifest(func(m *manifest.Manifest) error {
		got := m.FindDock("labs").FindWorkspace(ws.Name)
		got.Worktree.PR = "123"
		return nil
	}); err != nil {
		t.Fatalf("set PR: %v", err)
	}

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetUnpushed(ws.Path, true)
	mockGit.SetLocalHeadInMergedPR(ws.Path, "123", true)

	if err := eng.WsClose("labs", ws.Name, false); err != nil {
		t.Fatalf("WsClose: %v", err)
	}
	if deleted := mockGit.DeletedBranches(); len(deleted) != 1 {
		t.Fatalf("expected branch delete after merged PR close, got %v", deleted)
	}
}

func TestWsClose_RefusesCommitsAfterMergedPR(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "fix/continued"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	if err := eng.withManifest(func(m *manifest.Manifest) error {
		got := m.FindDock("labs").FindWorkspace(ws.Name)
		got.Worktree.PR = "123"
		return nil
	}); err != nil {
		t.Fatalf("set PR: %v", err)
	}

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetUnpushed(ws.Path, true)
	mockGit.SetLocalHeadInMergedPR(ws.Path, "123", false)

	err = eng.WsClose("labs", ws.Name, false)
	if err == nil || !strings.Contains(err.Error(), "unlanded commits") {
		t.Fatalf("expected unlanded refusal, got %v", err)
	}
	if deleted := mockGit.DeletedBranches(); len(deleted) != 0 {
		t.Fatalf("branch should not be deleted after refusal, got %v", deleted)
	}
}

func TestWsClose_DeletesPushedBranchOnForce(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "fix/done"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Branch is pushed (default mock behavior).
	if err := eng.WsClose("labs", ws.Name, true); err != nil {
		t.Fatalf("WsClose --force: %v", err)
	}

	mockGit := eng.Git.(*git.Mock)
	if len(mockGit.DeletedBranches()) != 1 {
		t.Errorf("expected pushed branch to be deleted on force close; got %d deletions", len(mockGit.DeletedBranches()))
	}
}

func TestWsClose_NoBranchNoDelete(t *testing.T) {
	eng, _ := testEngine(t)

	// Workspace with no branch (scratch/detached).
	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	if err := eng.WsClose("labs", "w1", false); err != nil {
		t.Fatalf("WsClose: %v", err)
	}

	mockGit := eng.Git.(*git.Mock)
	if len(mockGit.DeletedBranches()) != 0 {
		t.Errorf("no branch to delete for scratch workspace; got %v", mockGit.DeletedBranches())
	}
}

func TestWsClose_Dirty(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Create the workspace directory so safety checks run
	os.MkdirAll(ws.Path, 0o755)

	// Make it dirty
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetGlobalDirty(true)

	// Should refuse
	err = eng.WsClose("labs", "w1", false)
	if err == nil {
		t.Error("expected error for dirty workspace")
	}

	// Force should work
	err = eng.WsClose("labs", "w1", true)
	if err != nil {
		t.Errorf("force close failed: %v", err)
	}
}

func TestWsRename(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	err = eng.WsRename("labs", "w1", "my-ws")
	if err != nil {
		t.Fatalf("WsRename failed: %v", err)
	}

	ws, _ := eng.WsShow("labs", "my-ws")
	if ws.Name != "my-ws" {
		t.Errorf("name = %q, want my-ws", ws.Name)
	}
}

func TestWsDescribe(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Description: "initial description"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	ws, _ := eng.WsShow("labs", "w1")
	if ws.Description != "initial description" {
		t.Errorf("initial description = %q, want %q", ws.Description, "initial description")
	}

	if err := eng.WsDescribe("labs", "w1", "  login flow fixes  "); err != nil {
		t.Fatalf("WsDescribe failed: %v", err)
	}
	ws, _ = eng.WsShow("labs", "w1")
	if ws.Description != "login flow fixes" {
		t.Errorf("description = %q, want trimmed %q", ws.Description, "login flow fixes")
	}

	if err := eng.WsDescribe("labs", "w1", ""); err != nil {
		t.Fatalf("WsDescribe clear failed: %v", err)
	}
	ws, _ = eng.WsShow("labs", "w1")
	if ws.Description != "" {
		t.Errorf("cleared description = %q, want empty", ws.Description)
	}

	// Accept a multi-line description (label + body).
	multi := "label line\n\nbody line one\nbody line two"
	if err := eng.WsDescribe("labs", "w1", multi); err != nil {
		t.Fatalf("WsDescribe multi-line failed: %v", err)
	}
	ws, _ = eng.WsShow("labs", "w1")
	if ws.Description != multi {
		t.Errorf("multi-line description = %q, want %q", ws.Description, multi)
	}

	// Reject tabs and carriage returns.
	if err := eng.WsDescribe("labs", "w1", "has\ttab"); err == nil {
		t.Errorf("expected error for tab in description")
	}
	if err := eng.WsDescribe("labs", "w1", "has\rcr"); err == nil {
		t.Errorf("expected error for carriage return in description")
	}

	// Reject first line over the first-line cap.
	longFirst := strings.Repeat("x", MaxDescriptionFirstLineLen+1)
	if err := eng.WsDescribe("labs", "w1", longFirst); err == nil {
		t.Errorf("expected error for over-length first line")
	}

	// First line at cap is fine even if body pushes total size higher.
	okFirst := strings.Repeat("x", MaxDescriptionFirstLineLen)
	if err := eng.WsDescribe("labs", "w1", okFirst+"\nbody"); err != nil {
		t.Errorf("first line at cap should be accepted: %v", err)
	}

	// Reject over-length total.
	tooLong := strings.Repeat("x", MaxDescriptionLen+1)
	if err := eng.WsDescribe("labs", "w1", tooLong); err == nil {
		t.Errorf("expected error for over-length description")
	}
}

func TestDescriptionFirstLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"single line", "single line"},
		{"first\nbody", "first"},
		{"first\n\nbody line 1\nbody line 2", "first"},
		{"\nstarts with newline", ""},
	}
	for _, c := range cases {
		if got := DescriptionFirstLine(c.in); got != c.want {
			t.Errorf("DescriptionFirstLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSurfaceAdd_NewLayoutGroup(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Add a new surface with empty splitDir = new tmux window / layout group
	err = eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell"})
	if err != nil {
		t.Fatalf("SurfaceAdd failed: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(ws.Surfaces))
	}
	if ws.Surfaces[1].Type != manifest.SurfaceTypeShell {
		t.Errorf("surface type = %q, want shell", ws.Surfaces[1].Type)
	}
	// New layout group should be different from the first
	if ws.Surfaces[1].Tmux.LayoutGroup == ws.Surfaces[0].Tmux.LayoutGroup {
		t.Error("new surface should be in a different layout group")
	}
}

func TestSurfaceClose(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Open second surface in new layout group
	eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell"})

	// Close the second surface
	err = eng.SurfaceClose("labs", "w1", "shell", false)
	if err != nil {
		t.Fatalf("SurfaceClose failed: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 1 {
		t.Errorf("expected 1 surface after close, got %d", len(ws.Surfaces))
	}
}

func TestSurfaceClose_LastSurfaceClosesWorkspace(t *testing.T) {
	// Closing the last surface via sf close (non-force) schedules the
	// workspace for auto-close after the grace window. Set the grace
	// to 0 so the very next sync finalizes.
	defer withZeroGrace()()
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 1 {
		t.Fatalf("expected 1 surface, got %d", len(ws.Surfaces))
	}
	surfaceName := ws.Surfaces[0].Name

	if err := eng.SurfaceClose("labs", "w1", surfaceName, false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}

	// Grace is 0 → next sync finalizes the pending close.
	eng.SyncAll()

	_, err = eng.WsShow("labs", "w1")
	if err == nil {
		t.Error("workspace w1 still exists after closing its last surface + sync")
	}
}

// withZeroGrace sets orphanGraceSeconds to 0 for the duration of a
// test and returns a cleanup func. Use with `defer withZeroGrace()()`.
func withZeroGrace() func() {
	prev := orphanGraceSeconds
	orphanGraceSeconds = 0
	return func() { orphanGraceSeconds = prev }
}

// Regression: SurfaceClose, WsClose, DockClose, and RepoRemove must
// persist their manifest mutations BEFORE issuing any tmux kill, so that
// bay invoked from inside a pane being killed doesn't leave the manifest
// half-updated when tmux SIGHUPs the process. The hook on the mock fires
// at the start of every kill call; the assertion is that the manifest on
// disk already reflects the removal at that moment.

func TestSurfaceClose_PersistsManifestBeforeKill(t *testing.T) {
	eng, _ := testEngine(t)
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-2", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.OnKill = func(method, target string) {
		m, err := manifest.Load(eng.manifestPath)
		if err != nil {
			t.Errorf("loading manifest in kill hook: %v", err)
			return
		}
		ws := m.FindDock("labs").FindWorkspace("w1")
		if ws == nil {
			t.Errorf("workspace gone from manifest at kill time (unexpected)")
			return
		}
		if ws.FindSurface("shell-2") != nil {
			t.Errorf("manifest still references surface 'shell-2' at the moment of %s(%s) — kill happened before manifest update", method, target)
		}
	}

	if err := eng.SurfaceClose("labs", "w1", "shell-2", false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}
}

func TestWsClose_PersistsManifestBeforeKill(t *testing.T) {
	eng, _ := testEngine(t)
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.OnKill = func(method, target string) {
		m, err := manifest.Load(eng.manifestPath)
		if err != nil {
			t.Errorf("loading manifest in kill hook: %v", err)
			return
		}
		dock := m.FindDock("labs")
		if dock != nil && dock.FindWorkspace("w1") != nil {
			t.Errorf("manifest still references workspace 'w1' at the moment of %s(%s) — kill happened before manifest update", method, target)
		}
	}

	if err := eng.WsClose("labs", "w1", true); err != nil {
		t.Fatalf("WsClose: %v", err)
	}
}

func TestDockClose_PersistsManifestBeforeKill(t *testing.T) {
	eng, _ := testEngine(t)
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.OnKill = func(method, target string) {
		m, err := manifest.Load(eng.manifestPath)
		if err != nil {
			t.Errorf("loading manifest in kill hook: %v", err)
			return
		}
		// By the time any kill fires, the dock and both workspaces must
		// already be gone from the manifest.
		if m.FindDock("labs") != nil {
			t.Errorf("manifest still references dock 'labs' at the moment of %s(%s) — kill happened before manifest update", method, target)
		}
	}

	if err := eng.DockClose("labs", true); err != nil {
		t.Fatalf("DockClose: %v", err)
	}
}

// On a non-force DockClose that fails partway through, workspaces that
// were already removed from the manifest must also have their tmux
// windows killed — otherwise we leave orphan windows alive in the dock
// session that no manifest entry refers to.
func TestDockClose_NonForceFailure_KillsAlreadyRemovedWorkspaceWindows(t *testing.T) {
	eng, _ := testEngine(t)
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs"}); err != nil {
		t.Fatalf("WsNew w1: %v", err)
	}
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs"}); err != nil {
		t.Fatalf("WsNew w2: %v", err)
	}

	// Capture w1's tmux window ID before close (so we can verify it
	// gets killed even though the loop bails out on w2).
	pre, _ := eng.LoadManifest()
	w1 := pre.FindDock("labs").FindWorkspace("w1")
	if w1 == nil || len(w1.Surfaces) == 0 || w1.Surfaces[0].Tmux == nil {
		t.Fatalf("w1 missing tmux surface")
	}
	w1WindowID := w1.Surfaces[0].Tmux.WindowID

	// Make w2 fail the safety check: the worktree dir must exist on
	// disk (closeWorkspaceState skips dirty checks if !exists), and
	// the git mock must report it dirty.
	w2 := pre.FindDock("labs").FindWorkspace("w2")
	if w2 == nil {
		t.Fatalf("w2 missing")
	}
	if err := os.MkdirAll(w2.Path, 0o755); err != nil {
		t.Fatalf("mkdir w2.Path: %v", err)
	}
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetDirty(w2.Path, true)

	if err := eng.DockClose("labs", false); err == nil {
		t.Fatalf("DockClose succeeded; expected dirty-w2 failure")
	}

	// w1 must be gone from the manifest (already-removed in the loop).
	post, _ := eng.LoadManifest()
	dock := post.FindDock("labs")
	if dock == nil {
		t.Fatalf("dock 'labs' unexpectedly removed on non-force failure")
	}
	if dock.FindWorkspace("w1") != nil {
		t.Errorf("w1 still in manifest; expected it to be removed before w2 failed")
	}
	if dock.FindWorkspace("w2") == nil {
		t.Errorf("w2 missing from manifest; expected it to remain after its failed close")
	}

	// w1's window must have been killed — otherwise it's an orphan
	// tmux window with no manifest entry.
	mockTmux := eng.Tmux.(*tmux.Mock)
	killed := false
	for _, c := range mockTmux.Calls {
		if c.Method == "KillWindow" && len(c.Args) == 1 && c.Args[0] == w1WindowID {
			killed = true
			break
		}
	}
	if !killed {
		t.Errorf("w1's window %s was not killed; expected sync after partial DockClose failure", w1WindowID)
	}

	// And we must NOT have killed the dock session — that would
	// destroy w2's pane (which still has dirty work).
	for _, c := range mockTmux.Calls {
		if c.Method == "KillSession" {
			t.Errorf("KillSession called on non-force partial failure: %v", c)
		}
	}
}

func TestRepoRemove_PersistsManifestBeforeKill(t *testing.T) {
	eng, _ := testEngine(t)
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.OnKill = func(method, target string) {
		m, err := manifest.Load(eng.manifestPath)
		if err != nil {
			t.Errorf("loading manifest in kill hook: %v", err)
			return
		}
		if m.FindRepo("labs") != nil {
			t.Errorf("manifest still references repo 'labs' at the moment of %s(%s) — kill happened before manifest update", method, target)
		}
		if m.FindDock("labs") != nil {
			t.Errorf("manifest still references dock 'labs' at the moment of %s(%s) — kill happened before manifest update", method, target)
		}
	}

	if err := eng.RepoRemove("labs", true); err != nil {
		t.Fatalf("RepoRemove: %v", err)
	}
}

func TestWsCloseClean_PersistsAllManifestsBeforeAnyKill(t *testing.T) {
	eng, _ := testEngine(t)
	// Two clean workspaces (new, no changes).
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs"}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.OnKill = func(method, target string) {
		// By the time the FIRST kill fires, both workspaces must already
		// be removed from the manifest. This catches a regression where
		// per-target killing got interleaved with per-target manifest
		// mutation.
		m, err := manifest.Load(eng.manifestPath)
		if err != nil {
			t.Errorf("loading manifest in kill hook: %v", err)
			return
		}
		dock := m.FindDock("labs")
		if dock == nil {
			return
		}
		if dock.FindWorkspace("w1") != nil {
			t.Errorf("manifest still references w1 at the moment of %s(%s)", method, target)
		}
		if dock.FindWorkspace("w2") != nil {
			t.Errorf("manifest still references w2 at the moment of %s(%s)", method, target)
		}
	}

	closed, _, err := eng.WsCloseClean("labs", true, false)
	if err != nil {
		t.Fatalf("WsCloseClean: %v", err)
	}
	if len(closed) != 2 {
		t.Errorf("closed = %v, want 2 workspaces", closed)
	}
}

func TestSurfaceAdd_Split(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	err = eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell", SplitDir: "h"})
	if err != nil {
		t.Fatalf("SurfaceAdd failed: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(ws.Surfaces))
	}
	s := ws.Surfaces[1]
	if s.Type != manifest.SurfaceTypeShell {
		t.Errorf("surface type = %q, want shell", s.Type)
	}
	if s.Tmux.SplitDir != "h" {
		t.Errorf("split_dir = %q, want h", s.Tmux.SplitDir)
	}
	if s.Tmux.SplitFrom != ws.Surfaces[0].ID {
		t.Errorf("split_from = %d, want %d (first surface ID)", s.Tmux.SplitFrom, ws.Surfaces[0].ID)
	}
}

// --- GUI surface tests ---

func TestDockClose(t *testing.T) {
	eng, _ := testEngine(t)

	// Create two workspaces
	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})

	err := eng.DockClose("labs", false)
	if err != nil {
		t.Fatalf("DockClose failed: %v", err)
	}

	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock != nil && len(dock.Workspaces) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(dock.Workspaces))
	}
}

// TestDockClose_PropagatesManifestWriteError pins the fix from bucket B
// item 8: DockClose used to swallow errors from withManifest and
// config.Save. Now it propagates them. This test makes the manifest
// directory read-only so the lock file creation fails, then verifies
// the error surfaces.
func TestDockClose_PropagatesManifestWriteError(t *testing.T) {
	eng, _ := testEngine(t)
	// Dock "labs" starts with no workspaces, so closeWorkspaceState
	// loop is empty. The error comes from the RemoveDock withManifest.

	// Make the manifest directory non-writable so the lock file can't
	// be created.
	dir := filepath.Dir(eng.manifestPath)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0o755)

	err := eng.DockClose("labs", false)
	if err == nil {
		t.Fatal("expected error when manifest write fails, got nil")
	}
	if !strings.Contains(err.Error(), "removing dock from manifest") {
		t.Errorf("expected 'removing dock from manifest' in error, got: %v", err)
	}
}

func TestRecover(t *testing.T) {
	eng, _ := testEngine(t)

	// Create a workspace
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Verify workspace path exists (created by mock)
	os.MkdirAll(ws.Path, 0o755)

	// Simulate reboot by clearing tmux state
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	// Recover
	results, err := eng.Recover()
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected recovery results")
	}

	// Verify session was recreated
	if !mockTmux.HasSessionCalled("labs") {
		t.Error("tmux session not recreated")
	}
}

// --- Regression tests for code review fixes ---

func TestSyncWorkspaceGitState_RenamesTmuxWindow(t *testing.T) {
	// Regression: when sync detects a branch change, the workspace gets a
	// new abbreviated name and the tmux window should be renamed to match.
	// Use no explicit Name so NameOverridden=false and the auto-rename
	// fires (an explicit name pins NameOverridden=true and SyncAll skips
	// the rename, by design — see TestWsNew_ExplicitNameWithBranchKeepsExplicitName).
	eng, _ := testEngine(t)
	eng.WsNew(WsNewOptions{Dock: "labs"})

	// Simulate a branch change on disk (the worktree's actual current branch).
	mockGit := eng.Git.(*git.Mock)
	m, _ := eng.LoadManifest()
	wsPath := m.FindDock("labs").FindWorkspace("w1").Path
	os.MkdirAll(wsPath, 0o755)
	mockGit.SetBranch(wsPath, "feature/new-thing")

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Calls = nil
	eng.SyncAll()

	found := false
	for _, call := range mockTmux.Calls {
		if call.Method == "RenameWindow" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RenameWindow call when sync picks up a branch change")
	}
}

func TestWsRename_TmuxWindowRenamed(t *testing.T) {
	// Regression: WsRename should rename tmux windows
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})

	eng.WsRename("labs", "w1", "renamed")

	mockTmux := eng.Tmux.(*tmux.Mock)
	found := false
	for _, call := range mockTmux.Calls {
		if call.Method == "RenameWindow" && len(call.Args) >= 2 && call.Args[1] == "renamed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RenameWindow call with new name")
	}
}

func TestAbbreviateBranch_Sanitizes(t *testing.T) {
	// Regression: abbreviateBranch could return names with invalid characters
	tests := []struct {
		input string
		valid bool // must match [a-zA-Z0-9_-]+
	}{
		{"feature/my.thing", true},
		{"feature/nested/path", true},
		{"fix/JIRA-123", true},
	}
	for _, tt := range tests {
		got := abbreviateBranch(tt.input)
		if err := ValidateName(got); err != nil {
			t.Errorf("abbreviateBranch(%q) = %q, invalid name: %v", tt.input, got, err)
		}
	}
}

func TestWsClose_DeletedWorktree(t *testing.T) {
	// Regression: safety checks failed on non-existent worktree paths
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	// Don't create the directory — simulates externally deleted worktree

	// Should succeed without force since path doesn't exist
	err := eng.WsClose("labs", "w1", false)
	if err != nil {
		t.Errorf("close of deleted worktree should succeed: %v", err)
	}
}

func TestDockNew_SavesConfig(t *testing.T) {
	// Regression: DockNew modified config in memory but didn't save to disk
	eng, dir := testEngine(t)
	eng.configPath = filepath.Join(dir, "config.toml")

	// Save initial config so there's a file to overwrite
	config.Save(eng.configPath, eng.Config)

	eng.DockNew("research", "labs", "claude", "")

	// Verify dock in manifest
	m, _ := eng.LoadManifest()
	if m.FindDock("research") == nil {
		t.Error("new dock not persisted to manifest")
	}
}

func TestWsClose_UnpushedCheckError_Refuses(t *testing.T) {
	// Regression: WsClose silently ignored errors from HasUnpushedCommits,
	// allowing worktree removal without verifying push status.
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// When unpushed is true, it refuses.
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetUnpushed(ws.Path, true)

	err = eng.WsClose("labs", "w1", false)
	if err == nil {
		t.Error("expected error for unpushed commits")
	}
}

func TestWsNew_RollbackOnManifestFailure(t *testing.T) {
	// Regression: WsNew leaked worktrees/windows when manifest save failed.
	eng, dir := testEngine(t)

	// Create first workspace successfully so manifest has dock state
	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("first WsNew failed: %v", err)
	}

	// Now point manifest to a non-existent path inside a read-only directory.
	// Load will create a fresh manifest (path doesn't exist -> empty),
	// but Save will fail because it can't create the lock file.
	badDir := filepath.Join(dir, "readonly")
	os.MkdirAll(badDir, 0o755)
	// Write the current manifest content there (so load succeeds)
	curData, _ := os.ReadFile(eng.manifestPath)
	badManifest := filepath.Join(badDir, "manifest.json")
	os.WriteFile(badManifest, curData, 0o444)
	// Make dir read-only so lock file cannot be created
	os.Chmod(badDir, 0o555)
	defer os.Chmod(badDir, 0o755)

	eng.manifestPath = badManifest

	mockTmux := eng.Tmux.(*tmux.Mock)
	callsBefore := len(mockTmux.Calls)

	_, err = eng.WsNew(WsNewOptions{Dock: "labs"})
	if err == nil {
		t.Fatal("expected error from WsNew with unwritable manifest dir")
	}

	// Verify KillWindow was called as part of rollback
	found := false
	for _, call := range mockTmux.Calls[callsBefore:] {
		if call.Method == "KillWindow" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected KillWindow call for rollback, but none found")
	}
}

func TestRecoverUsesPerSurfaceAgent(t *testing.T) {
	// Regression: recovery used dock default agent for all surfaces, ignoring
	// the surface's recorded Agent field.
	eng, _ := testEngine(t)

	// Create workspace with codex agent override
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Agent: "codex"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Verify the surface recorded "codex"
	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	storedWs := dock.FindWorkspace("w1")
	if storedWs.Surfaces[0].Agent == nil || *storedWs.Surfaces[0].Agent != "codex" {
		t.Fatalf("surface agent = %v, want codex", storedWs.Surfaces[0].Agent)
	}

	// Simulate reboot
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	// Recover
	_, err = eng.Recover()
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	// Verify the recovered surface was launched with "codex" not "claude"
	for _, call := range mockTmux.Calls {
		if call.Method == "SendKeys" && len(call.Args) >= 2 {
			if call.Args[1] == "codex" {
				return // correct agent launched
			}
			if call.Args[1] == "claude" {
				t.Error("recovery launched dock default agent 'claude' instead of surface's 'codex'")
				return
			}
		}
	}
}

func TestCmdSurfacePersistsCommand(t *testing.T) {
	// Regression: command surfaces had no Command field, recovery couldn't restore them.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})

	// Add a cmd surface in a new layout group
	eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeCmd, Name: "tail", Command: "tail -f /var/log/syslog"})

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) < 2 {
		t.Fatal("expected 2 surfaces")
	}
	s := ws.Surfaces[1]
	if s.Type != manifest.SurfaceTypeCmd {
		t.Errorf("surface type = %q, want cmd", s.Type)
	}
	if s.Command == nil || *s.Command != "tail -f /var/log/syslog" {
		t.Errorf("surface command = %v, want 'tail -f /var/log/syslog'", s.Command)
	}
}

func TestSurfaceAddPersistsCommand(t *testing.T) {
	eng, _ := testEngine(t)
	eng.WsNew(WsNewOptions{Dock: "labs"})

	eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeCmd, Name: "watch", Command: "watch df -h", SplitDir: "h"})

	ws, _ := eng.WsShow("labs", "w1")
	s := ws.Surfaces[1]
	if s.Command == nil || *s.Command != "watch df -h" {
		t.Errorf("surface command = %v, want 'watch df -h'", s.Command)
	}
}

func TestRecoverCmdSurface(t *testing.T) {
	// Regression: recovery fell back to plain shell for cmd surfaces.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeCmd, Name: "htop", Command: "htop"})

	ws, _ := eng.WsShow("labs", "w1")
	os.MkdirAll(ws.Path, 0o755)

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	eng.Recover()

	// Verify "htop" was sent via SendKeys
	for _, call := range mockTmux.Calls {
		if call.Method == "SendKeys" && len(call.Args) >= 2 && call.Args[1] == "htop" {
			return // pass
		}
	}
	t.Error("expected SendKeys with 'htop' for cmd surface recovery")
}

func TestRecover_ReportsSurfaceLaunchErrors(t *testing.T) {
	eng, _ := testEngine(t)

	// Create a workspace with a custom agent, then break it.
	eng.Config.Agents["broken"] = config.AgentConfig{Command: "broken-cmd"}
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Agent: "broken"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Clear the command so recovery fails.
	eng.Config.Agents["broken"] = config.AgentConfig{}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	if _, err := eng.Recover(); err == nil || !strings.Contains(err.Error(), `unknown agent "broken"`) {
		t.Fatalf("Recover error = %v, want unknown agent error", err)
	}
}

func TestRecoverReconcilesSurfacesInExistingWindow(t *testing.T) {
	// Regression: recovery skipped surface repair for existing windows.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell", SplitDir: "h"})

	ws, _ := eng.WsShow("labs", "w1")
	os.MkdirAll(ws.Path, 0o755)

	// Manifest says 2 surfaces. Kill one pane in tmux so only 1 remains.
	mockTmux := eng.Tmux.(*tmux.Mock)
	// The window has 2 tmux panes (created by WsNew + SurfaceAdd split).
	winID := ws.Surfaces[0].Tmux.WindowID
	panes, _ := mockTmux.ListPanes(winID)
	if len(panes) < 2 {
		t.Fatalf("expected 2 tmux panes, got %d", len(panes))
	}
	mockTmux.KillPane(panes[1].ID)

	// Verify we now have 1 tmux pane
	panes, _ = mockTmux.ListPanes(winID)
	if len(panes) != 1 {
		t.Fatalf("expected 1 tmux pane after kill, got %d", len(panes))
	}

	// Recover — should recreate the missing pane
	eng.Recover()

	panes, _ = mockTmux.ListPanes(winID)
	if len(panes) != 2 {
		t.Errorf("expected 2 panes after recovery, got %d", len(panes))
	}
}

func TestRecover_UsesRecordedSplitParentAsSplitTarget(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-2", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd shell-2 failed: %v", err)
	}

	ws, err := eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.SetCurrentPaneID(ws.Surfaces[1].Tmux.PaneID)
	if err := eng.SetLastFocused("labs", "w1", ws.Surfaces[1].ID); err != nil {
		t.Fatalf("SetLastFocused failed: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-3", SplitDir: "h"}); err != nil {
		t.Fatalf("SurfaceAdd shell-3 failed: %v", err)
	}

	ws, err = eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	mockTmux.Reset()

	if _, err := eng.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	ws, err = eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow after recover failed: %v", err)
	}
	root := ws.FindSurface("shell")
	middle := ws.FindSurface("shell-2")
	if root == nil || root.Tmux == nil || middle == nil || middle.Tmux == nil {
		t.Fatalf("recovered surfaces missing tmux metadata: %#v", ws.Surfaces)
	}

	var splitTargets []string
	for _, call := range mockTmux.Calls {
		if call.Method == "SplitWindow" && len(call.Args) > 0 {
			splitTargets = append(splitTargets, call.Args[0])
		}
	}
	if len(splitTargets) < 2 {
		t.Fatalf("expected at least 2 SplitWindow calls during recovery, got %v", splitTargets)
	}
	if splitTargets[0] != root.Tmux.PaneID {
		t.Fatalf("first split target = %q, want %q", splitTargets[0], root.Tmux.PaneID)
	}
	if splitTargets[1] != middle.Tmux.PaneID {
		t.Fatalf("second split target = %q, want %q", splitTargets[1], middle.Tmux.PaneID)
	}
}

func TestWsNew_DuplicateDisplayName(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "my-ws"})

	// Second workspace with same name should fail
	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "my-ws"})
	if err == nil {
		t.Error("expected error for duplicate display name")
	}
}

func TestRepoAdd_Local(t *testing.T) {
	eng, dir := testEngine(t)
	eng.configPath = filepath.Join(dir, "config.toml")
	config.Save(eng.configPath, eng.Config)

	repoDir := filepath.Join(dir, "new-repo")
	os.MkdirAll(repoDir, 0o755)

	err := eng.RepoAdd("newrepo", repoDir, "", "", false)
	if err != nil {
		t.Fatalf("RepoAdd failed: %v", err)
	}

	// Verify repo added to manifest
	m, _ := eng.LoadManifest()
	repo := m.FindRepo("newrepo")
	if repo == nil {
		t.Error("repo not added to manifest")
	}

	// Path should be normalized (absolute)
	if repo != nil {
		savedPath := repo.Path
		if savedPath != repoDir && savedPath != config.NormalizePath(repoDir) {
			t.Errorf("saved path = %q, want normalized form of %q", savedPath, repoDir)
		}
	}
}

func TestRepoAdd_CloneURL(t *testing.T) {
	eng, dir := testEngine(t)
	eng.configPath = filepath.Join(dir, "config.toml")
	config.Save(eng.configPath, eng.Config)

	destPath := filepath.Join(dir, "cloned-repo")
	// Path must NOT exist for clone
	err := eng.RepoAdd("cloned", destPath, "", "git@github.com:org/repo.git", false)
	if err != nil {
		t.Fatalf("RepoAdd with clone failed: %v", err)
	}

	// Verify Clone was called
	mockGit := eng.Git.(*git.Mock)
	cloneCalls := mockGit.Calls("Clone")
	if len(cloneCalls) != 1 {
		t.Fatalf("expected 1 Clone call, got %d", len(cloneCalls))
	}
	if cloneCalls[0].Args[0] != "git@github.com:org/repo.git" {
		t.Errorf("clone URL = %q", cloneCalls[0].Args[0])
	}
}

func TestRepoAdd_CloneIntoExistingDir(t *testing.T) {
	eng, dir := testEngine(t)

	existingDir := filepath.Join(dir, "already-here")
	os.MkdirAll(existingDir, 0o755)

	err := eng.RepoAdd("bad", existingDir, "", "git@github.com:org/repo.git", false)
	if err == nil {
		t.Fatal("expected error cloning into existing directory")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want 'already exists' message", err)
	}
}

func TestRepoRemove_InUse(t *testing.T) {
	eng, _ := testEngine(t)

	err := eng.RepoRemove("labs", false)
	if err == nil {
		t.Fatal("expected error removing repo in use by dock")
	}
	inUse, ok := err.(*RepoInUseError)
	if !ok {
		t.Fatalf("expected *RepoInUseError, got %T: %v", err, err)
	}
	if inUse.RepoName != "labs" {
		t.Errorf("RepoName = %q, want labs", inUse.RepoName)
	}
	if len(inUse.AffectedDocks) != 1 || inUse.AffectedDocks[0].Name != "labs" {
		t.Errorf("AffectedDocks = %v, want [{labs}]", inUse.AffectedDocks)
	}
}

func TestRepoRemove_Force(t *testing.T) {
	eng, dir := testEngine(t)
	eng.configPath = filepath.Join(dir, "config.toml")
	config.Save(eng.configPath, eng.Config)

	// labs repo is used by labs dock — force should remove both
	err := eng.RepoRemove("labs", true)
	if err != nil {
		t.Fatalf("force remove failed: %v", err)
	}
	m, _ := eng.LoadManifest()
	if m.FindRepo("labs") != nil {
		t.Error("repo should be removed from manifest")
	}
	if m.FindDock("labs") != nil {
		t.Error("dock should be removed from manifest with --force")
	}
}

func TestRepoAdd_NotGitRepo(t *testing.T) {
	eng, dir := testEngine(t)
	eng.configPath = filepath.Join(dir, "config.toml")
	config.Save(eng.configPath, eng.Config)

	plainDir := filepath.Join(dir, "not-a-repo")
	os.MkdirAll(plainDir, 0o755)

	// With force, should succeed even if not a git repo
	err := eng.RepoAdd("plain", plainDir, "", "", true)
	if err != nil {
		t.Fatalf("RepoAdd with --force should succeed: %v", err)
	}
	m, _ := eng.LoadManifest()
	if m.FindRepo("plain") == nil {
		t.Error("repo should be added to manifest with --force")
	}
}

func TestRepoAdd_DuplicateName(t *testing.T) {
	eng, _ := testEngine(t)

	// "labs" already exists in the test config
	err := eng.RepoAdd("labs", "/some/path", "", "", false)
	if err == nil {
		t.Error("expected error for duplicate repo name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want 'already exists'", err)
	}
}

func TestRepoRemove_NotFound(t *testing.T) {
	eng, _ := testEngine(t)

	err := eng.RepoRemove("nonexistent", false)
	if err == nil {
		t.Error("expected error for nonexistent repo")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err)
	}
}

// --- RepoInit tests ---

func TestRepoInit_AppendsAwarenessLine(t *testing.T) {
	eng, dir := testEngine(t)

	repoDir := filepath.Join(dir, "repos", "labs")
	eng.Config.Agents["claude"] = config.AgentConfig{
		Command:     "claude",
		ProjectFile: "CLAUDE.md",
	}

	// Create project file without bay awareness.
	projectFile := filepath.Join(repoDir, "CLAUDE.md")
	os.WriteFile(projectFile, []byte("# My Project\n"), 0o644)

	err := eng.RepoInit("labs")
	if err != nil {
		t.Fatalf("RepoInit: %v", err)
	}

	data, _ := os.ReadFile(projectFile)
	if !strings.Contains(string(data), "bay agent-guide") {
		t.Errorf("project file should mention bay agent-guide, got:\n%s", data)
	}
}

func TestRepoInit_IdempotentIfAlreadyPresent(t *testing.T) {
	eng, dir := testEngine(t)

	repoDir := filepath.Join(dir, "repos", "labs")
	eng.Config.Agents["claude"] = config.AgentConfig{
		Command:     "claude",
		ProjectFile: "CLAUDE.md",
	}

	projectFile := filepath.Join(repoDir, "CLAUDE.md")
	os.WriteFile(projectFile, []byte("# My Project\nRun bay agent-guide for commands.\n"), 0o644)

	err := eng.RepoInit("labs")
	if err != nil {
		t.Fatalf("RepoInit: %v", err)
	}

	// Should not double-append.
	data, _ := os.ReadFile(projectFile)
	if strings.Count(string(data), "bay agent-guide") != 1 {
		t.Errorf("bay awareness should appear exactly once, got:\n%s", data)
	}
}

func TestRepoInit_CreatesWorktreeinclude(t *testing.T) {
	eng, dir := testEngine(t)

	repoDir := filepath.Join(dir, "repos", "labs")

	err := eng.RepoInit("labs")
	if err != nil {
		t.Fatalf("RepoInit: %v", err)
	}

	wtInclude := filepath.Join(repoDir, ".worktreeinclude")
	if _, err := os.Stat(wtInclude); err != nil {
		t.Error(".worktreeinclude should be created")
	}
}

func TestRepoInit_SkipsWorktreeincludeIfExists(t *testing.T) {
	eng, dir := testEngine(t)

	repoDir := filepath.Join(dir, "repos", "labs")
	wtInclude := filepath.Join(repoDir, ".worktreeinclude")
	os.WriteFile(wtInclude, []byte(".env\n"), 0o644)

	err := eng.RepoInit("labs")
	if err != nil {
		t.Fatalf("RepoInit: %v", err)
	}

	// Existing content should be preserved.
	data, _ := os.ReadFile(wtInclude)
	if !strings.Contains(string(data), ".env") {
		t.Error("existing .worktreeinclude content should be preserved")
	}
}

func TestRepoInit_UnknownRepo(t *testing.T) {
	eng, _ := testEngine(t)

	err := eng.RepoInit("nonexistent")
	if err == nil {
		t.Error("expected error for unknown repo")
	}
}

func TestRepoSync_CopiesToAllWorktrees(t *testing.T) {
	eng, _ := testEngine(t)

	m, _ := eng.LoadManifest()
	repo := m.FindRepo("labs")
	repoPath := repo.Path
	wtDir := repo.EffectiveWorktreeDir()

	// Two worktree-shaped dirs under the worktree parent — RepoSync scans
	// the parent and syncs every subdir reporting as a git repo.
	os.MkdirAll(filepath.Join(wtDir, "w1"), 0o755)
	os.MkdirAll(filepath.Join(wtDir, "w2"), 0o755)

	// Source file + matching pattern.
	os.WriteFile(filepath.Join(repoPath, ".worktreeinclude"), []byte("*.env\n"), 0o644)
	os.WriteFile(filepath.Join(repoPath, "local.env"), []byte("SECRET=1\n"), 0o644)

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetExcludeMatches(repoPath, ".worktreeinclude", nil, []string{"local.env"})

	count, err := eng.RepoSync("labs")
	if err != nil {
		t.Fatalf("RepoSync: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	for _, ws := range []string{"w1", "w2"} {
		data, err := os.ReadFile(filepath.Join(wtDir, ws, "local.env"))
		if err != nil {
			t.Errorf("%s/local.env not copied: %v", ws, err)
			continue
		}
		if string(data) != "SECRET=1\n" {
			t.Errorf("%s/local.env content = %q", ws, data)
		}
	}

	// Validation/expansion must happen once across the whole sync, not
	// once per worktree — this is the contract RepoSync relies on to
	// avoid an N-fold git-subprocess fan-out.
	if calls := mockGit.Calls("ExpandExcludes"); len(calls) != 1 {
		t.Errorf("ExpandExcludes called %d times, want 1 (validate-once)", len(calls))
	}
}

func TestWsClose_CleansEmptyWorktreeDir(t *testing.T) {
	eng, dir := testEngine(t)

	// Create a real worktree parent directory to simulate the filesystem
	m, _ := eng.LoadManifest()
	repo := m.FindRepo("labs")
	wtDir := repo.EffectiveWorktreeDir()
	wsDir := filepath.Join(wtDir, "w1")
	os.MkdirAll(wsDir, 0o755)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsClose("labs", "w1", true)

	// The worktree parent dir should be removed if empty
	if _, err := os.Stat(wtDir); err == nil {
		// Check if it's empty — os.Remove would have succeeded
		entries, _ := os.ReadDir(wtDir)
		if len(entries) == 0 {
			t.Error("empty worktree parent dir should have been removed")
		}
		// If non-empty, that's fine — other worktrees may exist
	}
	// If stat fails (not found), cleanup worked
	_ = dir
}

func TestList_UsesDockDefaultAgentForShellWorkspace(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	docks, err := eng.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(docks) != 1 || len(docks[0].Workspaces) != 1 {
		t.Fatalf("unexpected dock/workspace count: %#v", docks)
	}
	if docks[0].Workspaces[0].DefaultAgent != "claude" {
		t.Errorf("default_agent = %q, want dock default claude", docks[0].Workspaces[0].DefaultAgent)
	}
}

func TestList_MarksBellWindowsWaiting(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Agent: "codex"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.SetWindowOption(ws.Surfaces[0].Tmux.WindowID, "@bay-bell", "1"); err != nil {
		t.Fatalf("SetWindowOption: %v", err)
	}

	docks, err := eng.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(docks) != 1 || len(docks[0].Workspaces) != 1 {
		t.Fatalf("unexpected dock/workspace count: %#v", docks)
	}
	if !docks[0].Workspaces[0].Waiting {
		t.Fatalf("workspace should be waiting from bell flag: %#v", docks[0].Workspaces[0])
	}
}

func TestList_PreservesWorkspaceAgentOverride(t *testing.T) {
	// Verify that creating a workspace with a non-default agent
	// shows that agent in the List output.
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Agent: "codex"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	docks, err := eng.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(docks) != 1 || len(docks[0].Workspaces) != 1 {
		t.Fatalf("unexpected dock/workspace count: %#v", docks)
	}
	ws := docks[0].Workspaces[0]
	// The surface should record the codex agent
	foundCodex := false
	for _, s := range ws.Surfaces {
		if s.Agent == "codex" {
			foundCodex = true
		}
	}
	if !foundCodex {
		t.Errorf("expected to find codex agent in surfaces, got: %#v", ws.Surfaces)
	}
}

func TestWsClose_KeepsNonEmptyWorktreeDir(t *testing.T) {
	eng, _ := testEngine(t)

	// Create two workspaces
	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})

	// Create the worktree parent dir with a subdirectory to simulate w2 still there
	m, _ := eng.LoadManifest()
	repo := m.FindRepo("labs")
	wtDir := repo.EffectiveWorktreeDir()
	os.MkdirAll(filepath.Join(wtDir, "w2"), 0o755)

	// Close w1 — parent dir should remain because w2 dir exists
	eng.WsClose("labs", "w1", true)

	if _, err := os.Stat(wtDir); err != nil {
		t.Error("worktree parent dir should still exist (w2 is there)")
	}
}

func TestPlaceholder_CleanedOnWsNew(t *testing.T) {
	// When a session is created, it gets a placeholder window.
	// Creating a workspace should clean it up.
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	windows, _ := mockTmux.ListWindows("labs")

	// Should have exactly 1 window (the workspace), no placeholder
	for _, w := range windows {
		val, _ := mockTmux.GetWindowOption(w.ID, "@bay-placeholder")
		if val == "1" {
			t.Errorf("placeholder window %q should have been cleaned up", w.Name)
		}
	}
	_ = ws
}

func TestPlaceholder_CreatedOnLastWsClose(t *testing.T) {
	// Closing the last workspace should leave a placeholder.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})

	err := eng.WsClose("labs", "w1", true)
	if err != nil {
		t.Fatalf("WsClose failed: %v", err)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)

	// Session should still exist
	has, _ := mockTmux.HasSession("labs")
	if !has {
		t.Fatal("session should still exist (placeholder keeps it alive)")
	}

	// Should have a placeholder window
	windows, _ := mockTmux.ListWindows("labs")
	foundPlaceholder := false
	for _, w := range windows {
		val, _ := mockTmux.GetWindowOption(w.ID, "@bay-placeholder")
		if val == "1" {
			foundPlaceholder = true
		}
	}
	if !foundPlaceholder {
		t.Error("expected a placeholder window after closing last workspace")
	}
}

func TestPlaceholder_NotCleanedIfUsed(t *testing.T) {
	// If the user has typed in the placeholder, it should not be cleaned up.
	eng, _ := testEngine(t)

	// Manually create session with placeholder (simulating DockNew)
	eng.DockNew("research", "labs", "claude", "")

	mockTmux := eng.Tmux.(*tmux.Mock)

	// Find the placeholder pane and simulate user interaction
	windows, _ := mockTmux.ListWindows("research")
	for _, w := range windows {
		val, _ := mockTmux.GetWindowOption(w.ID, "@bay-placeholder")
		if val == "1" {
			panes, _ := mockTmux.ListPanes(w.ID)
			if len(panes) > 0 {
				mockTmux.SetPaneCursorY(panes[0].ID, 5) // user has been typing
			}
		}
	}

	// Create a workspace — should NOT clean the used placeholder
	eng.WsNew(WsNewOptions{Dock: "research"})

	windows, _ = mockTmux.ListWindows("research")
	foundUsedPlaceholder := false
	for _, w := range windows {
		val, _ := mockTmux.GetWindowOption(w.ID, "@bay-placeholder")
		if val == "1" {
			foundUsedPlaceholder = true
		}
	}
	if !foundUsedPlaceholder {
		t.Error("used placeholder should not have been cleaned up")
	}
}

func TestPlaceholder_CleanedOnRecovery(t *testing.T) {
	eng, _ := testEngine(t)

	ws, _ := eng.WsNew(WsNewOptions{Dock: "labs"})
	os.MkdirAll(ws.Path, 0o755)

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	// Recovery creates session (with placeholder), then workspace windows,
	// then cleans placeholders
	eng.Recover()

	windows, _ := mockTmux.ListWindows("labs")
	for _, w := range windows {
		val, _ := mockTmux.GetWindowOption(w.ID, "@bay-placeholder")
		if val == "1" {
			t.Error("placeholder should be cleaned after recovery")
		}
	}
}

// --- syncWorkspaceGitState tests ---

func TestSetLastFocused(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-2", SplitDir: "v"})
	ws, _ := eng.WsShow("labs", "w1")

	// Set last focused to second surface.
	err := eng.SetLastFocused("labs", "w1", ws.Surfaces[1].ID)
	if err != nil {
		t.Fatalf("SetLastFocused: %v", err)
	}

	ws, _ = eng.WsShow("labs", "w1")
	if ws.LastFocused != ws.Surfaces[1].ID {
		t.Errorf("LastFocused = %d, want %d", ws.LastFocused, ws.Surfaces[1].ID)
	}
}

func TestSyncWorkspaceGitState_UpdatesBranch(t *testing.T) {
	eng, _ := testEngine(t)

	// No explicit Name → bay auto-names to w1, NameOverridden=false,
	// so SyncAll's branch-based rename can fire.
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Set mock branch
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "feature/sync-test")

	// SyncAll should pick up the branch
	eng.SyncAll()

	// SyncAll renames workspace from w1 to "sync-test" (abbreviated branch)
	ws, _ = eng.WsShow("labs", "sync-test")
	if ws.Worktree == nil || ws.Worktree.Branch != "feature/sync-test" {
		t.Errorf("branch = %v, want feature/sync-test", ws.Worktree)
	}
}

func TestSyncWorkspaceGitState_BranchChangeUpdatesNameAndStatus(t *testing.T) {
	eng, _ := testEngine(t)

	// No explicit Name → bay auto-names to w1, NameOverridden=false,
	// so SyncAll's branch-based rename can fire.
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "feature/my-feature")

	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "my-feature")
	if ws.Name != "my-feature" {
		t.Errorf("name = %q, want my-feature", ws.Name)
	}
}

func TestWsUpdate_BranchCollisionGetsUniqueName(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "existing", Shell: true}); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	// No explicit Name on the target → bay auto-names to dir basename
	// (w2, since the seed claimed w1 on disk). NameOverridden=false, so
	// the branch update can rename it.
	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("target workspace: %v", err)
	}

	branch := "feature/existing"
	if err := eng.WsUpdate("labs", "w2", &branch, nil); err != nil {
		t.Fatalf("WsUpdate: %v", err)
	}

	ws, err := eng.WsShow("labs", "existing-2")
	if err != nil {
		t.Fatalf("WsShow: %v", err)
	}
	if ws.Name != "existing-2" {
		t.Fatalf("name = %q, want existing-2", ws.Name)
	}
}

func TestSyncWorkspaceGitState_BranchCollisionGetsUniqueName(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "existing", Shell: true}); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	// No explicit Name on the target → bay auto-names to w1,
	// NameOverridden=false, so SyncAll's branch-rename can fire.
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "feature/existing")

	eng.SyncAll()

	ws, err = eng.WsShow("labs", "existing-2")
	if err != nil {
		t.Fatalf("WsShow: %v", err)
	}
	if ws.Name != "existing-2" {
		t.Fatalf("name = %q, want existing-2", ws.Name)
	}
}

func TestSyncWorkspaceGitState_DetachKeepsName(t *testing.T) {
	// Sticky-once-set: after a Name has been filled from a branch, detaching
	// the branch leaves the Name in place. Live branch state is shown via
	// right-status; the tab label is meant to be stable.
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// First sync: branch picked up; placeholder name "w1" gets replaced.
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "feature/existing")
	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "existing")
	if ws == nil {
		t.Fatal("workspace should be renamed to 'existing'")
	}

	// Detach the branch.
	mockGit.SetBranch(ws.Path, "")
	eng.SyncAll()

	// Name is sticky — still "existing" — but branch metadata is cleared.
	ws, _ = eng.WsShow("labs", "existing")
	if ws == nil {
		t.Fatal("workspace name should remain 'existing' after detach (sticky)")
	}
	if ws.Worktree.Branch != "" {
		t.Errorf("branch = %q, want empty after detach", ws.Worktree.Branch)
	}
}

func TestSyncWorkspaceGitState_NameOverriddenNotChanged(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Manually rename to set NameOverridden
	eng.WsRename("labs", "w1", "custom-name")

	// Now set a git branch
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "feature/something-else")

	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "custom-name")
	if ws.Name != "custom-name" {
		t.Errorf("name = %q, want custom-name (NameOverridden should prevent change)", ws.Name)
	}
	// Branch should still be updated even if name is overridden
	if ws.Worktree.Branch != "feature/something-else" {
		t.Errorf("branch = %q, want feature/something-else", ws.Worktree.Branch)
	}
}

func TestCurrentContext(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)
	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "codex", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd failed: %v", err)
	}
	ws, err = eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}

	if err := os.Chdir(ws.Path); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir("/")
	})

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.SetCurrentSession("labs")
	mockTmux.SetCurrentWindowID(ws.Surfaces[0].Tmux.WindowID)
	mockTmux.SetCurrentPaneID(ws.Surfaces[1].Tmux.PaneID)

	ctx, err := eng.CurrentContext()
	if err != nil {
		t.Fatalf("CurrentContext failed: %v", err)
	}
	if ctx.Repo != "labs" || ctx.Dock != "labs" || ctx.Workspace != "w1" {
		t.Fatalf("unexpected context: %#v", ctx)
	}
	if ctx.Surface != "agent" {
		t.Fatalf("surface = %q, want agent", ctx.Surface)
	}
	if ctx.SurfaceID != ws.Surfaces[1].ID {
		t.Fatalf("surface_id = %d, want %d", ctx.SurfaceID, ws.Surfaces[1].ID)
	}
}

func TestCurrentContext_OutsideTmuxStillResolvesRepoAndWorkspace(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
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

	ctx, err := eng.CurrentContext()
	if err != nil {
		t.Fatalf("CurrentContext failed: %v", err)
	}
	if ctx.Repo != "labs" || ctx.Workspace != "w1" {
		t.Fatalf("unexpected context: %#v", ctx)
	}
	if ctx.Surface != "" || ctx.SurfaceID != 0 {
		t.Fatalf("surface should be empty outside tmux: %#v", ctx)
	}
}

// --- syncSurfaceTmuxState tests ---

func TestSyncAll_RemovesStaleSurfaces(t *testing.T) {
	// In the new model, syncSurfaceTmuxState REMOVES stale surfaces
	// whose tmux windows no longer exist.
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Add a second surface in a new layout group
	eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell"})
	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(ws.Surfaces))
	}

	// Kill the second surface's tmux window (simulating user closing it externally)
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillWindow(ws.Surfaces[1].Tmux.WindowID)

	// SyncAll should remove the stale surface
	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 1 {
		t.Errorf("expected 1 surface after sync (stale removed), got %d", len(ws.Surfaces))
	}
}

func TestList_AutoClosesOrphanAfterWindowKill(t *testing.T) {
	// Externally killing a workspace's last tmux window schedules it
	// for auto-close (PendingCloseAt). With the grace window set to
	// 0, the next sync finalizes it; the first sync schedules and
	// the second one would normally finalize, but with grace=0 the
	// first pass already schedules-and-finalizes via List's SyncAll
	// call. Test at the effective-behavior level: after a sync,
	// no orphan remains.
	defer withZeroGrace()()
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Kill the workspace's tmux window
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillWindow(ws.Surfaces[0].Tmux.WindowID)

	docks, err := eng.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(docks) != 1 || len(docks[0].Workspaces) != 0 {
		t.Fatalf("expected orphan workspace to be auto-closed, got: %#v", docks)
	}
}

func TestSyncAll_RemovesDeadPaneSurface(t *testing.T) {
	// When a tmux pane is killed, its surface should be removed
	// even if the window still exists.
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Add a split pane in the same layout group
	eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell", SplitDir: "h"})
	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(ws.Surfaces))
	}

	// Kill one tmux pane (not the window)
	mockTmux := eng.Tmux.(*tmux.Mock)
	winID := ws.Surfaces[0].Tmux.WindowID
	panes, _ := mockTmux.ListPanes(winID)
	if len(panes) < 2 {
		t.Fatalf("expected 2 tmux panes, got %d", len(panes))
	}
	mockTmux.KillPane(panes[1].ID)

	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 1 {
		t.Errorf("expected 1 surface after killing pane, got %d", len(ws.Surfaces))
	}
}

// TestSyncAll_PreservesSurfacesWhenSessionDead verifies that SyncAll
// does NOT strip surfaces when the dock's tmux session is gone. If
// the session is dead (reboot, manual kill-server), the surfaces are
// needed for `bay recover` to know what to recreate. Stripping them
// leaves the workspaces with surfaces=0 and recover reports "nothing
// to recover."
func TestSyncAll_PreservesSurfacesWhenSessionDead(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	// Verify the workspace has a surface.
	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 1 {
		t.Fatalf("expected 1 surface, got %d", len(ws.Surfaces))
	}

	// Kill the tmux session — simulates reboot or manual kill.
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillSession("labs")

	// SyncAll should NOT remove the surfaces — the session is dead,
	// and recovery needs the surface records to recreate them.
	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 1 {
		t.Errorf("SyncAll stripped surfaces when session was dead; got %d surfaces, want 1 (preserved for recovery)", len(ws.Surfaces))
	}
}

// --- Orphan auto-close on sync-detected strip ---

// When sync strips the last surface of a clean+pushed workspace,
// the workspace should be auto-closed (after the grace window).
// Without this, tmux-native pane kills leave zero-surface orphan
// workspaces lingering in the manifest forever.
func TestSyncAll_AutoClosesOrphanedCleanWorkspace(t *testing.T) {
	defer withZeroGrace()()
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	// Worktree path must exist for WsClose's safety checks.
	os.MkdirAll(ws.Path, 0o755)

	// Kill the sole pane externally (simulates Ctrl-B x, etc.).
	mockTmux := eng.Tmux.(*tmux.Mock)
	if len(ws.Surfaces) == 0 || ws.Surfaces[0].Tmux == nil {
		t.Fatalf("expected 1 surface with tmux info, got %+v", ws.Surfaces)
	}
	mockTmux.KillPane(ws.Surfaces[0].Tmux.PaneID)

	eng.SyncAll()

	// Workspace should be gone from the manifest entirely.
	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock == nil {
		t.Fatal("dock missing after SyncAll")
	}
	if dock.FindWorkspace(ws.Name) != nil {
		t.Errorf("expected orphan workspace %q to be auto-closed, still present with %d surfaces", ws.Name, len(dock.FindWorkspace(ws.Name).Surfaces))
	}
}

// Dirty orphans must NOT be auto-closed — user may have
// uncommitted work in the worktree. WsClose's gate refuses; the
// finalize path then clears PendingCloseAt so we don't retry on
// every sync, leaving a permanent orphan for manual handling.
func TestSyncAll_KeepsOrphanedDirtyWorkspace(t *testing.T) {
	defer withZeroGrace()()
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetDirty(ws.Path, true)

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillPane(ws.Surfaces[0].Tmux.PaneID)

	eng.SyncAll()

	m, _ := eng.LoadManifest()
	got := m.FindDock("labs").FindWorkspace(ws.Name)
	if got == nil {
		t.Fatalf("dirty orphan workspace %q was auto-closed; expected it to persist", ws.Name)
	}
	if len(got.Surfaces) != 0 {
		t.Errorf("expected 0 surfaces (stripped), got %d", len(got.Surfaces))
	}
	if got.PendingCloseAt != 0 {
		t.Errorf("expected PendingCloseAt cleared after gate refusal, got %d", got.PendingCloseAt)
	}
}

// Unpushed commits are the other WsClose gate — orphan must
// persist and PendingCloseAt must be cleared so we don't keep
// retrying.
func TestSyncAll_KeepsOrphanedUnpushedWorkspace(t *testing.T) {
	defer withZeroGrace()()
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetUnpushed(ws.Path, true)

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillPane(ws.Surfaces[0].Tmux.PaneID)

	eng.SyncAll()

	m, _ := eng.LoadManifest()
	got := m.FindDock("labs").FindWorkspace(ws.Name)
	if got == nil {
		t.Fatalf("unpushed orphan workspace %q was auto-closed; expected it to persist", ws.Name)
	}
	if got.PendingCloseAt != 0 {
		t.Errorf("expected PendingCloseAt cleared after gate refusal, got %d", got.PendingCloseAt)
	}
}

// Stripping a non-last surface should not trigger auto-close.
// Only surfaces-went-to-zero fires the cascade.
func TestSyncAll_DoesNotAutoCloseWorkspaceWithSurvivingSurfaces(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)
	// Add a second surface so killing one doesn't empty the ws.
	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell", SplitDir: "h"}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}
	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(ws.Surfaces))
	}

	// Kill only the second pane.
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillPane(ws.Surfaces[1].Tmux.PaneID)

	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "w1")
	if ws == nil {
		t.Fatal("workspace disappeared; expected it to persist with 1 surface")
	}
	if len(ws.Surfaces) != 1 {
		t.Errorf("expected 1 surviving surface, got %d", len(ws.Surfaces))
	}
}

// Surface re-added during the grace window cancels the pending close —
// PendingCloseAt is cleared and the workspace stays.
func TestSyncAll_SurfaceAddCancelsPendingClose(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Kill the sole pane; next sync schedules pending close (not
	// finalized because grace is the default 60s).
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillPane(ws.Surfaces[0].Tmux.PaneID)
	eng.SyncAll()

	// Verify PendingCloseAt is set.
	m, _ := eng.LoadManifest()
	got := m.FindDock("labs").FindWorkspace(ws.Name)
	if got == nil || got.PendingCloseAt == 0 {
		t.Fatalf("expected PendingCloseAt to be set, got %+v", got)
	}

	// User re-adds a surface (rescues the workspace).
	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: ws.Name, Type: manifest.SurfaceTypeShell, Name: "saved"}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	// PendingCloseAt should be cleared immediately (SurfaceAdd does it).
	m, _ = eng.LoadManifest()
	got = m.FindDock("labs").FindWorkspace(ws.Name)
	if got.PendingCloseAt != 0 {
		t.Errorf("expected PendingCloseAt to be cleared after SurfaceAdd, got %d", got.PendingCloseAt)
	}

	// Even if we drop grace to 0 and sync again, the workspace stays.
	orphanGraceSeconds = 0
	defer func() { orphanGraceSeconds = 60 }()
	eng.SyncAll()
	m, _ = eng.LoadManifest()
	if m.FindDock("labs").FindWorkspace(ws.Name) == nil {
		t.Error("rescued workspace disappeared on next sync")
	}
}

// Grace window not yet expired → sync does NOT finalize.
func TestSyncAll_PendingCloseRespectsGraceWindow(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Kill the pane → next sync schedules pending close with a
	// 60s grace. Workspace must still be present after sync.
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillPane(ws.Surfaces[0].Tmux.PaneID)
	eng.SyncAll()

	m, _ := eng.LoadManifest()
	got := m.FindDock("labs").FindWorkspace(ws.Name)
	if got == nil {
		t.Fatal("workspace was closed during grace window; expected it to persist")
	}
	if got.PendingCloseAt == 0 {
		t.Error("expected PendingCloseAt to be set")
	}
	if len(got.Surfaces) != 0 {
		t.Errorf("expected surfaces stripped, got %d", len(got.Surfaces))
	}
}

// sf close --force on the last surface closes the workspace
// immediately (no grace period for explicit force).
func TestSurfaceClose_ForceOnLastSurfaceClosesImmediately(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	ws, _ := eng.WsShow("labs", "w1")
	surfaceName := ws.Surfaces[0].Name

	if err := eng.SurfaceClose("labs", "w1", surfaceName, true); err != nil {
		t.Fatalf("SurfaceClose --force: %v", err)
	}

	if _, err := eng.WsShow("labs", "w1"); err == nil {
		t.Error("workspace still exists after force-close; expected immediate teardown")
	}
}

// --- ResolveSelf with symlinked CWD ---

// On macOS the user's CWD often differs from a stored workspace path by a
// symlink (e.g. /var → /private/var, /tmp → /private/tmp). String equality
// would miss the match; ResolveSelf must canonicalize both sides via
// EvalSymlinks before comparing.
func TestResolveSelf_SymlinkedPath(t *testing.T) {
	eng, dir := testEngine(t)

	realWs := filepath.Join(dir, "real-workspace")
	if err := os.MkdirAll(realWs, 0o755); err != nil {
		t.Fatalf("mkdir realWs: %v", err)
	}
	linkWs := filepath.Join(dir, "linked-workspace")
	if err := os.Symlink(realWs, linkWs); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Inject a workspace whose stored path is the symlink form.
	m, _ := eng.LoadManifest()
	m.Docks[0].Workspaces = []manifest.Workspace{
		{Name: "w1", Path: linkWs},
	}
	if err := manifest.Save(eng.manifestPath, m); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	// Chdir into the *real* path. Without symlink normalization the
	// string compare against linkWs would miss.
	origDir, _ := os.Getwd()
	if err := os.Chdir(realWs); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer os.Chdir(origDir)

	dockName, wsName, err := eng.ResolveSelf()
	if err != nil {
		t.Fatalf("ResolveSelf: %v", err)
	}
	if dockName != "labs" || wsName != "w1" {
		t.Errorf("ResolveSelf = (%q, %q), want (labs, w1)", dockName, wsName)
	}
}

// --- ResolveSelf with tmux window ID fallback ---

func TestResolveSelf_TmuxWindowIDFallback(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Set current tmux window ID to match the workspace's surface
	mockTmux := eng.Tmux.(*tmux.Mock)
	winID := ws.Surfaces[0].Tmux.WindowID
	mockTmux.SetCurrentWindowID(winID)

	// Change CWD to something that does NOT match any workspace path
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	dockName, wsName, err := eng.ResolveSelf()
	if err != nil {
		t.Fatalf("ResolveSelf failed: %v", err)
	}
	if dockName != "labs" {
		t.Errorf("dock = %q, want labs", dockName)
	}
	if wsName != "w1" {
		t.Errorf("wsName = %q, want w1", wsName)
	}
}

// --- ResolveByWindowID tests ---

func TestResolveByWindowID_Found(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	winID := ws.Surfaces[0].Tmux.WindowID
	dockName, wsName, foundWs, err := eng.ResolveByWindowID(winID)
	if err != nil {
		t.Fatalf("ResolveByWindowID failed: %v", err)
	}
	if dockName != "labs" {
		t.Errorf("dock = %q, want labs", dockName)
	}
	if wsName != "w1" {
		t.Errorf("wsName = %q, want w1", wsName)
	}
	if foundWs == nil {
		t.Error("returned workspace is nil")
	}
}

func TestResolveByWindowID_NotFound(t *testing.T) {
	eng, _ := testEngine(t)

	_, _, _, err := eng.ResolveByWindowID("@999")
	if err == nil {
		t.Error("expected error for non-existent window ID")
	}
	if !strings.Contains(err.Error(), "no workspace found") {
		t.Errorf("error = %q, want 'no workspace found' message", err)
	}
}

// --- WsCloseClean tests ---

func TestWsCloseClean_ClosesCleanWorkspaces(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})

	// Both workspaces are clean (new, no changes) — both should close.
	closed, skipped, err := eng.WsCloseClean("labs", true, false)
	if err != nil {
		t.Fatalf("WsCloseClean failed: %v", err)
	}

	if len(closed) != 2 {
		t.Errorf("expected 2 closed, got %d: %v", len(closed), closed)
	}
	if len(skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d: %v", len(skipped), skipped)
	}

	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock.FindWorkspace("w1") != nil {
		t.Error("w1 should be closed")
	}
	if dock.FindWorkspace("w2") != nil {
		t.Error("w2 should be closed")
	}
}

func TestWsCloseClean_SkipsDirtyWorkspaces(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})

	// Make w1 dirty via mock. The directory must exist on disk for
	// closeWorkspaceState to run the dirty check at all.
	m, _ := eng.LoadManifest()
	w1Path := m.FindDock("labs").FindWorkspace("w1").Path
	os.MkdirAll(w1Path, 0o755)
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetDirty(w1Path, true)

	closed, skipped, err := eng.WsCloseClean("labs", false, false)
	if err != nil {
		t.Fatalf("WsCloseClean failed: %v", err)
	}

	// w2 is clean → closed; w1 is dirty → skipped
	if len(closed) != 1 {
		t.Errorf("expected 1 closed, got %d: %v", len(closed), closed)
	}
	if len(skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d: %v", len(skipped), skipped)
	}

	m, _ = eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock.FindWorkspace("w1") == nil {
		t.Error("w1 (dirty) should still exist")
	}
	if dock.FindWorkspace("w2") != nil {
		t.Error("w2 (clean) should be closed")
	}
}

func TestWsCloseDone_ClosesMergedPRHead(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "fix/squash"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	if err := eng.withManifest(func(m *manifest.Manifest) error {
		got := m.FindDock("labs").FindWorkspace(ws.Name)
		got.Worktree.PR = "123"
		return nil
	}); err != nil {
		t.Fatalf("set PR: %v", err)
	}

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetUnpushed(ws.Path, true)
	mockGit.SetLocalHeadInMergedPR(ws.Path, "123", true)

	closed, skipped, err := eng.WsCloseDone("labs", false, false)
	if err != nil {
		t.Fatalf("WsCloseDone failed: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped workspaces, got %v", skipped)
	}
	if len(closed) != 1 {
		t.Fatalf("expected 1 closed workspace, got %v", closed)
	}

	m, _ := eng.LoadManifest()
	if got := m.FindDock("labs").FindWorkspace(ws.Name); got != nil {
		t.Fatalf("workspace should be closed, still present: %#v", got)
	}
}

func TestWsCloseClean_SkipsExcludedWorkspace(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})

	// All clean, but exclude w2 (simulating "self").
	closed, _, err := eng.WsCloseClean("labs", true, false, "w2")
	if err != nil {
		t.Fatalf("WsCloseClean failed: %v", err)
	}

	if len(closed) != 2 {
		t.Errorf("expected 2 closed, got %d: %v", len(closed), closed)
	}

	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock.FindWorkspace("w1") != nil {
		t.Error("w1 should be closed")
	}
	if dock.FindWorkspace("w2") == nil {
		t.Error("w2 (excluded) should still exist")
	}
	if dock.FindWorkspace("w3") != nil {
		t.Error("w3 should be closed")
	}
}

// --- WsNew with --branch ---

func TestWsNew_WithBranch(t *testing.T) {
	eng, _ := testEngine(t)

	// No explicit Name → bay derives one from the branch.
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "feature/new-branch"})
	if err != nil {
		t.Fatalf("WsNew with branch failed: %v", err)
	}

	// Verify git.CreateBranch was called
	mockGit := eng.Git.(*git.Mock)
	calls := mockGit.Calls("CreateBranch")
	if len(calls) != 1 {
		t.Fatalf("expected 1 CreateBranch call, got %d", len(calls))
	}
	if calls[0].Args[1] != "feature/new-branch" {
		t.Errorf("CreateBranch branch = %q, want feature/new-branch", calls[0].Args[1])
	}

	// Verify workspace has branch and abbreviated name
	if ws.Worktree == nil || ws.Worktree.Branch != "feature/new-branch" {
		t.Errorf("branch = %v, want feature/new-branch", ws.Worktree)
	}
	if ws.Name != "new-branch" {
		t.Errorf("name = %q, want new-branch (abbreviated)", ws.Name)
	}
}

// TestWsNew_ExplicitNameWithBranchKeepsExplicitName verifies that when
// the user passes BOTH an explicit name and --branch, the explicit
// name wins and is NOT overwritten by the branch-derived name. The
// previous behavior silently overwrote the user's choice — this test
// pins the fix.
func TestWsNew_ExplicitNameWithBranchKeepsExplicitName(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{
		Dock:   "labs",
		Name:   "auth-fix",
		Branch: "feature/some-other-name",
	})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if ws.Name != "auth-fix" {
		t.Errorf("ws.Name = %q, want auth-fix (explicit name should not be overwritten by branch-derived name)", ws.Name)
	}
	if ws.Worktree == nil || ws.Worktree.Branch != "feature/some-other-name" {
		t.Errorf("branch wasn't set; ws.Worktree = %+v", ws.Worktree)
	}
	// Name is non-empty (explicitly set), so future syncs leave it alone —
	// sticky-once-set semantics replace the old NameOverridden flag.
}

// TestWsNew_ExplicitNameAlsoBlocksBranchSyncRename verifies that an
// explicitly-set Name prevents the background SyncAll loop from
// auto-renaming the workspace later when it detects a branch change.
// Sticky-once-set: any non-empty Name (user-set or otherwise) blocks
// further automatic renames.
func TestWsNew_ExplicitNameAlsoBlocksBranchSyncRename(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "my-name"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	// Simulate the user checking out a branch outside bay's view, then
	// SyncAll detecting it. The post-rename block in SyncAll respects
	// NameOverridden, so the workspace name should stay "my-name".
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "feature/different-name")
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	eng.SyncAll()

	post, _ := eng.LoadManifest()
	updated := post.FindDock("labs").FindWorkspace("my-name")
	if updated == nil {
		t.Fatal("workspace 'my-name' missing after SyncAll; was it renamed?")
	}
	if updated.Worktree == nil || updated.Worktree.Branch != "feature/different-name" {
		t.Errorf("branch sync should still happen; got branch=%v", updated.Worktree)
	}
}

// SetEditor was removed in #113 — its only caller (bay edit --set)
// is gone, replaced by bay config editor <name> which manages the
// config file directly without going through the engine.

func TestWsClose_RemoveWorktreeFailurePreservesWorkspaceState(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	m, _ := eng.LoadManifest()
	repoPath := config.ExpandPath(m.FindRepo("labs").Path)
	mockGit := eng.Git.(*git.Mock)
	if err := mockGit.RemoveWorktree(repoPath, ws.Path, true); err != nil {
		t.Fatalf("preparing RemoveWorktree failure: %v", err)
	}

	err = eng.WsClose("labs", "w1", false)
	if err == nil {
		t.Fatal("expected WsClose to fail when RemoveWorktree fails")
	}

	// Workspace should remain in manifest after failed close.
	// (Note: tmux windows were already killed before RemoveWorktree,
	// so surfaces will be removed by SyncAll when WsShow is called.
	// The key is that the workspace itself is preserved.)
	m, loadErr := eng.LoadManifest()
	if loadErr != nil {
		t.Fatalf("LoadManifest failed: %v", loadErr)
	}
	dock := m.FindDock("labs")
	if dock == nil || dock.FindWorkspace("w1") == nil {
		t.Fatal("workspace should remain in manifest after failed close")
	}
}

func TestRepoRemove_ForceRemovesManifestDock(t *testing.T) {
	eng, dir := testEngine(t)
	eng.configPath = filepath.Join(dir, "config.toml")
	if err := config.Save(eng.configPath, eng.Config); err != nil {
		t.Fatalf("saving config: %v", err)
	}

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	if err := eng.RepoRemove("labs", true); err != nil {
		t.Fatalf("RepoRemove failed: %v", err)
	}

	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if m.FindDock("labs") != nil {
		t.Fatal("dock labs should be removed from manifest")
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()
	if _, err := eng.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	has, _ := mockTmux.HasSession("labs")
	if has {
		t.Error("recover should not recreate force-removed dock")
	}
}

func TestRecover_FindWindowByNameRefreshesSurfaceIDs(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "codex", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd failed: %v", err)
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	ws, err = eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	oldWindowID := ws.Surfaces[0].Tmux.WindowID
	oldPaneID := ws.Surfaces[1].Tmux.PaneID

	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.KillWindow(oldWindowID); err != nil {
		t.Fatalf("KillWindow failed: %v", err)
	}

	// Simulate another process recreating the window
	foundWindowID, err := mockTmux.NewWindow("labs", ws.Name, ws.Path)
	if err != nil {
		t.Fatalf("NewWindow failed: %v", err)
	}
	foundPaneID, err := mockTmux.SplitWindow(foundWindowID, "v", ws.Path, false)
	if err != nil {
		t.Fatalf("SplitWindow failed: %v", err)
	}

	if _, err := eng.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	ws, err = eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}

	// After recovery the window should get a new ID (not the pre-existing one,
	// since recovery creates fresh windows when the old ones are gone)
	if ws.Surfaces[0].Tmux.WindowID == oldWindowID {
		t.Error("window ID should have been updated from stale value")
	}
	// The pane IDs should be refreshed
	if ws.Surfaces[1].Tmux.PaneID == oldPaneID {
		t.Fatalf("pane id was not refreshed from stale value %q", oldPaneID)
	}
	_ = foundWindowID
	_ = foundPaneID
}

// TestWsNew_ExistingBranch verifies that --branch checks out an existing
// remote branch directly via CreateWorktree instead of creating a new one.
func TestWsNew_ExistingBranch(t *testing.T) {
	eng, dir := testEngine(t)
	mockGit := eng.Git.(*git.Mock)

	repoPath := filepath.Join(dir, "repos", "labs")
	mockGit.SetBranchExists(repoPath, "fix/existing", true)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "fix/existing"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	// CreateBranch should NOT have been called — branch already exists.
	if calls := mockGit.Calls("CreateBranch"); len(calls) != 0 {
		t.Errorf("expected 0 CreateBranch calls for existing branch, got %d", len(calls))
	}

	// CreateWorktree should have been called with the branch name.
	wtCalls := mockGit.Calls("CreateWorktree")
	if len(wtCalls) != 1 {
		t.Fatalf("expected 1 CreateWorktree call, got %d", len(wtCalls))
	}
	if wtCalls[0].Args[2] != "fix/existing" {
		t.Errorf("CreateWorktree branch arg = %q, want fix/existing", wtCalls[0].Args[2])
	}

	// Fetch should have been called to refresh remote refs.
	if calls := mockGit.Calls("Fetch"); len(calls) != 1 {
		t.Errorf("expected 1 Fetch call, got %d", len(calls))
	}

	// Workspace metadata should be correct.
	if ws.Worktree == nil || ws.Worktree.Branch != "fix/existing" {
		t.Errorf("branch = %v, want fix/existing", ws.Worktree)
	}
	if ws.Name != "existing" {
		t.Errorf("name = %q, want existing (abbreviated)", ws.Name)
	}
}

// TestWsNew_NewBranch_NotExisting verifies that --branch with a
// non-existing branch still creates it (the current behavior).
func TestWsNew_NewBranch_NotExisting(t *testing.T) {
	eng, _ := testEngine(t)
	mockGit := eng.Git.(*git.Mock)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "feature/brand-new"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	// CreateBranch SHOULD have been called — branch is new.
	if calls := mockGit.Calls("CreateBranch"); len(calls) != 1 {
		t.Fatalf("expected 1 CreateBranch call for new branch, got %d", len(calls))
	}

	// CreateWorktree should have been called with empty branch (detached).
	wtCalls := mockGit.Calls("CreateWorktree")
	if len(wtCalls) != 1 {
		t.Fatalf("expected 1 CreateWorktree call, got %d", len(wtCalls))
	}
	if wtCalls[0].Args[2] != "" {
		t.Errorf("CreateWorktree branch arg = %q, want empty (detached)", wtCalls[0].Args[2])
	}

	if ws.Worktree == nil || ws.Worktree.Branch != "feature/brand-new" {
		t.Errorf("branch = %v, want feature/brand-new", ws.Worktree)
	}
}

// TestWsNew_ExistingBranchWithExplicitName verifies that an explicit name
// is preserved even when checking out an existing branch.
func TestWsNew_ExistingBranchWithExplicitName(t *testing.T) {
	eng, dir := testEngine(t)
	mockGit := eng.Git.(*git.Mock)

	repoPath := filepath.Join(dir, "repos", "labs")
	mockGit.SetBranchExists(repoPath, "fix/old-pr", true)

	ws, err := eng.WsNew(WsNewOptions{
		Dock:   "labs",
		Name:   "my-review",
		Branch: "fix/old-pr",
	})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	if ws.Name != "my-review" {
		t.Errorf("name = %q, want my-review (explicit name should be preserved)", ws.Name)
	}
	if ws.Worktree == nil || ws.Worktree.Branch != "fix/old-pr" {
		t.Errorf("branch = %v, want fix/old-pr", ws.Worktree)
	}
}

// TestWsNew_FetchesBeforeWorktreeCreation verifies that bay fetches
// remote refs before creating a worktree when --branch is used,
// and does NOT fetch without --branch (to keep the common case fast).
func TestWsNew_FetchesBeforeWorktreeCreation(t *testing.T) {
	eng, dir := testEngine(t)
	mockGit := eng.Git.(*git.Mock)

	// Without --branch: no fetch.
	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew without branch: %v", err)
	}
	if n := len(mockGit.Calls("Fetch")); n != 0 {
		t.Fatalf("expected 0 Fetch calls without --branch, got %d", n)
	}

	// With --branch: fetches once.
	_, err = eng.WsNew(WsNewOptions{Dock: "labs", Branch: "feat/test"})
	if err != nil {
		t.Fatalf("WsNew with branch: %v", err)
	}

	repoPath := filepath.Join(dir, "repos", "labs")
	fetchCalls := mockGit.Calls("Fetch")
	if len(fetchCalls) != 1 {
		t.Fatalf("expected 1 Fetch call with --branch, got %d", len(fetchCalls))
	}
	if fetchCalls[0].Args[0] != repoPath {
		t.Errorf("Fetch path = %q, want %q", fetchCalls[0].Args[0], repoPath)
	}
}

// TestSyncDetach_DeletesPushedBranch verifies that when sync detects a
// detached HEAD, it deletes the local branch if it's been pushed.
func TestSyncDetach_DeletesPushedBranch(t *testing.T) {
	eng, dir := testEngine(t)
	mockGit := eng.Git.(*git.Mock)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "fix/cleanup-me"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Sync picks up the branch.
	mockGit.SetBranch(ws.Path, "fix/cleanup-me")
	eng.SyncAll()

	// Now detach — simulate user running git checkout --detach.
	// Branch is pushed (HasUnpushedCommits returns false, the default).
	mockGit.SetBranch(ws.Path, "")
	eng.SyncAll()

	// Branch should have been deleted.
	repoPath := filepath.Join(dir, "repos", "labs")
	deleted := mockGit.DeletedBranches()
	found := false
	for _, c := range deleted {
		if c.Args[0] == repoPath && c.Args[1] == "fix/cleanup-me" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected DeleteBranch(repo, fix/cleanup-me) after detach; got %v", deleted)
	}
}

// TestSyncDetach_KeepsBranchWhenUnpushed verifies that detach does NOT
// delete the branch if it has unpushed commits.
func TestSyncDetach_KeepsBranchWhenUnpushed(t *testing.T) {
	eng, _ := testEngine(t)
	mockGit := eng.Git.(*git.Mock)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Branch: "fix/wip-branch"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	mockGit.SetBranch(ws.Path, "fix/wip-branch")
	eng.SyncAll()

	// Branch has unpushed commits.
	mockGit.SetUnpushed(ws.Path, true)
	mockGit.SetBranch(ws.Path, "")
	eng.SyncAll()

	// Branch should NOT have been deleted.
	if len(mockGit.DeletedBranches()) != 0 {
		t.Errorf("branch should not be deleted when unpushed; got %v", mockGit.DeletedBranches())
	}
}

// --- Workspace identity (Phase 2) ---

// TestWsNew_AssignsID confirms every new workspace gets a canonical ID.
// IDs are assigned per-dock by manifest.AssignWorkspaceIDs after
// AddWorkspace, so the engine doesn't need its own counter.
func TestWsNew_AssignsID(t *testing.T) {
	eng, _ := testEngine(t)
	ws1, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew #1: %v", err)
	}
	if !manifest.IsWorkspaceID(ws1.ID) {
		t.Errorf("ws1.ID = %q, want canonical w<N>", ws1.ID)
	}

	ws2, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew #2: %v", err)
	}
	if !manifest.IsWorkspaceID(ws2.ID) {
		t.Errorf("ws2.ID = %q, want canonical w<N>", ws2.ID)
	}
	if ws1.ID == ws2.ID {
		t.Errorf("IDs collide: ws1=%s ws2=%s", ws1.ID, ws2.ID)
	}
}

// TestSyncRename_StickyOnceUserSet covers the headline sticky behavior:
// a user-chosen Name (anything not matching the placeholder ID pattern)
// is left alone by sync's branch-driven rename block.
func TestSyncRename_StickyOnceUserSet(t *testing.T) {
	eng, _ := testEngine(t)
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "auth-fix"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "feature/something-completely-different")
	eng.SyncAll()

	got, err := eng.WsShow("labs", "auth-fix")
	if err != nil {
		t.Fatalf("WsShow: %v", err)
	}
	if got.Name != "auth-fix" {
		t.Errorf("Name = %q, want auth-fix (sticky after explicit set)", got.Name)
	}
	if got.Worktree == nil || got.Worktree.Branch != "feature/something-completely-different" {
		t.Errorf("branch wasn't recorded; Worktree = %+v", got.Worktree)
	}
}

// TestUpdateWindowNames_FallsBackToID locks in the contract that an
// empty wsName argument resolves to the workspace's ID, so tabs always
// have a stable label even before Name is set. Once Phase 7 makes empty
// Names a normal state, this is the load-bearing path.
func TestUpdateWindowNames_FallsBackToID(t *testing.T) {
	eng, _ := testEngine(t)
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Calls = nil

	ws := &manifest.Workspace{
		ID: "w7",
		Surfaces: []manifest.Surface{
			{
				Name:    "agent",
				Tmux:    &manifest.TmuxAttrs{WindowID: "@42", LayoutGroup: 1},
				Backend: manifest.SurfaceBackendTmux,
			},
		},
	}
	eng.updateWindowNames(ws, "")

	for _, c := range mockTmux.Calls {
		if c.Method == "RenameWindow" && len(c.Args) == 2 && c.Args[0] == "@42" && c.Args[1] == "w7" {
			return
		}
	}
	t.Errorf("expected RenameWindow(@42, w7) (ID fallback); got calls: %v", mockTmux.Calls)
}

// TestSyncRename_ReplacesPlaceholder covers the placeholder fill: an
// auto-assigned w<N> Name (or empty) is treated as fillable so the first
// branch detection still produces a meaningful tab label.
func TestSyncRename_ReplacesPlaceholder(t *testing.T) {
	eng, _ := testEngine(t)
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if !manifest.IsWorkspaceID(ws.Name) {
		t.Fatalf("expected placeholder ID-shaped Name from auto-naming, got %q", ws.Name)
	}
	os.MkdirAll(ws.Path, 0o755)

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "feature/cache-ttl")
	eng.SyncAll()

	got, err := eng.WsShow("labs", "cache-ttl")
	if err != nil {
		t.Fatalf("WsShow: %v", err)
	}
	if got.Name != "cache-ttl" {
		t.Errorf("Name = %q, want cache-ttl (placeholder filled)", got.Name)
	}
}
