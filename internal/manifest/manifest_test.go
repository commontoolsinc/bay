package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// sampleManifestTOML returns a full manifest in TOML form for testing round-trips.
func sampleManifestTOML() string {
	return `
[docks.labs.workspaces.w3]
name = "mem-refactor"
type = "worktree"
repo = "labs"
path = "~/projects/labs-worktrees/w3"
branch = "feature/refactor-memory-access"
pr = "234"
status = "active"

[[docks.labs.workspaces.w3.windows]]
id = 1
tmux_window_id = "@4"
name = "mem-refactor"

[[docks.labs.workspaces.w3.windows.panes]]
id = 1
type = "agent"
agent = "claude"

[[docks.labs.workspaces.w3.windows.panes]]
id = 2
type = "shell"
split_from = 1
split_dir = "h"

[[docks.labs.workspaces.w3.windows]]
id = 2
tmux_window_id = "@9"
name = "mem-refactor:2"

[[docks.labs.workspaces.w3.windows.panes]]
id = 1
type = "shell"

[docks.labs.workspaces.w4]
name = "w4"
type = "worktree"
repo = "labs"
path = "~/projects/labs-worktrees/w4"
status = "idle"

[docks.research.workspaces.w1]
name = "perf-study"
type = "external"
path = "~/projects/research/perf"
status = "idle"
`
}

// --- Empty manifest handling ---

func TestParse_EmptyManifest(t *testing.T) {
	m, err := Parse("")
	if err != nil {
		t.Fatalf("Parse empty failed: %v", err)
	}
	if m.Docks == nil {
		t.Error("Docks map should be initialized, not nil")
	}
	if len(m.Docks) != 0 {
		t.Errorf("expected 0 docks, got %d", len(m.Docks))
	}
}

func TestParse_InvalidTOML(t *testing.T) {
	_, err := Parse("[bad toml = =")
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
}

// --- Parse/serialize round-trip ---

func TestRoundTrip(t *testing.T) {
	m, err := Parse(sampleManifestTOML())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Verify structure parsed correctly.
	if len(m.Docks) != 2 {
		t.Fatalf("expected 2 docks, got %d", len(m.Docks))
	}

	labs := m.Docks["labs"]
	if len(labs.Workspaces) != 2 {
		t.Fatalf("expected 2 workspaces in labs, got %d", len(labs.Workspaces))
	}

	w3 := labs.Workspaces["w3"]
	if w3.Name != "mem-refactor" {
		t.Errorf("w3 name = %q, want %q", w3.Name, "mem-refactor")
	}
	if w3.Type != WorkspaceTypeWorktree {
		t.Errorf("w3 type = %q, want %q", w3.Type, WorkspaceTypeWorktree)
	}
	if w3.Repo != "labs" {
		t.Errorf("w3 repo = %q, want %q", w3.Repo, "labs")
	}
	if w3.Path != "~/projects/labs-worktrees/w3" {
		t.Errorf("w3 path = %q", w3.Path)
	}
	if w3.Branch != "feature/refactor-memory-access" {
		t.Errorf("w3 branch = %q", w3.Branch)
	}
	if w3.PR != "234" {
		t.Errorf("w3 pr = %q", w3.PR)
	}
	if w3.Status != WorkspaceStatusActive {
		t.Errorf("w3 status = %q, want %q", w3.Status, WorkspaceStatusActive)
	}

	// Windows
	if len(w3.Windows) != 2 {
		t.Fatalf("w3 windows count = %d, want 2", len(w3.Windows))
	}
	win1 := w3.Windows[0]
	if win1.ID != 1 {
		t.Errorf("win1 id = %d", win1.ID)
	}
	if win1.TmuxWindowID != "@4" {
		t.Errorf("win1 tmux_window_id = %q", win1.TmuxWindowID)
	}
	if win1.Name != "mem-refactor" {
		t.Errorf("win1 name = %q", win1.Name)
	}

	// Panes
	if len(win1.Panes) != 2 {
		t.Fatalf("win1 panes count = %d, want 2", len(win1.Panes))
	}
	p1 := win1.Panes[0]
	if p1.ID != 1 {
		t.Errorf("p1 id = %d", p1.ID)
	}
	if p1.Type != PaneTypeAgent {
		t.Errorf("p1 type = %q, want %q", p1.Type, PaneTypeAgent)
	}
	if p1.Agent != "claude" {
		t.Errorf("p1 agent = %q", p1.Agent)
	}
	p2 := win1.Panes[1]
	if p2.ID != 2 {
		t.Errorf("p2 id = %d", p2.ID)
	}
	if p2.Type != PaneTypeShell {
		t.Errorf("p2 type = %q", p2.Type)
	}
	if p2.SplitFrom != 1 {
		t.Errorf("p2 split_from = %d", p2.SplitFrom)
	}
	if p2.SplitDir != "h" {
		t.Errorf("p2 split_dir = %q", p2.SplitDir)
	}

	// Second window
	win2 := w3.Windows[1]
	if win2.ID != 2 {
		t.Errorf("win2 id = %d", win2.ID)
	}
	if len(win2.Panes) != 1 {
		t.Fatalf("win2 panes count = %d, want 1", len(win2.Panes))
	}

	// Research dock
	research := m.Docks["research"]
	if len(research.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace in research, got %d", len(research.Workspaces))
	}
	rw1 := research.Workspaces["w1"]
	if rw1.Type != WorkspaceTypeExternal {
		t.Errorf("research w1 type = %q, want %q", rw1.Type, WorkspaceTypeExternal)
	}

	// --- Save and re-load ---
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.toml")
	if err := Save(path, m); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	m2, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Spot check key fields survived the round trip.
	w3b := m2.Docks["labs"].Workspaces["w3"]
	if w3b.Name != "mem-refactor" {
		t.Errorf("round-trip w3 name = %q", w3b.Name)
	}
	if w3b.Status != WorkspaceStatusActive {
		t.Errorf("round-trip w3 status = %q", w3b.Status)
	}
	if len(w3b.Windows) != 2 {
		t.Errorf("round-trip w3 windows = %d, want 2", len(w3b.Windows))
	}
	if len(w3b.Windows[0].Panes) != 2 {
		t.Errorf("round-trip w3 win1 panes = %d, want 2", len(w3b.Windows[0].Panes))
	}
	if w3b.Windows[0].Panes[1].SplitDir != "h" {
		t.Errorf("round-trip p2 split_dir = %q", w3b.Windows[0].Panes[1].SplitDir)
	}
}

