package monitor

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

// --- TestStripANSI ---

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no escape codes",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "color code",
			input: "\033[31mred text\033[0m",
			want:  "red text",
		},
		{
			name:  "bold and color",
			input: "\033[1;32mbold green\033[0m normal",
			want:  "bold green normal",
		},
		{
			name:  "cursor movement",
			input: "\033[2Jhello\033[H",
			want:  "hello",
		},
		{
			name:  "multiple sequences",
			input: "\033[36m>\033[0m \033[1mPrompt:\033[0m ",
			want:  "> Prompt: ",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "OSC sequences",
			input: "\033]0;title\007some text",
			want:  "some text",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StripANSI(tc.input)
			if got != tc.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// --- TestLoadPatterns ---

func TestLoadPatterns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bay-prompts.txt")

	content := `# This is a comment
(?i)waiting for input

# Another comment
\? \[Y/n\]
Do you want to continue
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing patterns file: %v", err)
	}

	patterns, err := LoadPatterns(path)
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	if len(patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(patterns))
	}

	if !patterns[0].MatchString("WAITING FOR INPUT") {
		t.Error("pattern 0 should match case-insensitively")
	}
	if !patterns[1].MatchString("? [Y/n]") {
		t.Error("pattern 1 should match Y/n prompt")
	}
	if !patterns[2].MatchString("Do you want to continue") {
		t.Error("pattern 2 should match continuation prompt")
	}
}

func TestLoadPatterns_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte("# only comments\n\n"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	patterns, err := LoadPatterns(path)
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	if len(patterns) != 0 {
		t.Errorf("expected 0 patterns, got %d", len(patterns))
	}
}

func TestLoadPatterns_MissingFile(t *testing.T) {
	patterns, err := LoadPatterns("/nonexistent/file.txt")
	if err != nil {
		t.Fatalf("LoadPatterns should not error on missing file, got: %v", err)
	}
	if len(patterns) != 0 {
		t.Errorf("expected 0 patterns for missing file, got %d", len(patterns))
	}
}

func TestLoadPatterns_BadRegex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(path, []byte("[invalid\n"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}
	_, err := LoadPatterns(path)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

// --- TestCheckPane ---

func TestCheckPane(t *testing.T) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)waiting for input`),
		regexp.MustCompile(`\? \[Y/n\]`),
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "matching pattern",
			content: "some output\nWaiting for input\nmore output",
			want:    true,
		},
		{
			name:    "matching Y/n prompt",
			content: "Install package? [Y/n] ",
			want:    true,
		},
		{
			name:    "no match",
			content: "compiling...\ndone.\n$",
			want:    false,
		},
		{
			name:    "empty content",
			content: "",
			want:    false,
		},
		{
			name:    "empty patterns",
			content: "anything here",
			want:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := patterns
			if tc.name == "empty patterns" {
				p = nil
			}
			got := CheckPane(tc.content, p)
			if got != tc.want {
				t.Errorf("CheckPane(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

// --- TestPIDFile ---

func TestPIDFile(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "monitor.pid")

	if err := WritePIDFile(pidPath, 12345); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}

	pid, err := ReadPIDFile(pidPath)
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected PID 12345, got %d", pid)
	}

	if err := RemovePIDFile(pidPath); err != nil {
		t.Fatalf("RemovePIDFile: %v", err)
	}

	_, err = ReadPIDFile(pidPath)
	if err == nil {
		t.Error("expected error reading removed PID file")
	}
}

// --- TestCheckLoop ---

