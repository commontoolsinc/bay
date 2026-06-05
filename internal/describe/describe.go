// Package describe is the auto-description backstop worker. It summarizes
// what a bay is working on — from the agent conversation first, with git
// state as enrichment and fallback — into the bay's description, without
// the working agent's involvement.
//
// It is conversation-first: the goal comes from the user's own prompts
// (the one thing git doesn't capture); git (branch, commits, diffstat)
// grounds the body and is the sole input only when no transcript is
// readable. It never overwrites a human- or agent-set description, and
// failures are non-fatal (logged, retried next cycle).
//
// See docs/design/auto-descriptions.md.
package describe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/transcript"
)

const (
	summarizeTimeout = 60 * time.Second
	maxPromptLen     = 600
	maxRecentPrompts = 6
	maxInputLen      = 6000
)

// Options identifies the bay a worker should describe.
type Options struct {
	Dock string
	Bay  string
}

// Summarizer turns a full prompt into a description string.
type Summarizer interface {
	Summarize(ctx context.Context, prompt string) (string, error)
}

// Worker owns one describe run for one bay. The function/interface fields
// are overridable for tests; production defaults are wired lazily.
type Worker struct {
	Config       *config.Config
	ManifestPath string
	DataDir      string
	Now          func() time.Time

	Summarizer    Summarizer
	GatherPrompts func(cwd string) []transcript.Prompt
	GatherGit     func(ctx context.Context, bayPath string) GitSignal
}

// Run generates (or refreshes) the description for one bay.
func (w *Worker) Run(ctx context.Context, opts Options) error {
	if opts.Dock == "" || opts.Bay == "" {
		return fmt.Errorf("dock and bay are required")
	}
	if w.DataDir == "" {
		return fmt.Errorf("data dir is required")
	}
	if w.Config != nil && !w.Config.Describe.EffectiveEnabled() {
		return nil
	}

	// One describe worker per bay at a time.
	lock, acquired, err := tryBayLock(bayLockPath(w.DataDir, opts.Dock, opts.Bay))
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	defer lock.Unlock()

	m, err := manifest.Load(w.ManifestPath)
	if err != nil {
		return err
	}
	bay := findBay(m, opts)
	if bay == nil || !eligible(bay) || bay.Path == "" {
		return nil
	}
	cwd := bay.Path

	git := w.gitSignal(ctx, cwd)
	prompts := recentPrompts(w.prompts(cwd), git.WorkStart)
	input, mode := assembleInput(prompts, git)
	if mode == modeNone {
		// Nothing to summarize (no transcript, no git signal). Stamp so
		// the monitor's min-interval gate paces re-checks instead of
		// re-probing every cycle (and so this bay doesn't perpetually
		// sort to the front of the oldest-first dispatch queue).
		return w.stampSummarized(opts)
	}

	hash := hashInput(input)
	if hash == bay.DescriptionInputHash {
		// Unchanged since the last run: bump the timestamp so the
		// monitor's min-interval gate backs off, and skip the summarizer.
		return w.stampSummarized(opts)
	}

	// Every terminal path below stamps DescriptionSummarizedAt, so a
	// persistently-failing bay backs off to the min-interval cadence
	// instead of burning a summarizer call every dispatch cycle.
	desc, err := w.summarize(ctx, input, mode)
	if err != nil {
		w.logErr(opts, err)
		return w.stampSummarized(opts)
	}
	desc = sanitizeDescription(desc)
	if desc == "" {
		w.logErr(opts, fmt.Errorf("summarizer produced an empty description"))
		return w.stampSummarized(opts)
	}
	if err := engine.ValidateDescription(desc); err != nil {
		w.logErr(opts, fmt.Errorf("generated description invalid: %w", err))
		return w.stampSummarized(opts)
	}
	return w.commit(opts, desc, hash)
}

