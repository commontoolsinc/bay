package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/commontoolsinc/bay/internal/manifest"
)

func TestMaxTabNameLen(t *testing.T) {
	tests := []struct {
		name        string
		clientWidth int
		reserved    int
		wsCount     int
		want        int
	}{
		{"wide terminal few ws light status", 200, 50, 3, 20},  // (150/3)-4=46, clamped to 20
		{"wide terminal few ws heavy status", 200, 140, 3, 16}, // (60/3)-4=16
		{"normal 3 ws", 100, 50, 3, 12},                        // (50/3)-4=12
		{"normal 5 ws", 100, 50, 5, 6},                         // (50/5)-4=6
		{"normal 8 ws", 100, 50, 8, 3},                         // (50/8)-4=2, clamped to 3
		{"narrow terminal", 60, 50, 5, 3},                      // (10/5)-4<0, clamped to 3
		{"heavy status overflows", 100, 140, 3, 3},             // available<0, clamped to 3
		{"zero workspaces", 100, 50, 0, 20},                    // edge case
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxTabNameLen(tt.clientWidth, tt.reserved, tt.wsCount)
			if got != tt.want {
				t.Errorf("maxTabNameLen(%d, %d, %d) = %d, want %d", tt.clientWidth, tt.reserved, tt.wsCount, got, tt.want)
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

func TestTruncateTabName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"no truncation", "auth-fix", 20, "auth-fix"},
		{"exact fit", "auth-fix", 8, "auth-fix"},
		{"truncate", "auth-fix", 6, "auth-…"},
		{"floor 3", "auth-fix", 3, "au…"},
		{"two", "auth-fix", 2, "a…"},
		{"single char", "hello", 1, "h"},
		{"unicode in name", "café-fix", 5, "café…"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateTabName(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateTabName(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestNextWorkspaceDir(t *testing.T) {
	wtDir := t.TempDir()

	// Empty dir, empty dock → w1.
	if got := nextWorkspaceDir(wtDir, &manifest.Dock{}); got != "w1" {
		t.Errorf("empty: got %q, want w1", got)
	}

	// w1 exists on disk → w2.
	os.MkdirAll(filepath.Join(wtDir, "w1"), 0o755)
	if got := nextWorkspaceDir(wtDir, &manifest.Dock{}); got != "w2" {
		t.Errorf("w1 on disk: got %q, want w2", got)
	}

	// Manifest claims w2 but disk doesn't have it → w3 (avoids collision
	// with the path the manifest-claimed workspace would recreate).
	dock := &manifest.Dock{Workspaces: []manifest.Workspace{
		{Name: "feat-foo", Path: filepath.Join(wtDir, "w2")},
	}}
	if got := nextWorkspaceDir(wtDir, dock); got != "w3" {
		t.Errorf("w1 on disk + w2 in manifest: got %q, want w3", got)
	}

	// Workspace name differs from path basename — verifying decoupling.
	if dock.Workspaces[0].Name == filepath.Base(dock.Workspaces[0].Path) {
		t.Error("test precondition: name should not equal basename")
	}
}
