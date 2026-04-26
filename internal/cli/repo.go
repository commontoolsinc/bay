package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/spf13/cobra"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "repo",
		Aliases: []string{"rp"},
		Short:   "Manage repos",
	}

	cmd.AddCommand(
		newRepoAddCmd(),
		newRepoLsCmd(),
		newRepoTreeCmd(),
		newRepoShowCmd(),
		newRepoRemoveCmd(),
		newRepoInitCmd(),
		newRepoSyncCmd(),
	)

	return cmd
}

func newRepoAddCmd() *cobra.Command {
	var worktreeDir, cloneURL string
	var force bool

	cmd := &cobra.Command{
		Use:   "add <name> <path>",
		Short: "Add a repo to bay config",
		Long: `Add a repo to bay config. The path must be an existing local git checkout.

With --url, clones the repo first. The path must NOT already exist:
  bay repo add myproject ~/projects/myproject --url git@github.com:org/myproject.git

Use --force to add a directory that is not a git repo.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			if cloneURL != "" {
				fmt.Printf("Cloning %s into %s...\n", cloneURL, args[1])
			}
			if err := eng.RepoAdd(args[0], args[1], worktreeDir, cloneURL, force); err != nil {
				return err
			}
			fmt.Printf("Repo %q added (%s)\n", args[0], args[1])

			// Auto-init bay awareness.
			if initErr := eng.RepoInit(args[0]); initErr != nil {
				fmt.Fprintf(os.Stderr, "warning: repo init: %v\n", initErr)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&worktreeDir, "worktree-dir", "", "worktree directory (default: <path>-worktrees)")
	cmd.Flags().StringVar(&cloneURL, "url", "", "git URL to clone (path must not exist)")
	cmd.Flags().BoolVar(&force, "force", false, "add even if path is not a git repo")

	return cmd
}

func newRepoLsCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List configured repos",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			repos, repoErr := eng.RepoList()
			if repoErr != nil {
				return repoErr
			}
			docks, err := eng.List()
			if err != nil {
				return err
			}

			repoInfos := make([]engine.RepoInfo, len(repos))
			for i, r := range repos {
				repoInfos[i] = engine.RepoInfo{
					Name:        r.Name,
					Path:        r.Path,
					WorktreeDir: r.EffectiveWorktreeDir(),
				}
			}

			if jsonOutput {
				entries := make([]RepoListEntry, len(repoInfos))
				for i, r := range repoInfos {
					entries[i] = RepoListEntry{Name: r.Name, Path: r.Path, WorktreeDir: r.WorktreeDir}
					for _, d := range docks {
						if d.Repo == r.Name {
							entries[i].Docks = append(entries[i].Docks, d.Name)
						}
					}
				}
				data, jsonErr := json.MarshalIndent(entries, "", "  ")
				if jsonErr != nil {
					return jsonErr
				}
				fmt.Println(string(data))
				return nil
			}

			if len(repos) == 0 {
				fmt.Println("No repos configured.")
				return nil
			}

			fmt.Print(FormatRepoTree(repoInfos, docks))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

func newRepoTreeCmd() *cobra.Command {
	var longOutput bool
	var shortOutput bool

	cmd := &cobra.Command{
		Use:   "tree [name]",
		Short: "Tree view of a repo's docks, workspaces, and surfaces (default: current)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			name, err := inferRepoName(eng, args)
			if err != nil {
				return err
			}

			docks, err := eng.List()
			if err != nil {
				return err
			}

			view := BuildListView(docks, ListViewOptions{
				Focus:     ListFocus{Kind: FocusRepo, Repo: name},
				Recursive: true,
			})
			view.SetCurrentContext(eng)

			fmt.Print(FormatListView(view, longOutput, shortOutput))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&longOutput, "long", "l", false, "show extended details such as tmux IDs")
	cmd.Flags().BoolVarP(&shortOutput, "short", "s", false, "compact output without labels or key names")

	return cmd
}

// RepoListEntry is a repo summary plus its dock names, returned by `bay rp ls --json`.
type RepoListEntry struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	WorktreeDir string   `json:"worktree_dir"`
	Docks       []string `json:"docks,omitempty"`
}

func newRepoShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show [name]",
		Aliases: []string{"cat"},
		Short:   "Show repo details (defaults to current repo)",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			m, mErr := eng.LoadManifest()
			if mErr != nil {
				return mErr
			}

			var name string
			if len(args) > 0 {
				name = args[0]
			} else {
				// Infer from current dock's repo.
				session, err := eng.Tmux.CurrentSession()
				if err == nil {
					if dock := m.FindDock(session); dock != nil && dock.Repo != "" {
						name = dock.Repo
					}
				}
				if name == "" {
					return fmt.Errorf("not in a dock — pass a repo name")
				}
			}

			repo := m.FindRepo(name)
			if repo == nil {
				return fmt.Errorf("repo %q not found", name)
			}

			var dockNames []string
			for i := range m.Docks {
				if m.Docks[i].Repo == name {
					dockNames = append(dockNames, m.Docks[i].Name)
				}
			}
			wsCount := 0
			for _, dn := range dockNames {
				if dock := m.FindDock(dn); dock != nil {
					wsCount += len(dock.Workspaces)
				}
			}

			rows := []showRow{
				{"repo", name},
				{"path", repo.Path},
				{"worktree dir", repo.EffectiveWorktreeDir()},
			}
			if len(dockNames) > 0 {
				rows = append(rows, showRow{"docks", strings.Join(dockNames, ", ")})
			}
			rows = append(rows, showRow{"workspaces", fmt.Sprintf("%d", wsCount)})

			printAlignedRows(rows)
			return nil
		},
	}
}

func newRepoRemoveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a repo from bay config",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			// Save repo path before removal (manifest is gone after)
			var repoPath string
			m, _ := eng.LoadManifest()
			if m != nil {
				if repo := m.FindRepo(args[0]); repo != nil {
					repoPath = config.ExpandPath(repo.Path)
				}
			}

			if err := eng.RepoRemove(args[0], force); err != nil {
				if inUse, ok := err.(*engine.RepoInUseError); ok {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"Cannot remove repo %q. Use --force to also remove:\n%s",
						args[0],
						FormatSubtreeForRemoval(inUse.RepoName, inUse.AffectedDocks))
					return fmt.Errorf("repo %q is in use", args[0])
				}
				return err
			}
			fmt.Printf("Repo %q removed from bay.\n", args[0])
			if repoPath != "" {
				fmt.Printf("The repo directory is still on disk at %s\n", repoPath)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "also close and remove docks that use this repo")

	return cmd
}

func newRepoInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Set up bay awareness in a repo (idempotent)",
		Long: `Set up bay awareness in a repo's project files.

For each configured agent with a project_file, appends a bay awareness
line if not already present. Creates .worktreeinclude if missing.

  bay repo init labs    init by repo name
  bay repo init         infer repo from CWD`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			repoName, err := inferRepoName(eng, args)
			if err != nil {
				return err
			}

			if err := eng.RepoInit(repoName); err != nil {
				return err
			}
			fmt.Printf("Repo %q initialized for bay.\n", repoName)
			return nil
		},
	}
}

func newRepoSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync [name]",
		Short: "Copy .worktreeinclude files into all worktrees",
		Long: `Copy files matching .worktreeinclude patterns (gitignore
syntax) from the repo root into every existing worktree for the repo.

Bay refuses to sync matches that are tracked in git or not covered by
.gitignore (refusals are logged to stderr; valid matches still copy).

  bay repo sync labs    sync by repo name
  bay repo sync         infer repo from CWD`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			repoName, err := inferRepoName(eng, args)
			if err != nil {
				return err
			}

			count, err := eng.RepoSync(repoName)
			if err != nil {
				return err
			}
			fmt.Printf("Synced .worktreeinclude into %d worktree(s).\n", count)
			return nil
		},
	}
}

// inferRepoName returns args[0] if provided, otherwise resolves the repo
// from the current working directory (checking both repo paths and worktree dirs).
func inferRepoName(eng *engine.Engine, args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		return "", fmt.Errorf("cannot determine working directory")
	}
	root, rootErr := eng.Git.RepoRoot(cwd)
	if rootErr != nil {
		return "", fmt.Errorf("not in a git repository")
	}
	rootCanonical := config.CanonicalPath(root)
	repos, _ := eng.RepoList()
	for _, repo := range repos {
		if config.CanonicalPath(repo.Path) == rootCanonical {
			return repo.Name, nil
		}
		wtDir := config.CanonicalPath(repo.EffectiveWorktreeDir())
		if strings.HasPrefix(rootCanonical, wtDir+string(filepath.Separator)) {
			return repo.Name, nil
		}
	}
	return "", fmt.Errorf("current repo not in bay config; use bay repo add first")
}
