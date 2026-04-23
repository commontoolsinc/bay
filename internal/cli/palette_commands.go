package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/palette"
	"github.com/commontoolsinc/bay/internal/picker"
)

// paletteEnv collects the long-lived state the palette's 24 commands all
// need. buildPaletteEntries threads it through every Action closure.
type paletteEnv struct {
	Engine  *engine.Engine
	Scope   palette.Scope
	Ctx     *engine.Context
	Recents *palette.Recents
	Hotkeys *palette.Hotkeys
	In      *os.File
	Out     *os.File
}

// modeSplit converts a palette Mode to the split-direction string used by
// engine.SurfaceAddOptions (and the surface creator runners).
func modeSplit(m palette.Mode) string {
	if m == palette.ModePane {
		return "v"
	}
	return ""
}

// modeHotkey looks up the hotkey for a mode-sensitive signature.
func modeHotkey(h *palette.Hotkeys, window, pane string, m palette.Mode) string {
	if m == palette.ModePane {
		return h.Lookup(pane)
	}
	return h.Lookup(window)
}

// buildPaletteEntries returns all 24 palette entries with Actions bound to
// the current env and mode. Called at startup and again on every Tab press.
func buildPaletteEntries(env *paletteEnv, mode palette.Mode) []palette.Entry {
	h := env.Hotkeys
	split := modeSplit(mode)

	return []palette.Entry{
		// === Navigation ===
		{
			ID:      "go-surface",
			Title:   "Go to surface...",
			Section: palette.SectionNavigation,
			Needs:   palette.ScopeInWorkspace,
			Hotkey:  h.Lookup("bay go --pick"),
			Action: func() (string, error) {
				return "", surfaceGo(env.Engine, nil, false)
			},
		},
		{
			ID:      "go-workspace",
			Title:   "Go to workspace...",
			Section: palette.SectionNavigation,
			Needs:   palette.ScopeInDock,
			Hotkey:  h.Lookup("bay ws go --pick"),
			Action: func() (string, error) {
				return "", wsGo(env.Engine, nil, false, false)
			},
		},
		{
			ID:      "show-context",
			Title:   "Show current context",
			Section: palette.SectionNavigation,
			Needs:   palette.ScopeAnywhere,
			Action: func() (string, error) {
				ctx, err := env.Engine.CurrentContext()
				if err != nil {
					return "", err
				}
				description := ""
				if ctx.Dock != "" && ctx.Workspace != "" {
					if ws, wsErr := env.Engine.WsShow(ctx.Dock, ctx.Workspace); wsErr == nil {
						description = ws.Description
					}
				}
				palette.Notice(env.In, env.Out, formatPWD(ctx, description, false))
				return "", nil
			},
		},

		// === Create — surface ===
		{
			ID:      "new-shell",
			Title:   "New shell",
			Section: palette.SectionCreateSurface,
			Needs:   palette.ScopeInWorkspace,
			Hotkey:  modeHotkey(h, "bay shell --window", "bay shell --pane", mode),
			Action: func() (string, error) {
				dock, ws, err := env.Engine.ResolveSelf()
				if err != nil {
					return "", err
				}
				return "", env.Engine.SurfaceAdd(engine.SurfaceAddOptions{
					DockName: dock,
					WsName:   ws,
					Type:     manifest.SurfaceTypeShell,
					Name:     "shell",
					SplitDir: split,
				})
			},
		},
		{
			ID:      "new-agent",
			Title:   "New agent",
			Section: palette.SectionCreateSurface,
			Needs:   palette.ScopeInWorkspace,
			Hotkey:  modeHotkey(h, "bay agent --window", "bay agent --pane", mode),
			Action: func() (string, error) {
				dock, ws, err := env.Engine.ResolveSelf()
				if err != nil {
					return "", err
				}
				return "", runSurfaceNew(env.Engine, dock, ws, surfaceNewOpts{
					Type:     manifest.SurfaceTypeAgent,
					SplitDir: split,
				})
			},
		},
		{
			ID:      "new-agent-pick",
			Title:   "New agent...",
			Section: palette.SectionCreateSurface,
			Needs:   palette.ScopeInWorkspace,
			TitleWithParam: func(p string) string {
				return fmt.Sprintf("New agent (%s)", p)
			},
			Action: func() (string, error) {
				agent, ok := paletteAgentPick(env)
				if !ok {
					return "", nil
				}
				dock, ws, err := env.Engine.ResolveSelf()
				if err != nil {
					return "", err
				}
				if err := runSurfaceNew(env.Engine, dock, ws, surfaceNewOpts{
					Type:     manifest.SurfaceTypeAgent,
					Agent:    agent,
					SplitDir: split,
				}); err != nil {
					return "", err
				}
				env.Recents.RecordAgentType(agent)
				_ = env.Recents.Save()
				return agent, nil
			},
		},
		{
			ID:      "new-cmd",
			Title:   "New cmd...",
			Section: palette.SectionCreateSurface,
			Needs:   palette.ScopeInWorkspace,
			Action: func() (string, error) {
				cmdLine, ok, err := picker.Prompt("new cmd: ", "", env.In, env.Out)
				if err != nil || !ok || strings.TrimSpace(cmdLine) == "" {
					return "", err
				}
				dock, ws, err := env.Engine.ResolveSelf()
				if err != nil {
					return "", err
				}
				return "", runSurfaceNew(env.Engine, dock, ws, surfaceNewOpts{
					Type:     manifest.SurfaceTypeCmd,
					Command:  cmdLine,
					SplitDir: split,
				})
			},
		},
		{
			ID:      "edit-workspace",
			Title:   "Open editor (workspace)",
			Section: palette.SectionCreateSurface,
			Needs:   palette.ScopeInWorkspace,
			Hotkey:  h.Lookup("bay edit --ws"),
			Action: func() (string, error) {
				return "", runEditCreate(env.Engine, "self", "", split)
			},
		},
		{
			ID:      "edit-dock",
			Title:   "Open editor (dock)",
			Section: palette.SectionCreateSurface,
			Needs:   palette.ScopeInDock,
			Hotkey:  h.Lookup("bay edit --dock"),
			Action: func() (string, error) {
				return "", runEditDock(env.Engine, "", "")
			},
		},

		// === Create — workspace ===
		{
			ID:      "new-workspace",
			Title:   "New workspace",
			Section: palette.SectionCreateWorkspace,
			Needs:   palette.ScopeInDock,
			Hotkey:  h.Lookup("bay ws new -q"),
			Action: func() (string, error) {
				_, err := env.Engine.WsNew(engine.WsNewOptions{
					Dock:  env.Ctx.Dock,
					Shell: true,
				})
				return "", err
			},
		},
		{
			ID:      "new-workspace-agent",
			Title:   "New workspace with default agent",
			Section: palette.SectionCreateWorkspace,
			Needs:   palette.ScopeInDock,
			Hotkey:  h.Lookup("bay ws new -q --agent"),
			Action: func() (string, error) {
				_, err := env.Engine.WsNew(engine.WsNewOptions{
					Dock:         env.Ctx.Dock,
					RequireAgent: true,
				})
				return "", err
			},
		},
		{
			ID:      "new-workspace-agent-pick",
			Title:   "New workspace with agent...",
			Section: palette.SectionCreateWorkspace,
			Needs:   palette.ScopeInDock,
			TitleWithParam: func(p string) string {
				return fmt.Sprintf("New workspace with agent (%s)", p)
			},
			Action: func() (string, error) {
				agent, ok := paletteAgentPick(env)
				if !ok {
					return "", nil
				}
				if _, err := env.Engine.WsNew(engine.WsNewOptions{
					Dock:         env.Ctx.Dock,
					Agent:        agent,
					RequireAgent: true,
				}); err != nil {
					return "", err
				}
				env.Recents.RecordAgentType(agent)
				_ = env.Recents.Save()
				return agent, nil
			},
		},

		// === Current workspace ===
		{
			ID:      "rename-workspace",
			Title:   "Rename workspace...",
			Section: palette.SectionCurrentWorkspace,
			Needs:   palette.ScopeInWorkspace,
			Action: func() (string, error) {
				current := env.Ctx.Workspace
				name, ok, err := picker.Prompt("rename workspace: ", current, env.In, env.Out)
				if err != nil || !ok || name == "" || name == current {
					return "", err
				}
				return "", env.Engine.WsRename(env.Ctx.Dock, current, name)
			},
		},
		{
			ID:      "describe-workspace",
			Title:   "Describe workspace...",
			Section: palette.SectionCurrentWorkspace,
			Needs:   palette.ScopeInWorkspace,
			Action: func() (string, error) {
				prefill := ""
				if info, err := env.Engine.WorkspaceInfoByName(env.Ctx.Dock, env.Ctx.Workspace); err == nil {
					prefill = info.Description
				}
				desc, ok, err := picker.Prompt("describe: ", prefill, env.In, env.Out)
				if err != nil || !ok {
					return "", err
				}
				return "", env.Engine.WsDescribe(env.Ctx.Dock, env.Ctx.Workspace, desc)
			},
		},
		{
			ID:      "clear-workspace-description",
			Title:   "Clear workspace description",
			Section: palette.SectionCurrentWorkspace,
			Needs:   palette.ScopeInWorkspace,
			Action: func() (string, error) {
				return "", env.Engine.WsDescribe(env.Ctx.Dock, env.Ctx.Workspace, "")
			},
		},
		{
			ID:      "show-workspace",
			Title:   "Show workspace details",
			Section: palette.SectionCurrentWorkspace,
			Needs:   palette.ScopeInWorkspace,
			Action: func() (string, error) {
				env.Engine.SyncAll()
				info, err := env.Engine.WorkspaceInfoByName(env.Ctx.Dock, env.Ctx.Workspace)
				if err != nil {
					return "", err
				}
				text := FormatWorkspaceShow(env.Ctx.Repo, env.Ctx.Dock, info, false)
				palette.Notice(env.In, env.Out, text)
				return "", nil
			},
		},
		{
			ID:      "close-workspace",
			Title:   "Close workspace",
			Section: palette.SectionCurrentWorkspace,
			Needs:   palette.ScopeInWorkspace,
			Action: func() (string, error) {
				return "", env.Engine.WsClose(env.Ctx.Dock, env.Ctx.Workspace, false)
			},
		},

		// === Current surface ===
		{
			ID:      "rename-surface",
			Title:   "Rename surface...",
			Section: palette.SectionCurrentSurface,
			Needs:   palette.ScopeInWorkspace,
			Action: func() (string, error) {
				current := env.Ctx.Surface
				if current == "" {
					return "", fmt.Errorf("no current surface")
				}
				name, ok, err := picker.Prompt("rename surface: ", current, env.In, env.Out)
				if err != nil || !ok || name == "" || name == current {
					return "", err
				}
				return "", env.Engine.SurfaceRename(env.Ctx.Dock, env.Ctx.Workspace, current, name)
			},
		},
		{
			ID:      "close-surface",
			Title:   "Close surface",
			Section: palette.SectionCurrentSurface,
			Needs:   palette.ScopeInWorkspace,
			Hotkey:  h.Lookup("bay sf close self"),
			Action: func() (string, error) {
				if env.Ctx.Surface == "" {
					return "", fmt.Errorf("no current surface")
				}
				return "", env.Engine.SurfaceClose(env.Ctx.Dock, env.Ctx.Workspace, env.Ctx.Surface, false)
			},
		},
		{
			ID:      "show-surface",
			Title:   "Show surface details",
			Section: palette.SectionCurrentSurface,
			Needs:   palette.ScopeInWorkspace,
			Action: func() (string, error) {
				if env.Ctx.Surface == "" {
					return "", fmt.Errorf("no current surface")
				}
				env.Engine.SyncAll()
				ws, err := env.Engine.WsShow(env.Ctx.Dock, env.Ctx.Workspace)
				if err != nil {
					return "", err
				}
				s := ws.FindSurface(env.Ctx.Surface)
				if s == nil {
					return "", fmt.Errorf("surface %q not found", env.Ctx.Surface)
				}
				palette.Notice(env.In, env.Out, formatSurfaceShow(s))
				return "", nil
			},
		},

		// === Admin ===
		{
			ID:      "edit-config",
			Title:   "Edit config",
			Section: palette.SectionAdmin,
			Needs:   palette.ScopeAnywhere,
			Action: func() (string, error) {
				return "", runConfigEdit(env.Engine)
			},
		},
		{
			ID:      "show-config",
			Title:   "Show config",
			Section: palette.SectionAdmin,
			Needs:   palette.ScopeAnywhere,
			Action: func() (string, error) {
				text, err := configShowString(env.Engine.Config)
				if err != nil {
					return "", err
				}
				palette.Notice(env.In, env.Out, text)
				return "", nil
			},
		},
		{
			ID:      "run-doctor",
			Title:   "Run doctor",
			Section: palette.SectionAdmin,
			Needs:   palette.ScopeAnywhere,
			Action: func() (string, error) {
				var sb strings.Builder
				runDoctor(env.Engine, &sb)
				palette.Notice(env.In, env.Out, sb.String())
				return "", nil
			},
		},
		{
			ID:      "monitor-status",
			Title:   "Monitor status",
			Section: palette.SectionAdmin,
			Needs:   palette.ScopeAnywhere,
			Action: func() (string, error) {
				palette.Notice(env.In, env.Out, monitorStatusString())
				return "", nil
			},
		},
	}
}

