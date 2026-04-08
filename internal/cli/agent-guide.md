# Bay — Agent Guide

Bay manages concurrent workspaces with surfaces across tmux and GUI
applications. This guide is for AI agents (Claude Code, Codex, etc.)
that create, monitor, and manage workspaces — either from an
orchestrating session or from within a workspace.

## Key concepts

A **workspace** is a managed working directory with metadata (branch,
PR, status). Each workspace has:
- A **name** — the primary identifier. Defaults to auto-abbreviated
  branch name (stripping prefixes like `feature/`, `fix/`). Can be
  overridden with `bay ws rename`, which sticks permanently.
- A **full reference** — `dock:name` (e.g., `labs:auth-fix`). Bare
  name (`auth-fix`) works when unambiguous across docks.

A **dock** is a named tmux session grouping related workspaces. Each
dock has a default agent type and optionally a default repo and host
terminal.

A **surface** is the unit of navigation — anything you can focus and
jump to. Surfaces replace the old window/pane hierarchy. Each surface
has:
- An **ID** — integer, unique within its workspace, never reused.
- A **name** — unique within its workspace (e.g., `agent`, `shell`,
  `editor`).
- A **type** — semantic role: `agent`, `editor`, `shell`, or `cmd`.
- A **backend** — interaction model: `tmux-pane` or `gui-app`.

### Workspace types

| Type | CWD | Bay manages the directory |
|------|-----|--------------------------|
| **Worktree** | git worktree | yes — creates and deletes it, safety checks on close |
| **External** | any user path | no — bay remembers it for recovery |

### Statuses

- `idle` — created, no work assigned yet
- `active` — has a branch, work in progress
- `done` — work complete (detected automatically on PR merge, or set manually)

### Surface types and backends

| Type | Backend | Description |
|------|---------|-------------|
| `agent` | `tmux-pane` | AI agent (Claude Code, Codex, etc.) |
| `shell` | `tmux-pane` | Interactive shell |
| `cmd` | `tmux-pane` | One-off command |
| `editor` | `gui-app` | GUI editor (Cursor, VS Code, Zed) |

## Self resolution

Most commands accept `self` or default to it when no target is given.

**Workspace resolution**: `self` matches CWD against known workspace
paths. If CWD is inside a workspace directory, that workspace is
resolved.

**Surface resolution**: For surface-scoped commands (`bay surface
close`, `bay surface restart`), `self` additionally matches the
current tmux pane ID to identify which surface within the workspace.

Commands an agent inside a workspace typically uses:
- `bay pwd` — confirm bay context (repo, dock, workspace, surface)
- `bay ws show self` — check own workspace metadata
- `bay ls` — see what other workspaces are doing
- `bay ws close self` — shut down when work is complete

## Operational model

Bay is a coordination layer over:
- git worktree lifecycle
- tmux session/window/pane lifecycle
- GUI editor lifecycle (launch, focus, liveness probing)
- workspace metadata (branch, PR, status)
- recovery after tmux or host restart

Important invariants:
- A **workspace** is the durable unit. Surfaces are views onto it.
- Branch, PR number, and status are all auto-detected. Branch comes
  from the filesystem on every display. PR is looked up via `gh pr
  view` once and cached. Status transitions to `done` automatically
  when the branch is merged into the default branch.
- Bay does not generate config files into worktrees. Project
  instructions go in the repo's `CLAUDE.md` (or equivalent).
  Per-workspace context is discoverable via `bay pwd --json`.

## Parsing and output

Prefer JSON output for automation:
- `bay pwd --json`
- `bay ls --json`
- `bay ws show [name] --json` (defaults to current; pass `self` explicitly for the same effect)

Human-formatted output is for display only and may change between
versions.

### JSON: `bay pwd --json`

Returns the current bay context:

```json
{
  "repo": "labs",
  "dock": "labs",
  "workspace": "auth-fix",
  "surface": "agent",
  "surface_id": 1,
  "path": "/Users/dev/projects/labs-worktrees/auth-fix"
}
```

Fields:
- `repo` — repo config key.
- `dock` — dock name (tmux session).
- `workspace` — workspace name.
- `surface` — current surface name (resolved from tmux pane).
- `surface_id` — current surface ID.
- `path` — absolute working directory path.

Fields are omitted when empty.

### JSON: `bay ls --json`

Returns a tree:

```json
{
  "focus": {
    "kind": "workspace",
    "repo": "labs",
    "dock": "labs",
    "workspace_id": "auth-fix"
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
          "agent": "claude",
          "workspaces": [
            {
              "name": "auth-fix",
              "type": "worktree",
              "path": "~/projects/labs-worktrees/auth-fix",
              "branch": "feature/auth-fix",
              "status": "active",
              "sync_status": "ok",
              "surface_count": 2,
              "surfaces": [
                {
                  "id": 1,
                  "name": "agent",
                  "type": "agent",
                  "backend": "tmux-pane",
                  "agent": "claude",
                  "status": "ok"
                },
                {
                  "id": 2,
                  "name": "shell",
                  "type": "shell",
                  "backend": "tmux-pane",
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
```

