package cli

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
)

func TestFormatPWD_OmitsMissingLevels(t *testing.T) {
	out := stripANSI(formatPWD(&engine.Context{
		Repo:      "bay",
		Dock:      "api",
		Workspace: "auth-fix",
	}, ""))

	if !strings.Contains(out, "repo bay") || !strings.Contains(out, "dock api") || !strings.Contains(out, "workspace auth-fix") {
		t.Fatalf("unexpected pwd output: %q", out)
	}
	if strings.Contains(out, "surface") {
		t.Fatalf("pwd output should omit missing surface: %q", out)
	}
}

func TestFormatPWD_IncludesSurface(t *testing.T) {
	out := stripANSI(formatPWD(&engine.Context{
		Repo:      "bay",
		Dock:      "api",
		Workspace: "auth-fix",
		Surface:   "agent",
		SurfaceID: 2,
	}, ""))

	for _, want := range []string{"repo bay", "dock api", "workspace auth-fix", "surface agent"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pwd output missing %q: %q", want, out)
		}
	}
}

func TestFormatPWD_IncludesDescription(t *testing.T) {
	out := stripANSI(formatPWD(&engine.Context{
		Dock:      "api",
		Workspace: "auth-fix",
	}, "Login flow fixes"))

	if !strings.Contains(out, "Login flow fixes") {
		t.Fatalf("pwd output missing description: %q", out)
	}
}

func TestFormatPWD_NoDescriptionWhenAbsent(t *testing.T) {
	// Empty description must not add an em-dash or stray separator.
	out := stripANSI(formatPWD(&engine.Context{
		Dock:      "api",
		Workspace: "auth-fix",
	}, ""))

	if strings.Contains(out, "—") {
		t.Fatalf("pwd output should not include em-dash when description is empty: %q", out)
	}
}
