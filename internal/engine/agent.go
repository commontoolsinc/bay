package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

// bayAgentPreamble is automatically prepended to every agent config file.
// It gives agents the essential bay commands without requiring a user template.
const bayAgentPreamble = `# Bay Workspace

You are in bay workspace {workspace_name} ({workspace_id}) in the {dock} dock.
Working directory: {workspace_path}

Bay automatically detects your git branch and updates tmux window
names. You do not need to tell bay about branches.

## Updating bay

When you open a PR, tell bay the number:

    !bay ws update self --pr <number>

When your work is complete:

    !bay ws update self --status done

## Other bay commands

    !bay ws show self              # see workspace details
    !bay ls                        # see all workspaces across docks
    !bay win open self --shell     # open a shell window for this workspace

`

// generateAgentConfig writes the agent's config file into the workspace CWD.
// wsRepo is the workspace's actual repo name (may differ from the dock's default).
func (e *Engine) generateAgentConfig(dockName, agentName, wsID, wsName, wsPath string, wsType manifest.WorkspaceType, wsRepo string) error {
	agentCfg, ok := e.Config.Agents[agentName]
	if !ok {
		return fmt.Errorf("unknown agent %q", agentName)
	}

	dockCfg := e.Config.Docks[dockName]

	// Determine the effective repo for gitignore checks and template vars.
	// Use the workspace's repo if set, otherwise the dock's default.
	effectiveRepo := wsRepo
	if effectiveRepo == "" {
		effectiveRepo = dockCfg.Repo
	}

	// Check gitignore against the effective repo, not the workspace CWD.
	// The workspace CWD (a worktree) may not have its own .gitignore;
	// the check should be against the repo root.
	if agentCfg.ConfigFile != "" && effectiveRepo != "" {
		repoCfg, ok := e.Config.Repos[effectiveRepo]
		if ok {
			repoPath := config.ExpandPath(repoCfg.Path)
			ignored, err := e.Git.IsIgnored(repoPath, agentCfg.ConfigFile)
			if err != nil {
				// If not a git repo, skip gitignore check
			} else if !ignored {
				return fmt.Errorf(
					"%s is not in .gitignore for repo %q — add it first or run: bay setup",
					agentCfg.ConfigFile, effectiveRepo,
				)
			}
		}
	}

	if wsName == "" {
		wsName = wsID
	}

	// Resolve repo paths from the effective repo
	dockRepo := ""
	dockWorktreeDir := ""
	if effectiveRepo != "" {
		repoCfg, hasRepo := e.Config.Repos[effectiveRepo]
		if hasRepo {
			dockRepo = config.ExpandPath(repoCfg.Path)
			dockWorktreeDir = repoCfg.EffectiveWorktreeDir()
		}
	}

	replacements := map[string]string{
		"{workspace_id}":      wsID,
		"{workspace_name}":    wsName,
		"{workspace_type}":    string(wsType),
		"{dock}":              dockName,
		"{workspace_path}":    wsPath,
		"{dock_repo}":         dockRepo,
		"{dock_worktree_dir}": dockWorktreeDir,
	}

	// Build config content: bay preamble + user template
	var content strings.Builder
	content.WriteString(bayAgentPreamble)

	// Load and append user template if configured
	if dockCfg.AgentConfigTemplate != "" {
		templatePath := config.ExpandPath(dockCfg.AgentConfigTemplate)
		tmplData, err := os.ReadFile(templatePath)
		if err != nil {
			return fmt.Errorf("reading template %s: %w", templatePath, err)
		}
		content.WriteString("\n")
		content.WriteString(string(tmplData))
	}

	// Variable substitution on the full content
	result := content.String()
	for k, v := range replacements {
		result = strings.ReplaceAll(result, k, v)
	}

	// Write config file (ensure directory exists — worktree may be mock-created)
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		return fmt.Errorf("creating workspace dir: %w", err)
	}
	configPath := filepath.Join(wsPath, agentCfg.ConfigFile)
	if err := os.WriteFile(configPath, []byte(result), 0o644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// buildAgentCommand assembles the agent launch command.
func (e *Engine) buildAgentCommand(agentName string, dockCfg config.DockConfig) string {
	agentCfg := e.Config.Agents[agentName]
	parts := []string{agentCfg.Command}
	parts = append(parts, dockCfg.AgentArgs...)
	return strings.Join(parts, " ")
}

// cleanupAgentConfig removes bay-generated config files from a workspace.
func (e *Engine) cleanupAgentConfig(dockName string, ws *manifest.Workspace) {
	dockAgent := ""
	if dc, ok := e.Config.Docks[dockName]; ok {
		dockAgent = dc.Agent
	}
	agents := collectWorkspaceAgents(ws, dockAgent)
	// Remove config files for each agent
	for agentName := range agents {
		agentCfg, ok := e.Config.Agents[agentName]
		if !ok || agentCfg.ConfigFile == "" {
			continue
		}
		configPath := filepath.Join(ws.Path, agentCfg.ConfigFile)
		_ = os.Remove(configPath)
	}
}
