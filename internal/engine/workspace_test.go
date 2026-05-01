package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
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

func TestWorkspaceCompactLabel(t *testing.T) {
	tests := []struct {
		name string
		ws   *manifest.Workspace
		want string
	}{
		{
			name: "renamed worktree",
			ws:   &manifest.Workspace{Name: "auth-fix", Path: "/tmp/bay-worktrees/w4"},
			want: "w4.auth-fix",
		},
		{
			name: "unnamed worktree",
			ws:   &manifest.Workspace{Name: "", Path: "/tmp/bay-worktrees/w4"},
			want: "w4",
		},
		{
			name: "name equals dir",
			ws:   &manifest.Workspace{Name: "w4", Path: "/tmp/bay-worktrees/w4"},
			want: "w4",
		},
		{
			name: "external path name equals dir",
			ws:   &manifest.Workspace{Name: "notes", Path: "/Users/me/notes"},
			want: "notes",
		},
		{
			name: "external path distinct name",
			ws:   &manifest.Workspace{Name: "research", Path: "/Users/me/notes"},
			want: "notes.research",
		},
		{
			name: "missing path",
			ws:   &manifest.Workspace{Name: "scratch"},
			want: "scratch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WorkspaceCompactLabel(tt.ws); got != tt.want {
				t.Errorf("WorkspaceCompactLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncateWorkspaceCompactLabel(t *testing.T) {
	ws := &manifest.Workspace{Name: "auth-fix", Path: "/tmp/bay-worktrees/w4"}
	tests := []struct {
		maxLen int
		want   string
	}{
		{20, "w4.auth-fix"},
		{7, "w4.auth"},
		{5, "w4.au"},
		{4, "w4"},
		{2, "w4"},
		{1, "w"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := TruncateWorkspaceCompactLabel(ws, tt.maxLen); got != tt.want {
				t.Errorf("TruncateWorkspaceCompactLabel(maxLen=%d) = %q, want %q", tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestCommonHyphenPrefixes(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  []string
	}{
		{"empty", nil, []string{}},
		{"single name", []string{"codex-foo"}, []string{""}},
		{
			"mixed dock still strips matching siblings",
			[]string{"codex-home-mail-account-filter", "codex-lane-scheduler-phase1", "agents-md-split"},
			[]string{"codex", "codex", ""},
		},
		{
			"prefers most specific sibling prefix",
			[]string{"codex-feature-x", "codex-feature-y", "codex-bug-z"},
			[]string{"codex-feature", "codex-feature", "codex"},
		},
		{
			"ignores names without a remaining tail",
			[]string{"codex", "codex-a", "other-a"},
			[]string{"", "", ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commonHyphenPrefixes(tt.names)
			if len(got) != len(tt.want) {
				t.Fatalf("commonHyphenPrefixes(%v) length = %d, want %d", tt.names, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("commonHyphenPrefixes(%v)[%d] = %q, want %q", tt.names, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTruncateWorkspaceCompactLabelWithSiblings(t *testing.T) {
	wsHome := &manifest.Workspace{Name: "codex-home-mail-account-filter", Path: "/tmp/bay-worktrees/w1"}
	wsLane := &manifest.Workspace{Name: "codex-lane-scheduler-phase1", Path: "/tmp/bay-worktrees/w2"}
	wsAgents := &manifest.Workspace{Name: "agents-md-split", Path: "/tmp/bay-worktrees/w4"}
	wsBare := &manifest.Workspace{Name: "codex", Path: "/tmp/bay-worktrees/w1"}
	tests := []struct {
		name   string
		ws     *manifest.Workspace
		maxLen int
		prefix string
		want   string
	}{
		{"empty prefix falls through", wsHome, 10, "", "w1.codex-h"},
		{"strip prefix tight budget", wsHome, 10, "codex", "w1.…home-…"},
		{"strip prefix tight budget lane", wsLane, 10, "codex", "w2.…lane-…"},
		{"strip prefix loose budget shows full tail", wsHome, 30, "codex", "w1.…home-mail-account-filter"},
		{"prefix not present falls through", wsAgents, 10, "codex", "w4.agents-"},
		{"name equals prefix not stripped", wsBare, 6, "codex", "w1.cod"},
		{"label fits returns as-is", wsHome, 100, "codex", "w1.codex-home-mail-account-filter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateWorkspaceCompactLabelWithSiblings(tt.ws, tt.maxLen, tt.prefix)
			if got != tt.want {
				t.Errorf("TruncateWorkspaceCompactLabelWithSiblings(%q, %d, %q) = %q, want %q",
					tt.ws.Name, tt.maxLen, tt.prefix, got, tt.want)
			}
		})
	}
}

func TestRefreshDockWindowNames_StripsSharedPrefixInMixedDock(t *testing.T) {
	mockTmux := tmux.NewMock()
	mockTmux.SetClientWidth(92)
	mockTmux.SetStatusReservedCells(50)
	eng := &Engine{Tmux: mockTmux}
	dock := &manifest.Dock{Workspaces: []manifest.Workspace{
		{
			Name: "codex-home-mail-account-filter",
			Path: "/tmp/bay-worktrees/w1",
			Surfaces: []manifest.Surface{
				{Name: "agent", Tmux: &manifest.TmuxAttrs{WindowID: "@1", LayoutGroup: 1}},
			},
		},
		{
			Name: "codex-lane-scheduler-phase1",
			Path: "/tmp/bay-worktrees/w2",
			Surfaces: []manifest.Surface{
				{Name: "agent", Tmux: &manifest.TmuxAttrs{WindowID: "@2", LayoutGroup: 1}},
			},
		},
		{
			Name: "agents-md-split",
			Path: "/tmp/bay-worktrees/w4",
			Surfaces: []manifest.Surface{
				{Name: "agent", Tmux: &manifest.TmuxAttrs{WindowID: "@3", LayoutGroup: 1}},
			},
		},
	}}

	eng.refreshDockWindowNames(dock)

	got := map[string]string{}
	for _, call := range mockTmux.Calls {
		if call.Method == "RenameWindow" && len(call.Args) == 2 {
			got[call.Args[0]] = call.Args[1]
		}
	}
	want := map[string]string{
		"@1": "w1.…home-…",
		"@2": "w2.…lane-…",
		"@3": "w4.agents-",
	}
	for windowID, wantName := range want {
		if got[windowID] != wantName {
			t.Errorf("RenameWindow(%s) = %q, want %q; calls: %v", windowID, got[windowID], wantName, mockTmux.Calls)
		}
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
