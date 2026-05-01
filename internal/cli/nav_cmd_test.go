package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/nav"
	"github.com/commontoolsinc/bay/internal/tmux"
)

func testNavEngine(t *testing.T) (*engine.Engine, *tmux.Mock, *git.Mock, string) {
	t.Helper()
	dir := t.TempDir()

	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Command: "claude"},
			"codex":  {Command: "codex"},
		},
		Docks: map[string]config.DockConfig{},
	}

	repoDir := filepath.Join(dir, "repos", "labs")
	os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755)

	mockTmux := tmux.NewMock()
	mockGit := git.NewMock()
	mockGit.SetGlobalIgnored(true)

	manifestPath := filepath.Join(dir, "manifest.json")
	manifest.Save(manifestPath, &manifest.Manifest{
		Version: manifest.CurrentVersion,
		Docks: []manifest.Dock{
			{Name: "labs", Path: repoDir, Agent: "claude", Workspaces: []manifest.Workspace{}},
		},
	})

	eng := engine.New(
		cfg,
		filepath.Join(dir, "config.toml"),
		manifestPath,
		filepath.Join(dir, "archive.json"),
		mockTmux,
		mockGit,
	)
	return eng, mockTmux, mockGit, dir
}

// --- filterSurfaceEntries ---

func TestFilterSurfaceEntries_ByName(t *testing.T) {
	entries := []nav.SurfaceEntry{
		{Name: "agent", Type: "agent"},
		{Name: "shell", Type: "shell"},
		{Name: "tests", Type: "cmd"},
	}
	result := filterSurfaceEntries(entries, "shell")
	if len(result) != 1 || result[0].Name != "shell" {
		t.Errorf("expected 1 match for 'shell', got %d", len(result))
	}
}

func TestFilterSurfaceEntries_ByType(t *testing.T) {
	entries := []nav.SurfaceEntry{
		{Name: "agent", Type: "agent"},
		{Name: "shell", Type: "shell"},
		{Name: "tests", Type: "cmd"},
	}
	result := filterSurfaceEntries(entries, "cmd")
	if len(result) != 1 || result[0].Name != "tests" {
		t.Errorf("expected 1 match for 'cmd', got %d", len(result))
	}
}

func TestFilterSurfaceEntries_CaseInsensitive(t *testing.T) {
	entries := []nav.SurfaceEntry{
		{Name: "Agent", Type: "agent"},
	}
	result := filterSurfaceEntries(entries, "agent")
	if len(result) != 1 {
		t.Errorf("expected case-insensitive match, got %d", len(result))
	}
}

func TestFilterSurfaceEntries_NoMatch(t *testing.T) {
	entries := []nav.SurfaceEntry{
		{Name: "agent", Type: "agent"},
	}
	result := filterSurfaceEntries(entries, "zzz")
	if len(result) != 0 {
		t.Errorf("expected 0 matches, got %d", len(result))
	}
}

// --- focusSurface ---

func TestFocusSurface_SelectsWindowAndPane(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	mockTmux.NewSession("labs")
	winID, _ := mockTmux.NewWindow("labs", "w1", "/tmp")
	panes, _ := mockTmux.ListPanes(winID)

	entry := &nav.SurfaceEntry{WindowID: winID, PaneID: panes[0].ID}
	if err := focusSurface(eng, entry, "labs", "w1"); err != nil {
		t.Fatalf("focusSurface: %v", err)
	}

	// Verify SelectWindow and SelectPane were called.
	foundWindow, foundPane := false, false
	for _, call := range mockTmux.Calls {
		if call.Method == "SelectWindow" && call.Args[0] == winID {
			foundWindow = true
		}
		if call.Method == "SelectPane" && call.Args[0] == panes[0].ID {
			foundPane = true
		}
	}
	if !foundWindow {
		t.Error("expected SelectWindow call")
	}
	if !foundPane {
		t.Error("expected SelectPane call")
	}
}

func TestFocusSurface_SkipsEmptyIDs(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	entry := &nav.SurfaceEntry{} // no window or pane ID
	if err := focusSurface(eng, entry, "labs", "w1"); err != nil {
		t.Fatalf("focusSurface: %v", err)
	}

	for _, call := range mockTmux.Calls {
		if call.Method == "SelectWindow" || call.Method == "SelectPane" {
			t.Errorf("should not call %s with empty IDs", call.Method)
		}
	}
}

// --- surfaceCycle ---

