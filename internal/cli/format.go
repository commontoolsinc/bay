package cli

import (
	"fmt"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
)

// workspaceColumns returns the display values for a workspace row.
func workspaceColumns(ws engine.WorkspaceInfo) (id, name, branch, pr, status, agent, suffix string) {
	id = ws.ID
	name = ws.Name
	branch = ws.Branch
	if branch == "" {
		branch = "\u2014"
	}
	pr = ""
	if ws.PR != "" {
		pr = "#" + ws.PR
	}
	status = ws.Status
	agent = ws.Agent
	if agent == "" {
		agent = "\u2014"
	}
	if ws.Missing {
		suffix += " [missing]"
	}
	if ws.Waiting {
		suffix += " \u23f3"
	}
	return
}

// columnWidths tracks the maximum width of each column.
type columnWidths struct {
	id, name, branch, pr, status, agent int
}

// update expands widths to accommodate the given values.
func (c *columnWidths) update(id, name, branch, pr, status, agent string) {
	if len(id) > c.id {
		c.id = len(id)
	}
	if len(name) > c.name {
		c.name = len(name)
	}
	if len(branch) > c.branch {
		c.branch = len(branch)
	}
	if len(pr) > c.pr {
		c.pr = len(pr)
	}
	if len(status) > c.status {
		c.status = len(status)
	}
	if len(agent) > c.agent {
		c.agent = len(agent)
	}
}

func (c *columnWidths) format(id, name, branch, pr, status, agent, suffix string) string {
	return fmt.Sprintf("%-*s %-*s %-*s %-*s %-*s %-*s%s",
		c.id, id, c.name, name, c.branch, branch, c.pr, pr, c.status, status, c.agent, agent, suffix)
}

// formatWorkspaceLine formats a single workspace as a table row using default widths.
func formatWorkspaceLine(ws engine.WorkspaceInfo) string {
	id, name, branch, pr, status, agent, suffix := workspaceColumns(ws)
	// Use reasonable minimums for single-line formatting
	w := &columnWidths{id: 3, name: 4, branch: 6, pr: 2, status: 6, agent: 5}
	w.update(id, name, branch, pr, status, agent)
	return w.format(id, name, branch, pr, status, agent, suffix)
}

// FormatFullTree formats the full repo → dock → workspace hierarchy.
func FormatFullTree(cfg *config.Config, docks []engine.DockInfo) string {
	var b strings.Builder

	// Group docks by repo
	type repoEntry struct {
		name  string
		path  string
		docks []engine.DockInfo
	}

	repoMap := map[string]*repoEntry{}
	var repoOrder []string

	// Initialize repos from config
	for name, repo := range cfg.Repos {
		repoMap[name] = &repoEntry{name: name, path: repo.Path}
		repoOrder = append(repoOrder, name)
	}

	// Assign docks to repos
	for _, d := range docks {
		if re, ok := repoMap[d.Repo]; ok {
			re.docks = append(re.docks, d)
		} else {
			// Dock with no repo or unknown repo — show under a synthetic entry
			key := "(no repo)"
			if _, ok := repoMap[key]; !ok {
				repoMap[key] = &repoEntry{name: key}
				repoOrder = append(repoOrder, key)
			}
			repoMap[key].docks = append(repoMap[key].docks, d)
		}
	}

	// First pass: compute column widths across all workspaces
	w := &columnWidths{id: 2, name: 4, branch: 6, pr: 2, status: 6, agent: 5}
	for _, d := range docks {
		for _, ws := range d.Workspaces {
			id, name, branch, pr, status, agent, _ := workspaceColumns(ws)
			w.update(id, name, branch, pr, status, agent)
		}
	}

	for _, repoName := range repoOrder {
		re := repoMap[repoName]
		if re.path != "" {
			fmt.Fprintf(&b, "repo %s (%s)\n", re.name, re.path)
		} else {
			fmt.Fprintf(&b, "repo %s\n", re.name)
		}
		if len(re.docks) == 0 {
			fmt.Fprintf(&b, "  (no docks)\n")
			continue
		}
		for _, d := range re.docks {
			fmt.Fprintf(&b, "  dock %s\n", d.Name)
			if len(d.Workspaces) == 0 {
				fmt.Fprintf(&b, "    (no workspaces)\n")
				continue
			}
			for _, ws := range d.Workspaces {
				id, name, branch, pr, status, agent, suffix := workspaceColumns(ws)
				fmt.Fprintf(&b, "    %s\n", w.format(id, name, branch, pr, status, agent, suffix))
			}
		}
	}

	return b.String()
}

