package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	m, err := Parse([]byte(`{"version":1,"docks":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Docks) != 0 {
		t.Errorf("docks length = %d, want 0", len(m.Docks))
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
	if ws.Type != WorkspaceTypeWorktree {
		t.Errorf("workspace type = %q, want %q", ws.Type, WorkspaceTypeWorktree)
	}
	if ws.Status != WorkspaceStatusActive {
		t.Errorf("workspace status = %q, want %q", ws.Status, WorkspaceStatusActive)
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
						Name:   "auth-fix",
						Type:   WorkspaceTypeWorktree,
						Path:   "/tmp/ws1",
						Status: WorkspaceStatusActive,
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
			{Name: "ws1", Type: WorkspaceTypeWorktree, Status: WorkspaceStatusIdle},
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

	// No backup on first save
	backup := path + ".bak"
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Error("backup should not exist on first save")
	}

	// Save again — should create backup
	m.Docks[0].Name = "second"
	if err := Save(path, m); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(backup); err != nil {
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

	if err := d.AddWorkspace(Workspace{Name: "auth-fix", Status: WorkspaceStatusIdle}); err != nil {
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
			{Name: "labs", Workspaces: []Workspace{{Name: "ws1", Status: WorkspaceStatusIdle}}},
		},
	}

	refs := AllWorkspaces(m)
	refs[0].Workspace.Status = WorkspaceStatusActive

	if m.Docks[0].Workspaces[0].Status != WorkspaceStatusActive {
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

	backup := path + ".bak"
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Error("backup should not exist after first LockedUpdate")
	}

	// Second update — backup should be created
	err = LockedUpdate(path, func(m *Manifest) error {
		m.Docks[0].Name = "second"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(backup); err != nil {
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
