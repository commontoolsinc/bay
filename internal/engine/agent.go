package engine

import (
	"fmt"
	"strings"

	"github.com/commontoolsinc/bay/internal/manifest"
)

func (e *Engine) validateAgentName(agentName string) error {
	if agentName == "" {
		return fmt.Errorf("agent name is required")
	}
	agentCfg, ok := e.Config.Agents[agentName]
	if !ok {
		return fmt.Errorf("unknown agent %q", agentName)
	}
	if strings.TrimSpace(agentCfg.Command) == "" {
		return fmt.Errorf("agent %q has no command configured", agentName)
	}
	return nil
}

func (e *Engine) resolveWorkspaceAgent(dockName string, m *manifest.Manifest, requested string, requireAgent bool) (string, error) {
	if requested != "" {
		if err := e.validateAgentName(requested); err != nil {
			return "", err
		}
		return requested, nil
	}

	agentName := e.resolvedDockAgent(dockName, m)
	if agentName == "" {
		if requireAgent {
			return "", fmt.Errorf("dock %q has no default agent configured", dockName)
		}
		return "", nil
	}
	if err := e.validateAgentName(agentName); err != nil {
		return "", err
	}
	return agentName, nil
}

// buildAgentCommand assembles the agent launch command for first launch.
func (e *Engine) buildAgentCommand(agentName string, agentArgs []string) (string, error) {
	if err := e.validateAgentName(agentName); err != nil {
		return "", err
	}
	agentCfg := e.Config.Agents[agentName]
	parts := []string{agentCfg.Command}
	parts = append(parts, agentArgs...)
	return strings.Join(parts, " "), nil
}

// buildAgentResumeCommand assembles the agent launch command for restart/recovery.
// Includes resume_args if configured (e.g. "--continue" for Claude Code).
func (e *Engine) buildAgentResumeCommand(agentName string, agentArgs []string) (string, error) {
	if err := e.validateAgentName(agentName); err != nil {
		return "", err
	}
	agentCfg := e.Config.Agents[agentName]
	parts := []string{agentCfg.Command}
	if agentCfg.ResumeArgs != "" {
		parts = append(parts, strings.Fields(agentCfg.ResumeArgs)...)
	}
	parts = append(parts, agentArgs...)
	return strings.Join(parts, " "), nil
}
