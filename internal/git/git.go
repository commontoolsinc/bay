// Package git provides an interface for git operations that bay needs.
package git

// Interface defines git operations that bay needs.
type Interface interface {
	// Repo operations
	Clone(url string, destPath string) error
	IsGitRepo(path string) bool
	RepoRoot(path string) (string, error)

	// Worktree operations
	CreateWorktree(repoPath string, worktreePath string) error
	RemoveWorktree(repoPath string, worktreePath string, force bool) error

	// State checks
	IsDirty(path string) (bool, error)
	HasUnpushedCommits(path string) (bool, error)
	CurrentBranch(path string) (string, error)

	// Gitignore
	IsIgnored(repoPath string, filename string) (bool, error)
	AddToGitignore(repoPath string, filename string) error

	// Branch operations
	CreateBranch(path, branchName string) error

	// PR detection
	PRForBranch(path, branch string) (string, error)

	// Default branch
	DefaultBranch(repoPath string) (string, error)
}
