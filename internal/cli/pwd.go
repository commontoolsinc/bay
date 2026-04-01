package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newPwdCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "pwd",
		Short: "Show the current Bay context",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			ctx, err := eng.CurrentContext()
			if err != nil {
				return err
			}

			if jsonOutput {
				data, err := json.MarshalIndent(ctx, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			var parts []string
			if ctx.Repo != "" {
				parts = append(parts, kv("repo", ctx.Repo))
			}
			if ctx.Dock != "" {
				parts = append(parts, kv("dock", ctx.Dock))
			}
			if ctx.WorkspaceID != "" {
				parts = append(parts, kv("workspace", ctx.WorkspaceID))
			}
			if ctx.WindowID != 0 {
				parts = append(parts, kv("window", fmt.Sprintf("%d", ctx.WindowID)))
			}
			if ctx.PaneID != 0 {
				parts = append(parts, kv("pane", fmt.Sprintf("%d", ctx.PaneID)))
			}
			fmt.Println(strings.Join(parts, " "))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}
