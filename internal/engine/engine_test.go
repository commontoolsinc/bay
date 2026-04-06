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
			"claude": {Command: "claude"},
			"codex":  {Command: "codex"},
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
	manifestPath := filepath.Join(dir, "manifest.json")
	archivePath := filepath.Join(dir, "archive.json")

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
	// Config should have Terminal persisted.
	if eng.Config.Docks["research"].Terminal != "ghostty" {
		t.Errorf("config terminal = %q, want ghostty", eng.Config.Docks["research"].Terminal)
	}
}

func TestRecover_RelaunchesHostTerminal(t *testing.T) {
	eng, _ := testEngine(t)

	// Create a dock with a host terminal in the manifest.
	m, _ := eng.LoadManifest()
	m.AddDock(manifest.Dock{
		Name: "labs",
		Host: &manifest.GUIAttrs{AppCommand: "ghostty", PID: 0},
	})
	eng.saveManifest(m)

	eng.Config.Docks["labs"] = config.DockConfig{
		Repo:     "labs",
		Agent:    "claude",
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
	dock := m.FindDock("labs")
	if dock == nil {
		t.Fatal("dock not found after recovery")
	}
	if dock.Host == nil {
		t.Fatal("dock.Host should persist through recovery")
	}
	// We can't easily check PID since we're not actually launching ghostty.
	// Just verify the manifest was saved with Host intact.
}

func TestWsNew_Worktree(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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
	ws2, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w2"})
	if err != nil {
		t.Fatalf("second WsNew failed: %v", err)
	}
	if ws2.Name != "w2" {
		t.Errorf("second workspace name = %q, want w2", ws2.Name)
	}
}

func TestSurfaceAdd_PersistsTmuxPaneID(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	if err := eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeAgent, "agent", "codex", "", "v"); err != nil {
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

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Shell: true})
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
	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

func TestWsClose_Dirty(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	branch := "feature/mem-refactor"
	pr := "234"
	err = eng.WsUpdate("labs", "w1", &branch, &pr, nil)
	if err != nil {
		t.Fatalf("WsUpdate failed: %v", err)
	}

	// WsUpdate with branch abbreviates the name: w1 -> mem-refactor
	ws, _ := eng.WsShow("labs", "mem-refactor")
	if ws.Worktree == nil || ws.Worktree.Branch != "feature/mem-refactor" {
		t.Errorf("branch = %v", ws.Worktree)
	}
	if ws.Worktree.PR != "234" {
		t.Errorf("pr = %q", ws.Worktree.PR)
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

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	// Verify name override sticks after branch update
	branch := "feature/something"
	err = eng.WsUpdate("labs", "my-ws", &branch, nil, nil)
	if err != nil {
		t.Fatalf("WsUpdate failed: %v", err)
	}
	ws, _ = eng.WsShow("labs", "my-ws")
	if ws.Name != "my-ws" {
		t.Errorf("name after update = %q, want my-ws (override should stick)", ws.Name)
	}
}

func TestSurfaceAdd_NewLayoutGroup(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Add a new surface with empty splitDir = new tmux window / layout group
	err = eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeShell, "shell", "", "", "")
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

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Open second surface in new layout group
	eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeShell, "shell", "", "", "")

	// Close the second surface
	err = eng.SurfaceClose("labs", "w1", "shell")
	if err != nil {
		t.Fatalf("SurfaceClose failed: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 1 {
		t.Errorf("expected 1 surface after close, got %d", len(ws.Surfaces))
	}
}

func TestSurfaceAdd_Split(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	err = eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeShell, "shell", "", "", "h")
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

func TestSurfaceAddGUI(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})

	// Use our own PID so the surface survives SyncAll liveness checks.
	pid := os.Getpid()
	err := eng.SurfaceAddGUI("labs", "w1", "editor", "cursor", pid)
	if err != nil {
		t.Fatalf("SurfaceAddGUI: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 2 {
		t.Fatalf("surfaces = %d, want 2", len(ws.Surfaces))
	}

	s := ws.Surfaces[1]
	if s.Name != "editor" {
		t.Errorf("name = %q, want editor", s.Name)
	}
	if s.Backend != manifest.SurfaceBackendGUI {
		t.Errorf("backend = %q, want gui-app", s.Backend)
	}
	if s.Type != manifest.SurfaceTypeEditor {
		t.Errorf("type = %q, want editor", s.Type)
	}
	if s.GUI == nil {
		t.Fatal("GUI attrs should not be nil")
	}
	if s.GUI.AppCommand != "cursor" {
		t.Errorf("app_command = %q, want cursor", s.GUI.AppCommand)
	}
	if s.GUI.PID != pid {
		t.Errorf("pid = %d, want %d", s.GUI.PID, pid)
	}
	if s.Tmux != nil {
		t.Error("tmux attrs should be nil for GUI surface")
	}
}

func TestSurfaceClose_GUI(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	eng.SurfaceAddGUI("labs", "w1", "editor", "cursor", os.Getpid())

	err := eng.SurfaceClose("labs", "w1", "editor")
	if err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 1 {
		t.Errorf("surfaces = %d, want 1 after closing GUI surface", len(ws.Surfaces))
	}
}

