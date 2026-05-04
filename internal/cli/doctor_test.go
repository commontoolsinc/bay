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

func TestKeybindingsIncludeHomeChord(t *testing.T) {
	// The M-o h home submenu must ship every documented chord.
	// Pin the canonical lines so tmux's `Enter` token (and the table
	// name) cannot silently drift — the chord is hand-typed by users
	// who learned it from the docs, not auto-completed.
	lines := tmuxKeybindingLines()
	joined := strings.Join(lines, "\n")

	wantLines := []string{
		"bind-key -T bay-home Enter run-shell 'bay home || true'",
		"bind-key -T bay-home s run-shell 'bay shell --bay home || true'",
		"bind-key -T bay-home e run-shell 'bay edit --bay home || true'",
		"bind-key -T bay-home c run-shell 'bay agent claude --bay home || true'",
		"bind-key -T bay-home x run-shell 'bay agent codex --bay home || true'",
		"bind-key -T bay-home g run-shell 'bay agent gemini --bay home || true'",
	}
	for _, want := range wantLines {
		if !strings.Contains(joined, want) {
			t.Errorf("home submenu missing canonical line:\n  %s", want)
		}
	}

	// The M-o launcher toast must advertise the home chord so users
	// know to press `h` after `M-o`.
	if !strings.Contains(joined, "h=home") {
		t.Errorf("M-o launcher toast does not mention home; full lines:\n%s", joined)
	}

	// The home submenu binds in the bay-home key-table — drift on the
	// table name would silently disable every chord.
	if !strings.Contains(joined, "switch-client -T bay-home") {
		t.Errorf("M-o h chord does not switch to bay-home table; full lines:\n%s", joined)
	}
}

func TestMissingKeybindings_DetectsCommentedHomeBinding(t *testing.T) {
	// A user who removed (or never installed) the M-o h Enter binding
	// and left no commented stub should be flagged by doctor. This
	// mirrors the existing missing-binding contract for new chords.
	content := `# Bay keybindings
bind-key -n M-h previous-window
`
	missing := missingKeybindings(content)
	joined := strings.Join(missing, "\n")
	if !strings.Contains(joined, "bind-key -T bay-home Enter run-shell 'bay home || true'") {
		t.Errorf("missingKeybindings did not flag the home Enter binding; got:\n%s", joined)
	}
}

func TestMissingKeybindings_CommentedHomeBindingIsOptOut(t *testing.T) {
	// Commented stubs are an opt-out signal — bay should stop nagging
	// about a chord the user explicitly removed.
	content := `# Bay keybindings
# bind-key -T bay-home Enter run-shell 'bay home || true'
`
	for _, line := range missingKeybindings(content) {
		if strings.Contains(line, "bay-home Enter") {
			t.Errorf("commented home binding should be opt-out, but flagged: %q", line)
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
		t.Fatalf("warnings = %v, want at least 3 consistency warnings (dup bay name, dup surface id, dup surface name)", warnings)
	}
}
