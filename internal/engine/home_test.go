package engine

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

func TestBayNew_RejectsHomeNameWithGuidance(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.BayNew(BayNewOptions{Dock: "labs", Name: manifest.HomeBayID, Shell: true})
	if err == nil {
		t.Fatal("BayNew(home) succeeded, want reserved-name error")
	}
	const want = "home is reserved for the dock checkout; use `bay home`"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}

	mockGit := eng.Git.(*git.Mock)
	if got := len(mockGit.CreatedWorktrees()); got != 0 {
		t.Fatalf("created worktrees = %d, want 0", got)
	}
}

func TestBayNew_RejectsExternalHomeName(t *testing.T) {
	eng, dir := testEngine(t)

	_, err := eng.BayNew(BayNewOptions{Dock: "labs", Dir: dir, Name: manifest.HomeBayID, Shell: true})
	if err == nil || err.Error() != "home is reserved for the dock checkout; use `bay home`" {
		t.Fatalf("BayNew external home error = %v", err)
	}
}

func TestBayNew_BranchDerivedHomeNameIsMadeSafe(t *testing.T) {
	eng, _ := testEngine(t)

	bay, err := eng.BayNew(BayNewOptions{Dock: "labs", Branch: "feature/home", Shell: true})
	if err != nil {
		t.Fatalf("BayNew(feature/home): %v", err)
	}
	if bay.Name != branchAbbrevReservedPrefix+manifest.HomeBayID {
		t.Fatalf("bay.Name = %q, want %q", bay.Name, branchAbbrevReservedPrefix+manifest.HomeBayID)
	}
}

func TestBayCompactLabel_HomeUsesReservedLabel(t *testing.T) {
	label := BayCompactLabel(&manifest.Bay{
		ID:   manifest.HomeBayID,
		Name: manifest.HomeBayID,
		Type: manifest.BayTypeHome,
		Path: "/repo/labs",
	})
	if label != manifest.HomeBayID {
		t.Fatalf("BayCompactLabel(home) = %q, want %q", label, manifest.HomeBayID)
	}
}

func TestBayRename_RejectsHomeBay(t *testing.T) {
	eng, _ := testEngine(t)

	err := eng.BayRename("labs", manifest.HomeBayID, "anything")
	if err == nil || !strings.Contains(err.Error(), "rename is not supported") {
		t.Fatalf("BayRename(home) error = %v", err)
	}
}

func TestBayRename_RejectsRenamingToHome(t *testing.T) {
	eng, _ := testEngine(t)
	bay, err := eng.BayNew(BayNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("BayNew: %v", err)
	}

	err = eng.BayRename("labs", bay.ID, manifest.HomeBayID)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("BayRename(to home) error = %v", err)
	}
}

func TestBayDescribe_RejectsHomeBay(t *testing.T) {
	eng, _ := testEngine(t)

	err := eng.BayDescribe("labs", manifest.HomeBayID, "anything")
	if err == nil || !strings.Contains(err.Error(), "describe is not supported") {
		t.Fatalf("BayDescribe(home) error = %v", err)
	}
}

func TestBayUpdate_RejectsHomeBay(t *testing.T) {
	eng, _ := testEngine(t)
	branch := "main"

	err := eng.BayUpdate("labs", manifest.HomeBayID, &branch, nil)
	if err == nil || !strings.Contains(err.Error(), "branch/PR metadata") {
		t.Fatalf("BayUpdate(home) error = %v", err)
	}
}

func TestBayCleanReview_RejectsHomeBay(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.BayCleanReview("labs", manifest.HomeBayID)
	if err == nil || !strings.Contains(err.Error(), "does not apply") {
		t.Fatalf("BayCleanReview(home) error = %v", err)
	}
}

