package cli

import (
	"fmt"
	"os"
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
		newRepoShowCmd(),
		newRepoRemoveCmd(),
		newRepoInitCmd(),
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
	return &cobra.Command{
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
			if len(repos) == 0 {
				fmt.Println("No repos configured.")
				return nil
			}
			var repoInfos []engine.RepoInfo
			for _, r := range repos {
				repoInfos = append(repoInfos, engine.RepoInfo{
					Name:        r.Name,
					Path:        r.Path,
					WorktreeDir: r.EffectiveWorktreeDir(),
				})
			}
			docks, _ := eng.List()
			fmt.Print(FormatRepoTree(repoInfos, docks))
			return nil
		},
	}
}

func newRepoShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show <name>",
		Aliases: []string{"cat"},
		Short:   "Show repo details",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			name := args[0]
			m, mErr := eng.LoadManifest()
			if mErr != nil {
				return mErr
			}
			repo := m.FindRepo(name)
			if repo == nil {
				return fmt.Errorf("repo %q not found", name)
			}

			fmt.Printf("Repo: %s\n", name)
			fmt.Printf("  path:         %s\n", repo.Path)
			fmt.Printf("  worktree_dir: %s\n", repo.EffectiveWorktreeDir())

			// Docks using this repo
			var dockNames []string
			for i := range m.Docks {
				if m.Docks[i].Repo == name {
					dockNames = append(dockNames, m.Docks[i].Name)
				}
			}
			if len(dockNames) > 0 {
				fmt.Printf("  docks: %s\n", strings.Join(dockNames, ", "))
			} else {
				fmt.Println("  docks: (none)")
			}

			// Active worktree count
			wsCount := 0
			for _, dockName := range dockNames {
				if dock := m.FindDock(dockName); dock != nil {
					wsCount += len(dock.Workspaces)
				}
			}
			fmt.Printf("  active worktrees: %d\n", wsCount)

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

			var repoName string
			if len(args) > 0 {
				repoName = args[0]
			} else {
				// Infer from CWD.
				cwd, cwdErr := os.Getwd()
				if cwdErr != nil {
					return fmt.Errorf("cannot determine working directory")
				}
				root, rootErr := eng.Git.RepoRoot(cwd)
				if rootErr != nil {
					return fmt.Errorf("not in a git repository")
				}
				// Find repo by path. Canonicalize both sides so a stored
				// repo.Path that contains a symlink (e.g. /tmp on macOS)
				// still matches what git reports as the repo root.
				rootCanonical := config.CanonicalPath(root)
				repos, _ := eng.RepoList()
				for _, repo := range repos {
					if config.CanonicalPath(repo.Path) == rootCanonical {
						repoName = repo.Name
						break
					}
				}
				if repoName == "" {
					return fmt.Errorf("current repo not in bay config; use bay repo add first")
				}
			}

			if err := eng.RepoInit(repoName); err != nil {
				return err
			}
			fmt.Printf("Repo %q initialized for bay.\n", repoName)
			return nil
		},
	}
}
