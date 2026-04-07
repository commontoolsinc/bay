package cli

import (
	"fmt"
	"strings"

	"github.com/commontoolsinc/bay/internal/engine"
)

// parseWsArg parses a workspace positional argument like "w1" or "labs:w1".
// dock may be empty if no prefix was given.
func parseWsArg(arg string) (dock, ws string, err error) {
	parts := strings.Split(arg, ":")
	switch len(parts) {
	case 1:
		return "", parts[0], nil
	case 2:
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("invalid workspace argument %q (expected name or dock:name)", arg)
	}
}

// parseSurfaceArg parses a surface positional argument. Accepted forms:
//
//	name
//	ws:name
//	dock:ws:name
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
		return "", "", "", fmt.Errorf("invalid surface argument %q (expected name, ws:name, or dock:ws:name)", arg)
	}
}

// resolveWsArg resolves a workspace positional + --dock flag into
// (dockName, wsName). The positional may be "self", a bare workspace name,
// or "dock:name". --dock disambiguates a bare name and may not conflict
// with a dock prefix in the positional.
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
	return eng.ResolveWorkspace(ws)
}

// resolveSurfaceArg resolves a surface positional + --ws/--dock flags into
// (dockName, wsName, surfaceName). The positional may be "name", "ws:name",
// or "dock:ws:name". Flags may not conflict with corresponding parts in the
// positional. If neither positional nor flags name a workspace, the current
// workspace (ResolveSelf) is used.
func resolveSurfaceArg(eng *engine.Engine, posArg, wsFlag, dockFlag string) (string, string, string, error) {
	dock, ws, surface, err := parseSurfaceArg(posArg)
	if err != nil {
		return "", "", "", err
	}

	if wsFlag != "" {
		if ws != "" {
			return "", "", "", fmt.Errorf("--ws conflicts with workspace prefix in argument")
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
		return "", "", "", fmt.Errorf("--dock requires --ws or a workspace prefix")
	}

	var dockName, wsName string
	switch {
	case dock != "" && ws != "":
		dockName, wsName, err = eng.ResolveWorkspace(dock + ":" + ws)
	case ws != "":
		dockName, wsName, err = eng.ResolveWorkspace(ws)
	default:
		dockName, wsName, err = eng.ResolveSelf()
	}
	if err != nil {
		return "", "", "", err
	}
	return dockName, wsName, surface, nil
}
