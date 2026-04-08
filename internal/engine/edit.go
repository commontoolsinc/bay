package engine

import (
	"fmt"
	"time"

	"github.com/commontoolsinc/bay/internal/manifest"
)

// Edit returns the workspace path for opening in an editor and bumps
// LastActive so the monitor's activity gate keeps fetching merge data
// for this workspace's repo.
func (e *Engine) Edit(dockName, wsName string) (string, error) {
	var path string
	err := e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspace(wsName)
		if ws == nil {
			return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
		}
		ws.LastActive = time.Now().Unix()
		path = ws.Path
		return nil
	})
	return path, err
}

// EditAll returns all active workspace paths for a single dock.
func (e *Engine) EditAll(dockName string) ([]string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	var paths []string
	for _, ws := range dock.Workspaces {
		if ws.Path != "" {
			paths = append(paths, ws.Path)
		}
	}
	return paths, nil
}

// EditAllParentDir returns the worktree parent directory for a dock's repo.
func (e *Engine) EditAllParentDir(dockName string) (string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return "", err
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return "", fmt.Errorf("unknown dock %q", dockName)
	}
	if dock.Repo == "" {
		return "", fmt.Errorf("dock %q has no repo configured", dockName)
	}
	repo := m.FindRepo(dock.Repo)
	if repo == nil {
		return "", fmt.Errorf("unknown repo %q", dock.Repo)
	}
	return repo.EffectiveWorktreeDir(), nil
}
