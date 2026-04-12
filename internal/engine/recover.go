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
	Warnings  []string
	AttachCmd string
}

type recoverOutcome struct {
	recovered []string
	errs      []string
	warnings  []string
	changed   bool
}

// Recover reconstructs all tmux state from the manifest after reboot.
func (e *Engine) Recover() ([]RecoverResult, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	results := make([]RecoverResult, 0, len(m.Docks))
	var errs []string
	for i := range m.Docks {
		dock := &m.Docks[i]
		agentArgs := e.resolvedDockAgentArgs(dock.Name, m)

		if err := e.ensureSession(dock.Name); err != nil {
			return nil, err
		}

		outcome := e.recoverDockWorkspaces(dock, agentArgs)
		e.cleanPlaceholders(dock.Name)
		e.recoverDockHostTerminal(dock, &outcome)

		if outcome.changed {
			if err := e.mergeRecoveredDockState(dock); err != nil {
				return nil, err
			}
		}

		results = append(results, RecoverResult{
			Dock:      dock.Name,
			Recovered: outcome.recovered,
			Warnings:  outcome.warnings,
			AttachCmd: fmt.Sprintf("tmux attach -t %s", dock.Name),
		})
		errs = append(errs, outcome.errs...)
	}

	if len(errs) > 0 {
		return results, fmt.Errorf("recovery completed with errors:\n%s", strings.Join(errs, "\n"))
	}
	return results, nil
}

// DockRecover recovers a single dock by name.
func (e *Engine) DockRecover(name string) (RecoverResult, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return RecoverResult{}, err
	}
	dock := m.FindDock(name)
	if dock == nil {
		return RecoverResult{}, fmt.Errorf("unknown dock %q in manifest", name)
	}
	agentArgs := e.resolvedDockAgentArgs(name, m)

	if err := e.ensureSession(name); err != nil {
		return RecoverResult{}, err
	}

	outcome := e.recoverDockWorkspaces(dock, agentArgs)
	e.recoverDockSurfaces(dock, &outcome)
	e.cleanPlaceholders(name)
	e.recoverDockHostTerminal(dock, &outcome)

	if outcome.changed {
		if err := e.mergeRecoveredDockState(dock); err != nil {
			return RecoverResult{}, err
		}
	}

	result := RecoverResult{
		Dock:      name,
		Recovered: outcome.recovered,
		Warnings:  outcome.warnings,
		AttachCmd: fmt.Sprintf("tmux attach -t %s", name),
	}
	if len(outcome.errs) > 0 {
		return result, fmt.Errorf("recovery completed with errors:\n%s", strings.Join(outcome.errs, "\n"))
	}
	return result, nil
}

// recoverDockSurfaces recreates dock-level surfaces (e.g., dock editor).
func (e *Engine) recoverDockSurfaces(dock *manifest.Dock, outcome *recoverOutcome) {
	for i := range dock.Surfaces {
		s := &dock.Surfaces[i]
		if s.Tmux == nil {
			continue
		}
		// Check if the window still exists.
		if s.Tmux.WindowID != "" {
			exists, _ := e.Tmux.WindowExists(s.Tmux.WindowID)
			if exists {
				continue // already alive
			}
		}

		// Determine CWD — use the repo's worktree dir if available.
		cwd := "/tmp"
		if dock.Repo != "" {
			m, _ := e.LoadManifest()
			if m != nil {
				if parentDir, err := e.EditAllParentDir(dock.Name); err == nil {
					cwd = parentDir
				}
			}
		}

		// Recreate the window.
		winID, err := e.Tmux.NewWindow(dock.Name, s.Name, cwd)
		if err != nil {
			outcome.errs = append(outcome.errs, fmt.Sprintf("dock surface %s: %v", s.Name, err))
			continue
		}
		_ = e.Tmux.SetWindowOption(winID, "@bay-dock-editor", "1")
		_ = e.Tmux.MoveWindow(winID, 0)

		panes, _ := e.Tmux.ListPanes(winID)
		if len(panes) > 0 {
			s.Tmux.PaneID = panes[0].ID
			if s.Command != nil && *s.Command != "" {
				_ = e.Tmux.RespawnPane(panes[0].ID, cwd, *s.Command)
			}
		}
		s.Tmux.WindowID = winID
		outcome.recovered = append(outcome.recovered, fmt.Sprintf("dock surface: %s", s.Name))
		outcome.changed = true
	}
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
						dir := s.Tmux.SplitDir
						if dir == "" {
							dir = "v"
						}
						splitTargetID, err := recoverSplitTargetID(ws, s, newWindowID)
						if err != nil {
							outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s surface %s: %v", ws.Name, s.Name, err))
							continue
						}
						newPaneID, err := e.Tmux.SplitWindow(splitTargetID, dir, ws.Path)
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
		dir := s.Tmux.SplitDir
		if dir == "" {
			dir = "v"
		}
		splitTargetID, err := recoverSplitTargetID(ws, s, tmuxWindowID)
		if err != nil {
			outcome.errs = append(outcome.errs, fmt.Sprintf("workspace %s surface %s: %v", ws.Name, s.Name, err))
			continue
		}
		newPaneID, err := e.Tmux.SplitWindow(splitTargetID, dir, ws.Path)
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

func (e *Engine) recoverDockHostTerminal(dock *manifest.Dock, outcome *recoverOutcome) {
	terminal := e.Config.ResolvedDockTerminal(dock.Name)
	if dock.Host == nil || terminal == "" {
		return
	}
	if dock.Host.PID != 0 && processAlive(dock.Host.PID) {
		return
	}

	pid, err := launchTerminal(terminal, dock.Name)
	if err != nil {
		outcome.warnings = append(outcome.warnings, fmt.Sprintf("dock %s: launch terminal: %v", dock.Name, err))
		return
	}
	if dock.Host.PID != pid {
		dock.Host.PID = pid
		outcome.changed = true
	}
}

func (e *Engine) mergeRecoveredDockState(recovered *manifest.Dock) error {
	return e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		dock := m.FindDock(recovered.Name)
		if dock == nil {
			return false, fmt.Errorf("dock %q disappeared during recovery", recovered.Name)
		}
		return mergeRecoveredDockRuntimeState(dock, recovered), nil
	})
}

