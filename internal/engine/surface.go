package engine

import (
	"fmt"
	"time"

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

func (e *Engine) preferredSplitTarget(ws *manifest.Workspace) *manifest.Surface {
	if paneID, err := e.Tmux.CurrentPaneID(); err == nil {
		for i := range ws.Surfaces {
			s := &ws.Surfaces[i]
			if s.Tmux != nil && s.Tmux.PaneID == paneID {
				return s
			}
		}
	}
	if ws.LastFocused != 0 {
		if s := ws.FindSurfaceByID(ws.LastFocused); s != nil && s.Tmux != nil && s.Tmux.WindowID != "" {
			return s
		}
	}
	for i := len(ws.Surfaces) - 1; i >= 0; i-- {
		s := &ws.Surfaces[i]
		if s.Tmux != nil && s.Tmux.WindowID != "" {
			return s
		}
	}
	return nil
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
	var tmuxSplitTargetID string
	var layoutGroup int
	var splitFromSurface int

	if splitDir != "" && len(ws.Surfaces) > 0 {
		if parent := e.preferredSplitTarget(ws); parent != nil {
			tmuxWindowID = parent.Tmux.WindowID
			tmuxSplitTargetID = parent.Tmux.PaneID
			layoutGroup = parent.Tmux.LayoutGroup
			splitFromSurface = parent.ID
		}
	}

	var tmuxPaneID string
	rollbackSurface := func() {}

	if tmuxWindowID != "" && splitDir != "" {
		// Split an existing window.
		targetID := tmuxSplitTargetID
		if targetID == "" {
			targetID = tmuxWindowID
		}
		newPaneID, err := e.Tmux.SplitWindow(targetID, splitDir, ws.Path)
		if err != nil {
			return fmt.Errorf("splitting window: %w", err)
		}
		tmuxPaneID = newPaneID
		rollbackSurface = func() {
			_ = e.Tmux.KillPane(newPaneID)
		}
	} else {
		// Create a new tmux window.
		winID, err := e.Tmux.NewWindow(dockName, ws.Name, ws.Path)
		if err != nil {
			return fmt.Errorf("creating tmux window: %w", err)
		}
		e.cleanPlaceholders(dockName)

		tmuxWindowID = winID
		layoutGroup = nextLayoutGroup(ws)

		panes, _ := e.Tmux.ListPanes(winID)
		if len(panes) > 0 {
			tmuxPaneID = panes[0].ID
		}
		rollbackSurface = func() {
			_ = e.Tmux.KillWindow(winID)
		}
	}

	if surfaceType == manifest.SurfaceTypeAgent {
		if err := e.validateAgentName(agent); err != nil {
			rollbackSurface()
			return err
		}
	}

	// Resolve agent args for the dock.
	m2, _ := e.LoadManifest()
	agentArgs := e.resolvedDockAgentArgs(dockName, m2)

	// Launch the surface process.
	surface, err := e.launchSurfaceInTmux(tmuxPaneID, dockName, surfaceType, agent, cmd, agentArgs)
	if err != nil {
		rollbackSurface()
		return err
	}
	surface.Name = uniqueSurfaceName(ws, name)
	surface.Tmux.PaneID = tmuxPaneID
	surface.Tmux.WindowID = tmuxWindowID
	surface.Tmux.LayoutGroup = layoutGroup
	surface.Tmux.SplitFrom = splitFromSurface
	surface.Tmux.SplitDir = splitDir

	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			rollbackSurface()
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspace(wsName)
		if ws == nil {
			rollbackSurface()
			return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
		}

		surface.Name = uniqueSurfaceName(ws, name)
		if _, err := ws.AddSurface(surface); err != nil {
			rollbackSurface()
			return err
		}
		ws.LastActive = time.Now().Unix()
		return nil
	})
}

// SurfaceAddGUI adds a GUI application surface (e.g., an editor) to a workspace.
// Unlike SurfaceAdd, no tmux operations are performed.
func (e *Engine) SurfaceAddGUI(dockName, wsName, name, appCommand string, pid int) error {
	return e.withManifest(func(m *manifest.Manifest) error {
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
		ws.LastActive = time.Now().Unix()
		return nil
	})
}

