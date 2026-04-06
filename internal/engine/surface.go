package engine

import (
	"fmt"

	"github.com/commontoolsinc/bay/internal/manifest"
)

// uniqueSurfaceName returns a name that doesn't collide with existing surfaces.
// If "shell" is taken, tries "shell-2", "shell-3", etc.
func uniqueSurfaceName(ws *manifest.Workspace, name string) string {
	if ws.FindSurface(name) == nil {
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if ws.FindSurface(candidate) == nil {
			return candidate
		}
	}
}

// SurfaceAdd adds a new surface to a workspace.
func (e *Engine) SurfaceAdd(dockName, wsName string, surfaceType manifest.SurfaceType, name, agent, cmd, splitDir string) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dock := m.FindDock(dockName)
	if dock == nil {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws := dock.FindWorkspace(wsName)
	if ws == nil {
		return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
	}

	// Determine the layout group — find an existing tmux window to split into,
	// or create a new one.
	var tmuxWindowID string
	var layoutGroup int
	var splitFromSurface int

	if splitDir != "" && len(ws.Surfaces) > 0 {
		// Split from an existing surface's tmux window.
		// Find the last tmux surface as the split parent.
		for i := len(ws.Surfaces) - 1; i >= 0; i-- {
			s := &ws.Surfaces[i]
			if s.Tmux != nil && s.Tmux.WindowID != "" {
				tmuxWindowID = s.Tmux.WindowID
				layoutGroup = s.Tmux.LayoutGroup
				splitFromSurface = s.ID
				break
			}
		}
	}

	var tmuxPaneID string

	if tmuxWindowID != "" && splitDir != "" {
		// Split an existing window.
		newPaneID, err := e.Tmux.SplitWindow(tmuxWindowID, splitDir, ws.Path)
		if err != nil {
			return fmt.Errorf("splitting window: %w", err)
		}
		tmuxPaneID = newPaneID
	} else {
		// Create a new tmux window.
		winID, err := e.Tmux.NewWindow(dockName, ws.Name, ws.Path)
		if err != nil {
			return fmt.Errorf("creating tmux window: %w", err)
		}
		_ = e.Tmux.SetWindowOption(winID, "remain-on-exit", "on")
		e.cleanPlaceholders(dockName)

		tmuxWindowID = winID
		layoutGroup = nextLayoutGroup(ws)

		panes, _ := e.Tmux.ListPanes(winID)
		if len(panes) > 0 {
			tmuxPaneID = panes[0].ID
		}
	}

	// Launch the surface process.
	surface := e.launchSurfaceInTmux(tmuxPaneID, dockName, surfaceType, agent, cmd)
	surface.Name = uniqueSurfaceName(ws, name)
	surface.Tmux.PaneID = tmuxPaneID
	surface.Tmux.WindowID = tmuxWindowID
	surface.Tmux.LayoutGroup = layoutGroup
	surface.Tmux.SplitFrom = splitFromSurface
	surface.Tmux.SplitDir = splitDir

	if _, err := ws.AddSurface(surface); err != nil {
		return err
	}

	return e.saveManifest(m)
}

// SurfaceAddGUI adds a GUI application surface (e.g., an editor) to a workspace.
// Unlike SurfaceAdd, no tmux operations are performed.
func (e *Engine) SurfaceAddGUI(dockName, wsName, name, appCommand string, pid int) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dock := m.FindDock(dockName)
	if dock == nil {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws := dock.FindWorkspace(wsName)
	if ws == nil {
		return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
	}

	surface := manifest.Surface{
		Name:    uniqueSurfaceName(ws, name),
		Type:    manifest.SurfaceTypeEditor,
		Backend: manifest.SurfaceBackendGUI,
		GUI: &manifest.GUIAttrs{
			AppCommand: appCommand,
			PID:        pid,
		},
	}

	if _, err := ws.AddSurface(surface); err != nil {
		return err
	}

	return e.saveManifest(m)
}

// SurfaceClose removes a surface from a workspace.
func (e *Engine) SurfaceClose(dockName, wsName, surfaceName string) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dock := m.FindDock(dockName)
	if dock == nil {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws := dock.FindWorkspace(wsName)
	if ws == nil {
		return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
	}

	s := ws.FindSurface(surfaceName)
	if s == nil {
		return fmt.Errorf("surface %q not found in workspace %q", surfaceName, wsName)
	}

	// If this is the last surface in its layout group, kill the tmux window.
	// Otherwise, kill just the pane.
	if s.Tmux != nil && s.Tmux.WindowID != "" {
		if countSurfacesInLayoutGroup(ws, s.Tmux.LayoutGroup) <= 1 {
			e.ensurePlaceholderIfLastWindow(dockName, s.Tmux.WindowID)
			_ = e.Tmux.KillWindow(s.Tmux.WindowID)
		} else if s.Tmux.PaneID != "" {
			_ = e.Tmux.KillPane(s.Tmux.PaneID)
		}
	}

	if err := ws.RemoveSurface(surfaceName); err != nil {
		return err
	}

	return e.saveManifest(m)
}

// SurfaceRestart respawns a surface's process.
func (e *Engine) SurfaceRestart(dockName, wsName, surfaceName string) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dock := m.FindDock(dockName)
	if dock == nil {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	ws := dock.FindWorkspace(wsName)
	if ws == nil {
		return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
	}

	s := ws.FindSurface(surfaceName)
	if s == nil {
		return fmt.Errorf("surface %q not found in workspace %q", surfaceName, wsName)
	}

	if s.Tmux == nil || s.Tmux.PaneID == "" {
		return fmt.Errorf("surface %q has no tmux pane to restart", surfaceName)
	}

	dockCfg := e.Config.Docks[dockName]
	var respawnCmd string

	switch s.Type {
	case manifest.SurfaceTypeAgent:
		if s.Agent != nil && *s.Agent != "" {
			if _, ok := e.Config.Agents[*s.Agent]; ok {
				respawnCmd = e.buildAgentResumeCommand(*s.Agent, dockCfg)
			}
		}
	case manifest.SurfaceTypeCmd:
		if s.Command != nil {
			respawnCmd = *s.Command
		}
	case manifest.SurfaceTypeShell:
		// Empty command = default shell.
	}

	_ = e.Tmux.RespawnPane(s.Tmux.PaneID, ws.Path, respawnCmd)
	return nil
}

// nextLayoutGroup returns the next layout group number for a workspace.
func nextLayoutGroup(ws *manifest.Workspace) int {
	max := 0
	for _, s := range ws.Surfaces {
		if s.Tmux != nil && s.Tmux.LayoutGroup > max {
			max = s.Tmux.LayoutGroup
		}
	}
	return max + 1
}

// countSurfacesInLayoutGroup counts surfaces sharing a layout group.
func countSurfacesInLayoutGroup(ws *manifest.Workspace, group int) int {
	count := 0
	for _, s := range ws.Surfaces {
		if s.Tmux != nil && s.Tmux.LayoutGroup == group {
			count++
		}
	}
	return count
}
