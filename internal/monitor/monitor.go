// Package monitor provides background detection of agent panes waiting for user input.
// It reads the manifest to find windows with agent/cmd panes, captures their content,
// checks for configured patterns, and sets tmux window styles/options to highlight waiting windows.
package monitor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

// ansiRE matches ANSI escape sequences: CSI sequences, OSC sequences, and simple escapes.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b[^[\]]`)

const (
	// waitingOption is the tmux user option set on windows where an agent is waiting.
	waitingOption = "@bay-waiting"

	// highlightStyle is the tmux window-status-style applied to waiting windows.
	highlightStyle = "fg=red,bg=colour238,bold"

	// captureLinesCount is how many lines to capture from each pane.
	captureLinesCount = 20

	// MergeCheckCycles is how many check cycles between merge detection runs.
	// With a default 3-second interval, 100 cycles = ~5 minutes.
	MergeCheckCycles = 100

	// activityWindow is how long a workspace must have been active to
	// trigger fetch + merge checks (2 hours in seconds).
	activityWindow = 2 * 60 * 60
)

// Monitor watches agent panes for input prompts and highlights their tmux windows.
// It also periodically detects PR numbers for workspaces with branches.
type Monitor struct {
	tmux         tmux.Interface
	git          git.Interface
	manifestPath string
	patternsPath string
	pidPath      string
	intervalSecs int

	// engine, if set, has its SyncAll() called at the start of every
	// CheckOnce cycle so that branch changes (and the resulting tmux
	// window renames) propagate without waiting for a CLI command.
	// Optional — when nil, the monitor still does prompt detection
	// and PR/merge probing on its own cadence.
	engine *engine.Engine

	// tracked keeps state of which windows are currently highlighted to avoid
	// redundant tmux calls and to know when to clear.
	tracked map[string]bool

	// cycle counts check cycles for cadence-gated operations.
	cycle int
}

// New creates a new Monitor without git support (PR detection disabled).
func New(t tmux.Interface, manifestPath, patternsPath, pidPath string, intervalSecs int) *Monitor {
	return &Monitor{
		tmux:         t,
		manifestPath: manifestPath,
		patternsPath: patternsPath,
		pidPath:      pidPath,
		intervalSecs: intervalSecs,
		tracked:      make(map[string]bool),
	}
}

// NewWithGit creates a Monitor with git support for background PR detection.
func NewWithGit(t tmux.Interface, g git.Interface, manifestPath, patternsPath, pidPath string, intervalSecs int) *Monitor {
	m := New(t, manifestPath, patternsPath, pidPath, intervalSecs)
	m.git = g
	return m
}

// SetEngine attaches an engine whose SyncAll() will run at the start of
// every CheckOnce cycle. Pass nil to disable. Used by the CLI to wire
// background branch detection into the same process that already does
// prompt-waiting detection.
func (m *Monitor) SetEngine(e *engine.Engine) {
	m.engine = e
}

// Run is the main loop. It periodically checks all windows and exits when ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) error {
	interval := time.Duration(m.intervalSecs) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Errors during a check cycle are non-fatal; we log and continue.
			_ = m.CheckOnce()
		}
	}
}

