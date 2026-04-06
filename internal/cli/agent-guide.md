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
matching CWD against known workspace paths. For surface-level commands
(`bay surface close`, `bay surface restart`), it additionally matches
the tmux pane ID to identify which surface.

Commands an agent inside a workspace typically uses:
- `bay pwd` — confirm bay context and what `self` means here
- `bay ws update self` — report branch, PR, or status changes
- `bay ws show self` — check own workspace metadata
- `bay ls` — see what other workspaces are doing
- `bay ws close self` — shut down when work is complete

Everything else — creating workspaces, opening additional windows,
navigation, recovery — is typically run from outside.

## Operational model

Bay is a coordination layer over:

- git worktree lifecycle
- tmux session/window/pane lifecycle
- workspace metadata (branch, PR, status, names)
- recovery after tmux or host restart

Use bay when you want durable managed state. Do not manually recreate
bay-managed windows after a reboot; use `bay recover`.

Important invariants:

- A **workspace** is the durable unit. Windows and panes are views onto
  it.
- Git branch is auto-read from the filesystem and kept in sync by bay.
  PR and status are manual metadata.
- `bay ls` is structural. Workspace rows show branch and sync state;
  pane rows show live pane kind plus pane agent for agent panes.
- Missing bay-managed tmux windows/panes are expected to be recoverable.

## Parsing and output

Prefer JSON output for any automation:

- `bay pwd --json`
- `bay ls --json`
- `bay ws show <name|self> --json`

Treat the normal human-formatted output of commands like `bay ls`,
`bay ws show`, `bay repo ls`, and `bay dock ls` as display output, not
as a stable parse contract.

### JSON: `bay ls --json`

Returns a tree object:

```json
{
  "focus": {
    "kind": "workspace",
    "repo": "labs",
    "dock": "labs",
    "workspace_id": "w1"
  },
  "recursive": true,
  "repos": [
    {
      "name": "labs",
      "path": "~/projects/labs",
      "docks": [
        {
          "name": "labs",
          "repo": "labs",
          "workspaces": [
            {
              "id": "w1",
              "name": "auth-fix",
              "type": "worktree",
              "path": "~/projects/labs-worktrees/w1",
              "branch": "feature/auth-fix",
              "status": "active",
              "sync_status": "ok",
              "window_count": 2,
              "windows": [
                {
                  "id": 1,
                  "name": "editor",
                  "tmux_window_id": "@12",
                  "status": "ok",
                  "panes": [
                    {
                      "id": 2,
                      "tmux_pane_id": "%22",
                      "type": "agent",
                      "agent": "codex",
                      "status": "ok"
                    }
                  ]
                }
              ]
            }
          ]
        }
      ]
    }
  ]
}
```

Field semantics:

- `focus` is the object bay inferred from cwd and tmux.
- `recursive` tells you whether descendants are expanded.
- `sync_status` is `ok`, `stale`, or `missing`.
- pane `type` is `agent`, `shell`, or `cmd`.

Use `bay ls --json --rows` when denormalized rows are easier to filter.

### JSON: `bay pwd --json`

Returns the current bay context:

```json
{
  "repo": "labs",
  "dock": "labs",
  "workspace_id": "w1",
  "workspace": "auth-fix",
  "window_id": 1,
  "pane_id": 2,
  "path": "~/projects/labs-worktrees/w1"
}
```

### JSON: `bay ws show <name|self> --json`

Returns a JSON object:

```json
{
  "id": "w1",
  "name": "auth-fix",
  "repo": "labs",
  "dock": "labs",
  "type": "worktree",
  "path": "...",
  "branch": "feature/auth-fix",
  "pr": "347",
  "status": "active",
  "sync_status": "ok",
  "default_agent": "codex",
  "windows": [
    {
      "id": 1,
      "tmux_window_id": "@12",
      "name": "editor",
      "status": "ok",
      "panes": [
        {
          "id": 2,
          "tmux_pane_id": "%22",
          "type": "agent",
          "agent": "codex",
          "status": "ok"
        }
      ]
    }
  ]
}
```

Pane semantics:

- `type` is `agent`, `shell`, or `cmd`.
- `agent` is the configured agent type for agent panes.
- `command` is the recorded shell command for `cmd` panes.
- `status` is the pane sync state.

Fields may be omitted when empty.

## Default target rules

These defaults are important for agent behavior:

| Command | Default target when omitted |
|---------|-----------------------------|
| `bay ws new [dock]` | current tmux session name, if it is a bay dock |
| `bay pwd` | current bay context |
| `bay ws show [name|self]` | `self` |
| `bay surface new [workspace]` | `self` |
| `bay surface close [workspace]` | `self` |
| `bay surface restart [workspace]` | `self` |
| `bay edit [name|self]` | `self` |
| `bay shell` | current workspace, split pane |

## Commands

Commands use explicit nouns: `ws` (workspace), `surface`/`sf` (surface), `dock`.
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
Use `--json` if you need stable machine-readable output.

```
bay ws show self
bay ws show labs:w3
bay ws show self --json
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

### Surface commands

#### `bay surface new [workspace] [--agent TYPE|--shell|--cmd "..."] [--window|--split h|v]`

Add a surface (pane) to a workspace. Defaults to a vertical split in the
current window. Use `--window` for a new tmux window instead of a split.

```
bay surface new --shell                     # shell pane (split)
bay surface new --window --agent codex      # agent in new window
bay surface new auth-fix --cmd "npm test"   # cmd in another workspace
bay sf new --split h                        # horizontal split (sf is alias)
```

#### `bay surface close [workspace] [--surface NAME]`

Close a surface. Defaults to current pane if in a workspace.

```
bay surface close
bay sf close --surface shell
```

#### `bay surface restart [workspace] [--surface NAME]`

Respawn a surface's process. Useful if an agent gets stuck. The worktree
and all git state are preserved.

```
bay surface restart
bay sf restart --surface agent
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
bay ls
bay ls --json
```

Safe to run from anywhere. Prefer `--json` for automation. The human
output is intended for display and may change.

#### `bay recover`

Reconstruct all docks, workspaces, windows, and panes after a reboot.
Recreates tmux sessions and windows, regenerates agent configs,
relaunches agents, starts the monitor, and prints attach commands.
Idempotent. Recovery can partially succeed and still return an error,
for example if some agent configs could not be regenerated.

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

Creates a worktree workspace with a fresh checkout. By default the
workspace starts in a shell. Pass `--agent` to launch the dock's
default agent, or `--agent NAME` for a specific agent.

### Track progress from inside

An agent working inside a workspace should keep its metadata current:

```
bay ws update self --branch fix-auth-header --status active
bay ws update self --pr 347
bay ws update self --status done
```

### Open a shell alongside your agent

From outside, add a shell surface to work in the same worktree:

```
bay surface new auth-fix --shell --window
```

Or add a shell pane split within the agent's window:

```
bay surface new --shell --split v
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
