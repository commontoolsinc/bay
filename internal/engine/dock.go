package engine

import (
	"fmt"
	"os"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// workspaceAgent returns the effective agent for a workspace:
// the first pane's agent if set, otherwise the dock's default.
func workspaceAgent(ws *manifest.Workspace, dockAgent string) string {
	if len(ws.Windows) > 0 && len(ws.Windows[0].Panes) > 0 {
		if a := ws.Windows[0].Panes[0].Agent; a != "" {
			return a
		}
	}
	return dockAgent
}

// collectWorkspaceAgents returns all unique agent names used across a workspace's panes,
// falling back to the dock default if none found.
func collectWorkspaceAgents(ws *manifest.Workspace, dockAgent string) map[string]bool {
	agents := map[string]bool{}
	for _, win := range ws.Windows {
		for _, pane := range win.Panes {
			if pane.Type == manifest.PaneTypeAgent && pane.Agent != "" {
				agents[pane.Agent] = true
			}
		}
	}
	if len(agents) == 0 && dockAgent != "" {
		agents[dockAgent] = true
	}
	return agents
}

// updateWindowNames sets each window's Name in the manifest and renames in tmux.
func (e *Engine) updateWindowNames(ws *manifest.Workspace, displayName string) {
	for i := range ws.Windows {
		winName := displayName
		if ws.Windows[i].ID > 1 {
			winName = fmt.Sprintf("%s:%d", displayName, ws.Windows[i].ID)
		}
		ws.Windows[i].Name = winName
		if ws.Windows[i].TmuxWindowID != "" {
			_ = e.Tmux.RenameWindow(ws.Windows[i].TmuxWindowID, winName)
		}
	}
}

// DockInfo holds summary information about a dock.
type DockInfo struct {
	Name       string          `json:"name"`
	Agent      string          `json:"agent,omitempty"`
	Repo       string          `json:"repo,omitempty"`
	Workspaces []WorkspaceInfo `json:"workspaces"`
}

// WorkspaceInfo holds summary information about a workspace.
type WorkspaceInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Path    string `json:"path,omitempty"`
	Branch  string `json:"branch,omitempty"`
	PR      string `json:"pr,omitempty"`
	Status  string `json:"status"`
	Waiting bool   `json:"waiting,omitempty"`
	Missing bool   `json:"missing,omitempty"`
	Stale   bool   `json:"stale,omitempty"`
	Agent   string `json:"agent,omitempty"`
}

// DockNew creates a new dock configuration and tmux session.
func (e *Engine) DockNew(name, repo, agent, template string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if _, exists := e.Config.Docks[name]; exists {
		return fmt.Errorf("dock %q already exists", name)
	}

	// Validate references
	if repo != "" {
		if _, ok := e.Config.Repos[repo]; !ok {
			return fmt.Errorf("unknown repo %q", repo)
		}
	}
	if agent != "" {
		if _, ok := e.Config.Agents[agent]; !ok {
			return fmt.Errorf("unknown agent %q", agent)
		}
	}

	// Check tmux session doesn't already exist
	exists, err := e.Tmux.HasSession(name)
	if err != nil {
		return fmt.Errorf("checking tmux session: %w", err)
	}
	if exists {
		return fmt.Errorf("tmux session %q already exists and is not a bay dock", name)
	}

	// Create tmux session (default window is tagged as placeholder)
	if err := e.ensureSession(name); err != nil {
		return err
	}

	// Add dock to config and save to disk
	e.Config.Docks[name] = config.DockConfig{
		Repo:                repo,
		Agent:               agent,
		AgentConfigTemplate: template,
	}
	if e.configPath != "" {
		if err := config.Save(e.configPath, e.Config); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}

	// Initialize dock in manifest
	return e.withManifest(func(m *manifest.Manifest) error {
		if m.Docks == nil {
			m.Docks = make(map[string]*manifest.DockState)
		}
		if _, exists := m.Docks[name]; !exists {
			m.Docks[name] = &manifest.DockState{
				Workspaces: make(map[string]*manifest.Workspace),
			}
		}
		return nil
	})
}

// DockClose closes all workspaces in a dock.
func (e *Engine) DockClose(name string, force bool) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[name]
	if !ok {
		return fmt.Errorf("unknown dock %q", name)
	}

	// Collect workspace IDs first to avoid stale-map iteration
	var wsIDs []string
	for wsID := range dockState.Workspaces {
		wsIDs = append(wsIDs, wsID)
	}

	// Close each workspace
	for _, wsID := range wsIDs {
		if err := e.WsClose(name, wsID, force); err != nil {
			if !force {
				return fmt.Errorf("workspace %s: %w", wsID, err)
			}
		}
	}

	// Kill the tmux session (placeholders and all)
	_ = e.Tmux.KillSession(name)

	return nil
}

// List returns all workspaces across all docks with waiting status.
// Syncs git state (branches) before building the output.
func (e *Engine) List() ([]DockInfo, error) {
	e.SyncAllGitState()

	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	var docks []DockInfo
	for name, dockCfg := range e.Config.Docks {
		info := DockInfo{
			Name:  name,
			Agent: dockCfg.Agent,
			Repo:  dockCfg.Repo,
		}
		if ds, ok := m.Docks[name]; ok {
			for id, ws := range ds.Workspaces {
				wsPath := config.ExpandPath(ws.Path)
				_, statErr := os.Stat(wsPath)
				wsInfo := WorkspaceInfo{
					ID:      id,
					Name:    ws.Name,
					Type:    string(ws.Type),
					Path:    ws.Path,
					Branch:  ws.Branch,
					PR:      ws.PR,
					Status:  string(ws.Status),
					Missing: ws.Path != "" && statErr != nil,
					Agent:   workspaceAgent(ws, dockCfg.Agent),
				}
				// Check tmux window state
				for _, win := range ws.Windows {
					if win.TmuxWindowID != "" {
						exists, _ := e.Tmux.WindowExists(win.TmuxWindowID)
						if !exists {
							wsInfo.Stale = true
						}
						val, err := e.Tmux.GetWindowOption(win.TmuxWindowID, "@bay-waiting")
						if err == nil && val == "1" {
							wsInfo.Waiting = true
						}
					}
				}
				info.Workspaces = append(info.Workspaces, wsInfo)
			}
		}
		docks = append(docks, info)
	}
	return docks, nil
}
