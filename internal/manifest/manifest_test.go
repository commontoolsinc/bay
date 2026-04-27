package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestNew_ReturnsEmptyManifest(t *testing.T) {
	m := New()
	if m.Version != CurrentVersion {
		t.Errorf("version = %d, want %d", m.Version, CurrentVersion)
	}
	if m.Docks == nil {
		t.Fatal("docks is nil")
	}
	if len(m.Docks) != 0 {
		t.Errorf("docks length = %d, want 0", len(m.Docks))
	}
}

func TestParse_EmptyJSON(t *testing.T) {
	m, err := Parse([]byte(`{"version":2,"repos":[],"docks":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Docks) != 0 {
		t.Errorf("docks length = %d, want 0", len(m.Docks))
	}
	if len(m.Repos) != 0 {
		t.Errorf("repos length = %d, want 0", len(m.Repos))
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	_, err := Parse([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParse_FullManifest(t *testing.T) {
	data := []byte(`{
		"version": 1,
		"docks": [
			{
				"name": "labs",
				"workspaces": [
					{
						"name": "auth-fix",
						"type": "worktree",
						"path": "/tmp/ws1",
						"status": "active",
						"last_focused": 1,
						"worktree": {
							"repo": "labs",
							"branch": "fix-auth",
							"pr": "52"
						},
						"surfaces": [
							{
								"id": 1,
								"name": "agent",
								"type": "agent",
								"backend": "tmux-pane",
								"agent": "claude-code",
								"tmux": {
									"pane_id": "%42",
									"window_id": "@15",
									"layout_group": 1
								}
							},
							{
								"id": 2,
								"name": "shell",
								"type": "shell",
								"backend": "tmux-pane",
								"tmux": {
									"layout_group": 1,
									"split_from": 1,
									"split_dir": "h"
								}
							},
							{
								"id": 3,
								"name": "editor",
								"type": "editor",
								"backend": "gui-app",
								"gui": {
									"app_command": "cursor",
									"bundle_id": "com.todesktop.230313mzl4w4u92"
								}
							}
						]
					}
				]
			}
		]
	}`)

	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(m.Docks) != 1 {
		t.Fatalf("docks length = %d, want 1", len(m.Docks))
	}

	dock := &m.Docks[0]
	if dock.Name != "labs" {
		t.Errorf("dock name = %q, want %q", dock.Name, "labs")
	}
	if len(dock.Workspaces) != 1 {
		t.Fatalf("workspaces length = %d, want 1", len(dock.Workspaces))
	}

	ws := &dock.Workspaces[0]
	if ws.Name != "auth-fix" {
		t.Errorf("workspace name = %q, want %q", ws.Name, "auth-fix")
	}
	// Path basename "ws1" is non-canonical, so pass 1 skips it and pass 2
	// assigns the first sequential ID.
	if ws.ID != "w1" {
		t.Errorf("workspace ID = %q, want %q", ws.ID, "w1")
	}
	if ws.Type != WorkspaceTypeWorktree {
		t.Errorf("workspace type = %q, want %q", ws.Type, WorkspaceTypeWorktree)
	}
	// v2 manifest with status=active should not set Merged
	if ws.Worktree != nil && ws.Worktree.Merged {
		t.Error("workspace should not be merged")
	}
	if ws.LastFocused != 1 {
		t.Errorf("last_focused = %d, want 1", ws.LastFocused)
	}
	if ws.Worktree == nil {
		t.Fatal("worktree attrs is nil")
	}
	if ws.Worktree.Repo != "labs" {
		t.Errorf("worktree repo = %q, want %q", ws.Worktree.Repo, "labs")
	}
	if ws.Worktree.Branch != "fix-auth" {
		t.Errorf("worktree branch = %q, want %q", ws.Worktree.Branch, "fix-auth")
	}
	if ws.Worktree.PR != "52" {
		t.Errorf("worktree pr = %q, want %q", ws.Worktree.PR, "52")
	}

	if len(ws.Surfaces) != 3 {
		t.Fatalf("surfaces length = %d, want 3", len(ws.Surfaces))
	}

	// Agent surface
	s := &ws.Surfaces[0]
	if s.ID != 1 || s.Name != "agent" || s.Type != SurfaceTypeAgent || s.Backend != SurfaceBackendTmux {
		t.Errorf("surface 0: got id=%d name=%q type=%q backend=%q", s.ID, s.Name, s.Type, s.Backend)
	}
	if s.Agent == nil || *s.Agent != "claude-code" {
		t.Errorf("surface 0: agent = %v, want %q", s.Agent, "claude-code")
	}
	if s.Tmux == nil {
		t.Fatal("surface 0: tmux attrs is nil")
	}
	if s.Tmux.PaneID != "%42" || s.Tmux.LayoutGroup != 1 {
		t.Errorf("surface 0 tmux: pane_id=%q layout_group=%d", s.Tmux.PaneID, s.Tmux.LayoutGroup)
	}

	// Shell surface
	s = &ws.Surfaces[1]
	if s.Type != SurfaceTypeShell || s.Tmux.SplitFrom != 1 || s.Tmux.SplitDir != "h" {
		t.Errorf("surface 1: type=%q split_from=%d split_dir=%q", s.Type, s.Tmux.SplitFrom, s.Tmux.SplitDir)
	}

	// Editor surface
	s = &ws.Surfaces[2]
	if s.Type != SurfaceTypeEditor || s.Backend != SurfaceBackendGUI {
		t.Errorf("surface 2: type=%q backend=%q", s.Type, s.Backend)
	}
	if s.GUI == nil || s.GUI.AppCommand != "cursor" {
		t.Errorf("surface 2: gui = %v", s.GUI)
	}
}

func TestRoundTrip(t *testing.T) {
	agent := "claude-code"
	original := &Manifest{
		Version: CurrentVersion,
		Docks: []Dock{
			{
				Name: "labs",
				Workspaces: []Workspace{
					{
						Name: "auth-fix",
						Type: WorkspaceTypeWorktree,
						Path: "/tmp/ws1",
						Worktree: &WorktreeAttrs{
							Repo:   "labs",
							Branch: "fix-auth",
							PR:     "52",
						},
						Surfaces: []Surface{
							{
								ID:      1,
								Name:    "agent",
								Type:    SurfaceTypeAgent,
								Backend: SurfaceBackendTmux,
								Agent:   &agent,
								Tmux:    &TmuxAttrs{LayoutGroup: 1},
							},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(restored.Docks) != 1 {
		t.Fatalf("docks length = %d, want 1", len(restored.Docks))
	}
	if restored.Docks[0].Name != "labs" {
		t.Errorf("dock name = %q, want %q", restored.Docks[0].Name, "labs")
	}
	ws := &restored.Docks[0].Workspaces[0]
	if ws.Name != "auth-fix" {
		t.Errorf("workspace name = %q, want %q", ws.Name, "auth-fix")
	}
	if ws.Surfaces[0].Agent == nil || *ws.Surfaces[0].Agent != "claude-code" {
		t.Error("agent not round-tripped")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	original := New()
	original.Docks = append(original.Docks, Dock{
		Name: "test",
		Workspaces: []Workspace{
			{Name: "ws1", Type: WorkspaceTypeWorktree},
		},
	})

	if err := Save(path, original); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Docks) != 1 || loaded.Docks[0].Name != "test" {
		t.Errorf("loaded dock: got %v", loaded.Docks)
	}
	if len(loaded.Docks[0].Workspaces) != 1 || loaded.Docks[0].Workspaces[0].Name != "ws1" {
		t.Errorf("loaded workspace: got %v", loaded.Docks[0].Workspaces)
	}
}

func TestSave_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "manifest.json")

	if err := Save(path, New()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	// Save initial version
	m := New()
	m.Docks = append(m.Docks, Dock{Name: "first"})
	if err := Save(path, m); err != nil {
		t.Fatal(err)
	}

	// No backup on first save (no previous file to back up).
	backupDir := filepath.Join(dir, "backups")
	backups, _ := ListBackups(path)
	if len(backups) != 0 {
		t.Error("no backups should exist on first save")
	}

	// Save again — should create a backup of the first version.
	m.Docks[0].Name = "second"
	if err := Save(path, m); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(backupDir); err != nil {
		t.Error("backup directory should exist after second save")
	}
	backups, _ = ListBackups(path)
	if len(backups) == 0 {
		t.Error("backup should exist after second save")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/manifest.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLockedUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	// First update creates the manifest
	err := LockedUpdate(path, func(m *Manifest) error {
		m.Docks = append(m.Docks, Dock{Name: "labs"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Second update modifies it
	err = LockedUpdate(path, func(m *Manifest) error {
		if len(m.Docks) != 1 || m.Docks[0].Name != "labs" {
			t.Errorf("expected dock 'labs', got %v", m.Docks)
		}
		m.Docks = append(m.Docks, Dock{Name: "research"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify final state
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Docks) != 2 {
		t.Fatalf("docks length = %d, want 2", len(m.Docks))
	}
}

// --- Dock operations ---

func TestFindDock(t *testing.T) {
	m := New()
	m.Docks = []Dock{{Name: "labs"}, {Name: "research"}}

	d := m.FindDock("labs")
	if d == nil || d.Name != "labs" {
		t.Errorf("FindDock(labs) = %v", d)
	}

	d = m.FindDock("nonexistent")
	if d != nil {
		t.Errorf("FindDock(nonexistent) = %v, want nil", d)
	}
}

func TestFindDock_ReturnsMutablePointer(t *testing.T) {
	m := New()
	m.Docks = []Dock{{Name: "labs"}}

	d := m.FindDock("labs")
	d.Workspaces = append(d.Workspaces, Workspace{Name: "ws1"})

	if len(m.Docks[0].Workspaces) != 1 {
		t.Error("mutation through FindDock pointer did not affect manifest")
	}
}

func TestAddDock(t *testing.T) {
	m := New()

	if err := m.AddDock(Dock{Name: "labs"}); err != nil {
		t.Fatal(err)
	}
	if len(m.Docks) != 1 || m.Docks[0].Name != "labs" {
		t.Errorf("after add: %v", m.Docks)
	}
}

func TestAddDock_DuplicateName(t *testing.T) {
	m := New()
	m.Docks = []Dock{{Name: "labs"}}

	err := m.AddDock(Dock{Name: "labs"})
	if err == nil {
		t.Fatal("expected error for duplicate dock name")
	}
}

func TestRemoveDock(t *testing.T) {
	m := New()
	m.Docks = []Dock{{Name: "labs"}, {Name: "research"}}

	if err := m.RemoveDock("labs"); err != nil {
		t.Fatal(err)
	}
	if len(m.Docks) != 1 || m.Docks[0].Name != "research" {
		t.Errorf("after remove: %v", m.Docks)
	}
}

func TestRemoveDock_NotFound(t *testing.T) {
	m := New()
	if err := m.RemoveDock("nonexistent"); err == nil {
		t.Fatal("expected error for missing dock")
	}
}

// --- Workspace operations ---

func TestFindWorkspace(t *testing.T) {
	d := &Dock{
		Name:       "labs",
		Workspaces: []Workspace{{Name: "auth-fix"}, {Name: "perf"}},
	}

	ws := d.FindWorkspace("auth-fix")
	if ws == nil || ws.Name != "auth-fix" {
		t.Errorf("FindWorkspace(auth-fix) = %v", ws)
	}

	ws = d.FindWorkspace("nonexistent")
	if ws != nil {
		t.Errorf("FindWorkspace(nonexistent) = %v, want nil", ws)
	}
}

func TestAddWorkspace(t *testing.T) {
	d := &Dock{Name: "labs"}

	if err := d.AddWorkspace(Workspace{Name: "auth-fix"}); err != nil {
		t.Fatal(err)
	}
	if len(d.Workspaces) != 1 || d.Workspaces[0].Name != "auth-fix" {
		t.Errorf("after add: %v", d.Workspaces)
	}
}

func TestAddWorkspace_DuplicateName(t *testing.T) {
	d := &Dock{
		Name:       "labs",
		Workspaces: []Workspace{{Name: "auth-fix"}},
	}

	err := d.AddWorkspace(Workspace{Name: "auth-fix"})
	if err == nil {
		t.Fatal("expected error for duplicate workspace name")
	}
}

func TestRemoveWorkspace(t *testing.T) {
	d := &Dock{
		Name:       "labs",
		Workspaces: []Workspace{{Name: "auth-fix"}, {Name: "perf"}},
	}

	if err := d.RemoveWorkspace("auth-fix"); err != nil {
		t.Fatal(err)
	}
	if len(d.Workspaces) != 1 || d.Workspaces[0].Name != "perf" {
		t.Errorf("after remove: %v", d.Workspaces)
	}
}

func TestRemoveWorkspace_NotFound(t *testing.T) {
	d := &Dock{Name: "labs"}
	if err := d.RemoveWorkspace("nonexistent"); err == nil {
		t.Fatal("expected error for missing workspace")
	}
}

// --- Surface operations ---

func TestNextSurfaceID_Empty(t *testing.T) {
	ws := &Workspace{}
	if id := ws.NextSurfaceID(); id != 1 {
		t.Errorf("NextSurfaceID() = %d, want 1", id)
	}
}

func TestNextSurfaceID_WithExisting(t *testing.T) {
	ws := &Workspace{
		Surfaces: []Surface{{ID: 1}, {ID: 3}},
	}
	if id := ws.NextSurfaceID(); id != 4 {
		t.Errorf("NextSurfaceID() = %d, want 4", id)
	}
}

func TestFindSurface(t *testing.T) {
	ws := &Workspace{
		Surfaces: []Surface{{ID: 1, Name: "agent"}, {ID: 2, Name: "shell"}},
	}

	s := ws.FindSurface("agent")
	if s == nil || s.Name != "agent" {
		t.Errorf("FindSurface(agent) = %v", s)
	}

	s = ws.FindSurface("nonexistent")
	if s != nil {
		t.Errorf("FindSurface(nonexistent) = %v, want nil", s)
	}
}

func TestFindSurfaceByID(t *testing.T) {
	ws := &Workspace{
		Surfaces: []Surface{{ID: 1, Name: "agent"}, {ID: 2, Name: "shell"}},
	}

	s := ws.FindSurfaceByID(2)
	if s == nil || s.Name != "shell" {
		t.Errorf("FindSurfaceByID(2) = %v", s)
	}

	s = ws.FindSurfaceByID(99)
	if s != nil {
		t.Errorf("FindSurfaceByID(99) = %v, want nil", s)
	}
}

func TestAddSurface(t *testing.T) {
	ws := &Workspace{}

	id, err := ws.AddSurface(Surface{
		Name:    "agent",
		Type:    SurfaceTypeAgent,
		Backend: SurfaceBackendTmux,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Errorf("assigned id = %d, want 1", id)
	}
	if len(ws.Surfaces) != 1 || ws.Surfaces[0].ID != 1 {
		t.Errorf("after add: %v", ws.Surfaces)
	}

	// Add a second surface
	id, err = ws.AddSurface(Surface{Name: "shell", Type: SurfaceTypeShell, Backend: SurfaceBackendTmux})
	if err != nil {
		t.Fatal(err)
	}
	if id != 2 {
		t.Errorf("second id = %d, want 2", id)
	}
}

func TestAddSurface_DuplicateName(t *testing.T) {
	ws := &Workspace{
		Surfaces: []Surface{{ID: 1, Name: "agent"}},
	}

	_, err := ws.AddSurface(Surface{Name: "agent"})
	if err == nil {
		t.Fatal("expected error for duplicate surface name")
	}
}

func TestAddSurface_IDsNeverReused(t *testing.T) {
	ws := &Workspace{
		Surfaces: []Surface{{ID: 1, Name: "agent"}, {ID: 3, Name: "shell"}},
	}

	id, err := ws.AddSurface(Surface{Name: "editor"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 4 {
		t.Errorf("id = %d, want 4 (should not reuse 2)", id)
	}
}

func TestRemoveSurface(t *testing.T) {
	ws := &Workspace{
		Surfaces: []Surface{
			{ID: 1, Name: "agent"},
			{ID: 2, Name: "shell"},
			{ID: 3, Name: "editor"},
		},
	}

	if err := ws.RemoveSurface("shell"); err != nil {
		t.Fatal(err)
	}
	if len(ws.Surfaces) != 2 {
		t.Fatalf("surfaces length = %d, want 2", len(ws.Surfaces))
	}
	if ws.Surfaces[0].Name != "agent" || ws.Surfaces[1].Name != "editor" {
		t.Errorf("after remove: %v", ws.Surfaces)
	}
}

func TestRemoveSurface_NotFound(t *testing.T) {
	ws := &Workspace{}
	if err := ws.RemoveSurface("nonexistent"); err == nil {
		t.Fatal("expected error for missing surface")
	}
}

// --- Workspace resolution ---

func TestResolveWorkspace_ByDockColonName(t *testing.T) {
	m := &Manifest{
		Docks: []Dock{
			{Name: "labs", Workspaces: []Workspace{{Name: "auth-fix"}}},
		},
	}

	ws, dock, err := m.ResolveWorkspace("labs:auth-fix")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "auth-fix" || dock.Name != "labs" {
		t.Errorf("resolve = ws=%q dock=%q", ws.Name, dock.Name)
	}
}

func TestResolveWorkspace_ByName(t *testing.T) {
	m := &Manifest{
		Docks: []Dock{
			{Name: "labs", Workspaces: []Workspace{{Name: "auth-fix"}}},
		},
	}

	ws, dock, err := m.ResolveWorkspace("auth-fix")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "auth-fix" || dock.Name != "labs" {
		t.Errorf("resolve = ws=%q dock=%q", ws.Name, dock.Name)
	}
}

func TestResolveWorkspace_AmbiguousName(t *testing.T) {
	m := &Manifest{
		Docks: []Dock{
			{Name: "labs", Workspaces: []Workspace{{Name: "shared"}}},
			{Name: "research", Workspaces: []Workspace{{Name: "shared"}}},
		},
	}

	_, _, err := m.ResolveWorkspace("shared")
	if err == nil {
		t.Fatal("expected error for ambiguous name")
	}
}

func TestResolveWorkspace_NotFound(t *testing.T) {
	m := &Manifest{
		Docks: []Dock{{Name: "labs", Workspaces: []Workspace{{Name: "auth-fix"}}}},
	}

	_, _, err := m.ResolveWorkspace("nonexistent")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

// --- AllWorkspaces ---

func TestAllWorkspaces(t *testing.T) {
	m := &Manifest{
		Docks: []Dock{
			{Name: "research", Workspaces: []Workspace{{Name: "beta"}, {Name: "alpha"}}},
			{Name: "labs", Workspaces: []Workspace{{Name: "ws1"}}},
		},
	}

	refs := AllWorkspaces(m)
	if len(refs) != 3 {
		t.Fatalf("AllWorkspaces length = %d, want 3", len(refs))
	}
	// Sorted by dock then workspace name
	if refs[0].Dock != "labs" || refs[0].Workspace.Name != "ws1" {
		t.Errorf("refs[0] = %q/%q", refs[0].Dock, refs[0].Workspace.Name)
	}
	if refs[1].Dock != "research" || refs[1].Workspace.Name != "alpha" {
		t.Errorf("refs[1] = %q/%q", refs[1].Dock, refs[1].Workspace.Name)
	}
	if refs[2].Dock != "research" || refs[2].Workspace.Name != "beta" {
		t.Errorf("refs[2] = %q/%q", refs[2].Dock, refs[2].Workspace.Name)
	}
}

func TestAllWorkspaces_ReturnsMutablePointers(t *testing.T) {
	m := &Manifest{
		Docks: []Dock{
			{Name: "labs", Workspaces: []Workspace{{Name: "ws1"}}},
		},
	}

	refs := AllWorkspaces(m)
	refs[0].Workspace.Name = "ws1-renamed"

	if m.Docks[0].Workspaces[0].Name != "ws1-renamed" {
		t.Error("mutation through AllWorkspaces pointer did not affect manifest")
	}
}

// --- LockedUpdate atomic write ---

func TestLockedUpdate_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	// First update — no backup expected
	err := LockedUpdate(path, func(m *Manifest) error {
		m.Docks = append(m.Docks, Dock{Name: "first"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	backups, _ := ListBackups(path)
	if len(backups) != 0 {
		t.Error("no backups should exist after first LockedUpdate")
	}

	// Second update — backup should be created
	err = LockedUpdate(path, func(m *Manifest) error {
		m.Docks[0].Name = "second"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	backups, _ = ListBackups(path)
	if len(backups) == 0 {
		t.Error("backup should exist after second LockedUpdate")
	}

	// Verify the temp file was cleaned up
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should not exist after LockedUpdate")
	}
}

// --- Validation ---

func TestSurface_Validate_Valid(t *testing.T) {
	agent := "claude-code"
	s := Surface{
		Name:    "agent",
		Type:    SurfaceTypeAgent,
		Backend: SurfaceBackendTmux,
		Agent:   &agent,
		Tmux:    &TmuxAttrs{LayoutGroup: 1},
	}
	if errs := s.Validate(); len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestSurface_Validate_MissingTmuxAttrs(t *testing.T) {
	s := Surface{Name: "shell", Type: SurfaceTypeShell, Backend: SurfaceBackendTmux}
	errs := s.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
}

func TestSurface_Validate_MissingGUIAttrs(t *testing.T) {
	s := Surface{Name: "editor", Type: SurfaceTypeEditor, Backend: SurfaceBackendGUI}
	errs := s.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
}

func TestSurface_Validate_BothBackendAttrs(t *testing.T) {
	s := Surface{
		Name:    "bad",
		Type:    SurfaceTypeShell,
		Backend: SurfaceBackendTmux,
		Tmux:    &TmuxAttrs{},
		GUI:     &GUIAttrs{AppCommand: "cursor"},
	}
	errs := s.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
}

func TestSurface_Validate_AgentMissing(t *testing.T) {
	s := Surface{Name: "agent", Type: SurfaceTypeAgent, Backend: SurfaceBackendTmux, Tmux: &TmuxAttrs{}}
	errs := s.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
}

func TestSurface_Validate_AgentOnWrongType(t *testing.T) {
	agent := "claude"
	s := Surface{Name: "shell", Type: SurfaceTypeShell, Backend: SurfaceBackendTmux, Tmux: &TmuxAttrs{}, Agent: &agent}
	errs := s.Validate()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
}

func TestResolveWorkspace_DockColonNotFound(t *testing.T) {
	m := &Manifest{
		Docks: []Dock{{Name: "labs", Workspaces: []Workspace{{Name: "auth-fix"}}}},
	}

	_, _, err := m.ResolveWorkspace("labs:nonexistent")
	if err == nil {
		t.Fatal("expected error for not found")
	}

	_, _, err = m.ResolveWorkspace("baddock:auth-fix")
	if err == nil {
		t.Fatal("expected error for bad dock")
	}
}

// --- Repo operations ---

func TestFindRepo(t *testing.T) {
	m := New()
	m.Repos = []Repo{{Name: "labs", Path: "/p/labs"}, {Name: "other", Path: "/p/other"}}

	r := m.FindRepo("labs")
	if r == nil || r.Name != "labs" {
		t.Errorf("FindRepo(labs) = %v", r)
	}

	r = m.FindRepo("nonexistent")
	if r != nil {
		t.Errorf("FindRepo(nonexistent) = %v, want nil", r)
	}
}

func TestAddRepo(t *testing.T) {
	m := New()

	if err := m.AddRepo(Repo{Name: "labs", Path: "/p/labs"}); err != nil {
		t.Fatal(err)
	}
	if len(m.Repos) != 1 || m.Repos[0].Name != "labs" {
		t.Errorf("after add: %v", m.Repos)
	}
}

func TestAddRepo_Duplicate(t *testing.T) {
	m := New()
	m.Repos = []Repo{{Name: "labs", Path: "/p/labs"}}

	err := m.AddRepo(Repo{Name: "labs", Path: "/p/labs2"})
	if err == nil {
		t.Fatal("expected error for duplicate repo name")
	}
}

func TestRemoveRepo(t *testing.T) {
	m := New()
	m.Repos = []Repo{{Name: "labs", Path: "/p/labs"}, {Name: "other", Path: "/p/other"}}

	if err := m.RemoveRepo("labs"); err != nil {
		t.Fatal(err)
	}
	if len(m.Repos) != 1 || m.Repos[0].Name != "other" {
		t.Errorf("after remove: %v", m.Repos)
	}
}

func TestRemoveRepo_NotFound(t *testing.T) {
	m := New()
	if err := m.RemoveRepo("nonexistent"); err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestDockRepoAndAgentRoundTrip(t *testing.T) {
	original := &Manifest{
		Version: CurrentVersion,
		Repos:   []Repo{{Name: "labs", Path: "/p/labs"}},
		Docks: []Dock{
			{
				Name:       "dev",
				Repo:       "labs",
				Agent:      "claude",
				AgentArgs:  map[string][]string{"claude": {"--add-dir", "/extra"}},
				Workspaces: []Workspace{},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	if len(restored.Repos) != 1 || restored.Repos[0].Name != "labs" {
		t.Errorf("repos not round-tripped: %v", restored.Repos)
	}

	dock := restored.FindDock("dev")
	if dock == nil {
		t.Fatal("dock not found")
	}
	if dock.Repo != "labs" {
		t.Errorf("dock.Repo = %q, want labs", dock.Repo)
	}
	if dock.Agent != "claude" {
		t.Errorf("dock.Agent = %q, want claude", dock.Agent)
	}
	if len(dock.AgentArgs["claude"]) != 2 || dock.AgentArgs["claude"][0] != "--add-dir" {
		t.Errorf("dock.AgentArgs[claude] = %v, want [--add-dir /extra]", dock.AgentArgs["claude"])
	}
}

func TestParse_MigratesV2StatusDoneToMerged(t *testing.T) {
	data := []byte(`{
		"version": 2,
		"repos": [],
		"docks": [
			{
				"name": "labs",
				"workspaces": [
					{
						"name": "ws-done",
						"type": "worktree",
						"path": "/tmp/ws1",
						"status": "done",
						"worktree": {"repo": "labs", "branch": "feat-x"},
						"surfaces": []
					},
					{
						"name": "ws-active",
						"type": "worktree",
						"path": "/tmp/ws2",
						"status": "active",
						"worktree": {"repo": "labs", "branch": "feat-y"},
						"surfaces": []
					},
					{
						"name": "ws-no-worktree",
						"type": "external",
						"path": "/tmp/ws3",
						"status": "done",
						"surfaces": []
					}
				]
			}
		]
	}`)

	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	ws0 := &m.Docks[0].Workspaces[0]
	if ws0.Worktree == nil || !ws0.Worktree.Merged {
		t.Errorf("ws-done: expected Merged=true, got Merged=%v", ws0.Worktree != nil && ws0.Worktree.Merged)
	}

	ws1 := &m.Docks[0].Workspaces[1]
	if ws1.Worktree == nil || ws1.Worktree.Merged {
		t.Errorf("ws-active: expected Merged=false, got Merged=%v", ws1.Worktree != nil && ws1.Worktree.Merged)
	}

	// External workspace with status=done but no worktree — Merged should not be set
	ws2 := &m.Docks[0].Workspaces[2]
	if ws2.Worktree != nil {
		t.Errorf("ws-no-worktree: expected no worktree attrs")
	}

	if m.Version != CurrentVersion {
		t.Errorf("version = %d, want %d", m.Version, CurrentVersion)
	}
}

func TestIsWorkspaceID(t *testing.T) {
	cases := []struct {
		s    string
		want bool
	}{
		{"w1", true},
		{"w42", true},
		{"w999", true},
		{"", false},
		{"w", false},
		{"w0", false},  // n must be >= 1
		{"w01", false}, // leading zero rejected
		{"W1", false},  // case-sensitive
		{"w1a", false}, // trailing non-digit
		{"v1", false},  // wrong prefix
		{"auth-fix", false},
		{"w-1", false},
		{"w+1", false}, // strconv.Atoi accepts +/- prefixes; we don't
		{"w1.5", false},
	}
	for _, c := range cases {
		if got := IsWorkspaceID(c.s); got != c.want {
			t.Errorf("IsWorkspaceID(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}

// TestParse_FillsMissingIDsFromPathBasename covers the common legacy
// case: a v3 manifest where every workspace's path is already w<N>-shaped
// (because bay's worktree dirs have always been). Each workspace's ID
// should equal its path basename — no renumbering, no surprises.
func TestParse_FillsMissingIDsFromPathBasename(t *testing.T) {
	data := []byte(`{
		"version": 3,
		"repos": [],
		"docks": [
			{
				"name": "labs",
				"workspaces": [
					{"name": "auth-fix", "type": "worktree", "path": "/repo-worktrees/w1", "surfaces": []},
					{"name": "cache-ttl", "type": "worktree", "path": "/repo-worktrees/w2", "surfaces": []},
					{"name": "api-log",  "type": "worktree", "path": "/repo-worktrees/w5", "surfaces": []}
				]
			}
		]
	}`)
	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, ws := range m.Docks[0].Workspaces {
		got = append(got, ws.ID)
	}
	want := []string{"w1", "w2", "w5"}
	if !slices.Equal(got, want) {
		t.Errorf("IDs = %v, want %v", got, want)
	}
}

// TestParse_AssignsSequentialIDsForUnusablePaths covers external
// workspaces (or any with non-w<N> path basenames): they fall through
// to pass 2 and get the next available sequential ID, picking up after
// the highest-claimed worktree ID.
func TestParse_AssignsSequentialIDsForUnusablePaths(t *testing.T) {
	data := []byte(`{
		"version": 3,
		"repos": [],
		"docks": [
			{
				"name": "mixed",
				"workspaces": [
					{"name": "wt", "type": "worktree", "path": "/repo-worktrees/w3", "surfaces": []},
					{"name": "ext1", "type": "external", "path": "/Users/me/projects/foo", "surfaces": []},
					{"name": "ext2", "type": "external", "path": "/Users/me/projects/bar", "surfaces": []}
				]
			}
		]
	}`)
	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	// wt claims w3 from its path; externals get w4, w5 (continuing from max).
	got := []string{}
	for _, ws := range m.Docks[0].Workspaces {
		got = append(got, ws.ID)
	}
	want := []string{"w3", "w4", "w5"}
	if !slices.Equal(got, want) {
		t.Errorf("IDs = %v, want %v", got, want)
	}
}

// TestParse_HandlesIDCollisions covers the pathological case where two
// workspaces claim the same w<N> path basename (shouldn't normally
// happen, but guard against it). First one to be processed wins; the
// second falls through to sequential assignment.
func TestParse_HandlesIDCollisions(t *testing.T) {
	data := []byte(`{
		"version": 3,
		"repos": [],
		"docks": [
			{
				"name": "labs",
				"workspaces": [
					{"name": "a", "type": "worktree", "path": "/x/w1", "surfaces": []},
					{"name": "b", "type": "worktree", "path": "/x/w1", "surfaces": []}
				]
			}
		]
	}`)
	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Docks[0].Workspaces[0].ID != "w1" {
		t.Errorf("workspace[0].ID = %q, want w1", m.Docks[0].Workspaces[0].ID)
	}
	if m.Docks[0].Workspaces[1].ID != "w2" {
		t.Errorf("workspace[1].ID = %q, want w2 (collision fallback)", m.Docks[0].Workspaces[1].ID)
	}
}

// TestParse_PreservesExistingIDs ensures Parse never overwrites an ID
// already present in the manifest. Migration is fill-only, idempotent.
func TestParse_PreservesExistingIDs(t *testing.T) {
	data := []byte(`{
		"version": 4,
		"repos": [],
		"docks": [
			{
				"name": "labs",
				"workspaces": [
					{"id": "w7", "name": "a", "type": "worktree", "path": "/x/w1", "surfaces": []}
				]
			}
		]
	}`)
	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Docks[0].Workspaces[0].ID != "w7" {
		t.Errorf("ID = %q, want w7 (preserved)", m.Docks[0].Workspaces[0].ID)
	}
}

// TestParse_BumpsToV4 confirms a v3 manifest is upgraded to the current
// version after the ID-fill migration runs.
func TestParse_BumpsToV4(t *testing.T) {
	data := []byte(`{"version": 3, "repos": [], "docks": []}`)
	m, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != CurrentVersion {
		t.Errorf("version = %d, want %d", m.Version, CurrentVersion)
	}
}

// TestSaveAndLoad_PreservesID confirms IDs round-trip through the
// JSON encoder/decoder without modification.
func TestSaveAndLoad_PreservesID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	original := New()
	original.Docks = append(original.Docks, Dock{
		Name: "test",
		Workspaces: []Workspace{
			{ID: "w3", Name: "alpha", Type: WorkspaceTypeWorktree, Path: "/repo-worktrees/w3"},
		},
	})
	if err := Save(path, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Docks[0].Workspaces[0].ID != "w3" {
		t.Errorf("ID after round-trip = %q, want w3", loaded.Docks[0].Workspaces[0].ID)
	}
}

func TestRepoEffectiveWorktreeDir(t *testing.T) {
	// Explicit worktree_dir
	r := Repo{Name: "labs", Path: "/projects/labs", WorktreeDir: "/custom/worktrees"}
	if got := r.EffectiveWorktreeDir(); got != "/custom/worktrees" {
		t.Errorf("expected /custom/worktrees, got %q", got)
	}

	// Default: path + "-worktrees"
	r2 := Repo{Name: "labs", Path: "/projects/labs"}
	if got := r2.EffectiveWorktreeDir(); got != "/projects/labs-worktrees" {
		t.Errorf("expected /projects/labs-worktrees, got %q", got)
	}
}