func TestSurfaceCycle_Forward(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	ws, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)
	// Add a second surface.
	eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-2", SplitDir: "v"})

	ws, _ = eng.WsShow("labs", "w1")

	// ResolveSelf uses tmux window ID fallback when CWD doesn't match.
	mockTmux.SetCurrentWindowID(ws.Surfaces[0].Tmux.WindowID)
	mockTmux.SetCurrentPaneID(ws.Surfaces[0].Tmux.PaneID)

	mockTmux.Calls = nil
	if err := surfaceCycle(eng, true); err != nil {
		t.Fatalf("surfaceCycle: %v", err)
	}

	// Should have selected the second surface's pane.
	foundSelect := false
	for _, call := range mockTmux.Calls {
		if call.Method == "SelectPane" && call.Args[0] == ws.Surfaces[1].Tmux.PaneID {
			foundSelect = true
		}
	}
	if !foundSelect {
		t.Error("expected SelectPane for second surface")
	}
}

func TestSurfaceCycle_Backward(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	ws, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)
	eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "shell-2", SplitDir: "v"})

	ws, _ = eng.WsShow("labs", "w1")

	// Current = second surface (index 1). Prev should go to first (index 0).
	mockTmux.SetCurrentWindowID(ws.Surfaces[1].Tmux.WindowID)
	mockTmux.SetCurrentPaneID(ws.Surfaces[1].Tmux.PaneID)

	mockTmux.Calls = nil
	if err := surfaceCycle(eng, false); err != nil {
		t.Fatalf("surfaceCycle backward: %v", err)
	}

	foundSelect := false
	for _, call := range mockTmux.Calls {
		if call.Method == "SelectPane" && call.Args[0] == ws.Surfaces[0].Tmux.PaneID {
			foundSelect = true
		}
	}
	if !foundSelect {
		t.Error("expected SelectPane for first surface")
	}
}

func TestSurfaceCycle_NoFlashOnSingleSurface(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	ws, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	os.MkdirAll(ws.Path, 0o755)
	ws, _ = eng.WsShow("labs", "w1")

	mockTmux.SetCurrentWindowID(ws.Surfaces[0].Tmux.WindowID)
	mockTmux.SetCurrentPaneID(ws.Surfaces[0].Tmux.PaneID)

	mockTmux.Calls = nil
	if err := surfaceCycle(eng, true); err != nil {
		t.Fatalf("surfaceCycle: %v", err)
	}

	if msgs := mockTmux.DisplayMessages(); len(msgs) != 0 {
		t.Errorf("expected no DisplayMessage calls on single-surface workspace, got %d: %v", len(msgs), msgs)
	}
}

// --- surfaceGo ---

func TestSurfaceGo_QueryFilter(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	ws, _ := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true})
	os.MkdirAll(ws.Path, 0o755)
	eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "claude", SplitDir: "v"})
	ws, _ = eng.WsShow("labs", "w1")

	mockTmux.SetCurrentWindowID(ws.Surfaces[0].Tmux.WindowID)
	mockTmux.SetCurrentPaneID(ws.Surfaces[0].Tmux.PaneID)

	mockTmux.Calls = nil
	// Query "agent" should match the agent surface and jump to it.
	if err := surfaceGo(eng, []string{"agent"}, false); err != nil {
		t.Fatalf("surfaceGo: %v", err)
	}

	foundSelect := false
	for _, call := range mockTmux.Calls {
		if call.Method == "SelectPane" && call.Args[0] == ws.Surfaces[1].Tmux.PaneID {
			foundSelect = true
		}
	}
	if !foundSelect {
		t.Error("expected SelectPane for agent surface")
	}
}

func TestSurfaceGo_NextWaitingIncludesBellWindow(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	ws, _ := eng.WsNew(engine.WsNewOptions{Dock: "labs", Shell: true})
	os.MkdirAll(ws.Path, 0o755)
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "codex"}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}
	ws, _ = eng.WsShow("labs", "w1")
	shell := ws.Surfaces[0]
	agent := ws.Surfaces[1]

	mockTmux.SetCurrentWindowID(shell.Tmux.WindowID)
	mockTmux.SetCurrentPaneID(shell.Tmux.PaneID)
	if err := mockTmux.SetWindowOption(agent.Tmux.WindowID, "@bay-bell", "1"); err != nil {
		t.Fatalf("SetWindowOption: %v", err)
	}

	mockTmux.Calls = nil
	if err := surfaceGo(eng, nil, true); err != nil {
		t.Fatalf("surfaceGo: %v", err)
	}

	foundSelect := false
	for _, call := range mockTmux.Calls {
		if call.Method == "SelectWindow" && call.Args[0] == agent.Tmux.WindowID {
			foundSelect = true
		}
	}
	if !foundSelect {
		t.Fatalf("expected SelectWindow for bell-waiting agent, calls: %#v", mockTmux.Calls)
	}
}

// --- formatSurfaceItems ---

