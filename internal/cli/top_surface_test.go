package cli

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

// --- runSurfaceNew ---

func TestRunSurfaceNew_Shell(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	err := runSurfaceNew(eng, "labs", "w1", surfaceNewOpts{
		Type:     manifest.SurfaceTypeShell,
		SplitDir: "v",
	})
	if err != nil {
		t.Fatalf("runSurfaceNew: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	// w1 was created with --shell so it has one shell already; the new one
	// should be the second.
	if len(ws.Surfaces) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(ws.Surfaces))
	}
	added := ws.Surfaces[1]
	if added.Type != manifest.SurfaceTypeShell {
		t.Errorf("type = %s, want shell", added.Type)
	}
	// uniqueSurfaceName disambiguates against the existing "shell".
	if !strings.HasPrefix(added.Name, "shell") {
		t.Errorf("name = %q, want a shell-prefixed name", added.Name)
	}
}

func TestRunSurfaceNew_Agent(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	err := runSurfaceNew(eng, "labs", "w1", surfaceNewOpts{
		Type:     manifest.SurfaceTypeAgent,
		Agent:    "claude",
		SplitDir: "v",
	})
	if err != nil {
		t.Fatalf("runSurfaceNew: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(ws.Surfaces))
	}
	added := ws.Surfaces[1]
	if added.Type != manifest.SurfaceTypeAgent {
		t.Errorf("type = %s, want agent", added.Type)
	}
	if added.Agent == nil || *added.Agent != "claude" {
		t.Errorf("agent = %v, want claude", added.Agent)
	}
}

func TestRunSurfaceNew_Cmd(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	err := runSurfaceNew(eng, "labs", "w1", surfaceNewOpts{
		Type:     manifest.SurfaceTypeCmd,
		Command:  "tail -f log.txt",
		Name:     "tail",
		SplitDir: "v",
	})
	if err != nil {
		t.Fatalf("runSurfaceNew: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	if len(ws.Surfaces) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(ws.Surfaces))
	}
	added := ws.Surfaces[1]
	if added.Type != manifest.SurfaceTypeCmd {
		t.Errorf("type = %s, want cmd", added.Type)
	}
	if added.Name != "tail" {
		t.Errorf("name = %q, want tail (explicit)", added.Name)
	}
	if added.Command == nil || *added.Command != "tail -f log.txt" {
		t.Errorf("command = %v, want tail -f log.txt", added.Command)
	}
}

func TestRunSurfaceNew_AgentDefaultName(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	// No opts.Name — should default to "agent".
	err := runSurfaceNew(eng, "labs", "w1", surfaceNewOpts{
		Type:     manifest.SurfaceTypeAgent,
		Agent:    "claude",
		SplitDir: "v",
	})
	if err != nil {
		t.Fatalf("runSurfaceNew: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	foundAgent := false
	for _, s := range ws.Surfaces {
		if s.Type == manifest.SurfaceTypeAgent && strings.HasPrefix(s.Name, "agent") {
			foundAgent = true
			break
		}
	}
	if !foundAgent {
		t.Error("expected an agent-typed surface with name prefix 'agent'")
	}
}

func TestRunSurfaceNew_CmdDefaultName(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	// No opts.Name — should default to "cmd".
	err := runSurfaceNew(eng, "labs", "w1", surfaceNewOpts{
		Type:     manifest.SurfaceTypeCmd,
		Command:  "tail -f log.txt",
		SplitDir: "v",
	})
	if err != nil {
		t.Fatalf("runSurfaceNew: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	foundCmd := false
	for _, s := range ws.Surfaces {
		if s.Type == manifest.SurfaceTypeCmd && strings.HasPrefix(s.Name, "cmd") {
			foundCmd = true
			break
		}
	}
	if !foundCmd {
		t.Error("expected a cmd-typed surface with name prefix 'cmd'")
	}
}

func TestRunSurfaceNew_NameWithColonIsRejected(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}

	err := runSurfaceNew(eng, "labs", "w1", surfaceNewOpts{
		Type:     manifest.SurfaceTypeShell,
		Name:     "w1:logs",
		SplitDir: "v",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot contain ':'") {
		t.Errorf("expected colon-rejection error, got %v", err)
	}
}

// --- editTargetFromArgs ---

func TestEditTargetFromArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		ws      string
		dock    string
		want    string
		wantErr bool
	}{
		{"no args no flags", nil, "", "", "self", false},
		{"positional only", []string{"auth-fix"}, "", "", "auth-fix", false},
		{"positional with dock prefix", []string{"labs:auth-fix"}, "", "", "labs:auth-fix", false},
		{"ws flag", nil, "auth-fix", "", "auth-fix", false},
		{"ws + dock flags", nil, "auth-fix", "labs", "labs:auth-fix", false},
		{"positional + ws flag", []string{"a"}, "b", "", "", true},
		{"positional + dock flag", []string{"a"}, "", "labs", "", true},
		{"dock flag alone", nil, "", "labs", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := editTargetFromArgs(c.args, c.ws, c.dock)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// --- runSurfaceClose ---

func TestRunSurfaceClose_ByName(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "claude", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	// Use qualified form to avoid depending on tmux ResolveSelf state.
	// force=true bypasses the agent close prompt — the prompt path is
	// covered by dedicated tests below.
	if err := runSurfaceClose(eng, []string{"w1:agent"}, "", "", true); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	for _, s := range ws.Surfaces {
		if s.Name == "agent" {
			t.Errorf("surface 'agent' should have been closed, still present")
		}
	}
}

func TestRunSurfaceClose_NoArgsErrors(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	// Bare invocation: no args, no flags. Should give the friendlier message
	// that mentions the 'self' keyword.
	err := runSurfaceClose(eng, nil, "", "", false)
	if err == nil || !strings.Contains(err.Error(), "specify a surface name") {
		t.Errorf("expected 'specify a surface name' error, got %v", err)
	}
	// Flags set but no positional: more specific message.
	err = runSurfaceClose(eng, nil, "w1", "", false)
	if err == nil || !strings.Contains(err.Error(), "--ws/--dock require") {
		t.Errorf("expected '--ws/--dock require' error with --ws, got %v", err)
	}
	err = runSurfaceClose(eng, nil, "", "labs", false)
	if err == nil || !strings.Contains(err.Error(), "--ws/--dock require") {
		t.Errorf("expected '--ws/--dock require' error with --dock, got %v", err)
	}
}

func TestRunSurfaceRestart_FlagsRequireName(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	// Same flag-without-name rule for restart.
	err := runSurfaceRestart(eng, nil, "w1", "")
	if err == nil || !strings.Contains(err.Error(), "--ws/--dock require") {
		t.Errorf("expected '--ws/--dock require' error, got %v", err)
	}
}

func TestRunSurfaceClose_CrossWorkspace(t *testing.T) {
	eng := surfaceArgFixture(t)

	// "labs:solo:agent" is a uniquely-named cross-workspace surface from the
	// fixture. Close it via the helper using bare ws:surface form.
	if err := runSurfaceClose(eng, []string{"solo:agent"}, "", "", true); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	ws, _ := eng.WsShow("labs", "solo")
	for _, s := range ws.Surfaces {
		if s.Name == "agent" {
			t.Errorf("agent should have been closed in labs:solo")
		}
	}
}

// --- self keyword end-to-end ---

// selfFixture (defined in target_test.go) builds a workspace with two
// surfaces and pins the tmux mock to the second one. Tests below reuse it.

func TestRunSurfaceClose_Self(t *testing.T) {
	eng := selfFixture(t)
	// Pane is pinned to "second" (a shell). bay close self should remove it.
	if err := runSurfaceClose(eng, []string{"self"}, "", "", true); err != nil {
		t.Fatalf("runSurfaceClose self: %v", err)
	}
	ws, _ := eng.WsShow("labs", "w1")
	for _, s := range ws.Surfaces {
		if s.Name == "second" {
			t.Errorf("surface 'second' should have been closed via 'self'")
		}
	}
}

func TestRunSurfaceClose_LiteralSelfWins(t *testing.T) {
	eng := selfFixture(t)
	// Add a literal surface named "self" — bay close self should target it.
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "self", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd self: %v", err)
	}

	if err := runSurfaceClose(eng, []string{"self"}, "", "", true); err != nil {
		t.Fatalf("runSurfaceClose self: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	hasLiteralSelf, hasSecond := false, false
	for _, s := range ws.Surfaces {
		if s.Name == "self" {
			hasLiteralSelf = true
		}
		if s.Name == "second" {
			hasSecond = true
		}
	}
	if hasLiteralSelf {
		t.Error("literal 'self' surface should have been closed")
	}
	if !hasSecond {
		t.Error("'second' (the current pane) should NOT have been closed when literal 'self' exists")
	}
}

func TestRunSurfaceRestart_NoArgsStillWorks(t *testing.T) {
	eng := selfFixture(t)
	// Bare bay restart should work — restart is non-destructive. Verify it
	// targets the current pane via RespawnPane.
	mockTmux := mockTmuxFromEngine(t, eng)
	ws, _ := eng.WsShow("labs", "w1")
	wantPaneID := ws.Surfaces[1].Tmux.PaneID

	mockTmux.Calls = nil
	if err := runSurfaceRestart(eng, nil, "", ""); err != nil {
		t.Fatalf("runSurfaceRestart with no args: %v", err)
	}
	if !mockHasRespawn(mockTmux, wantPaneID) {
		t.Errorf("expected RespawnPane on %q (the current pane), got calls: %v", wantPaneID, mockTmux.Calls)
	}
}

func TestRunSurfaceRestart_Self(t *testing.T) {
	eng := selfFixture(t)
	mockTmux := mockTmuxFromEngine(t, eng)
	ws, _ := eng.WsShow("labs", "w1")
	wantPaneID := ws.Surfaces[1].Tmux.PaneID

	mockTmux.Calls = nil
	if err := runSurfaceRestart(eng, []string{"self"}, "", ""); err != nil {
		t.Fatalf("runSurfaceRestart self: %v", err)
	}
	if !mockHasRespawn(mockTmux, wantPaneID) {
		t.Errorf("expected RespawnPane on %q, got calls: %v", wantPaneID, mockTmux.Calls)
	}
}

func TestRunSurfaceRestart_LiteralSelfWins(t *testing.T) {
	eng := selfFixture(t)
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeShell, Name: "self", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd self: %v", err)
	}
	mockTmux := mockTmuxFromEngine(t, eng)
	ws, _ := eng.WsShow("labs", "w1")
	// Find the literal "self" surface and grab its pane ID — that's what
	// should be restarted.
	var literalPaneID string
	for _, s := range ws.Surfaces {
		if s.Name == "self" {
			literalPaneID = s.Tmux.PaneID
		}
	}
	if literalPaneID == "" {
		t.Fatal("literal 'self' surface has no pane ID")
	}

	mockTmux.Calls = nil
	if err := runSurfaceRestart(eng, []string{"self"}, "", ""); err != nil {
		t.Fatalf("runSurfaceRestart self: %v", err)
	}
	if !mockHasRespawn(mockTmux, literalPaneID) {
		t.Errorf("expected RespawnPane on literal self %q, got calls: %v", literalPaneID, mockTmux.Calls)
	}
}

func TestRunSurfaceShow_Self(t *testing.T) {
	eng := selfFixture(t)
	if err := runSurfaceShow(eng, []string{"self"}, "", ""); err != nil {
		t.Errorf("runSurfaceShow self: %v", err)
	}
}

func TestRunSurfaceRename_Self(t *testing.T) {
	eng := selfFixture(t)
	// Rename "second" (the current pane's surface) to "renamed" via self keyword.
	if err := runSurfaceRename(eng, []string{"self", "renamed"}, "", ""); err != nil {
		t.Fatalf("runSurfaceRename self: %v", err)
	}
	ws, _ := eng.WsShow("labs", "w1")
	foundRenamed, foundSecond := false, false
	for _, s := range ws.Surfaces {
		if s.Name == "renamed" {
			foundRenamed = true
		}
		if s.Name == "second" {
			foundSecond = true
		}
	}
	if !foundRenamed {
		t.Error("renamed surface 'renamed' not found")
	}
	if foundSecond {
		t.Error("'second' should have been renamed away")
	}
}

// mockTmuxFromEngine extracts the tmux Mock from an engine. The engine's
// Tmux field is the Interface, but the test fixtures install a *tmux.Mock.
func mockTmuxFromEngine(t *testing.T, eng *engine.Engine) *tmux.Mock {
	t.Helper()
	m, ok := eng.Tmux.(*tmux.Mock)
	if !ok {
		t.Fatalf("engine tmux is not a *tmux.Mock")
	}
	return m
}

// mockHasRespawn returns true if mockTmux recorded a RespawnPane call on the
// given pane ID since its Calls slice was last reset.
func mockHasRespawn(mockTmux *tmux.Mock, paneID string) bool {
	for _, c := range mockTmux.Calls {
		if c.Method == "RespawnPane" && len(c.Args) > 0 && c.Args[0] == paneID {
			return true
		}
	}
	return false
}

// --- runSurfaceShow ---

func TestRunSurfaceShow_FoundAndNotFound(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "claude", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	// Found case: should not error.
	if err := runSurfaceShow(eng, []string{"w1:agent"}, "", ""); err != nil {
		t.Errorf("runSurfaceShow on existing surface: %v", err)
	}

	// Not found case: should error.
	err := runSurfaceShow(eng, []string{"w1:nonexistent"}, "", "")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

// --- runSurfaceRename ---

func TestRunSurfaceRename(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd(engine.SurfaceAddOptions{DockName: "labs", WsName: "w1", Type: manifest.SurfaceTypeAgent, Name: "agent", Agent: "claude", SplitDir: "v"}); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	if err := runSurfaceRename(eng, []string{"w1:agent", "agent2"}, "", ""); err != nil {
		t.Fatalf("runSurfaceRename: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	foundOld, foundNew := false, false
	for _, s := range ws.Surfaces {
		if s.Name == "agent" {
			foundOld = true
		}
		if s.Name == "agent2" {
			foundNew = true
		}
	}
	if foundOld {
		t.Error("surface 'agent' still present after rename")
	}
	if !foundNew {
		t.Error("surface 'agent2' not present after rename")
	}
}

// --- resolveSurfaceWorkspace ---

func TestResolveSurfaceWorkspace_NoFlagsUsesSelf(t *testing.T) {
	eng, mockTmux, _, _ := testNavEngine(t)
	ws, _ := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true})
	mockTmux.SetCurrentWindowID(ws.Surfaces[0].Tmux.WindowID)
	mockTmux.SetCurrentPaneID(ws.Surfaces[0].Tmux.PaneID)

	dock, wsName, err := resolveSurfaceWorkspace(eng, "", "")
	if err != nil {
		t.Fatalf("resolveSurfaceWorkspace: %v", err)
	}
	if dock != "labs" || wsName != "w1" {
		t.Errorf("got (%q,%q), want (labs,w1)", dock, wsName)
	}
}

func TestResolveSurfaceWorkspace_WsFlag(t *testing.T) {
	eng := wsArgFixture(t)

	dock, wsName, err := resolveSurfaceWorkspace(eng, "solo", "")
	if err != nil {
		t.Fatalf("resolveSurfaceWorkspace: %v", err)
	}
	if dock != "labs" || wsName != "solo" {
		t.Errorf("got (%q,%q), want (labs,solo)", dock, wsName)
	}
}

func TestResolveSurfaceWorkspace_DockWithoutWs(t *testing.T) {
	eng, _, _, _ := testNavEngine(t)
	_, _, err := resolveSurfaceWorkspace(eng, "", "labs")
	if err == nil || !strings.Contains(err.Error(), "--ws") {
		t.Errorf("expected --dock-requires-ws error, got %v", err)
	}
}

// --- command tree wiring ---

func TestRoot_SurfaceGroupExists(t *testing.T) {
	root := NewRootCmd("test")

	groups := root.Groups()
	var hasSurface, hasWorkspace bool
	for _, g := range groups {
		if g.ID == "surface" {
			hasSurface = true
		}
		if g.ID == "workspace" {
			hasWorkspace = true
		}
	}
	if !hasSurface {
		t.Error("expected 'surface' group to be defined on root")
	}
	if !hasWorkspace {
		t.Error("expected 'workspace' group to be defined on root")
	}
}

func TestRoot_TopLevelSurfaceVerbsRegistered(t *testing.T) {
	root := NewRootCmd("test")

	surfaceVerbs := []string{"new", "close", "show"}
	for _, name := range surfaceVerbs {
		c, _, err := root.Find([]string{name})
		if err != nil {
			t.Errorf("could not find top-level command %q: %v", name, err)
			continue
		}
		if c.Name() != name {
			t.Errorf("Find(%q) returned %q", name, c.Name())
		}
		if c.GroupID != "surface" {
			t.Errorf("command %q has GroupID %q, want surface", name, c.GroupID)
		}
	}

	// bay rename targets workspaces, not surfaces.
	renameCmd, _, err := root.Find([]string{"rename"})
	if err != nil {
		t.Errorf("could not find top-level command rename: %v", err)
	} else if renameCmd.GroupID != "workspace" {
		t.Errorf("command rename has GroupID %q, want workspace", renameCmd.GroupID)
	}
}

func TestRoot_HiddenAliasesResolve(t *testing.T) {
	root := NewRootCmd("test")

	cases := []struct {
		alias  string
		target string
	}{
		{"rm", "close"},
		{"cat", "show"},
		{"mv", "rename"},
	}
	for _, c := range cases {
		cmd, _, err := root.Find([]string{c.alias})
		if err != nil {
			t.Errorf("could not find alias %q: %v", c.alias, err)
			continue
		}
		if cmd.Name() != c.target {
			t.Errorf("alias %q resolved to %q, want %q", c.alias, cmd.Name(), c.target)
		}
	}
}

func TestRoot_BayNewSubcommands(t *testing.T) {
	root := NewRootCmd("test")

	newCmd, _, err := root.Find([]string{"new"})
	if err != nil {
		t.Fatalf("could not find 'new': %v", err)
	}

	want := []string{"shell", "agent", "cmd", "edit"}
	have := map[string]bool{}
	for _, c := range newCmd.Commands() {
		have[c.Name()] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("bay new is missing subcommand %q", name)
		}
	}
}

// --- existing surface commands moved to surface group ---

func TestRoot_SurfaceFlavoredCommandsInSurfaceGroup(t *testing.T) {
	root := NewRootCmd("test")

	want := []string{"surface", "shell", "edit", "restart"}
	for _, name := range want {
		c, _, err := root.Find([]string{name})
		if err != nil {
			t.Errorf("could not find %q: %v", name, err)
			continue
		}
		if c.GroupID != "surface" {
			t.Errorf("command %q has GroupID %q, want surface", name, c.GroupID)
		}
	}
}

func TestRoot_WsStaysInWorkspaceGroup(t *testing.T) {
	root := NewRootCmd("test")

	c, _, err := root.Find([]string{"ws"})
	if err != nil {
		t.Fatalf("could not find ws: %v", err)
	}
	if c.GroupID != "workspace" {
		t.Errorf("ws GroupID = %q, want workspace", c.GroupID)
	}
}