func TestBayClose_HomeIsNoOp(t *testing.T) {
	eng, _ := testEngine(t)

	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	dock := m.FindDock("labs")
	if dock == nil {
		t.Fatal("missing labs dock")
	}
	dockPath := dock.Path

	if err := eng.BayClose("labs", manifest.HomeBayID, false); err != nil {
		t.Fatalf("BayClose(home): %v", err)
	}

	m2, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest after close: %v", err)
	}
	dock2 := m2.FindDock("labs")
	if dock2 == nil {
		t.Fatal("dock disappeared after BayClose(home)")
	}
	if dock2.Path != dockPath {
		t.Fatalf("dock.Path = %q, want %q", dock2.Path, dockPath)
	}
	mockGit := eng.Git.(*git.Mock)
	if got := len(mockGit.RemovedWorktrees()); got != 0 {
		t.Fatalf("removed worktrees = %d, want 0", got)
	}
}

func TestBayClose_LastNonHomeCreatesHomeShell(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.BayNew(BayNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("BayNew: %v", err)
	}
	if err := eng.BayClose("labs", "w1", true); err != nil {
		t.Fatalf("BayClose(w1): %v", err)
	}

	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	dock := m.FindDock("labs")
	if dock == nil {
		t.Fatal("missing labs dock")
	}
	home := dock.FindBayByID(manifest.HomeBayID)
	if home == nil {
		t.Fatal("closing the last non-home bay did not create home")
	}
	if len(home.Surfaces) != 1 || home.Surfaces[0].Type != manifest.SurfaceTypeShell {
		t.Fatalf("home surfaces = %+v, want one shell", home.Surfaces)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	windows, _ := mockTmux.ListWindows("labs")
	for _, w := range windows {
		val, _ := mockTmux.GetWindowOption(w.ID, "@bay-placeholder")
		if val == "1" {
			t.Fatalf("placeholder %s should be replaced by home", w.ID)
		}
	}
}

func TestBayCloseBatch_LastNonHomeCreatesHomeShell(t *testing.T) {
	eng, _ := testEngine(t)

	if _, err := eng.BayNew(BayNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("BayNew: %v", err)
	}
	closed, skipped, err := eng.BayCloseClean("labs", true, false)
	if err != nil {
		t.Fatalf("BayCloseClean: %v", err)
	}
	if len(closed) != 1 || closed[0] != "labs:w1" || len(skipped) != 0 {
		t.Fatalf("closed=%v skipped=%v, want labs:w1 only", closed, skipped)
	}

	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	dock := m.FindDock("labs")
	if dock == nil {
		t.Fatal("missing labs dock")
	}
	home := dock.FindBayByID(manifest.HomeBayID)
	if home == nil || len(home.Surfaces) != 1 || home.Surfaces[0].Type != manifest.SurfaceTypeShell {
		t.Fatalf("home = %+v, want one shell surface", home)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	windows, _ := mockTmux.ListWindows("labs")
	for _, w := range windows {
		val, _ := mockTmux.GetWindowOption(w.ID, "@bay-placeholder")
		if val == "1" {
			t.Fatalf("placeholder %s should be replaced by home", w.ID)
		}
	}
}

func TestResolveBay_HomeWithDockPrefix(t *testing.T) {
	eng, _ := testEngine(t)

	dockName, bayID, err := eng.ResolveBay("labs:home")
	if err != nil {
		t.Fatalf("ResolveBay(labs:home): %v", err)
	}
	if dockName != "labs" || bayID != manifest.HomeBayID {
		t.Fatalf("resolved (%q, %q), want (labs, home)", dockName, bayID)
	}
}

func TestSurfaceAdd_HomeMaterializesBay(t *testing.T) {
	eng, _ := testEngine(t)

	err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	})
	if err != nil {
		t.Fatalf("SurfaceAdd(home): %v", err)
	}

	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	dock := m.FindDock("labs")
	if dock == nil {
		t.Fatal("missing labs dock")
	}
	home := dock.FindBayByID(manifest.HomeBayID)
	if home == nil {
		t.Fatal("home bay was not materialized")
	}
	if home.Name != manifest.HomeBayID || home.Type != manifest.BayTypeHome || home.Path != dock.Path || home.Worktree != nil {
		t.Fatalf("home shape = %+v, dock.Path=%q", home, dock.Path)
	}
	if len(home.Surfaces) != 1 || home.Surfaces[0].Type != manifest.SurfaceTypeShell {
		t.Fatalf("home surfaces = %+v, want one shell", home.Surfaces)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	sawMoveZero := false
	for _, call := range mockTmux.Calls {
		if call.Method == "MoveWindow" && len(call.Args) == 2 && call.Args[1] == "0" {
			sawMoveZero = true
		}
	}
	if !sawMoveZero {
		t.Fatalf("first home window should move to index 0; calls: %+v", mockTmux.Calls)
	}
}

func TestHome_FocusesExistingSurface(t *testing.T) {
	eng, _ := testEngine(t)

	if err := eng.Home("labs"); err != nil {
		t.Fatalf("Home create: %v", err)
	}
	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	home := m.FindDock("labs").FindBayByID(manifest.HomeBayID)
	if home == nil || len(home.Surfaces) != 1 || home.Surfaces[0].Tmux == nil {
		t.Fatalf("home = %+v, want one tmux surface", home)
	}
	winID := home.Surfaces[0].Tmux.WindowID

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Calls = nil
	if err := eng.Home("labs"); err != nil {
		t.Fatalf("Home focus: %v", err)
	}
	for _, call := range mockTmux.Calls {
		if call.Method == "NewWindow" || call.Method == "SplitWindow" {
			t.Fatalf("Home focus should not create another surface; calls: %+v", mockTmux.Calls)
		}
	}
	sawSelect := false
	for _, call := range mockTmux.Calls {
		if call.Method == "SelectWindow" && len(call.Args) == 1 && call.Args[0] == winID {
			sawSelect = true
		}
	}
	if !sawSelect {
		t.Fatalf("Home focus did not select %s; calls: %+v", winID, mockTmux.Calls)
	}
}

func TestBayClose_HomeClosesMaterializedSurfaces(t *testing.T) {
	eng, _ := testEngine(t)

	if err := eng.SurfaceAdd(SurfaceAddOptions{DockName: "labs", BayName: manifest.HomeBayID, Type: manifest.SurfaceTypeShell, Name: "shell"}); err != nil {
		t.Fatalf("SurfaceAdd(home): %v", err)
	}
	m, _ := eng.LoadManifest()
	dockPath := m.FindDock("labs").Path
	home := m.FindDock("labs").FindBayByID(manifest.HomeBayID)
	winID := home.Surfaces[0].Tmux.WindowID

	if err := eng.BayClose("labs", manifest.HomeBayID, false); err != nil {
		t.Fatalf("BayClose(home): %v", err)
	}
	m2, _ := eng.LoadManifest()
	dock := m2.FindDock("labs")
	if dock == nil {
		t.Fatal("dock disappeared")
	}
	if dock.Path != dockPath {
		t.Fatalf("dock.Path = %q, want %q", dock.Path, dockPath)
	}
	if home := dock.FindBayByID(manifest.HomeBayID); home != nil {
		t.Fatalf("home bay still persisted after close: %+v", home)
	}
	mockGit := eng.Git.(*git.Mock)
	if got := len(mockGit.RemovedWorktrees()); got != 0 {
		t.Fatalf("removed worktrees = %d, want 0", got)
	}
	mockTmux := eng.Tmux.(*tmux.Mock)
	if exists, _ := mockTmux.WindowExists(winID); exists {
		t.Fatalf("home window %s still exists after close", winID)
	}
}

func TestEdit_HomeReturnsDockPathWithoutMaterializing(t *testing.T) {
	eng, _ := testEngine(t)

	m, _ := eng.LoadManifest()
	wantPath := m.FindDock("labs").Path
	path, err := eng.Edit("labs", manifest.HomeBayID)
	if err != nil {
		t.Fatalf("Edit(home): %v", err)
	}
	if path != wantPath {
		t.Fatalf("Edit(home) path = %q, want %q", path, wantPath)
	}
	m2, _ := eng.LoadManifest()
	if home := m2.FindDock("labs").FindBayByID(manifest.HomeBayID); home != nil {
		t.Fatalf("Edit(home) should not materialize empty home, got %+v", home)
	}
}
