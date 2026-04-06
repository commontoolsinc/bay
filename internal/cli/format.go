package cli

import (
	"fmt"
	"sort"
	"strings"

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
	Focus        ListFocus  `json:"focus"`
	Recursive    bool       `json:"recursive"`
	Repos        []RepoInfo `json:"repos"`
	CurrentDock    string `json:"-"` // for highlighting; not serialized
	CurrentWs      string `json:"-"`
	CurrentSurface string `json:"-"`
}

// SetCurrentContext populates the current dock/workspace/surface for highlighting.
func (v *ListView) SetCurrentContext(eng *engine.Engine) {
	if session, err := eng.Tmux.CurrentSession(); err == nil {
		v.CurrentDock = session
	}
	if dock, ws, err := eng.ResolveSelf(); err == nil {
		v.CurrentDock = dock
		v.CurrentWs = ws
	}
	// Resolve current surface from tmux pane.
	if ctx, err := eng.CurrentContext(); err == nil {
		v.CurrentSurface = ctx.Surface
	}
}

type ListRow struct {
	Repo                string `json:"repo"`
	Dock                string `json:"dock,omitempty"`
	WorkspaceName       string `json:"workspace_name,omitempty"`
	WorkspaceBranch     string `json:"workspace_branch,omitempty"`
	WorkspaceStatus     string `json:"workspace_status,omitempty"`
	WorkspaceSyncStatus string `json:"workspace_sync_status,omitempty"`
	WorkspaceWaiting    bool   `json:"workspace_waiting,omitempty"`
	SurfaceID           int    `json:"surface_id,omitempty"`
	SurfaceName         string `json:"surface_name,omitempty"`
	SurfaceType         string `json:"surface_type,omitempty"`
	SurfaceAgent        string `json:"surface_agent,omitempty"`
	SurfaceCommand      string `json:"surface_command,omitempty"`
	SurfaceStatus       string `json:"surface_status,omitempty"`
}

