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

func TestBayClose_HomeIsNoOpWhenEmpty(t *testing.T) {
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

func TestBayClose_HomeClosesAllSurfacesAndPreservesCheckout(t *testing.T) {
	eng, _ := testEngine(t)

	// Create two home surfaces.
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	}); err != nil {
		t.Fatalf("SurfaceAdd 1: %v", err)
	}
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "logs",
		SplitDir: "v",
	}); err != nil {
		t.Fatalf("SurfaceAdd 2: %v", err)
	}

	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	dockPath := m.FindDock("labs").Path
	home := m.FindDock("labs").FindBayByID(manifest.HomeBayID)
	if home == nil || len(home.Surfaces) < 2 {
		t.Fatalf("expected home with 2 surfaces, got %+v", home)
	}

	if err := eng.BayClose("labs", manifest.HomeBayID, true); err != nil {
		t.Fatalf("BayClose(home): %v", err)
	}

	// Empty home should be removed; dock.Path untouched.
	m2, _ := eng.LoadManifest()
	dock2 := m2.FindDock("labs")
	if dock2 == nil {
		t.Fatal("dock disappeared after BayClose(home)")
	}
	if dock2.Path != dockPath {
		t.Errorf("dock.Path = %q, want %q (must not be mutated)", dock2.Path, dockPath)
	}
	if h := dock2.FindBayByID(manifest.HomeBayID); h != nil {
		t.Errorf("home should be removed after closing all surfaces, got %+v", h)
	}
	mockGit := eng.Git.(*git.Mock)
	if got := len(mockGit.RemovedWorktrees()); got != 0 {
		t.Errorf("removed worktrees = %d, want 0", got)
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

func TestSurfaceAdd_HomeMaterializesBayAtDockPath(t *testing.T) {
	eng, _ := testEngine(t)

	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	}); err != nil {
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
		t.Fatal("home bay not persisted after first surface")
	}
	if home.Type != manifest.BayTypeHome {
		t.Errorf("home.Type = %q, want %q", home.Type, manifest.BayTypeHome)
	}
	if home.Path != dock.Path {
		t.Errorf("home.Path = %q, want %q (dock.Path)", home.Path, dock.Path)
	}
	if home.Worktree != nil {
		t.Errorf("home.Worktree = %+v, want nil", home.Worktree)
	}
	if got := len(home.Surfaces); got != 1 {
		t.Fatalf("home surfaces = %d, want 1", got)
	}
	if home.Surfaces[0].Name != "shell" || home.Surfaces[0].Type != manifest.SurfaceTypeShell {
		t.Errorf("home surface = %+v, want shell/shell", home.Surfaces[0])
	}
}

func TestSurfaceAdd_HomeFirstWindowGoesToTabIndex0(t *testing.T) {
	eng, _ := testEngine(t)
	mockTmux := eng.Tmux.(*tmux.Mock)

	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	}); err != nil {
		t.Fatalf("SurfaceAdd(home): %v", err)
	}

	moved := false
	for _, call := range mockTmux.Calls {
		if call.Method == "MoveWindow" && len(call.Args) >= 2 && call.Args[1] == "0" {
			moved = true
		}
	}
	if !moved {
		t.Error("first home surface should be moved to tab index 0")
	}
}

func TestSurfaceClose_LastHomeSurfaceRemovesPersistedHome(t *testing.T) {
	eng, _ := testEngine(t)

	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	}); err != nil {
		t.Fatalf("SurfaceAdd(home): %v", err)
	}

	if err := eng.SurfaceClose("labs", manifest.HomeBayID, "shell", false); err != nil {
		t.Fatalf("SurfaceClose: %v", err)
	}

	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	dock := m.FindDock("labs")
	if dock == nil {
		t.Fatal("missing labs dock")
	}
	if home := dock.FindBayByID(manifest.HomeBayID); home != nil {
		t.Errorf("empty home should not be persisted; got %+v", home)
	}
}

func TestEdit_HomeReturnsDockPath(t *testing.T) {
	eng, _ := testEngine(t)

	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	dockPath := m.FindDock("labs").Path

	got, err := eng.Edit("labs", manifest.HomeBayID)
	if err != nil {
		t.Fatalf("Edit(home): %v", err)
	}
	if got != dockPath {
		t.Errorf("Edit(home) = %q, want %q (dock.Path)", got, dockPath)
	}
}

func TestHome_FocusesExistingSurface(t *testing.T) {
	eng, _ := testEngine(t)
	mockTmux := eng.Tmux.(*tmux.Mock)

	if err := eng.Home("labs"); err != nil {
		t.Fatalf("Home(labs) initial: %v", err)
	}

	m, _ := eng.LoadManifest()
	home := m.FindDock("labs").FindBayByID(manifest.HomeBayID)
	if home == nil || len(home.Surfaces) != 1 {
		t.Fatalf("home not materialized; bay = %+v", home)
	}
	wantWindow := home.Surfaces[0].Tmux.WindowID

	mockTmux.Calls = nil
	if err := eng.Home("labs"); err != nil {
		t.Fatalf("Home(labs) follow-up: %v", err)
	}

	// Second invocation should focus, not create.
	for _, call := range mockTmux.Calls {
		if call.Method == "NewWindow" {
			t.Error("Home() with existing surface created a new window")
		}
	}
	selected := false
	for _, call := range mockTmux.Calls {
		if call.Method == "SelectWindow" && len(call.Args) > 0 && call.Args[0] == wantWindow {
			selected = true
		}
	}
	if !selected {
		t.Errorf("Home() did not select existing home window %s", wantWindow)
	}
}

