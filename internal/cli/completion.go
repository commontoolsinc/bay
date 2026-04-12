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
		// Sourced from shell rc on every new shell — never fork the
		// monitor from this path.
		Annotations: map[string]string{noMonitorAutostartAnnotation: "true"},
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
	sfCompl := surfaceCompletions()
	dockCompl := dockCompletions()
	goCompl := goCompletions()
	wsFlagCompl := workspaceFlagCompletions()
	dockFlagCompl := dockFlagCompletions()

	// Workspace-target positional: ws verbs operate on workspaces.
	// surface new / shell / ws new select their target via
	// --ws/--dock flags, not a positional, so they're not in this list.
	for _, path := range []string{
		"workspace close", "workspace show", "workspace rename",
		"edit",
		"new edit",
		"rename",
	} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.ValidArgsFunction = wsCompl
		}
	}

	// Surface-name positional: sf verbs and the top-level surface verbs.
	for _, path := range []string{
		"surface close", "surface restart", "surface show", "surface rename",
		"close", "show", "restart",
	} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.ValidArgsFunction = sfCompl
		}
	}

	// bay new agent <agent> — first positional is the agent type.
	if cmd := findCmd(root, "new agent"); cmd != nil {
		cmd.ValidArgsFunction = agentArgCompletions
	}

	// Dock commands: dock close, dock recover, dock tree, dock show, dock rename.
	for _, path := range []string{"dock close", "dock recover", "dock tree", "dock show", "dock rename"} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.ValidArgsFunction = dockCompl
		}
	}

	// Repo commands: repo remove, repo show.
	for _, path := range []string{"repo remove", "repo show"} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.ValidArgsFunction = repoCompletionsFunc()
		}
	}

	// bay go (surface-scoped, no completions needed — surfaces are within workspace)
	// bay ws go (workspace-scoped fuzzy match)
	if cmd := findCmd(root, "workspace go"); cmd != nil {
		cmd.ValidArgsFunction = goCompl
	}

	// bay ws new — positional is the new workspace's display name (free
	// text, no completion). Flags carry completions for the things bay
	// can suggest: --dock, --agent, --repo.
	if cmd := findCmd(root, "workspace new"); cmd != nil {
		cmd.RegisterFlagCompletionFunc("dock", dockFlagCompl)
		cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletions)
		cmd.RegisterFlagCompletionFunc("repo", repoCompletions)
	}
	// surface new is now a parent with shell/agent/cmd/edit subcommands
	// (same objects as bay new). Flag completions are registered on those
	// subcommands via "new shell", "new agent", etc. below.
	if cmd := findCmd(root, "dock new"); cmd != nil {
		cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletions)
		cmd.RegisterFlagCompletionFunc("repo", repoCompletions)
	}

	// --ws and --dock flag completions on every surface verb that
	// supports them. Workspace commands get --dock too. The bay new
	// <kind> commands also get --split for symmetry with sf new.
	for _, path := range []string{
		"surface close", "surface restart", "surface show", "surface rename",
		"close", "show", "restart",
		"shell",
		"new shell", "new agent", "new cmd",
	} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.RegisterFlagCompletionFunc("ws", wsFlagCompl)
			cmd.RegisterFlagCompletionFunc("dock", dockFlagCompl)
		}
	}
	for _, path := range []string{"new shell", "new agent", "new cmd"} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.RegisterFlagCompletionFunc("split", splitCompletions)
		}
	}
	for _, path := range []string{"workspace close", "workspace show", "workspace rename", "rename"} {
		if cmd := findCmd(root, path); cmd != nil {
			cmd.RegisterFlagCompletionFunc("dock", dockFlagCompl)
		}
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

// --- Candidate builders ---
//
// These pure functions take manifest/config data and return ready-to-use
// completion strings. The positional and flag completers below wrap them
// with the right args-guard semantics.

// addCandidate is a helper that appends "val\tdesc" to completions, deduping
// by val. Used by all candidate builders.
func addCandidate(completions *[]string, seen map[string]bool, val, desc string) {
	if val == "" || seen[val] {
		return
	}
	seen[val] = true
	if desc != "" {
		*completions = append(*completions, val+"\t"+desc)
	} else {
		*completions = append(*completions, val)
	}
}

// workspaceCandidates returns workspace identifier candidates: each workspace
// in two forms (bare name and dock:name), plus the "self" keyword.
func workspaceCandidates(m *manifest.Manifest) []string {
	seen := make(map[string]bool)
	var completions []string
	for _, ref := range manifest.AllWorkspaces(m) {
		ws := ref.Workspace
		desc := ref.Dock
		if ws.Worktree != nil {
			if ws.Worktree.Branch != "" {
				desc += " " + ws.Worktree.Branch
			}
			if ws.Worktree.PR != "" {
				desc += " #" + ws.Worktree.PR
			}
		}
		addCandidate(&completions, seen, ws.Name, desc)
		addCandidate(&completions, seen, ref.Dock+":"+ws.Name, ws.Name)
	}
	addCandidate(&completions, seen, "self", "current workspace")
	return completions
}

// surfaceCandidates returns surface identifier candidates: each surface in
// two qualified forms (ws:name and dock:ws:name). Bare names are deliberately
// omitted because they collide across workspaces and would suggest a surface
// from one workspace while the resolver picks one from another.
//
// includeSelf controls whether the "self" keyword is appended; callers should
// pass false when the user has already typed a colon (qualified `self` is
// always literal, not a keyword).
func surfaceCandidates(m *manifest.Manifest, includeSelf bool) []string {
	seen := make(map[string]bool)
	var completions []string
	if includeSelf {
		addCandidate(&completions, seen, "self", "current surface")
	}
	for _, ref := range manifest.AllWorkspaces(m) {
		ws := ref.Workspace
		for _, s := range ws.Surfaces {
			desc := string(s.Type) + " in " + ref.Dock + ":" + ws.Name
			addCandidate(&completions, seen, ws.Name+":"+s.Name, desc)
			addCandidate(&completions, seen, ref.Dock+":"+ws.Name+":"+s.Name, desc)
		}
	}
	return completions
}

// dockCandidates returns dock name candidates with repo/agent descriptions.
func dockCandidates(m *manifest.Manifest) []string {
	var completions []string
	for _, dock := range m.Docks {
		var parts []string
		if dock.Repo != "" {
			parts = append(parts, "repo="+dock.Repo)
		}
		if dock.Agent != "" {
			parts = append(parts, "agent="+dock.Agent)
		}
		desc := strings.Join(parts, " ")
		if desc != "" {
			completions = append(completions, dock.Name+"\t"+desc)
		} else {
			completions = append(completions, dock.Name)
		}
	}
	return completions
}

// agentCandidates returns agent type candidates from config.
func agentCandidates(cfg *config.Config) []string {
	var completions []string
	for name, agent := range cfg.Agents {
		completions = append(completions, name+"\t"+agent.Command)
	}
	return completions
}

// --- Positional completers (with len(args) > 0 short-circuit) ---

// workspaceCompletions returns a positional completer for workspace
// identifiers.
func workspaceCompletions() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		m := loadManifestForCompletions()
		if m == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return workspaceCandidates(m), cobra.ShellCompDirectiveNoFileComp
	}
}

