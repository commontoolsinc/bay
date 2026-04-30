package cli

import (
	"testing"

	"github.com/commontoolsinc/bay/internal/manifest"
)

func statusLineTestWorkspace() *manifest.Workspace {
	return &manifest.Workspace{Name: "auth-fix", Path: "/tmp/bay-worktrees/w4"}
}

func TestFormatStatusLine_NoWidth(t *testing.T) {
	tests := []struct {
		name   string
		ws     *manifest.Workspace
		branch string
		pr     string
		status string
		want   string
	}{
		{"full", statusLineTestWorkspace(), "feature/auth-fix", "42", "dirty", "w4.auth-fix feature/auth-fix #42 dirty"},
		{"no pr", statusLineTestWorkspace(), "feature/auth-fix", "", "dirty", "w4.auth-fix feature/auth-fix dirty"},
		{"clean", statusLineTestWorkspace(), "feature/auth-fix", "42", "", "w4.auth-fix feature/auth-fix #42"},
		{"merged", statusLineTestWorkspace(), "main", "", "merged", "w4.auth-fix main merged"},
		{"no branch", statusLineTestWorkspace(), "", "", "", "w4.auth-fix"},
		{"dir tag only", &manifest.Workspace{Name: "", Path: "/tmp/bay-worktrees/w4"}, "", "", "", "w4"},
		{"empty", &manifest.Workspace{}, "", "", "", ""},
		{"status only", &manifest.Workspace{}, "", "", "dirty", "dirty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusLine(tt.ws, tt.branch, tt.pr, tt.status, 0)
			if got != tt.want {
				t.Errorf("formatStatusLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatStatusLine_WithWidth(t *testing.T) {
	tests := []struct {
		name   string
		ws     *manifest.Workspace
		branch string
		pr     string
		status string
		width  int
		want   string
	}{
		{
			"fits in width",
			statusLineTestWorkspace(), "main", "42", "dirty",
			40,
			"w4.auth-fix main #42 dirty",
		},
		{
			"crops label before branch",
			statusLineTestWorkspace(), "feature/auth-fix", "42", "dirty",
			34,
			"w4.auth feature/auth-fix #42 dirty",
		},
		{
			"truncates branch after label",
			statusLineTestWorkspace(), "feature/auth-fix", "42", "dirty",
			24,
			"w4.auth-f fe.. #42 dirty",
		},
		{
			"abbreviates status after branch",
			statusLineTestWorkspace(), "feature/auth-fix", "42", "dirty",
			16,
			"w4.au fe.. #42 *",
		},
		{
			"very tight keeps dir tag",
			statusLineTestWorkspace(), "fix/x", "", "dirty",
			2,
			"w4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusLine(tt.ws, tt.branch, tt.pr, tt.status, tt.width)
			if got != tt.want {
				t.Errorf("formatStatusLine(%d) = %q, want %q", tt.width, got, tt.want)
			}
			if tt.width > 0 && len(got) > tt.width {
				t.Errorf("output %q exceeds width %d (len=%d)", got, tt.width, len(got))
			}
		})
	}
}

func TestFormatStatusLine_EmptyBranch(t *testing.T) {
	tests := []struct {
		name   string
		ws     *manifest.Workspace
		status string
		width  int
		want   string
	}{
		{"label + dirty fits", statusLineTestWorkspace(), "dirty", 20, "w4.auth-fix dirty"},
		{"label cropped with status", statusLineTestWorkspace(), "dirty", 11, "w4.au dirty"},
		{"label cropped tight", statusLineTestWorkspace(), "dirty", 7, "w4.au *"},
		{"dir tag only", statusLineTestWorkspace(), "dirty", 2, "w4"},
		{"no status, no label", &manifest.Workspace{}, "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusLine(tt.ws, "", "", tt.status, tt.width)
			if got != tt.want {
				t.Errorf("formatStatusLine(repo=%q, status=%q, width=%d) = %q, want %q",
					tt.ws.Name, tt.status, tt.width, got, tt.want)
			}
			if tt.width > 0 && len(got) > tt.width {
				t.Errorf("output %q exceeds width %d", got, tt.width)
			}
		})
	}
}

func TestAbbreviateStatus(t *testing.T) {
	if got := abbreviateStatus("dirty"); got != "*" {
		t.Errorf("dirty = %q, want *", got)
	}
	if got := abbreviateStatus("merged"); got != "M" {
		t.Errorf("merged = %q, want M", got)
	}
	if got := abbreviateStatus(""); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}
