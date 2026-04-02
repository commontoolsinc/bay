package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
)

type FocusKind string

const (
	FocusAll       FocusKind = "all"
	FocusRepo      FocusKind = "repo"
	FocusDock      FocusKind = "dock"
	FocusWorkspace FocusKind = "workspace"
)

type ListFocus struct {
	Kind        FocusKind `json:"kind"`
	Repo        string    `json:"repo,omitempty"`
	Dock        string    `json:"dock,omitempty"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
}

type ListViewOptions struct {
	Focus     ListFocus
	Recursive bool
}

type RepoInfo struct {
	Name  string            `json:"name"`
	Path  string            `json:"path,omitempty"`
	Docks []engine.DockInfo `json:"docks"`
}

type ListView struct {
	Focus     ListFocus  `json:"focus"`
	Recursive bool       `json:"recursive"`
	Repos     []RepoInfo `json:"repos"`
}

type ListRow struct {
	Repo                string `json:"repo"`
	Dock                string `json:"dock,omitempty"`
	WorkspaceID         string `json:"workspace_id,omitempty"`
	WorkspaceName       string `json:"workspace_name,omitempty"`
	WorkspaceBranch     string `json:"workspace_branch,omitempty"`
	WorkspaceStatus     string `json:"workspace_status,omitempty"`
	WorkspaceSyncStatus string `json:"workspace_sync_status,omitempty"`
	WorkspaceWaiting    bool   `json:"workspace_waiting,omitempty"`
	WindowID            int    `json:"window_id,omitempty"`
	WindowName          string `json:"window_name,omitempty"`
	WindowTmuxID        string `json:"window_tmux_id,omitempty"`
	WindowStatus        string `json:"window_status,omitempty"`
	PaneID              int    `json:"pane_id,omitempty"`
	PaneTmuxID          string `json:"pane_tmux_id,omitempty"`
	PaneType            string `json:"pane_type,omitempty"`
	PaneAgent           string `json:"pane_agent,omitempty"`
	PaneCommand         string `json:"pane_command,omitempty"`
	PaneStatus          string `json:"pane_status,omitempty"`
}

func BuildListView(cfg *config.Config, docks []engine.DockInfo, opts ListViewOptions) ListView {
	focus := opts.Focus
	if focus.Kind == "" {
		focus.Kind = FocusAll
	}
	recursive := opts.Recursive || focus.Kind == FocusWorkspace

	dockMap := map[string]engine.DockInfo{}
	for _, dock := range docks {
		dockMap[dock.Name] = dock
	}
	assignedDocks := map[string]bool{}

	repoNames := make([]string, 0, len(cfg.Repos))
	for name := range cfg.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	view := ListView{Focus: focus, Recursive: recursive}

	for _, repoName := range repoNames {
		if (focus.Kind == FocusRepo || focus.Kind == FocusDock || focus.Kind == FocusWorkspace) && focus.Repo != "" && focus.Repo != repoName {
			continue
		}
		repoCfg := cfg.Repos[repoName]
		repoInfo := RepoInfo{Name: repoName, Path: repoCfg.Path}

		dockNames := make([]string, 0, len(cfg.Docks))
		for dockName, dockCfg := range cfg.Docks {
			if dockCfg.Repo == repoName {
				dockNames = append(dockNames, dockName)
			}
		}
		sort.Strings(dockNames)

		for _, dockName := range dockNames {
			if (focus.Kind == FocusDock || focus.Kind == FocusWorkspace) && focus.Dock != "" && focus.Dock != dockName {
				continue
			}
			assignedDocks[dockName] = true
			dock, ok := dockMap[dockName]
			if !ok {
				dock = engine.DockInfo{Name: dockName, Repo: repoName}
			}
			filtered := dock
			filtered.Workspaces = nil

			for _, ws := range dock.Workspaces {
				if focus.Kind == FocusWorkspace && focus.WorkspaceID != "" && focus.WorkspaceID != ws.ID {
					continue
				}
				filtered.Workspaces = append(filtered.Workspaces, trimWorkspace(ws, recursive))
			}

			if focus.Kind == FocusWorkspace && len(filtered.Workspaces) == 0 {
				continue
			}
			repoInfo.Docks = append(repoInfo.Docks, filtered)
		}

		if focus.Kind == FocusRepo && len(repoInfo.Docks) == 0 {
			continue
		}
		if focus.Kind == FocusAll && len(repoInfo.Docks) == 0 {
			repoInfo.Docks = []engine.DockInfo{}
		}
		if len(repoInfo.Docks) > 0 || focus.Kind == FocusAll || focus.Kind == FocusRepo {
			view.Repos = append(view.Repos, repoInfo)
		}
	}

	orphanRepo := RepoInfo{Name: "(no repo)"}
	orphanNames := make([]string, 0)
	for dockName, dock := range cfg.Docks {
		if assignedDocks[dockName] {
			continue
		}
		if dock.Repo != "" {
			if _, ok := cfg.Repos[dock.Repo]; ok {
				continue
			}
		}
		orphanNames = append(orphanNames, dockName)
	}
	sort.Strings(orphanNames)
	for _, dockName := range orphanNames {
		if (focus.Kind == FocusDock || focus.Kind == FocusWorkspace) && focus.Dock != "" && focus.Dock != dockName {
			continue
		}
		dock, ok := dockMap[dockName]
		if !ok {
			dock = engine.DockInfo{Name: dockName}
		}
		filtered := dock
		filtered.Workspaces = nil
		for _, ws := range dock.Workspaces {
			if focus.Kind == FocusWorkspace && focus.WorkspaceID != "" && focus.WorkspaceID != ws.ID {
				continue
			}
			filtered.Workspaces = append(filtered.Workspaces, trimWorkspace(ws, recursive))
		}
		if focus.Kind == FocusWorkspace && len(filtered.Workspaces) == 0 {
			continue
		}
		orphanRepo.Docks = append(orphanRepo.Docks, filtered)
	}
	if len(orphanRepo.Docks) > 0 {
		view.Repos = append(view.Repos, orphanRepo)
	}

	return view
}

func trimWorkspace(ws engine.WorkspaceInfo, recursive bool) engine.WorkspaceInfo {
	if recursive {
		ws.WindowCount = len(ws.Windows)
		return ws
	}
	ws.WindowCount = len(ws.Windows)
	ws.Windows = nil
	return ws
}

func ListRows(view ListView) []ListRow {
	var rows []ListRow
	for _, repo := range view.Repos {
		for _, dock := range repo.Docks {
			for _, ws := range dock.Workspaces {
				if len(ws.Windows) == 0 {
					rows = append(rows, ListRow{
						Repo:                repo.Name,
						Dock:                dock.Name,
						WorkspaceID:         ws.ID,
						WorkspaceName:       ws.Name,
						WorkspaceBranch:     ws.Branch,
						WorkspaceStatus:     ws.Status,
						WorkspaceSyncStatus: ws.SyncStatus,
						WorkspaceWaiting:    ws.Waiting,
					})
					continue
				}
				for _, win := range ws.Windows {
					if len(win.Panes) == 0 {
						rows = append(rows, ListRow{
							Repo:                repo.Name,
							Dock:                dock.Name,
							WorkspaceID:         ws.ID,
							WorkspaceName:       ws.Name,
							WorkspaceBranch:     ws.Branch,
							WorkspaceStatus:     ws.Status,
							WorkspaceSyncStatus: ws.SyncStatus,
							WorkspaceWaiting:    ws.Waiting,
							WindowID:            win.ID,
							WindowName:          win.Name,
							WindowTmuxID:        win.TmuxWindowID,
							WindowStatus:        win.Status,
						})
						continue
					}
					for _, pane := range win.Panes {
						rows = append(rows, ListRow{
							Repo:                repo.Name,
							Dock:                dock.Name,
							WorkspaceID:         ws.ID,
							WorkspaceName:       ws.Name,
							WorkspaceBranch:     ws.Branch,
							WorkspaceStatus:     ws.Status,
							WorkspaceSyncStatus: ws.SyncStatus,
							WorkspaceWaiting:    ws.Waiting,
							WindowID:            win.ID,
							WindowName:          win.Name,
							WindowTmuxID:        win.TmuxWindowID,
							WindowStatus:        win.Status,
							PaneID:              pane.ID,
							PaneTmuxID:          pane.TmuxPaneID,
							PaneType:            pane.Type,
							PaneAgent:           pane.Agent,
							PaneCommand:         pane.Command,
							PaneStatus:          pane.Status,
						})
					}
				}
			}
		}
	}
	return rows
}

func dim(s string) string {
	return "\x1b[2m" + s + "\x1b[0m"
}

func kv(key, value string) string {
	return dim(key+"=") + value
}

func labelValue(label, value string) string {
	return dim(label) + " " + value
}

func dimmedSeparator(sep string) string {
	return dim(sep)
}

func truncateCommand(cmd string) string {
	if len(cmd) <= 32 {
		return cmd
	}
	return cmd[:29] + "..."
}

func appendMeta(parts []string, key, value string) []string {
	if value == "" {
		return parts
	}
	return append(parts, kv(key, value))
}

func syncSuffix(sync string) []string {
	if sync == "" || sync == "ok" {
		return nil
	}
	return []string{kv("sync", sync)}
}

func workspaceMeta(ws engine.WorkspaceInfo, showCounts bool) string {
	var parts []string
	parts = appendMeta(parts, "id", ws.ID)
	parts = appendMeta(parts, "branch", ws.Branch)
	if ws.Status != "" && ws.Status != "idle" {
		parts = appendMeta(parts, "status", ws.Status)
	}
	if showCounts {
		parts = append(parts, kv("windows", fmt.Sprintf("%d", ws.WindowCount)))
	}
	parts = append(parts, syncSuffix(ws.SyncStatus)...)
	if ws.Waiting {
		parts = append(parts, "⏳")
	}
	return strings.Join(parts, " ")
}

func windowMeta(win engine.WindowInfo, long bool) string {
	var parts []string
	parts = appendMeta(parts, "title", win.Name)
	if long {
		parts = appendMeta(parts, "tmux", win.TmuxWindowID)
	}
	parts = append(parts, syncSuffix(win.Status)...)
	return strings.Join(parts, " ")
}

func paneMeta(pane engine.PaneInfo, long bool) string {
	var parts []string
	parts = appendMeta(parts, "kind", pane.Type)
	if pane.Type == "agent" {
		parts = appendMeta(parts, "agent", pane.Agent)
	}
	if pane.Type == "cmd" {
		parts = appendMeta(parts, "command", truncateCommand(pane.Command))
	}
	if long {
		parts = appendMeta(parts, "tmux", pane.TmuxPaneID)
	}
	parts = append(parts, syncSuffix(pane.Status)...)
	return strings.Join(parts, " ")
}

func writeIndentedLine(b *strings.Builder, indent int, prefix string, meta string) {
	b.WriteString(strings.Repeat("  ", indent))
	b.WriteString(prefix)
	if meta != "" {
		b.WriteString(" ")
		b.WriteString(meta)
	}
	b.WriteString("\n")
}

func FormatListView(view ListView, long bool) string {
	var b strings.Builder
	for _, repo := range view.Repos {
		writeIndentedLine(&b, 0, labelValue("repo", repo.Name), "")
		if len(repo.Docks) == 0 {
			writeIndentedLine(&b, 1, "(no docks)", "")
			continue
		}
		for _, dock := range repo.Docks {
			writeIndentedLine(&b, 1, labelValue("dock", dock.Name), "")
			if len(dock.Workspaces) == 0 {
				writeIndentedLine(&b, 2, "(no workspaces)", "")
				continue
			}
			showChildren := view.Recursive || view.Focus.Kind == FocusWorkspace
			for _, ws := range dock.Workspaces {
				writeIndentedLine(&b, 2, labelValue("workspace", ws.Name), workspaceMeta(ws, !showChildren))
				if !showChildren {
					continue
				}
				for _, win := range ws.Windows {
					writeIndentedLine(&b, 3, labelValue("window", fmt.Sprintf("%d", win.ID)), windowMeta(win, long))
					for _, pane := range win.Panes {
						writeIndentedLine(&b, 4, labelValue("pane", fmt.Sprintf("%d", pane.ID)), paneMeta(pane, long))
					}
				}
			}
		}
	}
	return b.String()
}

func FormatWorkspaceShow(repoName, dockName string, ws *engine.WorkspaceInfo, long bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", labelValue("workspace", ws.Name), ws.ID)
	fmt.Fprintf(&b, "%s\n", labelValue("repo", repoName))
	fmt.Fprintf(&b, "%s\n", labelValue("dock", dockName))
	fmt.Fprintf(&b, "%s\n", labelValue("type", ws.Type))
	fmt.Fprintf(&b, "%s\n", labelValue("path", ws.Path))
	fmt.Fprintf(&b, "%s\n", labelValue("branch", ws.Branch))
	fmt.Fprintf(&b, "%s\n", labelValue("pr", ws.PR))
	fmt.Fprintf(&b, "%s\n", labelValue("status", ws.Status))
	if ws.DefaultAgent != "" {
		fmt.Fprintf(&b, "%s\n", labelValue("default agent", ws.DefaultAgent))
	}
	if ws.SyncStatus != "" && ws.SyncStatus != "ok" {
		fmt.Fprintf(&b, "%s\n", labelValue("sync", ws.SyncStatus))
	}
	for _, win := range ws.Windows {
		writeIndentedLine(&b, 0, labelValue("window", fmt.Sprintf("%d", win.ID)), windowMeta(win, long))
		for _, pane := range win.Panes {
			writeIndentedLine(&b, 1, labelValue("pane", fmt.Sprintf("%d", pane.ID)), paneMeta(pane, long))
		}
	}
	return b.String()
}

// FormatFullTree formats the full repo → dock → workspace hierarchy.
func FormatFullTree(cfg *config.Config, docks []engine.DockInfo) string {
	return FormatListView(BuildListView(cfg, docks, ListViewOptions{}), false)
}

// FormatDockTree formats dock → workspace hierarchy for one or more docks.
func FormatDockTree(docks []engine.DockInfo) string {
	cfg := &config.Config{
		Repos: map[string]config.RepoConfig{},
		Docks: map[string]config.DockConfig{},
	}
	for _, dock := range docks {
		cfg.Docks[dock.Name] = config.DockConfig{Repo: dock.Repo, Agent: dock.Agent}
		if dock.Repo != "" {
			cfg.Repos[dock.Repo] = config.RepoConfig{}
		}
	}
	focus := ListFocus{Kind: FocusAll}
	if len(docks) == 1 {
		focus = ListFocus{Kind: FocusDock, Repo: docks[0].Repo, Dock: docks[0].Name}
	}
	return FormatListView(BuildListView(cfg, docks, ListViewOptions{Focus: focus}), false)
}

// FormatRepoTree formats repo → docks summary.
func FormatRepoTree(cfg *config.Config) string {
	var b strings.Builder
	repoNames := make([]string, 0, len(cfg.Repos))
	for name := range cfg.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)
	for _, name := range repoNames {
		repo := cfg.Repos[name]
		wtDir := repo.EffectiveWorktreeDir()
		fmt.Fprintf(&b, "%s  %s  (worktrees: %s)\n", name, repo.Path, wtDir)
		var dockNames []string
		for dockName, dock := range cfg.Docks {
			if dock.Repo == name {
				dockNames = append(dockNames, dockName)
			}
		}
		sort.Strings(dockNames)
		if len(dockNames) > 0 {
			fmt.Fprintf(&b, "  docks: %s\n", strings.Join(dockNames, ", "))
		}
	}
	return b.String()
}

// FormatSubtreeForRemoval formats the repo → dock → workspace hierarchy
// for items that would be removed. Used by repo remove's error message.
func FormatSubtreeForRemoval(cfg *config.Config, repoName string, docks []engine.DockInfo) string {
	view := BuildListView(cfg, docks, ListViewOptions{
		Focus: ListFocus{Kind: FocusRepo, Repo: repoName},
	})
	return indentBlock(FormatListView(view, false), 1)
}

func indentBlock(s string, depth int) string {
	prefix := strings.Repeat("  ", depth)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}
