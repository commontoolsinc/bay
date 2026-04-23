package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/nav"
	"github.com/commontoolsinc/bay/internal/picker"
	"github.com/spf13/cobra"
)

func newWsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Manage workspaces",
	}

	cmd.AddCommand(
		newWsNewCmd(),
		newWsCloseCmd(),
		newWsShowCmd(),
		newWsRenameCmd(),
		newWsDescribeCmd(),
		newWsLsCmd(),
		newWsTreeCmd(),
		newWsGoCmd(),
		newWsNextCmd(),
		newWsPrevCmd(),
	)

	return cmd
}

func newWsNewCmd() *cobra.Command {
	var opts engine.WsNewOptions
	var shell bool
	var quiet bool
	var dockFlag string

	cmd := &cobra.Command{
		Use:   "new [name]",
		Short: "Create a new workspace",
		Long: `Create a new workspace in a dock.

  bay ws new                          create in current dock, auto-named
  bay ws new login-bug                create with display name "login-bug"
  bay ws new --branch fix/login-bug   checkout or create a git branch
  bay ws new --dock labs              create in a specific dock`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			// Positional is the workspace's display name.
			if len(args) > 0 {
				opts.Name = args[0]
			}

			// Resolve dock: explicit --dock > current tmux session > auto-bootstrap.
			// Check $TMUX (not $TMUX_PANE) because tmux run-shell doesn't
			// set TMUX_PANE. The FindDock check below ensures we only use
			// the session if it's actually a bay dock.
			opts.Dock = dockFlag
			if opts.Dock == "" && os.Getenv("TMUX") != "" {
				dock, tmuxErr := eng.Tmux.CurrentSession()
				if tmuxErr == nil {
					m, _ := eng.LoadManifest()
					if m != nil && m.FindDock(dock) != nil {
						opts.Dock = dock
					}
				}
			}
			if opts.Dock == "" {
				dockName, bootstrapErr := autoBootstrap(eng, quiet)
				if bootstrapErr != nil {
					return bootstrapErr
				}
				opts.Dock = dockName
			}

			// Shell-first default: if neither --agent nor --shell was
			// explicitly passed, default to shell mode.
			if !cmd.Flags().Changed("agent") && !cmd.Flags().Changed("shell") {
				opts.Shell = true
			} else {
				opts.Shell = shell
			}

			// Bare --agent: use dock's default agent.
			// --agent <name>: use that specific agent.
			opts.RequireAgent = cmd.Flags().Changed("agent")
			if opts.Agent == "default" {
				opts.Agent = "" // resolve to dock/global default
			}

			ws, err := eng.WsNew(opts)
			if err != nil {
				return err
			}
			if !quiet {
				fmt.Printf("Created %s:%s\n", opts.Dock, ws.Name)
				currentSession, tmuxErr := eng.Tmux.CurrentSession()
				if tmuxErr != nil || os.Getenv("TMUX") == "" {
					fmt.Printf("\nAttach with:\n  tmux attach -t %s\n", opts.Dock)
				} else if currentSession != opts.Dock {
					fmt.Printf("\nSwitch with:\n  tmux switch-client -t %s\n", opts.Dock)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (defaults to current tmux session)")
	cmd.Flags().StringVar(&opts.Repo, "repo", "", "repo name")
	cmd.Flags().StringVar(&opts.Dir, "dir", "", "external directory (creates external workspace)")
	cmd.Flags().StringVar(&opts.Agent, "agent", "", "agent type (bare --agent uses dock default)")
	cmd.Flags().BoolVar(&shell, "shell", false, "open shell instead of agent")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "git branch to checkout (creates it if new)")
	cmd.Flags().StringVar(&opts.Description, "description", "", "short description shown in picker/ls/tree (~40 chars)")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress output")
	cmd.Flags().Lookup("agent").NoOptDefVal = "default"

	return cmd
}

func newWsCloseCmd() *cobra.Command {
	var force, clean, done, dryRun bool
	var dockFlag string

	cmd := &cobra.Command{
		Use:     "close [name]",
		Aliases: []string{"rm"},
		Short:   "Close a workspace and all its surfaces",
		Long: `Close a workspace and all its windows. Pass "self" to close the current
workspace, or use --done/--clean to batch-close workspaces.

  bay ws close w1               close a specific workspace
  bay ws close labs:w1          dock-qualified
  bay ws close w1 --dock labs   same, with flag
  bay ws close self             close the current workspace
  bay ws close --done           close workspaces that are not dirty or pending
  bay ws close --clean          close all non-dirty workspaces
  bay ws close --done --dry-run preview what --done would close`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			if clean || done {
				dockName := dockFlag
				if dockName == "" {
					sess, tmuxErr := eng.Tmux.CurrentSession()
					if tmuxErr == nil {
						m, _ := eng.LoadManifest()
						if m != nil && m.FindDock(sess) != nil {
							dockName = sess
						}
					}
				}

				// Exclude the current workspace so batch close doesn't kill
				// the session the user is running from.
				var exclude []string
				if ctx, ctxErr := eng.CurrentContext(); ctxErr == nil && ctx.Workspace != "" {
					exclude = append(exclude, ctx.Workspace)
				}

				var closed, skipped []string
				var closeErr error
				if done {
					closed, skipped, closeErr = eng.WsCloseDone(dockName, force, dryRun, exclude...)
				} else {
					closed, skipped, closeErr = eng.WsCloseClean(dockName, force, dryRun, exclude...)
				}

				verb := "Closed"
				if dryRun {
					verb = "Would close"
				}
				for _, c := range closed {
					fmt.Printf("%s %s\n", verb, c)
				}
				for _, s := range skipped {
					fmt.Printf("Skipped %s\n", s)
				}
				if len(skipped) > 0 && !force && !dryRun {
					fmt.Println("\nUse --force to close dirty workspaces.")
				}
				return closeErr
			}

			if len(args) == 0 {
				return fmt.Errorf("specify a workspace to close (bay ws close <name>), or use --done / --clean")
			}

			dockName, wsID, err := resolveWsArg(eng, args[0], dockFlag)
			if err != nil {
				return err
			}

			return eng.WsClose(dockName, wsID, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "force close even if dirty")
	cmd.Flags().BoolVar(&done, "done", false, "close workspaces that are not dirty or pending")
	cmd.Flags().BoolVar(&clean, "clean", false, "close all non-dirty workspaces")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview what --done/--clean would close")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (disambiguates a bare workspace name)")

	return cmd
}

func newWsShowCmd() *cobra.Command {
	var jsonOutput, short, plain, flash bool
	var dockFlag string

	cmd := &cobra.Command{
		Use:     "show [name]",
		Aliases: []string{"cat"},
		Short:   "Show workspace details (default: current)",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			target := "self"
			if len(args) > 0 {
				target = args[0]
			}
			dockName, wsID, err := resolveWsArg(eng, target, dockFlag)
			if err != nil {
				if short || flash {
					// Swallow the error so `M-?` outside a workspace
					// leaves the status bar clean instead of flashing a
					// traceback. Non-short paths still surface the error.
					return nil
				}
				return err
			}

			if short || flash {
				ws, wsErr := eng.WsShow(dockName, wsID)
				if wsErr != nil {
					return nil
				}
				out := formatWorkspaceShort(ws)
				if flash {
					// Route via tmux display-message so the status bar
					// updates synchronously on keypress. Using tmux's
					// `#(cmd)` format cache (the earlier approach)
					// meant the first keypress showed empty and later
					// keypresses showed stale cached output.
					return eng.Tmux.DisplayMessage(stripANSI(out), 5000)
				}
				if plain {
					out = stripANSI(out)
				}
				fmt.Println(out)
				return nil
			}

			eng.SyncAll()
			wsInfo, err := eng.WorkspaceInfoByName(dockName, wsID)
			if err != nil {
				return err
			}
			repoName := ""
			ws, wsErr := eng.WsShow(dockName, wsID)
			if wsErr == nil && ws.Worktree != nil {
				repoName = ws.Worktree.Repo
			}
			if repoName == "" {
				m, _ := eng.LoadManifest()
				if m != nil {
					if dock := m.FindDock(dockName); dock != nil {
						if wsInfo.DefaultAgent == "" {
							wsInfo.DefaultAgent = dock.Agent
						}
						if repoName == "" {
							repoName = dock.Repo
						}
					}
				}
			}

			if jsonOutput {
				out := map[string]interface{}{
					"name":          wsInfo.Name,
					"description":   wsInfo.Description,
					"repo":          repoName,
					"dock":          dockName,
					"type":          wsInfo.Type,
					"path":          wsInfo.Path,
					"branch":        wsInfo.Branch,
					"pr":            wsInfo.PR,
					"dirty":         wsInfo.Dirty,
					"pending":       wsInfo.Pending,
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
	cmd.Flags().BoolVarP(&short, "short", "s", false, "one-line workspace summary (name — description — branch — #PR)")
	cmd.Flags().BoolVar(&plain, "plain", false, "plain-text output (no ANSI colors); useful with --short for tmux display-message")
	cmd.Flags().BoolVar(&flash, "flash", false, "send the short summary to tmux display-message for 5s (used by the M-? binding)")
	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (disambiguates a bare workspace name)")

	return cmd
}

// formatWorkspaceShort builds a one-line workspace summary:
// `name — description — branch — #PR`, skipping empty fields.
// The branch is omitted when it equals the workspace name — bay
// auto-derives workspace names from branches, so in the common case
// they match and showing both just duplicates the identifier.
// Used by `bay ws show --short` (and the M-? flash binding).
func formatWorkspaceShort(ws *manifest.Workspace) string {
	if ws == nil {
		return ""
	}
	parts := []string{ws.Name}
	if ws.Description != "" {
		parts = append(parts, ws.Description)
	}
	if ws.Worktree != nil {
		if ws.Worktree.Branch != "" && ws.Worktree.Branch != ws.Name {
			parts = append(parts, ws.Worktree.Branch)
		}
		if ws.Worktree.PR != "" {
			parts = append(parts, "#"+ws.Worktree.PR)
		}
	}
	return strings.Join(parts, dim(" — "))
}

func newWsRenameCmd() *cobra.Command {
	var dockFlag string

	cmd := &cobra.Command{
		Use:     "rename [name] <new-name>",
		Aliases: []string{"mv"},
		Short:   "Rename a workspace (defaults to current)",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			var source, newName string
			if len(args) == 1 {
				source = "self"
				newName = args[0]
			} else {
				source = args[0]
				newName = args[1]
			}

			dockName, wsID, err := resolveWsArg(eng, source, dockFlag)
			if err != nil {
				return err
			}

			return eng.WsRename(dockName, wsID, newName)
		},
	}

	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (disambiguates a bare workspace name)")

	return cmd
}

func newWsDescribeCmd() *cobra.Command {
	var dockFlag string
	var clear bool

	cmd := &cobra.Command{
		Use:   "describe [name] [<description>]",
		Short: "Set a workspace description (defaults to current)",
		Long: `Set a short, free-form description for a workspace. Descriptions appear
in the workspace picker, bay ls, and bay tree; they do not affect tmux
tab names. Target around 40 characters (hard cap 80).

  bay ws describe "Login flow fixes"         set current workspace's description
  bay ws describe auth-fix "Login fixes"     set by workspace name
  bay ws describe self ""                    clear current workspace's description
  bay ws describe --clear                    clear current workspace's description`,
		Args: cobra.RangeArgs(0, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDescribe(args, dockFlag, clear)
		},
	}

	cmd.Flags().StringVar(&dockFlag, "dock", "", "dock name (disambiguates a bare workspace name)")
	cmd.Flags().BoolVar(&clear, "clear", false, "clear the description")

	return cmd
}

// runDescribe is shared between `bay ws describe` and the top-level `bay
// describe`. Unlike `bay rename` (which has distinct tab-rename behavior on
// the top-level variant), describe is a direct pass-through to the workspace
// command, so the two cobra wrappers only differ in Use/Short/Long strings.
func runDescribe(args []string, dockFlag string, clear bool) error {
	eng, err := newEngine()
	if err != nil {
		return err
	}

	var target, desc string
	switch len(args) {
	case 0:
		if !clear {
			return fmt.Errorf("specify a description, or pass --clear")
		}
		target = "self"
	case 1:
		target = "self"
		desc = args[0]
	case 2:
		target = args[0]
		desc = args[1]
	}
	if clear {
		desc = ""
	}

	dockName, wsID, err := resolveWsArg(eng, target, dockFlag)
	if err != nil {
		return err
	}
	return eng.WsDescribe(dockName, wsID, desc)
}

// autoBootstrap detects the CWD git repo, creates a dock and repo in the manifest,
// and saves. Returns the dock name to use for ws new.
func autoBootstrap(eng *engine.Engine, quiet bool) (string, error) {
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

	// Add repo to manifest if not already present.
	m, _ := eng.LoadManifest()
	if m == nil || m.FindRepo(repoName) == nil {
		if addErr := eng.RepoAdd(repoName, repoRoot, "", "", true); addErr != nil {
			// Ignore duplicate errors
			if m != nil && m.FindRepo(repoName) == nil {
				return "", addErr
			}
		}
	}

	// Probe for an agent on PATH and set as global default if unset.
	agentName := probeAgent()
	if agentName != "" && eng.Config.DefaultAgent == "" {
		eng.Config.DefaultAgent = agentName
		_ = eng.SaveConfig()
	}

	// Create dock if not already present in manifest.
	m, _ = eng.LoadManifest()
	if m == nil || m.FindDock(dockName) == nil {
		if err := eng.DockNew(dockName, repoName, "", ""); err != nil {
			return "", err
		}
	}

	if !quiet {
		fmt.Printf("Auto-configured: repo %s, dock %s", repoName, dockName)
		if agentName != "" {
			fmt.Printf(", agent %s", agentName)
		}
		fmt.Println()
		fmt.Println("Run `bay setup` to install keybindings and shell completions.")
	}

	return dockName, nil
}

// probeAgent scans PATH for known AI agent commands.
func probeAgent() string {
	return config.ProbeAgent()
}

// resolveCurrentDock resolves the dock for listing commands. Tries:
// 1. Current tmux session (if it's a bay dock)
// 2. CWD → git repo → matching dock in the manifest
// Returns the dock name and repo name, or an error if neither works.
func resolveCurrentDock(eng *engine.Engine) (dockName, repoName string, err error) {
	m, _ := eng.LoadManifest()

	// Try: current tmux session (only when inside tmux).
	if os.Getenv("TMUX") != "" {
		if sess, tmuxErr := eng.Tmux.CurrentSession(); tmuxErr == nil && m != nil && m.FindDock(sess) != nil {
			dock := m.FindDock(sess)
			return sess, dock.Repo, nil
		}
	}

	// Fallback: CWD → git repo → matching dock.
	if m != nil {
		cwd, cwdErr := os.Getwd()
		if cwdErr == nil {
			if repoRoot, rootErr := eng.Git.RepoRoot(cwd); rootErr == nil {
				repoBase := filepath.Base(repoRoot)
				for i := range m.Docks {
					d := &m.Docks[i]
					if d.Repo == repoBase {
						return d.Name, d.Repo, nil
					}
				}
			}
		}
	}

	return "", "", fmt.Errorf("cannot determine current dock — not in a tmux session or a known git repo")
}

// resolveTarget resolves "self" or a workspace query. Bare names
// prefer the current dock for disambiguation (see resolveBareWs).
func resolveTarget(eng *engine.Engine, target string) (string, string, error) {
	if target == "self" {
		return eng.ResolveSelf()
	}
	if dock, ws, err := parseWsArg(target); err == nil && dock == "" {
		return resolveBareWs(eng, ws)
	}
	return eng.ResolveWorkspace(target)
}

func newWsLsCmd() *cobra.Command {
	var shortOutput bool

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List workspaces in the current dock",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, repo, err := resolveCurrentDock(eng)
			if err != nil {
				return err
			}

			docks, err := eng.List()
			if err != nil {
				return err
			}

			view := BuildListView(docks, ListViewOptions{
				Focus:     ListFocus{Kind: FocusDock, Repo: repo, Dock: dockName},
				Recursive: false,
			})
			view.SetCurrentContext(eng)

			fmt.Print(FormatListView(view, false, shortOutput))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&shortOutput, "short", "s", false, "compact output without labels or key names")

	return cmd
}

func newWsTreeCmd() *cobra.Command {
	var longOutput bool
	var shortOutput bool

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Tree view of workspaces and surfaces in the current dock",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}

			dockName, repo, err := resolveCurrentDock(eng)
			if err != nil {
				return err
			}

			docks, err := eng.List()
			if err != nil {
				return err
			}

			view := BuildListView(docks, ListViewOptions{
				Focus:     ListFocus{Kind: FocusDock, Repo: repo, Dock: dockName},
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

func newWsGoCmd() *cobra.Command {
	var waiting, nextWaiting, pick bool

	cmd := &cobra.Command{
		Use:   "go [query]",
		Short: "Fuzzy find and switch to a workspace in the current dock",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return err
			}
			if pick {
				return wsGoPick(eng)
			}
			return wsGo(eng, args, waiting, nextWaiting)
		},
	}

	cmd.Flags().BoolVar(&waiting, "waiting", false, "filter to waiting workspaces")
	cmd.Flags().BoolVar(&nextWaiting, "next-waiting", false, "jump to next waiting workspace")
	cmd.Flags().BoolVar(&pick, "pick", false, "open picker in a popup (used by keybindings)")
	cmd.Flags().MarkHidden("pick")

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

// wsGoPick checks if there are enough workspaces to pick from, then
// opens a tmux display-popup with "bay ws go". Avoids flashing an
// empty popup when there's nothing to pick.
func wsGoPick(eng *engine.Engine) error {
	currentSession, err := eng.Tmux.CurrentSession()
	if err != nil {
		return nil
	}
	m, err := eng.LoadManifest()
	if err != nil {
		return nil
	}
	entries := nav.CollectEntries(m, eng.Tmux)
	count := 0
	for _, e := range entries {
		if e.DockName == currentSession && e.TmuxWindowID != "" {
			count++
		}
	}
	if count < 2 {
		return nil
	}
	return eng.Tmux.DisplayPopup("bay ws go")
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

	// Filter to current dock, excluding workspaces with no tmux presence.
	var dockEntries []nav.Entry
	for _, e := range entries {
		if e.DockName == currentSession && e.TmuxWindowID != "" {
			dockEntries = append(dockEntries, e)
		}
	}
	entries = dockEntries

	if nextWaiting {
		currentWinID, _ := eng.Tmux.CurrentWindowID()
		entry, _ := nav.NextWaiting(entries, currentWinID)
		if entry == nil {
			return nil
		}
		if err := eng.Tmux.SelectWindow(entry.TmuxWindowID); err != nil {
			return err
		}
		return nil
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

	currentWinID, _ := eng.Tmux.CurrentWindowID()

	switch len(entries) {
	case 0:
		return nil
	case 1:
		if entries[0].TmuxWindowID == currentWinID {
			return nil // already here
		}
		return eng.Tmux.SelectWindow(entries[0].TmuxWindowID)
	default:
		return pickWorkspace(eng, entries)
	}
}

// wsCycle moves to next/prev workspace in the current dock and flashes the
// new position via the cycling indicator.
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
		if e.DockName == currentSession && e.TmuxWindowID != "" {
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
	currentWinID, _ := eng.Tmux.CurrentWindowID()
	currentIdx := 0
	maxWs, maxDesc, maxBranch, maxPR := 0, 0, 0, 0
	descs := make([]string, len(entries))
	for i, e := range entries {
		descs[i] = engine.TruncateName(e.Description, pickerDescMaxLen)
		if len(e.WsName) > maxWs {
			maxWs = len(e.WsName)
		}
		if len(descs[i]) > maxDesc {
			maxDesc = len(descs[i])
		}
		if len(e.Branch) > maxBranch {
			maxBranch = len(e.Branch)
		}
		pr := ""
		if e.PR != "" {
			pr = "#" + e.PR
		}
		if len(pr) > maxPR {
			maxPR = len(pr)
		}
		if e.TmuxWindowID == currentWinID {
			currentIdx = i
		}
	}
	items := make([]picker.Item, len(entries))
	for i, e := range entries {
		pr := ""
		if e.PR != "" {
			pr = "#" + e.PR
		}
		tags := ""
		if e.Pending {
			tags += "  PENDING"
		}
		if e.Waiting {
			tags += "  WAITING"
		}
		if maxDesc > 0 {
			s := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s%s",
				maxWs, e.WsName, maxDesc, descs[i], maxBranch, e.Branch, maxPR, pr, tags)
			items[i] = picker.Item{Display: s, Value: i}
			continue
		}
		s := fmt.Sprintf("%-*s  %-*s  %-*s%s",
			maxWs, e.WsName, maxBranch, e.Branch, maxPR, pr, tags)
		items[i] = picker.Item{Display: s, Value: i}
	}

	selected, err := defaultPicker.Pick(items, picker.Options{Prompt: "workspace> ", Selected: currentIdx})
	if err != nil || selected < 0 {
		return nil
	}
	return eng.Tmux.SelectWindow(entries[selected].TmuxWindowID)
}

// pickerDescMaxLen caps description width in the workspace picker. Larger than
// tab names (where space is scarce) but still bounded to keep the picker
// scannable.
const pickerDescMaxLen = 40
