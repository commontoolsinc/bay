package engine

import "fmt"

// Edit returns the workspace path for opening in an editor.
func (e *Engine) Edit(dockName, wsID string) (string, error) {
	ws, err := e.WsShow(dockName, wsID)
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
	dockState, ok := m.Docks[dockName]
	if !ok {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	var paths []string
	for _, ws := range dockState.Workspaces {
		if ws.Path != "" {
			paths = append(paths, ws.Path)
		}
	}
	return paths, nil
}

// EditAllParentDir returns the worktree parent directory for a dock's repo.
// This is the directory containing all worktrees (e.g., ~/projects/myproject-worktrees/).
// Useful for terminal editors that can browse a directory tree.
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
