package cli

import (
	"fmt"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/spf13/cobra"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage repos",
	}

	cmd.AddCommand(
		newRepoAddCmd(),
		newRepoLsCmd(),
		newRepoRemoveCmd(),
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

			// Check gitignore for agent config files
			printGitignoreAdvice(eng, args[0])

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
			if len(eng.Config.Repos) == 0 {
				fmt.Println("No repos configured.")
				return nil
			}
			fmt.Print(FormatRepoTree(eng.Config))
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
			if err := eng.RepoRemove(args[0], force); err != nil {
				if inUse, ok := err.(*engine.RepoInUseError); ok {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"Cannot remove repo %q. Use --force to also remove:\n%s",
						args[0],
						FormatSubtreeForRemoval(eng.Config, inUse.RepoName, inUse.AffectedDocks))
					return fmt.Errorf("repo %q is in use", args[0])
				}
				return err
			}
			fmt.Printf("Repo %q removed.\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "also close and remove docks that use this repo")

	return cmd
}

// printGitignoreAdvice checks if a repo's .gitignore covers agent config files
// and advises the user about any missing entries.
func printGitignoreAdvice(eng *engine.Engine, repoName string) {
	repoCfg, ok := eng.Config.Repos[repoName]
	if !ok {
		return
	}
	repoPath := config.ExpandPath(repoCfg.Path)

	var missing []string
	for agentName, agent := range eng.Config.Agents {
		if agent.ConfigFile == "" {
			continue
		}
		ignored, err := eng.Git.IsIgnored(repoPath, agent.ConfigFile)
		if err != nil || !ignored {
			missing = append(missing, fmt.Sprintf("  echo %q >> %s/.gitignore  # for %s",
				agent.ConfigFile, repoPath, agentName))
		}
	}

	if len(missing) == 0 {
		return
	}

	fmt.Println("\nTo use agents with this repo, add to .gitignore:")
	for _, m := range missing {
		fmt.Println(m)
	}
}
