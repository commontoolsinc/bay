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
	Repo      string `json:"repo,omitempty"`
	Dock      string `json:"dock,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Surface   string `json:"surface,omitempty"`
	SurfaceID int    `json:"surface_id,omitempty"`
	Path      string `json:"path,omitempty"`
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

	// Try matching CWD against workspace paths.
	for i := range m.Docks {
		dock := &m.Docks[i]
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			wsPath := config.ExpandPath(ws.Path)
			if resolved, err := filepath.EvalSymlinks(wsPath); err == nil {
				wsPath = resolved
			}
			if cwd != "" && (cwd == wsPath || strings.HasPrefix(cwd, wsPath+"/")) {
				ctx.Dock = dock.Name
				ctx.Workspace = ws.Name
				if ws.Worktree != nil {
					ctx.Repo = ws.Worktree.Repo
				}
				if ctx.Repo == "" {
					ctx.Repo = e.Config.Docks[dock.Name].Repo
				}
				// Find current surface from tmux pane.
				for _, s := range ws.Surfaces {
					if s.Tmux != nil && s.Tmux.PaneID == currentPaneID {
						ctx.Surface = s.Name
						ctx.SurfaceID = s.ID
						break
					}
				}
				return ctx, nil
			}
		}
	}

	// Fallback: match current tmux window/pane against surfaces.
	if currentWindowID != "" {
		for i := range m.Docks {
			dock := &m.Docks[i]
			for j := range dock.Workspaces {
				ws := &dock.Workspaces[j]
				for _, s := range ws.Surfaces {
					if s.Tmux == nil {
						continue
					}
					if s.Tmux.WindowID != currentWindowID {
						continue
					}
					ctx.Dock = dock.Name
					ctx.Workspace = ws.Name
					ctx.Path = config.ExpandPath(ws.Path)
					if ws.Worktree != nil {
						ctx.Repo = ws.Worktree.Repo
					}
					if ctx.Repo == "" {
						ctx.Repo = e.Config.Docks[dock.Name].Repo
					}
					if s.Tmux.PaneID == currentPaneID {
						ctx.Surface = s.Name
						ctx.SurfaceID = s.ID
					}
					return ctx, nil
				}
			}
		}
	}

	// Fallback: match tmux session to a dock.
	if currentSession != "" {
		if dockCfg, ok := e.Config.Docks[currentSession]; ok {
			ctx.Dock = currentSession
			ctx.Repo = dockCfg.Repo
			return ctx, nil
		}
	}

	// Fallback: match CWD to a repo.
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