// BuildListView builds the repo -> dock -> workspace hierarchy from DockInfo data.
// Repos and dock-repo assignments come from the DockInfo.Repo field (set by engine.List).
func BuildListView(docks []engine.DockInfo, opts ListViewOptions) ListView {
	focus := opts.Focus
	if focus.Kind == "" {
		focus.Kind = FocusAll
	}
	recursive := opts.Recursive || focus.Kind == FocusWorkspace

	// Group docks by repo.
	dockMap := map[string]engine.DockInfo{}
	repoSet := map[string]bool{}
	dockToRepo := map[string]string{}
	for _, dock := range docks {
		dockMap[dock.Name] = dock
		repo := dock.Repo
		if repo == "" {
			repo = "(no repo)"
		}
		repoSet[repo] = true
		dockToRepo[dock.Name] = repo
	}

	repoNames := make([]string, 0, len(repoSet))
	for name := range repoSet {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	assignedDocks := map[string]bool{}
	view := ListView{Focus: focus, Recursive: recursive}

	for _, repoName := range repoNames {
		if repoName == "(no repo)" {
			continue // handled below
		}
		if (focus.Kind == FocusRepo || focus.Kind == FocusDock || focus.Kind == FocusWorkspace) && focus.Repo != "" && focus.Repo != repoName {
			continue
		}
		repoInfo := RepoInfo{Name: repoName}

		var dockNames []string
		for dockName, repo := range dockToRepo {
			if repo == repoName {
				dockNames = append(dockNames, dockName)
			}
		}
		sort.Strings(dockNames)

		for _, dockName := range dockNames {
			if (focus.Kind == FocusDock || focus.Kind == FocusWorkspace) && focus.Dock != "" && focus.Dock != dockName {
				continue
			}
			assignedDocks[dockName] = true
			dock := dockMap[dockName]
			filtered := dock
			filtered.Workspaces = nil

			for _, ws := range dock.Workspaces {
				if focus.Kind == FocusWorkspace && focus.WorkspaceID != "" && focus.WorkspaceID != ws.Name {
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

	// Orphan docks (no repo).
	orphanRepo := RepoInfo{Name: "(no repo)"}
	var orphanNames []string
	for dockName, repo := range dockToRepo {
		if assignedDocks[dockName] {
			continue
		}
		if focus.Kind == FocusRepo {
			continue
		}
		if repo != "(no repo)" {
			continue
		}
		orphanNames = append(orphanNames, dockName)
	}
	sort.Strings(orphanNames)
	for _, dockName := range orphanNames {
		if (focus.Kind == FocusDock || focus.Kind == FocusWorkspace) && focus.Dock != "" && focus.Dock != dockName {
			continue
		}
		dock := dockMap[dockName]
		filtered := dock
		filtered.Workspaces = nil
		for _, ws := range dock.Workspaces {
			if focus.Kind == FocusWorkspace && focus.WorkspaceID != "" && focus.WorkspaceID != ws.Name {
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
		return ws
	}
	ws.Surfaces = nil
	return ws
}

func ListRows(view ListView) []ListRow {
	var rows []ListRow
	for _, repo := range view.Repos {
		for _, dock := range repo.Docks {
			for _, ws := range dock.Workspaces {
				if len(ws.Surfaces) == 0 {
					rows = append(rows, ListRow{
						Repo:                repo.Name,
						Dock:                dock.Name,
						WorkspaceName:       ws.Name,
						WorkspaceBranch:     ws.Branch,
						WorkspaceStatus:     ws.Status,
						WorkspaceSyncStatus: ws.SyncStatus,
						WorkspaceWaiting:    ws.Waiting,
					})
					continue
				}
				for _, s := range ws.Surfaces {
					rows = append(rows, ListRow{
						Repo:                repo.Name,
						Dock:                dock.Name,
						WorkspaceName:       ws.Name,
						WorkspaceBranch:     ws.Branch,
						WorkspaceStatus:     ws.Status,
						WorkspaceSyncStatus: ws.SyncStatus,
						WorkspaceWaiting:    ws.Waiting,
						SurfaceID:           s.ID,
						SurfaceName:         s.Name,
						SurfaceType:         s.Type,
						SurfaceAgent:        s.Agent,
						SurfaceCommand:      s.Command,
						SurfaceStatus:       s.Status,
					})
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
	parts = appendMeta(parts, "branch", ws.Branch)
	if ws.Status != "" && ws.Status != "idle" {
		parts = appendMeta(parts, "status", ws.Status)
	}
	if showCounts {
		parts = append(parts, kv("surfaces", fmt.Sprintf("%d", ws.SurfaceCount)))
	}
	parts = append(parts, syncSuffix(ws.SyncStatus)...)
	if ws.Waiting {
		parts = append(parts, "\u23f3")
	}
	return strings.Join(parts, " ")
}

func surfaceMeta(s engine.SurfaceInfo, long bool) string {
	var parts []string
	parts = appendMeta(parts, "type", s.Type)
	if s.Agent != "" {
		parts = appendMeta(parts, "agent", s.Agent)
	}
	if s.Command != "" {
		parts = appendMeta(parts, "command", truncateCommand(s.Command))
	}
	parts = append(parts, syncSuffix(s.Status)...)
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
			dockMarker := ""
			if dock.Name == view.CurrentDock {
				dockMarker = " *"
			}
			writeIndentedLine(&b, 1, labelValue("dock", dock.Name+dockMarker), "")
			if len(dock.Workspaces) == 0 {
				writeIndentedLine(&b, 2, "(no workspaces)", "")
				continue
			}
			showChildren := view.Recursive || view.Focus.Kind == FocusWorkspace
			for _, ws := range dock.Workspaces {
				wsName := ws.Name
				if dock.Name == view.CurrentDock && ws.Name == view.CurrentWs {
					wsName += " *"
				}
				writeIndentedLine(&b, 2, labelValue("workspace", wsName), workspaceMeta(ws, !showChildren))
				if !showChildren {
					continue
				}
				for _, s := range ws.Surfaces {
					sName := s.Name
					if dock.Name == view.CurrentDock && ws.Name == view.CurrentWs && s.Name == view.CurrentSurface {
						sName += " *"
					}
					writeIndentedLine(&b, 3, labelValue("surface", sName), surfaceMeta(s, long))
				}
			}
		}
	}
	return b.String()
}

func FormatWorkspaceShow(repoName, dockName string, ws *engine.WorkspaceInfo, long bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", labelValue("workspace", ws.Name))
	fmt.Fprintf(&b, "%s\n", labelValue("repo", repoName))
	fmt.Fprintf(&b, "%s\n", labelValue("dock", dockName))
	fmt.Fprintf(&b, "%s\n", labelValue("type", ws.Type))
	fmt.Fprintf(&b, "%s\n", labelValue("path", ws.Path))
	if ws.Branch != "" {
		fmt.Fprintf(&b, "%s\n", labelValue("branch", ws.Branch))
	}
	if ws.PR != "" {
		fmt.Fprintf(&b, "%s\n", labelValue("pr", ws.PR))
	}
	fmt.Fprintf(&b, "%s\n", labelValue("status", ws.Status))
	if ws.DefaultAgent != "" {
		fmt.Fprintf(&b, "%s\n", labelValue("default agent", ws.DefaultAgent))
	}
	if ws.SyncStatus != "" && ws.SyncStatus != "ok" {
		fmt.Fprintf(&b, "%s\n", labelValue("sync", ws.SyncStatus))
	}
	for _, s := range ws.Surfaces {
		writeIndentedLine(&b, 0, labelValue("surface", s.Name), surfaceMeta(s, long))
	}
	return b.String()
}

// FormatFullTree formats the full repo -> dock -> workspace hierarchy.
func FormatFullTree(docks []engine.DockInfo) string {
	return FormatListView(BuildListView(docks, ListViewOptions{}), false)
}

// FormatDockTree formats dock -> workspace hierarchy for one or more docks.
func FormatDockTree(docks []engine.DockInfo) string {
	focus := ListFocus{Kind: FocusAll}
	if len(docks) == 1 {
		focus = ListFocus{Kind: FocusDock, Repo: docks[0].Repo, Dock: docks[0].Name}
	}
	return FormatListView(BuildListView(docks, ListViewOptions{Focus: focus}), false)
}

// FormatRepoTree formats repos with their docks (from manifest data).
func FormatRepoTree(repos []engine.RepoInfo, docks []engine.DockInfo) string {
	var b strings.Builder
	for _, repo := range repos {
		fmt.Fprintf(&b, "%s  %s  (worktrees: %s)\n", repo.Name, repo.Path, repo.WorktreeDir)
		var dockNames []string
		for _, dock := range docks {
			if dock.Repo == repo.Name {
				dockNames = append(dockNames, dock.Name)
			}
		}
		sort.Strings(dockNames)
		if len(dockNames) > 0 {
			fmt.Fprintf(&b, "  docks: %s\n", strings.Join(dockNames, ", "))
		}
	}
	return b.String()
}

// FormatSubtreeForRemoval formats the repo -> dock -> workspace hierarchy
// for items that would be removed. Used by repo remove's error message.
func FormatSubtreeForRemoval(repoName string, docks []engine.DockInfo) string {
	view := BuildListView(docks, ListViewOptions{
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
