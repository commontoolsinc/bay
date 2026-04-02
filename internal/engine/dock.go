package engine

import (
	"fmt"
	"os"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// configuredWorkspaceAgent returns the configured workspace agent:
// a workspace-specific override when present, otherwise the dock default.
func configuredWorkspaceAgent(ws *manifest.Workspace, dockAgent string) string {
	if ws.AgentOverride != "" {
		return ws.AgentOverride
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

// WindowInfo holds runtime information about a tracked tmux window.
type WindowInfo struct {
	ID           int        `json:"id"`
	Name         string     `json:"name"`
	TmuxWindowID string     `json:"tmux_window_id,omitempty"`
	Status       string     `json:"status"`
	Panes        []PaneInfo `json:"panes,omitempty"`
}

// PaneInfo holds runtime information about a tracked tmux pane.
type PaneInfo struct {
	ID         int    `json:"id"`
	TmuxPaneID string `json:"tmux_pane_id,omitempty"`
	Type       string `json:"type"`
	Agent      string `json:"agent,omitempty"`
	Command    string `json:"command,omitempty"`
	Status     string `json:"status"`
}

// WorkspaceInfo holds summary information about a workspace.
type WorkspaceInfo struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Type         string       `json:"type"`
	Path         string       `json:"path,omitempty"`
	Branch       string       `json:"branch,omitempty"`
	PR           string       `json:"pr,omitempty"`
	Status       string       `json:"status"`
	Waiting      bool         `json:"waiting,omitempty"`
	Missing      bool         `json:"missing,omitempty"`
	Stale        bool         `json:"stale,omitempty"`
	Agent        string       `json:"agent,omitempty"`
	DefaultAgent string       `json:"default_agent,omitempty"`
	SyncStatus   string       `json:"sync_status"`
	WindowCount  int          `json:"window_count"`
	Windows      []WindowInfo `json:"windows,omitempty"`
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

// DockCloseWorkspaces closes all workspaces in a dock but does not
// kill the tmux session. Used by RepoRemove which needs to save
// config before killing sessions.
func (e *Engine) DockCloseWorkspaces(name string, force bool) {
	m, err := e.LoadManifest()
	if err != nil {
		return
	}
	dockState, ok := m.Docks[name]
	if !ok {
		return
	}
	var wsIDs []string
	for wsID := range dockState.Workspaces {
		wsIDs = append(wsIDs, wsID)
	}
	for _, wsID := range wsIDs {
		_ = e.WsClose(name, wsID, force)
	}
}

// DockClose closes all workspaces in a dock and kills the tmux session.
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
	e.SyncAll()

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
					ID:           id,
					Name:         ws.Name,
					Type:         string(ws.Type),
					Path:         ws.Path,
					Branch:       ws.Branch,
					PR:           ws.PR,
					Status:       string(ws.Status),
					Missing:      ws.Path != "" && statErr != nil,
					Agent:        configuredWorkspaceAgent(ws, dockCfg.Agent),
					DefaultAgent: configuredWorkspaceAgent(ws, dockCfg.Agent),
					SyncStatus:   "ok",
					WindowCount:  len(ws.Windows),
				}

				if wsInfo.Missing {
					wsInfo.SyncStatus = "missing"
				}

				for _, win := range ws.Windows {
					winInfo := WindowInfo{
						ID:           win.ID,
						Name:         win.Name,
						TmuxWindowID: win.TmuxWindowID,
						Status:       "ok",
					}

					var livePaneIDs map[string]bool
					if win.TmuxWindowID != "" {
						exists, _ := e.Tmux.WindowExists(win.TmuxWindowID)
						if !exists {
							winInfo.Status = "stale"
							wsInfo.Stale = true
						} else {
							tmuxPanes, err := e.Tmux.ListPanes(win.TmuxWindowID)
							if err == nil {
								livePaneIDs = make(map[string]bool, len(tmuxPanes))
								for _, tmuxPane := range tmuxPanes {
									livePaneIDs[tmuxPane.ID] = true
								}
							}
							val, err := e.Tmux.GetWindowOption(win.TmuxWindowID, "@bay-waiting")
							if err == nil && val == "1" {
								wsInfo.Waiting = true
							}
						}
					}

					for paneIdx, pane := range win.Panes {
						paneInfo := PaneInfo{
							ID:         pane.ID,
							TmuxPaneID: pane.TmuxPaneID,
							Type:       string(pane.Type),
							Agent:      pane.Agent,
							Command:    pane.Command,
							Status:     "ok",
						}

						if winInfo.Status == "stale" {
							paneInfo.Status = "stale"
						} else if paneInfo.TmuxPaneID != "" {
							if livePaneIDs != nil && !livePaneIDs[paneInfo.TmuxPaneID] {
								paneInfo.Status = "stale"
								winInfo.Status = "stale"
								wsInfo.Stale = true
							}
						} else if win.TmuxWindowID != "" {
							tmuxPanes, err := e.Tmux.ListPanes(win.TmuxWindowID)
							if err == nil && paneIdx >= len(tmuxPanes) {
								paneInfo.Status = "stale"
								winInfo.Status = "stale"
								wsInfo.Stale = true
							}
						}

						winInfo.Panes = append(winInfo.Panes, paneInfo)
					}

					if winInfo.Status == "stale" {
						wsInfo.Stale = true
					}
					wsInfo.Windows = append(wsInfo.Windows, winInfo)
				}

				if wsInfo.Missing {
					wsInfo.Stale = false
				}
				if wsInfo.SyncStatus == "ok" && wsInfo.Stale {
					wsInfo.SyncStatus = "stale"
				}
				info.Workspaces = append(info.Workspaces, wsInfo)
			}
		}
		docks = append(docks, info)
	}
	return docks, nil
}

// WorkspaceInfo returns the tree/runtime view for a single workspace.
func (e *Engine) WorkspaceInfo(dockName, wsID string) (*WorkspaceInfo, error) {
	docks, err := e.List()
	if err != nil {
		return nil, err
	}
	for _, dock := range docks {
		if dock.Name != dockName {
			continue
		}
		for _, ws := range dock.Workspaces {
			if ws.ID == wsID {
				return &ws, nil
			}
		}
		return nil, fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}
	return nil, fmt.Errorf("unknown dock %q", dockName)
}
