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

func TestCheckManifestConsistency(t *testing.T) {
	cfg := &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Command: "claude", ConfigFile: "CLAUDE.local.md"},
		},
	}
	m := manifest.New()
	m.Docks["labs"] = &manifest.DockState{
		Workspaces: map[string]*manifest.Workspace{
			"w1": {
				Name:          "dup",
				AgentOverride: "missing-agent",
				Windows: []manifest.Window{
					{ID: 1, Panes: []manifest.Pane{{ID: 1}, {ID: 1}}},
					{ID: 1},
				},
			},
			"w2": {
				Name: "dup",
				Windows: []manifest.Window{
					{ID: 2, Panes: []manifest.Pane{{ID: 1}}},
				},
			},
		},
	}

	warnings := checkManifestConsistency(m, cfg)
	if len(warnings) < 4 {
		t.Fatalf("warnings = %v, want multiple consistency warnings", warnings)
	}
}
