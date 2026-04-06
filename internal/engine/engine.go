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

// SaveConfig writes the current config to disk.
func (e *Engine) SaveConfig() error {
	if e.configPath == "" {
		return nil
	}
	return config.Save(e.configPath, e.Config)
}

// findRepo loads the manifest and looks up a repo by name.
func (e *Engine) findRepo(name string) (*manifest.Repo, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	r := m.FindRepo(name)
	if r == nil {
		return nil, fmt.Errorf("repo %q not found", name)
	}
	return r, nil
}

// resolvedDockAgent returns the effective agent for a dock,
// checking config overrides first, then the manifest default.
func (e *Engine) resolvedDockAgent(dockName string, m *manifest.Manifest) string {
	dock := m.FindDock(dockName)
	manifestDefault := ""
	if dock != nil {
		manifestDefault = dock.Agent
	}
	return e.Config.ResolvedDockAgent(dockName, manifestDefault)
}

// resolvedDockAgentArgs returns the effective agent args for a dock.
func (e *Engine) resolvedDockAgentArgs(dockName string, m *manifest.Manifest) []string {
	dock := m.FindDock(dockName)
	var manifestDefault []string
	if dock != nil {
		manifestDefault = dock.AgentArgs
	}
	return e.Config.ResolvedDockAgentArgs(dockName, manifestDefault)
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

// launchSurfaceInTmux launches the appropriate command in a tmux pane
// based on the surface type and returns a populated Surface.
// Used by workspace creation and surface add operations.
func (e *Engine) launchSurfaceInTmux(tmuxPaneID, dockName string, surfaceType manifest.SurfaceType, agent, cmd string, agentArgs []string) manifest.Surface {
	s := manifest.Surface{
		Type:    surfaceType,
		Backend: manifest.SurfaceBackendTmux,
		Tmux:    &manifest.TmuxAttrs{},
	}

	switch surfaceType {
	case manifest.SurfaceTypeAgent:
		s.Agent = &agent
		agentCmd := e.buildAgentCommand(agent, agentArgs)
		_ = e.Tmux.SendKeys(tmuxPaneID, agentCmd)
	case manifest.SurfaceTypeCmd:
		s.Command = &cmd
		_ = e.Tmux.SendKeys(tmuxPaneID, cmd)
	case manifest.SurfaceTypeShell:
		// Shell — no command to send.
	case manifest.SurfaceTypeEditor:
		// Editor tmux surface — no command to send (editor launched separately).
	}

	return s
}
