package engine

import "testing"

func TestMaxTabNameLen(t *testing.T) {
	tests := []struct {
		name        string
		clientWidth int
		wsCount     int
		want        int
	}{
		{"wide terminal few ws", 200, 3, 20}, // (150/3)-6=44, clamped to 20
		{"normal 3 ws", 100, 3, 10},          // (50/3)-6=10
		{"normal 5 ws", 100, 5, 4},           // (50/5)-6=4
		{"normal 8 ws", 100, 8, 3},           // (50/8)-6=0, clamped to 3
		{"narrow terminal", 60, 5, 3},        // (10/5)-6<0, clamped to 3
		{"zero workspaces", 100, 0, 20},      // edge case
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxTabNameLen(tt.clientWidth, tt.wsCount)
			if got != tt.want {
				t.Errorf("maxTabNameLen(%d, %d) = %d, want %d", tt.clientWidth, tt.wsCount, got, tt.want)
			}
		})
	}
}

func TestTruncateName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"no truncation", "auth-fix", 20, "auth-fix"},
		{"exact fit", "auth-fix", 8, "auth-fix"},
		{"truncate", "auth-fix", 6, "auth.."},
		{"very short", "auth-fix", 3, "a.."},
		{"min", "auth-fix", 2, "au"},
		{"single char", "hello", 1, "h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateName(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateName(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}