// --- Load/Save file operations ---

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/manifest.toml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestSave_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "manifest.toml")
	m := New()
	if err := Save(path, m); err != nil {
		t.Fatalf("Save failed to create directories: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("saved file does not exist: %v", err)
	}
}

// --- NextWorkspaceID ---

func TestNextWorkspaceID_EmptyDock(t *testing.T) {
	m := New()
	m.Docks["labs"] = &DockState{
		Workspaces: make(map[string]*Workspace),
	}
	id := NextWorkspaceID(m.Docks["labs"])
	if id != "w1" {
		t.Errorf("expected w1, got %q", id)
	}
}

func TestNextWorkspaceID_ExistingWorkspaces(t *testing.T) {
	m := New()
	m.Docks["labs"] = &DockState{
		Workspaces: map[string]*Workspace{
			"w1": {Name: "first"},
			"w3": {Name: "third"},
		},
	}
	id := NextWorkspaceID(m.Docks["labs"])
	if id != "w4" {
		t.Errorf("expected w4, got %q", id)
	}
}

func TestNextWorkspaceID_NilDock(t *testing.T) {
	dock := &DockState{Workspaces: nil}
	id := NextWorkspaceID(dock)
	if id != "w1" {
		t.Errorf("expected w1, got %q", id)
	}
}

// --- NextWindowID ---

func TestNextWindowID_EmptyWorkspace(t *testing.T) {
	ws := &Workspace{}
	id := NextWindowID(ws)
	if id != 1 {
		t.Errorf("expected 1, got %d", id)
	}
}

func TestNextWindowID_ExistingWindows(t *testing.T) {
	ws := &Workspace{
		Windows: []Window{
			{ID: 1},
			{ID: 3},
		},
	}
	id := NextWindowID(ws)
	if id != 4 {
		t.Errorf("expected 4, got %d", id)
	}
}

// --- NextPaneID ---

func TestNextPaneID_EmptyWindow(t *testing.T) {
	win := &Window{}
	id := NextPaneID(win)
	if id != 1 {
		t.Errorf("expected 1, got %d", id)
	}
}

