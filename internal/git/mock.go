package git

import (
	"fmt"
	"sync"
)

// Call records a single method invocation on the mock.
type Call struct {
	Method string
	Args   []string
}

// repoState holds the simulated state for a single repository path.
type repoState struct {
	dirty          bool
	unpushed       bool
	branch         string
	defaultBranch  string
	ignored        map[string]bool
	worktrees      map[string]bool
	prs            map[string]string // branch → PR number
	recoverableRef string            // non-empty when dirty tree matches a durable ref
	mergedPRs      map[string]bool   // PR number → local HEAD is contained in merged PR
	merged         map[string]bool   // branch → merged
	branchExists   map[string]bool   // branch → exists (local or remote)
	repoRoot       string
	// excludeMatches[excludeFile] → {tracked, untracked}. Set via
	// SetExcludeMatches for tests exercising .worktreeinclude expansion.
	excludeMatches map[string]excludeMatch
}

type excludeMatch struct {
	tracked   []string
	untracked []string
}

// Mock is a test double for Interface that tracks calls and stores state.
//
// Concurrency contract: read-side methods (PRForBranch, IsMergedIntoDefault,
// CurrentBranch, etc.) and the call recorder are safe for concurrent use —
// the engine's parallel sync paths call them from multiple goroutines.
//
// Setter methods (SetPR, SetMerged, SetBranch, etc.) and the inner repoState
// maps they touch are NOT lock-protected beyond the *repoState lookup. All
// test setup using setters must complete before any concurrent reads begin.
// In practice this means: configure the mock fully, then call the engine
// method under test.
type Mock struct {
	mu            sync.Mutex
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
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.repos[path]
	if !ok {
		r = &repoState{
			ignored:        make(map[string]bool),
			worktrees:      make(map[string]bool),
			prs:            make(map[string]string),
			mergedPRs:      make(map[string]bool),
			merged:         make(map[string]bool),
			branchExists:   make(map[string]bool),
			excludeMatches: make(map[string]excludeMatch),
		}
		m.repos[path] = r
	}
	return r
}

func (m *Mock) record(method string, args ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, Call{Method: method, Args: args})
}

