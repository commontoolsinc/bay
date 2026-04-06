package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/nav"
	"github.com/commontoolsinc/bay/internal/picker"
	"github.com/spf13/cobra"
)

func newWsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ws",
		Aliases: []string{"workspace"},
		Short:   "Manage workspaces",
	}

	cmd.AddCommand(
		newWsNewCmd(),
		newWsCloseCmd(),
		newWsShowCmd(),
		newWsUpdateCmd(),
		newWsRenameCmd(),
		newWsGoCmd(),
		newWsNextCmd(),
		newWsPrevCmd(),
	)

	return cmd
}

func newWsNewCmd() *cobra.Command {
	var opts engine.WsNewOptions
	var shell bool

	cmd := &cobra.Command{
		Use:   "new [dock]",
		Short: "Create a new workspace",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			if len(args) > 0 {
				opts.Dock = args[0]
			} else {
				// Try: infer dock from current tmux session.
				dock, tmuxErr := eng.Tmux.CurrentSession()
				if tmuxErr == nil {
					if _, ok := eng.Config.Docks[dock]; ok {
						opts.Dock = dock
					}
				}

				// Fallback: auto-bootstrap from CWD.
				if opts.Dock == "" {
					dockName, bootstrapErr := autoBootstrap(eng)
					if bootstrapErr != nil {
						return bootstrapErr
					}
					opts.Dock = dockName
				}
			}

			// Shell-first default: if neither --agent nor --shell was
			// explicitly passed, default to shell mode.
			if !cmd.Flags().Changed("agent") && !cmd.Flags().Changed("shell") {
				opts.Shell = true
			} else {
				opts.Shell = shell
			}

			// Bare --agent (no value): use dock's default agent.
			// --agent <name>: use that specific agent.
			// Both cases: opts.Agent is already set by the flag binding.

			ws, err := eng.WsNew(opts)
			if err != nil {
				return err
			}
			fmt.Printf("Workspace %s created in dock %s (path: %s)\n",
				ws.Name, opts.Dock, ws.Path)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Repo, "repo", "", "repo name")
	cmd.Flags().StringVar(&opts.Dir, "dir", "", "external directory (creates external workspace)")
	cmd.Flags().StringVar(&opts.Name, "name", "", "display name")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "agent type (bare --agent uses dock default)")
	cmd.Flags().BoolVar(&shell, "shell", false, "open shell instead of agent")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "create and checkout a git branch in the worktree")
	cmd.Flags().Lookup("agent").NoOptDefVal = ""

	return cmd
}