// CheckOnce runs a single check cycle: reload patterns, read manifest, check all windows.
func (m *Monitor) CheckOnce() error {
	// Branch sync first, so the manifest we load below reflects any
	// branch changes the user just made (and so the tmux windows have
	// already been renamed by the time we iterate them for prompt
	// detection). Cheap — engine.SyncAll's per-workspace probe is one
	// local git call.
	if m.engine != nil {
		m.engine.SyncAll()
	}

	patterns, err := LoadPatterns(m.patternsPath)
	if err != nil {
		return fmt.Errorf("loading patterns: %w", err)
	}

	mf, err := manifest.Load(m.manifestPath)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	// Collect the set of windows we visit this cycle, so we can clear stale entries.
	visited := make(map[string]bool)

	for i := range mf.Docks {
		dock := &mf.Docks[i]
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			for _, s := range ws.Surfaces {
				if s.Tmux == nil || s.Tmux.WindowID == "" {
					continue
				}
				if !hasMonitorableSurface(s) {
					continue
				}

				visited[s.Tmux.WindowID] = true
				waiting := m.checkWindow(s.Tmux.WindowID, patterns)

				if waiting {
					if err := m.setHighlight(s.Tmux.WindowID); err != nil {
						continue
					}
				} else {
					if err := m.clearHighlight(s.Tmux.WindowID); err != nil {
						continue
					}
				}
			}
		}
	}

	// Clear highlight on windows we previously tracked but no longer appear in manifest.
	for wid := range m.tracked {
		if !visited[wid] {
			_ = m.clearHighlight(wid)
		}
	}

	// Periodic background tasks (slower cadence than prompt checking).
	m.cycle++
	manifestDirty := false

	if m.git != nil && m.cycle%MergeCheckCycles == 0 {
		if m.detectMerges(mf) {
			manifestDirty = true
		}
	}

	if manifestDirty {
		_ = manifest.Save(m.manifestPath, mf)
	}

	return nil
}

// detectPRs checks workspaces with a branch but no PR and tries to find one.
// Uses the PRCheckedAt timestamp + manifest.PRCheckTTL to avoid re-hammering
// workspaces that have already been checked. The TTL ensures that PRs opened
// after the first check are eventually picked up.
//
// Returns true if any workspace's PR state changed.
func (m *Monitor) detectPRs(mf *manifest.Manifest) bool {
	now := time.Now().Unix()
	changed := false
	for i := range mf.Docks {
		dock := &mf.Docks[i]
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			if ws.Path == "" || !ws.Worktree.NeedsPRCheck(now) {
				continue
			}
			pr, err := m.git.PRForBranch(ws.Path, ws.Worktree.Branch)
			if err != nil {
				// gh unavailable or transient — leave PRCheckedAt
				// unchanged so we retry next cycle.
				continue
			}
			// Definitive answer: either a PR number, or confirmed no PR.
			ws.Worktree.PR = pr
			ws.Worktree.PRCheckedAt = time.Now().Unix()
			changed = true
		}
	}
	return changed
}

// detectMerges checks active workspaces for branches merged into default.
// Only checks workspaces with recent activity (within activityWindow).
// Performs git fetch before merge check. Returns true if any status changed.
func (m *Monitor) detectMerges(mf *manifest.Manifest) bool {
	now := time.Now().Unix()
	changed := false

	// Collect repos that need fetching (deduplicate).
	fetched := map[string]bool{}

	for i := range mf.Docks {
		dock := &mf.Docks[i]
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			if ws.Worktree == nil || ws.Worktree.Branch == "" || ws.Path == "" {
				continue
			}
			if ws.Worktree.Merged {
				continue
			}
			// Activity gate: skip workspaces not active recently.
			if ws.LastActive == 0 || (now-ws.LastActive) > activityWindow {
				continue
			}

			// Fetch once per repo path.
			if !fetched[ws.Path] {
				_ = m.git.Fetch(ws.Path)
				fetched[ws.Path] = true
			}

			merged, err := m.git.IsMergedIntoDefault(ws.Path, ws.Worktree.Branch)
			if err != nil || !merged {
				continue
			}

			ws.Worktree.Merged = true
			changed = true
		}
	}
	return changed
}

// hasMonitorableSurface returns true if the surface is an agent or cmd that should be monitored.
func hasMonitorableSurface(s manifest.Surface) bool {
	return s.Type == manifest.SurfaceTypeAgent || s.Type == manifest.SurfaceTypeCmd
}

