package cli

import (
	_ "embed"
	"fmt"

	"github.com/spf13/cobra"
)

//go:embed agent-guide.md
var agentGuideContent string

func newAgentGuideCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "agent-guide",
		Short:  "Print bay instructions for agents",
		Hidden: true, // used by skills, not direct user invocation
		Args:   cobra.NoArgs,
		// Pure stdout — never fork the monitor.
		Annotations: map[string]string{noMonitorAutostartAnnotation: "true"},
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(agentGuideContent)
		},
	}
}