// createTestManifest builds a manifest with one dock ("dev") containing one workspace
// with an agent surface and a shell surface.
func createTestManifest(t *testing.T, dir string, tmuxWindowID string) string {
	t.Helper()
	agentName := "claude"
	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "dev",
			Workspaces: []manifest.Workspace{
				{
					Name:   "test-ws",
					Type:   manifest.WorkspaceTypeWorktree,
					Status: manifest.WorkspaceStatusActive,
					Surfaces: []manifest.Surface{
						{
							ID: 1, Name: "agent", Type: manifest.SurfaceTypeAgent,
							Backend: manifest.SurfaceBackendTmux, Agent: &agentName,
							Tmux: &manifest.TmuxAttrs{WindowID: tmuxWindowID, PaneID: "%1", LayoutGroup: 1},
						},
						{
							ID: 2, Name: "shell", Type: manifest.SurfaceTypeShell,
							Backend: manifest.SurfaceBackendTmux,
							Tmux:    &manifest.TmuxAttrs{WindowID: tmuxWindowID, PaneID: "%2", LayoutGroup: 1},
						},
					},
				},
			},
		},
	}
	path := filepath.Join(dir, "manifest.json")
	if err := manifest.Save(path, m); err != nil {
		t.Fatalf("saving test manifest: %v", err)
	}
	return path
}

// createShellOnlyManifest builds a manifest where the workspace has only a shell surface.
func createShellOnlyManifest(t *testing.T, dir string, tmuxWindowID string) string {
	t.Helper()
	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "dev",
			Workspaces: []manifest.Workspace{
				{
					Name:   "shell-ws",
					Type:   manifest.WorkspaceTypeWorktree,
					Status: manifest.WorkspaceStatusActive,
					Surfaces: []manifest.Surface{
						{
							ID: 1, Name: "shell", Type: manifest.SurfaceTypeShell,
							Backend: manifest.SurfaceBackendTmux,
							Tmux:    &manifest.TmuxAttrs{WindowID: tmuxWindowID, PaneID: "%1", LayoutGroup: 1},
						},
					},
				},
			},
		},
	}
	path := filepath.Join(dir, "manifest.json")
	if err := manifest.Save(path, m); err != nil {
		t.Fatalf("saving test manifest: %v", err)
	}
	return path
}

func TestCheckLoop_SetsHighlightOnMatch(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()

	mock.NewSession("dev")
	winID, _ := mock.NewWindow("dev", "test-ws", "/tmp")
	panes, _ := mock.ListPanes(winID)
	paneID := panes[0].ID

	mock.SetCaptureContent(paneID, "some output\nWaiting for input\n$")

	manifestPath := createTestManifest(t, dir, winID)

	patternsPath := filepath.Join(dir, "bay-prompts.txt")
	if err := os.WriteFile(patternsPath, []byte("(?i)waiting for input\n"), 0o644); err != nil {
		t.Fatalf("writing patterns: %v", err)
	}

	pidPath := filepath.Join(dir, "monitor.pid")
	mon := New(mock, manifestPath, patternsPath, pidPath, 1)

	if err := mon.CheckOnce(); err != nil {
		t.Fatalf("CheckOnce: %v", err)
	}

	val, err := mock.GetWindowOption(winID, "@bay-waiting")
	if err != nil {
		t.Fatalf("GetWindowOption: %v", err)
	}
	if val != "1" {
		t.Errorf("expected @bay-waiting=1, got %q", val)
	}

	style, err := mock.GetWindowOption(winID, "window-status-style")
	if err != nil {
		t.Fatalf("GetWindowOption for style: %v", err)
	}
	if style == "" {
		t.Error("expected window-status-style to be set")
	}
}