func (w *Worker) prompts(cwd string) []transcript.Prompt {
	if w.GatherPrompts != nil {
		return w.GatherPrompts(cwd)
	}
	return transcript.Reader{}.Gather(cwd)
}

func (w *Worker) gitSignal(ctx context.Context, bayPath string) GitSignal {
	if w.GatherGit != nil {
		return w.GatherGit(ctx, bayPath)
	}
	return gatherGitSignal(ctx, bayPath)
}

func (w *Worker) summarizer() Summarizer {
	if w.Summarizer != nil {
		return w.Summarizer
	}
	return commandSummarizer{config: w.Config}
}

func (w *Worker) summarize(ctx context.Context, input string, mode inputMode) (string, error) {
	sctx, cancel := context.WithTimeout(ctx, summarizeTimeout)
	defer cancel()
	prompt := instructionFor(mode) + "\n\n" + input
	return w.summarizer().Summarize(sctx, prompt)
}

func (w *Worker) now() int64 {
	if w.Now != nil {
		return w.Now().Unix()
	}
	return time.Now().Unix()
}

// stampSummarized records that we ran without writing a new description
// (input unchanged), so the monitor doesn't re-dispatch immediately.
func (w *Worker) stampSummarized(opts Options) error {
	now := w.now()
	return manifest.LockedUpdateMaybe(w.ManifestPath, func(m *manifest.Manifest) (bool, error) {
		bay := findBay(m, opts)
		if bay == nil || !eligible(bay) {
			return false, nil
		}
		bay.DescriptionSummarizedAt = now
		return true, nil
	})
}

// commit writes a generated description, re-checking eligibility under
// the lock since the user may have set one in the meantime.
func (w *Worker) commit(opts Options, desc, hash string) error {
	now := w.now()
	return manifest.LockedUpdateMaybe(w.ManifestPath, func(m *manifest.Manifest) (bool, error) {
		bay := findBay(m, opts)
		if bay == nil || !eligible(bay) {
			return false, nil
		}
		bay.Description = desc
		bay.DescriptionSource = manifest.DescriptionSourceAuto
		bay.DescriptionSummarizedAt = now
		bay.DescriptionInputHash = hash
		return true, nil
	})
}

// eligible reports whether the backstop may write this bay's description:
// only when it's empty or previously auto-generated, and never for the
// home pseudo-bay.
func eligible(bay *manifest.Bay) bool {
	if bay.ID == manifest.HomeBayID || bay.Type == manifest.BayTypeHome {
		return false
	}
	return bay.Description == "" || bay.DescriptionSource == manifest.DescriptionSourceAuto
}

func findBay(m *manifest.Manifest, opts Options) *manifest.Bay {
	dock := m.FindDock(opts.Dock)
	if dock == nil {
		return nil
	}
	return dock.FindBayByID(opts.Bay)
}

// --- input assembly ---

type inputMode int

const (
	modeNone inputMode = iota
	modeConversation
	modeGitOnly
)

