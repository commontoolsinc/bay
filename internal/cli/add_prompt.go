package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/mpsalisbury/bay/internal/config"
	tmuxpkg "github.com/mpsalisbury/bay/internal/tmux"
	"github.com/spf13/cobra"
)

func newAddPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-prompt [name]",
		Short: "Capture agent-waiting pattern from current pane",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := tmuxpkg.NewReal()

			// Capture current pane content
			winID, err := t.CurrentWindowID()
			if err != nil {
				return fmt.Errorf("not in a tmux window: %w", err)
			}

			panes, err := t.ListPanes(winID)
			if err != nil {
				return fmt.Errorf("listing panes: %w", err)
			}
			if len(panes) == 0 {
				return fmt.Errorf("no panes in current window")
			}

			// Capture last 5 lines from first pane
			content, err := t.CapturePane(panes[0].ID, 5)
			if err != nil {
				return fmt.Errorf("capturing pane: %w", err)
			}

			lines := strings.Split(strings.TrimSpace(content), "\n")
			if len(lines) == 0 {
				return fmt.Errorf("pane is empty")
			}

			// Use the last non-empty line as the pattern
			var pattern string
			for i := len(lines) - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				if line != "" {
					pattern = line
					break
				}
			}
			if pattern == "" {
				return fmt.Errorf("no non-empty lines found")
			}

			// Append to prompts file
			promptsPath := config.DefaultConfigDir() + "/bay-prompts.txt"
			f, err := os.OpenFile(promptsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("opening prompts file: %w", err)
			}
			defer f.Close()

			if _, err := fmt.Fprintf(f, "\n# Captured from pane\n%s\n", pattern); err != nil {
				return fmt.Errorf("writing pattern: %w", err)
			}

			fmt.Printf("Added pattern: %s\n", pattern)
			fmt.Printf("Edit %s to generalize to a regex.\n", promptsPath)
			return nil
		},
	}
}