// Calls returns all recorded calls for the given method name.
func (m *Mock) Calls(method string) []Call {
	m.mu.Lock()
	defer m.mu.Unlock()
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

// SetWorktreeMatchesRecoverableRef configures whether the repo's current
// working tree exactly matches a durable, recoverable ref.
func (m *Mock) SetWorktreeMatchesRecoverableRef(path, ref string) {
	m.repo(path).recoverableRef = ref
}

// SetBranch configures the current branch for a repo.
func (m *Mock) SetBranch(path, branch string) {
	m.repo(path).branch = branch
}

// AddIgnored marks a filename as gitignored in the repo.
func (m *Mock) AddIgnored(repoPath, filename string) {
	m.repo(repoPath).ignored[filename] = true
}

// SetExcludeMatches configures the tracked/untracked files that
// ExpandExcludes should return for a given excludeFile under repoPath.
func (m *Mock) SetExcludeMatches(repoPath, excludeFile string, tracked, untracked []string) {
	m.repo(repoPath).excludeMatches[excludeFile] = excludeMatch{tracked: tracked, untracked: untracked}
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

// DeletedBranches returns all DeleteBranch calls.
func (m *Mock) DeletedBranches() []Call {
	return m.Calls("DeleteBranch")
}

// --- Interface implementation ---

func (m *Mock) Clone(url, destPath string) error {
	m.record("Clone", url, destPath)
	return nil
}

func (m *Mock) IsGitRepo(path string) bool {
	m.record("IsGitRepo", path)
	return true // mock defaults to yes
}

func (m *Mock) RepoRoot(path string) (string, error) {
	m.record("RepoRoot", path)
	r := m.repo(path)
	if r.repoRoot != "" {
		return r.repoRoot, nil
	}
	return "", fmt.Errorf("not a git repository: %s", path)
}

// SetRepoRoot configures the repo root for a path.
func (m *Mock) SetRepoRoot(path, root string) {
	m.repo(path).repoRoot = root
}

func (m *Mock) CreateWorktree(repoPath, worktreePath, branch string) error {
	m.record("CreateWorktree", repoPath, worktreePath, branch)
	r := m.repo(repoPath)
	if r.worktrees[worktreePath] {
		return fmt.Errorf("worktree %q already exists", worktreePath)
	}
	r.worktrees[worktreePath] = true
	// When an existing branch is specified, simulate checking it out.
	if branch != "" {
		m.repo(worktreePath).branch = branch
	}
	return nil
}

func (m *Mock) RemoveWorktree(repoPath, worktreePath string, force bool) error {
	m.record("RemoveWorktree", repoPath, worktreePath, fmt.Sprintf("%t", force))
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

func (m *Mock) WorktreeMatchesRecoverableRef(path string) (bool, string, error) {
	m.record("WorktreeMatchesRecoverableRef", path)
	ref := m.repo(path).recoverableRef
	return ref != "", ref, nil
}

func (m *Mock) DiscardWorktreeChanges(path string) error {
	m.record("DiscardWorktreeChanges", path)
	m.repo(path).dirty = false
	return nil
}

func (m *Mock) HasUnpushedCommits(path string) (bool, error) {
	m.record("HasUnpushedCommits", path)
	return m.repo(path).unpushed, nil
}

func (m *Mock) LocalHeadInMergedPR(path string, pr string) (bool, error) {
	m.record("LocalHeadInMergedPR", path, pr)
	return m.repo(path).mergedPRs[pr], nil
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

func (m *Mock) ExpandExcludes(repoPath, excludeFile string) ([]string, []string, error) {
	m.record("ExpandExcludes", repoPath, excludeFile)
	em := m.repo(repoPath).excludeMatches[excludeFile]
	return em.tracked, em.untracked, nil
}

func (m *Mock) BranchExists(repoPath, branchName string) (bool, error) {
	m.record("BranchExists", repoPath, branchName)
	return m.repo(repoPath).branchExists[branchName], nil
}

// SetBranchExists configures whether a branch exists in a repo.
func (m *Mock) SetBranchExists(repoPath, branchName string, exists bool) {
	m.repo(repoPath).branchExists[branchName] = exists
}

func (m *Mock) CreateBranch(path, branchName string) error {
	m.record("CreateBranch", path, branchName)
	m.repo(path).branch = branchName
	return nil
}

func (m *Mock) DeleteBranch(repoPath, branchName string) error {
	m.record("DeleteBranch", repoPath, branchName)
	return nil
}

func (m *Mock) PRForBranch(path, branch string) (string, error) {
	m.record("PRForBranch", path, branch)
	pr := m.repo(path).prs[branch]
	return pr, nil
}

// SetPR configures the PR number for a branch in a repo.
func (m *Mock) SetPR(path, branch, pr string) {
	m.repo(path).prs[branch] = pr
}

// SetLocalHeadInMergedPR configures whether the current HEAD is contained in
// the merged PR head for a repo path.
func (m *Mock) SetLocalHeadInMergedPR(path, pr string, landed bool) {
	m.repo(path).mergedPRs[pr] = landed
}

func (m *Mock) Fetch(path string) error {
	m.record("Fetch", path)
	return nil
}

func (m *Mock) IsMergedIntoDefault(path, branch string) (bool, error) {
	m.record("IsMergedIntoDefault", path, branch)
	return m.repo(path).merged[branch], nil
}

// SetMerged configures whether a branch is merged in a repo.
func (m *Mock) SetMerged(path, branch string, merged bool) {
	m.repo(path).merged[branch] = merged
}

func (m *Mock) DefaultBranch(repoPath string) (string, error) {
	m.record("DefaultBranch", repoPath)
	r := m.repo(repoPath)
	if r.defaultBranch != "" {
		return r.defaultBranch, nil
	}
	return "main", nil
}
