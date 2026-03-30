// Package engine implements bay's core business logic, coordinating
// config, manifest, tmux, and git operations.
package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/git"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

// Engine is the core coordinator for all bay operations.
type Engine struct {
	Config       *config.Config
	Tmux         tmux.Interface
	Git          git.Interface
	configPath   string
	manifestPath string
	archivePath  string
}

// New creates a new Engine.
func New(cfg *config.Config, configPath, manifestPath, archivePath string, t tmux.Interface, g git.Interface) *Engine {
	return &Engine{
		Config:       cfg,
		configPath:   configPath,
		manifestPath: manifestPath,
		archivePath:  archivePath,
		Tmux:         t,
		Git:          g,
	}
}

// LoadManifest loads the manifest, creating an empty one if it doesn't exist.
// NOTE: LoadManifest + saveManifest do NOT hold a lock across the full
// read-modify-write cycle. Use withManifest for operations that must be
// atomic (e.g. WsUpdate, WsRename — called concurrently by agents).
// Operations like WsNew, WsClose, WinOpen are typically user-initiated
// and single-threaded, so the separate load/save is acceptable.
func (e *Engine) LoadManifest() (*manifest.Manifest, error) {
	m, err := manifest.Load(e.manifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return manifest.New(), nil
		}
		return nil, err
	}
	return m, nil
}

// saveManifest saves the manifest with file locking.
func (e *Engine) saveManifest(m *manifest.Manifest) error {
	return manifest.Save(e.manifestPath, m)
}

// withManifest loads the manifest, passes it to fn for modification, and saves.
// The entire read-modify-write cycle is done under a file lock.
func (e *Engine) withManifest(fn func(m *manifest.Manifest) error) error {
	return manifest.LockedUpdate(e.manifestPath, fn)
}

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var validStatuses = map[manifest.WorkspaceStatus]bool{
	manifest.WorkspaceStatusIdle:   true,
	manifest.WorkspaceStatusActive: true,
	manifest.WorkspaceStatusDone:   true,
}

// ValidateStatus checks that a status string is one of idle/active/done.
func ValidateStatus(s string) error {
	if !validStatuses[manifest.WorkspaceStatus(s)] {
		return fmt.Errorf("invalid status %q: must be idle, active, or done", s)
	}
	return nil
}

// ValidateName checks that a name matches the naming constraints.
func ValidateName(name string) error {
	if !validNameRe.MatchString(name) {
		return fmt.Errorf("invalid name %q: must match [a-zA-Z0-9_-]+", name)
	}
	return nil
}

// abbreviateBranch strips common prefixes from branch names for display.
// The result is sanitized to be a valid display name.
func abbreviateBranch(branch string) string {
	prefixes := []string{
		"feature/", "fix/", "chore/", "bugfix/", "hotfix/",
		"release/", "refactor/", "docs/", "test/", "ci/",
	}
	name := branch
	for _, p := range prefixes {
		if strings.HasPrefix(branch, p) {
			name = branch[len(p):]
			break
		}
	}
	// Sanitize: replace invalid characters with hyphens
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := b.String()
	// Trim leading/trailing hyphens and collapse multiple hyphens
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if result == "" {
		return branch // fallback to raw branch if sanitization empties it
	}
	return result
}

// launchPaneInTmux determines the pane type and launches the appropriate
// command in a tmux pane. Returns the pane metadata for the manifest.
// Used by WsNew, WinOpen, and PaneAdd to avoid duplicating the switch logic.
func (e *Engine) launchPaneInTmux(tmuxPaneID, dockName, agent string, shell bool, cmd string) (paneType manifest.PaneType, paneAgent, paneCmd string) {
	dockCfg := e.Config.Docks[dockName]

	switch {
	case shell:
		paneType = manifest.PaneTypeShell
	case cmd != "":
		paneType = manifest.PaneTypeCmd
		paneCmd = cmd
		_ = e.Tmux.SendKeys(tmuxPaneID, cmd)
	case agent != "":
		paneType = manifest.PaneTypeAgent
		paneAgent = agent
		agentCmd := e.buildAgentCommand(agent, dockCfg)
		_ = e.Tmux.SendKeys(tmuxPaneID, agentCmd)
	default:
		paneType = manifest.PaneTypeShell
	}
	return
}
