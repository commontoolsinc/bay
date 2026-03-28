package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Real implements Interface by executing git commands via os/exec.
type Real struct{}

// Compile-time check that Real implements Interface.
var _ Interface = (*Real)(nil)

// NewReal returns a new Real git implementation.
func NewReal() *Real {
	return &Real{}
}

func (r *Real) Clone(url, destPath string) error {
	cmd := exec.Command("git", "clone", url, destPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (r *Real) CreateWorktree(repoPath, worktreePath string) error {
	branch, err := r.DefaultBranch(repoPath)
	if err != nil {
		return fmt.Errorf("finding default branch: %w", err)
	}
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "--detach", worktreePath, branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (r *Real) RemoveWorktree(repoPath, worktreePath string, force bool) error {
	args := []string{"-C", repoPath, "worktree", "remove", worktreePath}
	if force {
		args = append(args, "--force")
	}
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (r *Real) IsDirty(path string) (bool, error) {
	cmd := exec.Command("git", "-C", path, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (r *Real) HasUnpushedCommits(path string) (bool, error) {
	cmd := exec.Command("git", "-C", path, "log", "@{upstream}..", "--oneline")
	out, err := cmd.Output()
	if err != nil {
		// No upstream tracking branch. Compare HEAD against the default
		// branch to detect commits the user made. This covers both:
		// - Detached HEAD (bay's default worktree state) with new commits
		// - Named branches with no upstream tracking
		return r.hasCommitsAboveDefault(path)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// hasCommitsAboveDefault checks if HEAD has commits beyond the default branch.
func (r *Real) hasCommitsAboveDefault(path string) (bool, error) {
	defaultBranch, err := r.DefaultBranch(path)
	if err != nil {
		// Can't determine default branch — fail safe
		return false, fmt.Errorf("cannot determine default branch: %w", err)
	}
	logCmd := exec.Command("git", "-C", path, "log", defaultBranch+"..HEAD", "--oneline")
	logOut, logErr := logCmd.Output()
	if logErr != nil {
		return false, nil
	}
	return strings.TrimSpace(string(logOut)) != "", nil
}

func (r *Real) CurrentBranch(path string) (string, error) {
	cmd := exec.Command("git", "-C", path, "symbolic-ref", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		// Error means detached HEAD.
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *Real) IsIgnored(repoPath, filename string) (bool, error) {
	cmd := exec.Command("git", "-C", repoPath, "check-ignore", "-q", filename)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	// Exit code 1 means not ignored; other codes are real errors.
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git check-ignore: %w", err)
}

func (r *Real) AddToGitignore(repoPath, filename string) error {
	gitignorePath := filepath.Join(repoPath, ".gitignore")
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening .gitignore: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(filename + "\n"); err != nil {
		return fmt.Errorf("writing to .gitignore: %w", err)
	}
	return nil
}

func (r *Real) DefaultBranch(repoPath string) (string, error) {
	// Try the remote HEAD symref first.
	cmd := exec.Command("git", "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		// ref looks like "refs/remotes/origin/main"
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Fallback: check if "main" or "master" branch exists.
	for _, branch := range []string{"main", "master"} {
		cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", branch)
		if err := cmd.Run(); err == nil {
			return branch, nil
		}
	}

	return "", fmt.Errorf("could not determine default branch for %s", repoPath)
}
