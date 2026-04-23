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
	}, "", false))

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
	}, "", false))

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
	}, "Login flow fixes", false))

	if !strings.Contains(out, "Login flow fixes") {
		t.Fatalf("pwd output missing description: %q", out)
	}
}

func TestFormatPWD_NoDescriptionWhenAbsent(t *testing.T) {
	// Empty description must not add an em-dash or stray separator.
	out := stripANSI(formatPWD(&engine.Context{
		Dock:      "api",
		Workspace: "auth-fix",
	}, "", false))

	if strings.Contains(out, "—") {
		t.Fatalf("pwd output should not include em-dash when description is empty: %q", out)
	}
}

func TestFormatPWD_ShortOmitsLabels(t *testing.T) {
	out := stripANSI(formatPWD(&engine.Context{
		Repo:      "bay",
		Dock:      "api",
		Workspace: "auth-fix",
		Path:      "/tmp/repos/bay-worktrees/w4",
		Surface:   "agent",
	}, "Login flow fixes", true))

	for _, unwanted := range []string{"repo ", "dock ", "workspace ", "surface ", "(dir "} {
		if strings.Contains(out, unwanted) {
			t.Errorf("short pwd output should omit %q: %q", unwanted, out)
		}
	}
	for _, want := range []string{"bay", "api", "auth-fix", "(w4)", "Login flow fixes", "agent"} {
		if !strings.Contains(out, want) {
			t.Errorf("short pwd output missing %q: %q", want, out)
		}
	}
}
