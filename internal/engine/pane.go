package engine

import (
	"fmt"

	"github.com/commontoolsinc/bay/internal/manifest"
)

// PaneAdd adds a pane to the current window.
func (e *Engine) PaneAdd(dockName, wsID string, winID int, agent string, shell bool, cmd string, splitDir string) error {
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

	if splitDir == "" {
		splitDir = "v"
	}

	// Determine the actual split parent: the currently active tmux pane.
	// Match it against manifest panes by position in the tmux pane list.
	splitFrom := 0
	tmuxPanesBefore, err := e.Tmux.ListPanes(win.TmuxWindowID)
	if err == nil {
		for tmuxIdx, tp := range tmuxPanesBefore {
			if tp.Active && tmuxIdx < len(win.Panes) {
				splitFrom = win.Panes[tmuxIdx].ID
				break
			}
		}
		// Fallback: if no active match, use last manifest pane
		if splitFrom == 0 && len(win.Panes) > 0 {
			splitFrom = win.Panes[len(win.Panes)-1].ID
		}
	}

	// Split the window
	newPaneID, err := e.Tmux.SplitWindow(win.TmuxWindowID, splitDir, ws.Path)
	if err != nil {
		return fmt.Errorf("splitting window: %w", err)
	}

	// Determine pane type
	paneID := manifest.NextPaneID(win)
	var paneType manifest.PaneType
	var paneAgent string
	var paneCmd string
	switch {
	case shell:
		paneType = manifest.PaneTypeShell
	case cmd != "":
		paneType = manifest.PaneTypeCmd
		paneCmd = cmd
		_ = e.Tmux.SendKeys(newPaneID, cmd)
	case agent != "":
		paneType = manifest.PaneTypeAgent
		paneAgent = agent
		dockCfg := e.Config.Docks[dockName]
		agentCmd := e.buildAgentCommand(agent, dockCfg)
		_ = e.Tmux.SendKeys(newPaneID, agentCmd)
	default:
		paneType = manifest.PaneTypeShell
	}

	pane := manifest.Pane{
		ID:        paneID,
		Type:      paneType,
		Agent:     paneAgent,
		Command:   paneCmd,
		SplitFrom: splitFrom,
		SplitDir:  splitDir,
	}
	win.Panes = append(win.Panes, pane)

	return e.saveManifest(m)
}