func TestNextPaneID_ExistingPanes(t *testing.T) {
	win := &Window{
		Panes: []Pane{
			{ID: 1},
			{ID: 5},
		},
	}
	id := NextPaneID(win)
	if id != 6 {
		t.Errorf("expected 6, got %d", id)
	}
}

// --- AddWorkspace / RemoveWorkspace ---

func TestAddWorkspace(t *testing.T) {
	m := New()
	ws := &Workspace{
		Name:   "test-ws",
		Type:   WorkspaceTypeWorktree,
		Status: WorkspaceStatusIdle,
	}
	id, err := m.AddWorkspace("labs", ws)
	if err != nil {
		t.Fatalf("AddWorkspace failed: %v", err)
	}
	if id != "w1" {
		t.Errorf("expected w1, got %q", id)
	}

	// Verify it was added.
	got, ok := m.Docks["labs"].Workspaces["w1"]
	if !ok {
		t.Fatal("workspace w1 not found in dock")
	}
	if got.Name != "test-ws" {
		t.Errorf("workspace name = %q", got.Name)
	}

	// Add another.
	ws2 := &Workspace{
		Name:   "test-ws-2",
		Type:   WorkspaceTypeExternal,
		Status: WorkspaceStatusActive,
	}
	id2, err := m.AddWorkspace("labs", ws2)
	if err != nil {
		t.Fatalf("AddWorkspace second failed: %v", err)
	}
	if id2 != "w2" {
		t.Errorf("expected w2, got %q", id2)
	}
}

func TestRemoveWorkspace(t *testing.T) {
	m := New()
	ws := &Workspace{Name: "to-remove", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusIdle}
	m.AddWorkspace("labs", ws)

	err := m.RemoveWorkspace("labs", "w1")
	if err != nil {
		t.Fatalf("RemoveWorkspace failed: %v", err)
	}
	if len(m.Docks["labs"].Workspaces) != 0 {
		t.Error("workspace should have been removed")
	}
}

func TestRemoveWorkspace_NotFound(t *testing.T) {
	m := New()
	err := m.RemoveWorkspace("labs", "w1")
	if err == nil {
		t.Error("expected error removing from nonexistent dock")
	}

	m.Docks["labs"] = &DockState{Workspaces: make(map[string]*Workspace)}
	err = m.RemoveWorkspace("labs", "w99")
	if err == nil {
		t.Error("expected error removing nonexistent workspace")
	}
}

// --- GetWorkspace ---

func TestGetWorkspace_ByDockAndID(t *testing.T) {
	m := New()
	ws := &Workspace{Name: "target", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusActive}
	m.AddWorkspace("labs", ws)

	got, dock, id, err := m.GetWorkspace("labs:w1")
	if err != nil {
		t.Fatalf("GetWorkspace failed: %v", err)
	}
	if got.Name != "target" {
		t.Errorf("name = %q", got.Name)
	}
	if dock != "labs" {
		t.Errorf("dock = %q", dock)
	}
	if id != "w1" {
		t.Errorf("id = %q", id)
	}
}

func TestGetWorkspace_BareID_Unambiguous(t *testing.T) {
	m := New()
	ws := &Workspace{Name: "only-one", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusIdle}
	m.AddWorkspace("labs", ws)

	got, dock, id, err := m.GetWorkspace("w1")
	if err != nil {
		t.Fatalf("GetWorkspace bare failed: %v", err)
	}
	if got.Name != "only-one" {
		t.Errorf("name = %q", got.Name)
	}
	if dock != "labs" {
		t.Errorf("dock = %q", dock)
	}
	if id != "w1" {
		t.Errorf("id = %q", id)
	}
}

func TestGetWorkspace_BareID_Ambiguous(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "a", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusIdle})
	m.AddWorkspace("research", &Workspace{Name: "b", Type: WorkspaceTypeExternal, Status: WorkspaceStatusIdle})

	_, _, _, err := m.GetWorkspace("w1")
	if err == nil {
		t.Error("expected ambiguous error")
	}
}

func TestGetWorkspace_NotFound(t *testing.T) {
	m := New()
	_, _, _, err := m.GetWorkspace("labs:w99")
	if err == nil {
		t.Error("expected not-found error")
	}
}

// --- GetWorkspaceByName ---

