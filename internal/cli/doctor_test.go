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

func TestMissingKeybindings_HonorsBayKeep(t *testing.T) {
	// A user who pinned M-s via `# bay-keep:` has chosen a non-canonical
	// binding on purpose. Doctor should not warn about the canonical
	// M-s line being "missing" — otherwise the pin has no end-to-end
	// effect (setup stops asking but doctor still nags).
	content := `# Bay keybindings
# bay-keep: M-s
bind-key -n M-s run-shell 'bay shell --window || true'
`
	for _, line := range missingKeybindings(content) {
		if strings.Contains(line, "M-s ") {
			t.Errorf("missingKeybindings reported pinned key M-s as missing: %q", line)
		}
	}
}

func TestKeybindingsIncludeSurfaceNavigation(t *testing.T) {
	lines := tmuxKeybindingLines()
	joined := strings.Join(lines, "\n")

	// Navigation + creation keybindings must be present.
	for _, want := range []string{
		"M-h", "M-l", // prev/next window (tmux-native)
		"M-j", "M-k", // pane down/up (tmux-native)
		"M-H", "M-L", "M-J", "M-K", // pane left/right + shift-mirror of j/k
		"M-g", // bay picker
		"M-a", // create agent
		"M-c", // create bay
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("keybindings missing %q", want)
		}
	}
	if !strings.Contains(joined, "bind-key -n M-p display-popup -w 80% -h 80% -E 'bay palette --split pane || true'") {
		t.Errorf("keybindings should default Option+p palette to pane mode; got:\n%s", joined)
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
			Bays: []manifest.Bay{
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
