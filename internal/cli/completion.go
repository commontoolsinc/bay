package cli

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

// RegisterCompletion adds the completion subcommand and registers all
// dynamic completion functions for arguments and flags.
func RegisterCompletion(root *cobra.Command) {
	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for bay.

Add to your .zshrc or .bashrc:

  if command -v bay > /dev/null ; then
    source <(bay completion zsh)    # or bash
  fi

For fish (~/.config/fish/config.fish):

  if command -v bay > /dev/null
    bay completion fish | source
  end
`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			default:
				return cmd.Help()
			}
		},
	}

	completionCmd.GroupID = "other"
	root.AddCommand(completionCmd)

	// Register dynamic completions for all subcommands.
	// We look up commands by path from the root so this stays decoupled
	// from the individual command constructors.
	registerCompletions(root)
}

// findCmd walks the command tree to find a subcommand by space-separated path.
func findCmd(root *cobra.Command, path string) *cobra.Command {
	parts := strings.Fields(path)
	cmd := root
	for _, p := range parts {
		found := false
		for _, c := range cmd.Commands() {
			if c.Name() == p {
				cmd = c
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return cmd
}

func registerCompletions(root *cobra.Command) {
	wsCompl := workspaceCompletions()
	dockCompl := dockCompletions()
	goCompl := goCompletions()

	// Workspace commands: ws close, ws show, ws update, ws rename
	for _, path := range []string{"ws close", "ws show", "ws update", "ws rename"} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.ValidArgsFunction = wsCompl
		}
	}

	// Window commands: win open, win close, win restart
	for _, path := range []string{"win open", "win close", "win restart"} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.ValidArgsFunction = wsCompl
		}
	}

	// Dock commands: dock close, dock recover
	for _, path := range []string{"dock close", "dock recover"} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.ValidArgsFunction = dockCompl
		}
	}

	// Repo commands: repo remove
	if cmd := findCmd(root, "repo remove"); cmd != nil {
		cmd.ValidArgsFunction = repoCompletionsFunc()
	}

	// bay go
	if cmd := findCmd(root, "go"); cmd != nil {
		cmd.ValidArgsFunction = goCompl
	}

	// bay ws new [dock] — first arg is dock name
	if cmd := findCmd(root, "ws new"); cmd != nil {
		cmd.ValidArgsFunction = dockCompl
	}

	// Flag completions
	if cmd := findCmd(root, "ws new"); cmd != nil {
		cmd.RegisterFlagCompletionFunc("agent", agentCompletions)
		cmd.RegisterFlagCompletionFunc("repo", repoCompletions)
	}
	if cmd := findCmd(root, "ws update"); cmd != nil {
		cmd.RegisterFlagCompletionFunc("status", statusCompletions)
	}
	if cmd := findCmd(root, "win open"); cmd != nil {
		cmd.RegisterFlagCompletionFunc("agent", agentCompletions)
	}
	if cmd := findCmd(root, "pane add"); cmd != nil {
		cmd.RegisterFlagCompletionFunc("agent", agentCompletions)
		cmd.RegisterFlagCompletionFunc("split", splitCompletions)
	}
	if cmd := findCmd(root, "dock new"); cmd != nil {
		cmd.RegisterFlagCompletionFunc("agent", agentCompletions)
		cmd.RegisterFlagCompletionFunc("repo", repoCompletions)
	}
}

// loadManifestForCompletions loads the manifest silently (errors return nil).
func loadManifestForCompletions() *manifest.Manifest {
	p := bayPaths()
	m, err := manifest.Load(p.ManifestFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return manifest.New()
		}
		return nil
	}
	return m
}

// loadConfigForCompletions loads the config silently.
func loadConfigForCompletions() *config.Config {
	p := bayPaths()
	cfg, err := config.Load(p.ConfigFile)
	if err != nil {
		return nil
	}
	return cfg
}

// workspaceCompletions returns completions for workspace identifiers.
// Includes: IDs, display names, dock:id, branch names, PR numbers, and "self".
func workspaceCompletions() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		m := loadManifestForCompletions()
		if m == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		seen := make(map[string]bool)
		var completions []string
		add := func(val, desc string) {
			if val == "" || seen[val] {
				return
			}
			seen[val] = true
			if desc != "" {
				completions = append(completions, val+"\t"+desc)
			} else {
				completions = append(completions, val)
			}
		}

		for _, ref := range manifest.AllWorkspaces(m) {
			ws := ref.Workspace
			desc := ref.Dock + ":" + ref.ID
			if ws.Branch != "" {
				desc += " " + ws.Branch
			}
			if ws.PR != "" {
				desc += " #" + ws.PR
			}

			// Only offer values that ResolveWorkspace can actually resolve:
			// bare IDs (wN), qualified IDs (dock:wN), and display names.
			// Branch/PR completions are only in goCompletions (fuzzy match).
			add(ref.ID, desc)
			add(ref.Dock+":"+ref.ID, ws.Name)
			if ws.Name != "" && ws.Name != ref.ID {
				add(ws.Name, desc)
			}
		}

		add("self", "current workspace")
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// dockCompletions returns dock names with descriptions.
func dockCompletions() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		cfg := loadConfigForCompletions()
		if cfg == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var completions []string
		for name, dock := range cfg.Docks {
			var parts []string
			if dock.Repo != "" {
				parts = append(parts, "repo="+dock.Repo)
			}
			if dock.Agent != "" {
				parts = append(parts, "agent="+dock.Agent)
			}
			desc := strings.Join(parts, " ")
			if desc != "" {
				completions = append(completions, name+"\t"+desc)
			} else {
				completions = append(completions, name)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// goCompletions returns workspace names, branches, PR numbers for bay go.
func goCompletions() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		m := loadManifestForCompletions()
		if m == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		seen := make(map[string]bool)
		var completions []string
		add := func(val, desc string) {
			if val == "" || seen[val] {
				return
			}
			seen[val] = true
			if desc != "" {
				completions = append(completions, val+"\t"+desc)
			} else {
				completions = append(completions, val)
			}
		}

		for _, ref := range manifest.AllWorkspaces(m) {
			ws := ref.Workspace
			add(ws.Name, ref.Dock+" "+string(ws.Status))
			add(ref.ID, ref.Dock+" "+ws.Name)
			add(ref.Dock, "dock")
			if ws.Branch != "" {
				add(ws.Branch, ref.Dock+" "+ws.Name)
			}
			if ws.PR != "" {
				add("#"+ws.PR, ref.Dock+" "+ws.Name)
			}
		}

		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// agentCompletions returns agent type names from config.
func agentCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg := loadConfigForCompletions()
	if cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var completions []string
	for name, agent := range cfg.Agents {
		completions = append(completions, name+"\t"+agent.Command)
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// repoCompletionsFunc returns a ValidArgsFunction for repo name positional args.
func repoCompletionsFunc() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return repoCompletions(cmd, args, toComplete)
	}
}

// repoCompletions returns repo names from config.
func repoCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg := loadConfigForCompletions()
	if cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var completions []string
	for name, repo := range cfg.Repos {
		completions = append(completions, name+"\t"+repo.Path)
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// statusCompletions returns valid workspace status values.
func statusCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"idle\tfresh workspace, no work assigned",
		"active\twork in progress",
		"done\twork complete, ready to close",
	}, cobra.ShellCompDirectiveNoFileComp
}

// splitCompletions returns valid pane split directions.
func splitCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"h\thorizontal",
		"v\tvertical",
	}, cobra.ShellCompDirectiveNoFileComp
}
