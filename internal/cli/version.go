package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print bay version",
		Args:  cobra.NoArgs,
		// Pure stdout — never fork the monitor.
		Annotations: map[string]string{noMonitorAutostartAnnotation: "true"},
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("bay " + version)
		},
	}
}
