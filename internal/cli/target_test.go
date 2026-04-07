package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// twoDockFixture extends testNavEngine with a second repo "labs2" and dock
// "labs2" so tests can exercise cross-dock ambiguity and dock-qualified names.
func twoDockFixture(t *testing.T) *engine.Engine {
	t.Helper()
	eng, _, _, dir := testNavEngine(t)

	labs2Dir := filepath.Join(dir, "repos", "labs2")
	if err := os.MkdirAll(labs2Dir, 0o755); err != nil {
		t.Fatalf("mkdir labs2 repo: %v", err)
	}
	if err := eng.RepoAdd("labs2", labs2Dir, "", "", true); err != nil {
		t.Fatalf("RepoAdd labs2: %v", err)
	}
	if err := eng.DockNew("labs2", "labs2", "claude", ""); err != nil {
		t.Fatalf("DockNew labs2: %v", err)
	}
	return eng
}

// --- parseSurfaceArg ---

func TestParseSurfaceArg(t *testing.T) {
	cases := []struct {
		in                string
		dock, ws, surface string
		wantErr           bool
	}{
		{"agent", "", "", "agent", false},
		{"w1:agent", "", "w1", "agent", false},
		{"labs:w1:agent", "labs", "w1", "agent", false},
		{"a:b:c:d", "", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			dock, ws, surface, err := parseSurfaceArg(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if dock != c.dock || ws != c.ws || surface != c.surface {
				t.Errorf("got (%q,%q,%q), want (%q,%q,%q)",
					dock, ws, surface, c.dock, c.ws, c.surface)
			}
		})
	}
}

// --- parseWsArg ---

func TestParseWsArg(t *testing.T) {
	cases := []struct {
		in       string
		dock, ws string
		wantErr  bool
	}{
		{"w1", "", "w1", false},
		{"labs:w1", "labs", "w1", false},
		{"a:b:c", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			dock, ws, err := parseWsArg(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if dock != c.dock || ws != c.ws {
				t.Errorf("got (%q,%q), want (%q,%q)", dock, ws, c.dock, c.ws)
			}
		})
	}
}

// --- resolveSurfaceArg ---

// surfaceArgFixture creates two docks each with a workspace and an "agent"
// surface, so we can test bare/qualified resolution and conflicts.
func surfaceArgFixture(t *testing.T) *engine.Engine {
	t.Helper()
	eng := twoDockFixture(t)

	// labs:w1 with default shell + extra agent surface
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew labs:w1: %v", err)
	}
	if err := eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeAgent, "agent", "claude", "", "v"); err != nil {
		t.Fatalf("SurfaceAdd labs:w1:agent: %v", err)
	}

	// labs2:w1 with the same surface — bare ws name "w1" is now ambiguous.
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs2", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew labs2:w1: %v", err)
	}
	if err := eng.SurfaceAdd("labs2", "w1", manifest.SurfaceTypeAgent, "agent", "claude", "", "v"); err != nil {
		t.Fatalf("SurfaceAdd labs2:w1:agent: %v", err)
	}

	// Also create a uniquely-named workspace so bare resolution works.
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "solo", Shell: true}); err != nil {
		t.Fatalf("WsNew labs:solo: %v", err)
	}
	if err := eng.SurfaceAdd("labs", "solo", manifest.SurfaceTypeAgent, "agent", "claude", "", "v"); err != nil {
		t.Fatalf("SurfaceAdd labs:solo:agent: %v", err)
	}

	return eng
}

func TestResolveSurfaceArg_FullyQualified(t *testing.T) {
	eng := surfaceArgFixture(t)

	dock, ws, surface, err := resolveSurfaceArg(eng, "labs:w1:agent", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArg: %v", err)
	}
	if dock != "labs" || ws != "w1" || surface != "agent" {
		t.Errorf("got (%q,%q,%q), want (labs,w1,agent)", dock, ws, surface)
	}
}

