package cli

import "testing"

func TestFormatStatusLine_NoWidth(t *testing.T) {
	tests := []struct {
		name   string
		repo   string
		branch string
		pr     string
		status string
		want   string
	}{
		{"full", "bay", "fix/login", "42", "dirty", "bay:fix/login #42 | dirty"},
		{"no pr", "bay", "fix/login", "", "dirty", "bay:fix/login | dirty"},
		{"clean", "bay", "fix/login", "42", "", "bay:fix/login #42"},
		{"merged", "bay", "main", "", "merged", "bay:main | merged"},
		{"no branch", "bay", "", "", "", "bay"},
		{"no repo", "", "fix/login", "", "", "fix/login"},
		{"empty", "", "", "", "", ""},
		{"status only", "", "", "", "dirty", "dirty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusLine(tt.repo, tt.branch, tt.pr, tt.status, 0)
			if got != tt.want {
				t.Errorf("formatStatusLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatStatusLine_WithWidth(t *testing.T) {
	tests := []struct {
		name   string
		repo   string
		branch string
		pr     string
		status string
		width  int
		want   string
	}{
		{
			"fits in width",
			"bay", "main", "42", "dirty",
			40,
			"bay:main #42 | dirty",
		},
		{
			"medium truncates branch",
			"bay", "fix/login-crash-on-submit", "42", "dirty",
			30,
			// overhead: "bay:" (4) + " #42" (4) + " | dirty" (8) = 16
			// budget: 30 - 16 = 14
			"bay:fix/login-cr.. #42 | dirty",
		},
		{
			"compact drops repo",
			"bay", "fix/login-crash-on-submit", "42", "dirty",
			18,
			// compact: branch + " #42" (4) + " *" (2) = branch budget 12
			"fix/login-.. #42 *",
		},
		{
			"minimal drops PR",
			"bay", "fix/login-crash-on-submit", "", "dirty",
			10,
			// minimal: branch + " *" (2) = budget 8
			"fix/lo.. *",
		},
		{
			"very tight just status",
			"bay", "fix/x", "", "dirty",
			2,
			"*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusLine(tt.repo, tt.branch, tt.pr, tt.status, tt.width)
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
	// When branch is empty (unusual but possible), fall back to
	// repo-only tier instead of dropping the repo entirely.
	tests := []struct {
		name   string
		repo   string
		status string
		width  int
		want   string
	}{
		{"repo + dirty fits", "bay", "dirty", 20, "bay | dirty"},
		{"repo-only tier", "bay", "dirty", 5, "bay *"},
		{"repo truncated", "verylongrepo", "dirty", 8, "very.. *"},
		{"too tight, status only", "bay", "dirty", 2, "*"},
		{"no status, no repo", "", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatStatusLine(tt.repo, "", "", tt.status, tt.width)
			if got != tt.want {
				t.Errorf("formatStatusLine(repo=%q, status=%q, width=%d) = %q, want %q",
					tt.repo, tt.status, tt.width, got, tt.want)
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