// paletteAgentPick shows a sub-picker over the available agent types,
// anchored by the recents MRU. Returns the chosen value, or ("", false)
// on cancel.
func paletteAgentPick(env *paletteEnv) (string, bool) {
	available := availableAgents(env.Engine.Config)
	availSet := make(map[string]bool, len(available))
	for _, name := range available {
		availSet[name] = true
	}
	mru := env.Recents.TopAgentTypes(availSet)

	items, labels := buildAgentPickerItems(mru, available)

	sel, err := (&picker.Builtin{}).Pick(items, picker.Options{Prompt: "agent type> "})
	if err != nil || sel < 0 {
		return "", false
	}
	if sel >= len(labels) {
		return "", false
	}
	chosen := labels[sel]
	if strings.HasPrefix(chosen, "─") {
		return "", false
	}
	return chosen, true
}

// buildAgentPickerItems builds the sub-picker's item list. MRU entries
// go first, followed by a separator and the full alphabetical list. The
// full list always contains every available agent — including those
// also in MRU — so "pick an agent" feels like a stable directory and
// the MRU is strictly a shortcut.
//
// Returns items paired with a parallel labels slice keyed by Item.Value,
// so callers can look up the chosen string without a second pass.
func buildAgentPickerItems(mru, available []string) ([]picker.Item, []string) {
	var items []picker.Item
	for _, name := range mru {
		items = append(items, picker.Item{Display: name, Value: len(items)})
	}
	if len(items) > 0 {
		items = append(items, picker.Item{Display: strings.Repeat("─", 6), Value: -1})
	}
	sortedAll := make([]string, len(available))
	copy(sortedAll, available)
	sort.Strings(sortedAll)
	for _, name := range sortedAll {
		items = append(items, picker.Item{Display: name, Value: len(items)})
	}
	labels := make([]string, 0, len(items))
	for _, it := range items {
		labels = append(labels, it.Display)
	}
	return items, labels
}

// availableAgents returns the sorted list of known + configured agent names.
func availableAgents(cfg *config.Config) []string {
	seen := map[string]bool{}
	for name := range config.KnownAgents {
		seen[name] = true
	}
	for name := range cfg.Agents {
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// formatSurfaceShow produces a multi-line human-readable summary of s,
// matching the fields that `bay surface show` prints. Mirrors the
// label/value layout in runSurfaceShow by reusing writeAlignedRows.
func formatSurfaceShow(s *manifest.Surface) string {
	rows := []showRow{
		{"surface", s.Name},
		{"type", string(s.Type)},
	}
	if s.Tmux != nil {
		if s.Tmux.WindowID != "" {
			rows = append(rows, showRow{"window", s.Tmux.WindowID})
		}
		if s.Tmux.PaneID != "" {
			rows = append(rows, showRow{"pane", s.Tmux.PaneID})
		}
	}
	if s.Agent != nil && *s.Agent != "" {
		rows = append(rows, showRow{"agent", *s.Agent})
	}
	if s.Command != nil && *s.Command != "" {
		rows = append(rows, showRow{"command", *s.Command})
	}
	var b strings.Builder
	writeAlignedRows(&b, rows, 0)
	return b.String()
}
