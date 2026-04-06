package engine

import (
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
)

// buildAgentCommand assembles the agent launch command for first launch.
func (e *Engine) buildAgentCommand(agentName string, dockCfg config.DockConfig) string {
	agentCfg := e.Config.Agents[agentName]
	parts := []string{agentCfg.Command}
	parts = append(parts, dockCfg.AgentArgs...)
	return strings.Join(parts, " ")
}

// buildAgentResumeCommand assembles the agent launch command for restart/recovery.
// Includes resume_args if configured (e.g. "--continue" for Claude Code).
func (e *Engine) buildAgentResumeCommand(agentName string, dockCfg config.DockConfig) string {
	agentCfg := e.Config.Agents[agentName]
	parts := []string{agentCfg.Command}
	if agentCfg.ResumeArgs != "" {
		parts = append(parts, strings.Fields(agentCfg.ResumeArgs)...)
	}
	parts = append(parts, dockCfg.AgentArgs...)
	return strings.Join(parts, " ")
}
