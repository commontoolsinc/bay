package cli

import (
	"encoding/json"
	"fmt"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func newLsCmd() *cobra.Command {
	var jsonOutput bool
	var rowsOutput bool
	var dirtyOnly bool
	var recursive bool
	var longOutput bool
	var shortOutput bool

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "Browse the Bay hierarchy",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			docks, err := eng.List()
			if err != nil {
				return err
			}

			// User-interest signal: any workspace the user is viewing
			// that still has no PR despite having a branch gets marked
			// stale so the monitor's next tick re-queries gh, rather
			// than waiting for the full TTL.
			for _, d := range docks {
				for _, ws := range d.Workspaces {
					if ws.Branch != "" && ws.PR == "" {
						_ = eng.MarkPRCheckStale(d.Name, ws.Name)
					}
				}
			}

			if dirtyOnly {
				docks = filterDirtyWorkspaces(eng, docks)
			}

			view := BuildListView(docks, ListViewOptions{
				Focus:     resolveListFocus(eng, dirtyOnly),
				Recursive: recursive,
			})

			view.SetCurrentContext(eng)

			if jsonOutput {
				var payload interface{} = view
				if rowsOutput {
					payload = ListRows(view)
				}
				data, err := json.MarshalIndent(payload, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			fmt.Print(FormatListView(view, longOutput, shortOutput))

			// Print advice for stale or missing workspaces visible in the output.
			hasStale, hasMissing := false, false
			for _, repo := range view.Repos {
				for _, d := range repo.Docks {
					for _, ws := range d.Workspaces {
						if ws.SyncStatus == manifest.SyncStatusStale {
							hasStale = true
						}
						if ws.SyncStatus == manifest.SyncStatusMissing {
							hasMissing = true
						}
					}
				}
			}
			if hasStale {
				fmt.Println("\n[stale] = tmux window was closed. Run 'bay recover' to recreate, or 'bay ws close <name>' to remove.")
			}
			if hasMissing {
				fmt.Println("\n[missing] = worktree directory was deleted. Run 'bay ws close <name> --force' to clean up.")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().BoolVar(&rowsOutput, "rows", false, "output JSON as denormalized rows")
	cmd.Flags().BoolVar(&dirtyOnly, "dirty", false, "only show dirty workspaces")
	cmd.Flags().BoolVarP(&recursive, "recursive", "R", false, "show full descendant tree from the current focus")
	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "show extended details such as tmux IDs")
	cmd.Flags().BoolVarP(&shortOutput, "short", "s", false, "compact output without labels or key names")

	return cmd
}

// filterDirtyWorkspaces filters dock info to only include workspaces with
// uncommitted changes.
func filterDirtyWorkspaces(eng *engine.Engine, docks []engine.DockInfo) []engine.DockInfo {
	var result []engine.DockInfo
	for _, d := range docks {
		filtered := engine.DockInfo{
			Name:  d.Name,
			Agent: d.Agent,
			Repo:  d.Repo,
		}
		for _, ws := range d.Workspaces {
			if ws.Path == "" {
				continue
			}
			path := config.ExpandPath(ws.Path)
			dirty, err := eng.Git.IsDirty(path)
			if err != nil {
				continue
			}
			if dirty {
				filtered.Workspaces = append(filtered.Workspaces, ws)
			}
		}
		if len(filtered.Workspaces) > 0 {
			result = append(result, filtered)
		}
	}
	return result
}

func resolveListFocus(eng *engine.Engine, dirtyOnly bool) ListFocus {
	if dirtyOnly {
		return ListFocus{Kind: FocusAll}
	}

	return inferListFocus(eng)
}

func inferListFocus(eng *engine.Engine) ListFocus {
	ctx, err := eng.CurrentContext()
	if err != nil {
		return ListFocus{Kind: FocusAll}
	}
	switch {
	case ctx.Workspace != "":
		return ListFocus{Kind: FocusWorkspace, Repo: ctx.Repo, Dock: ctx.Dock, WorkspaceID: ctx.Workspace}
	case ctx.Dock != "":
		return ListFocus{Kind: FocusDock, Repo: ctx.Repo, Dock: ctx.Dock}
	case ctx.Repo != "":
		return ListFocus{Kind: FocusRepo, Repo: ctx.Repo}
	default:
		return ListFocus{Kind: FocusAll}
	}
}
