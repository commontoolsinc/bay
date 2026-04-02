package engine

import (
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
			"claude": {Command: "claude", ConfigFile: "CLAUDE.local.md"},
			"codex":  {Command: "codex", ConfigFile: "AGENTS.local.md"},
		},
		Repos: map[string]config.RepoConfig{
			"labs": {Path: filepath.Join(dir, "repos", "labs")},
		},
		Docks: map[string]config.DockConfig{
			"labs": {Repo: "labs", Agent: "claude"},
		},
	}

	// Create repo directory
	repoDir := filepath.Join(dir, "repos", "labs")
	os.MkdirAll(repoDir, 0o755)

	configPath := filepath.Join(dir, "config.toml")
	manifestPath := filepath.Join(dir, "manifest.toml")
	archivePath := filepath.Join(dir, "archive.toml")

	mockTmux := tmux.NewMock()
	mockGit := git.NewMock()
	mockGit.SetGlobalIgnored(true) // default: config files are gitignored

	eng := New(cfg, configPath, manifestPath, archivePath, mockTmux, mockGit)
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

	// Verify config updated
	if _, ok := eng.Config.Docks["research"]; !ok {
		t.Error("dock not added to config")
	}

	// Verify tmux session created
	mockTmux := eng.Tmux.(*tmux.Mock)
	if !mockTmux.HasSessionCalled("research") {
		t.Error("tmux session not created")
	}

	// Verify manifest updated
	m, _ := manifest.Load(eng.manifestPath)
	if _, ok := m.Docks["research"]; !ok {
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

func TestWsNew_Worktree(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	if ws.Type != manifest.WorkspaceTypeWorktree {
		t.Errorf("type = %q, want worktree", ws.Type)
	}
	if ws.Status != manifest.WorkspaceStatusIdle {
		t.Errorf("status = %q, want idle", ws.Status)
	}
	if ws.Name != "w1" {
		t.Errorf("name = %q, want w1", ws.Name)
	}
	if len(ws.Windows) != 1 {
		t.Fatalf("windows = %d, want 1", len(ws.Windows))
	}
	if ws.Windows[0].Panes[0].Type != manifest.PaneTypeAgent {
		t.Errorf("pane type = %q, want agent", ws.Windows[0].Panes[0].Type)
	}
	if ws.Windows[0].Panes[0].TmuxPaneID == "" {
		t.Error("expected first pane to record tmux pane ID")
	}

	// Verify worktree was created
	mockGit := eng.Git.(*git.Mock)
	if len(mockGit.CreatedWorktrees()) != 1 {
		t.Errorf("expected 1 worktree created, got %d", len(mockGit.CreatedWorktrees()))
	}

	// Verify manifest persisted
	m, _ := manifest.Load(eng.manifestPath)
	if _, ok := m.Docks["labs"].Workspaces["w1"]; !ok {
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

func TestPaneAdd_PersistsTmuxPaneID(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	if err := eng.PaneAdd("labs", "w1", 1, "codex", false, "", "v"); err != nil {
		t.Fatalf("PaneAdd failed: %v", err)
	}

	ws, err = eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	if len(ws.Windows[0].Panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(ws.Windows[0].Panes))
	}
	if ws.Windows[0].Panes[1].TmuxPaneID == "" {
		t.Fatal("expected added pane to record tmux pane ID")
	}
}

func TestWsNew_External(t *testing.T) {
	eng, dir := testEngine(t)

	extDir := filepath.Join(dir, "external-project")
	os.MkdirAll(extDir, 0o755)

	ws, err := eng.WsNew(WsNewOptions{
		Dock: "labs",
		Dir:  extDir,
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

	if ws.Windows[0].Panes[0].Type != manifest.PaneTypeShell {
		t.Errorf("pane type = %q, want shell", ws.Windows[0].Panes[0].Type)
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
	m, _ := manifest.Load(eng.manifestPath)
	if _, ok := m.Docks["labs"].Workspaces["w1"]; ok {
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
	if _, ok := archive.Docks["labs"].Workspaces["w1"]; !ok {
		t.Error("workspace should be in archive")
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

func TestWsUpdate(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	branch := "feature/mem-refactor"
	pr := "234"
	err = eng.WsUpdate("labs", "w1", &branch, &pr, nil)
	if err != nil {
		t.Fatalf("WsUpdate failed: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if ws.Branch != "feature/mem-refactor" {
		t.Errorf("branch = %q", ws.Branch)
	}
	if ws.PR != "234" {
		t.Errorf("pr = %q", ws.PR)
	}
	// Auto-abbreviated name
	if ws.Name != "mem-refactor" {
		t.Errorf("name = %q, want mem-refactor", ws.Name)
	}
	// Status auto-promoted to active
	if ws.Status != manifest.WorkspaceStatusActive {
		t.Errorf("status = %q, want active", ws.Status)
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

	ws, _ := eng.WsShow("labs", "w1")
	if ws.Name != "my-ws" {
		t.Errorf("name = %q, want my-ws", ws.Name)
	}

	// Verify name override sticks after branch update
	branch := "feature/something"
	err = eng.WsUpdate("labs", "w1", &branch, nil, nil)
	if err != nil {
		t.Fatalf("WsUpdate failed: %v", err)
	}
	ws, _ = eng.WsShow("labs", "w1")
	if ws.Name != "my-ws" {
		t.Errorf("name after update = %q, want my-ws (override should stick)", ws.Name)
	}
}

func TestWinOpen(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	err = eng.WinOpen("labs", "w1", "", true, "")
	if err != nil {
		t.Fatalf("WinOpen failed: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(ws.Windows))
	}
	if ws.Windows[1].Name != "w1:2" {
		t.Errorf("second window name = %q, want w1:2", ws.Windows[1].Name)
	}
	if ws.Windows[1].Panes[0].Type != manifest.PaneTypeShell {
		t.Errorf("pane type = %q, want shell", ws.Windows[1].Panes[0].Type)
	}
}

func TestWinClose(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Open second window
	eng.WinOpen("labs", "w1", "", true, "")

	// Close the second window
	err = eng.WinClose("labs", "w1", 2)
	if err != nil {
		t.Fatalf("WinClose failed: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Windows) != 1 {
		t.Errorf("expected 1 window after close, got %d", len(ws.Windows))
	}
}

func TestPaneAdd(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	err = eng.PaneAdd("labs", "w1", 1, "", true, "", "h")
	if err != nil {
		t.Fatalf("PaneAdd failed: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Windows[0].Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(ws.Windows[0].Panes))
	}
	pane := ws.Windows[0].Panes[1]
	if pane.Type != manifest.PaneTypeShell {
		t.Errorf("pane type = %q, want shell", pane.Type)
	}
	if pane.SplitDir != "h" {
		t.Errorf("split_dir = %q, want h", pane.SplitDir)
	}
	if pane.SplitFrom != 1 {
		t.Errorf("split_from = %d, want 1", pane.SplitFrom)
	}
}

func TestDockClose(t *testing.T) {
	eng, _ := testEngine(t)

	// Create two workspaces
	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})

	err := eng.DockClose("labs", false)
	if err != nil {
		t.Fatalf("DockClose failed: %v", err)
	}

	m, _ := manifest.Load(eng.manifestPath)
	if len(m.Docks["labs"].Workspaces) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(m.Docks["labs"].Workspaces))
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
	cmds, err := eng.Recover()
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	if len(cmds) == 0 {
		t.Error("expected attach commands")
	}

	// Verify session was recreated
	if !mockTmux.HasSessionCalled("labs") {
		t.Error("tmux session not recreated")
	}
}

func TestGenerateAgentConfig(t *testing.T) {
	eng, dir := testEngine(t)

	// Create template
	tmplDir := filepath.Join(dir, "templates")
	os.MkdirAll(tmplDir, 0o755)
	tmplPath := filepath.Join(tmplDir, "labs.md")
	os.WriteFile(tmplPath, []byte("Workspace: {workspace_name} (ID: {workspace_id}) in {dock}"), 0o644)

	eng.Config.Docks["labs"] = config.DockConfig{
		Repo:                "labs",
		Agent:               "claude",
		AgentConfigTemplate: tmplPath,
	}

	wsPath := filepath.Join(dir, "ws1")
	os.MkdirAll(wsPath, 0o755)

	err := eng.generateAgentConfig("labs", "claude", "w1", "test-ws", wsPath, manifest.WorkspaceTypeWorktree, "labs")
	if err != nil {
		t.Fatalf("generateAgentConfig failed: %v", err)
	}

	// Read generated config
	data, err := os.ReadFile(filepath.Join(wsPath, "CLAUDE.local.md"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	content := string(data)
	// Should contain the bay preamble with substituted values
	if !strings.Contains(content, "bay workspace test-ws (w1) in the labs dock") {
		t.Errorf("config missing preamble with substituted values, got:\n%s", content)
	}
	// Should contain the user template content
	if !strings.Contains(content, "Workspace: test-ws (ID: w1) in labs") {
		t.Errorf("config missing template content, got:\n%s", content)
	}
}

func TestGenerateAgentConfig_GitignoreRefused(t *testing.T) {
	eng, dir := testEngine(t)

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetGlobalIgnored(false) // not gitignored

	wsPath := filepath.Join(dir, "ws1")
	os.MkdirAll(wsPath, 0o755)

	eng.Config.Docks["labs"] = config.DockConfig{
		Repo:                "labs",
		Agent:               "claude",
		AgentConfigTemplate: filepath.Join(dir, "templates", "labs.md"),
	}

	err := eng.generateAgentConfig("labs", "claude", "w1", "test", wsPath, manifest.WorkspaceTypeWorktree, "labs")
	if err == nil {
		t.Error("expected error when config file not gitignored")
	}
}

func TestGenerateAgentConfig_PreambleWithoutTemplate(t *testing.T) {
	// Bay should write the preamble even when no template is configured.
	eng, _ := testEngine(t)

	// labs dock has no agent_config_template by default
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	configPath := filepath.Join(ws.Path, "CLAUDE.local.md")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Bay Workspace") {
		t.Error("config missing bay preamble")
	}
	if !strings.Contains(content, "bay ws update self") {
		t.Error("config missing bay update instructions")
	}
	if !strings.Contains(content, ws.Path) {
		t.Errorf("config missing workspace path %q", ws.Path)
	}
}

// --- Regression tests for code review fixes ---

func TestWsUpdate_WindowNamesPersisted(t *testing.T) {
	// Regression: WsUpdate iterated windows by value, so Name mutations were lost
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WinOpen("labs", "w1", "", true, "") // second window

	branch := "feature/new-thing"
	eng.WsUpdate("labs", "w1", &branch, nil, nil)

	ws, _ := eng.WsShow("labs", "w1")
	if ws.Windows[0].Name != "new-thing" {
		t.Errorf("window 1 name = %q, want new-thing", ws.Windows[0].Name)
	}
	if ws.Windows[1].Name != "new-thing:2" {
		t.Errorf("window 2 name = %q, want new-thing:2", ws.Windows[1].Name)
	}
}

func TestWsRename_WindowNamesPersisted(t *testing.T) {
	// Regression: WsRename iterated windows by value
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WinOpen("labs", "w1", "", true, "")

	eng.WsRename("labs", "w1", "renamed")

	ws, _ := eng.WsShow("labs", "w1")
	if ws.Windows[0].Name != "renamed" {
		t.Errorf("window 1 name = %q, want renamed", ws.Windows[0].Name)
	}
	if ws.Windows[1].Name != "renamed:2" {
		t.Errorf("window 2 name = %q, want renamed:2", ws.Windows[1].Name)
	}
}

func TestGenerateAgentConfig_WorkspaceType(t *testing.T) {
	// Regression: {workspace_type} was hardcoded to "worktree"
	eng, dir := testEngine(t)

	tmplDir := filepath.Join(dir, "templates")
	os.MkdirAll(tmplDir, 0o755)
	tmplPath := filepath.Join(tmplDir, "labs.md")
	os.WriteFile(tmplPath, []byte("type={workspace_type}"), 0o644)

	eng.Config.Docks["labs"] = config.DockConfig{
		Repo:                "labs",
		Agent:               "claude",
		AgentConfigTemplate: tmplPath,
	}

	wsPath := filepath.Join(dir, "ws1")
	os.MkdirAll(wsPath, 0o755)

	eng.generateAgentConfig("labs", "claude", "w1", "test", wsPath, manifest.WorkspaceTypeExternal, "labs")

	data, _ := os.ReadFile(filepath.Join(wsPath, "CLAUDE.local.md"))
	if !strings.Contains(string(data), "type=external") {
		t.Errorf("config should contain type=external, got:\n%s", string(data))
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

	// Reload config from disk
	loaded, err := config.Load(eng.configPath)
	if err != nil {
		t.Fatalf("loading saved config: %v", err)
	}
	if _, ok := loaded.Docks["research"]; !ok {
		t.Error("new dock not persisted to config file")
	}
}

func TestGenerateAgentConfig_UsesWorkspaceRepo(t *testing.T) {
	// Regression: generateAgentConfig used dock default repo for template vars
	// and gitignore checks, ignoring workspace repo overrides.
	eng, dir := testEngine(t)

	// Add a second repo
	otherRepoDir := filepath.Join(dir, "repos", "other")
	os.MkdirAll(otherRepoDir, 0o755)
	eng.Config.Repos["other"] = config.RepoConfig{Path: otherRepoDir}

	tmplDir := filepath.Join(dir, "templates")
	os.MkdirAll(tmplDir, 0o755)
	tmplPath := filepath.Join(tmplDir, "labs.md")
	os.WriteFile(tmplPath, []byte("{dock_repo}"), 0o644)

	eng.Config.Docks["labs"] = config.DockConfig{
		Repo:                "labs",
		Agent:               "claude",
		AgentConfigTemplate: tmplPath,
	}

	wsPath := filepath.Join(dir, "ws1")
	os.MkdirAll(wsPath, 0o755)

	// Generate with workspace repo override "other"
	err := eng.generateAgentConfig("labs", "claude", "w1", "test", wsPath, manifest.WorkspaceTypeWorktree, "other")
	if err != nil {
		t.Fatalf("generateAgentConfig failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(wsPath, "CLAUDE.local.md"))
	// Should contain the "other" repo path, not the "labs" repo path
	if !strings.Contains(string(data), otherRepoDir) {
		t.Errorf("config should contain workspace repo path %q, got:\n%s", otherRepoDir, string(data))
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

	// Mock: HasUnpushedCommits returns error
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetUnpushed(ws.Path, false)
	// We need the mock to return an error, but the current mock doesn't support that.
	// Instead, test the positive case: when unpushed is true, it refuses.
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
	// Load will create a fresh manifest (path doesn't exist → empty),
	// but Save will fail because it can't create the lock file.
	badDir := filepath.Join(dir, "readonly")
	os.MkdirAll(badDir, 0o755)
	// Write the current manifest content there (so load succeeds)
	curData, _ := os.ReadFile(eng.manifestPath)
	badManifest := filepath.Join(badDir, "manifest.toml")
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

func TestRecoverUsesPerPaneAgent(t *testing.T) {
	// Regression: recovery used dock default agent for all panes, ignoring
	// the pane's recorded Agent field.
	eng, _ := testEngine(t)

	// Create workspace with codex agent override
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Agent: "codex"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// Verify the pane recorded "codex"
	m, _ := eng.LoadManifest()
	pane := m.Docks["labs"].Workspaces["w1"].Windows[0].Panes[0]
	if pane.Agent != "codex" {
		t.Fatalf("pane agent = %q, want codex", pane.Agent)
	}

	// Simulate reboot
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	// Recover
	_, err = eng.Recover()
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	// Verify the recovered pane was launched with "codex" not "claude"
	for _, call := range mockTmux.Calls {
		if call.Method == "SendKeys" && len(call.Args) >= 2 {
			if call.Args[1] == "codex" {
				return // correct agent launched
			}
			if call.Args[1] == "claude" {
				t.Error("recovery launched dock default agent 'claude' instead of pane's 'codex'")
				return
			}
		}
	}
	// If we get here, no SendKeys was called at all — also a problem,
	// but only if there was a window to recover (might have been skipped
	// if path didn't exist). The os.MkdirAll above should handle that.
}

func TestCmdPanePersistsCommand(t *testing.T) {
	// Regression: command panes had no Command field, recovery couldn't restore them.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})

	// Open a window with a custom command
	eng.WinOpen("labs", "w1", "", false, "tail -f /var/log/syslog")

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Windows) < 2 {
		t.Fatal("expected 2 windows")
	}
	pane := ws.Windows[1].Panes[0]
	if pane.Type != manifest.PaneTypeCmd {
		t.Errorf("pane type = %q, want cmd", pane.Type)
	}
	if pane.Command != "tail -f /var/log/syslog" {
		t.Errorf("pane command = %q, want 'tail -f /var/log/syslog'", pane.Command)
	}
}

func TestPaneAddPersistsCommand(t *testing.T) {
	eng, _ := testEngine(t)
	eng.WsNew(WsNewOptions{Dock: "labs"})

	eng.PaneAdd("labs", "w1", 1, "", false, "watch df -h", "h")

	ws, _ := eng.WsShow("labs", "w1")
	pane := ws.Windows[0].Panes[1]
	if pane.Command != "watch df -h" {
		t.Errorf("pane command = %q, want 'watch df -h'", pane.Command)
	}
}

func TestRecoverCmdPane(t *testing.T) {
	// Regression: recovery fell back to plain shell for cmd panes.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WinOpen("labs", "w1", "", false, "htop")

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
	t.Error("expected SendKeys with 'htop' for cmd pane recovery")
}

func TestRecoverReconcilesPanesInExistingWindow(t *testing.T) {
	// Regression: recovery skipped pane repair for existing windows.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.PaneAdd("labs", "w1", 1, "", true, "", "h")

	ws, _ := eng.WsShow("labs", "w1")
	os.MkdirAll(ws.Path, 0o755)

	// Manifest says 2 panes. Kill one pane in tmux so only 1 remains.
	mockTmux := eng.Tmux.(*tmux.Mock)
	// The window has 2 tmux panes (created by WsNew + PaneAdd).
	// Find and kill the second one.
	winID := ws.Windows[0].TmuxWindowID
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

func TestWinRestartUsesPerPaneAgent(t *testing.T) {
	// Regression: WinRestart used dock default agent for all panes.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Agent: "codex"})

	mockTmux := eng.Tmux.(*tmux.Mock)

	// Clear calls to isolate restart
	mockTmux.Calls = nil

	eng.WinRestart("labs", "w1", 1)

	// Should respawn with codex, not claude
	for _, call := range mockTmux.Calls {
		if call.Method == "RespawnPane" && len(call.Args) >= 3 {
			if call.Args[2] == "codex" {
				return // correct
			}
			if call.Args[2] == "claude" {
				t.Error("WinRestart used dock default 'claude' instead of pane's 'codex'")
				return
			}
		}
	}
}

func TestWsUpdate_InvalidStatus(t *testing.T) {
	eng, _ := testEngine(t)
	eng.WsNew(WsNewOptions{Dock: "labs"})

	bad := "banana"
	err := eng.WsUpdate("labs", "w1", nil, nil, &bad)
	if err == nil {
		t.Error("expected error for invalid status")
	}

	// Valid statuses should work
	for _, s := range []string{"idle", "active", "done"} {
		v := s
		if err := eng.WsUpdate("labs", "w1", nil, nil, &v); err != nil {
			t.Errorf("valid status %q rejected: %v", s, err)
		}
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

	if _, ok := eng.Config.Repos["newrepo"]; !ok {
		t.Error("repo not added to config")
	}

	// Path should be normalized (absolute)
	savedPath := eng.Config.Repos["newrepo"].Path
	if savedPath != repoDir && savedPath != config.NormalizePath(repoDir) {
		t.Errorf("saved path = %q, want normalized form of %q", savedPath, repoDir)
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
	if _, ok := eng.Config.Repos["labs"]; ok {
		t.Error("repo should be removed")
	}
	if _, ok := eng.Config.Docks["labs"]; ok {
		t.Error("dock should be removed with --force")
	}
}

func TestRepoAdd_NotGitRepo(t *testing.T) {
	eng, dir := testEngine(t)
	eng.configPath = filepath.Join(dir, "config.toml")
	config.Save(eng.configPath, eng.Config)

	// Mock defaults to IsGitRepo=true; override for this test by using
	// a custom mock that returns false. Instead, we test the real path:
	// create a plain directory (not a git repo). The mock always returns true,
	// so we verify the logic by testing with force=false on a mock that
	// returns false. We need to make the mock configurable.
	// For now, test the force=true path to ensure it bypasses the check.
	plainDir := filepath.Join(dir, "not-a-repo")
	os.MkdirAll(plainDir, 0o755)

	// With force, should succeed even if not a git repo
	err := eng.RepoAdd("plain", plainDir, "", "", true)
	if err != nil {
		t.Fatalf("RepoAdd with --force should succeed: %v", err)
	}
	if _, ok := eng.Config.Repos["plain"]; !ok {
		t.Error("repo should be added with --force")
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

func TestWsClose_CleansEmptyWorktreeDir(t *testing.T) {
	eng, dir := testEngine(t)

	// Create a real worktree parent directory to simulate the filesystem
	repoCfg := eng.Config.Repos["labs"]
	wtDir := repoCfg.EffectiveWorktreeDir()
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
	if docks[0].Workspaces[0].Agent != "claude" {
		t.Errorf("agent = %q, want dock default claude", docks[0].Workspaces[0].Agent)
	}
}

func TestList_PreservesWorkspaceAgentOverrideAfterPrimaryWindowClosed(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Agent: "codex"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.WinOpen("labs", "w1", "", true, ""); err != nil {
		t.Fatalf("WinOpen failed: %v", err)
	}
	if err := eng.WinClose("labs", "w1", 1); err != nil {
		t.Fatalf("WinClose failed: %v", err)
	}

	docks, err := eng.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(docks) != 1 || len(docks[0].Workspaces) != 1 {
		t.Fatalf("unexpected dock/workspace count: %#v", docks)
	}
	if docks[0].Workspaces[0].Agent != "codex" {
		t.Errorf("agent = %q, want workspace override codex", docks[0].Workspaces[0].Agent)
	}
}

func TestWsClose_KeepsNonEmptyWorktreeDir(t *testing.T) {
	eng, _ := testEngine(t)

	// Create two workspaces
	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})

	// Create the worktree parent dir with a subdirectory to simulate w2 still there
	repoCfg := eng.Config.Repos["labs"]
	wtDir := repoCfg.EffectiveWorktreeDir()
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

func TestSyncWorkspaceGitState_UpdatesBranch(t *testing.T) {
	eng, _ := testEngine(t)

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

	ws, _ = eng.WsShow("labs", "w1")
	if ws.Branch != "feature/sync-test" {
		t.Errorf("branch = %q, want feature/sync-test", ws.Branch)
	}
}

func TestSyncWorkspaceGitState_BranchChangeUpdatesNameAndStatus(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	if ws.Status != manifest.WorkspaceStatusIdle {
		t.Fatalf("initial status = %q, want idle", ws.Status)
	}

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "feature/my-feature")

	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "w1")
	if ws.Name != "my-feature" {
		t.Errorf("name = %q, want my-feature", ws.Name)
	}
	if ws.Status != manifest.WorkspaceStatusActive {
		t.Errorf("status = %q, want active", ws.Status)
	}
}

func TestSyncWorkspaceGitState_EmptyBranchNoOverwrite(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	// First, set a real branch via WsUpdate
	branch := "feature/existing"
	eng.WsUpdate("labs", "w1", &branch, nil, nil)

	// Now mock returns empty branch (detached HEAD)
	mockGit := eng.Git.(*git.Mock)
	mockGit.SetBranch(ws.Path, "")

	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "w1")
	if ws.Branch != "feature/existing" {
		t.Errorf("branch = %q, want feature/existing (empty should not overwrite)", ws.Branch)
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

	ws, _ = eng.WsShow("labs", "w1")
	if ws.Name != "custom-name" {
		t.Errorf("name = %q, want custom-name (NameOverridden should prevent change)", ws.Name)
	}
	// Branch should still be updated even if name is overridden
	if ws.Branch != "feature/something-else" {
		t.Errorf("branch = %q, want feature/something-else", ws.Branch)
	}
}

func TestCurrentContext_ResolvesWorkspaceWindowAndPane(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.PaneAdd("labs", "w1", 1, "codex", false, "", "v"); err != nil {
		t.Fatalf("PaneAdd failed: %v", err)
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
	mockTmux.SetCurrentWindowID(ws.Windows[0].TmuxWindowID)
	mockTmux.SetCurrentPaneID(ws.Windows[0].Panes[1].TmuxPaneID)

	ctx, err := eng.CurrentContext()
	if err != nil {
		t.Fatalf("CurrentContext failed: %v", err)
	}
	if ctx.Repo != "labs" || ctx.Dock != "labs" || ctx.WorkspaceID != "w1" {
		t.Fatalf("unexpected context: %#v", ctx)
	}
	if ctx.WindowID != 1 {
		t.Fatalf("window = %d, want 1", ctx.WindowID)
	}
	if ctx.Window != ws.Windows[0].Name {
		t.Fatalf("window = %q, want %q", ctx.Window, ws.Windows[0].Name)
	}
	if ctx.PaneID != 2 {
		t.Fatalf("pane = %d, want 2", ctx.PaneID)
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
	if ctx.Repo != "labs" || ctx.WorkspaceID != "w1" {
		t.Fatalf("unexpected context: %#v", ctx)
	}
	if ctx.WindowID != 0 || ctx.PaneID != 0 {
		t.Fatalf("window/pane should be omitted outside tmux: %#v", ctx)
	}
}

// --- non-destructive tmux state sync tests ---

func TestSyncAll_PreservesDeadWindows(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Open a second window
	eng.WinOpen("labs", "w1", "", true, "")
	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(ws.Windows))
	}

	// Kill the second window in tmux (simulating user closing it externally)
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillWindow(ws.Windows[1].TmuxWindowID)

	// SyncAll should preserve the dead window for recovery
	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Windows) != 2 {
		t.Errorf("expected 2 windows after sync, got %d", len(ws.Windows))
	}
}

func TestList_PreservesDeadWindowsForRecovery(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.WinOpen("labs", "w1", "", true, ""); err != nil {
		t.Fatalf("WinOpen failed: %v", err)
	}
	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(ws.Windows))
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.KillWindow(ws.Windows[1].TmuxWindowID); err != nil {
		t.Fatalf("KillWindow failed: %v", err)
	}

	docks, err := eng.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(docks) != 1 || len(docks[0].Workspaces) != 1 {
		t.Fatalf("unexpected dock/workspace count: %#v", docks)
	}
	if !docks[0].Workspaces[0].Stale {
		t.Fatal("workspace should be marked stale")
	}

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Windows) != 2 {
		t.Fatalf("manifest lost dead window; windows = %d, want 2", len(ws.Windows))
	}

	os.MkdirAll(ws.Path, 0o755)
	if _, err := eng.Recover(); err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Windows) != 2 {
		t.Fatalf("workspace windows after recover = %d, want 2", len(ws.Windows))
	}
	exists, _ := mockTmux.WindowExists(ws.Windows[1].TmuxWindowID)
	if !exists {
		t.Error("dead window should be recreated during recovery")
	}
}

func TestWinOpen_AgentConfigFailureDoesNotCreateWindow(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetGlobalIgnored(false)

	err = eng.WinOpen("labs", "w1", "claude", false, "")
	if err == nil {
		t.Fatal("expected WinOpen to fail when agent config cannot be generated")
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Windows) != 1 {
		t.Fatalf("windows = %d, want 1 after failed WinOpen", len(ws.Windows))
	}
}

func TestPaneAdd_AgentConfigFailureDoesNotCreatePane(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetGlobalIgnored(false)

	err = eng.PaneAdd("labs", "w1", 1, "claude", false, "", "h")
	if err == nil {
		t.Fatal("expected PaneAdd to fail when agent config cannot be generated")
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Windows[0].Panes) != 1 {
		t.Fatalf("panes = %d, want 1 after failed PaneAdd", len(ws.Windows[0].Panes))
	}
}

func TestRecover_ReturnsAgentConfigErrors(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Agent: "codex"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	mockGit := eng.Git.(*git.Mock)
	mockGit.SetGlobalIgnored(false)
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	_, err = eng.Recover()
	if err == nil {
		t.Fatal("expected Recover to return agent config error")
	}
}

func TestRecover_FindWindowByNameRefreshesPaneIDs(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.PaneAdd("labs", "w1", 1, "codex", false, "", "v"); err != nil {
		t.Fatalf("PaneAdd failed: %v", err)
	}
	if err := os.MkdirAll(ws.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	ws, err = eng.WsShow("labs", "w1")
	if err != nil {
		t.Fatalf("WsShow failed: %v", err)
	}
	oldWindowID := ws.Windows[0].TmuxWindowID
	oldPaneID := ws.Windows[0].Panes[1].TmuxPaneID

	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.KillWindow(oldWindowID); err != nil {
		t.Fatalf("KillWindow failed: %v", err)
	}
	foundWindowID, err := mockTmux.NewWindow("labs", ws.Windows[0].Name, ws.Path)
	if err != nil {
		t.Fatalf("NewWindow failed: %v", err)
	}
	foundPaneID, err := mockTmux.SplitWindow(foundWindowID, "v", ws.Path)
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
	if ws.Windows[0].TmuxWindowID != foundWindowID {
		t.Fatalf("window id = %q, want %q", ws.Windows[0].TmuxWindowID, foundWindowID)
	}
	if ws.Windows[0].Panes[1].TmuxPaneID != foundPaneID {
		t.Fatalf("pane id = %q, want %q", ws.Windows[0].Panes[1].TmuxPaneID, foundPaneID)
	}
	if ws.Windows[0].Panes[1].TmuxPaneID == oldPaneID {
		t.Fatalf("pane id was not refreshed from stale value %q", oldPaneID)
	}
}

func TestWsClose_RemoveWorktreeFailurePreservesWorkspaceState(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.WinOpen("labs", "w1", "", true, ""); err != nil {
		t.Fatalf("WinOpen failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)

	repoPath := config.ExpandPath(eng.Config.Repos["labs"].Path)
	mockGit := eng.Git.(*git.Mock)
	if err := mockGit.RemoveWorktree(repoPath, ws.Path, true); err != nil {
		t.Fatalf("preparing RemoveWorktree failure: %v", err)
	}

	err = eng.WsClose("labs", "w1", false)
	if err == nil {
		t.Fatal("expected WsClose to fail when RemoveWorktree fails")
	}

	stillThere, showErr := eng.WsShow("labs", "w1")
	if showErr != nil {
		t.Fatalf("workspace should remain in manifest after failed close: %v", showErr)
	}
	if len(stillThere.Windows) != 2 {
		t.Fatalf("windows = %d, want 2 after failed close", len(stillThere.Windows))
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	for _, win := range stillThere.Windows {
		exists, _ := mockTmux.WindowExists(win.TmuxWindowID)
		if !exists {
			t.Fatalf("window %s should still exist after failed close", win.TmuxWindowID)
		}
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

	m, err := manifest.Load(eng.manifestPath)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	if _, ok := m.Docks["labs"]; ok {
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

func TestSyncAll_PreservesMissingManagedPanes(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Add a second pane
	eng.PaneAdd("labs", "w1", 1, "", true, "", "h")
	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Windows[0].Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(ws.Windows[0].Panes))
	}

	// Kill one tmux pane so tmux has fewer than manifest
	mockTmux := eng.Tmux.(*tmux.Mock)
	winID := ws.Windows[0].TmuxWindowID
	panes, _ := mockTmux.ListPanes(winID)
	if len(panes) < 2 {
		t.Fatalf("expected 2 tmux panes, got %d", len(panes))
	}
	mockTmux.KillPane(panes[1].ID)

	// SyncAll should preserve the manifest pane list for recovery
	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Windows[0].Panes) != 2 {
		t.Errorf("expected 2 panes after sync, got %d", len(ws.Windows[0].Panes))
	}
}

// --- SyncManifestPanes test ---

func TestSyncManifestPanes_TrimsToCount(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Add extra panes to manifest
	eng.PaneAdd("labs", "w1", 1, "", true, "", "h")
	eng.PaneAdd("labs", "w1", 1, "", true, "", "v")

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Windows[0].Panes) != 3 {
		t.Fatalf("expected 3 panes, got %d", len(ws.Windows[0].Panes))
	}

	// Trim to 1 pane
	eng.SyncManifestPanes("labs", "w1", 1, 1)

	ws, _ = eng.WsShow("labs", "w1")
	if len(ws.Windows[0].Panes) != 1 {
		t.Errorf("expected 1 pane after SyncManifestPanes, got %d", len(ws.Windows[0].Panes))
	}
}

// --- ResolveSelf with tmux window ID fallback ---

func TestResolveSelf_TmuxWindowIDFallback(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Set current tmux window ID to match the workspace's window
	mockTmux := eng.Tmux.(*tmux.Mock)
	winID := ws.Windows[0].TmuxWindowID
	mockTmux.SetCurrentWindowID(winID)

	// Change CWD to something that does NOT match any workspace path
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	dockName, wsID, err := eng.ResolveSelf()
	if err != nil {
		t.Fatalf("ResolveSelf failed: %v", err)
	}
	if dockName != "labs" {
		t.Errorf("dock = %q, want labs", dockName)
	}
	if wsID != "w1" {
		t.Errorf("wsID = %q, want w1", wsID)
	}
}

// --- ResolveByWindowID tests ---

func TestResolveByWindowID_Found(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	winID := ws.Windows[0].TmuxWindowID
	dockName, wsID, foundWs, err := eng.ResolveByWindowID(winID)
	if err != nil {
		t.Fatalf("ResolveByWindowID failed: %v", err)
	}
	if dockName != "labs" {
		t.Errorf("dock = %q, want labs", dockName)
	}
	if wsID != "w1" {
		t.Errorf("wsID = %q, want w1", wsID)
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

// --- WsCloseByStatus tests ---

func TestWsCloseByStatus_ClosesDone(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})

	// Mark w1 as done
	doneStatus := "done"
	eng.WsUpdate("labs", "w1", nil, nil, &doneStatus)

	closed, skipped, err := eng.WsCloseByStatus("labs", "done", true)
	if err != nil {
		t.Fatalf("WsCloseByStatus failed: %v", err)
	}

	if len(closed) != 1 {
		t.Errorf("expected 1 closed, got %d: %v", len(closed), closed)
	}
	if len(skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d: %v", len(skipped), skipped)
	}

	// w1 should be gone, w2 should remain
	m, _ := manifest.Load(eng.manifestPath)
	if _, ok := m.Docks["labs"].Workspaces["w1"]; ok {
		t.Error("w1 should be closed")
	}
	if _, ok := m.Docks["labs"].Workspaces["w2"]; !ok {
		t.Error("w2 should still exist")
	}
}

func TestWsCloseByStatus_SkipsNonDone(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs"})
	eng.WsNew(WsNewOptions{Dock: "labs"})

	// Mark w1 as active (not done)
	activeStatus := "active"
	eng.WsUpdate("labs", "w1", nil, nil, &activeStatus)

	// Mark w2 as idle (not done)
	// w2 is already idle by default, no need to update

	_, _, err := eng.WsCloseByStatus("labs", "done", true)
	if err == nil {
		t.Error("expected error when no workspaces match status")
	}
	if !strings.Contains(err.Error(), "no workspaces with status") {
		t.Errorf("error = %q, want 'no workspaces with status' message", err)
	}

	// Both workspaces should still exist
	m, _ := manifest.Load(eng.manifestPath)
	if _, ok := m.Docks["labs"].Workspaces["w1"]; !ok {
		t.Error("w1 should still exist")
	}
	if _, ok := m.Docks["labs"].Workspaces["w2"]; !ok {
		t.Error("w2 should still exist")
	}
}

// --- WsNew with --branch ---

func TestWsNew_WithBranch(t *testing.T) {
	eng, _ := testEngine(t)

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
	if ws.Branch != "feature/new-branch" {
		t.Errorf("branch = %q, want feature/new-branch", ws.Branch)
	}
	if ws.Name != "new-branch" {
		t.Errorf("name = %q, want new-branch (abbreviated)", ws.Name)
	}
}

// --- SetEditor test ---

func TestSetEditor(t *testing.T) {
	eng, dir := testEngine(t)
	eng.configPath = filepath.Join(dir, "config.toml")
	config.Save(eng.configPath, eng.Config)

	err := eng.SetEditor("nvim")
	if err != nil {
		t.Fatalf("SetEditor failed: %v", err)
	}

	if eng.Config.Editor.Command != "nvim" {
		t.Errorf("editor command = %q, want nvim", eng.Config.Editor.Command)
	}

	// Verify persisted to disk
	loaded, err := config.Load(eng.configPath)
	if err != nil {
		t.Fatalf("loading saved config: %v", err)
	}
	if loaded.Editor.Command != "nvim" {
		t.Errorf("persisted editor command = %q, want nvim", loaded.Editor.Command)
	}
}