func newWsCloseCmd() *cobra.Command {
	var force, done bool

	cmd := &cobra.Command{
		Use:   "close [name|self]",
		Short: "Close a workspace and all its windows",
		Long: `Close a workspace and all its windows.

  bay ws close w1          close a specific workspace
  bay ws close self        close the current workspace
  bay ws close --done      close all workspaces with status "done"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			if done {
				// Batch close all done workspaces
				dockName := ""
				sess, tmuxErr := eng.Tmux.CurrentSession()
				if tmuxErr == nil {
					if _, ok := eng.Config.Docks[sess]; ok {
						dockName = sess
					}
				}

				closed, skipped, closeErr := eng.WsCloseByStatus(dockName, "done", force)
				for _, c := range closed {
					fmt.Printf("Closed %s\n", c)
				}
				for _, s := range skipped {
					fmt.Printf("Skipped %s\n", s)
				}
				if len(skipped) > 0 && !force {
					fmt.Println("\nUse --force to close dirty workspaces.")
				}
				return closeErr
			}

			if len(args) == 0 {
				return fmt.Errorf("specify a workspace to close (bay ws close <name>) or use --done to close all finished workspaces")
			}

			dockName, wsID, err := resolveTarget(eng, args[0])
			if err != nil {
				return err
			}

			return eng.WsClose(dockName, wsID, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force close even if dirty")
	cmd.Flags().BoolVar(&done, "done", false, "close all workspaces with status done")

	return cmd
}

func newWsShowCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "show [name|self]",
		Short: "Show workspace details (default: current)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			target := "self"
			if len(args) > 0 {
				target = args[0]
			}
			dockName, wsID, err := resolveTarget(eng, target)
			if err != nil {
				return err
			}

			wsInfo, err := eng.WorkspaceInfoByName(dockName, wsID)
			if err != nil {
				return err
			}
			if wsInfo.DefaultAgent == "" {
				wsInfo.DefaultAgent = eng.Config.Docks[dockName].Agent
			}

			repoName := ""
			ws, wsErr := eng.WsShow(dockName, wsID)
			if wsErr == nil && ws.Worktree != nil {
				repoName = ws.Worktree.Repo
			}
			if repoName == "" {
				repoName = eng.Config.Docks[dockName].Repo
			}

			if jsonOutput {
				out := map[string]interface{}{
					"name":          wsInfo.Name,
					"repo":          repoName,
					"dock":          dockName,
					"type":          wsInfo.Type,
					"path":          wsInfo.Path,
					"branch":        wsInfo.Branch,
					"pr":            wsInfo.PR,
					"status":        wsInfo.Status,
					"sync_status":   wsInfo.SyncStatus,
					"default_agent": wsInfo.DefaultAgent,
					"surfaces":      wsInfo.Surfaces,
				}
				data, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
				return nil
			}

			fmt.Print(FormatWorkspaceShow(repoName, dockName, wsInfo, false))
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

func newWsUpdateCmd() *cobra.Command {
	var branch, pr, status string

	cmd := &cobra.Command{
		Use:   "update <name|self>",
		Short: "Update workspace metadata (at least one of --branch, --pr, or --status is required)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("branch") && !cmd.Flags().Changed("pr") && !cmd.Flags().Changed("status") {
				return fmt.Errorf("at least one of --branch, --pr, or --status is required")
			}

			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsID, err := resolveTarget(eng, args[0])
			if err != nil {
				return err
			}

			var bp, pp, sp *string
			if cmd.Flags().Changed("branch") {
				bp = &branch
			}
			if cmd.Flags().Changed("pr") {
				pp = &pr
			}
			if cmd.Flags().Changed("status") {
				sp = &status
			}

			return eng.WsUpdate(dockName, wsID, bp, pp, sp)
		},
	}

	cmd.Flags().StringVar(&branch, "branch", "", "branch name")
	cmd.Flags().StringVar(&pr, "pr", "", "PR number")
	cmd.Flags().StringVar(&status, "status", "", "status (idle|active|done)")

	return cmd
}

func newWsRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <name|self> <new-name>",
		Short: "Rename a workspace display name",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, wsID, err := resolveTarget(eng, args[0])
			if err != nil {
				return err
			}

			return eng.WsRename(dockName, wsID, args[1])
		},
	}
}

// autoBootstrap detects the CWD git repo, creates a dock and repo config,
// and saves the config. Returns the dock name to use for ws new.
func autoBootstrap(eng *engine.Engine) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine working directory")
	}

	repoRoot, err := eng.Git.RepoRoot(cwd)
	if err != nil {
		return "", fmt.Errorf("not in a git repository; specify dock name or run from a git repo")
	}

	repoName := filepath.Base(repoRoot)
	dockName := repoName

	// Add repo if not already configured.
	if _, ok := eng.Config.Repos[repoName]; !ok {
		eng.Config.Repos[repoName] = config.RepoConfig{
			Path: config.NormalizePath(repoRoot),
		}
	}

	// Probe for an agent on PATH.
	agentName := probeAgent()
	if agentName != "" {
		if _, ok := eng.Config.Agents[agentName]; !ok {
			eng.Config.Agents[agentName] = config.AgentConfig{Command: agentName}
		}
	}

	// Create dock if not already configured.
	// DockNew handles adding to Config.Docks and creating the tmux session.
	if _, ok := eng.Config.Docks[dockName]; !ok {
		if err := eng.DockNew(dockName, repoName, agentName, ""); err != nil {
			return "", err
		}
	}

	// Save config for future commands.
	p := bayPaths()
	if err := config.Save(p.ConfigFile, eng.Config); err != nil {
		return "", fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Auto-configured: repo %s, dock %s", repoName, dockName)
	if agentName != "" {
		fmt.Printf(", agent %s", agentName)
	}
	fmt.Println()

	return dockName, nil
}

// probeAgent scans PATH for known AI agent commands.
func probeAgent() string {
	for _, name := range []string{"claude", "codex", "gemini"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}

// resolveTarget resolves "self" or a workspace query.
func resolveTarget(eng *engine.Engine, target string) (string, string, error) {
	if target == "self" {
		return eng.ResolveSelf()
	}
	return eng.ResolveWorkspace(target)
}

func newWsGoCmd() *cobra.Command {
	var waiting, nextWaiting bool

	cmd := &cobra.Command{
		Use:   "go [query]",
		Short: "Fuzzy find and switch to a workspace in the current dock",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return wsGo(eng, args, waiting, nextWaiting)
		},
	}

	cmd.Flags().BoolVar(&waiting, "waiting", false, "filter to waiting workspaces")
	cmd.Flags().BoolVar(&nextWaiting, "next-waiting", false, "jump to next waiting workspace")

	return cmd
}

func newWsNextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Switch to the next workspace in the current dock",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return wsCycle(eng, true)
		},
	}
}

func newWsPrevCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prev",
		Short: "Switch to the previous workspace in the current dock",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			return wsCycle(eng, false)
		},
	}
}

// wsGo implements workspace picker scoped to the current dock.
func wsGo(eng *engine.Engine, args []string, waiting, nextWaiting bool) error {
	currentSession, err := eng.Tmux.CurrentSession()
	if err != nil {
		return fmt.Errorf("bay ws go requires tmux — use bay ls to see workspaces")
	}

	m, err := eng.LoadManifest()
	if err != nil {
		return err
	}

	entries := nav.CollectEntries(m, eng.Tmux)

	// Filter to current dock.
	var dockEntries []nav.Entry
	for _, e := range entries {
		if e.DockName == currentSession {
			dockEntries = append(dockEntries, e)
		}
	}
	entries = dockEntries

	if nextWaiting {
		currentWinID, _ := eng.Tmux.CurrentWindowID()
		entry := nav.NextWaiting(entries, currentWinID)
		if entry == nil {
			fmt.Println("No waiting workspaces in this dock.")
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
		return pickWorkspace(eng, entries)
	}
}

// wsCycle moves to next/prev workspace in the current dock.
func wsCycle(eng *engine.Engine, forward bool) error {
	currentSession, err := eng.Tmux.CurrentSession()
	if err != nil {
		return fmt.Errorf("not in a tmux session")
	}

	m, err := eng.LoadManifest()
	if err != nil {
		return err
	}

	entries := nav.CollectEntries(m, eng.Tmux)

	var dockEntries []nav.Entry
	for _, e := range entries {
		if e.DockName == currentSession {
			dockEntries = append(dockEntries, e)
		}
	}

	if len(dockEntries) < 2 {
		return nil
	}

	currentWinID, _ := eng.Tmux.CurrentWindowID()
	cur := -1
	for i, e := range dockEntries {
		if e.TmuxWindowID == currentWinID {
			cur = i
			break
		}
	}

	var next int
	if forward {
		next = (cur + 1) % len(dockEntries)
	} else {
		if cur == -1 {
			next = len(dockEntries) - 1
		} else {
			next = (cur - 1 + len(dockEntries)) % len(dockEntries)
		}
	}

	return eng.Tmux.SelectWindow(dockEntries[next].TmuxWindowID)
}

// pickWorkspace shows the built-in picker for workspace selection.
func pickWorkspace(eng *engine.Engine, entries []nav.Entry) error {
	items := make([]picker.Item, len(entries))
	for i, e := range entries {
		items[i] = picker.Item{
			Display: nav.FormatEntry(e),
			Value:   i,
		}
	}

	selected, err := picker.Run(items, picker.Options{Prompt: "workspace> "}, os.Stdin, os.Stdout)
	if err != nil || selected < 0 {
		return nil
	}
	return eng.Tmux.SelectWindow(entries[selected].TmuxWindowID)
}
