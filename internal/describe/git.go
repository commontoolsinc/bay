package describe

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// maxGitCommits caps how many commit subjects we feed the summarizer.
const maxGitCommits = 12

// GitSignal is the git-derived context for a bay: the branch, recent
// commit subjects ahead of the base, and a diffstat. It grounds the
// description body and is the sole input when no transcript is readable.
type GitSignal struct {
	Branch   string
	Commits  []string
	DiffStat string
	// WorkStart is the commit date of merge-base(HEAD, default) — where
	// the current line of work diverged, i.e. just after the last merge
	// the bay incorporated. Conversation before it belongs to prior,
	// already-merged work and is excluded. Zero if undeterminable.
	WorkStart time.Time
}

func (g GitSignal) hasContent() bool {
	return strings.TrimSpace(g.Branch) != "" || len(g.Commits) > 0 || strings.TrimSpace(g.DiffStat) != ""
}

func (g GitSignal) String() string {
	var b strings.Builder
	if g.Branch != "" {
		fmt.Fprintf(&b, "Branch: %s\n", g.Branch)
	}
	if len(g.Commits) > 0 {
		b.WriteString("Recent commits:\n")
		for _, c := range g.Commits {
			b.WriteString("- ")
			b.WriteString(c)
			b.WriteString("\n")
		}
	}
	if g.DiffStat != "" {
		b.WriteString("Changed files:\n")
		b.WriteString(g.DiffStat)
		if !strings.HasSuffix(g.DiffStat, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// gatherGitSignal collects the branch, commits ahead of the default
// branch, and a diffstat. Any failure (not a repo, no base) yields an
// empty/partial signal rather than an error.
func gatherGitSignal(ctx context.Context, path string) GitSignal {
	if path == "" {
		return GitSignal{}
	}
	var sig GitSignal
	sig.Branch = gitOutput(ctx, path, "rev-parse", "--abbrev-ref", "HEAD")
	if sig.Branch == "HEAD" {
		// Detached HEAD — the normal state for a fresh worktree and after
		// `bay tidy`. "HEAD" is not a meaningful branch name, so omit it
		// (also keeps hasContent() honest for an otherwise-empty bay).
		sig.Branch = ""
	}
	if base := defaultBaseRef(ctx, path); base != "" {
		sig.Commits = gitLines(ctx, path, "log", "--format=%s", "-n", fmt.Sprint(maxGitCommits), base+"..HEAD")
		sig.DiffStat = gitOutput(ctx, path, "diff", "--stat", base+"..HEAD")
		sig.WorkStart = workStart(ctx, path, base)
	}
	return sig
}

// workStart returns the commit date of merge-base(HEAD, base) — when the
// current line of work diverged from the default branch (just after the
// last merge the bay incorporated). Zero if undeterminable.
func workStart(ctx context.Context, path, base string) time.Time {
	mb := gitOutput(ctx, path, "merge-base", "HEAD", base)
	if mb == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, gitOutput(ctx, path, "show", "-s", "--format=%cI", mb)); err == nil {
		return t
	}
	return time.Time{}
}

// defaultBaseRef finds the ref to diff against: origin/HEAD if set,
// otherwise the first of the common default branch names that exists.
func defaultBaseRef(ctx context.Context, path string) string {
	if ref := gitOutput(ctx, path, "rev-parse", "--abbrev-ref", "origin/HEAD"); ref != "" && ref != "origin/HEAD" {
		return ref
	}
	for _, c := range []string{"origin/main", "origin/master", "main", "master"} {
		if gitOK(ctx, path, "rev-parse", "--verify", "--quiet", c) {
			return c
		}
	}
	return ""
}

func gitOutput(ctx context.Context, path string, args ...string) string {
	full := append([]string{"-C", path}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitLines(ctx context.Context, path string, args ...string) []string {
	out := gitOutput(ctx, path, args...)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func gitOK(ctx context.Context, path string, args ...string) bool {
	full := append([]string{"-C", path}, args...)
	return exec.CommandContext(ctx, "git", full...).Run() == nil
}
