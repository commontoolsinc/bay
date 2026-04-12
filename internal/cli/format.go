package cli

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
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
	Focus          ListFocus  `json:"focus"`
	Recursive      bool       `json:"recursive"`
	Repos          []RepoInfo `json:"repos"`
	CurrentDock    string     `json:"-"` // for highlighting; not serialized
	CurrentWs      string     `json:"-"`
	CurrentSurface string     `json:"-"`
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
	return "\x1b[38;5;243m" + s + "\x1b[0m"
}

func kv(key, value string) string {
	return dim(key+"=") + value
}

// metaCol is a single field in a metadata row (e.g., "branch=fix/login").
// width is the visible column count, excluding ANSI escape codes.
type metaCol struct {
	text  string
	width int
}

// kvCol builds a metaCol from a key=value pair. Returns a zero metaCol when
// value is empty.
func kvCol(key, value string) metaCol {
	if value == "" {
		return metaCol{}
	}
	return metaCol{
		text:  kv(key, value),
		width: utf8.RuneCountInString(key) + 1 + utf8.RuneCountInString(value),
	}
}

// bareCol builds a metaCol from a bare value (no key= prefix).
func bareCol(value string) metaCol {
	if value == "" {
		return metaCol{}
	}
	return metaCol{text: value, width: utf8.RuneCountInString(value)}
}

// metaField returns a kvCol or bareCol depending on whether short mode is on.
func metaField(key, value string, short bool) metaCol {
	if short {
		return bareCol(value)
	}
	return kvCol(key, value)
}

func labelValue(label, value string) string {
	return dim(label) + " " + value
}

// labelValueWithWidth returns the labelValue prefix and its visible column
// count. The width is computed directly from the inputs because labelValue's
// structure (dimmed label + space + value) is fixed and the dim() escape
// codes are zero-width — no ANSI stripping needed.
func labelValueWithWidth(label, value string) (string, int) {
	return labelValue(label, value), utf8.RuneCountInString(label) + 1 + utf8.RuneCountInString(value)
}