Field semantics:
- `focus` — the scope bay inferred from CWD and tmux.
  `focus.workspace_id` is the workspace **name** (not a numeric ID).
- `recursive` — whether surfaces are expanded in the output.
- `sync_status` — `ok`, `stale` (tmux window missing), or `missing`
  (worktree directory gone).
- `waiting` — true if any surface in the workspace has an agent
  waiting for input.

Use `bay ls --json --rows` for denormalized rows (easier to filter):

```json
[
  {
    "repo": "labs",
    "dock": "labs",
    "workspace_name": "auth-fix",
    "workspace_branch": "feature/auth-fix",
    "workspace_status": "active",
    "workspace_sync_status": "ok",
    "surface_id": 1,
    "surface_name": "agent",
    "surface_type": "agent",
    "surface_agent": "claude",
    "surface_status": "ok"
  }
]
```

Use `bay ls -R` to force recursive output (show surfaces) regardless
of focus scope.

### JSON: `bay ws show [name] --json`

```json
{
  "name": "auth-fix",
  "repo": "labs",
  "dock": "labs",
  "type": "worktree",
  "path": "/Users/dev/projects/labs-worktrees/auth-fix",
  "branch": "feature/auth-fix",
  "pr": "347",
  "status": "active",
  "sync_status": "ok",
  "default_agent": "claude",
  "surfaces": [
    {
      "id": 1,
      "name": "agent",
      "type": "agent",
      "backend": "tmux-pane",
      "agent": "claude",
      "status": "ok"
    },
    {
      "id": 2,
      "name": "shell",
      "type": "shell",
      "backend": "tmux-pane",
      "status": "ok"
    }
  ]
}
```

Surface fields:
- `type` — `agent`, `editor`, `shell`, or `cmd`.
- `backend` — `tmux-pane` or `gui-app`.
- `agent` — agent config key (present when type is `agent`).
- `command` — shell command (present when type is `cmd`).
- `status` — `ok` or `stale`.

Fields are omitted when empty.

## Default target rules

Across all create-verbs (`ws new`, `surface new`, `shell`, `new
shell|agent|cmd`) the positional names the **thing being created**.
The container (dock, workspace) is selected via `--dock` / `--ws`
flags or, when omitted, inherited from the current tmux session.

| Command | Default target when omitted |
|---------|-----------------------------|
| `bay ws new [name]` | current dock from tmux session, or auto-bootstrap from CWD |
| `bay ws show [name]` | current workspace |
| `bay ws close [name]` | required (no default; use `--done` for batch) |
| `bay ws close --done` | all done workspaces in current dock |
| `bay pwd` | current bay context |
| `bay surface new [name]` | current workspace |
| `bay surface close <name>` | required (use `self` for current pane) |
| `bay surface restart [name]` | current pane's surface |
| `bay surface show [name]` | current pane's surface |
| `bay surface tree` (none) | n/a — surfaces are leaves; use `bay surface show` |
| `bay dock tree [name]` | current dock from tmux session |
| `bay go [query]` | surfaces in current workspace |
| `bay ws go [query]` | workspaces in current dock |
| `bay edit [workspace]` | current workspace |
| `bay shell [name]` | current workspace, surface auto-named "shell" |

## Commands

Commands use explicit nouns: `ws` (workspace), `surface`/`sf`
(surface), `dock`, `repo`. Top-level shorthands exist for frequent
operations.

### Workspace commands

#### `bay ws new [name] [--dock DOCK] [--repo NAME] [--dir PATH] [--branch NAME] [--agent [TYPE]] [--shell]`

Create a workspace with its first surface. The positional names the
new workspace; `--dock` selects which dock to create it in. `--dock`
defaults to the current tmux session if it is a bay dock, or
auto-bootstraps from CWD (creates a dock and repo config
automatically).

Default behavior opens a shell. Use `--agent` (bare flag) for the
dock's default agent, or `--agent TYPE` for a specific one.

```
bay ws new                                  # auto-bootstrap from CWD
bay ws new auth-fix                         # named workspace in current dock
bay ws new auth-fix --dock labs             # named workspace in a specific dock
bay ws new auth-fix --repo ct-server        # using a different repo
bay ws new auth-fix --dir ~/projects/foo    # external workspace
bay ws new auth-fix --agent                 # launch dock's default agent
bay ws new auth-fix --agent codex           # launch specific agent
bay ws new auth-fix --branch fix-auth       # create and checkout branch
```

