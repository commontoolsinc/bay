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
	info, ok := e.Config.ResolveAgent(agentName)
	if !ok {
		return fmt.Errorf("unknown agent %q", agentName)
	}
	if strings.TrimSpace(info.Command) == "" {
		return fmt.Errorf("agent %q has no command configured", agentName)
	}
	return nil
}

// DefaultAgent returns the effective default agent for a dock, or "" if none.
func (e *Engine) DefaultAgent(dockName string) string {
	m, err := e.LoadManifest()
	if err != nil {
		return ""
	}
	return e.resolvedDockAgent(dockName, m)
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

// buildAgentCommand assembles the agent launch command. When resume is
// true, the agent's configured resume_args are spliced in (e.g.
// "--continue" for Claude Code) so the prior session is picked up;
// used by recovery and undo-close.
func (e *Engine) buildAgentCommand(agentName string, agentArgs []string, resume bool) (string, error) {
	if err := e.validateAgentName(agentName); err != nil {
		return "", err
	}
	info, _ := e.Config.ResolveAgent(agentName)
	parts := []string{info.Command}
	if resume && info.ResumeArgs != "" {
		parts = append(parts, strings.Fields(info.ResumeArgs)...)
	}
	parts = append(parts, agentArgs...)
	return strings.Join(parts, " "), nil
}
