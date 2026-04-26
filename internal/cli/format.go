package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"golang.org/x/term"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI escape sequences from s. Used when we need to
// hand bay's colored output to a consumer that renders plain text only
// (e.g. tmux display-message, which does not interpret ANSI).
func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

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
	WorkspaceDirty      bool   `json:"workspace_dirty,omitempty"`
	WorkspacePending    bool   `json:"workspace_pending,omitempty"`
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

// printListViewJSON marshals a ListView (or its denormalized rows) to stdout.
func printListViewJSON(view ListView, rows bool) error {
	var payload interface{} = view
	if rows {
		payload = ListRows(view)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
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
						WorkspaceDirty:      ws.Dirty,
						WorkspacePending:    ws.Pending,
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
						WorkspaceDirty:      ws.Dirty,
						WorkspacePending:    ws.Pending,
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
// Column positions: 0=description, 1=branch, 2=dir (only when different from
// name), 3=status, 4=count, 5=sync/waiting.
//
// Descriptions come through at full stored length; FormatListView shrinks
// them in place (index 0) if the computed terminal budget is tighter. Only
// the first line of the description is shown — bodies are opt-in and appear
// in the M-? popup, not on the list row.
func workspaceMetaCols(ws engine.WorkspaceInfo, showCounts, short bool) []metaCol {
	cols := make([]metaCol, 6)
	if first := engine.DescriptionFirstLine(ws.Description); first != "" {
		cols[0] = descField(first, engine.MaxDescriptionFirstLineLen, short)
	}
	cols[1] = metaField("br", ws.Branch, short)
	// Show directory basename only when it differs from the workspace name.
	if ws.Path != "" {
		dir := filepath.Base(ws.Path)
		if dir != ws.Name {
			cols[2] = metaField("dir", dir, short)
		}
	}
	if ws.Dirty {
		cols[3] = metaField("st", "dirty", short)
	} else if ws.Pending {
		cols[3] = metaField("st", "pending", short)
	}
	if showCounts {
		cols[4] = metaField("n", fmt.Sprintf("%d", ws.SurfaceCount), short)
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
		cols[5] = metaCol{text: strings.Join(parts, " "), width: tailWidth}
	}
	return cols
}

// listDescMinStrLen floors the description string length so even a narrow
// terminal shows a useful 15-char hint instead of near-nothing.
const listDescMinStrLen = 15

// listDescPipedStrMax is the cap applied when stdout isn't a terminal
// (pipe, redirect, CI). Pipes have no natural width; downstream consumers
// should see the complete first-line value (bodies are opt-in popup-only).
const listDescPipedStrMax = engine.MaxDescriptionFirstLineLen

// descColLabel is the key-prefix for a description column in long format.
const descColLabel = "ds="

// descColOverhead returns the non-string characters in a rendered
// description column: two quotes, plus the "ds=" prefix in long format.
// Used by the width budget to map a column budget back to a string budget.
func descColOverhead(short bool) int {
	if short {
		return 2
	}
	return 2 + len(descColLabel)
}

// descField renders a description as ds="..." (or a bare quoted string in
// short mode), truncating the string to strMax characters and quoting so
// embedded spaces don't blur the column boundary.
func descField(desc string, strMax int, short bool) metaCol {
	quoted := `"` + engine.TruncateName(desc, strMax) + `"`
	w := utf8.RuneCountInString(quoted)
	if short {
		return metaCol{text: quoted, width: w}
	}
	return metaCol{text: dim(descColLabel) + quoted, width: len(descColLabel) + w}
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

// detectStdoutWidth returns the current terminal width of stdout. Returns 0
// when stdout is not a terminal (pipe, redirect, CI) so callers can switch to
// pipe-friendly behavior (no width-aware truncation).
//
// `COLUMNS` wins when it's set to a positive integer — standard Unix override
// that also works through pipes (e.g. `COLUMNS=80 bay tree | less -S`).
func detectStdoutWidth() int {
	if c := os.Getenv("COLUMNS"); c != "" {
		if w, err := strconv.Atoi(c); err == nil && w > 0 {
			return w
		}
	}
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return 0
	}
	w, _, err := term.GetSize(fd)
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

// descStrMaxForWidth returns the max description string length that fits
// alongside the rest of a workspace row at the given terminal width.
// termWidth == 0 means "not a terminal" (pipe/redirect) and returns the
// storage cap so consumers see the full stored value.
//
// nonDescWidth is the precomputed width of everything in the row except the
// description column and its leading inter-column separator.
func descStrMaxForWidth(termWidth, nonDescWidth int, short bool) int {
	if termWidth <= 0 {
		return listDescPipedStrMax
	}
	const interColSeparator = 1
	budget := termWidth - nonDescWidth - interColSeparator - descColOverhead(short)
	if budget < listDescMinStrLen {
		return listDescMinStrLen
	}
	if budget > listDescPipedStrMax {
		return listDescPipedStrMax
	}
	return budget
}

// nonDescRowWidth returns the displayed row width excluding the description
// column (index 0), using the already-computed prefix alignment and
// per-column max widths.
func nonDescRowWidth(prefixAlign int, colWidths []int) int {
	total := prefixAlign + 2 // prefix + min padding to first col
	first := true
	for i := 1; i < len(colWidths); i++ {
		if colWidths[i] == 0 {
			continue
		}
		if !first {
			total++
		}
		first = false
		total += colWidths[i]
	}
	return total
}

func FormatListView(view ListView, long, short bool) string {
	termWidth := detectStdoutWidth()
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
			// Dock-level surfaces (e.g., dock editor).
			if len(dock.Surfaces) > 0 {
				var dockSfRows []alignedRow
				for _, s := range dock.Surfaces {
					sfP, sfW := indentedPrefix(2, "sf", s.Name, short)
					dockSfRows = append(dockSfRows, alignedRow{
						prefix:      sfP,
						prefixWidth: sfW,
						metaCols:    surfaceMetaCols(s, long, short),
					})
				}
				dockSfAlign := alignWidth(dockSfRows)
				dockSfColWidths := alignColumnWidths(dockSfRows)
				for _, sr := range dockSfRows {
					writeAlignedLine(&b, sr.prefix, sr.prefixWidth, sr.metaCols, dockSfAlign, dockSfColWidths)
				}
			}

			if len(dock.Workspaces) == 0 && len(dock.Surfaces) == 0 {
				p, _ := indentedPrefix(2, "", "(no workspaces)", short)
				fmt.Fprintln(&b, p)
				continue
			}
			if len(dock.Workspaces) == 0 {
				continue
			}
			showChildren := view.Recursive || view.Focus.Kind == FocusWorkspace

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

			// Fit description column (index 0) to terminal width by
			// shrinking too-long rows in place. No-op when no workspace
			// has a description (wsColWidths[0] is 0).
			if wsColWidths[0] > 0 {
				strMax := descStrMaxForWidth(termWidth, nonDescRowWidth(wsAlign, wsColWidths), short)
				shrunk := false
				for i, ws := range dock.Workspaces {
					first := engine.DescriptionFirstLine(ws.Description)
					if first == "" || len(first) <= strMax {
						continue
					}
					wsRows[i].metaCols[0] = descField(first, strMax, short)
					shrunk = true
				}
				if shrunk {
					wsColWidths = alignColumnWidths(wsRows)
				}
			}

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

// showRow is a label/value pair for detail views (ws show, dock show, etc.).
type showRow struct{ label, value string }

// maxShowRowLabel returns the max label width across rows.
func maxShowRowLabel(rows []showRow) int {
	max := 0
	for _, r := range rows {
		if len(r.label) > max {
			max = len(r.label)
		}
	}
	return max
}

// printAlignedRows prints rows with right-aligned labels.
func printAlignedRows(rows []showRow) {
	maxLabel := maxShowRowLabel(rows)
	for _, r := range rows {
		pad := strings.Repeat(" ", maxLabel-len(r.label))
		fmt.Printf("%s%s %s\n", pad, dim(r.label), r.value)
	}
}

// writeAlignedRows writes rows with right-aligned labels to a builder.
func writeAlignedRows(b *strings.Builder, rows []showRow, minWidth int) {
	maxLabel := maxShowRowLabel(rows)
	if minWidth > maxLabel {
		maxLabel = minWidth
	}
	for _, r := range rows {
		pad := strings.Repeat(" ", maxLabel-len(r.label))
		fmt.Fprintf(b, "%s%s %s\n", pad, dim(r.label), r.value)
	}
}

func FormatWorkspaceShow(repoName, dockName string, ws *engine.WorkspaceInfo, long bool) string {
	var b strings.Builder

	var rows []showRow
	rows = append(rows, showRow{"workspace", ws.Name})
	if ws.Description != "" {
		// Show the first line as the aligned "description" row; render any
		// body as follow-on unlabeled rows so alignment stays clean.
		first, body, _ := strings.Cut(ws.Description, "\n")
		rows = append(rows, showRow{"description", first})
		if body != "" {
			for _, line := range strings.Split(body, "\n") {
				rows = append(rows, showRow{"", line})
			}
		}
	}
	rows = append(rows, showRow{"repo", repoName})
	rows = append(rows, showRow{"dock", dockName})
	if ws.Type != "" && ws.Type != "worktree" {
		rows = append(rows, showRow{"type", ws.Type})
	}
	rows = append(rows, showRow{"path", ws.Path})
	if ws.Branch != "" {
		rows = append(rows, showRow{"branch", ws.Branch})
	}
	if ws.PR != "" {
		rows = append(rows, showRow{"pr", ws.PR})
	}
	if ws.Dirty {
		rows = append(rows, showRow{"dirty", "yes"})
	}
	if ws.Pending {
		rows = append(rows, showRow{"pending", "yes"})
	}
	if ws.DefaultAgent != "" {
		rows = append(rows, showRow{"default agent", ws.DefaultAgent})
	}
	if ws.SyncStatus != "" && ws.SyncStatus != manifest.SyncStatusOK {
		rows = append(rows, showRow{"sync", ws.SyncStatus})
	}

	// Write rows, ensuring "surface" label fits in the alignment.
	sfLabel := "surface"
	writeAlignedRows(&b, rows, len(sfLabel))
	maxLabel := maxShowRowLabel(rows)
	if len(sfLabel) > maxLabel {
		maxLabel = len(sfLabel)
	}

	// Surface rows: right-align "surface" label to same column.
	sfPad := strings.Repeat(" ", maxLabel-len(sfLabel))
	sfRows := make([]alignedRow, len(ws.Surfaces))
	for i, s := range ws.Surfaces {
		prefix := sfPad + dim(sfLabel) + " " + s.Name
		width := maxLabel + 1 + utf8.RuneCountInString(s.Name)
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

// FormatSurfaceList renders a flat list of surfaces using the shared
// surfaceMetaCols / aligned-row machinery. currentSurface is highlighted
// with " *" when non-empty.
func FormatSurfaceList(surfaces []engine.SurfaceInfo, currentSurface string, short bool) string {
	if len(surfaces) == 0 {
		return ""
	}
	rows := make([]alignedRow, len(surfaces))
	for i, s := range surfaces {
		name := s.Name
		if s.Name == currentSurface {
			name += " *"
		}
		p, w := indentedPrefix(0, "sf", name, short)
		rows[i] = alignedRow{
			prefix:      p,
			prefixWidth: w,
			metaCols:    surfaceMetaCols(s, false, short),
		}
	}
	align := alignWidth(rows)
	colWidths := alignColumnWidths(rows)
	var b strings.Builder
	for _, r := range rows {
		writeAlignedLine(&b, r.prefix, r.prefixWidth, r.metaCols, align, colWidths)
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
