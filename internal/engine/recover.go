package engine

import (
	"fmt"
	"os"
	"slices"
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
	changed   bool
}

// Recover reconstructs all tmux state from the manifest after reboot.
// Note: agent config files are no longer generated during recovery.
// Agents are relaunched directly; project-level CLAUDE.md provides bay awareness.
func (e *Engine) Recover() ([]RecoverResult, error) {
	var results []RecoverResult
	var errs []string
	if err := e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		changed := false
		results = nil
		errs = nil

		for i := range m.Docks {
			dock := &m.Docks[i]
			agentArgs := e.resolvedDockAgentArgs(dock.Name, m)

			if err := e.ensureSession(dock.Name); err != nil {
				return false, err
			}

			outcome := e.recoverDockWorkspaces(dock, agentArgs)
			e.cleanPlaceholders(dock.Name)

			// Recover host terminal if configured and dead.
			terminal := e.Config.ResolvedDockTerminal(dock.Name)
			if dock.Host != nil && terminal != "" {
				if dock.Host.PID == 0 || !processAlive(dock.Host.PID) {
					if pid, err := launchTerminal(terminal, dock.Name); err == nil {
						if dock.Host.PID != pid {
							dock.Host.PID = pid
							outcome.changed = true
						}
					} else {
						outcome.errs = append(outcome.errs, fmt.Sprintf("dock %s: launch terminal: %v", dock.Name, err))
					}
				}
			}

			results = append(results, RecoverResult{
				Dock:      dock.Name,
				Recovered: outcome.recovered,
				AttachCmd: fmt.Sprintf("tmux attach -t %s", dock.Name),
			})
			errs = append(errs, outcome.errs...)
			changed = changed || outcome.changed
		}

		return changed, nil
	}); err != nil {
		return nil, err
	}

	if len(errs) > 0 {
		return results, fmt.Errorf("recovery completed with errors:\n%s", strings.Join(errs, "\n"))
	}
	return results, nil
}

