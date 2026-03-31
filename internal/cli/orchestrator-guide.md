# Bay — Orchestrator Guide

Bay manages concurrent workspaces across tmux windows and git worktrees.
Use these commands to create, monitor, and manage workspaces.

## Quick reference

### Setup

    bay repo add <name> <path>                     # register a local repo
    bay repo add <name> <path> --url <git-url>     # clone and register
    bay dock new <name> --repo <r>                  # create a dock (tmux session)

### Create workspaces

    bay ws new [dock]                              # worktree + shell (default)
    bay ws new [dock] --agent                      # worktree + dock's default agent
    bay ws new [dock] --agent codex                # worktree + specific agent
    bay ws new [dock] --name <n>                   # with display name
    bay ws new [dock] --repo <r>                   # override dock repo

### Monitor

    bay ls                                         # all repos/docks/workspaces
    bay ws show <name>                             # workspace details

### Navigate

    bay go                                         # fzf picker (shows window types)
    bay go <query>                                 # jump by name/branch/PR/type
    bay go --next-waiting                          # cycle through waiting agents

### Editor

    bay edit <name>                                # open workspace in editor
    bay edit --all                                 # open all workspaces (multi-root)

### Shell

    bay shell                                      # split pane with shell
    bay shell <name>                               # new shell window for workspace

### Close and clean up

    bay ws close <name>                            # safety checks first
    bay ws close <name> --force                    # skip checks
    bay dock close <name>                          # close all in dock
    bay repo remove <name>                         # remove repo config
    bay repo remove <name> --force                 # also remove docks

### Recovery

    bay recover                                    # reconstruct after reboot

## Concepts

- **Repo**: a local git checkout bay creates worktrees from.
- **Dock**: a tmux session grouping workspaces, with a default repo.
- **Workspace**: an isolated worktree + tmux window(s). Has an ID (w1, w2)
  and a display name (auto-abbreviated from branch).
- **Status**: idle (fresh), active (branch set), done (ready to close).

## Typical orchestrator workflow

    # Create workspaces for tasks
    bay ws new dev --name auth-fix
    bay ws new dev --name update-deps

    # Add agents to specific workspaces
    bay win open auth-fix --agent claude

    # Check progress
    bay ls

    # Close completed work
    bay ws close auth-fix

    # After reboot
    bay recover

## Automatic branch detection

Bay automatically detects git branches in each workspace. When a branch
is created or changed, bay updates the workspace name and tmux window
title. No manual `bay ws update --branch` is needed.

PR numbers and status must still be set manually:

    bay ws update <name> --pr <number>
    bay ws update <name> --status done

## Workspace agents

When an agent is launched in a workspace (via --agent), it automatically
receives bay instructions in its config file. You do not need to teach
workspace agents about bay — it happens automatically.

## Machine-readable output

Both `bay ls` and `bay ws show` support `--json` for structured output.
Empty fields are omitted (`omitempty` semantics).

### `bay ls --json`

Returns an array of dock objects. Each dock has:
- `name` — dock name
- `agent` — default agent type (omitted if none)
- `repo` — default repo name (omitted if none)
- `workspaces` — array of workspace objects

Each workspace in the array has:
- `id` — workspace ID (e.g., `w1`)
- `name` — display name
- `type` — `worktree` or `external`
- `branch` — branch name (omitted if unset)
- `pr` — PR number as string (omitted if unset)
- `status` — `idle`, `active`, or `done`
- `waiting` — `true` if agent is waiting for input (omitted if false)
- `agent` — effective agent type (omitted if none)

### `bay ws show <name> --json`

Returns a single workspace object with full details:
- `id`, `name`, `dock`, `type`, `path`, `branch`, `pr`, `status`
- `windows` — array of window objects with pane details
