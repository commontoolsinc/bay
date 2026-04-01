package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/nav"
	"github.com/spf13/cobra"
)

func newGoCmd() *cobra.Command {
	var waiting, nextWaiting bool

	cmd := &cobra.Command{
		Use:   "go [query]",
		Short: "Fuzzy find and switch to a window",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			// Early tmux detection: bail out if not running inside tmux
			if _, tmuxErr := eng.Tmux.CurrentSession(); tmuxErr != nil {
				fmt.Println("bay go requires tmux — use bay ls to see workspaces")
				return nil
			}

			m, err := eng.LoadManifest()
			if err != nil {
				return err
			}

			entries := nav.CollectEntries(m, eng.Tmux)

			if nextWaiting {
				currentWinID, _ := eng.Tmux.CurrentWindowID()
				entry := nav.NextWaiting(entries, currentWinID)
				if entry == nil {
					fmt.Println("No waiting windows.")
					return nil
				}
				return eng.Tmux.SelectWindow(entry.TmuxWindowID)
			}

			if waiting {
				entries = nav.FilterWaiting(entries)
			}

			query := ""
			if len(args) > 0 {
				query = args[0]
			}

			if query != "" {
				entries = nav.FuzzyMatch(entries, query)
			}

			switch len(entries) {
			case 0:
				fmt.Println("No matching workspaces.")
				return nil
			case 1:
				return eng.Tmux.SelectWindow(entries[0].TmuxWindowID)
			default:
				return fzfPick(entries, eng)
			}
		},
	}

	cmd.Flags().BoolVar(&waiting, "waiting", false, "filter to waiting windows")
	cmd.Flags().BoolVar(&nextWaiting, "next-waiting", false, "jump to next waiting window")

	return cmd
}

func fzfPick(entries []nav.Entry, eng *engine.Engine) error {
	// Build aligned lines — 1:1 with entries slice by index
	formatted := nav.FormatEntries(entries)
	lines := strings.Split(strings.TrimRight(formatted, "\n"), "\n")

	// Try fzf
	fzfPath, err := exec.LookPath("fzf")
	if err != nil {
		// No fzf, just print
		fmt.Println(formatted)
		return nil
	}

	fzfCmd := exec.Command(fzfPath, "--ansi", "--no-sort")
	fzfCmd.Stdin = strings.NewReader(formatted)
	fzfCmd.Stderr = os.Stderr

	out, err := fzfCmd.Output()
	if err != nil {
		return nil // user cancelled fzf
	}

	// Match selected line back to entry by exact content.
	// This correctly handles multi-window workspaces where dock+wsID are identical.
	selected := strings.TrimSpace(string(out))
	for i, line := range lines {
		if line == selected {
			return eng.Tmux.SelectWindow(entries[i].TmuxWindowID)
		}
	}

	return nil
}
