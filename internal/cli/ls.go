package cli

import (
	"github.com/commontoolsinc/bay/internal/engine"
)

// filterDirtyWorkspaces filters dock info to only include workspaces with
// uncommitted changes. Relies on Dirty being populated by engine.List().
func filterDirtyWorkspaces(docks []engine.DockInfo) []engine.DockInfo {
	var result []engine.DockInfo
	for _, d := range docks {
		filtered := engine.DockInfo{
			Name:        d.Name,
			Agent:       d.Agent,
			Path:        d.Path,
			WorktreeDir: d.WorktreeDir,
		}
		for _, ws := range d.Workspaces {
			if ws.Dirty {
				filtered.Workspaces = append(filtered.Workspaces, ws)
			}
		}
		if len(filtered.Workspaces) > 0 {
			result = append(result, filtered)
		}
	}
	return result
}

func resolveListFocus(eng *engine.Engine, dirtyOnly bool) ListFocus {
	if dirtyOnly {
		return ListFocus{Kind: FocusAll}
	}

	return inferListFocus(eng)
}

func inferListFocus(eng *engine.Engine) ListFocus {
	ctx, err := eng.CurrentContext()
	if err != nil {
		return ListFocus{Kind: FocusAll}
	}
	switch {
	case ctx.WorkspaceID != "":
		return ListFocus{Kind: FocusWorkspace, Dock: ctx.Dock, WorkspaceID: ctx.WorkspaceID}
	case ctx.Dock != "":
		return ListFocus{Kind: FocusDock, Dock: ctx.Dock}
	default:
		return ListFocus{Kind: FocusAll}
	}
}
