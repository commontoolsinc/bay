package engine

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// RepoInfo holds summary information about a repo.
type RepoInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	WorktreeDir string `json:"worktree_dir"`
}

// DockInfo holds summary information about a dock.
type DockInfo struct {
	Name       string          `json:"name"`
	Agent      string          `json:"agent,omitempty"`
	Repo       string          `json:"repo,omitempty"`
	Surfaces   []SurfaceInfo   `json:"surfaces,omitempty"` // dock-level surfaces
	Workspaces []WorkspaceInfo `json:"workspaces"`
}

// SurfaceInfo holds runtime information about a tracked surface.
type SurfaceInfo struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Backend string `json:"backend"`
	Agent   string `json:"agent,omitempty"`
	Command string `json:"command,omitempty"`
	Status  string `json:"status"`
}

// WorkspaceInfo holds summary information about a workspace.
type WorkspaceInfo struct {
	Name         string        `json:"name"`
	Type         string        `json:"type"`
	Path         string        `json:"path,omitempty"`
	Branch       string        `json:"branch,omitempty"`
	PR           string        `json:"pr,omitempty"`
	Dirty        bool          `json:"dirty"`
	Pending      bool          `json:"pending"`
	Waiting      bool          `json:"waiting,omitempty"`
	Missing      bool          `json:"missing,omitempty"`
	Stale        bool          `json:"stale,omitempty"`
	DefaultAgent string        `json:"default_agent,omitempty"`
	SyncStatus   string        `json:"sync_status"`
	SurfaceCount int           `json:"surface_count"`
	Surfaces     []SurfaceInfo `json:"surfaces,omitempty"`
}

// DockNew creates a new dock configuration and tmux session.
func (e *Engine) DockNew(name, repo, agent, terminal string) error {
	if err := ValidateName(name); err != nil {
		return err
	}

	if agent != "" {
		if _, ok := e.Config.ResolveAgent(agent); !ok {
			return fmt.Errorf("unknown agent %q", agent)
		}
	}

	exists, err := e.Tmux.HasSession(name)
	if err != nil {
		return fmt.Errorf("checking tmux session: %w", err)
	}
	if exists {
		return fmt.Errorf("tmux session %q already exists and is not a bay dock", name)
	}

	if err := e.ensureSession(name); err != nil {
		return err
	}

	// Save terminal override to config if set.
	if terminal != "" {
		e.Config.Docks[name] = config.DockConfig{Terminal: terminal}
		if e.configPath != "" {
			if err := config.Save(e.configPath, e.Config); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
		}
	}

	// Record host terminal if configured.
	var host *manifest.GUIAttrs
	if terminal != "" {
		host = &manifest.GUIAttrs{AppCommand: terminal}
		// Attempt to launch the terminal. Best-effort — don't fail dock creation.
		if pid, err := launchTerminal(terminal, name); err == nil {
			host.PID = pid
		}
	}

	return e.withManifest(func(m *manifest.Manifest) error {
		// Validate repo reference against manifest repos.
		if repo != "" {
			if m.FindRepo(repo) == nil {
				return fmt.Errorf("unknown repo %q", repo)
			}
		}
		if m.FindDock(name) != nil {
			return fmt.Errorf("dock %q already exists", name)
		}
		return m.AddDock(manifest.Dock{
			Name:  name,
			Repo:  repo,
			Agent: agent,
			Host:  host,
		})
	})
}

// launchTerminal launches a terminal app attached to a tmux session.
// Returns the PID of the launched process.
var launchTerminal = func(terminal, session string) (int, error) {
	cmd := exec.Command(terminal, "-e", "tmux", "attach", "-t", session)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// DockRename renames a dock in manifest, config overrides, and tmux.
func (e *Engine) DockRename(oldName, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}

	// Rename tmux session.
	exists, _ := e.Tmux.HasSession(oldName)
	if exists {
		if err := e.Tmux.RenameSession(oldName, newName); err != nil {
			return fmt.Errorf("renaming tmux session: %w", err)
		}
	}

	// Rename config overrides if any.
	if dockCfg, ok := e.Config.Docks[oldName]; ok {
		e.Config.Docks[newName] = dockCfg
		delete(e.Config.Docks, oldName)
		if e.configPath != "" {
			if err := config.Save(e.configPath, e.Config); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
		}
	}

	// Rename in manifest.
	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(oldName)
		if dock == nil {
			return fmt.Errorf("dock %q not found", oldName)
		}
		if m.FindDock(newName) != nil {
			return fmt.Errorf("dock %q already exists", newName)
		}
		dock.Name = newName
		return nil
	})
}