// DockRecover recovers a single dock by name.
func (e *Engine) DockRecover(name string) ([]string, error) {
	var recovered []string
	var errs []string
	if err := e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		dock := m.FindDock(name)
		if dock == nil {
			return false, fmt.Errorf("unknown dock %q in manifest", name)
		}
		agentArgs := e.resolvedDockAgentArgs(name, m)

		if err := e.ensureSession(name); err != nil {
			return false, err
		}

		outcome := e.recoverDockWorkspaces(dock, agentArgs)
		e.cleanPlaceholders(name)
		recovered = outcome.recovered
		errs = outcome.errs
		return outcome.changed, nil
	}); err != nil {
		return nil, err
	}

	if len(errs) > 0 {
		return recovered, fmt.Errorf("recovery completed with errors:\n%s", strings.Join(errs, "\n"))
	}
	return recovered, nil
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

		for _, layoutGroup := range sortedLayoutGroups(groups) {
			surfaceIndices := orderedSurfaceIndices(ws, groups[layoutGroup])
			// Check if the tmux window for this group still exists.
			var existingWindowID string
			for _, idx := range surfaceIndices {
				s := &ws.Surfaces[idx]
				if s.Tmux != nil && s.Tmux.WindowID != "" {
					exists, err := e.Tmux.WindowExists(s.Tmux.WindowID)
					if err != nil {
						outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s: check window %s: %v", ws.Name, s.Tmux.WindowID, err))
						continue
					}
					if exists {
						existingWindowID = s.Tmux.WindowID
						break
					}
				}
			}

			if existingWindowID != "" {
				// Window exists — reconcile panes.
				outcome.changed = e.reconcileSurfaces(existingWindowID, ws, surfaceIndices, agentArgs, &outcome) || outcome.changed
			} else {
				// Window gone — recreate it.
				wsRecovered = true
				newWindowID, err := e.Tmux.NewWindow(dock.Name, ws.Name, ws.Path)
				if err != nil {
					outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s: create window: %v", ws.Name, err))
					continue
				}
				outcome.changed = true

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
						if err != nil {
							outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s: list panes for %s: %v", ws.Name, newWindowID, err))
							continue
						}
						if len(panes) == 0 {
							outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s: window %s has no initial pane", ws.Name, newWindowID))
							continue
						}
						s.Tmux.PaneID = panes[0].ID
						if err := e.recoverSurfaceLaunch(s, s.Tmux.PaneID, agentArgs, true); err != nil {
							outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s surface %s: %v", ws.Name, s.Name, err))
						}
					} else {
						// Subsequent surfaces split from their recorded parent pane.
						if err := e.selectRecoverSplitParent(ws, s); err != nil {
							outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s surface %s: %v", ws.Name, s.Name, err))
							continue
						}
						dir := s.Tmux.SplitDir
						if dir == "" {
							dir = "v"
						}
						newPaneID, err := e.Tmux.SplitWindow(newWindowID, dir, ws.Path)
						if err != nil {
							outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s surface %s: split window: %v", ws.Name, s.Name, err))
							continue
						}
						s.Tmux.PaneID = newPaneID
						if err := e.recoverSurfaceLaunch(s, newPaneID, agentArgs, true); err != nil {
							outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s surface %s: %v", ws.Name, s.Name, err))
						}
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
func (e *Engine) reconcileSurfaces(tmuxWindowID string, ws *manifest.Workspace, surfaceIndices []int, agentArgs []string, outcome *recoverOutcome) bool {
	tmuxPanes, err := e.Tmux.ListPanes(tmuxWindowID)
	if err != nil {
		outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s: list panes for %s: %v", ws.Name, tmuxWindowID, err))
		return false
	}
	changed := false

	// Update pane IDs for existing surfaces.
	for i, idx := range surfaceIndices {
		s := &ws.Surfaces[idx]
		if s.Tmux != nil && i < len(tmuxPanes) && s.Tmux.PaneID != tmuxPanes[i].ID {
			s.Tmux.PaneID = tmuxPanes[i].ID
			changed = true
		}
	}

	// Recreate missing panes.
	for j := len(tmuxPanes); j < len(surfaceIndices); j++ {
		s := &ws.Surfaces[surfaceIndices[j]]
		if s.Tmux == nil {
			continue
		}
		if err := e.selectRecoverSplitParent(ws, s); err != nil {
			outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s surface %s: %v", ws.Name, s.Name, err))
			continue
		}
		dir := s.Tmux.SplitDir
		if dir == "" {
			dir = "v"
		}
		newPaneID, err := e.Tmux.SplitWindow(tmuxWindowID, dir, ws.Path)
		if err != nil {
			outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s surface %s: split window: %v", ws.Name, s.Name, err))
			continue
		}
		s.Tmux.PaneID = newPaneID
		changed = true
		if err := e.recoverSurfaceLaunch(s, newPaneID, agentArgs, true); err != nil {
			outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s surface %s: %v", ws.Name, s.Name, err))
		}
	}
	return changed
}

// recoverSurfaceLaunch sends the appropriate launch command to a recovered surface.
func (e *Engine) recoverSurfaceLaunch(s *manifest.Surface, tmuxPaneID string, agentArgs []string, newlyCreated bool) error {
	if tmuxPaneID == "" {
		return fmt.Errorf("missing tmux pane id")
	}

	if !newlyCreated {
		pid, err := e.Tmux.GetPanePID(tmuxPaneID)
		if err == nil && pid > 0 {
			if proc, findErr := os.FindProcess(pid); findErr == nil {
				if proc.Signal(syscall.Signal(0)) == nil {
					return nil
				}
			}
		}
	}

	switch s.Type {
	case manifest.SurfaceTypeAgent:
		if s.Agent == nil || *s.Agent == "" {
			return fmt.Errorf("missing agent configuration")
		}
		agentName := *s.Agent
		agentCmd, err := e.buildAgentResumeCommand(agentName, agentArgs)
		if err != nil {
			return err
		}
		if err := e.Tmux.SendKeys(tmuxPaneID, agentCmd); err != nil {
			return err
		}
	case manifest.SurfaceTypeCmd:
		if s.Command != nil && *s.Command != "" {
			if err := e.Tmux.SendKeys(tmuxPaneID, *s.Command); err != nil {
				return err
			}
		}
	case manifest.SurfaceTypeShell:
		// No command needed.
	}
	return nil
}

func (e *Engine) selectRecoverSplitParent(ws *manifest.Workspace, s *manifest.Surface) error {
	if s.Tmux == nil || s.Tmux.SplitFrom == 0 {
		return nil
	}
	parent := ws.FindSurfaceByID(s.Tmux.SplitFrom)
	if parent == nil || parent.Tmux == nil || parent.Tmux.PaneID == "" {
		return fmt.Errorf("missing split parent %d", s.Tmux.SplitFrom)
	}
	if err := e.Tmux.SelectPane(parent.Tmux.PaneID); err != nil {
		return fmt.Errorf("select split parent %d: %w", s.Tmux.SplitFrom, err)
	}
	return nil
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

func sortedLayoutGroups(groups map[int][]int) []int {
	keys := make([]int, 0, len(groups))
	for group := range groups {
		keys = append(keys, group)
	}
	slices.Sort(keys)
	return keys
}

func orderedSurfaceIndices(ws *manifest.Workspace, indices []int) []int {
	if len(indices) < 2 {
		return append([]int(nil), indices...)
	}

	idxByID := make(map[int]int, len(indices))
	for _, idx := range indices {
		idxByID[ws.Surfaces[idx].ID] = idx
	}

	children := map[int][]int{}
	for _, idx := range indices {
		parentID := 0
		if tmuxAttrs := ws.Surfaces[idx].Tmux; tmuxAttrs != nil {
			parentID = tmuxAttrs.SplitFrom
			if parentID != 0 {
				if _, ok := idxByID[parentID]; !ok {
					parentID = 0
				}
			}
		}
		children[parentID] = append(children[parentID], idx)
	}
	for parentID := range children {
		slices.SortFunc(children[parentID], func(a, b int) int {
			return ws.Surfaces[a].ID - ws.Surfaces[b].ID
		})
	}

	var ordered []int
	visited := make(map[int]bool, len(indices))
	var visit func(parentID int)
	visit = func(parentID int) {
		for _, idx := range children[parentID] {
			if visited[idx] {
				continue
			}
			visited[idx] = true
			ordered = append(ordered, idx)
			visit(ws.Surfaces[idx].ID)
		}
	}
	visit(0)

	if len(ordered) == len(indices) {
		return ordered
	}

	fallback := append([]int(nil), indices...)
	slices.SortFunc(fallback, func(a, b int) int {
		return ws.Surfaces[a].ID - ws.Surfaces[b].ID
	})
	for _, idx := range fallback {
		if !visited[idx] {
			ordered = append(ordered, idx)
		}
	}
	return ordered
}