func TestGetWorkspaceByName_Found(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "mem-refactor", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusActive})
	m.AddWorkspace("labs", &Workspace{Name: "other", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusIdle})

	got, dock, id, err := m.GetWorkspaceByName("mem-refactor")
	if err != nil {
		t.Fatalf("GetWorkspaceByName failed: %v", err)
	}
	if got.Name != "mem-refactor" {
		t.Errorf("name = %q", got.Name)
	}
	if dock != "labs" {
		t.Errorf("dock = %q", dock)
	}
	if id != "w1" {
		t.Errorf("id = %q", id)
	}
}

func TestGetWorkspaceByName_NotFound(t *testing.T) {
	m := New()
	_, _, _, err := m.GetWorkspaceByName("nonexistent")
	if err == nil {
		t.Error("expected not-found error")
	}
}

func TestGetWorkspaceByName_Ambiguous(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "dup", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusIdle})
	m.AddWorkspace("research", &Workspace{Name: "dup", Type: WorkspaceTypeExternal, Status: WorkspaceStatusIdle})

	_, _, _, err := m.GetWorkspaceByName("dup")
	if err == nil {
		t.Error("expected ambiguous error")
	}
}

// --- ResolveWorkspace ---

func TestResolveWorkspace_DockColonID(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "ws1", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusActive})

	got, dock, id, err := m.ResolveWorkspace("labs:w1")
	if err != nil {
		t.Fatalf("ResolveWorkspace dock:id failed: %v", err)
	}
	if got.Name != "ws1" || dock != "labs" || id != "w1" {
		t.Errorf("got name=%q dock=%q id=%q", got.Name, dock, id)
	}
}

func TestResolveWorkspace_BareID(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "ws1", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusActive})

	got, dock, id, err := m.ResolveWorkspace("w1")
	if err != nil {
		t.Fatalf("ResolveWorkspace bare id failed: %v", err)
	}
	if got.Name != "ws1" || dock != "labs" || id != "w1" {
		t.Errorf("got name=%q dock=%q id=%q", got.Name, dock, id)
	}
}

func TestResolveWorkspace_ByName(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "mem-refactor", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusActive})

	got, dock, id, err := m.ResolveWorkspace("mem-refactor")
	if err != nil {
		t.Fatalf("ResolveWorkspace by name failed: %v", err)
	}
	if got.Name != "mem-refactor" || dock != "labs" || id != "w1" {
		t.Errorf("got name=%q dock=%q id=%q", got.Name, dock, id)
	}
}

func TestResolveWorkspace_Ambiguous(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "dup", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusIdle})
	m.AddWorkspace("research", &Workspace{Name: "dup", Type: WorkspaceTypeExternal, Status: WorkspaceStatusIdle})

	// Bare w1 is ambiguous across docks.
	_, _, _, err := m.ResolveWorkspace("w1")
	if err == nil {
		t.Error("expected ambiguous error for bare w1")
	}

	// Name "dup" is ambiguous across docks.
	_, _, _, err = m.ResolveWorkspace("dup")
	if err == nil {
		t.Error("expected ambiguous error for name dup")
	}
}

func TestResolveWorkspace_NotFound(t *testing.T) {
	m := New()
	_, _, _, err := m.ResolveWorkspace("nonexistent")
	if err == nil {
		t.Error("expected not-found error")
	}
}

// --- AddWindow / RemoveWindow ---

func TestAddWindow(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "ws1", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusActive})

	win := Window{
		TmuxWindowID: "@10",
		Name:         "ws1",
	}
	id, err := m.AddWindow("labs", "w1", win)
	if err != nil {
		t.Fatalf("AddWindow failed: %v", err)
	}
	if id != 1 {
		t.Errorf("expected window id 1, got %d", id)
	}

	ws := m.Docks["labs"].Workspaces["w1"]
	if len(ws.Windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(ws.Windows))
	}
	if ws.Windows[0].TmuxWindowID != "@10" {
		t.Errorf("tmux_window_id = %q", ws.Windows[0].TmuxWindowID)
	}

	// Add a second window.
	win2 := Window{TmuxWindowID: "@11", Name: "ws1:2"}
	id2, err := m.AddWindow("labs", "w1", win2)
	if err != nil {
		t.Fatalf("AddWindow second failed: %v", err)
	}
	if id2 != 2 {
		t.Errorf("expected window id 2, got %d", id2)
	}
}

