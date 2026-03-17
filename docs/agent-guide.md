# Bay — Agent Guide

Bay manages concurrent workspaces across tmux windows and git worktrees.
This guide is for AI agents that need to create, monitor, and manage
workspaces — either from an orchestrating session outside any workspace,
or from within a workspace itself.

## Key concepts

A **workspace** is a managed working directory with metadata (branch, PR,
status). It can have multiple tmux windows, each with multiple panes.
Each workspace has:
- An **ID** — immutable, auto-assigned per dock (`w1`, `w2`, ...).
- A **name** — defaults to auto-abbreviated branch name (stripping
  prefixes like `feature/`, `fix/`). Can be overridden with
  `bay ws rename`, which sticks permanently.
- A **full reference** — `dock:id` or `dock:name` (e.g., `labs:w3`,
  `labs:mem-refactor`). Short form (`w3`) works when unambiguous
  across docks.

A **dock** is a named tmux session grouping related workspaces. Each
dock has a default agent type and optionally a default repo.

A **window** is a tmux window attached to a workspace. Multiple windows
can point at the same workspace — e.g., an agent window and a shell
window side by side.

A **pane** is a tmux pane within a window. Runs an agent, a shell, or
a command. Bay tracks panes it creates for recovery; manual tmux splits
are not tracked.

### Workspace types

| Type | CWD | Bay manages the directory |
|------|-----|--------------------------|
| **Worktree** | git worktree | yes — creates and deletes it, safety checks on close |
| **External** | any user path | no — bay remembers it for recovery and config injection |

### Statuses

- `idle` — created, no work assigned yet
- `active` — has a branch, work in progress
- `done` — work complete, ready to close (window dimmed in tmux)

## Inside vs. outside a workspace

Most management happens from **outside** — an orchestrating agent or
human creating workspaces, listing status, and closing finished work.

A few commands are designed to be run from **inside** a workspace by the
agent working in it. Inside a workspace, agents run `bay` through shell
escapes (e.g., `!bay ws update self ...` in Claude Code). The keyword
`self` resolves based on CWD.

**How `self` works**: `self` resolves to the current **workspace** by
matching CWD against known workspace paths. For window-level commands
(`bay win close self`, `bay win restart self`), it additionally matches
the tmux window ID to identify which window.

Commands an agent inside a workspace typically uses:
- `bay ws update self` — report branch, PR, or status changes
- `bay ws show self` — check own workspace metadata
- `bay ls` — see what other workspaces are doing
- `bay ws close self` — shut down when work is complete

Everything else — creating workspaces, opening additional windows,
navigation, recovery — is typically run from outside.

## Commands

Commands use explicit nouns: `ws` (workspace), `win` (window), `dock`.
`ls` is shorthand for list.

### Workspace commands

#### `bay ws new [dock] [--repo NAME] [--dir PATH] [--name NAME] [--agent TYPE] [--shell]`

Create a workspace with its first window. Dock defaults to the current
tmux session name — pass it explicitly if outside tmux or in a non-dock
session.

By default creates a worktree workspace using the dock's default repo.
Use `--dir` for an external workspace pointing at an existing directory.
Use `--shell` to open a shell instead of launching an agent.

```
bay ws new labs                          # worktree workspace, default repo
bay ws new labs --repo ct-server         # worktree using a different repo
bay ws new labs --dir ~/projects/foo     # external workspace
bay ws new labs --name auth-fix          # with explicit name
bay ws new research --shell              # shell instead of agent
```

#### `bay ws close <name|self> [--force]`

Close a workspace and all its windows/panes.

For worktree workspaces, checks for uncommitted changes and unpushed
commits. Refuses if dirty unless `--force` is used. Removes the
worktree and config files.

For external workspaces, closes windows and removes bay-generated config
files. The directory itself is untouched.

```
bay ws close auth-fix
bay ws close self
bay ws close self --force
```

#### `bay ws show <name|self>`

Show detailed information: paths, branch, PR, status, windows, panes.

```
bay ws show self
bay ws show labs:w3
```

#### `bay ws update <name|self> [--branch NAME] [--pr NUMBER] [--status STATUS]`

Update workspace metadata and refresh window names. This does **not**
modify git state — it's a metadata update. The agent has already done
the git work; this keeps bay's tracking in sync.

At least one flag is required. Status must be `idle`, `active`, or
`done`.

```
# From inside (the most common use):
bay ws update self --branch fix-auth-header --status active
bay ws update self --pr 347
bay ws update self --status done

# From outside:
bay ws update auth-fix --status done
```

#### `bay ws rename <name|self> <new>`

Rename a workspace's display name. Overrides the auto-abbreviation
permanently. Names must match `[a-zA-Z0-9_-]+`.

