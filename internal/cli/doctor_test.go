package cli

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

func TestMissingKeybindings(t *testing.T) {
	content := strings.Join(tmuxKeybindingLines()[:2], "\n")
	missing := missingKeybindings(content)
	if len(missing) != len(tmuxKeybindingLines())-2 {
		t.Fatalf("missing = %d, want %d", len(missing), len(tmuxKeybindingLines())-2)
	}
}

func TestKeybindingsIncludeSurfaceNavigation(t *testing.T) {
	lines := tmuxKeybindingLines()
	joined := strings.Join(lines, "\n")

	// Navigation + creation keybindings must be present.
	for _, want := range []string{
		"M-j", "M-k", // next/prev window (tmux-native)
		"M-g", // workspace picker
		"M-a", // create agent
		"M-c", // create workspace
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("keybindings missing %q", want)
		}
	}
}

func TestCheckManifestConsistency(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Command: "claude"},
		},
	}
	m := manifest.New()
	agentName := "missing-agent"
	m.Docks = []manifest.Dock{
		{
			Name: "labs",
			Workspaces: []manifest.Workspace{
				{
					Name: "dup",
					Surfaces: []manifest.Surface{
						{ID: 1, Name: "s1", Type: manifest.SurfaceTypeAgent, Backend: manifest.SurfaceBackendTmux, Agent: &agentName, Tmux: &manifest.TmuxAttrs{}},
						{ID: 1, Name: "s1", Type: manifest.SurfaceTypeShell, Backend: manifest.SurfaceBackendTmux, Tmux: &manifest.TmuxAttrs{}},
					},
				},
				{
					Name: "dup",
					Surfaces: []manifest.Surface{
						{ID: 2, Name: "s2", Type: manifest.SurfaceTypeShell, Backend: manifest.SurfaceBackendTmux, Tmux: &manifest.TmuxAttrs{}},
					},
				},
			},
		},
	}

	warnings := checkManifestConsistency(m, cfg)
	if len(warnings) < 3 {
		t.Fatalf("warnings = %v, want at least 3 consistency warnings (dup ws name, dup surface id, dup surface name)", warnings)
	}
}
