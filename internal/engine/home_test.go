package engine

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// `bay new home` must surface the bespoke alias-correcting message,
// not the generic ValidateBayName "reserved" text. Side-effect check:
// no worktree on disk — rejection must short-circuit before any git
// or filesystem work.
func TestBayNew_RejectsHomeName(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.BayNew(BayNewOptions{Dock: "labs", Name: manifest.HomeBayID, Shell: true})
	if err == nil {
		t.Fatal("expected BayNew(name=home) to fail")
	}
	if !strings.Contains(err.Error(), "reserved for the dock checkout") {
		t.Errorf("error = %v, want bespoke checkout-alias guidance", err)
	}
	mockGit := eng.Git.(*git.Mock)
	if got := len(mockGit.CreatedWorktrees()); got != 0 {
		t.Fatalf("created worktrees = %d, want 0 (home rejection must short-circuit)", got)
	}
}

// A branch like `feature/home` would derive a display name of
// "home" without abbreviateBranch's reserved-collision rewrite.
// Bay creation must succeed and produce a non-reserved Name.
func TestBayNew_BranchSanitizingToHome_GetsSafeName(t *testing.T) {
	eng, _ := testEngine(t)

	bay, err := eng.BayNew(BayNewOptions{Dock: "labs", Branch: "feature/home"})
	if err != nil {
		t.Fatalf("BayNew(branch=feature/home) errored: %v", err)
	}
	if bay.Name == manifest.HomeBayID {
		t.Errorf("bay name = %q, must not equal reserved handle", bay.Name)
	}
	if !strings.HasPrefix(bay.Name, branchAbbrevReservedPrefix) {
		t.Errorf("bay name = %q, want branch-derived name with reserved prefix", bay.Name)
	}
}

func TestBayRename_RejectsHomeBay(t *testing.T) {
	eng, _ := testEngine(t)
	if err := eng.BayRename("labs", manifest.HomeBayID, "anything"); err == nil {
		t.Fatal("expected BayRename(home, ...) to fail")
	}
}

func TestBayRename_RejectsRenamingExistingBayToHome(t *testing.T) {
	eng, _ := testEngine(t)
	bay, err := eng.BayNew(BayNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("seed bay: %v", err)
	}
	err = eng.BayRename("labs", bay.ID, manifest.HomeBayID)
	if err == nil {
		t.Fatal("expected BayRename(..., home) to fail")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Errorf("error = %v, want reserved-handle guidance", err)
	}
}

func TestBayDescribe_RejectsHomeBay(t *testing.T) {
	eng, _ := testEngine(t)
	if err := eng.BayDescribe("labs", manifest.HomeBayID, "anything"); err == nil {
		t.Fatal("expected BayDescribe(home) to fail")
	}
}

func TestBayUpdate_RejectsHomeBay(t *testing.T) {
	eng, _ := testEngine(t)
	branch := "main"
	if err := eng.BayUpdate("labs", manifest.HomeBayID, &branch, nil); err == nil {
		t.Fatal("expected BayUpdate(home) to fail")
	}
}

// BayClose("home") must be a no-op in Phase 1: no surfaces exist
// yet, and the canonical-checkout invariant ("never delete dock.Path")
// is enforced by routing around closeBayState entirely.
func TestBayClose_HomeIsNoOp(t *testing.T) {
	eng, _ := testEngine(t)

	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock == nil || dock.Path == "" {
		t.Fatal("test fixture missing labs dock with checkout path")
	}
	dockPath := dock.Path

	if err := eng.BayClose("labs", manifest.HomeBayID, false); err != nil {
		t.Fatalf("BayClose(labs, home) errored: %v", err)
	}

	m2, _ := eng.LoadManifest()
	dock2 := m2.FindDock("labs")
	if dock2 == nil {
		t.Fatal("dock disappeared after closing home")
	}
	if dock2.Path != dockPath {
		t.Errorf("dock.Path = %q, want %q (close must never rewrite the canonical checkout)", dock2.Path, dockPath)
	}
}

// SurfaceAdd must reject home in Phase 1 with explicit "Phase 2"
// guidance. Without this guard, `bay shell --bay home` and
// `bay agent --bay home` would reach `dock.FindBayByID("home")` and
// return the misleading "bay home not found in dock X".
func TestSurfaceAdd_RejectsHomeWithPhase2Guidance(t *testing.T) {
	eng, _ := testEngine(t)
	err := eng.SurfaceAdd(SurfaceAddOptions{
		DockName: "labs",
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	})
	if err == nil {
		t.Fatal("expected SurfaceAdd(home) to fail")
	}
	if !strings.Contains(err.Error(), "Phase 2") {
		t.Errorf("error = %v, want Phase 2 guidance", err)
	}
}

func TestEdit_RejectsHomeWithPhase2Guidance(t *testing.T) {
	eng, _ := testEngine(t)
	_, err := eng.Edit("labs", manifest.HomeBayID)
	if err == nil {
		t.Fatal("expected Edit(home) to fail")
	}
	if !strings.Contains(err.Error(), "Phase 2") {
		t.Errorf("error = %v, want Phase 2 guidance", err)
	}
}

// Engine-layer end-to-end resolver check: `<dock>:home` succeeds
// even with no persisted home entry, surfacing "home" as the bay
// ID for downstream callers.
func TestResolveBay_HomeSynthesizesEmpty(t *testing.T) {
	eng, _ := testEngine(t)

	dockName, bayID, err := eng.ResolveBay("labs:home")
	if err != nil {
		t.Fatalf("ResolveBay(labs:home) errored: %v", err)
	}
	if dockName != "labs" || bayID != manifest.HomeBayID {
		t.Errorf("resolved (%q, %q), want (labs, home)", dockName, bayID)
	}
}