func mergeRecoveredDockRuntimeState(dst, src *manifest.Dock) bool {
	changed := false

	if src.Host != nil {
		if dst.Host == nil {
			dst.Host = &manifest.GUIAttrs{}
			changed = true
		}
		if dst.Host.PID != src.Host.PID {
			dst.Host.PID = src.Host.PID
			changed = true
		}
	}

	for i := range src.Workspaces {
		srcWS := &src.Workspaces[i]
		dstWS := findWorkspaceForRecoveryMerge(dst, srcWS)
		if dstWS == nil {
			continue
		}
		for j := range srcWS.Surfaces {
			srcSurface := &srcWS.Surfaces[j]
			if srcSurface.Tmux == nil {
				continue
			}
			dstSurface := findSurfaceForRecoveryMerge(dstWS, srcSurface)
			if dstSurface == nil {
				continue
			}
			if dstSurface.Tmux == nil {
				dstSurface.Tmux = &manifest.TmuxAttrs{
					LayoutGroup: srcSurface.Tmux.LayoutGroup,
					SplitFrom:   srcSurface.Tmux.SplitFrom,
					SplitDir:    srcSurface.Tmux.SplitDir,
				}
				changed = true
			}
			if dstSurface.Tmux.WindowID != srcSurface.Tmux.WindowID {
				dstSurface.Tmux.WindowID = srcSurface.Tmux.WindowID
				changed = true
			}
			if dstSurface.Tmux.PaneID != srcSurface.Tmux.PaneID {
				dstSurface.Tmux.PaneID = srcSurface.Tmux.PaneID
				changed = true
			}
		}
	}

	return changed
}

func findWorkspaceForRecoveryMerge(dock *manifest.Dock, src *manifest.Workspace) *manifest.Workspace {
	for i := range dock.Workspaces {
		if dock.Workspaces[i].Path == src.Path {
			return &dock.Workspaces[i]
		}
	}
	if src.Name == "" {
		return nil
	}
	return dock.FindWorkspace(src.Name)
}

func findSurfaceForRecoveryMerge(ws *manifest.Workspace, src *manifest.Surface) *manifest.Surface {
	if src.ID != 0 {
		if surface := ws.FindSurfaceByID(src.ID); surface != nil {
			return surface
		}
	}
	if src.Name == "" {
		return nil
	}
	return ws.FindSurface(src.Name)
}

func recoverSplitTargetID(ws *manifest.Workspace, s *manifest.Surface, fallbackTargetID string) (string, error) {
	if s.Tmux == nil || s.Tmux.SplitFrom == 0 {
		return fallbackTargetID, nil
	}
	parent := ws.FindSurfaceByID(s.Tmux.SplitFrom)
	if parent == nil || parent.Tmux == nil || parent.Tmux.PaneID == "" {
		return "", fmt.Errorf("missing split parent %d", s.Tmux.SplitFrom)
	}
	return parent.Tmux.PaneID, nil
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
