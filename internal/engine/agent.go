package engine

import (
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
)

// buildAgentCommand assembles the agent launch command.
func (e *Engine) buildAgentCommand(agentName string, dockCfg config.DockConfig) string {
	agentCfg := e.Config.Agents[agentName]
	parts := []string{agentCfg.Command}
	parts = append(parts, dockCfg.AgentArgs...)
	return strings.Join(parts, " ")
}