func TestRemoveWindow(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "ws1", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusActive})
	m.AddWindow("labs", "w1", Window{TmuxWindowID: "@10", Name: "ws1"})
	m.AddWindow("labs", "w1", Window{TmuxWindowID: "@11", Name: "ws1:2"})

	err := m.RemoveWindow("labs", "w1", 1)
	if err != nil {
		t.Fatalf("RemoveWindow failed: %v", err)
	}
	ws := m.Docks["labs"].Workspaces["w1"]
	if len(ws.Windows) != 1 {
		t.Fatalf("expected 1 window after removal, got %d", len(ws.Windows))
	}
	if ws.Windows[0].ID != 2 {
		t.Errorf("remaining window id = %d, want 2", ws.Windows[0].ID)
	}
}

func TestRemoveWindow_NotFound(t *testing.T) {
	m := New()
	err := m.RemoveWindow("labs", "w1", 1)
	if err == nil {
		t.Error("expected error for nonexistent dock")
	}
}

// --- AddPane / RemovePane ---

func TestAddPane(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "ws1", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusActive})
	m.AddWindow("labs", "w1", Window{TmuxWindowID: "@10", Name: "ws1"})

	pane := Pane{Type: PaneTypeAgent, Agent: "claude"}
	id, err := m.AddPane("labs", "w1", 1, pane)
	if err != nil {
		t.Fatalf("AddPane failed: %v", err)
	}
	if id != 1 {
		t.Errorf("expected pane id 1, got %d", id)
	}

	pane2 := Pane{Type: PaneTypeShell, SplitFrom: 1, SplitDir: "h"}
	id2, err := m.AddPane("labs", "w1", 1, pane2)
	if err != nil {
		t.Fatalf("AddPane second failed: %v", err)
	}
	if id2 != 2 {
		t.Errorf("expected pane id 2, got %d", id2)
	}

	win := m.Docks["labs"].Workspaces["w1"].Windows[0]
	if len(win.Panes) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(win.Panes))
	}
}

func TestRemovePane(t *testing.T) {
	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "ws1", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusActive})
	m.AddWindow("labs", "w1", Window{TmuxWindowID: "@10", Name: "ws1"})
	m.AddPane("labs", "w1", 1, Pane{Type: PaneTypeAgent, Agent: "claude"})
	m.AddPane("labs", "w1", 1, Pane{Type: PaneTypeShell, SplitFrom: 1, SplitDir: "v"})

	err := m.RemovePane("labs", "w1", 1, 1)
	if err != nil {
		t.Fatalf("RemovePane failed: %v", err)
	}
	win := m.Docks["labs"].Workspaces["w1"].Windows[0]
	if len(win.Panes) != 1 {
		t.Fatalf("expected 1 pane after removal, got %d", len(win.Panes))
	}
	if win.Panes[0].ID != 2 {
		t.Errorf("remaining pane id = %d, want 2", win.Panes[0].ID)
	}
}

func TestRemovePane_NotFound(t *testing.T) {
	m := New()
	err := m.RemovePane("labs", "w1", 1, 1)
	if err == nil {
		t.Error("expected error for nonexistent dock")
	}
}

// --- File locking ---

func TestFileLock_BasicLockUnlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	unlock, err := lockFile(path)
	if err != nil {
		t.Fatalf("lockFile failed: %v", err)
	}
	defer unlock()
}

func TestSave_UsesLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.toml")

	m := New()
	m.AddWorkspace("labs", &Workspace{Name: "test", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusIdle})

	if err := Save(path, m); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify the lock file was created.
	lockPath := path + ".lock"
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lock file should exist at %q: %v", lockPath, err)
	}

	// Verify data was written.
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save failed: %v", err)
	}
	if loaded.Docks["labs"].Workspaces["w1"].Name != "test" {
		t.Error("saved data not correct after reload")
	}
}

// --- PaneType constants ---

func TestPaneTypeCmd(t *testing.T) {
	if PaneTypeCmd != "cmd" {
		t.Errorf("PaneTypeCmd = %q, want %q", PaneTypeCmd, "cmd")
	}
}

// --- New helper ---

func TestNew(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.Docks == nil {
		t.Error("Docks should be initialized")
	}
}