// recentPrompts drops prompts from before the current work began (the
// last-merge boundary), so a bay reused after a merged PR describes its
// new focus, not the old task. If the boundary is unknown or excluding
// would empty the set, all prompts are kept. Undated prompts are kept.
func recentPrompts(prompts []transcript.Prompt, since time.Time) []transcript.Prompt {
	if since.IsZero() {
		return prompts
	}
	kept := make([]transcript.Prompt, 0, len(prompts))
	for _, p := range prompts {
		if p.Timestamp.IsZero() || !p.Timestamp.Before(since) {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return prompts
	}
	return kept
}

func assembleInput(prompts []transcript.Prompt, git GitSignal) (string, inputMode) {
	var b strings.Builder
	mode := modeNone

	if len(prompts) > 0 {
		mode = modeConversation
		b.WriteString("=== Conversation (the user's own words) ===\n")
		b.WriteString("First request:\n")
		b.WriteString(truncate(prompts[0].Text, maxPromptLen))
		b.WriteString("\n\nRecent requests (most recent last):\n")
		recent := prompts
		if len(recent) > maxRecentPrompts {
			recent = recent[len(recent)-maxRecentPrompts:]
		}
		for _, p := range recent {
			b.WriteString("- ")
			b.WriteString(truncate(oneLine(p.Text), maxPromptLen))
			b.WriteString("\n")
		}
	}

	if git.hasContent() {
		if mode == modeNone {
			mode = modeGitOnly
		}
		b.WriteString("\n=== Git state ===\n")
		b.WriteString(git.String())
	}

	if mode == modeNone {
		return "", modeNone
	}
	return truncate(b.String(), maxInputLen), mode
}

const baseInstruction = "You are labeling a developer workspace. Write a standing brief describing what this workspace is FOR — its goal or scope, not a log of actions. " +
	"Output ONLY the description, with no preamble, quotes, or code fences. " +
	"The first line is a short imperative goal label: at most 80 characters, no trailing period. " +
	"Optionally add a blank line then 1-3 short lines of current context (where things stand). Keep it to a few lines total."

func instructionFor(mode inputMode) string {
	if mode == modeGitOnly {
		return baseInstruction + " Base it on the branch name, commit subjects, and changed files provided."
	}
	return baseInstruction + " Base the goal on the user's requests; you may use the git state for current context."
}

// --- description sanitation ---

func sanitizeDescription(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`")
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	if s == "" {
		return ""
	}
	// Clamp the first line, then the whole thing, to the manifest limits.
	first := engine.DescriptionFirstLine(s)
	if len(first) > engine.MaxDescriptionFirstLineLen {
		clamped := truncateWords(first, engine.MaxDescriptionFirstLineLen)
		rest := ""
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			rest = s[i:]
		}
		s = clamped + rest
	}
	if len(s) > engine.MaxDescriptionLen {
		s = truncate(s, engine.MaxDescriptionLen)
	}
	return strings.TrimSpace(s)
}

// truncate returns at most maxBytes of s, cut on a rune boundary and
// space-trimmed. It drops a trailing partial rune — both orphaned
// continuation bytes and a lone leading byte whose continuations were
// cut off — so the result is always valid UTF-8.
func truncate(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for len(b) > 0 {
		// DecodeLastRune reports (RuneError, 1) for an incomplete or
		// invalid trailing rune; a genuine encoded U+FFFD has size 3.
		if r, size := utf8.DecodeLastRuneInString(b); r == utf8.RuneError && size <= 1 {
			b = b[:len(b)-1]
			continue
		}
		break
	}
	return strings.TrimSpace(b)
}

// truncateWords clamps to ≤ maxBytes, preferring to cut at the last
// space in the latter half so the label doesn't end mid-word.
func truncateWords(s string, maxBytes int) string {
	cut := truncate(s, maxBytes)
	if i := strings.LastIndexByte(cut, ' '); i > maxBytes/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func hashInput(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (w *Worker) logErr(opts Options, err error) {
	if w.DataDir == "" {
		return
	}
	dir := filepath.Join(w.DataDir, "logs")
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return
	}
	f, openErr := os.OpenFile(filepath.Join(dir, "describe.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if openErr != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s dock=%s bay=%s: %v\n", time.Now().Format(time.RFC3339), opts.Dock, opts.Bay, err)
}

// --- per-bay lock ---

type bayLock struct{ f *os.File }

func tryBayLock(path string) (*bayLock, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, fmt.Errorf("creating describe lock dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, fmt.Errorf("opening describe lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("acquiring describe lock: %w", err)
	}
	return &bayLock{f: f}, true, nil
}

func (l *bayLock) Unlock() {
	if l == nil || l.f == nil {
		return
	}
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	l.f.Close()
}

func bayLockPath(dataDir, dock, bay string) string {
	return filepath.Join(dataDir, "locks", "describe", dock, bay+".lock")
}