// DockClose closes all workspaces in a dock and kills the tmux session.
//
// All manifest mutations (per workspace + the dock itself) happen before
// any tmux kill, so the user-visible state is correct even if bay is
// invoked from inside a pane in this dock and dies during KillSession.
func (e *Engine) DockClose(name string, force bool) error {
	m, _ := e.LoadManifest()
	var dock *manifest.Dock
	if m != nil {
		dock = m.FindDock(name)
	}
	if dock == nil {
		return fmt.Errorf("unknown dock %q", name)
	}

	// Collect workspace names first to avoid modifying the slice during
	// iteration.
	var wsNames []string
	for _, ws := range dock.Workspaces {
		wsNames = append(wsNames, ws.Name)
	}

	// Manifest pass: archive + remove each workspace. On the success
	// path we discard the returned window IDs because KillSession at
	// the end takes out every pane in the session in one shot. On a
	// non-force failure mid-loop we use them to kill just the windows
	// for workspaces that *were* removed from the manifest, so we don't
	// leave orphaned tmux windows with no manifest reference.
	var killedWindowIDs []string
	for _, wsName := range wsNames {
		ids, err := e.closeWorkspaceState(name, wsName, force)
		if err != nil {
			if !force {
				// Sync tmux state with the manifest mutations we already
				// did before bailing out — otherwise the workspaces we
				// closed earlier in the loop would have orphan windows.
				for _, id := range killedWindowIDs {
					e.ensurePlaceholderIfLastWindow(name, id)
					_ = e.Tmux.KillWindow(id)
				}
				return fmt.Errorf("workspace %q: %w", wsName, err)
			}
		}
		killedWindowIDs = append(killedWindowIDs, ids...)
	}

	// Remove dock from manifest.
	if err := e.withManifest(func(m *manifest.Manifest) error {
		return m.RemoveDock(name)
	}); err != nil {
		return fmt.Errorf("removing dock from manifest: %w", err)
	}

	// Remove config overrides.
	delete(e.Config.Docks, name)
	if e.configPath != "" {
		if err := config.Save(e.configPath, e.Config); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}

	// All manifest state is persisted. Now kill the tmux session.
	// KillSession errors are non-fatal — the session may already be dead.
	_ = e.Tmux.KillSession(name)
	return nil
}

// List returns all docks and workspaces with runtime status.
func (e *Engine) List() ([]DockInfo, error) {
	e.SyncAll()

	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	var docks []DockInfo
	for i := range m.Docks {
		dock := &m.Docks[i]
		agent := e.resolvedDockAgent(dock.Name, m)
		info := DockInfo{
			Name:  dock.Name,
			Agent: agent,
			Repo:  dock.Repo,
		}

		for _, s := range dock.Surfaces {
			sInfo := SurfaceInfo{
				ID:      s.ID,
				Name:    s.Name,
				Type:    string(s.Type),
				Backend: string(s.Backend),
				Status:  manifest.SyncStatusOK,
			}
			if s.Command != nil {
				sInfo.Command = *s.Command
			}
			info.Surfaces = append(info.Surfaces, sInfo)
		}
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			info.Workspaces = append(info.Workspaces, e.buildWorkspaceInfo(ws, agent))
		}
		docks = append(docks, info)
	}
	return docks, nil
}

// buildWorkspaceInfo constructs a WorkspaceInfo from a manifest workspace,
// checking path existence and tmux liveness for each surface.
func (e *Engine) buildWorkspaceInfo(ws *manifest.Workspace, agent string) WorkspaceInfo {
	wsPath := config.ExpandPath(ws.Path)
	_, statErr := os.Stat(wsPath)

	branch := ""
	pr := ""
	if ws.Worktree != nil {
		branch = ws.Worktree.Branch
		pr = ws.Worktree.PR
	}

	wsInfo := WorkspaceInfo{
		Name:         ws.Name,
		Type:         string(ws.Type),
		Path:         ws.Path,
		Branch:       branch,
		PR:           pr,
		Pending:      ws.Worktree != nil && ws.Worktree.Branch != "" && !ws.IsMerged(),
		Missing:      ws.Path != "" && statErr != nil,
		DefaultAgent: agent,
		SyncStatus:   manifest.SyncStatusOK,
		SurfaceCount: len(ws.Surfaces),
	}

	if wsInfo.Missing {
		wsInfo.SyncStatus = manifest.SyncStatusMissing
	}

	// Compute dirty state from git.
	if ws.Path != "" && statErr == nil {
		if dirty, err := e.Git.IsDirty(wsPath); err == nil {
			wsInfo.Dirty = dirty
		}
	}

	for _, s := range ws.Surfaces {
		sInfo := SurfaceInfo{
			ID:      s.ID,
			Name:    s.Name,
			Type:    string(s.Type),
			Backend: string(s.Backend),
			Status:  manifest.SyncStatusOK,
		}
		if s.Agent != nil {
			sInfo.Agent = *s.Agent
		}
		if s.Command != nil {
			sInfo.Command = *s.Command
		}

		if s.Tmux != nil && s.Tmux.WindowID != "" {
			exists, _ := e.Tmux.WindowExists(s.Tmux.WindowID)
			if !exists {
				sInfo.Status = manifest.SyncStatusStale
				wsInfo.Stale = true
			} else {
				val, err := e.Tmux.GetWindowOption(s.Tmux.WindowID, "@bay-waiting")
				if err == nil && val == "1" {
					wsInfo.Waiting = true
				}
			}
		}

		wsInfo.Surfaces = append(wsInfo.Surfaces, sInfo)
	}

	if wsInfo.Missing {
		wsInfo.Stale = false
	}
	if wsInfo.SyncStatus == manifest.SyncStatusOK && wsInfo.Stale {
		wsInfo.SyncStatus = manifest.SyncStatusStale
	}
	return wsInfo
}

// WorkspaceInfoByName returns the runtime view for a single workspace.
// This does NOT call List() or SyncAll — it loads the manifest and
// builds info for just the requested workspace. Callers that display
// data should call SyncAll first.
func (e *Engine) WorkspaceInfoByName(dockName, wsName string) (*WorkspaceInfo, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q", dockName)
	}
	ws := dock.FindWorkspace(wsName)
	if ws == nil {
		return nil, fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
	}
	agent := e.resolvedDockAgent(dockName, m)
	info := e.buildWorkspaceInfo(ws, agent)
	return &info, nil
}
