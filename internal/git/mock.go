package git

import "fmt"

// Call records a single method invocation on the mock.
type Call struct {
	Method string
	Args   []string
}

// repoState holds the simulated state for a single repository path.
type repoState struct {
	dirty         bool
	unpushed      bool
	branch        string
	defaultBranch string
	ignored       map[string]bool
	worktrees     map[string]bool
}

// Mock is a test double for Interface that tracks calls and stores state.
type Mock struct {
	calls         []Call
	repos         map[string]*repoState
	globalDirty   *bool
	globalIgnored *bool
}

// NewMock creates a new Mock with empty state.
func NewMock() *Mock {
	return &Mock{
		repos: make(map[string]*repoState),
	}
}

func (m *Mock) repo(path string) *repoState {
	r, ok := m.repos[path]
	if !ok {
		r = &repoState{
			ignored:   make(map[string]bool),
			worktrees: make(map[string]bool),
		}
		m.repos[path] = r
	}
	return r
}

func (m *Mock) record(method string, args ...string) {
	m.calls = append(m.calls, Call{Method: method, Args: args})
}

// Calls returns all recorded calls for the given method name.
func (m *Mock) Calls(method string) []Call {
	var result []Call
	for _, c := range m.calls {
		if c.Method == method {
			result = append(result, c)
		}
	}
	return result
}

// --- Configuration helpers ---

// SetDefaultBranch configures the default branch for a repo.
func (m *Mock) SetDefaultBranch(repoPath, branch string) {
	m.repo(repoPath).defaultBranch = branch
}

// SetDirty configures whether a repo has uncommitted changes.
func (m *Mock) SetDirty(path string, dirty bool) {
	m.repo(path).dirty = dirty
}

// SetUnpushed configures whether a repo has unpushed commits.
func (m *Mock) SetUnpushed(path string, unpushed bool) {
	m.repo(path).unpushed = unpushed
}

// SetBranch configures the current branch for a repo.
func (m *Mock) SetBranch(path, branch string) {
	m.repo(path).branch = branch
}

// AddIgnored marks a filename as gitignored in the repo.
func (m *Mock) AddIgnored(repoPath, filename string) {
	m.repo(repoPath).ignored[filename] = true
}

// HasWorktree reports whether a worktree is currently tracked.
func (m *Mock) HasWorktree(repoPath, worktreePath string) bool {
	return m.repo(repoPath).worktrees[worktreePath]
}

// SetGlobalDirty sets dirty state for all repos (convenience for tests).
func (m *Mock) SetGlobalDirty(dirty bool) {
	m.globalDirty = &dirty
}

// SetGlobalIgnored sets ignored state for all repos/files (convenience for tests).
func (m *Mock) SetGlobalIgnored(ignored bool) {
	m.globalIgnored = &ignored
}

// CreatedWorktrees returns all CreateWorktree calls.
func (m *Mock) CreatedWorktrees() []Call {
	return m.Calls("CreateWorktree")
}

// RemovedWorktrees returns all RemoveWorktree calls.
func (m *Mock) RemovedWorktrees() []Call {
	return m.Calls("RemoveWorktree")
}

// --- Interface implementation ---

func (m *Mock) Clone(url, destPath string) error {
	m.record("Clone", url, destPath)
	return nil
}

func (m *Mock) CreateWorktree(repoPath, worktreePath string) error {
	m.record("CreateWorktree", repoPath, worktreePath)
	r := m.repo(repoPath)
	if r.worktrees[worktreePath] {
		return fmt.Errorf("worktree %q already exists", worktreePath)
	}
	r.worktrees[worktreePath] = true
	return nil
}

func (m *Mock) RemoveWorktree(repoPath, worktreePath string, force bool) error {
	m.record("RemoveWorktree", repoPath, worktreePath)
	r := m.repo(repoPath)
	if !r.worktrees[worktreePath] && !force {
		return fmt.Errorf("worktree %q not found", worktreePath)
	}
	delete(r.worktrees, worktreePath)
	return nil
}

func (m *Mock) IsDirty(path string) (bool, error) {
	m.record("IsDirty", path)
	if m.globalDirty != nil {
		return *m.globalDirty, nil
	}
	return m.repo(path).dirty, nil
}

func (m *Mock) HasUnpushedCommits(path string) (bool, error) {
	m.record("HasUnpushedCommits", path)
	return m.repo(path).unpushed, nil
}

func (m *Mock) CurrentBranch(path string) (string, error) {
	m.record("CurrentBranch", path)
	return m.repo(path).branch, nil
}

func (m *Mock) IsIgnored(repoPath, filename string) (bool, error) {
	m.record("IsIgnored", repoPath, filename)
	if m.globalIgnored != nil {
		return *m.globalIgnored, nil
	}
	return m.repo(repoPath).ignored[filename], nil
}

func (m *Mock) AddToGitignore(repoPath, filename string) error {
	m.record("AddToGitignore", repoPath, filename)
	m.repo(repoPath).ignored[filename] = true
	return nil
}

func (m *Mock) DefaultBranch(repoPath string) (string, error) {
	m.record("DefaultBranch", repoPath)
	r := m.repo(repoPath)
	if r.defaultBranch != "" {
		return r.defaultBranch, nil
	}
	return "main", nil
}
