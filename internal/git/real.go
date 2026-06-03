package git

import (
	"encoding/json"
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

func (r *Real) IsGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

func (r *Real) RepoRoot(path string) (string, error) {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", path)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *Real) CreateWorktree(repoPath, worktreePath, branch string) error {
	if branch != "" {
		// Checkout an existing branch. --guess-remote lets git create a
		// local tracking branch from origin/<branch> when no local branch
		// exists (common after bay close deletes the local copy).
		cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "--guess-remote", worktreePath, branch)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	defaultBranch, err := r.DefaultBranch(repoPath)
	if err != nil {
		return fmt.Errorf("finding default branch: %w", err)
	}
	// Use origin/<default> so the worktree starts at the latest fetched
	// commit, not wherever the local branch happens to be.
	cmd := exec.Command("git", "-C", repoPath, "worktree", "add", "--detach", worktreePath, "origin/"+defaultBranch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (r *Real) CheckoutDetach(path, ref string) error {
	cmd := exec.Command("git", "-C", path, "checkout", "--detach", ref)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout --detach %s: %s: %w", ref, strings.TrimSpace(string(out)), err)
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

func (r *Real) WorktreeMatchesRecoverableRef(path string) (bool, string, error) {
	worktreeTree, err := currentWorktreeTree(path)
	if err != nil {
		return false, "", err
	}

	staged, err := hasStagedChanges(path)
	if err != nil {
		return false, "", err
	}
	indexTree := ""
	if staged {
		indexTree, err = currentIndexTree(path)
		if err != nil {
			return false, "", err
		}
	}

	refs, err := recoverableRefs(path)
	if err != nil {
		return false, "", err
	}
	for _, ref := range refs {
		if ref.Tree == worktreeTree && (!staged || ref.Tree == indexTree) {
			return true, ref.Name, nil
		}
	}

	return false, "", nil
}

type recoverableRef struct {
	Name string
	Tree string
}

func recoverableRefs(path string) ([]recoverableRef, error) {
	out, err := gitOutput(path, "for-each-ref", "--format=%(refname) %(tree)", "refs/heads", "refs/remotes", "refs/bay/review-heads")
	if err != nil {
		return nil, err
	}
	var refs []recoverableRef
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			refs = append(refs, recoverableRef{Name: fields[0], Tree: fields[1]})
		}
	}
	return refs, nil
}

func (r *Real) DiscardWorktreeChanges(path string) error {
	reset := exec.Command("git", "-C", path, "reset", "--hard", "HEAD")
	if out, err := reset.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset --hard HEAD: %s: %w", strings.TrimSpace(string(out)), err)
	}
	clean := exec.Command("git", "-C", path, "clean", "-fd")
	if out, err := clean.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean -fd: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func currentWorktreeTree(path string) (string, error) {
	dir, err := os.MkdirTemp("", "bay-git-index-*")
	if err != nil {
		return "", fmt.Errorf("creating temp index dir: %w", err)
	}
	defer os.RemoveAll(dir)

	indexPath := filepath.Join(dir, "index")
	if err := runGitWithIndex(path, indexPath, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if err := runGitWithIndex(path, indexPath, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	return gitOutputWithIndex(path, indexPath, "write-tree")
}

func hasStagedChanges(path string) (bool, error) {
	cmd := exec.Command("git", "-C", path, "diff", "--cached", "--quiet")
	err := cmd.Run()
	if err == nil {
		return false, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return true, nil
	}
	return false, fmt.Errorf("git diff --cached --quiet: %w", err)
}

func currentIndexTree(path string) (string, error) {
	return gitOutput(path, "write-tree")
}

func runGitWithIndex(path, indexPath string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}

func gitOutputWithIndex(path, indexPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitOutput(path string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *Real) HasUnpushedCommits(path string) (bool, error) {
	cmd := exec.Command("git", "-C", path, "log", "@{upstream}..", "--oneline")
	out, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(out)) == "" {
		return false, nil
	}

	// If HEAD is ahead of its upstream, or the upstream is missing/deleted,
	// do a patch-equivalence check against the remote default branch before
	// refusing close. This lets bay close squash-merged or cherry-picked
	// branches whose exact local commits are not on a remote ref anymore.
	if pushed, pushErr := r.headExistsOnRemoteBranch(path); pushErr == nil && pushed {
		return false, nil
	}
	return r.hasPatchUniqueCommitsAboveDefault(path)
}

func (r *Real) LocalHeadInMergedPR(path string, pr string) (bool, error) {
	pr = strings.TrimPrefix(strings.TrimSpace(pr), "#")
	if pr == "" {
		return false, nil
	}
	if _, lookErr := exec.LookPath("gh"); lookErr != nil {
		return false, fmt.Errorf("gh not installed")
	}
	cmd := exec.Command("gh", "pr", "view", pr, "--json", "state,headRefOid,mergedAt")
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("gh pr view %s: %s: %w", pr, strings.TrimSpace(string(out)), err)
	}

	var view struct {
		State      string  `json:"state"`
		HeadRefOID string  `json:"headRefOid"`
		MergedAt   *string `json:"mergedAt"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		return false, fmt.Errorf("parse gh pr view %s: %w", pr, err)
	}
	if view.State != "MERGED" && view.MergedAt == nil {
		return false, nil
	}
	if view.HeadRefOID == "" {
		return false, fmt.Errorf("gh pr view %s: missing headRefOid", pr)
	}

	head, err := revParse(path, "HEAD")
	if err != nil {
		return false, fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	if head == view.HeadRefOID {
		return true, nil
	}

	// HEAD behind the merged PR head is also safe: every local commit is part
	// of the merged PR. HEAD ahead of the PR head remains unsafe.
	ancestor := exec.Command("git", "-C", path, "merge-base", "--is-ancestor", "HEAD", view.HeadRefOID)
	err = ancestor.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base HEAD %s: %w", view.HeadRefOID, err)
}

// headExistsOnRemoteBranch reports whether the current named branch's
// origin/<branch> ref contains HEAD, even when upstream tracking is missing.
func (r *Real) headExistsOnRemoteBranch(path string) (bool, error) {
	branch, err := r.CurrentBranch(path)
	if err != nil || branch == "" {
		return false, err
	}
	remoteBranch := "origin/" + branch
	if _, err := revParse(path, remoteBranch); err != nil {
		return false, nil
	}
	cmd := exec.Command("git", "-C", path, "merge-base", "--is-ancestor", "HEAD", remoteBranch)
	err = cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base: %w", err)
}

// hasPatchUniqueCommitsAboveDefault checks whether HEAD contains any commits
// whose patches are not present on origin/<default>. This is intentionally
// broader than object identity: squash merges and cherry-picks produce new
// commit IDs, but the work is already landed and safe for bay to clean up.
func (r *Real) hasPatchUniqueCommitsAboveDefault(path string) (bool, error) {
	defaultBranch, err := r.DefaultBranch(path)
	if err != nil {
		return false, fmt.Errorf("cannot determine default branch: %w", err)
	}
	remoteDefault := "origin/" + defaultBranch
	if _, err := revParse(path, remoteDefault); err != nil {
		return false, fmt.Errorf("cannot resolve %s: %w", remoteDefault, err)
	}
	if hasMerge, err := hasMergeCommitsAbove(path, remoteDefault); err != nil {
		return false, err
	} else if hasMerge {
		return true, nil
	}

	cmd := exec.Command("git", "-C", path, "cherry", remoteDefault, "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git cherry %s HEAD: %w", remoteDefault, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "+") {
			return true, nil
		}
	}
	return false, nil
}

func hasMergeCommitsAbove(path, upstream string) (bool, error) {
	cmd := exec.Command("git", "-C", path, "rev-list", "--merges", "--max-count=1", upstream+"..HEAD")
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git rev-list --merges %s..HEAD: %w", upstream, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
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

// ExpandExcludes uses `git ls-files -i --exclude-from=<excludeFile>` to
// resolve gitignore-format patterns against the repo. With --cached we get
// tracked matches, with --others we get untracked matches. Matching is
// purely against the patterns in excludeFile — standard gitignore rules
// are not consulted here (the caller is expected to cross-check via
// IsIgnored when relevant).
func (r *Real) ExpandExcludes(repoPath, excludeFile string) ([]string, []string, error) {
	tracked, err := lsFilesMatching(repoPath, excludeFile, "--cached")
	if err != nil {
		return nil, nil, err
	}
	untracked, err := lsFilesMatching(repoPath, excludeFile, "--others")
	if err != nil {
		return nil, nil, err
	}
	return tracked, untracked, nil
}

func lsFilesMatching(repoPath, excludeFile, mode string) ([]string, error) {
	cmd := exec.Command("git", "-C", repoPath, "ls-files", "-z", "-i", "--exclude-from="+excludeFile, mode)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files %s: %w", mode, err)
	}
	if len(out) == 0 {
		return nil, nil
	}
	parts := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	return parts, nil
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

func (r *Real) BranchExists(repoPath, branchName string) (bool, error) {
	// Check local branch.
	local := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "refs/heads/"+branchName)
	if local.Run() == nil {
		return true, nil
	}
	// Check remote tracking branch.
	remote := exec.Command("git", "-C", repoPath, "rev-parse", "--verify", "refs/remotes/origin/"+branchName)
	if remote.Run() == nil {
		return true, nil
	}
	return false, nil
}

func (r *Real) CreateBranch(path, branchName string) error {
	cmd := exec.Command("git", "-C", path, "checkout", "-b", branchName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -b: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (r *Real) DeleteBranch(repoPath, branchName string) error {
	cmd := exec.Command("git", "-C", repoPath, "branch", "-D", branchName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch -D: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// PRForBranch looks up the PR number for the given branch via `gh pr view`.
//
// Return semantics (important for the PRChecked sentinel in the caller):
//   - (number, nil)  — gh ran, PR exists
//   - ("", nil)      — gh ran, no PR found for this branch (definitive)
//   - ("", err)      — gh is unavailable, unauthenticated, or otherwise
//     failed to give a definitive answer. Caller should NOT
//     cache the result, so it retries later.
func (r *Real) PRForBranch(path, branch string) (string, error) {
	if _, lookErr := exec.LookPath("gh"); lookErr != nil {
		return "", fmt.Errorf("gh not installed")
	}
	cmd := exec.Command("gh", "pr", "view", branch, "--json", "number", "-q", ".number")
	cmd.Dir = path
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	// Distinguish "no PR for this branch" (definitive, not an error) from
	// other failures (gh auth, network, rate limit, etc.).
	if strings.Contains(stderr.String(), "no pull requests found") {
		return "", nil
	}
	return "", fmt.Errorf("gh pr view: %w", err)
}

func (r *Real) Fetch(path string) error {
	cmd := exec.Command("git", "-C", path, "fetch", "--quiet")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (r *Real) IsMergedIntoDefault(path, branch string) (bool, error) {
	defaultBranch, err := r.DefaultBranch(path)
	if err != nil {
		return false, err
	}
	remoteDefault := "origin/" + defaultBranch
	// A branch that has never been pushed to the remote is local-only
	// and can't have been merged via a PR or remote merge. Without
	// this check, a brand-new branch created from main (bay new
	// --branch) is immediately detected as "merged" because
	// merge-base --is-ancestor is trivially true when both refs
	// point at the same commit (or after a fast-forward merge).
	if _, err := revParse(path, "origin/"+branch); err != nil {
		return false, nil // local-only branch → new, not merged
	}
	// Check if branch is an ancestor of the default branch.
	cmd := exec.Command("git", "-C", path, "merge-base", "--is-ancestor", branch, remoteDefault)
	err = cmd.Run()
	if err == nil {
		return true, nil // branch is merged
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil // not merged
	}
	return false, fmt.Errorf("git merge-base: %w", err)
}

func revParse(path, ref string) (string, error) {
	cmd := exec.Command("git", "-C", path, "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
