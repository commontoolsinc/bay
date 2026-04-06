package engine

import (
	"fmt"

	"github.com/commontoolsinc/bay/internal/config"
)

// Edit returns the workspace path for opening in an editor.
func (e *Engine) Edit(dockName, wsName string) (string, error) {
	ws, err := e.WsShow(dockName, wsName)
	if err != nil {
		return "", err
	}
	return ws.Path, nil
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
	dockCfg, ok := e.Config.Docks[dockName]
	if !ok {
		return "", fmt.Errorf("unknown dock %q", dockName)
	}
	if dockCfg.Repo == "" {
		return "", fmt.Errorf("dock %q has no repo configured", dockName)
	}
	repoCfg, ok := e.Config.Repos[dockCfg.Repo]
	if !ok {
		return "", fmt.Errorf("unknown repo %q", dockCfg.Repo)
	}
	return repoCfg.EffectiveWorktreeDir(), nil
}

// SetEditor sets the editor command in the config and saves.
func (e *Engine) SetEditor(command string) error {
	e.Config.Editor.Command = command
	if e.configPath != "" {
		if err := config.Save(e.configPath, e.Config); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}
	return nil
}