// surfaceCompletions returns a positional completer for surface identifiers.
// 'self' is only included when toComplete has no colon, since qualified
// `self` (`w1:self`, `labs:w1:self`) is always literal in the resolver.
func surfaceCompletions() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		m := loadManifestForCompletions()
		if m == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		includeSelf := !strings.Contains(toComplete, ":")
		return surfaceCandidates(m, includeSelf), cobra.ShellCompDirectiveNoFileComp
	}
}

// dockCompletions returns a positional completer for dock names.
func dockCompletions() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		m := loadManifestForCompletions()
		if m == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return dockCandidates(m), cobra.ShellCompDirectiveNoFileComp
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
			add(ref.Dock, "dock")
			if ws.Worktree != nil {
				if ws.Worktree.Branch != "" {
					add(ws.Worktree.Branch, ref.Dock+" "+ws.Name)
				}
				if ws.Worktree.PR != "" {
					add("#"+ws.Worktree.PR, ref.Dock+" "+ws.Name)
				}
			}
		}

		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// --- Flag completers (no args guard, since flag values aren't positionals) ---

// workspaceFlagCompletions returns a flag-value completer for workspace
// identifiers (used for --ws on surface verbs).
func workspaceFlagCompletions() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		m := loadManifestForCompletions()
		if m == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return workspaceCandidates(m), cobra.ShellCompDirectiveNoFileComp
	}
}

// dockFlagCompletions returns a flag-value completer for dock names (used
// for --dock on workspace and surface verbs).
func dockFlagCompletions() func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		m := loadManifestForCompletions()
		if m == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return dockCandidates(m), cobra.ShellCompDirectiveNoFileComp
	}
}

// agentFlagCompletions returns a flag-value completer for agent types (used
// for --agent on ws new, surface new, dock new).
func agentFlagCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg := loadConfigForCompletions()
	if cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return agentCandidates(cfg), cobra.ShellCompDirectiveNoFileComp
}

// agentArgCompletions is the positional version of agentFlagCompletions: it
// returns nothing once an agent has already been provided. Used for
// `bay new agent <agent>`.
func agentArgCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return agentFlagCompletions(cmd, args, toComplete)
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

// repoCompletions returns repo names from manifest.
func repoCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	m := loadManifestForCompletions()
	if m == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var completions []string
	for _, repo := range m.Repos {
		completions = append(completions, repo.Name+"\t"+repo.Path)
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// splitCompletions returns valid pane split directions.
func splitCompletions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"h\thorizontal",
		"v\tvertical",
	}, cobra.ShellCompDirectiveNoFileComp
}
