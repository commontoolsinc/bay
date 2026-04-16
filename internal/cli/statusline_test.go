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

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 4, "he.."},
		{"hello", 3, "h.."},
		{"hello", 2, "he"},
		{"hello", 1, "h"},
	}

	for _, tt := range tests {
		got := truncateStr(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
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