// SurfaceClose removes a surface from a workspace.
//
// The manifest update happens BEFORE the destructive tmux kill so that
// when bay is invoked from inside the pane being closed, the user-visible
// state is already correct by the time tmux SIGHUPs bay. See also
// SurfaceRestart and the closeWorkspaceState helper for the same pattern.
func (e *Engine) SurfaceClose(dockName, wsName, surfaceName string, force bool) error {
	var windowIDToKill, paneIDToKill string
	wsEmpty := false

	err := e.withManifest(func(m *manifest.Manifest) error {
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

		// Capture what we'll kill before the surface is removed from
		// the in-memory manifest (RemoveSurface invalidates s).
		if s.Tmux != nil && s.Tmux.WindowID != "" {
			if countSurfacesInLayoutGroup(ws, s.Tmux.LayoutGroup) <= 1 {
				windowIDToKill = s.Tmux.WindowID
			} else if s.Tmux.PaneID != "" {
				paneIDToKill = s.Tmux.PaneID
			}
		}

		if err := ws.RemoveSurface(s.Name); err != nil {
			return err
		}
		ws.LastActive = time.Now().Unix()
		wsEmpty = len(ws.Surfaces) == 0
		return nil
	})
	if err != nil {
		return err
	}

	// Manifest is saved. Now do the destructive tmux work — bay may die
	// mid-call if it's running in the pane being killed, but the
	// user-visible state is already correct.
	if windowIDToKill != "" {
		e.ensurePlaceholderIfLastWindow(dockName, windowIDToKill)
		_ = e.Tmux.KillWindow(windowIDToKill)
	} else if paneIDToKill != "" {
		_ = e.Tmux.KillPane(paneIDToKill)
	}

	// Auto-close the workspace if no surfaces remain. If the workspace
	// has dirty/unpushed state, WsClose returns an error — surface is
	// already gone, so just report the leftover workspace to the caller.
	if wsEmpty {
		if err := e.WsClose(dockName, wsName, force); err != nil {
			return fmt.Errorf("surface closed, but workspace %q not removed: %w", wsName, err)
		}
	}
	return nil
}

// SurfaceRestart respawns a surface's process.
func (e *Engine) SurfaceRestart(dockName, wsName, surfaceName string) error {
	// First, gather what we need from the manifest and bump LastActive.
	// We do this BEFORE the tmux respawn so that a save failure doesn't
	// mask a successful respawn (the user would see an error message
	// while the surface had actually restarted).
	var paneID, wsPath, respawnCmd string
	err := e.withManifest(func(m *manifest.Manifest) error {
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

		agentArgs := e.resolvedDockAgentArgs(dockName, m)
		switch s.Type {
		case manifest.SurfaceTypeAgent:
			if s.Agent != nil && *s.Agent != "" {
				cmd, err := e.buildAgentResumeCommand(*s.Agent, agentArgs)
				if err != nil {
					return err
				}
				respawnCmd = cmd
			}
		case manifest.SurfaceTypeCmd:
			if s.Command != nil {
				respawnCmd = *s.Command
			}
		case manifest.SurfaceTypeShell:
			// Empty command = default shell.
		}

		paneID = s.Tmux.PaneID
		wsPath = ws.Path
		ws.LastActive = time.Now().Unix()
		return nil
	})
	if err != nil {
		return err
	}

	_ = e.Tmux.RespawnPane(paneID, wsPath, respawnCmd)
	return nil
}

// SurfaceRename renames a surface within a workspace.
func (e *Engine) SurfaceRename(dockName, wsName, oldName, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}

	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", dockName)
		}
		ws := dock.FindWorkspace(wsName)
		if ws == nil {
			return fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
		}
		s := ws.FindSurface(oldName)
		if s == nil {
			return fmt.Errorf("surface %q not found in workspace %q", oldName, wsName)
		}
		if existing := ws.FindSurface(newName); existing != nil {
			return fmt.Errorf("surface name %q already in use in workspace %q", newName, wsName)
		}
		s.Name = newName
		ws.LastActive = time.Now().Unix()
		return nil
	})
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