func TestFormatSurfaceItems(t *testing.T) {
	entries := []nav.SurfaceEntry{
		{Name: "agent", Type: "agent", Current: true, Waiting: true},
		{Name: "shell", Type: "shell"},
	}
	items := formatSurfaceItems(entries)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	for _, want := range []string{"agent", "*", "WAITING"} {
		if !contains(items[0].Display, want) {
			t.Errorf("format missing %q in %q", want, items[0].Display)
		}
	}
	if contains(items[1].Display, "*") || contains(items[1].Display, "WAITING") {
		t.Errorf("plain entry should have no markers: %q", items[1].Display)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- wsCycle ---

func TestWsCycle_Forward(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	eng.WsNew(engine.WsNewOptions{Dock: "labs"})
	eng.WsNew(engine.WsNewOptions{Dock: "labs"})

	// Set tmux context to first workspace's window.
	ws1, _ := eng.WsShow("labs", "w1")
	mockTmux.SetCurrentSession("labs")
	mockTmux.SetCurrentWindowID(ws1.Surfaces[0].Tmux.WindowID)

	mockTmux.Calls = nil
	if err := wsCycle(eng, true); err != nil {
		t.Fatalf("wsCycle: %v", err)
	}

	// Should switch to w2's window.
	ws2, _ := eng.WsShow("labs", "w2")
	foundSelect := false
	for _, call := range mockTmux.Calls {
		if call.Method == "SelectWindow" && call.Args[0] == ws2.Surfaces[0].Tmux.WindowID {
			foundSelect = true
		}
	}
	if !foundSelect {
		t.Error("expected SelectWindow for second workspace")
	}
}

func TestWsCycle_Backward(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	eng.WsNew(engine.WsNewOptions{Dock: "labs"})
	eng.WsNew(engine.WsNewOptions{Dock: "labs"})

	// Current = w2. Prev should go to w1.
	ws1, _ := eng.WsShow("labs", "w1")
	ws2, _ := eng.WsShow("labs", "w2")
	mockTmux.SetCurrentSession("labs")
	mockTmux.SetCurrentWindowID(ws2.Surfaces[0].Tmux.WindowID)

	mockTmux.Calls = nil
	if err := wsCycle(eng, false); err != nil {
		t.Fatalf("wsCycle backward: %v", err)
	}

	foundSelect := false
	for _, call := range mockTmux.Calls {
		if call.Method == "SelectWindow" && call.Args[0] == ws1.Surfaces[0].Tmux.WindowID {
			foundSelect = true
		}
	}
	if !foundSelect {
		t.Error("expected SelectWindow for first workspace")
	}
}

func TestWsCycle_NoFlashOnSingleWorkspace(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)

	eng.WsNew(engine.WsNewOptions{Dock: "labs"})

	ws1, _ := eng.WsShow("labs", "w1")
	mockTmux.SetCurrentSession("labs")
	mockTmux.SetCurrentWindowID(ws1.Surfaces[0].Tmux.WindowID)

	mockTmux.Calls = nil
	if err := wsCycle(eng, true); err != nil {
		t.Fatalf("wsCycle: %v", err)
	}

	if msgs := mockTmux.DisplayMessages(); len(msgs) != 0 {
		t.Errorf("expected no DisplayMessage calls on single-workspace dock, got %d: %v", len(msgs), msgs)
	}
}

// --- ws rename self ---

func TestWsRename_SelfResolution(t *testing.T) {
	eng := selfFixture(t) // dock=labs, ws=w1, current surface=second

	dockName, wsID, err := resolveWsArg(eng, "self", "")
	if err != nil {
		t.Fatalf("resolveWsArg self: %v", err)
	}
	if err := eng.WsRename(dockName, wsID, "renamed-ws"); err != nil {
		t.Fatalf("WsRename: %v", err)
	}

	ws, err := eng.WsShow("labs", wsID)
	if err != nil {
		t.Fatalf("WsShow after rename: %v", err)
	}
	if ws.Name != "renamed-ws" {
		t.Errorf("bay name = %q, want renamed-ws", ws.Name)
	}
}

// --- dock rename self ---

func TestDockRename_SelfResolution(t *testing.T) {
	eng := selfFixture(t) // dock=labs, ws=w1, current surface=second

	dockName, _, err := eng.ResolveSelf()
	if err != nil {
		t.Fatalf("ResolveSelf: %v", err)
	}
	if dockName != "labs" {
		t.Fatalf("expected current dock=labs, got %q", dockName)
	}
	if err := eng.DockRename("labs", "renamed-dock"); err != nil {
		t.Fatalf("DockRename: %v", err)
	}

	// Verify old dock is gone and new one exists.
	m, _ := eng.LoadManifest()
	foundOld, foundNew := false, false
	for _, d := range m.Docks {
		if d.Name == "labs" {
			foundOld = true
		}
		if d.Name == "renamed-dock" {
			foundNew = true
		}
	}
	if foundOld {
		t.Error("dock 'labs' still present after rename")
	}
	if !foundNew {
		t.Error("dock 'renamed-dock' not found after rename")
	}
}

// --- autoBootstrap ---

func TestAutoBootstrap_CreatesRepoAndDock(t *testing.T) {
	dir := t.TempDir()

	cfg := config.DefaultConfig()
	mockTmux := tmux.NewMock()
	mockGit := git.NewMock()

	repoDir := filepath.Join(dir, "myproject")
	os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755)

	// Resolve symlinks so the mock key matches os.Getwd().
	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)
	mockGit.SetRepoRoot(resolvedRepo, resolvedRepo)

	configPath := filepath.Join(dir, "config.toml")

	eng := engine.New(
		cfg,
		configPath,
		filepath.Join(dir, "manifest.json"),
		filepath.Join(dir, "archive.json"),
		mockTmux,
		mockGit,
	)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	dockName, err := autoBootstrap(eng, false)
	if err != nil {
		t.Fatalf("autoBootstrap: %v", err)
	}

	if dockName != "myproject" {
		t.Errorf("dockName = %q, want myproject", dockName)
	}
	// Dock should be in the manifest with the checkout path.
	m, _ := eng.LoadManifest()
	dock := m.FindDock("myproject")
	if dock == nil {
		t.Error("dock not added to manifest")
	} else if dock.Path != resolvedRepo {
		t.Errorf("dock path = %q, want %q", dock.Path, resolvedRepo)
	}
}

