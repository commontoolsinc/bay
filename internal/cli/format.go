package cli

import (
	"fmt"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
)

// formatWorkspaceLine formats a single workspace as a table row.
func formatWorkspaceLine(ws engine.WorkspaceInfo) string {
	pr := ""
	if ws.PR != "" {
		pr = "#" + ws.PR
	}
	branch := ws.Branch
	if branch == "" {
		branch = "\u2014"
	}
	agent := ws.Agent
	if agent == "" {
		agent = "\u2014"
	}
	waiting := ""
	if ws.Waiting {
		waiting = " \u23f3"
	}
	return fmt.Sprintf("%-5s %-20s %-30s %-6s %-8s %-8s%s",
		ws.ID, ws.Name, branch, pr, ws.Status, agent, waiting)
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
				fmt.Fprintf(&b, "    %s\n", formatWorkspaceLine(ws))
			}
		}
	}

	return b.String()
}

// FormatDockTree formats dock → workspace hierarchy for one or more docks.
func FormatDockTree(docks []engine.DockInfo) string {
	var b strings.Builder
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
			fmt.Fprintf(&b, "  %s\n", formatWorkspaceLine(ws))
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
	repo, hasRepo := cfg.Repos[repoName]
	if hasRepo {
		fmt.Fprintf(&b, "  repo %s (%s)\n", repoName, repo.Path)
	} else {
		fmt.Fprintf(&b, "  repo %s\n", repoName)
	}
	for _, d := range docks {
		fmt.Fprintf(&b, "    dock %s\n", d.Name)
		for _, ws := range d.Workspaces {
			fmt.Fprintf(&b, "      %s\n", formatWorkspaceLine(ws))
		}
	}
	return b.String()
}