```
bay ws rename w3 mem-refactor
bay ws rename self mem-refactor
```

### Window commands

#### `bay win open <workspace> [--agent TYPE|--shell|--cmd "..."]`

Add a new window to an existing workspace. Defaults to launching the
dock's default agent. Use `--shell` for a plain shell or `--cmd` for
a specific command.

```
bay win open mem-refactor --shell        # shell window alongside agent
bay win open mem-refactor --agent codex  # different agent type
bay win open mem-refactor --cmd "npm test --watch"
```

#### `bay win close [self|name]`

Close a window without affecting the workspace or other windows.

```
bay win close self
```

#### `bay win restart [self|name]`

Kill all panes in the window, regenerate the config file from the
current dock template, and respawn each pane. Useful after template
changes or if an agent gets stuck. The worktree and all git state are
preserved.

```
bay win restart self
bay win restart mem-refactor
```

### Pane commands

#### `bay pane add [--agent TYPE|--shell|--cmd "..."] [--split h|v]`

Add a pane to the current window by splitting. Defaults to horizontal
split.

```
bay pane add --shell --split v           # vertical shell pane
bay pane add --cmd "tail -f app.log"     # log tail pane
```

### Navigation

#### `bay go [query]`

Fuzzy-find and switch to a window. Matches against workspace names,
branch names, PR numbers, and dock names.

- No args: opens an fzf picker showing all windows.
- With query: filters matches. One result jumps directly; multiple
  open the picker pre-filtered.
- `--waiting`: filters to windows with agents waiting for input.
- `--next-waiting`: jumps to the next waiting window, cycling through.
  Designed for use as a tmux keybinding.

```
bay go mem-refactor
bay go 347                               # match by PR number
bay go --waiting
bay go --next-waiting
```

### Global commands

#### `bay ls`

Show all workspaces and windows across all docks, with waiting status.

```
Dock: labs
  w3  mem-refactor  claude  feature/refactor-memory-access  #234  active  ⏳
  w4  w4            claude  —                                —    idle
Dock: research
  w1  perf-study    claude  —                                —    idle
```

Safe to run from anywhere. The `⏳` indicator means the monitor has
detected the agent is waiting for input.

#### `bay recover`

Reconstruct all docks, workspaces, windows, and panes after a reboot.
Recreates tmux sessions and windows, regenerates agent configs,
relaunches agents, starts the monitor, and prints attach commands.
Idempotent.

#### `bay doctor`

Health checks: config validity, repo accessibility, gitignore rules,
monitor status, tmux keybindings, manifest consistency.

#### `bay monitor start|stop|status`

Manage the background pane monitor. The monitor detects when agents are
waiting for input and highlights those tmux windows. Normally started
by `bay recover`.

#### `bay add-prompt [name]`

Capture pane text to create a new agent-waiting detection pattern.

### Dock management

```
bay dock new <name> [--repo NAME] [--agent TYPE] [--template PATH]
bay dock ls
bay dock close <name> [--force]
bay dock recover <name>
```

`bay dock close` runs safety checks on every workspace before closing.
Use `--force` to override.

## Typical workflows

### Spin up a workspace for a task

```
bay ws new labs --name auth-fix
```

Creates a worktree workspace with a fresh checkout. The agent starts
automatically in a new tmux window.

### Track progress from inside

An agent working inside a workspace should keep its metadata current:

```
bay ws update self --branch fix-auth-header --status active
bay ws update self --pr 347
bay ws update self --status done
```

### Open a shell alongside your agent

From outside, add a shell window to work in the same worktree:

```
bay win open auth-fix --shell
```

Or add a shell pane within the agent's window:

```
bay pane add --shell --split v
```

### Check on all active work

```
bay ls
```

Shows every workspace, its status, branch, PR, and whether any agent
is waiting for input.

### Navigate to a waiting agent

```
bay go --next-waiting
```

Jumps to the next window where an agent needs attention. Cycle through
repeatedly to triage all waiting agents.

### Use an existing directory

```
bay ws new labs --dir ~/projects/foo
```

External workspaces let bay manage tmux recovery and agent config for
directories it doesn't own. Closing removes windows and config files
but leaves the directory untouched.

### Close a workspace after a PR merges

```
bay ws close auth-fix
```

Checks for uncommitted changes and unpushed commits first. Either push
or use `--force` to override.

### Manage multiple concurrent PRs

```
bay ws new labs --name auth-fix
bay ws new labs --name perf-regression
bay ws new labs --name update-deps
bay ls
```

Each workspace gets its own worktree. They share the same repo but work
on independent branches.

### Recover after reboot

```
bay recover
```

Reconstructs all tmux sessions, windows, panes, worktrees, and agent
configs. Prints attach commands so you can reconnect.
