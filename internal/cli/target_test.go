package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// twoDockFixture extends testNavEngine with a second checkout and dock
// "labs2" so tests can exercise cross-dock ambiguity and dock-qualified names.
func twoDockFixture(t *testing.T) *engine.Engine {
	t.Helper()
	eng, _, _, dir := testNavEngine(t)

	labs2Dir := filepath.Join(dir, "repos", "labs2")
	if err := os.MkdirAll(filepath.Join(labs2Dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir labs2 checkout: %v", err)
	}
	if err := eng.DockNew("labs2", labs2Dir, "", "claude", ""); err != nil {
		t.Fatalf("DockNew labs2: %v", err)
	}
	return eng
}

// --- parseSurfaceArg ---

func TestParseSurfaceArg(t *testing.T) {
	cases := []struct {
		in                 string
		dock, bay, surface string
		wantErr            bool
	}{
		{"agent", "", "", "agent", false},
		{"w1:agent", "", "w1", "agent", false},
		{"labs:w1:agent", "labs", "w1", "agent", false},
		{"a:b:c:d", "", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			dock, bay, surface, err := parseSurfaceArg(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if dock != c.dock || bay != c.bay || surface != c.surface {
				t.Errorf("got (%q,%q,%q), want (%q,%q,%q)",
					dock, bay, surface, c.dock, c.bay, c.surface)
			}
		})
	}
}

// --- parseWsArg ---

func TestParseWsArg(t *testing.T) {
	cases := []struct {
		in        string
		dock, bay string
		wantErr   bool
	}{
		{"w1", "", "w1", false},
		{"labs:w1", "labs", "w1", false},
		{"a:b:c", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			dock, bay, err := parseWsArg(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if dock != c.dock || bay != c.bay {
				t.Errorf("got (%q,%q), want (%q,%q)", dock, bay, c.dock, c.bay)
			}
		})
	}
}

// --- resolveSurfaceArg ---

// surfaceArgFixture creates two docks each with a bay and an "agent"
// surface, so we can test bare/qualified resolution and conflicts.
func surfaceArgFixture(t *testing.T) *engine.Engine {
	t.Helper()
	eng := twoDockFixture(t)

	// labs:w1 with default shell + extra agent surface
	if _, err := eng.BayNew(engine.BayNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew labs:w1: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "claude", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd labs:w1:agent: %v", err)
	}

	// labs2:w1 with the same surface — bare ws name "w1" is now ambiguous.
	if _, err := eng.BayNew(engine.BayNewOptions{Dock: "labs2", Shell: true}); err != nil {
		t.Fatalf("WsNew labs2:w1: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs2", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "claude", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd labs2:w1:agent: %v", err)
	}

	// Also create a uniquely-named bay so bare resolution works.
	// Bay's ID is w2 (auto-assigned, since solo is the second labs ws).
	if _, err := eng.BayNew(engine.BayNewOptions{Dock: "labs", Name: "solo", Shell: true}); err != nil {
		t.Fatalf("WsNew labs:solo: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w2", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "claude", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd labs:w2:agent: %v", err)
	}

	return eng
}

func TestResolveSurfaceArg_FullyQualified(t *testing.T) {
	eng := surfaceArgFixture(t)

	dock, bay, surface, err := resolveSurfaceArg(eng, "labs:w1:agent", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArg: %v", err)
	}
	if dock != "labs" || bay != "w1" || surface != "agent" {
		t.Errorf("got (%q,%q,%q), want (labs,w1,agent)", dock, bay, surface)
	}
}

func TestResolveSurfaceArg_WsQualified(t *testing.T) {
	eng := surfaceArgFixture(t)

	// w2 is uniquely owned by labs, so bare ID lookup succeeds.
	dock, bay, surface, err := resolveSurfaceArg(eng, "w2:agent", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArg: %v", err)
	}
	if dock != "labs" || bay != "w2" || surface != "agent" {
		t.Errorf("got (%q,%q,%q), want (labs,w2,agent)", dock, bay, surface)
	}
}

func TestResolveSurfaceArg_AmbiguousWs(t *testing.T) {
	eng := surfaceArgFixture(t)

	// "w1" exists in both labs and labs2.
	_, _, _, err := resolveSurfaceArg(eng, "w1:agent", "", "")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous error, got %v", err)
	}
}

func TestResolveSurfaceArg_FlagsResolveAmbiguity(t *testing.T) {
	eng := surfaceArgFixture(t)

	// Use --dock to disambiguate the bare "w1".
	dock, bay, surface, err := resolveSurfaceArg(eng, "w1:agent", "", "labs2")
	if err != nil {
		t.Fatalf("resolveSurfaceArg: %v", err)
	}
	if dock != "labs2" || bay != "w1" || surface != "agent" {
		t.Errorf("got (%q,%q,%q), want (labs2,w1,agent)", dock, bay, surface)
	}
}

func TestResolveSurfaceArg_FlagsAlone(t *testing.T) {
	eng := surfaceArgFixture(t)

	// Bare surface name + --bay + --dock.
	dock, bay, surface, err := resolveSurfaceArg(eng, "agent", "w1", "labs2")
	if err != nil {
		t.Fatalf("resolveSurfaceArg: %v", err)
	}
	if dock != "labs2" || bay != "w1" || surface != "agent" {
		t.Errorf("got (%q,%q,%q), want (labs2,w1,agent)", dock, bay, surface)
	}
}

func TestResolveSurfaceArg_DockFlagConflict(t *testing.T) {
	eng := surfaceArgFixture(t)

	// --dock conflicts with dock prefix in the positional.
	_, _, _, err := resolveSurfaceArg(eng, "labs:w1:agent", "", "labs2")
	if err == nil || !strings.Contains(err.Error(), "--dock") {
		t.Errorf("expected --dock conflict, got %v", err)
	}
}

func TestResolveSurfaceArg_BayFlagConflict(t *testing.T) {
	eng := surfaceArgFixture(t)

	// --bay conflicts with bay prefix in the positional.
	_, _, _, err := resolveSurfaceArg(eng, "w1:agent", "other", "")
	if err == nil || !strings.Contains(err.Error(), "--bay") {
		t.Errorf("expected --bay conflict, got %v", err)
	}
}

func TestResolveSurfaceArg_DockFlagWithoutWs(t *testing.T) {
	eng := surfaceArgFixture(t)

	// --dock without --bay or bay prefix is meaningless.
	_, _, _, err := resolveSurfaceArg(eng, "agent", "", "labs")
	if err == nil {
		t.Errorf("expected error when --dock has no bay context")
	}
}

func TestResolveSurfaceArg_TooManyParts(t *testing.T) {
	eng := surfaceArgFixture(t)

	_, _, _, err := resolveSurfaceArg(eng, "a:b:c:d", "", "")
	if err == nil {
		t.Errorf("expected parse error for 4-part input")
	}
}

// --- resolveWsArg ---

func wsArgFixture(t *testing.T) *engine.Engine {
	t.Helper()
	eng := twoDockFixture(t)

	// Ambiguous bare ws name.
	if _, err := eng.BayNew(engine.BayNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew labs:w1: %v", err)
	}
	if _, err := eng.BayNew(engine.BayNewOptions{Dock: "labs2", Shell: true}); err != nil {
		t.Fatalf("WsNew labs2:w1: %v", err)
	}

	// Unique ws name for bare resolution.
	if _, err := eng.BayNew(engine.BayNewOptions{Dock: "labs", Name: "solo", Shell: true}); err != nil {
		t.Fatalf("WsNew labs:solo: %v", err)
	}

	return eng
}

func TestResolveWsArg_BareUnique(t *testing.T) {
	eng := wsArgFixture(t)

	// w2 is unique to labs (labs has ID w1 and w2; labs2 has only w1).
	dock, bay, err := resolveWsArg(eng, "w2", "")
	if err != nil {
		t.Fatalf("resolveWsArg: %v", err)
	}
	if dock != "labs" || bay != "w2" {
		t.Errorf("got (%q,%q), want (labs,w2)", dock, bay)
	}
}

func TestResolveWsArg_DockQualified(t *testing.T) {
	eng := wsArgFixture(t)

	dock, bay, err := resolveWsArg(eng, "labs2:w1", "")
	if err != nil {
		t.Fatalf("resolveWsArg: %v", err)
	}
	if dock != "labs2" || bay != "w1" {
		t.Errorf("got (%q,%q), want (labs2,w1)", dock, bay)
	}
}

func TestResolveWsArg_BareAmbiguous(t *testing.T) {
	eng := wsArgFixture(t)

	_, _, err := resolveWsArg(eng, "w1", "")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous error, got %v", err)
	}
}

func TestResolveWsArg_DockFlagDisambiguates(t *testing.T) {
	eng := wsArgFixture(t)

	dock, bay, err := resolveWsArg(eng, "w1", "labs2")
	if err != nil {
		t.Fatalf("resolveWsArg: %v", err)
	}
	if dock != "labs2" || bay != "w1" {
		t.Errorf("got (%q,%q), want (labs2,w1)", dock, bay)
	}
}

func TestResolveWsArg_DockFlagConflict(t *testing.T) {
	eng := wsArgFixture(t)

	_, _, err := resolveWsArg(eng, "labs:w1", "labs2")
	if err == nil || !strings.Contains(err.Error(), "--dock") {
		t.Errorf("expected --dock conflict, got %v", err)
	}
}

// chdirTo enters the given bay path so CurrentContext resolves to
// its dock. Restores cwd on test cleanup.
func chdirTo(t *testing.T, eng *engine.Engine, dockName, bayName string) {
	t.Helper()
	bay, err := eng.BayShow(dockName, bayName)
	if err != nil {
		t.Fatalf("WsShow %s:%s: %v", dockName, bayName, err)
	}
	if err := os.MkdirAll(bay.Path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", bay.Path, err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(bay.Path); err != nil {
		t.Fatalf("Chdir %s: %v", bay.Path, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestResolveWsArg_BareAmbiguousPrefersCurrentDock(t *testing.T) {
	// Both labs and labs2 have a bay with ID "w1". From inside
	// labs:w2 (the "solo" bay), a bare `w1` should resolve to labs:w1,
	// not error.
	eng := wsArgFixture(t)
	chdirTo(t, eng, "labs", "w2")

	dock, bay, err := resolveWsArg(eng, "w1", "")
	if err != nil {
		t.Fatalf("resolveWsArg: %v", err)
	}
	if dock != "labs" || bay != "w1" {
		t.Errorf("got (%q,%q), want (labs,w1)", dock, bay)
	}
}

func TestResolveWsArg_BareFallsThroughWhenCurrentDockLacksIt(t *testing.T) {
	// labs2 has no w2. From inside labs2:w1, a bare `w2` should still
	// resolve — the current-dock shortcut falls through to the all-dock
	// search when it misses.
	eng := wsArgFixture(t)
	chdirTo(t, eng, "labs2", "w1")

	dock, bay, err := resolveWsArg(eng, "w2", "")
	if err != nil {
		t.Fatalf("resolveWsArg: %v", err)
	}
	if dock != "labs" || bay != "w2" {
		t.Errorf("got (%q,%q), want (labs,w2)", dock, bay)
	}
}

func TestResolveWsArg_BareAmbiguousWhenCurrentDockLacksIt(t *testing.T) {
	// Without a current-dock context (cwd is outside every bay),
	// a bare `w1` should fall through to the manifest-wide search and
	// surface the cross-dock ambiguity (labs:w1 and labs2:w1).
	eng := wsArgFixture(t)
	// Make sure CurrentContext won't pick up any ambient bay bay.
	dir := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	_, _, err := resolveWsArg(eng, "w1", "")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous error, got %v", err)
	}
}

// --- resolveSurfaceArgOrSelf ---

// selfFixture builds a bay with two surfaces and pins the tmux mock to
// the second one. The "current pane" therefore maps to surface "second".
func selfFixture(t *testing.T) *engine.Engine {
	t.Helper()
	eng, mockTmux, _, _ := testNavEngine(t)

	if _, err := eng.BayNew(engine.BayNewOptions{Dock: "labs", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "second", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd second: %v", err)
	}
	bay, _ := eng.BayShow("labs", "w1")
	// Pin tmux context to the "second" surface (index 1).
	mockTmux.SetCurrentWindowID(bay.Surfaces[1].Tmux.WindowID)
	mockTmux.SetCurrentPaneID(bay.Surfaces[1].Tmux.PaneID)

	return eng
}

func TestResolveSurfaceArgOrSelf_VirtualSelf(t *testing.T) {
	eng := selfFixture(t)

	dock, bay, surface, err := resolveSurfaceArgOrSelf(eng, "self", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArgOrSelf: %v", err)
	}
	if dock != "labs" || bay != "w1" || surface != "second" {
		t.Errorf("got (%q,%q,%q), want (labs,w1,second)", dock, bay, surface)
	}
}

func TestResolveSurfaceArgOrSelf_LiteralWinsOverVirtual(t *testing.T) {
	eng := selfFixture(t)
	// Add a literal surface named "self" — should win over the virtual lookup.
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "self", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd self: %v", err)
	}

	_, _, surface, err := resolveSurfaceArgOrSelf(eng, "self", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArgOrSelf: %v", err)
	}
	if surface != "self" {
		t.Errorf("surface = %q, want literal 'self'", surface)
	}
}

func TestResolveSurfaceArgOrSelf_QualifiedSelfIsAlwaysLiteral(t *testing.T) {
	eng := selfFixture(t)
	// Qualified form must NOT silently substitute the current pane's surface.
	// It should return the literal "self" name regardless of whether such a
	// surface exists; the engine call would error later if not.
	_, _, surface, err := resolveSurfaceArgOrSelf(eng, "w1:self", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArgOrSelf: %v", err)
	}
	if surface != "self" {
		t.Errorf("surface = %q, want literal 'self' (no virtual fallback for qualified)", surface)
	}
}

func TestResolveSurfaceArgOrSelf_FlagDisablesSelfFallback(t *testing.T) {
	eng := selfFixture(t)
	// --bay set means "self" is a literal name, not the virtual keyword.
	_, _, surface, err := resolveSurfaceArgOrSelf(eng, "self", "w1", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArgOrSelf: %v", err)
	}
	if surface != "self" {
		t.Errorf("surface = %q, want literal 'self' (flags disable virtual fallback)", surface)
	}
}

func TestResolveSurfaceArgOrSelf_NonSelfDelegates(t *testing.T) {
	eng := selfFixture(t)
	// Any non-"self" positional should behave exactly like resolveSurfaceArg.
	dock, bay, surface, err := resolveSurfaceArgOrSelf(eng, "w1:second", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArgOrSelf: %v", err)
	}
	if dock != "labs" || bay != "w1" || surface != "second" {
		t.Errorf("got (%q,%q,%q), want (labs,w1,second)", dock, bay, surface)
	}
}

// TestResolveWsArg_NameRejectedWithHint covers the strict resolver's
// user-facing behavior: typing a friendly Name produces an error that
// names the canonical ID, so the user can copy it directly.
func TestResolveWsArg_NameRejectedWithHint(t *testing.T) {
	eng := wsArgFixture(t)
	// "solo" is the Name the user might type; its ID is w2 in labs.
	_, _, err := resolveWsArg(eng, "solo", "")
	if err == nil {
		t.Fatal("expected error: Name lookup should fail under strict resolver")
	}
	if !strings.Contains(err.Error(), `did you mean "w2"`) {
		t.Errorf("error should hint at canonical ID; got %v", err)
	}
}
