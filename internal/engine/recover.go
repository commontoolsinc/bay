package engine

import (
	"fmt"
	"os"
	"syscall"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// RecoverResult holds the results of a recovery operation.
type RecoverResult struct {
	Dock      string
	Recovered []string // workspace names that had windows recreated
	AttachCmd string
}

// Recover reconstructs all tmux state from the manifest after reboot.
func (e *Engine) Recover() ([]RecoverResult, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	var results []RecoverResult

	for dockName, dockState := range m.Docks {
		dockCfg, hasCfg := e.Config.Docks[dockName]

		if err := e.ensureSession(dockName); err != nil {
			return nil, err
		}

		recovered := e.recoverDockWorkspaces(dockName, dockState, dockCfg, hasCfg)
		e.cleanPlaceholders(dockName)

		results = append(results, RecoverResult{
			Dock:      dockName,
			Recovered: recovered,
			AttachCmd: fmt.Sprintf("tmux attach -t %s", dockName),
		})
	}

	// Save updated manifest (with new tmux IDs)
	if err := e.saveManifest(m); err != nil {
		return nil, err
	}

	return results, nil
}

// DockRecover recovers a single dock by name.
// Returns the names of workspaces that were recovered.
func (e *Engine) DockRecover(name string) ([]string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	dockState, ok := m.Docks[name]
	if !ok {
		return nil, fmt.Errorf("unknown dock %q in manifest", name)
	}
	dockCfg, hasCfg := e.Config.Docks[name]

	if err := e.ensureSession(name); err != nil {
		return nil, err
	}

	recovered := e.recoverDockWorkspaces(name, dockState, dockCfg, hasCfg)
	e.cleanPlaceholders(name)

	if err := e.saveManifest(m); err != nil {
		return nil, err
	}

	return recovered, nil
}

// recoverDockWorkspaces handles recovery for all workspaces in a dock.
// Returns the names of workspaces that were recovered (had windows recreated).
func (e *Engine) recoverDockWorkspaces(dockName string, dockState *manifest.DockState, dockCfg config.DockConfig, hasCfg bool) []string {
	var recovered []string
	for wsID, ws := range dockState.Workspaces {
		// Verify workspace path exists
		if _, err := os.Stat(ws.Path); err != nil {
			continue
		}

		// Regenerate agent config for each agent type used in this workspace.
		dockAgent := ""
		if hasCfg {
			dockAgent = dockCfg.Agent
		}
		for agentName := range collectWorkspaceAgents(ws, dockAgent) {
			_ = e.generateAgentConfig(dockName, agentName, wsID, ws.Name, ws.Path, ws.Type, ws.Repo)
		}

		wsRecovered := false
		for i, win := range ws.Windows {
			// Check if window still exists
			windowExists := false
			if win.TmuxWindowID != "" {
				windowExists, _ = e.Tmux.WindowExists(win.TmuxWindowID)
			}

			// Try finding by name as fallback
			if !windowExists {
				foundID, err := e.Tmux.FindWindowByName(dockName, win.Name)
				if err == nil && foundID != "" {
					ws.Windows[i].TmuxWindowID = foundID
					windowExists = true
				}
			}

			if windowExists {
				e.reconcileWindowPanes(ws.Windows[i].TmuxWindowID, win, ws.Path, dockCfg, hasCfg)
			} else {
				wsRecovered = true

				newID, err := e.Tmux.NewWindow(dockName, win.Name, ws.Path)
				if err != nil {
					continue
				}
				ws.Windows[i].TmuxWindowID = newID
				_ = e.Tmux.SetWindowOption(newID, "remain-on-exit", "on")

				for j, pane := range win.Panes {
					if j == 0 {
						e.recoverPaneLaunch(pane, newID, "", dockCfg, hasCfg, true)
						continue
					}
					dir := pane.SplitDir
					if dir == "" {
						dir = "v"
					}
					newPaneID, err := e.Tmux.SplitWindow(newID, dir, ws.Path)
					if err != nil {
						continue
					}
					e.recoverPaneLaunch(pane, "", newPaneID, dockCfg, hasCfg, true)
				}
			}
		}
		if wsRecovered {
			name := ws.Name
			if name == "" {
				name = wsID
			}
			recovered = append(recovered, name)
		}
	}
	return recovered
}

// reconcileWindowPanes checks an existing window's panes against the manifest
// and repairs missing ones. Panes with live foreground processes are skipped.
func (e *Engine) reconcileWindowPanes(tmuxWindowID string, win manifest.Window, wsPath string, dockCfg config.DockConfig, hasCfg bool) {
	tmuxPanes, err := e.Tmux.ListPanes(tmuxWindowID)
	if err != nil {
		return
	}

	manifestPaneCount := len(win.Panes)
	tmuxPaneCount := len(tmuxPanes)

	if tmuxPaneCount >= manifestPaneCount {
		return
	}

	for j := tmuxPaneCount; j < manifestPaneCount; j++ {
		pane := win.Panes[j]
		dir := pane.SplitDir
		if dir == "" {
			dir = "v"
		}
		newPaneID, err := e.Tmux.SplitWindow(tmuxWindowID, dir, wsPath)
		if err != nil {
			continue
		}
		e.recoverPaneLaunch(pane, "", newPaneID, dockCfg, hasCfg, true)
	}
}

// recoverPaneLaunch sends the appropriate launch command to a recovered pane.
// When newlyCreated is false (pane found in an existing window), skips panes
// that have a live foreground process to avoid corrupting running agents.
// When newlyCreated is true (pane just created by bay), always launches.
func (e *Engine) recoverPaneLaunch(pane manifest.Pane, windowID string, tmuxPaneID string, dockCfg config.DockConfig, hasCfg bool, newlyCreated bool) {
	var targetPaneID string
	if tmuxPaneID != "" {
		targetPaneID = tmuxPaneID
	} else if windowID != "" {
		panes, err := e.Tmux.ListPanes(windowID)
		if err != nil || len(panes) == 0 {
			return
		}
		targetPaneID = panes[0].ID
	} else {
		return
	}

	if !newlyCreated {
		pid, err := e.Tmux.GetPanePID(targetPaneID)
		if err == nil && pid > 0 {
			if proc, findErr := os.FindProcess(pid); findErr == nil {
				if proc.Signal(syscall.Signal(0)) == nil {
					return
				}
			}
		}
	}

	switch pane.Type {
	case manifest.PaneTypeAgent:
		agentName := pane.Agent
		if agentName == "" && hasCfg {
			agentName = dockCfg.Agent
		}
		if agentName == "" {
			return
		}
		if _, ok := e.Config.Agents[agentName]; !ok {
			return
		}
		agentCmd := e.buildAgentCommand(agentName, dockCfg)
		_ = e.Tmux.SendKeys(targetPaneID, agentCmd)
	case manifest.PaneTypeCmd:
		if pane.Command != "" {
			_ = e.Tmux.SendKeys(targetPaneID, pane.Command)
		}
	case manifest.PaneTypeShell:
		// Shell panes need no command
	}
}
