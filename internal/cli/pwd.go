package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/spf13/cobra"
)

func newPwdCmd() *cobra.Command {
	var jsonOutput, plain, short bool

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

			description := ""
			if ctx.Dock != "" && ctx.Workspace != "" {
				if ws, wsErr := eng.WsShow(ctx.Dock, ctx.Workspace); wsErr == nil {
					description = ws.Description
				}
			}

			out := formatPWD(ctx, description, short)
			if plain {
				out = stripANSI(out)
			}
			fmt.Println(out)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&plain, "plain", false, "plain-text output (no ANSI colors)")
	cmd.Flags().BoolVarP(&short, "short", "s", false, "compact output without labels or key names")
	return cmd
}

func formatPWD(ctx *engine.Context, description string, short bool) string {
	parts := make([]string, 0, 5)
	label := func(l, v string) string {
		if short {
			return v
		}
		return labelValue(l, v)
	}
	if ctx.Repo != "" {
		parts = append(parts, label("repo", ctx.Repo))
	}
	if ctx.Dock != "" {
		parts = append(parts, label("dock", ctx.Dock))
	}
	if ctx.Workspace != "" {
		ws := ctx.Workspace
		if ctx.Path != "" {
			dir := filepath.Base(ctx.Path)
			if dir != ctx.Workspace {
				if short {
					ws += " " + dim("(") + dir + dim(")")
				} else {
					ws += " " + dim("(") + dim("dir") + " " + dir + dim(")")
				}
			}
		}
		if description != "" {
			ws += " " + dim("—") + " " + description
		}
		parts = append(parts, label("workspace", ws))
	}
	if ctx.Surface != "" {
		parts = append(parts, label("surface", ctx.Surface))
	}
	return strings.Join(parts, dimmedSeparator(" / "))
}
