package engine

import (
	"strings"
)

// buildAgentCommand assembles the agent launch command for first launch.
func (e *Engine) buildAgentCommand(agentName string, agentArgs []string) string {
	agentCfg := e.Config.Agents[agentName]
	parts := []string{agentCfg.Command}
	parts = append(parts, agentArgs...)
	return strings.Join(parts, " ")
}

// buildAgentResumeCommand assembles the agent launch command for restart/recovery.
// Includes resume_args if configured (e.g. "--continue" for Claude Code).
func (e *Engine) buildAgentResumeCommand(agentName string, agentArgs []string) string {
	agentCfg := e.Config.Agents[agentName]
	parts := []string{agentCfg.Command}
	if agentCfg.ResumeArgs != "" {
		parts = append(parts, strings.Fields(agentCfg.ResumeArgs)...)
	}
	parts = append(parts, agentArgs...)
	return strings.Join(parts, " ")
}