func TestCheckLoop_ClearsHighlightWhenNoMatch(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()

	mock.NewSession("dev")
	winID, _ := mock.NewWindow("dev", "test-ws", "/tmp")
	panes, _ := mock.ListPanes(winID)
	paneID := panes[0].ID

	mock.SetCaptureContent(paneID, "Waiting for input")

	manifestPath := createTestManifest(t, dir, winID)
	patternsPath := filepath.Join(dir, "bay-prompts.txt")
	if err := os.WriteFile(patternsPath, []byte("(?i)waiting for input\n"), 0o644); err != nil {
		t.Fatalf("writing patterns: %v", err)
	}

	pidPath := filepath.Join(dir, "monitor.pid")
	mon := New(mock, manifestPath, patternsPath, pidPath, 1)

	if err := mon.CheckOnce(); err != nil {
		t.Fatalf("CheckOnce (1): %v", err)
	}
	val, _ := mock.GetWindowOption(winID, "@bay-waiting")
	if val != "1" {
		t.Fatalf("expected highlight to be set after first check")
	}

	mock.SetCaptureContent(paneID, "compiling...\ndone.\n$")

	if err := mon.CheckOnce(); err != nil {
		t.Fatalf("CheckOnce (2): %v", err)
	}
	val, err := mock.GetWindowOption(winID, "@bay-waiting")
	if err != nil {
		t.Fatalf("GetWindowOption: %v", err)
	}
	if val != "0" {
		t.Errorf("expected @bay-waiting=0, got %q", val)
	}
}

func TestCheckLoop_SkipsShellOnlyWindows(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()

	mock.NewSession("dev")
	winID, _ := mock.NewWindow("dev", "shell-ws", "/tmp")
	panes, _ := mock.ListPanes(winID)
	paneID := panes[0].ID

	mock.SetCaptureContent(paneID, "Waiting for input")

	manifestPath := createShellOnlyManifest(t, dir, winID)
	patternsPath := filepath.Join(dir, "bay-prompts.txt")
	if err := os.WriteFile(patternsPath, []byte("(?i)waiting for input\n"), 0o644); err != nil {
		t.Fatalf("writing patterns: %v", err)
	}

	pidPath := filepath.Join(dir, "monitor.pid")
	mon := New(mock, manifestPath, patternsPath, pidPath, 1)

	if err := mon.CheckOnce(); err != nil {
		t.Fatalf("CheckOnce: %v", err)
	}

	_, err := mock.GetWindowOption(winID, "@bay-waiting")
	if err == nil {
		t.Error("expected no @bay-waiting option on shell-only window")
	}
}

func TestCheckLoop_HandlesANSIInPaneContent(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()

	mock.NewSession("dev")
	winID, _ := mock.NewWindow("dev", "test-ws", "/tmp")
	panes, _ := mock.ListPanes(winID)
	paneID := panes[0].ID

	mock.SetCaptureContent(paneID, "\033[1;31mWaiting for input\033[0m")

	manifestPath := createTestManifest(t, dir, winID)
	patternsPath := filepath.Join(dir, "bay-prompts.txt")
	if err := os.WriteFile(patternsPath, []byte("(?i)waiting for input\n"), 0o644); err != nil {
		t.Fatalf("writing patterns: %v", err)
	}

	pidPath := filepath.Join(dir, "monitor.pid")
	mon := New(mock, manifestPath, patternsPath, pidPath, 1)

	if err := mon.CheckOnce(); err != nil {
		t.Fatalf("CheckOnce: %v", err)
	}

	val, err := mock.GetWindowOption(winID, "@bay-waiting")
	if err != nil {
		t.Fatalf("GetWindowOption: %v", err)
	}
	if val != "1" {
		t.Errorf("expected @bay-waiting=1 after ANSI stripping, got %q", val)
	}
}