#### `bay ws close [name] [--force] [--done]`

Close a workspace and all its surfaces. For worktree workspaces,
checks for uncommitted changes and unpushed commits. Refuses if dirty
unless `--force` is used. Pass `self` to close the current workspace.

Use `--done` (without a name) to batch-close all workspaces with
status `done` in the current dock.

```
bay ws close auth-fix
bay ws close self
bay ws close self --force
bay ws close --done
bay ws close --done --force
```

#### `bay ws show [name] [--json]`

Show workspace details: path, branch, PR, status, surfaces. Defaults
to the current workspace; pass `self` explicitly for the same effect.

```
bay ws show
bay ws show auth-fix
bay ws show self --json
```

#### `bay ws rename <name> <new-name>`

Rename a workspace. Overrides auto-abbreviation permanently. Names
must match `[a-zA-Z0-9_-]+`. Pass `self` as the first arg to rename
the current workspace.

```
bay ws rename auth-fix mem-refactor
bay ws rename self mem-refactor
```

### Navigation

#### `bay go [query]` (alias for `bay surface go`)

Navigate between surfaces within the current workspace. This is
intra-workspace navigation.

- No args: opens a picker showing all surfaces in the workspace.
- With query: fuzzy-matches surface name or type. One match jumps
  directly; multiple opens the picker pre-filtered.
- `--index N`: jump to surface by 1-based index.
- `--next-waiting`: jump to next waiting surface in the workspace.

```
bay go                      # pick from surfaces
bay go shell                # jump to the shell surface
bay go --index 2            # jump to surface #2
bay go --next-waiting       # jump to next waiting surface
```

#### `bay ws go [query]`

Navigate between workspaces within the current dock. This is
intra-dock navigation.

- No args: opens a picker showing all workspaces in the dock.
- With query: fuzzy-matches workspace name, branch, or PR number.
- `--waiting`: filter to workspaces with waiting agents.
- `--next-waiting`: jump to next waiting workspace, cycling.

```
bay ws go                       # pick from workspaces
bay ws go auth-fix              # jump to workspace
bay ws go 347                   # match by PR number
bay ws go --waiting             # filter to waiting
bay ws go --next-waiting        # cycle through waiting
```

#### `bay surface next` / `bay surface prev`

Cycle to the next or previous surface within the current workspace.

#### `bay ws next` / `bay ws prev`

Cycle to the next or previous workspace within the current dock.

### Surface commands

#### `bay surface new [name] [--ws WS] [--dock DOCK] [--agent TYPE|--shell|--cmd "..."] [--window|--split h|v]`

Add a surface to a workspace. The positional names the new surface;
`--ws` selects which workspace it goes in (default: current).
Defaults to a vertical split in the current tmux window. Use
`--window` for a new tmux window.

Alias: `bay sf new`.

```
bay surface new                             # auto-named shell, current ws
bay surface new tests                       # surface named "tests"
bay surface new --window --agent codex      # agent in new window
bay surface new tests --cmd "npm test"      # named cmd surface
bay surface new tests --ws auth-fix         # in a different workspace
bay sf new --split h                        # horizontal split
```

#### `bay surface close <name> [--ws WS] [--dock DOCK] [--force]`

Close a named surface. Pass `self` to close the current pane's
surface. Agent surfaces prompt for confirmation unless `--force`.

```
bay sf close shell-2
bay sf close self
bay sf close agent --force
```

#### `bay surface restart [name] [--ws WS] [--dock DOCK]`

Respawn a surface's process. Defaults to the current pane's surface.
For agent surfaces, uses `resume_args` from agent config (e.g.,
`--continue` for Claude Code) to reconnect to the existing session.
The worktree and git state are preserved.

```
bay surface restart                         # current pane
bay sf restart agent                        # named surface
```

#### `bay surface show [name] [--ws WS] [--dock DOCK]`

Print details for a surface. Defaults to the current pane's surface.

#### `bay surface rename <old> <new> [--ws WS] [--dock DOCK]`

Rename a surface. Pass `self` as `<old>` to rename the current
surface.

### Top-level shorthands

```
bay go [query]         → bay surface go [query]
bay shell [name]       → bay surface new --shell [name]
bay edit [workspace]   → open workspace in configured editor
bay ls                 → list everything
bay pwd                → show current bay context
bay recover            → reconstruct state after reboot
```

#### `bay edit [workspace] [--all]`

Open a workspace in the configured editor. For GUI editors (Cursor,
VS Code, Zed), creates a `gui-app` surface so the editor appears in
`bay go` navigation.

Editor resolution: config `[editor].command` > `$VISUAL` > `$EDITOR`
> probe (cursor, code, zed, nvim, vim).

```
bay edit                    # open current workspace
bay edit auth-fix           # open specific workspace
bay edit --all              # open all workspaces in dock
```

