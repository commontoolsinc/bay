package cli

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
)

func TestFormatPWD_OmitsMissingLevels(t *testing.T) {
	out := stripANSI(formatPWD(&engine.Context{
		Repo:        "bay",
		Dock:        "api",
		WorkspaceID: "w1",
	}))

	if !strings.Contains(out, "repo bay") || !strings.Contains(out, "dock api") || !strings.Contains(out, "workspace w1") {
		t.Fatalf("unexpected pwd output: %q", out)
	}
	if strings.Contains(out, "window") || strings.Contains(out, "pane") {
		t.Fatalf("pwd output should omit missing window/pane: %q", out)
	}
}

func TestFormatPWD_PrefersWindowName(t *testing.T) {
	out := stripANSI(formatPWD(&engine.Context{
		Repo:        "bay",
		Dock:        "api",
		WorkspaceID: "w1",
		Window:      "editor",
		PaneID:      2,
	}))

	for _, want := range []string{"repo bay", "dock api", "workspace w1", "window editor", "pane 2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pwd output missing %q: %q", want, out)
		}
	}
}
