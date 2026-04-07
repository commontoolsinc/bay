package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// --- agentClosePrompt unit tests ---

func TestAgentClosePrompt(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"y", "y\n", true},
		{"Y", "Y\n", true},
		{"yes", "yes\n", true},
		{"YES", "YES\n", true},
		{"yes with whitespace", "  yes  \n", true},
		{"n", "n\n", false},
		{"no", "no\n", false},
		{"empty (just enter)", "\n", false},
		{"EOF", "", false},
		{"random", "maybe\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			got := agentClosePrompt(strings.NewReader(c.input), &out, "claude")
			if got != c.want {
				t.Errorf("agentClosePrompt(%q) = %v, want %v", c.input, got, c.want)
			}
			// The prompt text should contain the surface name.
			if !strings.Contains(out.String(), "claude") {
				t.Errorf("prompt output should mention surface name: %q", out.String())
			}
		})
	}
}

// --- runSurfaceClose force / non-agent paths ---

func TestRunSurfaceClose_ForceClosesAgentWithoutPrompt(t *testing.T) {
	// force=true should close an agent surface without any prompt logic
	// running. (Tests can't easily simulate a TTY, but the contract is
	// "force always wins regardless of TTY state".)
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeAgent, "agent", "claude", "", "v"); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	if err := runSurfaceClose(eng, []string{"w1:agent"}, "", "", true); err != nil {
		t.Fatalf("runSurfaceClose force: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	for _, s := range ws.Surfaces {
		if s.Name == "agent" {
			t.Errorf("agent surface should be closed when force=true")
		}
	}
}

func TestRunSurfaceClose_NonAgentSkipsPrompt(t *testing.T) {
	// Closing a non-agent surface (a shell, in this case) should never
	// trigger the prompt path, even with force=false. We can't easily
	// verify "no prompt was shown" without injecting the prompt fn, but
	// we can verify the close succeeds without hanging or erroring.
	eng, _, _, _ := testNavEngine(t)
	if _, err := eng.WsNew(engine.WsNewOptions{Dock: "labs", Name: "w1", Shell: true}); err != nil {
		t.Fatalf("WsNew: %v", err)
	}
	if err := eng.SurfaceAdd("labs", "w1", manifest.SurfaceTypeShell, "extra", "", "", "v"); err != nil {
		t.Fatalf("SurfaceAdd: %v", err)
	}

	if err := runSurfaceClose(eng, []string{"w1:extra"}, "", "", false); err != nil {
		t.Fatalf("runSurfaceClose: %v", err)
	}

	ws, _ := eng.WsShow("labs", "w1")
	for _, s := range ws.Surfaces {
		if s.Name == "extra" {
			t.Errorf("extra shell surface should be closed (no prompt for non-agent)")
		}
	}
}
