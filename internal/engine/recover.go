package engine

import (
	"fmt"
	"os"
	"syscall"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// Recover reconstructs all tmux state from the manifest after reboot.
func (e *Engine) Recover() ([]string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	var attachCmds []string

	for dockName, dockState := range m.Docks {
		dockCfg, hasCfg := e.Config.Docks[dockName]

		if err := e.ensureSession(dockName); err != nil {
			return nil, err
		}

		e.recoverDockWorkspaces(dockName, dockState, dockCfg, hasCfg)
		e.cleanPlaceholders(dockName)

		attachCmds = append(attachCmds, fmt.Sprintf("tmux attach -t %s", dockName))
	}

	// Save updated manifest (with new tmux IDs)
	if err := e.saveManifest(m); err != nil {
		return nil, err
	}

	return attachCmds, nil
}

// recoverDockWorkspaces handles recovery for all workspaces in a dock.
func (e *Engine) recoverDockWorkspaces(dockName string, dockState *manifest.DockState, dockCfg config.DockConfig, hasCfg bool) {
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
				// Window exists — reconcile panes. Check if any
				// manifest panes are missing and recreate them.
				e.reconcileWindowPanes(ws.Windows[i].TmuxWindowID, win, ws.Path, dockCfg, hasCfg)
			} else {
				// Create new window
				newID, err := e.Tmux.NewWindow(dockName, win.Name, ws.Path)
				if err != nil {
					continue
				}
				ws.Windows[i].TmuxWindowID = newID
				_ = e.Tmux.SetWindowOption(newID, "remain-on-exit", "on")

				// Recreate all panes — these are freshly created, always launch
				for j, pane := range win.Panes {
					if j == 0 {
						// First pane already exists in new window
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
	}
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

	// If tmux has at least as many panes as the manifest, assume they're present.
	// If tmux has fewer, recreate the missing ones.
	if tmuxPaneCount >= manifestPaneCount {
		return
	}

	// Recreate missing panes (those beyond what tmux currently has)
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
	// Determine the target pane ID
	var targetPaneID string
	if tmuxPaneID != "" {
		targetPaneID = tmuxPaneID
	} else if windowID != "" {
		// First pane: look it up from the window
		panes, err := e.Tmux.ListPanes(windowID)
		if err != nil || len(panes) == 0 {
			return
		}
		targetPaneID = panes[0].ID
	} else {
		return
	}

	// For panes in existing windows (not freshly created by recovery),
	// skip launch if a foreground process is already running.
	// Freshly created panes always have a shell PID, which is expected —
	// we need to launch into them.
	if !newlyCreated {
		pid, err := e.Tmux.GetPanePID(targetPaneID)
		if err == nil && pid > 0 {
			if proc, findErr := os.FindProcess(pid); findErr == nil {
				if proc.Signal(syscall.Signal(0)) == nil {
					return // pane has a live process, skip
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
