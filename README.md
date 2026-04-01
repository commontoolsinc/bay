# Bay

Multi-session workspace management across tmux and git worktrees.

Bay manages concurrent workspaces — each with its own git worktree, tmux window, and shell. Run multiple PRs in parallel, open editors and agents alongside each other, and recover everything after a reboot.

For developers who work on multiple branches simultaneously and use tmux as their terminal multiplexer.

```
bay repo add myproject ~/projects/myproject
bay dock new dev --repo myproject
tmux attach -t dev
bay ws new                      # new worktree + shell window
bay ws new --agent claude       # new worktree + agent window
bay edit                        # open editor for current workspace
bay shell                       # split a shell pane
bay go                          # fuzzy-pick any window across docks
bay recover                     # reconstruct everything after reboot
```

## What it does

- **Worktree lifecycle** — create, close, rename workspaces backed by git worktrees or external directories. Safety checks on close (dirty files, unpushed commits). Each workspace is an isolated checkout.
- **Tmux management** — multiple windows and panes per workspace, automatic naming from branch names, placeholder windows to keep sessions alive, and full recovery after reboot.
- **Editor integration** — `bay edit` opens your workspace in cursor, VS Code, zed, nvim, or vim. `bay edit --all` for multi-root.
- **Agent support** — optionally launch AI agents (Claude Code, Codex, Gemini) with auto-injected config. Agents are opt-in per workspace.
- **Navigation** — `bay go` fuzzy-matches workspace names, branches, PR numbers, and window types with fzf.
- **Agent display** — `bay ls` shows the workspace's configured agent: the dock default unless you overrode it at workspace creation.
- **Status line** — `bay status-line` provides workspace info for tmux status bar composition.
- **Shell completion** — tab-complete workspace names, dock names, and flag values in bash, zsh, and fish.

## Install

```
go install github.com/commontoolsinc/bay/cmd/bay@latest
```

Then run `bay setup` to create your config and install shell completions.

## Documentation

- **[Tutorial](docs/tutorial.md)** — hands-on walkthrough from install to cleanup
- **[User Guide](docs/human-guide.md)** — configuration, commands, and daily workflow
- **[Agent Reference](docs/agent-reference.md)** — comprehensive reference for AI agents operating inside bay workspaces
- **[Design Doc](docs/design.md)** — architecture, requirements, and design decisions
