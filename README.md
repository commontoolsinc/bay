# Bay

Multi-session workspace management for AI coding agents.

Bay manages concurrent workspaces across tmux windows and git worktrees. It handles agent sessions, shell access, and full tmux recovery after reboot — so you can run multiple agents on separate PRs without managing the plumbing by hand.

```
bay ws new labs          # new worktree + agent in a tmux window
bay ws new labs --shell  # same, but with a shell instead
bay win open w1 --shell  # open a second window into the same workspace
bay go                   # fuzzy-pick any workspace across all docks
bay recover              # reconstruct everything after reboot
```

## What it does

- **Workspace lifecycle** — create, close, rename workspaces backed by git worktrees or external directories. Safety checks on close (dirty files, unpushed commits).
- **Agent config injection** — generates gitignored config files from templates, with per-workspace metadata. Works with Claude Code, Codex, or any terminal agent.
- **Tmux management** — multiple windows and panes per workspace, automatic naming from branch names, and full recovery after reboot from a single command.
- **Pane monitoring** — background process detects when agents are waiting for input and highlights those windows in the tmux status bar.
- **Navigation** — `bay go` fuzzy-matches workspace names, branches, and PR numbers with fzf.
- **Shell completion** — tab-complete workspace names, dock names, and flag values in bash, zsh, and fish.

## Install

```
go install github.com/mpsalisbury/bay@latest
```

Then run `bay setup` to create your config and install shell completions.

## Documentation

- **[User Guide](docs/human-guide.md)** — setup, configuration, daily workflow, and command reference
- **[Agent Guide](docs/agent-guide.md)** — reference for AI agents operating inside bay workspaces
- **[Design Doc](docs/design.md)** — architecture, requirements, and design decisions