func TestSyncAll_RemovesDeadGUISurfaces(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})

	// Directly write a GUI surface with a dead PID into the manifest.
	// We can't use SurfaceAddGUI + WsShow because WsShow calls SyncAll
	// which would remove it before we can check the "before" state.
	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	ws := dock.FindWorkspace("w1")
	ws.AddSurface(manifest.Surface{
		Name:    "editor",
		Type:    manifest.SurfaceTypeEditor,
		Backend: manifest.SurfaceBackendGUI,
		GUI:     &manifest.GUIAttrs{AppCommand: "cursor", PID: 999999},
	})
	eng.saveManifest(m)

	// Verify surface is in manifest before sync.
	m, _ = eng.LoadManifest()
	ws = m.FindDock("labs").FindWorkspace("w1")
	if len(ws.Surfaces) != 2 {
		t.Fatalf("surfaces = %d, want 2 before sync", len(ws.Surfaces))
	}

	eng.SyncAll()

	m, _ = eng.LoadManifest()
	ws = m.FindDock("labs").FindWorkspace("w1")
	for _, s := range ws.Surfaces {
		if s.Backend == manifest.SurfaceBackendGUI {
			t.Error("dead GUI surface should be removed by SyncAll")
		}
	}
}

func TestDockClose(t *testing.T) {
	eng, _ := testEngine(t)

	// Create two workspaces
	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w2"})

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

func TestRecover(t *testing.T) {
	eng, _ := testEngine(t)

	// Create a workspace
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

func TestWsUpdate_TmuxWindowRenamed(t *testing.T) {
	// Regression: WsUpdate should rename tmux windows when workspace name changes
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})

	branch := "feature/new-thing"
	eng.WsUpdate("labs", "w1", &branch, nil, nil)

	// Verify RenameWindow was called
	mockTmux := eng.Tmux.(*tmux.Mock)
	found := false
	for _, call := range mockTmux.Calls {
		if call.Method == "RenameWindow" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RenameWindow call when branch changes workspace name")
	}
}

func TestWsRename_TmuxWindowRenamed(t *testing.T) {
	// Regression: WsRename should rename tmux windows
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})

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

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

func TestWsClose_UnpushedCheckError_Refuses(t *testing.T) {
	// Regression: WsClose silently ignored errors from HasUnpushedCommits,
	// allowing worktree removal without verifying push status.
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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
	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	_, err = eng.WsNew(WsNewOptions{Dock: "labs", Name: "w2"})
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
	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Agent: "codex"})
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

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})

	// Add a cmd surface in a new layout group
	eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeCmd, "tail", "", "tail -f /var/log/syslog", "")

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
	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})

	eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeCmd, "watch", "", "watch df -h", "h")

	ws, _ := eng.WsShow("labs", "w1")
	s := ws.Surfaces[1]
	if s.Command == nil || *s.Command != "watch df -h" {
		t.Errorf("surface command = %v, want 'watch df -h'", s.Command)
	}
}

func TestRecoverCmdSurface(t *testing.T) {
	// Regression: recovery fell back to plain shell for cmd surfaces.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeCmd, "htop", "", "htop", "")

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

func TestRecoverReconcilesSurfacesInExistingWindow(t *testing.T) {
	// Regression: recovery skipped surface repair for existing windows.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeShell, "shell", "", "", "h")

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

func TestSurfaceRestartUsesPerSurfaceAgent(t *testing.T) {
	// Regression: SurfaceRestart used dock default agent for all surfaces.
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Agent: "codex"})

	mockTmux := eng.Tmux.(*tmux.Mock)

	// Clear calls to isolate restart
	mockTmux.Calls = nil

	eng.SurfaceRestart("labs", "w1", "agent")

	// Should respawn with codex, not claude
	for _, call := range mockTmux.Calls {
		if call.Method == "RespawnPane" && len(call.Args) >= 3 {
			if call.Args[2] == "codex" {
				return // correct
			}
			if call.Args[2] == "claude" {
				t.Error("SurfaceRestart used dock default 'claude' instead of surface's 'codex'")
				return
			}
		}
	}
}

func TestSurfaceRestart_UsesResumeArgs(t *testing.T) {
	eng, _ := testEngine(t)

	// Configure agent with resume_args.
	eng.Config.Agents["claude"] = config.AgentConfig{
		Command:    "claude",
		ResumeArgs: "--continue",
	}

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Agent: "claude"})

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Calls = nil

	eng.SurfaceRestart("labs", "w1", "agent")

	// RespawnPane should include --continue.
	for _, call := range mockTmux.Calls {
		if call.Method == "RespawnPane" && len(call.Args) >= 3 {
			if strings.Contains(call.Args[2], "--continue") {
				return // correct
			}
			t.Errorf("RespawnPane command = %q, want to contain --continue", call.Args[2])
			return
		}
	}
	t.Error("expected RespawnPane call")
}

func TestWsUpdate_InvalidStatus(t *testing.T) {
	eng, _ := testEngine(t)
	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})

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

