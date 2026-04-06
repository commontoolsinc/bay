package engine

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/commontoolsinc/bay/internal/manifest"
)

// RecoverResult holds the results of a recovery operation.
type RecoverResult struct {
	Dock      string
	Recovered []string // workspace names that had surfaces recreated
	AttachCmd string
}

type recoverOutcome struct {
	recovered []string
	errs      []string
}

// Recover reconstructs all tmux state from the manifest after reboot.
// Note: agent config files are no longer generated during recovery.
// Agents are relaunched directly; project-level CLAUDE.md provides bay awareness.
func (e *Engine) Recover() ([]RecoverResult, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	var results []RecoverResult
	var errs []string

	for i := range m.Docks {
		dock := &m.Docks[i]
		agentArgs := e.resolvedDockAgentArgs(dock.Name, m)

		if err := e.ensureSession(dock.Name); err != nil {
			return nil, err
		}

		outcome := e.recoverDockWorkspaces(dock, agentArgs)
		e.cleanPlaceholders(dock.Name)

		// Recover host terminal if configured and dead.
		terminal := e.Config.ResolvedDockTerminal(dock.Name)
		if dock.Host != nil && terminal != "" {
			if dock.Host.PID == 0 || !processAlive(dock.Host.PID) {
				if pid, err := launchTerminal(terminal, dock.Name); err == nil {
					dock.Host.PID = pid
				}
			}
		}

		results = append(results, RecoverResult{
			Dock:      dock.Name,
			Recovered: outcome.recovered,
			AttachCmd: fmt.Sprintf("tmux attach -t %s", dock.Name),
		})
		errs = append(errs, outcome.errs...)
	}

	if err := e.saveManifest(m); err != nil {
		return nil, err
	}

	if len(errs) > 0 {
		return results, fmt.Errorf("recovery completed with errors:\n%s", strings.Join(errs, "\n"))
	}
	return results, nil
}

// DockRecover recovers a single dock by name.
func (e *Engine) DockRecover(name string) ([]string, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	dock := m.FindDock(name)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q in manifest", name)
	}
	agentArgs := e.resolvedDockAgentArgs(name, m)

	if err := e.ensureSession(name); err != nil {
		return nil, err
	}

	outcome := e.recoverDockWorkspaces(dock, agentArgs)
	e.cleanPlaceholders(name)

	if err := e.saveManifest(m); err != nil {
		return nil, err
	}

	if len(outcome.errs) > 0 {
		return outcome.recovered, fmt.Errorf("recovery completed with errors:\n%s", strings.Join(outcome.errs, "\n"))
	}
	return outcome.recovered, nil
}

// recoverDockWorkspaces handles recovery for all workspaces in a dock.
func (e *Engine) recoverDockWorkspaces(dock *manifest.Dock, agentArgs []string) recoverOutcome {
	outcome := recoverOutcome{}

	for i := range dock.Workspaces {
		ws := &dock.Workspaces[i]

		if _, err := os.Stat(ws.Path); err != nil {
			continue
		}

		wsRecovered := false

		// Group surfaces by layout group for recovery.
		groups := groupSurfacesByLayout(ws)

		for layoutGroup, surfaceIndices := range groups {
			// Check if the tmux window for this group still exists.
			var existingWindowID string
			for _, idx := range surfaceIndices {
				s := &ws.Surfaces[idx]
				if s.Tmux != nil && s.Tmux.WindowID != "" {
					exists, _ := e.Tmux.WindowExists(s.Tmux.WindowID)
					if exists {
						existingWindowID = s.Tmux.WindowID
						break
					}
				}
			}

			if existingWindowID != "" {
				// Window exists — reconcile panes.
				e.reconcileSurfaces(existingWindowID, ws, surfaceIndices, agentArgs)
			} else {
				// Window gone — recreate it.
				wsRecovered = true
				newWindowID, err := e.Tmux.NewWindow(dock.Name, ws.Name, ws.Path)
				if err != nil {
					continue
				}
				_ = e.Tmux.SetWindowOption(newWindowID, "remain-on-exit", "on")

				for j, idx := range surfaceIndices {
					s := &ws.Surfaces[idx]
					if s.Tmux == nil {
						continue
					}
					s.Tmux.WindowID = newWindowID
					s.Tmux.LayoutGroup = layoutGroup

					if j == 0 {
						// First surface uses the window's initial pane.
						panes, err := e.Tmux.ListPanes(newWindowID)
						if err == nil && len(panes) > 0 {
							s.Tmux.PaneID = panes[0].ID
						}
						e.recoverSurfaceLaunch(s, s.Tmux.PaneID, agentArgs, true)
					} else {
						// Subsequent surfaces split from the window.
						dir := s.Tmux.SplitDir
						if dir == "" {
							dir = "v"
						}
						newPaneID, err := e.Tmux.SplitWindow(newWindowID, dir, ws.Path)
						if err != nil {
							continue
						}
						s.Tmux.PaneID = newPaneID
						e.recoverSurfaceLaunch(s, newPaneID, agentArgs, true)
					}
				}
			}
		}

		if wsRecovered {
			outcome.recovered = append(outcome.recovered, ws.Name)
		}
	}
	return outcome
}

