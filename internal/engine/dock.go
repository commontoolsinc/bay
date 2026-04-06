package engine

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// DockInfo holds summary information about a dock.
type DockInfo struct {
	Name       string          `json:"name"`
	Agent      string          `json:"agent,omitempty"`
	Repo       string          `json:"repo,omitempty"`
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
	Status       string        `json:"status"`
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
	if _, exists := e.Config.Docks[name]; exists {
		return fmt.Errorf("dock %q already exists", name)
	}

	if repo != "" {
		if _, ok := e.Config.Repos[repo]; !ok {
			return fmt.Errorf("unknown repo %q", repo)
		}
	}
	if agent != "" {
		if _, ok := e.Config.Agents[agent]; !ok {
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

	e.Config.Docks[name] = config.DockConfig{
		Repo:     repo,
		Agent:    agent,
		Terminal: terminal,
	}
	if e.configPath != "" {
		if err := config.Save(e.configPath, e.Config); err != nil {
			return fmt.Errorf("saving config: %w", err)
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
		if m.FindDock(name) == nil {
			return m.AddDock(manifest.Dock{Name: name, Host: host})
		}
		return nil
	})
}

// launchTerminal launches a terminal app attached to a tmux session.
// Returns the PID of the launched process.
func launchTerminal(terminal, session string) (int, error) {
	cmd := exec.Command(terminal, "-e", "tmux", "attach", "-t", session)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	return cmd.Process.Pid, nil
}

// DockCloseWorkspaces closes all workspaces in a dock.
func (e *Engine) DockCloseWorkspaces(name string, force bool) {
	m, err := e.LoadManifest()
	if err != nil {
		return
	}
	dock := m.FindDock(name)
	if dock == nil {
		return
	}
	// Collect names first to avoid modifying slice during iteration.
	var names []string
	for _, ws := range dock.Workspaces {
		names = append(names, ws.Name)
	}
	for _, wsName := range names {
		_ = e.WsClose(name, wsName, force)
	}
}

// DockClose closes all workspaces in a dock and kills the tmux session.
func (e *Engine) DockClose(name string, force bool) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}

	dock := m.FindDock(name)
	if dock == nil {
		return fmt.Errorf("unknown dock %q", name)
	}

	var names []string
	for _, ws := range dock.Workspaces {
		names = append(names, ws.Name)
	}

	for _, wsName := range names {
		if err := e.WsClose(name, wsName, force); err != nil {
			if !force {
				return fmt.Errorf("workspace %q: %w", wsName, err)
			}
		}
	}

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
	for cfgName, dockCfg := range e.Config.Docks {
		info := DockInfo{
			Name:  cfgName,
			Agent: dockCfg.Agent,
			Repo:  dockCfg.Repo,
		}

		dock := m.FindDock(cfgName)
		if dock != nil {
			for i := range dock.Workspaces {
				ws := &dock.Workspaces[i]
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
					Status:       string(ws.Status),
					Missing:      ws.Path != "" && statErr != nil,
					DefaultAgent: dockCfg.Agent,
					SyncStatus:   "ok",
					SurfaceCount: len(ws.Surfaces),
				}

				if wsInfo.Missing {
					wsInfo.SyncStatus = "missing"
				}

				for _, s := range ws.Surfaces {
					sInfo := SurfaceInfo{
						ID:      s.ID,
						Name:    s.Name,
						Type:    string(s.Type),
						Backend: string(s.Backend),
						Status:  "ok",
					}
					if s.Agent != nil {
						sInfo.Agent = *s.Agent
					}
					if s.Command != nil {
						sInfo.Command = *s.Command
					}

					// Check liveness for tmux surfaces.
					if s.Tmux != nil && s.Tmux.WindowID != "" {
						exists, _ := e.Tmux.WindowExists(s.Tmux.WindowID)
						if !exists {
							sInfo.Status = "stale"
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
				if wsInfo.SyncStatus == "ok" && wsInfo.Stale {
					wsInfo.SyncStatus = "stale"
				}
				info.Workspaces = append(info.Workspaces, wsInfo)
			}
		}
		docks = append(docks, info)
	}
	return docks, nil
}

// WorkspaceInfo returns the runtime view for a single workspace.
func (e *Engine) WorkspaceInfoByName(dockName, wsName string) (*WorkspaceInfo, error) {
	docks, err := e.List()
	if err != nil {
		return nil, err
	}
	for _, dock := range docks {
		if dock.Name != dockName {
			continue
		}
		for i := range dock.Workspaces {
			if dock.Workspaces[i].Name == wsName {
				return &dock.Workspaces[i], nil
			}
		}
		return nil, fmt.Errorf("workspace %q not found in dock %q", wsName, dockName)
	}
	return nil, fmt.Errorf("unknown dock %q", dockName)
}