// checkWindow captures all panes in a tmux window and returns true if any match a pattern.
func (m *Monitor) checkWindow(windowID string, patterns []*regexp.Regexp) bool {
	if len(patterns) == 0 {
		return false
	}

	panes, err := m.tmux.ListPanes(windowID)
	if err != nil {
		return false
	}

	for _, pane := range panes {
		content, err := m.tmux.CapturePane(pane.ID, captureLinesCount)
		if err != nil {
			continue
		}
		cleaned := StripANSI(content)
		if CheckPane(cleaned, patterns) {
			return true
		}
	}
	return false
}

// setHighlight marks a window as waiting by setting the tmux user option and style.
func (m *Monitor) setHighlight(windowID string) error {
	if m.tracked[windowID] {
		return nil // already highlighted
	}
	if err := m.tmux.SetWindowOption(windowID, waitingOption, "1"); err != nil {
		return err
	}
	if err := m.tmux.SetWindowOption(windowID, "window-status-style", highlightStyle); err != nil {
		return err
	}
	m.tracked[windowID] = true
	return nil
}

// clearHighlight removes the waiting flag and resets the window style.
func (m *Monitor) clearHighlight(windowID string) error {
	if !m.tracked[windowID] {
		return nil // not highlighted
	}
	if err := m.tmux.SetWindowOption(windowID, waitingOption, "0"); err != nil {
		return err
	}
	if err := m.tmux.UnsetWindowOption(windowID, "window-status-style"); err != nil {
		return err
	}
	delete(m.tracked, windowID)
	return nil
}

// Start forks the monitor as a background process and writes the PID file.
func (m *Monitor) Start() error {
	// Find our own executable.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	attr := &os.ProcAttr{
		Dir:   "/",
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}
	proc, err := os.StartProcess(exe, []string{exe, "monitor", "run"}, attr)
	if err != nil {
		return fmt.Errorf("starting monitor process: %w", err)
	}
	// Release the process so it continues after we exit.
	// The child process (bay monitor run) writes its own PID file,
	// so we must not write it here to avoid a race condition.
	if err := proc.Release(); err != nil {
		return fmt.Errorf("releasing monitor process: %w", err)
	}

	return nil
}

// Stop reads the PID file, sends SIGTERM, and removes the PID file.
func (m *Monitor) Stop() error {
	pid, err := ReadPIDFile(m.pidPath)
	if err != nil {
		return fmt.Errorf("reading PID file: %w", err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		// Process may have already exited; try to clean up anyway.
		_ = RemovePIDFile(m.pidPath)
		return fmt.Errorf("sending signal to process %d: %w", pid, err)
	}
	return RemovePIDFile(m.pidPath)
}

// Status checks whether the monitor is running.
func (m *Monitor) Status() (running bool, pid int, err error) {
	pid, err = ReadPIDFile(m.pidPath)
	if err != nil {
		// No PID file means not running.
		return false, 0, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, pid, nil
	}
	// On Unix, FindProcess always succeeds. Send signal 0 to check if alive.
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		return false, pid, nil
	}
	return true, pid, nil
}

// StripANSI removes ANSI escape sequences from a string.
func StripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// LoadPatterns reads a patterns file (one regex per line, # comments, blank lines ignored).
// Returns nil (no patterns) if the file does not exist.
func LoadPatterns(path string) ([]*regexp.Regexp, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening patterns file: %w", err)
	}
	defer f.Close()

	var patterns []*regexp.Regexp
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		re, err := regexp.Compile(line)
		if err != nil {
			return nil, fmt.Errorf("invalid regex on line %d (%q): %w", lineNum, line, err)
		}
		patterns = append(patterns, re)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading patterns file: %w", err)
	}
	return patterns, nil
}

// CheckPane returns true if the content matches any of the patterns.
func CheckPane(content string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(content) {
			return true
		}
	}
	return false
}

// WritePIDFile writes a PID to a file.
func WritePIDFile(path string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating PID file directory: %w", err)
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0o644)
}

// ReadPIDFile reads a PID from a file.
func ReadPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID in %s: %w", path, err)
	}
	return pid, nil
}

// RemovePIDFile removes the PID file.
func RemovePIDFile(path string) error {
	return os.Remove(path)
}
