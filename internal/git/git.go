// Package git provides an interface for git operations that bay needs.
package git

// Interface defines git operations that bay needs.
type Interface interface {
	// Repo operations
	Clone(url string, destPath string) error
	IsGitRepo(path string) bool
	RepoRoot(path string) (string, error)

	// Worktree operations
	// CreateWorktree creates a git worktree. If branch is empty the
	// worktree is created in detached HEAD at the default branch. If
	// branch names an existing local or remote-tracking branch, the
	// worktree checks it out directly.
	CreateWorktree(repoPath, worktreePath, branch string) error
	RemoveWorktree(repoPath string, worktreePath string, force bool) error

	// State checks
	IsDirty(path string) (bool, error)
	HasUnpushedCommits(path string) (bool, error)
	CurrentBranch(path string) (string, error)

	// Gitignore
	IsIgnored(repoPath string, filename string) (bool, error)
	AddToGitignore(repoPath string, filename string) error
	// ExpandExcludes returns files in the repo that match the gitignore-format
	// patterns in excludeFile (path relative to repoPath). Tracked files in the
	// index are returned in `tracked`; untracked files on disk are returned in
	// `untracked`. Paths are repo-root-relative, forward-slash separated.
	ExpandExcludes(repoPath, excludeFile string) (tracked, untracked []string, err error)

	// Branch operations
	CreateBranch(path, branchName string) error
	DeleteBranch(repoPath, branchName string) error
	// BranchExists reports whether branchName exists as a local branch
	// or as a remote-tracking branch (origin/<branchName>).
	BranchExists(repoPath, branchName string) (bool, error)

	// PR detection
	PRForBranch(path, branch string) (string, error)

	// Merge detection
	Fetch(path string) error
	IsMergedIntoDefault(path, branch string) (bool, error)

	// Default branch
	DefaultBranch(repoPath string) (string, error)
}