func TestAutoBootstrap_NotInGitRepo(t *testing.T) {
	dir := t.TempDir()

	cfg := config.DefaultConfig()
	mockTmux := tmux.NewMock()
	mockGit := git.NewMock()
	// No SetRepoRoot — RepoRoot will return error.

	eng := engine.New(
		cfg,
		filepath.Join(dir, "config.toml"),
		filepath.Join(dir, "manifest.json"),
		filepath.Join(dir, "archive.json"),
		mockTmux,
		mockGit,
	)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	_, err := autoBootstrap(eng, false)
	if err == nil {
		t.Error("expected error when not in a git repo")
	}
}

func TestAutoBootstrap_SkipsExistingDock(t *testing.T) {
	dir := t.TempDir()

	repoDir := filepath.Join(dir, "myproject")
	os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755)
	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)

	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{},
		Docks:  map[string]config.DockConfig{},
	}
	mockTmux := tmux.NewMock()
	mockGit := git.NewMock()
	mockGit.SetRepoRoot(resolvedRepo, resolvedRepo)

	manifestPath := filepath.Join(dir, "manifest.json")
	manifest.Save(manifestPath, &manifest.Manifest{
		Version: manifest.CurrentVersion,
		Docks: []manifest.Dock{
			{Name: "myproject", Path: resolvedRepo, Workspaces: []manifest.Workspace{}},
		},
	})

	eng := engine.New(
		cfg,
		filepath.Join(dir, "config.toml"),
		manifestPath,
		filepath.Join(dir, "archive.json"),
		mockTmux,
		mockGit,
	)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	dockName, err := autoBootstrap(eng, false)
	if err != nil {
		t.Fatalf("autoBootstrap: %v", err)
	}
	if dockName != "myproject" {
		t.Errorf("dockName = %q, want myproject", dockName)
	}

	// DockNew should NOT have been called (dock already exists).
	for _, call := range mockTmux.Calls {
		if call.Method == "NewSession" {
			t.Error("should not create new session for existing dock")
		}
	}
}

func TestAutoBootstrap_ReusesExistingDockByCheckoutPath(t *testing.T) {
	dir := t.TempDir()

	repoDir := filepath.Join(dir, "myproject")
	os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755)
	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)

	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{},
		Docks:  map[string]config.DockConfig{},
	}
	mockTmux := tmux.NewMock()
	mockGit := git.NewMock()
	mockGit.SetRepoRoot(resolvedRepo, resolvedRepo)

	manifestPath := filepath.Join(dir, "manifest.json")
	manifest.Save(manifestPath, &manifest.Manifest{
		Version: manifest.CurrentVersion,
		Docks: []manifest.Dock{
			{Name: "dev", Path: resolvedRepo, Workspaces: []manifest.Workspace{}},
		},
	})

	eng := engine.New(
		cfg,
		filepath.Join(dir, "config.toml"),
		manifestPath,
		filepath.Join(dir, "archive.json"),
		mockTmux,
		mockGit,
	)

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	dockName, err := autoBootstrap(eng, false)
	if err != nil {
		t.Fatalf("autoBootstrap: %v", err)
	}
	if dockName != "dev" {
		t.Errorf("dockName = %q, want dev", dockName)
	}

	for _, call := range mockTmux.Calls {
		if call.Method == "NewSession" {
			t.Error("should not create new session when checkout already belongs to a dock")
		}
	}
}