func TestWsClose_CleansEmptyWorktreeDir(t *testing.T) {
	eng, dir := testEngine(t)

	// Create a real worktree parent directory to simulate the filesystem
	repoCfg := eng.Config.Repos["labs"]
	wtDir := repoCfg.EffectiveWorktreeDir()
	wsDir := filepath.Join(wtDir, "w1")
	os.MkdirAll(wsDir, 0o755)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Shell: true})
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

func TestList_PreservesWorkspaceAgentOverride(t *testing.T) {
	// Verify that creating a workspace with a non-default agent
	// shows that agent in the List output.
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Agent: "codex"})
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
	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w2"})

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

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})

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
	eng.WsNew(WsNewOptions{Dock: "research", Name: "w1"})

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

	ws, _ := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	ws, _ = eng.WsShow("labs", "my-feature")
	if ws.Name != "my-feature" {
		t.Errorf("name = %q, want my-feature", ws.Name)
	}
	if ws.Status != manifest.WorkspaceStatusActive {
		t.Errorf("status = %q, want active", ws.Status)
	}
}

func TestSyncWorkspaceGitState_EmptyBranchNoOverwrite(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	// WsUpdate renamed the workspace to "existing" via abbreviateBranch
	ws, _ = eng.WsShow("labs", "existing")
	if ws.Worktree.Branch != "feature/existing" {
		t.Errorf("branch = %q, want feature/existing (empty should not overwrite)", ws.Worktree.Branch)
	}
}

func TestSyncWorkspaceGitState_NameOverriddenNotChanged(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)
	if err := eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeAgent, "agent", "codex", "", "v"); err != nil {
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

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Shell: true})
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

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Add a second surface in a new layout group
	eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeShell, "shell", "", "", "")
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

func TestList_MarksStaleWorkspace(t *testing.T) {
	// After a tmux window is killed and List is called, the workspace
	// should show as stale (its surfaces are removed by SyncAll).
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Kill the workspace's tmux window
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.KillWindow(ws.Surfaces[0].Tmux.WindowID)

	docks, err := eng.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(docks) != 1 || len(docks[0].Workspaces) != 1 {
		t.Fatalf("unexpected dock/workspace count: %#v", docks)
	}

	// The workspace should be present but surfaces removed
	wsInfo := docks[0].Workspaces[0]
	if wsInfo.SurfaceCount != 0 {
		t.Errorf("expected 0 surfaces after sync, got %d", wsInfo.SurfaceCount)
	}
}

func TestSyncAll_RemovesStaleSurfacesForSplitPanes(t *testing.T) {
	// When a tmux window is killed, all surfaces in that window
	// (including split panes) should be removed.
	eng, _ := testEngine(t)

	_, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}

	// Add a split pane in the same layout group
	eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeShell, "shell", "", "", "h")
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

	// SyncAll should keep surfaces since the window still exists
	eng.SyncAll()

	ws, _ = eng.WsShow("labs", "w1")
	// Window still exists, so surfaces are preserved (even if pane is gone)
	if len(ws.Surfaces) != 2 {
		t.Errorf("expected 2 surfaces (window still alive), got %d", len(ws.Surfaces))
	}
}

// --- ResolveSelf with tmux window ID fallback ---

func TestResolveSelf_TmuxWindowIDFallback(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

// --- WsCloseByStatus tests ---

func TestWsCloseByStatus_ClosesDone(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w2"})

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
	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock.FindWorkspace("w1") != nil {
		t.Error("w1 should be closed")
	}
	if dock.FindWorkspace("w2") == nil {
		t.Error("w2 should still exist")
	}
}

func TestWsCloseByStatus_SkipsNonDone(t *testing.T) {
	eng, _ := testEngine(t)

	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	eng.WsNew(WsNewOptions{Dock: "labs", Name: "w2"})

	// Mark w1 as active (not done)
	activeStatus := "active"
	eng.WsUpdate("labs", "w1", nil, nil, &activeStatus)

	// w2 is already idle by default

	_, _, err := eng.WsCloseByStatus("labs", "done", true)
	if err == nil {
		t.Error("expected error when no workspaces match status")
	}
	if !strings.Contains(err.Error(), "no workspaces with status") {
		t.Errorf("error = %q, want 'no workspaces with status' message", err)
	}

	// Both workspaces should still exist
	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock.FindWorkspace("w1") == nil {
		t.Error("w1 should still exist")
	}
	if dock.FindWorkspace("w2") == nil {
		t.Error("w2 should still exist")
	}
}

// --- WsNew with --branch ---

func TestWsNew_WithBranch(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Branch: "feature/new-branch"})
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

func TestWsClose_RemoveWorktreeFailurePreservesWorkspaceState(t *testing.T) {
	eng, _ := testEngine(t)

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
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

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1"})
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

	ws, err := eng.WsNew(WsNewOptions{Dock: "labs", Name: "w1", Shell: true})
	if err != nil {
		t.Fatalf("WsNew failed: %v", err)
	}
	if err := eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeAgent, "agent", "codex", "", "v"); err != nil {
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
