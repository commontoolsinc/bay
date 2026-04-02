package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
)

// Context describes the current Bay location resolved from cwd and tmux.
type Context struct {
	Repo        string `json:"repo,omitempty"`
	Dock        string `json:"dock,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	Window      string `json:"window,omitempty"`
	WindowID    int    `json:"window_id,omitempty"`
	PaneID      int    `json:"pane_id,omitempty"`
	Path        string `json:"path,omitempty"`
}

// CurrentContext resolves the current Bay context from cwd and tmux state.
func (e *Engine) CurrentContext() (*Context, error) {
	e.SyncAll()

	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	ctx := &Context{}
	cwd, _ := os.Getwd()
	if cwd != "" {
		ctx.Path = cwd
		if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = resolved
			ctx.Path = resolved
		}
	}

	currentSession, _ := e.Tmux.CurrentSession()
	currentWindowID, _ := e.Tmux.CurrentWindowID()
	currentPaneID, _ := e.Tmux.CurrentPaneID()

	for dockName, dockState := range m.Docks {
		for wsID, ws := range dockState.Workspaces {
			wsPath := config.ExpandPath(ws.Path)
			if resolved, err := filepath.EvalSymlinks(wsPath); err == nil {
				wsPath = resolved
			}
			if cwd != "" && (cwd == wsPath || strings.HasPrefix(cwd, wsPath+"/")) {
				ctx.Dock = dockName
				ctx.WorkspaceID = wsID
				ctx.Workspace = ws.Name
				ctx.Repo = ws.Repo
				if ctx.Repo == "" {
					ctx.Repo = e.Config.Docks[dockName].Repo
				}
				for _, win := range ws.Windows {
					if win.TmuxWindowID == currentWindowID {
						ctx.Window = win.Name
						ctx.WindowID = win.ID
						for _, pane := range win.Panes {
							if pane.TmuxPaneID == currentPaneID {
								ctx.PaneID = pane.ID
								break
							}
						}
						break
					}
				}
				return ctx, nil
			}
		}
	}

	if currentWindowID != "" {
		for dockName, dockState := range m.Docks {
			for wsID, ws := range dockState.Workspaces {
				for _, win := range ws.Windows {
					if win.TmuxWindowID != currentWindowID {
						continue
					}
					ctx.Dock = dockName
					ctx.WorkspaceID = wsID
					ctx.Workspace = ws.Name
					ctx.Window = win.Name
					ctx.WindowID = win.ID
					ctx.Repo = ws.Repo
					if ctx.Repo == "" {
						ctx.Repo = e.Config.Docks[dockName].Repo
					}
					ctx.Path = config.ExpandPath(ws.Path)
					for _, pane := range win.Panes {
						if pane.TmuxPaneID == currentPaneID {
							ctx.PaneID = pane.ID
							break
						}
					}
					return ctx, nil
				}
			}
		}
	}

	if currentSession != "" {
		if dockCfg, ok := e.Config.Docks[currentSession]; ok {
			ctx.Dock = currentSession
			ctx.Repo = dockCfg.Repo
			return ctx, nil
		}
	}

	for repoName, repoCfg := range e.Config.Repos {
		repoPath := config.ExpandPath(repoCfg.Path)
		if resolved, err := filepath.EvalSymlinks(repoPath); err == nil {
			repoPath = resolved
		}
		if cwd != "" && (cwd == repoPath || strings.HasPrefix(cwd, repoPath+"/")) {
			ctx.Repo = repoName
			return ctx, nil
		}
	}

	if ctx.Repo != "" || ctx.Dock != "" {
		return ctx, nil
	}
	return ctx, fmt.Errorf("not in a bay context")
}
