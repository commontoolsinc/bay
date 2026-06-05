// Package transcript reads coding-agent session logs and extracts the
// human's typed prompts, so the auto-description backstop can summarize
// what a bay is working on. It supports Claude Code and Codex; other
// agents yield no prompts (the backstop then falls back to git).
//
// Reading never returns an error: absent, unreadable, or malformed logs
// simply produce no prompts. "No prompts" is itself a meaningful signal
// to the caller (fall back to git), so failures stay silent here.
//
// See docs/design/auto-descriptions.md.
package transcript

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Prompt is one human-typed message extracted from a transcript.
type Prompt struct {
	Timestamp time.Time
	Text      string
}

// Reader locates and parses agent transcripts. The zero value reads
// from the standard per-user locations; tests override the roots.
type Reader struct {
	ClaudeRoot string // default ~/.claude/projects
	CodexRoot  string // default ~/.codex/sessions
}

// Gather returns the human-typed prompts for cwd across all supported
// agents, merged and sorted by timestamp. It reads the most recent
// session per agent — a bay may use Claude, Codex, or both.
func (r Reader) Gather(cwd string) []Prompt {
	resolved := resolvePath(cwd)
	var out []Prompt
	out = append(out, r.claudePrompts(resolved)...)
	out = append(out, r.codexPrompts(resolved)...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

// --- Claude Code ---

// nonAlnum collapses every run of non-alphanumeric characters to a
// single dash, matching how Claude Code encodes a cwd into its
// per-project transcript directory name (e.g. /Users/x/p -> -Users-x-p).
var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]+`)

func encodeClaudeDir(cwd string) string {
	return nonAlnum.ReplaceAllString(cwd, "-")
}

func (r Reader) claudeRoot() string {
	if r.ClaudeRoot != "" {
		return r.ClaudeRoot
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

func (r Reader) claudePrompts(cwd string) []Prompt {
	root := r.claudeRoot()
	if root == "" {
		return nil
	}
	path := newestJSONL(filepath.Join(root, encodeClaudeDir(cwd)))
	if path == "" {
		return nil
	}
	return parseClaude(path)
}

// claudeWrappers are prefixes of synthetic (non-typed) user entries —
// bash IO, slash-command expansions, and injected reminders — that must
// not pollute a "what is this about" summary.
var claudeWrappers = []string{"<bash-", "<command-", "<local-command-", "<system-reminder>"}

type claudeEntry struct {
	Type        string `json:"type"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Timestamp   string `json:"timestamp"`
	Message     struct {
		// Content is a string for a typed prompt and an array of blocks
		// for tool results; the string-decode below distinguishes them.
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func parseClaude(path string) []Prompt {
	var out []Prompt
	eachLine(path, func(line []byte) {
		var e claudeEntry
		if json.Unmarshal(line, &e) != nil {
			return
		}
		if e.Type != "user" || e.IsMeta || e.IsSidechain {
			return
		}
		var text string
		// A tool result has an array content; decoding to a string fails
		// and drops it, which is exactly what we want.
		if json.Unmarshal(e.Message.Content, &text) != nil {
			return
		}
		text = strings.TrimSpace(text)
		if text == "" || hasAnyPrefix(text, claudeWrappers) {
			return
		}
		out = append(out, Prompt{Timestamp: parseTime(e.Timestamp), Text: text})
	})
	return out
}

// --- Codex ---

func (r Reader) codexRoot() string {
	if r.CodexRoot != "" {
		return r.CodexRoot
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}

func (r Reader) codexPrompts(cwd string) []Prompt {
	root := r.codexRoot()
	if root == "" {
		return nil
	}
	path := newestCodexSession(root, cwd)
	if path == "" {
		return nil
	}
	return parseCodex(path)
}

// codexInjected are prefixes of auto-injected context that can appear in
// the raw user channel. The user_message event channel we read already
// excludes these, but guard defensively.
var codexInjected = []string{"<environment_context>", "<INSTRUCTIONS>", "# AGENTS.md"}

type codexMeta struct {
	Type    string `json:"type"`
	Payload struct {
		Cwd       string `json:"cwd"`
		Timestamp string `json:"timestamp"`
	} `json:"payload"`
}

// newestCodexSession finds the most recent rollout whose line-1
// session_meta records the target cwd. Reading only the first line of
// each file keeps the scan cheap.
func newestCodexSession(root, cwd string) string {
	var best string
	var bestTime time.Time
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		first := firstLine(p)
		if first == nil {
			return nil
		}
		var meta codexMeta
		if json.Unmarshal(first, &meta) != nil || meta.Type != "session_meta" {
			return nil
		}
		if !cwdMatches(meta.Payload.Cwd, cwd) {
			return nil
		}
		t := parseTime(meta.Payload.Timestamp)
		if t.IsZero() {
			if info, e := d.Info(); e == nil {
				t = info.ModTime()
			}
		}
		if best == "" || t.After(bestTime) {
			best, bestTime = p, t
		}
		return nil
	})
	return best
}

type codexEvent struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"payload"`
}

func parseCodex(path string) []Prompt {
	var out []Prompt
	eachLine(path, func(line []byte) {
		var e codexEvent
		if json.Unmarshal(line, &e) != nil {
			return
		}
		if e.Type != "event_msg" || e.Payload.Type != "user_message" {
			return
		}
		text := strings.TrimSpace(e.Payload.Message)
		if text == "" || hasAnyPrefix(text, codexInjected) {
			return
		}
		out = append(out, Prompt{Timestamp: parseTime(e.Timestamp), Text: text})
	})
	return out
}

func cwdMatches(stored, target string) bool {
	if stored == target {
		return true
	}
	return resolvePath(stored) == target
}

// --- shared helpers ---

// resolvePath returns the symlink-resolved absolute path, or the input
// unchanged if it can't be resolved (e.g. it doesn't exist).
func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// newestJSONL returns the newest-mtime *.jsonl in dir, or "".
func newestJSONL(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newest string
	var newestMod time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestMod) {
			newest = filepath.Join(dir, e.Name())
			newestMod = info.ModTime()
		}
	}
	return newest
}

// eachLine streams a file line by line, tolerating arbitrarily long
// lines (transcripts embed large tool results). It is silent on errors.
func eachLine(path string, fn func([]byte)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	br := bufio.NewReader(f)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			fn(line)
		}
		if err != nil {
			return
		}
	}
}

func firstLine(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	line, err := bufio.NewReader(f).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil
	}
	return line
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}
