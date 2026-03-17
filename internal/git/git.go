// Package git provides an interface for git operations that bay needs.
package git

// Interface defines git operations that bay needs.
type Interface interface {
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

	// Default branch
	DefaultBranch(repoPath string) (string, error)
}