For editor configuration, see `bay config editor` below.

#### `bay config edit|show|path|editor [name]`

Manage bay's config file (`~/.config/bay/config.toml`).

```
bay config                      # show config-format docs
bay config edit                 # open config in your editor
bay config show                 # print effective config (TOML)
bay config path                 # print config file path
bay config editor               # print resolved editor command
bay config editor cursor        # set the default editor
```

The `editor` subcommand replaces the old `bay edit --set` and
`bay edit --show` flags.

#### `bay shell [name] [--ws WS] [--dock DOCK] [--window] [--split h|v]`

Open a shell surface in a workspace. The positional names the new
shell; `--ws` selects which workspace it goes in (default: current).

```
bay shell                   # auto-named shell in current workspace
bay shell logs              # named "logs"
bay shell --window          # new tmux window instead of split
bay shell logs --ws auth-fix # in a different workspace
```

### Global commands

#### `bay ls [--json] [--rows] [-R] [-l] [--dirty]`

Show the full hierarchy: repos, docks, workspaces, surfaces. Use
`--json` for automation. Use `-R` for recursive output with surfaces.

```
bay ls
bay ls --json
bay ls --json --rows
bay ls -R
bay ls --dirty
```

#### `bay pwd [--json]`

Show the current bay context.

```
bay pwd
bay pwd --json
```

#### `bay recover`

Reconstruct all docks, workspaces, and surfaces after a reboot.
Recreates tmux sessions and windows, relaunches agents with
`resume_args`, starts the monitor. Idempotent.

#### `bay doctor`

Health checks: config validity, repo accessibility, agent availability,
manifest consistency, monitor status, tmux keybindings, bay awareness
in repos.

#### `bay monitor start|stop|status|add-prompt`

Manage the background pane monitor. Detects when agents are waiting
for input and highlights those tmux windows. Also handles
activity-gated merge detection.

`bay monitor add-prompt [name]` captures pane text to create a new
agent-waiting detection pattern. Auto-starts the monitor if it isn't
running, since a new pattern is dead weight until the monitor reads it.

### Dock management

```
bay dock new <name> [--repo NAME] [--agent TYPE] [--terminal APP]
bay dock ls
bay dock show <name>
bay dock close <name> [--force]
bay dock recover <name>
```

### Repo management

```
bay repo ls
bay repo show <name>
bay repo add <name> <path>
bay repo remove <name> [--force]
bay repo init [name]
```

`bay repo init` sets up bay awareness: appends a one-liner to each
agent's project file (e.g., `CLAUDE.md`) pointing to `bay agent-guide`,
and creates `.worktreeinclude` if missing.

## Typical workflows

### Spin up a workspace for a task

```
bay ws new auth-fix --dock labs
```

Creates a worktree workspace. Opens a shell by default. Use `--agent`
to launch the dock's default agent instead. Drop `--dock` to target
the current dock from your tmux session.

### Track progress from inside

Branch, PR number, and status are all tracked automatically. Branch
comes from the worktree on every display. PR is looked up via `gh pr
view` and cached. Status flips to `done` when the branch is merged
into the default branch. Just commit, push, and open a PR — bay sees
all of it.

### Open a shell alongside your agent

Split within the agent's surface group:

```
bay shell
```

Or as a new tmux window:

```
bay shell --window
```

### Open an editor for the workspace

```
bay edit
```

GUI editors become navigable surfaces visible in `bay go`.

### Check on all active work

```
bay ls
```

Shows every workspace, status, branch, PR, and waiting indicators.

### Navigate to a waiting agent

Within current workspace:

```
bay go --next-waiting
```

Across workspaces in the dock:

```
bay ws go --next-waiting
```

### Use an existing directory

```
bay ws new my-foo --dock labs --dir ~/projects/foo
```

External workspaces let bay manage tmux recovery for directories it
does not own. Closing removes surfaces but leaves the directory
untouched.

### Close a workspace after a PR merges

Status transitions to `done` automatically when bay detects a merge.
Then:

```
bay ws close auth-fix
```

Or batch-close all finished work:

```
bay ws close --done
```

### Manage multiple concurrent PRs

```
bay ws new auth-fix --dock labs
bay ws new perf-regression --dock labs
bay ws new update-deps --dock labs
bay ls
```

Each workspace gets its own worktree. They share the same repo but
work on independent branches.

### Recover after reboot

```
bay recover
```

Reconstructs all tmux sessions, surfaces, and agent sessions (using
`resume_args`). Prints attach commands.

### Zero-config quickstart

From any git repo directory, with no prior bay configuration:

```
bay ws new
```

Bay auto-detects the repo, creates a dock, probes for an agent on
PATH, and opens a workspace. No config file needed for basic usage.
