package engine

import (
	"fmt"

	"github.com/commontoolsinc/bay/internal/manifest"
)

// WinOpen opens a new window for an existing workspace.
func (e *Engine) WinOpen(dockName, wsID string, agent string, shell bool, cmd string) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[dockName]
	if !ok {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws, ok := dockState.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}

	// Determine window ID
	winID := manifest.NextWindowID(ws)

	// Window name: workspace name + :N for non-primary windows
	winName := ws.Name
	if winID > 1 {
		winName = fmt.Sprintf("%s:%d", ws.Name, winID)
	}

	// Create tmux window
	tmuxWinID, err := e.Tmux.NewWindow(dockName, winName, ws.Path)
	if err != nil {
		return fmt.Errorf("creating tmux window: %w", err)
	}

	// Set remain-on-exit
	_ = e.Tmux.SetWindowOption(tmuxWinID, "remain-on-exit", "on")

	// Clean up placeholder windows now that a real window exists
	e.cleanPlaceholders(dockName)

	// Determine the effective agent and generate its config if needed
	effectiveAgent := agent
	if effectiveAgent == "" && !shell && cmd == "" {
		effectiveAgent = e.Config.Docks[dockName].Agent
	}
	if effectiveAgent != "" && !shell {
		// Generate agent config file for this agent type (may differ from workspace creation agent)
		_ = e.generateAgentConfig(dockName, effectiveAgent, wsID, ws.Name, ws.Path, ws.Type, ws.Repo)
	}

	// Launch into the first pane
	panes, _ := e.Tmux.ListPanes(tmuxWinID)
	var tmuxPaneID string
	if len(panes) > 0 {
		tmuxPaneID = panes[0].ID
	}
	paneType, paneAgent, paneCmd := e.launchPaneInTmux(tmuxPaneID, dockName, effectiveAgent, shell, cmd)

	// Add window to manifest
	win := manifest.Window{
		ID:           winID,
		TmuxWindowID: tmuxWinID,
		Name:         winName,
		Panes: []manifest.Pane{
			{
				ID:      1,
				Type:    paneType,
				Agent:   paneAgent,
				Command: paneCmd,
			},
		},
	}
	ws.Windows = append(ws.Windows, win)

	return e.saveManifest(m)
}

// WinClose closes a window (not the workspace).
func (e *Engine) WinClose(dockName, wsID string, winID int) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[dockName]
	if !ok {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws, ok := dockState.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}

	found := false
	for i, win := range ws.Windows {
		if win.ID == winID {
			if win.TmuxWindowID != "" {
				e.ensurePlaceholderIfLastWindow(dockName, win.TmuxWindowID)
				_ = e.Tmux.KillWindow(win.TmuxWindowID)
			}
			ws.Windows = append(ws.Windows[:i], ws.Windows[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("window %d not found in workspace %s", winID, wsID)
	}

	return e.saveManifest(m)
}

// WinRestart kills the agent in a window and respawns it with fresh config.
// Each pane is respawned according to its recorded type (agent/cmd/shell).
func (e *Engine) WinRestart(dockName, wsID string, winID int) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dockState, ok := m.Docks[dockName]
	if !ok {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws, ok := dockState.Workspaces[wsID]
	if !ok {
		return fmt.Errorf("workspace %s not found in dock %s", wsID, dockName)
	}

	var win *manifest.Window
	for i := range ws.Windows {
		if ws.Windows[i].ID == winID {
			win = &ws.Windows[i]
			break
		}
	}
	if win == nil {
		return fmt.Errorf("window %d not found", winID)
	}

	dockCfg := e.Config.Docks[dockName]

	// Regenerate agent config for each agent type used in this window
	for _, pane := range win.Panes {
		if pane.Type == manifest.PaneTypeAgent && pane.Agent != "" {
			_ = e.generateAgentConfig(dockName, pane.Agent, wsID, ws.Name, ws.Path, ws.Type, ws.Repo)
		}
	}

	// Respawn each pane according to its recorded type
	if win.TmuxWindowID == "" {
		return nil
	}
	tmuxPanes, err := e.Tmux.ListPanes(win.TmuxWindowID)
	if err != nil {
		return nil
	}

	for i, pane := range win.Panes {
		if i >= len(tmuxPanes) {
			break
		}
		tmuxPaneID := tmuxPanes[i].ID

		// respawn-pane -k kills the live process and respawns atomically.
		// No separate SIGTERM needed.
		var respawnCmd string
		switch pane.Type {
		case manifest.PaneTypeAgent:
			agentName := pane.Agent
			if agentName == "" {
				agentName = dockCfg.Agent
			}
			if agentName != "" {
				if _, ok := e.Config.Agents[agentName]; ok {
					respawnCmd = e.buildAgentCommand(agentName, dockCfg)
				}
			}
		case manifest.PaneTypeCmd:
			if pane.Command != "" {
				respawnCmd = pane.Command
			}
		case manifest.PaneTypeShell:
			// Empty command = respawn-pane omits it, giving a default shell
		}
		_ = e.Tmux.RespawnPane(tmuxPaneID, ws.Path, respawnCmd)
	}

	return nil
}
