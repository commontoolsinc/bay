package cli

import (
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/manifest"
)

func TestFormatWorkspaceShort_AllFields(t *testing.T) {
	ws := &manifest.Workspace{
		Name:        "auth-fix",
		Description: "Login flow fixes",
		Worktree: &manifest.WorktreeAttrs{
			Branch: "fix/login",
			PR:     "123",
		},
	}
	got := stripANSI(formatWorkspaceShort(ws))
	want := "auth-fix — Login flow fixes — fix/login — PR#123"
	if got != want {
		t.Errorf("formatWorkspaceShort = %q, want %q", got, want)
	}
}

func TestFormatWorkspaceShort_SkipsEmptyFields(t *testing.T) {
	cases := []struct {
		name string
		ws   *manifest.Workspace
		want string
	}{
		{
			name: "name only",
			ws:   &manifest.Workspace{Name: "ws1"},
			want: "ws1",
		},
		{
			name: "no description, has branch+pr",
			ws: &manifest.Workspace{
				Name:     "ws1",
				Worktree: &manifest.WorktreeAttrs{Branch: "main", PR: "42"},
			},
			want: "ws1 — main — PR#42",
		},
		{
			name: "description, no branch",
			ws: &manifest.Workspace{
				Name:        "ws1",
				Description: "cleanup",
			},
			want: "ws1 — cleanup",
		},
		{
			name: "description + branch, no pr",
			ws: &manifest.Workspace{
				Name:        "ws1",
				Description: "cleanup",
				Worktree:    &manifest.WorktreeAttrs{Branch: "cleanup-branch"},
			},
			want: "ws1 — cleanup — cleanup-branch",
		},
		{
			name: "non-worktree workspace (no Worktree)",
			ws: &manifest.Workspace{
				Name:        "external",
				Description: "external dir",
			},
			want: "external — external dir",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripANSI(formatWorkspaceShort(c.ws))
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestFormatWorkspaceShort_NilWorkspace(t *testing.T) {
	if got := formatWorkspaceShort(nil); got != "" {
		t.Errorf("nil workspace should produce empty string, got %q", got)
	}
}

func TestFormatWorkspaceShort_SilencesBranchWhenEqualToName(t *testing.T) {
	// bay auto-derives workspace names from branches; when they match,
	// showing both duplicates the identifier.
	ws := &manifest.Workspace{
		Name:        "auth-fix",
		Description: "Login flow fixes",
		Worktree:    &manifest.WorktreeAttrs{Branch: "auth-fix", PR: "123"},
	}
	got := stripANSI(formatWorkspaceShort(ws))
	want := "auth-fix — Login flow fixes — PR#123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatWorkspaceShort_SeparatorsAreDimmed(t *testing.T) {
	// The raw output should contain ANSI-dimmed separators; stripping
	// them yields the plain-text form used by the M-? flash.
	ws := &manifest.Workspace{
		Name:        "ws1",
		Description: "desc",
	}
	raw := formatWorkspaceShort(ws)
	if !strings.Contains(raw, "\x1b[") {
		t.Errorf("expected ANSI codes in raw output, got %q", raw)
	}
	if stripANSI(raw) != "ws1 — desc" {
		t.Errorf("stripped output mismatch: got %q", stripANSI(raw))
	}
}