// indentedPrefix builds an indented prefix with a dimmed label and name.
// In short mode, the label is omitted entirely.
func indentedPrefix(level int, label, name string, short bool) (string, int) {
	indent := strings.Repeat("  ", level)
	if short {
		return indent + name, level*2 + utf8.RuneCountInString(name)
	}
	prefix := indent + dim(label) + " " + name
	width := level*2 + utf8.RuneCountInString(label) + 1 + utf8.RuneCountInString(name)
	return prefix, width
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

// workspaceMetaCols returns fixed-position columns for workspace metadata.
// Column positions: 0=branch, 1=status, 2=count, 3=sync/waiting.
func workspaceMetaCols(ws engine.WorkspaceInfo, showCounts, short bool) []metaCol {
	cols := make([]metaCol, 4)
	cols[0] = metaField("br", ws.Branch, short)
	if ws.Status != "" && ws.Status != string(manifest.WorkspaceStatusIdle) && ws.Status != string(manifest.WorkspaceStatusActive) {
		cols[1] = metaField("st", ws.Status, short)
	}
	if showCounts {
		cols[2] = metaField("n", fmt.Sprintf("%d", ws.SurfaceCount), short)
	}
	// Sync and waiting indicators share the trailing column.
	var parts []string
	var tailWidth int
	if sync := ws.SyncStatus; sync != "" && sync != manifest.SyncStatusOK {
		if short {
			parts = append(parts, sync)
			tailWidth = utf8.RuneCountInString(sync)
		} else {
			parts = append(parts, kv("sy", sync))
			tailWidth = 3 + utf8.RuneCountInString(sync)
		}
	}
	if ws.Waiting {
		parts = append(parts, "\u23f3")
		if tailWidth > 0 {
			tailWidth += 2 // space + emoji
		} else {
			tailWidth = 1
		}
	}
	if len(parts) > 0 {
		cols[3] = metaCol{text: strings.Join(parts, " "), width: tailWidth}
	}
	return cols
}

// surfaceMetaCols returns fixed-position columns for surface metadata.
// Column positions: 0=type, 1=agent, 2=command, 3=sync.
func surfaceMetaCols(s engine.SurfaceInfo, long, short bool) []metaCol {
	cols := make([]metaCol, 4)
	cols[0] = metaField("ty", s.Type, short)
	cols[1] = metaField("ag", s.Agent, short)
	cols[2] = metaField("cm", truncateCommand(s.Command), short)
	if s.Status != "" && s.Status != manifest.SyncStatusOK {
		cols[3] = metaField("sy", s.Status, short)
	}
	return cols
}

// writeAlignedLine writes "<prefix><pad><col0><pad><col1>...".
// prefixAlign controls where the first column starts (max prefixWidth across
// sibling rows). colWidths controls per-column alignment across sibling rows.
// Lines with all-empty meta columns omit padding entirely.
func writeAlignedLine(b *strings.Builder, prefix string, prefixWidth int, metaCols []metaCol, prefixAlign int, colWidths []int) {
	b.WriteString(prefix)

	// Find last column this row actually populates.
	lastPopulated := -1
	for i := len(metaCols) - 1; i >= 0; i-- {
		if metaCols[i].text != "" {
			lastPopulated = i
			break
		}
	}
	if lastPopulated < 0 {
		b.WriteString("\n")
		return
	}

	// Pad prefix to alignment point.
	if prefixAlign > 0 {
		b.WriteString(strings.Repeat(" ", prefixAlign-prefixWidth+2))
	} else {
		b.WriteString(" ")
	}

	// Write columns with inter-column padding.
	first := true
	for i := 0; i <= lastPopulated; i++ {
		cw := 0
		if i < len(colWidths) {
			cw = colWidths[i]
		}
		if cw == 0 {
			continue // column unused across all rows
		}
		if !first {
			b.WriteString(" ")
		}
		first = false

		col := metaCol{}
		if i < len(metaCols) {
			col = metaCols[i]
		}
		b.WriteString(col.text)

		// Pad to column width, except for the last populated column.
		if i < lastPopulated {
			b.WriteString(strings.Repeat(" ", cw-col.width))
		}
	}
	b.WriteString("\n")
}

// alignedRow holds a precomputed render row. The optional surfaceRows
// field is populated only on workspace rows in tree mode and carries the
// pre-built surface render rows for that workspace, so the render pass
// can iterate workspace rows without indexing back into dock.Workspaces.
type alignedRow struct {
	prefix      string
	prefixWidth int
	metaCols    []metaCol
	surfaceRows []alignedRow // workspace rows only, in tree mode
}

func hasMetaCols(cols []metaCol) bool {
	for _, c := range cols {
		if c.text != "" {
			return true
		}
	}
	return false
}

// alignWidth returns the max prefix width across rows that have non-empty
// meta. Rows without meta don't contribute, so they don't push the meta
// column out for their siblings.
func alignWidth(rows []alignedRow) int {
	max := 0
	for _, r := range rows {
		if !hasMetaCols(r.metaCols) {
			continue
		}
		if r.prefixWidth > max {
			max = r.prefixWidth
		}
	}
	return max
}

// alignColumnWidths returns the max visible width for each column position
// across all rows. Columns that no row populates have width 0.
func alignColumnWidths(rows []alignedRow) []int {
	var widths []int
	for _, r := range rows {
		for i, c := range r.metaCols {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			if c.width > widths[i] {
				widths[i] = c.width
			}
		}
	}
	return widths
}

func FormatListView(view ListView, long, short bool) string {
	var b strings.Builder
	for _, repo := range view.Repos {
		p, _ := indentedPrefix(0, "rp", repo.Name, short)
		fmt.Fprintln(&b, p)
		if len(repo.Docks) == 0 {
			p, _ := indentedPrefix(1, "", "(no docks)", short)
			fmt.Fprintln(&b, p)
			continue
		}
		for _, dock := range repo.Docks {
			dockMarker := ""
			if dock.Name == view.CurrentDock {
				dockMarker = " *"
			}
			p, _ := indentedPrefix(1, "dk", dock.Name+dockMarker, short)
			fmt.Fprintln(&b, p)
			if len(dock.Workspaces) == 0 {
				p, _ := indentedPrefix(2, "", "(no workspaces)", short)
				fmt.Fprintln(&b, p)
				continue
			}
			showChildren := view.Recursive || view.Focus.Kind == FocusWorkspace

			// First pass: build all workspace rows AND surface rows
			// up front. Both prefix alignment and per-column alignment
			// are computed across the whole dock so columns stay stable
			// as the eye scans down the output.
			wsRows := make([]alignedRow, len(dock.Workspaces))
			var allSfRows []alignedRow
			for i, ws := range dock.Workspaces {
				isCurrentWs := dock.Name == view.CurrentDock && ws.Name == view.CurrentWs
				wsName := ws.Name
				if isCurrentWs {
					wsName += " *"
				}
				p, w := indentedPrefix(2, "ws", wsName, short)
				wsRows[i] = alignedRow{
					prefix:      p,
					prefixWidth: w,
					metaCols:    workspaceMetaCols(ws, !showChildren, short),
				}
				if !showChildren {
					continue
				}
				sfRows := make([]alignedRow, len(ws.Surfaces))
				for j, s := range ws.Surfaces {
					sName := s.Name
					if isCurrentWs && s.Name == view.CurrentSurface {
						sName += " *"
					}
					sfP, sfW := indentedPrefix(3, "sf", sName, short)
					sfRows[j] = alignedRow{
						prefix:      sfP,
						prefixWidth: sfW,
						metaCols:    surfaceMetaCols(s, long, short),
					}
				}
				wsRows[i].surfaceRows = sfRows
				allSfRows = append(allSfRows, sfRows...)
			}
			wsAlign := alignWidth(wsRows)
			wsColWidths := alignColumnWidths(wsRows)
			sfAlign := alignWidth(allSfRows)
			sfColWidths := alignColumnWidths(allSfRows)

			for _, wsRow := range wsRows {
				writeAlignedLine(&b, wsRow.prefix, wsRow.prefixWidth, wsRow.metaCols, wsAlign, wsColWidths)
				for _, sr := range wsRow.surfaceRows {
					writeAlignedLine(&b, sr.prefix, sr.prefixWidth, sr.metaCols, sfAlign, sfColWidths)
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
	if ws.SyncStatus != "" && ws.SyncStatus != manifest.SyncStatusOK {
		fmt.Fprintf(&b, "%s\n", labelValue("sync", ws.SyncStatus))
	}
	sfRows := make([]alignedRow, len(ws.Surfaces))
	for i, s := range ws.Surfaces {
		prefix, width := labelValueWithWidth("surface", s.Name)
		sfRows[i] = alignedRow{
			prefix:      prefix,
			prefixWidth: width,
			metaCols:    surfaceMetaCols(s, long, false),
		}
	}
	sfAlign := alignWidth(sfRows)
	sfColWidths := alignColumnWidths(sfRows)
	for _, sr := range sfRows {
		writeAlignedLine(&b, sr.prefix, sr.prefixWidth, sr.metaCols, sfAlign, sfColWidths)
	}
	return b.String()
}

// FormatFullTree formats the full repo -> dock -> workspace hierarchy.
func FormatFullTree(docks []engine.DockInfo) string {
	return FormatListView(BuildListView(docks, ListViewOptions{}), false, false)
}

// FormatDockTree formats dock -> workspace hierarchy for one or more docks.
func FormatDockTree(docks []engine.DockInfo) string {
	focus := ListFocus{Kind: FocusAll}
	if len(docks) == 1 {
		focus = ListFocus{Kind: FocusDock, Repo: docks[0].Repo, Dock: docks[0].Name}
	}
	return FormatListView(BuildListView(docks, ListViewOptions{Focus: focus}), false, false)
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
	return indentBlock(FormatListView(view, false, false), 1)
}

func indentBlock(s string, depth int) string {
	prefix := strings.Repeat("  ", depth)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}
