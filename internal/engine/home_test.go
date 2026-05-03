package engine

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// Bespoke guidance, not the generic ValidateBayName message:
// accepting "home" would suggest bay was creating a new worktree at
// dock.path. Also asserts no worktree is created — the rejection
// must short-circuit before any disk side effects.
func TestBayNew_RejectsHomeName(t *testing.T) {
	eng, _ := testEngine(t)

	_, err := eng.BayNew(BayNewOptions{Dock: "labs", Name: manifest.HomeBayID, Shell: true})
	if err == nil {
		t.Fatal("expected BayNew(name=home) to fail")
	}
	if !strings.Contains(err.Error(), "reserved for the dock checkout") {
		t.Errorf("error = %v, want reserved-checkout guidance", err)
	}

	// Side-effect check: no worktree was created.
	mockGit := eng.Git.(*git.Mock)
	if got := len(mockGit.CreatedWorktrees()); got != 0 {
		t.Fatalf("created worktrees = %d, want 0 (home rejection must short-circuit)", got)
	}
}

func TestBayRename_RejectsHomeBay(t *testing.T) {
	eng, _ := testEngine(t)
	if err := eng.BayRename("labs", manifest.HomeBayID, "anything"); err == nil {
		t.Fatal("expected BayRename(home) to fail")
	}
}

func TestBayRename_RejectsRenamingToHome(t *testing.T) {
	eng, _ := testEngine(t)

	bay, err := eng.BayNew(BayNewOptions{Dock: "labs", Shell: true})
	if err != nil {
		t.Fatalf("seed bay: %v", err)
	}

	err = eng.BayRename("labs", bay.ID, manifest.HomeBayID)
	if err == nil {
		t.Fatal("expected BayRename(target=home) to fail")
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

// TestBayClose_HomeIsNoOp covers the canonical-checkout invariant:
// closing home with no surfaces is a safe no-op and never reaches the
// worktree-removal path. Step 2 will materialize home surfaces; until
// then this contract guards dock.Path against accidental deletion.
func TestBayClose_HomeIsNoOp(t *testing.T) {
	eng, _ := testEngine(t)

	// Sanity: dock.Path exists and is non-empty in the test fixture.
	m, _ := eng.LoadManifest()
	dock := m.FindDock("labs")
	if dock == nil || dock.Path == "" {
		t.Fatal("test fixture missing labs dock with checkout path")
	}
	dockPath := dock.Path

	if err := eng.BayClose("labs", manifest.HomeBayID, false); err != nil {
		t.Fatalf("BayClose(labs, home) errored: %v", err)
	}

	// Verify dock.Path is untouched in the manifest (no removal, no rewrite).
	m2, _ := eng.LoadManifest()
	dock2 := m2.FindDock("labs")
	if dock2 == nil {
		t.Fatal("dock disappeared after closing home")
	}
	if dock2.Path != dockPath {
		t.Errorf("dock.Path = %q, want %q (close must never rewrite the canonical checkout)", dock2.Path, dockPath)
	}
}

// TestResolveBay_HomeSynthesizesEmpty is the engine-layer end-to-end
// resolver check: resolving "labs:home" succeeds without a persisted
// home entry, and surfaces "home" as the bay ID for downstream callers.
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