func TestCheckLoop_ReloadsPatterns(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()

	mock.NewSession("dev")
	winID, _ := mock.NewWindow("dev", "test-ws", "/tmp")
	panes, _ := mock.ListPanes(winID)
	paneID := panes[0].ID

	mock.SetCaptureContent(paneID, "custom prompt here")

	manifestPath := createTestManifest(t, dir, winID)
	patternsPath := filepath.Join(dir, "bay-prompts.txt")

	if err := os.WriteFile(patternsPath, []byte("# empty\n"), 0o644); err != nil {
		t.Fatalf("writing patterns: %v", err)
	}

	pidPath := filepath.Join(dir, "monitor.pid")
	mon := New(mock, manifestPath, patternsPath, pidPath, 1)

	if err := mon.CheckOnce(); err != nil {
		t.Fatalf("CheckOnce (1): %v", err)
	}

	_, err := mock.GetWindowOption(winID, "@bay-waiting")
	if err == nil {
		t.Error("expected no @bay-waiting with empty patterns")
	}

	if err := os.WriteFile(patternsPath, []byte("custom prompt here\n"), 0o644); err != nil {
		t.Fatalf("rewriting patterns: %v", err)
	}

	if err := mon.CheckOnce(); err != nil {
		t.Fatalf("CheckOnce (2): %v", err)
	}

	val, err := mock.GetWindowOption(winID, "@bay-waiting")
	if err != nil {
		t.Fatalf("GetWindowOption: %v", err)
	}
	if val != "1" {
		t.Errorf("expected @bay-waiting=1 after pattern reload, got %q", val)
	}
}

func TestRun_CancelsOnContext(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()

	manifestPath := filepath.Join(dir, "manifest.json")
	m := manifest.New()
	manifest.Save(manifestPath, m)

	patternsPath := filepath.Join(dir, "bay-prompts.txt")
	os.WriteFile(patternsPath, []byte(""), 0o644)

	pidPath := filepath.Join(dir, "monitor.pid")
	mon := New(mock, manifestPath, patternsPath, pidPath, 1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- mon.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("Run returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

// --- PR detection tests ---

func TestCheckOnce_DetectsPR(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()
	mockGit := git.NewMock()

	mock.NewSession("dev")
	winID, _ := mock.NewWindow("dev", "test-ws", "/tmp")

	agentName := "claude"
	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "dev",
			Workspaces: []manifest.Workspace{
				{
					Name:     "test-ws",
					Type:     manifest.WorkspaceTypeWorktree,
					Path:     "/tmp",
					Status:   manifest.WorkspaceStatusActive,
					Worktree: &manifest.WorktreeAttrs{Repo: "labs", Branch: "feature/login"},
					Surfaces: []manifest.Surface{
						{
							ID: 1, Name: "agent", Type: manifest.SurfaceTypeAgent,
							Backend: manifest.SurfaceBackendTmux, Agent: &agentName,
							Tmux: &manifest.TmuxAttrs{WindowID: winID, PaneID: "%1", LayoutGroup: 1},
						},
					},
				},
			},
		},
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest.Save(manifestPath, m)

	// Configure mock: PR exists for this branch.
	mockGit.SetPR("/tmp", "feature/login", "99")

	patternsPath := filepath.Join(dir, "bay-prompts.txt")
	os.WriteFile(patternsPath, []byte(""), 0o644)
	pidPath := filepath.Join(dir, "monitor.pid")

	mon := NewWithGit(mock, mockGit, manifestPath, patternsPath, pidPath, 1)

	// Run enough cycles to trigger PR check (prCheckInterval).
	for i := 0; i < PRCheckCycles+1; i++ {
		mon.CheckOnce()
	}

	// Reload manifest and verify PR was cached.
	updated, err := manifest.Load(manifestPath)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	dock := updated.FindDock("dev")
	ws := dock.FindWorkspace("test-ws")
	if ws.Worktree.PR != "99" {
		t.Errorf("PR = %q, want 99", ws.Worktree.PR)
	}
}

func TestCheckOnce_SkipsPRDetectionForWorkspaceWithNoPath(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()
	mockGit := git.NewMock()

	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "dev",
			Workspaces: []manifest.Workspace{
				{
					Name:     "test-ws",
					Status:   manifest.WorkspaceStatusActive,
					Worktree: &manifest.WorktreeAttrs{Repo: "labs", Branch: "feature/no-pr"},
					// No Path — detectPRs skips workspaces without a path.
				},
			},
		},
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest.Save(manifestPath, m)

	patternsPath := filepath.Join(dir, "bay-prompts.txt")
	os.WriteFile(patternsPath, []byte(""), 0o644)
	pidPath := filepath.Join(dir, "monitor.pid")

	mon := NewWithGit(mock, mockGit, manifestPath, patternsPath, pidPath, 1)

	for i := 0; i < PRCheckCycles+1; i++ {
		if err := mon.CheckOnce(); err != nil {
			t.Fatalf("CheckOnce: %v", err)
		}
	}

	// PR should remain empty — workspace was skipped.
	updated, _ := manifest.Load(manifestPath)
	dock := updated.FindDock("dev")
	ws := dock.FindWorkspace("test-ws")
	if ws.Worktree.PR != "" {
		t.Errorf("PR = %q, want empty", ws.Worktree.PR)
	}
}

// --- Merge detection tests ---

func TestCheckOnce_DetectsMergedBranch(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()
	mockGit := git.NewMock()

	// Workspace with active branch that has been merged.
	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "dev",
			Workspaces: []manifest.Workspace{
				{
					Name:       "feature-ws",
					Path:       "/tmp",
					Status:     manifest.WorkspaceStatusActive,
					LastActive: time.Now().Unix(), // recently active
					Worktree:   &manifest.WorktreeAttrs{Repo: "labs", Branch: "feature/done"},
				},
			},
		},
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest.Save(manifestPath, m)

	// Configure: branch is merged into default.
	mockGit.SetMerged("/tmp", "feature/done", true)

	patternsPath := filepath.Join(dir, "bay-prompts.txt")
	os.WriteFile(patternsPath, []byte(""), 0o644)
	pidPath := filepath.Join(dir, "monitor.pid")

	mon := NewWithGit(mock, mockGit, manifestPath, patternsPath, pidPath, 1)

	// Run enough cycles to trigger merge check.
	for i := 0; i < MergeCheckCycles+1; i++ {
		mon.CheckOnce()
	}

	// Workspace status should be updated to done.
	updated, _ := manifest.Load(manifestPath)
	dock := updated.FindDock("dev")
	ws := dock.FindWorkspace("feature-ws")
	if ws.Status != manifest.WorkspaceStatusDone {
		t.Errorf("status = %q, want done", ws.Status)
	}
}

