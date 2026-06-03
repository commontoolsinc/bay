# Bay

Multi-session bay management across tmux and git worktrees.

Bay manages concurrent bays — each with its own git worktree, tmux window, and shell. Run multiple PRs in parallel, open editors and agents alongside each other, and recover everything after a reboot.

For developers who work on multiple branches simultaneously and use tmux as their terminal multiplexer.

```
cd ~/projects/myproject
bay new                      # auto-bootstrap dock + create worktree
bay new --agent              # new worktree with the dock's default agent
bay edit                        # open editor for current bay
bay shell                       # split a shell into the current bay
bay home                        # focus/create shell in the dock checkout
bay go                       # fuzzy-pick a bay in the current dock
bay surface go               # fuzzy-pick a surface in the current bay
bay recover                     # reconstruct everything after reboot
```

## What it does

- **Worktree lifecycle** — create, close, rename bays backed by git worktrees or external directories. Safety checks on close (dirty files, unlanded commits). Each bay is an isolated checkout.
- **Surface model** — each bay contains one or more **surfaces** (agent panes, shell panes, command panes, GUI editors). Bay tracks them all and recovers them after reboot.
- **Home checkout surfaces** — `bay home` and `--bay home` open shells, agents, editors, or command panes in the dock's canonical checkout without giving it worktree cleanup behavior.
- **Editor integration** — `bay edit` opens your bay in cursor, VS Code, zed, nvim, or vim. `bay edit --dock` for multi-root.
- **Agent support** — optionally launch AI agents (Claude Code, Codex, Antigravity). Agent surfaces resume on restart and on undo-close via configured `resume_args`. Closing an agent surface prompts for confirmation.
- **Scoped navigation** — `bay surface go` picks surfaces within the current bay; `bay go` picks bays within the current dock. Cycling flashes a brief position indicator.
- **Command palette** — `Option+p` opens a VS Code-style palette in a tmux popup: fuzzy-search every bay command, see its hotkey if it has one, and launch without leaving the keyboard.
- **Hierarchical browsing** — `bay ls` lists bays in the current dock; `bay tree` shows the full hierarchy including surfaces.
- **Status line** — `bay status-line <field>` provides bay info for tmux status bar composition.
- **Shell completion** — tab-complete bay IDs, dock names, and flag values in bash, zsh, and fish.

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
- **[Agent Reference](docs/agent-reference.md)** — comprehensive reference for AI agents operating inside bays