// reconcileSurfaces checks existing panes against manifest surfaces and repairs missing ones.
func (e *Engine) reconcileSurfaces(tmuxWindowID string, ws *manifest.Workspace, surfaceIndices []int, agentArgs []string) {
	tmuxPanes, err := e.Tmux.ListPanes(tmuxWindowID)
	if err != nil {
		return
	}

	// Update pane IDs for existing surfaces.
	for i, idx := range surfaceIndices {
		s := &ws.Surfaces[idx]
		if s.Tmux != nil && i < len(tmuxPanes) {
			s.Tmux.PaneID = tmuxPanes[i].ID
		}
	}

	// Recreate missing panes.
	for j := len(tmuxPanes); j < len(surfaceIndices); j++ {
		s := &ws.Surfaces[surfaceIndices[j]]
		if s.Tmux == nil {
			continue
		}
		dir := s.Tmux.SplitDir
		if dir == "" {
			dir = "v"
		}
		newPaneID, err := e.Tmux.SplitWindow(tmuxWindowID, dir, ws.Path)
		if err != nil {
			continue
		}
		s.Tmux.PaneID = newPaneID
		e.recoverSurfaceLaunch(s, newPaneID, agentArgs, true)
	}
}

// recoverSurfaceLaunch sends the appropriate launch command to a recovered surface.
func (e *Engine) recoverSurfaceLaunch(s *manifest.Surface, tmuxPaneID string, agentArgs []string, newlyCreated bool) {
	if tmuxPaneID == "" {
		return
	}

	if !newlyCreated {
		pid, err := e.Tmux.GetPanePID(tmuxPaneID)
		if err == nil && pid > 0 {
			if proc, findErr := os.FindProcess(pid); findErr == nil {
				if proc.Signal(syscall.Signal(0)) == nil {
					return
				}
			}
		}
	}

	switch s.Type {
	case manifest.SurfaceTypeAgent:
		if s.Agent == nil || *s.Agent == "" {
			return
		}
		agentName := *s.Agent
		if _, ok := e.Config.Agents[agentName]; !ok {
			return
		}
		agentCmd := e.buildAgentResumeCommand(agentName, agentArgs)
		_ = e.Tmux.SendKeys(tmuxPaneID, agentCmd)
	case manifest.SurfaceTypeCmd:
		if s.Command != nil && *s.Command != "" {
			_ = e.Tmux.SendKeys(tmuxPaneID, *s.Command)
		}
	case manifest.SurfaceTypeShell:
		// No command needed.
	}
}

// groupSurfacesByLayout groups surface indices by their layout group.
// Returns a map of layout group -> slice of surface indices, ordered by group number.
func groupSurfacesByLayout(ws *manifest.Workspace) map[int][]int {
	groups := map[int][]int{}
	for i, s := range ws.Surfaces {
		if s.Tmux == nil {
			continue
		}
		groups[s.Tmux.LayoutGroup] = append(groups[s.Tmux.LayoutGroup], i)
	}
	return groups
}