func TestResolveSurfaceArg_WsQualified(t *testing.T) {
	eng := surfaceArgFixture(t)

	// "solo" is uniquely named so bare ws lookup succeeds.
	dock, ws, surface, err := resolveSurfaceArg(eng, "solo:agent", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArg: %v", err)
	}
	if dock != "labs" || ws != "solo" || surface != "agent" {
		t.Errorf("got (%q,%q,%q), want (labs,solo,agent)", dock, ws, surface)
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
	dock, ws, surface, err := resolveSurfaceArg(eng, "w1:agent", "", "labs2")
	if err != nil {
		t.Fatalf("resolveSurfaceArg: %v", err)
	}
	if dock != "labs2" || ws != "w1" || surface != "agent" {
		t.Errorf("got (%q,%q,%q), want (labs2,w1,agent)", dock, ws, surface)
	}
}

func TestResolveSurfaceArg_FlagsAlone(t *testing.T) {
	eng := surfaceArgFixture(t)

	// Bare surface name + --ws + --dock.
	dock, ws, surface, err := resolveSurfaceArg(eng, "agent", "w1", "labs2")
	if err != nil {
		t.Fatalf("resolveSurfaceArg: %v", err)
	}
	if dock != "labs2" || ws != "w1" || surface != "agent" {
		t.Errorf("got (%q,%q,%q), want (labs2,w1,agent)", dock, ws, surface)
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

func TestResolveSurfaceArg_WsFlagConflict(t *testing.T) {
	eng := surfaceArgFixture(t)

	// --ws conflicts with ws prefix in the positional.
	_, _, _, err := resolveSurfaceArg(eng, "w1:agent", "other", "")
	if err == nil || !strings.Contains(err.Error(), "--ws") {
		t.Errorf("expected --ws conflict, got %v", err)
	}
}

func TestResolveSurfaceArg_DockFlagWithoutWs(t *testing.T) {
	eng := surfaceArgFixture(t)

	// --dock without --ws or ws prefix is meaningless.
	_, _, _, err := resolveSurfaceArg(eng, "agent", "", "labs")
	if err == nil {
		t.Errorf("expected error when --dock has no workspace context")
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
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew labs:w1: %v", err)
	}
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs2", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew labs2:w1: %v", err)
	}

	// Unique ws name for bare resolution.
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "solo", Shell: true}); err != nil {
		t.Fatalf("WsNew labs:solo: %v", err)
	}

	return eng
}

func TestResolveWsArg_BareUnique(t *testing.T) {
	eng := wsArgFixture(t)

	dock, ws, err := resolveWsArg(eng, "solo", "")
	if err != nil {
		t.Fatalf("resolveWsArg: %v", err)
	}
	if dock != "labs" || ws != "solo" {
		t.Errorf("got (%q,%q), want (labs,solo)", dock, ws)
	}
}

func TestResolveWsArg_DockQualified(t *testing.T) {
	eng := wsArgFixture(t)

	dock, ws, err := resolveWsArg(eng, "labs2:w1", "")
	if err != nil {
		t.Fatalf("resolveWsArg: %v", err)
	}
	if dock != "labs2" || ws != "w1" {
		t.Errorf("got (%q,%q), want (labs2,w1)", dock, ws)
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

	dock, ws, err := resolveWsArg(eng, "w1", "labs2")
	if err != nil {
		t.Fatalf("resolveWsArg: %v", err)
	}
	if dock != "labs2" || ws != "w1" {
		t.Errorf("got (%q,%q), want (labs2,w1)", dock, ws)
	}
}

func TestResolveWsArg_DockFlagConflict(t *testing.T) {
	eng := wsArgFixture(t)

	_, _, err := resolveWsArg(eng, "labs:w1", "labs2")
	if err == nil || !strings.Contains(err.Error(), "--dock") {
		t.Errorf("expected --dock conflict, got %v", err)
	}
}

// --- resolveSurfaceArgOrSelf ---

// selfFixture builds a workspace with two surfaces and pins the tmux mock to
// the second one. The "current pane" therefore maps to surface "second".
func selfFixture(t *testing.T) *engine.Engine {
	t.Helper()
	eng, mockTmux, _, _ := testNavEngine(t)

	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeShell, "second", "", "", "v"); err != nil {
		t.Fatalf("SurfaceAdd second: %v", err)
	}
	ws, _ := eng.WsShow("labs", "w1")
	// Pin tmux context to the "second" surface (index 1).
	mockTmux.SetCurrentWindowID(ws.Surfaces[1].Tmux.WindowID)
	mockTmux.SetCurrentPaneID(ws.Surfaces[1].Tmux.PaneID)

	return eng
}

func TestResolveSurfaceArgOrSelf_VirtualSelf(t *testing.T) {
	eng := selfFixture(t)

	dock, ws, surface, err := resolveSurfaceArgOrSelf(eng, "self", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArgOrSelf: %v", err)
	}
	if dock != "labs" || ws != "w1" || surface != "second" {
		t.Errorf("got (%q,%q,%q), want (labs,w1,second)", dock, ws, surface)
	}
}

func TestResolveSurfaceArgOrSelf_LiteralWinsOverVirtual(t *testing.T) {
	eng := selfFixture(t)
	// Add a literal surface named "self" — should win over the virtual lookup.
	if err := eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeShell, "self", "", "", "v"); err != nil {
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
	// --ws set means "self" is a literal name, not the virtual keyword.
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
	dock, ws, surface, err := resolveSurfaceArgOrSelf(eng, "w1:second", "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceArgOrSelf: %v", err)
	}
	if dock != "labs" || ws != "w1" || surface != "second" {
		t.Errorf("got (%q,%q,%q), want (labs,w1,second)", dock, ws, surface)
	}
}
