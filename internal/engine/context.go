package engine

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/commontoolsinc/bay/internal/config"
)

// Context describes the current Bay location resolved from cwd and tmux.
type Context struct {
	Repo        string `json:"repo,omitempty"`
	Dock        string `json:"dock,omitempty"`
	WorkspaceID string `json:"-"`
	Workspace   string `json:"-"`
	Surface     string `json:"surface,omitempty"`
	SurfaceID   int    `json:"surface_id,omitempty"`
	Path        string `json:"path,omitempty"`
}

// MarshalJSON emits the user-facing bay vocabulary while preserving the
// internal field names used by the rest of the engine.
func (c Context) MarshalJSON() ([]byte, error) {
	type contextJSON struct {
		Repo      string `json:"repo,omitempty"`
		Dock      string `json:"dock,omitempty"`
		BayID     string `json:"bay_id,omitempty"`
		Bay       string `json:"bay,omitempty"`
		Surface   string `json:"surface,omitempty"`
		SurfaceID int    `json:"surface_id,omitempty"`
		Path      string `json:"path,omitempty"`
	}
	return json.Marshal(contextJSON{
		Repo:      c.Repo,
		Dock:      c.Dock,
		BayID:     c.WorkspaceID,
		Bay:       c.Workspace,
		Surface:   c.Surface,
		SurfaceID: c.SurfaceID,
		Path:      c.Path,
	})
}

// CurrentContext resolves the current Bay context from cwd and tmux state.
// This is a read-only query — it does NOT call SyncAll. The monitor
// handles background sync; callers that need the freshest state should
// call SyncAll explicitly before calling this.
func (e *Engine) CurrentContext() (*Context, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}

	ctx := &Context{}
	cwd, _ := os.Getwd()
	if cwd != "" {
		ctx.Path = config.CanonicalPath(cwd)
	}

	currentSession, _ := e.Tmux.CurrentSession()
	currentWindowID, _ := e.Tmux.CurrentWindowID()
	currentPaneID, _ := e.Tmux.CurrentPaneID()

	// Try matching CWD against workspace paths.
	for i := range m.Docks {
		dock := &m.Docks[i]
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			if cwd != "" && config.IsPathUnder(cwd, ws.Path) {
				ctx.Dock = dock.Name
				ctx.WorkspaceID = ws.ID
				ctx.Workspace = ws.Name
				if ws.Worktree != nil {
					ctx.Repo = ws.Worktree.Repo
				}
				if ctx.Repo == "" {
					ctx.Repo = dock.Repo
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
					ctx.WorkspaceID = ws.ID
					ctx.Workspace = ws.Name
					ctx.Path = config.CanonicalPath(ws.Path)
					if ws.Worktree != nil {
						ctx.Repo = ws.Worktree.Repo
					}
					if ctx.Repo == "" {
						ctx.Repo = dock.Repo
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
		dock := m.FindDock(currentSession)
		if dock != nil {
			ctx.Dock = currentSession
			ctx.Repo = dock.Repo
			return ctx, nil
		}
	}

	// Fallback: match CWD to a repo.
	for _, repo := range m.Repos {
		if cwd != "" && config.IsPathUnder(cwd, repo.Path) {
			ctx.Repo = repo.Name
			return ctx, nil
		}
	}

	if ctx.Repo != "" || ctx.Dock != "" {
		return ctx, nil
	}
	return ctx, fmt.Errorf("not in a bay context")
}
