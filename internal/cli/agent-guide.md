# Bay — Agent Contract

Bay is a coordination layer over git worktrees and tmux. Its purpose is
to let an orchestrator or in-workspace agent create, inspect, update,
recover, and safely close concurrent workspaces without manually
managing git worktrees or tmux state.

If you need machine-readable state, prefer JSON output. Treat the normal
human-formatted output of commands like `bay ls` and `bay ws show` as
display-only and not stable for parsing.

## Core model

repo
: A registered local git checkout. Bay creates worktrees from repos.

dock
: A tmux session grouping related workspaces. A dock can define:
  - default repo
  - default agent
  - agent config template

workspace
: The durable unit bay manages. A workspace has:
  - immutable ID: `w1`, `w2`, ...
  - display name
  - type: `worktree` or `external`
  - path
  - branch, PR, status metadata
  - optional workspace-level agent override
  - one or more windows

window
: A tmux window attached to a workspace. Multiple windows can point at
  the same workspace path.

pane
: A tmux pane within a window. Pane type is `agent`, `shell`, or `cmd`.

## Important invariants

- The workspace is the durable object. Windows and panes are views onto
  it.
- Branch is read from git and auto-synced by bay. Agents usually should
  not call `bay ws update --branch` unless they are deliberately
  overriding metadata.
- PR and status are manual metadata. Agents should update these.
- In `bay ls`, the `AGENT` field means configured workspace agent:
  workspace override if one exists, otherwise dock default. It does not
  mean the currently running pane type.
- Missing tmux windows and panes are intended to be recoverable with
  `bay recover`.

## Defaults and target resolution

Bay uses several important defaults. Agents should rely on them
deliberately, not accidentally.

- `bay ws new [dock]`
  If `dock` is omitted, bay infers it from the current tmux session.
  This only works inside a bay dock.
- `bay ws show [name|self]`
  Defaults to `self`.
- `bay win open [workspace]`
  Defaults to `self`.
- `bay win close [self|workspace]`
  Defaults to `self`.
- `bay win restart [self|workspace]`
  Defaults to `self`.
- `bay edit [name|self]`
  Defaults to `self`.
- `bay shell`
  With no args, split a shell pane in the current window of the current
  workspace.
- `bay shell <name>`
  Open a new shell window for the named workspace.

## How `self` resolves

`self` resolves to the current workspace by matching the current working
directory against known workspace paths.

For window-level commands such as `bay win close self` and
`bay win restart self`, bay additionally uses the current tmux window ID
to identify which workspace window you mean. If tmux window matching
fails after the workspace is resolved, bay falls back to the primary
window of that workspace.

## Commands agents normally use

Inside a workspace:

```sh
bay ws show self
bay ws update self --pr <number>
bay ws update self --status done
bay ls --json
bay ws close self
```

Outside a workspace:

```sh
bay ws new [dock]
bay ws new [dock] --agent
bay ws new [dock] --agent codex
bay win open [workspace]
bay win open [workspace] --agent codex
bay recover
bay dock recover <name>
bay ws close <name>
```

## Commands that are usually for humans, not automation

- `bay go` is tmux navigation for humans.
- `bay edit` launches an editor.
- `bay doctor` is a diagnostic command for humans.

Use them when appropriate, but do not build automation around their
text output.

## Machine-readable output

### `bay ls --json`

Returns a JSON array of dock objects:

```json
[
  {
    "name": "labs",
    "agent": "claude",
    "repo": "labs",
    "workspaces": [
      {
        "id": "w1",
        "name": "auth-fix",
        "type": "worktree",
        "path": "~/projects/labs-worktrees/w1",
        "branch": "feature/auth-fix",
        "pr": "347",
        "status": "active",
        "waiting": true,
        "missing": false,
        "stale": false,
        "agent": "codex"
      }
    ]
  }
]
```

Field semantics:

- `dock.agent`: dock default agent
- `workspace.agent`: configured workspace agent
- `waiting`: a managed window in the workspace is marked waiting
- `missing`: workspace path does not exist on disk
- `stale`: bay-managed tmux state is missing and can be recovered

Omitted fields may be absent when empty because JSON uses `omitempty`.

### `bay ws show <name|self> --json`

Returns a JSON object:

```json
{
  "id": "w1",
  "name": "auth-fix",
  "dock": "labs",
  "type": "worktree",
  "path": "...",
  "branch": "feature/auth-fix",
  "pr": "347",
  "status": "active",
  "windows": [
    {
      "id": 1,
      "tmux_window_id": "@12",
      "name": "auth-fix",
      "panes": [
        {
          "id": 1,
          "type": "agent",
          "agent": "codex",
          "command": "",
          "split_from": 0,
          "split_dir": ""
        }
      ]
    }
  ]
}
```

Pane fields:

- `type`: `agent`, `shell`, or `cmd`
- `agent`: agent type for agent panes
- `command`: recorded command for `cmd` panes
- `split_from` and `split_dir`: bay-managed layout metadata

## Safety and error behavior

- `bay ws close` can refuse to close worktree workspaces with dirty
  files or unpushed commits unless `--force` is used.
- `bay recover` can partially succeed and still return an error. Read
  its printed recovery summary even when the exit status is non-zero.
- Agent launch, restart, and recovery can fail if bay cannot generate
  the agent config file. Typical causes are missing gitignore entries or
  broken template paths.
- `bay repo remove --force` removes bay state for the repo and any docks
  using it, but it does not delete the underlying repo checkout.

## Recommended agent workflow

1. Use `bay ls --json` to discover current state across docks.
2. Use `bay ws show self --json` inside a workspace when you need your
   current window and pane layout.
3. Use `bay ws update self --pr ...` and `bay ws update self --status done`
   for manual metadata.
4. Use `bay recover` after tmux or server loss instead of trying to
   reconstruct windows manually.
5. Parse JSON, not display output.