func TestCheckOnce_SkipsMergeCheckForInactiveWorkspace(t *testing.T) {
	dir := t.TempDir()
	mock := tmux.NewMock()
	mockGit := git.NewMock()

	// Workspace with no recent activity (LastActive = 0).
	m := manifest.New()
	m.Docks = []manifest.Dock{
		{
			Name: "dev",
			Workspaces: []manifest.Workspace{
				{
					Name:     "old-ws",
					Path:     "/tmp",
					Status:   manifest.WorkspaceStatusActive,
					Worktree: &manifest.WorktreeAttrs{Repo: "labs", Branch: "feature/old"},
				},
			},
		},
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest.Save(manifestPath, m)

	mockGit.SetMerged("/tmp", "feature/old", true)

	patternsPath := filepath.Join(dir, "bay-prompts.txt")
	os.WriteFile(patternsPath, []byte(""), 0o644)
	pidPath := filepath.Join(dir, "monitor.pid")

	mon := NewWithGit(mock, mockGit, manifestPath, patternsPath, pidPath, 1)

	for i := 0; i < MergeCheckCycles+1; i++ {
		mon.CheckOnce()
	}

	// Should NOT be marked done — workspace is inactive.
	updated, _ := manifest.Load(manifestPath)
	ws := updated.FindDock("dev").FindWorkspace("old-ws")
	if ws.Status != manifest.WorkspaceStatusActive {
		t.Errorf("status = %q, want active (inactive workspace should be skipped)", ws.Status)
	}
}

func TestStatus_NotRunning(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "monitor.pid")
	mock := tmux.NewMock()
	mon := New(mock, "", "", pidPath, 1)

	running, pid, err := mon.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if running {
		t.Error("expected not running when no PID file")
	}
	if pid != 0 {
		t.Errorf("expected pid 0, got %d", pid)
	}
}
