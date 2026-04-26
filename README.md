# Bay

Multi-session workspace management across tmux and git worktrees.

Bay manages concurrent workspaces — each with its own git worktree, tmux window, and shell. Run multiple PRs in parallel, open editors and agents alongside each other, and recover everything after a reboot.

For developers who work on multiple branches simultaneously and use tmux as their terminal multiplexer.

```
cd ~/projects/myproject
bay ws new                      # auto-bootstrap repo/dock + create worktree
bay ws new --agent              # new worktree with the dock's default agent
bay edit                        # open editor for current workspace
bay shell                       # split a shell into the current workspace
bay go                          # fuzzy-pick a surface in the current workspace
bay ws go                       # fuzzy-pick a workspace in the current dock
bay recover                     # reconstruct everything after reboot
```

## What it does

- **Worktree lifecycle** — create, close, rename workspaces backed by git worktrees or external directories. Safety checks on close (dirty files, unlanded commits). Each workspace is an isolated checkout.
- **Surface model** — each workspace contains one or more **surfaces** (agent panes, shell panes, command panes, GUI editors). Bay tracks them all and recovers them after reboot.
- **Editor integration** — `bay edit` opens your workspace in cursor, VS Code, zed, nvim, or vim. `bay edit --all` for multi-root.
- **Agent support** — optionally launch AI agents (Claude Code, Codex, Gemini). Agent surfaces resume on restart via configured `resume_args`. Closing an agent surface prompts for confirmation.
- **Scoped navigation** — `bay go` (Option+j/k) cycles surfaces within the current workspace; `bay ws go` (Option+Shift+J/K) cycles workspaces within the current dock. Cycling flashes a brief position indicator.
- **Command palette** — `Option+p` opens a VS Code-style palette in a tmux popup: fuzzy-search every bay command, see its hotkey if it has one, and launch without leaving the keyboard.
- **Hierarchical browsing** — `bay ls` shows the dock/workspace structure scoped to your current focus; `bay tree` (or `bay ls -R`) shows the full hierarchy including surfaces.
- **Status line** — `bay status-line <field>` provides workspace info for tmux status bar composition.
- **Shell completion** — tab-complete workspace names, dock names, and flag values in bash, zsh, and fish.

## Install

This is a private repository. Build from source:

```
git clone git@github.com:commontoolsinc/bay.git
cd bay
go install ./cmd/bay
```

Then run `bay setup` to create your config and install shell completions.

## Documentation

- **[Tutorial](docs/tutorial.md)** — hands-on walkthrough from install to cleanup
- **[Human Guide](docs/human-guide.md)** — configuration, commands, and daily workflow
- **[Agent Reference](docs/agent-reference.md)** — comprehensive reference for AI agents operating inside bay workspaces
