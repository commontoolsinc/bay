package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLines(t *testing.T, path string, lines []string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEncodeClaudeDir(t *testing.T) {
	cases := map[string]string{
		"/Users/mike/projects/bay-worktrees/b2": "-Users-mike-projects-bay-worktrees-b2",
		"/work/proj":                            "-work-proj",
		"/a/b.c_d e":                            "-a-b-c-d-e",
	}
	for in, want := range cases {
		if got := encodeClaudeDir(in); got != want {
			t.Errorf("encodeClaudeDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestGather exercises both readers, their filters, newest-session
// selection, cwd matching, and the merge-by-timestamp.
func TestGather(t *testing.T) {
	claudeRoot := t.TempDir()
	codexRoot := t.TempDir()
	const cwd = "/work/proj"
	const encoded = "-work-proj"

	// Older Claude session — ignored (older mtime).
	writeLines(t, filepath.Join(claudeRoot, encoded, "old.jsonl"), []string{
		`{"type":"user","timestamp":"2026-06-01T09:00:00Z","message":{"content":"OLD claude ignore me"}}`,
	}, time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))

	// Newer Claude session — the one we read. Only the plain typed
	// prompt should survive the filters.
	writeLines(t, filepath.Join(claudeRoot, encoded, "new.jsonl"), []string{
		`{"type":"user","isMeta":true,"timestamp":"2026-06-02T10:01:00Z","message":{"content":"<local-command-caveat>meta</local-command-caveat>"}}`,
		`{"type":"user","isSidechain":true,"timestamp":"2026-06-02T10:02:00Z","message":{"content":"sidechain prompt"}}`,
		`{"type":"user","timestamp":"2026-06-02T10:03:00Z","message":{"content":"<bash-input>b ls</bash-input>"}}`,
		`{"type":"assistant","timestamp":"2026-06-02T10:04:00Z","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"user","timestamp":"2026-06-02T10:06:00Z","message":{"content":[{"type":"tool_result","tool_use_id":"x","content":"out"}]}}`,
		`{"type":"user","timestamp":"2026-06-02T10:05:00Z","message":{"content":"fix the auth token refresh"}}`,
	}, time.Date(2026, 6, 2, 10, 10, 0, 0, time.UTC))

	// Matching Codex session, older — ignored (older session_meta ts).
	writeLines(t, filepath.Join(codexRoot, "2026/06/01/rollout-a.jsonl"), []string{
		`{"timestamp":"2026-06-01T08:00:00Z","type":"session_meta","payload":{"cwd":"/work/proj","timestamp":"2026-06-01T08:00:00Z"}}`,
		`{"timestamp":"2026-06-01T08:01:00Z","type":"event_msg","payload":{"type":"user_message","message":"OLD codex ignore"}}`,
	}, time.Time{})

	// Matching Codex session, newer — the one we read. Only the
	// user_message survives (agent_message + raw injected context drop).
	writeLines(t, filepath.Join(codexRoot, "2026/06/02/rollout-b.jsonl"), []string{
		`{"timestamp":"2026-06-02T09:59:00Z","type":"session_meta","payload":{"cwd":"/work/proj","timestamp":"2026-06-02T09:59:00Z"}}`,
		`{"timestamp":"2026-06-02T10:00:00Z","type":"event_msg","payload":{"type":"user_message","message":"investigate flaky CI"}}`,
		`{"timestamp":"2026-06-02T10:00:30Z","type":"event_msg","payload":{"type":"agent_message","message":"working on it"}}`,
		`{"timestamp":"2026-06-02T10:00:40Z","type":"event_msg","payload":{"type":"user_message","message":"<environment_context>x</environment_context>"}}`,
	}, time.Time{})

	// Non-matching cwd — ignored entirely.
	writeLines(t, filepath.Join(codexRoot, "2026/06/02/rollout-c.jsonl"), []string{
		`{"timestamp":"2026-06-02T11:00:00Z","type":"session_meta","payload":{"cwd":"/other/proj","timestamp":"2026-06-02T11:00:00Z"}}`,
		`{"timestamp":"2026-06-02T11:01:00Z","type":"event_msg","payload":{"type":"user_message","message":"other proj prompt"}}`,
	}, time.Time{})

	r := Reader{ClaudeRoot: claudeRoot, CodexRoot: codexRoot}
	got := r.Gather(cwd)

	want := []string{"investigate flaky CI", "fix the auth token refresh"}
	if len(got) != len(want) {
		t.Fatalf("Gather returned %d prompts, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Text != w {
			t.Errorf("prompt[%d] = %q, want %q", i, got[i].Text, w)
		}
	}
	if !got[0].Timestamp.Before(got[1].Timestamp) {
		t.Errorf("prompts not sorted by timestamp: %v then %v", got[0].Timestamp, got[1].Timestamp)
	}
}

func TestGatherNoLogs(t *testing.T) {
	r := Reader{ClaudeRoot: t.TempDir(), CodexRoot: t.TempDir()}
	if got := r.Gather("/nonexistent/path"); len(got) != 0 {
		t.Errorf("expected no prompts, got %+v", got)
	}
}