// FormatDockTree formats dock → workspace hierarchy for one or more docks.
func FormatDockTree(docks []engine.DockInfo) string {
	var b strings.Builder

	// Compute column widths across all docks
	w := &columnWidths{id: 2, name: 4, branch: 6, pr: 2, status: 6, agent: 5}
	for _, d := range docks {
		for _, ws := range d.Workspaces {
			id, name, branch, pr, status, agent, _ := workspaceColumns(ws)
			w.update(id, name, branch, pr, status, agent)
		}
	}

	for _, d := range docks {
		meta := ""
		if d.Repo != "" || d.Agent != "" {
			var parts []string
			if d.Repo != "" {
				parts = append(parts, "repo="+d.Repo)
			}
			if d.Agent != "" {
				parts = append(parts, "agent="+d.Agent)
			}
			meta = " (" + strings.Join(parts, ", ") + ")"
		}
		fmt.Fprintf(&b, "dock %s%s\n", d.Name, meta)
		if len(d.Workspaces) == 0 {
			fmt.Fprintf(&b, "  (no workspaces)\n")
			continue
		}
		for _, ws := range d.Workspaces {
			id, name, branch, pr, status, agent, suffix := workspaceColumns(ws)
			fmt.Fprintf(&b, "  %s\n", w.format(id, name, branch, pr, status, agent, suffix))
		}
	}
	return b.String()
}

// FormatRepoTree formats repo → docks summary.
func FormatRepoTree(cfg *config.Config) string {
	var b strings.Builder
	for name, repo := range cfg.Repos {
		wtDir := repo.EffectiveWorktreeDir()
		fmt.Fprintf(&b, "%s  %s  (worktrees: %s)\n", name, repo.Path, wtDir)
		// Find docks that use this repo
		var dockNames []string
		for dockName, dock := range cfg.Docks {
			if dock.Repo == name {
				dockNames = append(dockNames, dockName)
			}
		}
		if len(dockNames) > 0 {
			fmt.Fprintf(&b, "  docks: %s\n", strings.Join(dockNames, ", "))
		}
	}
	return b.String()
}

// FormatSubtreeForRemoval formats the repo → dock → workspace hierarchy
// for items that would be removed. Used by repo remove's error message.
func FormatSubtreeForRemoval(cfg *config.Config, repoName string, docks []engine.DockInfo) string {
	var b strings.Builder

	// Compute column widths
	w := &columnWidths{id: 2, name: 4, branch: 6, pr: 2, status: 6, agent: 5}
	for _, d := range docks {
		for _, ws := range d.Workspaces {
			id, name, branch, pr, status, agent, _ := workspaceColumns(ws)
			w.update(id, name, branch, pr, status, agent)
		}
	}

	repo, hasRepo := cfg.Repos[repoName]
	if hasRepo {
		fmt.Fprintf(&b, "  repo %s (%s)\n", repoName, repo.Path)
	} else {
		fmt.Fprintf(&b, "  repo %s\n", repoName)
	}
	for _, d := range docks {
		fmt.Fprintf(&b, "    dock %s\n", d.Name)
		for _, ws := range d.Workspaces {
			id, name, branch, pr, status, agent, suffix := workspaceColumns(ws)
			fmt.Fprintf(&b, "      %s\n", w.format(id, name, branch, pr, status, agent, suffix))
		}
	}
	return b.String()
}
