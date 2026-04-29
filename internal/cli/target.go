package cli

import (
	"fmt"
	"strings"

	"github.com/commontoolsinc/bay/internal/engine"
)

// parseWsArg parses a bay positional argument like "w1" or "labs:w1".
// dock may be empty if no prefix was given.
func parseWsArg(arg string) (dock, ws string, err error) {
	parts := strings.Split(arg, ":")
	switch len(parts) {
	case 1:
		return "", parts[0], nil
	case 2:
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("invalid bay argument %q (expected id or dock:id)", arg)
	}
}

// parseSurfaceArg parses a surface positional argument. Accepted forms:
//
//	name
//	bay:name
//	dock:bay:name
//
// dock and ws may be empty.
func parseSurfaceArg(arg string) (dock, ws, surface string, err error) {
	parts := strings.Split(arg, ":")
	switch len(parts) {
	case 1:
		return "", "", parts[0], nil
	case 2:
		return "", parts[0], parts[1], nil
	case 3:
		return parts[0], parts[1], parts[2], nil
	default:
		return "", "", "", fmt.Errorf("invalid surface argument %q (expected name, bay:name, or dock:bay:name)", arg)
	}
}

// resolveWsArg resolves a bay positional + --dock flag into
// (dockName, wsName). The positional may be "self", a bare bay ID,
// or "dock:id". --dock disambiguates a bare ID and may not conflict
// with a dock prefix in the positional.
//
// For a bare ID, the current dock (resolved from cwd/tmux) wins if it
// has a bay with that ID. This matches user intent: typing
// `bay close w2` from inside loom means loom:w2, not "error because
// crew also has w2". Falls through to a full-manifest search when the
// current dock doesn't have a match, which may then error with
// "ambiguous" as before.
func resolveWsArg(eng *engine.Engine, posArg, dockFlag string) (string, string, error) {
	if posArg == "self" {
		if dockFlag != "" {
			return "", "", fmt.Errorf("--dock cannot be combined with self")
		}
		return eng.ResolveSelf()
	}

	dock, ws, err := parseWsArg(posArg)
	if err != nil {
		return "", "", err
	}

	if dockFlag != "" {
		if dock != "" {
			return "", "", fmt.Errorf("--dock conflicts with dock prefix in argument")
		}
		dock = dockFlag
	}

	if dock != "" {
		return eng.ResolveWorkspace(dock + ":" + ws)
	}
	return resolveBareWs(eng, ws)
}

// resolveBareWs resolves a bare bay ID, preferring the current dock
// to disambiguate. If the current dock (from cwd/tmux) has a bay
// with this ID, that wins; otherwise falls through to a full-manifest
// search, which may error with "ambiguous" if the ID
// appears in multiple docks and none is the current one.
func resolveBareWs(eng *engine.Engine, ws string) (string, string, error) {
	if ctx, err := eng.CurrentContext(); err == nil && ctx.Dock != "" {
		if dn, wn, resolveErr := eng.ResolveWorkspace(ctx.Dock + ":" + ws); resolveErr == nil {
			return dn, wn, nil
		}
	}
	return eng.ResolveWorkspace(ws)
}

// resolveSurfaceArgOrSelf is like resolveSurfaceArg but treats a bare "self"
// positional (no flags, no colons) specially: it resolves to the current
// pane's surface, unless a literal surface named "self" exists in the
// resolved bay — in which case the literal wins. Qualified forms
// like "w1:self" or "labs:w1:self" are always literal — no virtual fallback.
func resolveSurfaceArgOrSelf(eng *engine.Engine, posArg, wsFlag, dockFlag string) (string, string, string, error) {
	if posArg != "self" || wsFlag != "" || dockFlag != "" {
		return resolveSurfaceArg(eng, posArg, wsFlag, dockFlag)
	}

	dockName, wsName, err := eng.ResolveSelf()
	if err != nil {
		return "", "", "", err
	}
	ws, err := eng.WsShow(dockName, wsName)
	if err != nil {
		return "", "", "", err
	}
	// Literal surface named "self" wins if it exists.
	if ws.FindSurface("self") != nil {
		return dockName, wsName, "self", nil
	}
	// Otherwise resolve to the surface owning the current tmux pane.
	paneID, tmuxErr := eng.Tmux.CurrentPaneID()
	if tmuxErr != nil {
		return "", "", "", fmt.Errorf("cannot determine current pane")
	}
	for _, s := range ws.Surfaces {
		if s.Tmux != nil && s.Tmux.PaneID == paneID {
			return dockName, wsName, s.Name, nil
		}
	}
	// Check dock-level surfaces (e.g., dock editor).
	m, loadErr := eng.LoadManifest()
	if loadErr == nil {
		if dock := m.FindDock(dockName); dock != nil {
			for _, s := range dock.Surfaces {
				if s.Tmux != nil && s.Tmux.PaneID == paneID {
					// Return a special marker so callers know this is a dock surface.
					return dockName, "", s.Name, nil
				}
			}
		}
	}
	return "", "", "", fmt.Errorf("current pane is not a tracked surface")
}

// isDockSurface returns true if the positional is "dock:surfacename",
// meaning a dock-level surface in the current dock.
func isDockSurface(posArg string) (string, bool) {
	parts := strings.SplitN(posArg, ":", 2)
	if len(parts) == 2 && parts[0] == "dock" {
		return parts[1], true
	}
	return "", false
}

// resolveSurfaceArg resolves a surface positional + --bay/--dock flags into
// (dockName, wsName, surfaceName). The positional may be "name", "bay:name",
// "dock:bay:name", or the special form "dock:name" for dock-level surfaces.
// Flags may not conflict with corresponding parts in the positional.
// If neither positional nor flags name a bay, the current bay (ResolveSelf)
// is used.
func resolveSurfaceArg(eng *engine.Engine, posArg, wsFlag, dockFlag string) (string, string, string, error) {
	dock, ws, surface, err := parseSurfaceArg(posArg)
	if err != nil {
		return "", "", "", err
	}

	if wsFlag != "" {
		if ws != "" {
			return "", "", "", fmt.Errorf("--bay conflicts with bay prefix in argument")
		}
		ws = wsFlag
	}
	if dockFlag != "" {
		if dock != "" {
			return "", "", "", fmt.Errorf("--dock conflicts with dock prefix in argument")
		}
		dock = dockFlag
	}
	if dock != "" && ws == "" {
		return "", "", "", fmt.Errorf("--dock requires --bay or a bay prefix")
	}

	var dockName, wsName string
	switch {
	case dock != "" && ws != "":
		dockName, wsName, err = eng.ResolveWorkspace(dock + ":" + ws)
	case ws != "":
		dockName, wsName, err = resolveBareWs(eng, ws)
	default:
		dockName, wsName, err = eng.ResolveSelf()
	}
	if err != nil {
		return "", "", "", err
	}
	return dockName, wsName, surface, nil
}
