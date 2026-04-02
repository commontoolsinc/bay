package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/commontoolsinc/bay/internal/engine"
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

			fmt.Println(formatPWD(ctx))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	return cmd
}

func formatPWD(ctx *engine.Context) string {
	parts := make([]string, 0, 5)
	if ctx.Repo != "" {
		parts = append(parts, labelValue("repo", ctx.Repo))
	}
	if ctx.Dock != "" {
		parts = append(parts, labelValue("dock", ctx.Dock))
	}
	if ctx.WorkspaceID != "" {
		parts = append(parts, labelValue("workspace", ctx.WorkspaceID))
	}
	if ctx.Window != "" {
		parts = append(parts, labelValue("window", ctx.Window))
	} else if ctx.WindowID != 0 {
		parts = append(parts, labelValue("window", fmt.Sprintf("%d", ctx.WindowID)))
	}
	if ctx.PaneID != 0 {
		parts = append(parts, labelValue("pane", fmt.Sprintf("%d", ctx.PaneID)))
	}
	return strings.Join(parts, dimmedSeparator(" / "))
}
