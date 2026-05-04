package cli

import (
	"testing"

	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
)

func statusLineTestBay() *manifest.Bay {
	return &manifest.Bay{Name: "auth-fix", Path: "/tmp/bay-worktrees/w4"}
}

func TestFormatStatusLine_NoWidth(t *testing.T) {
	tests := []struct {
		name   string
		bay    *manifest.Bay
		branch string
		pr     string
		status string
		want   string
	}{
		{"full", statusLineTestBay(), "feature/auth-fix", "42", "dirty", "w4.auth-fix feature/auth-fix #42 dirty"},
		{"no pr", statusLineTestBay(), "feature/auth-fix", "", "dirty", "w4.auth-fix feature/auth-fix dirty"},
		{"clean", statusLineTestBay(), "feature/auth-fix", "42", "", "w4.auth-fix feature/auth-fix #42"},
		{"merged", statusLineTestBay(), "main", "", "merged", "w4.auth-fix main merged"},
		{"no branch", statusLineTestBay(), "", "", "", "w4.auth-fix"},
		{"dir tag only", &manifest.Bay{Name: "", Path: "/tmp/bay-worktrees/w4"}, "", "", "", "w4"},
		{"empty", &manifest.Bay{}, "", "", "", ""},
		{"status only", &manifest.Bay{}, "", "", "dirty", "dirty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusLine(tt.bay, tt.branch, tt.pr, tt.status, 0)
			if got != tt.want {
				t.Errorf("formatStatusLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatStatusLine_WithWidth(t *testing.T) {
	tests := []struct {
		name   string
		bay    *manifest.Bay
		branch string
		pr     string
		status string
		width  int
		want   string
	}{
		{
			"fits in width",
			statusLineTestBay(), "main", "42", "dirty",
			40,
			"w4.auth-fix main #42 dirty",
		},
		{
			"crops label before branch",
			statusLineTestBay(), "feature/auth-fix", "42", "dirty",
			34,
			"w4.auth feature/auth-fix #42 dirty",
		},
		{
			"truncates branch after label",
			statusLineTestBay(), "feature/auth-fix", "42", "dirty",
			24,
			"w4.auth-f fe.. #42 dirty",
		},
		{
			"abbreviates status after branch",
			statusLineTestBay(), "feature/auth-fix", "42", "dirty",
			16,
			"w4.au fe.. #42 *",
		},
		{
			"very tight keeps dir tag",
			statusLineTestBay(), "fix/x", "", "dirty",
			2,
			"w4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusLine(tt.bay, tt.branch, tt.pr, tt.status, tt.width)
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
		bay    *manifest.Bay
		status string
		width  int
		want   string
	}{
		{"label + dirty fits", statusLineTestBay(), "dirty", 20, "w4.auth-fix dirty"},
		{"label cropped with status", statusLineTestBay(), "dirty", 11, "w4.au dirty"},
		{"label cropped tight", statusLineTestBay(), "dirty", 7, "w4.au *"},
		{"dir tag only", statusLineTestBay(), "dirty", 2, "w4"},
		{"no status, no label", &manifest.Bay{}, "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusLine(tt.bay, "", "", tt.status, tt.width)
			if got != tt.want {
				t.Errorf("formatStatusLine(repo=%q, status=%q, width=%d) = %q, want %q",
					tt.bay.Name, tt.status, tt.width, got, tt.want)
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

func TestStatusLineOutput_HomeSkipsWorktreeMetadataAndGitChecks(t *testing.T) {
	m := &manifest.Manifest{Docks: []manifest.Dock{{
		Name: "labs",
		Bays: []manifest.Bay{
			{
				ID:       manifest.HomeBayID,
				Name:     manifest.HomeBayID,
				Type:     manifest.BayTypeHome,
				Path:     "/repo/labs",
				Worktree: &manifest.WorktreeAttrs{Branch: "feature/not-home", PR: "99", Merged: true},
			},
			{
				ID:       "w1",
				Name:     "done",
				Path:     "/repo/worktrees/w1",
				Worktree: &manifest.WorktreeAttrs{Merged: true},
			},
		},
	}}}
	home := &m.Docks[0].Bays[0]
	g := git.NewMock()
	g.SetDirty(home.Path, true)

	tests := []struct {
		field string
		want  string
	}{
		{"branch", ""},
		{"pr", ""},
		{"status", ""},
		{"full", "home"},
		{"merged", "1 merged"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got, err := statusLineOutput(tt.field, "labs", home, m, g, "")
			if err != nil {
				t.Fatalf("statusLineOutput: %v", err)
			}
			if got != tt.want {
				t.Fatalf("statusLineOutput(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
	if calls := g.Calls("IsDirty"); len(calls) != 0 {
		t.Fatalf("home status line checked git dirty state: %+v", calls)
	}
}