func TestHome_NoCheckoutErrors(t *testing.T) {
	eng, _ := testEngine(t)
	if err := eng.withManifest(func(m *manifest.Manifest) error {
		m.FindDock("labs").Path = ""
		return nil
	}); err != nil {
		t.Fatalf("clearing dock path: %v", err)
	}
	if err := eng.Home("labs"); err == nil {
		t.Fatal("Home with no checkout should error")
	}
}

func TestSurfaceAdd_HomeAgentUsesDockDefault(t *testing.T) {
	eng, _ := testEngine(t)

	// `bay agent --bay home` arrives via runSurfaceNew which fills in the
	// dock default agent before calling SurfaceAdd. Mirror that here.
	agent := eng.DefaultAgent("labs")
	if agent == "" {
		t.Fatal("test fixture should set a dock default agent")
	}

	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeAgent,
		Name:     "agent",
		Agent:    agent,
	}); err != nil {
		t.Fatalf("SurfaceAdd(home agent): %v", err)
	}

	m, _ := eng.LoadManifest()
	home := m.FindDock("labs").FindBayByID(manifest.HomeBayID)
	if home == nil || len(home.Surfaces) != 1 {
		t.Fatalf("home not materialized with one surface; got %+v", home)
	}
	s := home.Surfaces[0]
	if s.Type != manifest.SurfaceTypeAgent {
		t.Errorf("surface type = %q, want agent", s.Type)
	}
	if s.Agent == nil || *s.Agent != agent {
		t.Errorf("agent = %v, want %q", s.Agent, agent)
	}
	if s.Tmux == nil {
		t.Fatal("agent surface missing tmux attrs")
	}
}

func TestEmptyHome_HiddenFromList(t *testing.T) {
	eng, _ := testEngine(t)

	docks, err := eng.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, d := range docks {
		if d.Name != "labs" {
			continue
		}
		for _, b := range d.Bays {
			if b.ID == manifest.HomeBayID {
				t.Errorf("empty home should not appear in List(); got %+v", b)
			}
		}
	}
}

func TestVisibleHome_AppearsInList(t *testing.T) {
	eng, _ := testEngine(t)

	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	}); err != nil {
		t.Fatalf("SurfaceAdd(home): %v", err)
	}

	docks, err := eng.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, d := range docks {
		if d.Name != "labs" {
			continue
		}
		for _, b := range d.Bays {
			if b.ID == manifest.HomeBayID {
				found = true
				if b.Type != string(manifest.BayTypeHome) {
					t.Errorf("home in List has Type=%q, want %q", b.Type, manifest.BayTypeHome)
				}
			}
		}
	}
	if !found {
		t.Error("visible home (with surfaces) should appear in List()")
	}
}

func TestSync_EmptyHomeIsRemovedNotOrphanGraced(t *testing.T) {
	eng, _ := testEngine(t)

	// Materialize home with a shell.
	if err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	// Kill the home pane natively (simulate user `exit`/Ctrl-D).
	m, _ := eng.LoadManifest()
	home := m.FindDock("labs").FindBayByID(manifest.HomeBayID)
	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.KillPane(home.Surfaces[0].Tmux.PaneID); err != nil {
		t.Fatalf("KillPane: %v", err)
	}

	eng.SyncAll()

	m2, _ := eng.LoadManifest()
	dock := m2.FindDock("labs")
	if h := dock.FindBayByID(manifest.HomeBayID); h != nil {
		t.Errorf("sync should drop empty home, got %+v (PendingCloseAt=%d)", h, h.PendingCloseAt)
	}
}

func TestBayShow_SynthesizesEmptyHome(t *testing.T) {
	eng, _ := testEngine(t)

	bay, err := eng.BayShow("labs", manifest.HomeBayID)
	if err != nil {
		t.Fatalf("BayShow(home) on empty home: %v", err)
	}
	if bay == nil {
		t.Fatal("BayShow returned nil bay")
	}
	if bay.ID != manifest.HomeBayID || bay.Name != manifest.HomeBayID {
		t.Errorf("ID/Name = %q/%q, want home/home", bay.ID, bay.Name)
	}
	if bay.Type != manifest.BayTypeHome {
		t.Errorf("Type = %q, want %q", bay.Type, manifest.BayTypeHome)
	}
	m, _ := eng.LoadManifest()
	if bay.Path != m.FindDock("labs").Path {
		t.Errorf("Path = %q, want dock.Path %q", bay.Path, m.FindDock("labs").Path)
	}
}

func TestBayInfoByName_SynthesizesEmptyHome(t *testing.T) {
	eng, _ := testEngine(t)

	info, err := eng.BayInfoByName("labs", manifest.HomeBayID)
	if err != nil {
		t.Fatalf("BayInfoByName(home) on empty home: %v", err)
	}
	if info.ID != manifest.HomeBayID {
		t.Errorf("ID = %q, want home", info.ID)
	}
	if info.Type != string(manifest.BayTypeHome) {
		t.Errorf("Type = %q, want home", info.Type)
	}
	if info.Dirty {
		t.Error("home info should not be marked dirty (worktree-only signal)")
	}
}

func TestBayNew_AutoBootstrapDoesNotCreateHome(t *testing.T) {
	// `bay new foo` auto-bootstrapping a dock should create only foo;
	// home stays unmaterialized because foo's window keeps the session alive.
	// Engine-level: BayNew on an existing dock with no bays must not insert
	// a home Bay alongside the new one.
	eng, _ := testEngine(t)

	bay, err := eng.BayNew(BayNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("BayNew: %v", err)
	}
	if bay == nil {
		t.Fatal("BayNew returned nil")
	}

	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if h := dock.FindBayByID(manifest.HomeBayID); h != nil {
		t.Errorf("BayNew should not auto-create home, got %+v", h)
	}
}
