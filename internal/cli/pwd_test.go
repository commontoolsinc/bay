package cli

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/engine"
)

func TestFormatPWD_OmitsMissingLevels(t *testing.T) {
	out := stripANSI(formatPWD(&engine.Context{
		Dock:      "api",
		Workspace: "auth-fix",
	}))

	if !strings.Contains(out, "dock api") || !strings.Contains(out, "bay auth-fix") {
		t.Fatalf("unexpected pwd output: %q", out)
	}
	if strings.Contains(out, "surface") {
		t.Fatalf("pwd output should omit missing surface: %q", out)
	}
}

func TestFormatPWD_IncludesSurface(t *testing.T) {
	out := stripANSI(formatPWD(&engine.Context{
		Dock:      "api",
		Workspace: "auth-fix",
		Surface:   "agent",
		SurfaceID: 2,
	}))

	for _, want := range []string{"dock api", "bay auth-fix", "surface agent"} {
		if !strings.Contains(out, want) {
			t.Fatalf("pwd output missing %q: %q", want, out)
		}
	}
}
