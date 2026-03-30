# Bay — User Guide

Bay manages concurrent workspaces built on git worktrees and tmux. Each
workspace gets its own worktree, tmux window, and shell — so you can
work on multiple branches simultaneously without `git stash` juggling or
directory cloning. Bay handles session recovery after reboot, and
optionally launches AI coding agents inside workspaces when you want
them.

## Prerequisites

- **tmux** — bay manages tmux sessions and windows. Install with
  `brew install tmux` (macOS) or your system package manager. If you're
  new to tmux, the key concept is: tmux keeps terminal sessions alive
  in the background. You can detach and reattach without losing state.
  Bay leans on this heavily.

- **An AI coding agent** (optional) — bay works with
  [Claude Code](https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/overview),
  [Codex](https://github.com/openai/codex), or any agent that runs in a
  terminal. Agents are opt-in per workspace — bay is useful on its own
  for worktree + tmux management.

- **git** — for worktree-based workspaces.

- **fzf** (optional) — enables the fuzzy picker in `bay go`. Install
  with `brew install fzf`. Bay falls back to a plain list without it.

## Installation

```
go install github.com/commontoolsinc/bay/cmd/bay@latest
```

For private repos, configure Go to use SSH:
```
go env -w GOPRIVATE=github.com/commontoolsinc/bay
git config --global url."git@github.com:".insteadOf "https://github.com/"
```

## Quick start

### 1. Run setup

```
bay setup
```

This creates your config file, seeds default agent definitions,
installs shell completions, and sets up tmux keybindings.

### 2. Add a repo and create a dock

```
bay repo add myproject ~/projects/myproject
bay dock new dev --repo myproject
tmux attach -t dev
```

Or clone and register in one step:
```
bay repo add myproject ~/projects/myproject --url git@github.com:org/myproject.git
```

### 3. Create a workspace

From inside the dock:
```
bay ws new
```

This creates a git worktree and opens a new tmux window with a shell.
The placeholder `~` window disappears automatically.

To launch an agent instead of a shell:
```
bay ws new --agent              # uses dock's default agent
bay ws new --agent codex        # specific agent
```

### 4. Do your work

Work in the shell, open an editor, run tests — whatever you need. If
you launched an agent, interact with it in the tmux window.

When you create a branch or open a PR, update bay's tracking:

```
bay ws update self --branch fix-auth-header --status active
bay ws update self --pr 347
```

(Agent templates can include instructions to do this automatically.)

### 5. Check on things

```
bay ls
```

Shows the full hierarchy: repos, docks, and workspaces with status,
branch, PR, and agent.

### 6. Clean up

```
bay ws close fix-auth-header
```

Bay checks for uncommitted/unpushed work, then removes the worktree
and archives the workspace. When the last workspace is closed, a
placeholder `~` window appears to keep the tmux session alive.

For a full interactive walkthrough, see the [Tutorial](tutorial.md).

## Concepts

### Repos

A **repo** is a local git checkout that bay creates worktrees from.
Register repos with `bay repo add` — bay validates the path is a git
repository and stores the absolute path in config.

```
bay repo add myproject ~/projects/myproject
bay repo add myproject ~/projects/myproject --url git@github.com:org/repo.git
bay repo ls
bay repo remove myproject
bay repo remove myproject --force    # also removes docks using this repo
```

### Docks

A **dock** is a named tmux session that groups related workspaces. You
might have a `dev` dock for one project and a `ops` dock for another.
Each dock has defaults: which repo to use, and optionally which agent
to launch and which template to inject.

When a dock is created, its tmux session starts with a placeholder `~`
window. This window is automatically cleaned up when you create your
first workspace, and recreated when you close your last — keeping the
tmux session alive so you don't have to reattach.

```
bay dock new dev --repo myproject
bay dock new dev --repo myproject --agent claude   # set default agent
bay dock ls
bay dock close dev
bay dock close dev --force
```

### Workspaces

A **workspace** is a working directory with metadata. It comes in two
types:

- **Worktree** — bay creates a git worktree from a configured repo.
  Bay owns the lifecycle: creation, safety checks on close, cleanup.
  This is the default.
- **External** — bay points at an existing directory you own. Bay
  manages tmux recovery and agent config, but doesn't create or delete
  the directory.

Each workspace gets an auto-assigned **ID** (`w1`, `w2`, ...) and a
display **name** that defaults to the auto-abbreviated branch name
(`feature/refactor-memory` becomes `refactor-memory`). You can rename
it with `bay ws rename`.

### Windows and panes

A workspace can have multiple **windows** — separate tmux tabs that
share the same working directory. The first window is created
automatically with `bay ws new`. Add more with `bay win open`.

Each window can have multiple **panes** — splits within the window.
Add them with `bay pane add`. Bay tracks windows and panes it creates
for recovery; manual tmux splits are not tracked and won't survive
`bay recover`.

### Statuses

- **idle** — workspace created, no branch yet
- **active** — branch set, work in progress
- **done** — work complete, ready to close (window dimmed in tmux)

### Naming and references

Workspaces can be referenced by name (`auth-fix`), by ID (`w3`), or by
full qualifier (`labs:w3`). The short form works when the name or ID is
unambiguous across docks. Names must match `[a-zA-Z0-9_-]+`.

`self` refers to the current workspace (resolved from your tmux
window and working directory).

## Configuration

Bay's config lives at `~/.config/bay/config.toml`. You can manage it
entirely through CLI commands — no need to edit directly.

### Agents

Define each agent type you use:

```toml
[agents.claude]
command = "claude"
config_file = "CLAUDE.local.md"

[agents.codex]
command = "codex"
config_file = "AGENTS.local.md"
```

`command` is what bay runs in the terminal. `config_file` is the
filename bay writes the template into — it must be gitignored in your
repos.

### Editor

Bay resolves your editor from these sources, in order: the `[editor]`
config section, `$VISUAL`, `$EDITOR`, or by probing for
cursor/code/zed/nvim/vim on `$PATH`.

```toml
[editor]
command = "cursor"
gui = true           # detach from terminal (default: auto-detected)
```

`gui = true` means bay launches the editor and returns immediately
(suitable for VS Code, Cursor, Zed). When `gui` is false or omitted
for a terminal editor, bay runs it in the foreground.

### Repos

Managed via `bay repo add` / `bay repo remove`:

```toml
[repos.labs]
path = "~/projects/labs"
worktree_dir = "~/projects/labs-worktrees"    # optional
```

`worktree_dir` is where bay creates worktrees for this repo. If
omitted, it defaults to `{path}-worktrees`. The directory is created
on first use and cleaned up when the last worktree is removed.

### Docks

Managed via `bay dock new`:

```toml
[docks.labs]
repo = "labs"
agent = "claude"                                       # optional
agent_args = ["--add-dir", "~/shared-data"]            # optional
agent_config_template = "~/.config/bay/templates/labs.md"  # optional
```

`agent` sets the default agent for `bay ws new --agent` (no argument).
`agent_args` are appended to the agent launch command. Bay always writes
a standard preamble to the agent config file (see [Templates](#templates)
below), so agents receive bay instructions even without a user template.

### Monitor and keybindings

```toml
[monitor]
interval_seconds = 3

[keybinding]
add_prompt = "P"           # prefix + P: capture a waiting pattern
next_waiting = "M-w"       # option/alt + w: jump to next waiting agent
```

## Templates

Every agent config file starts with a **standard preamble** that bay
always injects. The preamble gives the agent its workspace identity and
the essential bay commands. User templates are appended after the
preamble.

### Preamble (always injected)

```markdown
# Bay Workspace

You are in bay workspace {workspace_name} ({workspace_id}) in the {dock} dock.
Working directory: {workspace_path}

## Updating bay

When you create a branch, update bay so it can track your work and
name your tmux window:

    !bay ws update self --branch <branch-name>

When you open a PR:

    !bay ws update self --pr <number>

When your work is complete:

    !bay ws update self --status done

## Other bay commands

    !bay ws show self              # see workspace details
    !bay ls                        # see all workspaces across docks
    !bay win open self --shell     # open a shell window for this workspace
```

This preamble is defined in `internal/engine/agent.go` and cannot be
overridden. Variables (`{workspace_name}`, `{dock}`, etc.) are
substituted with the actual values at workspace creation time. If a
dock has a user template configured, its content follows the preamble
in the same config file.

### User templates

User templates inject additional project context into agent sessions.
They're plain text files (typically markdown) with variable
substitution.

### Available variables

| Variable | Value |
|----------|-------|
| `{workspace_id}` | `w1`, `w2`, etc. |
| `{workspace_name}` | Display name |
| `{workspace_type}` | `worktree` or `external` |
| `{dock}` | Dock name |
| `{workspace_path}` | Absolute path to workspace directory |
| `{dock_repo}` | Absolute path to the repo |
| `{dock_worktree_dir}` | Absolute path to the worktree directory |

### Example template

`~/.config/bay/templates/labs.md`:

```markdown
You are working in the labs project.

Your workspace is {workspace_name} (ID: {workspace_id}) in the
{dock} dock.

When you create a branch or open a PR, run:
  !bay ws update self --branch <branch-name> --pr <number>

When your work is complete, run:
  !bay ws update self --status done
```

Bay writes the rendered template to the agent's config file (e.g.,
`CLAUDE.local.md`) in the workspace directory at creation time. The
agent picks it up automatically on startup.

### When templates take effect

Templates are rendered once at workspace creation. If you edit a
template, existing workspaces won't see the change until you run
`bay win restart` to regenerate the config and relaunch the agent.

## Command reference

### Repos

```
bay repo add <name> <path>                  # register existing local repo
bay repo add <name> <path> --url <git-url>  # clone then register
bay repo add <name> <path> --force          # skip git repo check
bay repo ls                                 # list repos and their docks
bay repo remove <name>                      # remove (shows what --force would delete)
bay repo remove <name> --force              # remove repo + all its docks
```

### Docks

```
bay dock new <name> --repo <r>              # create dock + tmux session
bay dock new <name> --repo <r> --agent <a>  # with default agent
bay dock ls                                 # list docks (current dock if inside one)
bay dock close <name>                       # close all workspaces + kill session
bay dock close <name> --force               # skip safety checks
bay dock recover <name>                     # recover a single dock
```

### Workspaces

```
bay ws new [dock]                           # new worktree + shell (default)
bay ws new [dock] --agent                   # new worktree + dock's default agent
bay ws new [dock] --agent codex             # new worktree + specific agent
bay ws new [dock] --name <n>                # with display name
bay ws new [dock] --repo <r>                # override dock's repo
bay ws new [dock] --dir <path>              # external workspace
bay ws close <name|self>                    # close (safety checks)
bay ws close <name|self> --force            # skip safety checks
bay ws show <name|self>                     # detailed view
bay ws show <name|self> --json             # machine-readable JSON output
bay ws update <name|self> --branch <b>      # update metadata
bay ws update <name|self> --pr <n>
bay ws update <name|self> --status <s>      # idle, active, done
# At least one of --branch, --pr, or --status is required.
bay ws rename <name|self> <new-name>        # permanent rename
```

### Windows

```
bay win open <workspace>                    # add window (dock default agent)
bay win open <workspace> --shell            # add shell window
bay win open <workspace> --agent <a>        # add window with specific agent
bay win open <workspace> --cmd "..."        # add window running a command
bay win close [self|workspace]              # close window (not workspace)
bay win close [self|workspace] --window N   # close specific window
bay win restart [self|workspace]            # kill + respawn all panes
bay win restart [self|workspace] --window N # restart specific window
```

### Panes

```
bay pane add --shell                        # shell pane, vertical split
bay pane add --shell --split h              # horizontal split
bay pane add --agent <a>                    # agent pane
bay pane add --cmd "..."                    # command pane
```

### Editor

```
bay edit [name|self]                        # open workspace in editor
bay edit --all                              # multi-root: all workspaces in one editor
```

Resolves the editor from config `[editor].command`, `$VISUAL`,
`$EDITOR`, or probes cursor/code/zed/nvim/vim. GUI editors detach;
terminal editors run in the foreground.

### Shell

```
bay shell                                   # split pane with shell in current workspace
bay shell <name>                            # new tmux window with shell
bay shell <name> --window                   # force new window instead of split
```

> **When to use which:**
>
> - `bay shell` -- quick split pane in the current window. This is the
>   most common way to get a shell alongside what you're doing.
> - `bay shell <name>` -- new tmux window for a different workspace.
>   Use this when you want to jump to another workspace's directory in
>   a full window.
> - `bay pane add --shell` -- same effect as `bay shell`, but lets you
>   control split direction with `--split h` (horizontal) or
>   `--split v` (vertical).
> - `bay win open <workspace> --shell` -- same effect as
>   `bay shell <name>`, but more explicit. Use it when you want a new
>   window for a workspace you specify by name or ID.

### Navigation

```
bay go                                      # fzf picker of all windows
bay go <query>                              # fuzzy match name/branch/PR
bay go --waiting                            # picker filtered to waiting
bay go --next-waiting                       # cycle to next waiting
```

The picker shows a type tag per window: `[agent]`, `[shell]`, or
`[cmd]`.

### Status line

```
bay status-line <field>                     # output workspace field for tmux
```

Fields: `name`, `branch`, `pr`, `status`, `dock`, `full`.

Use this in your tmux config to show workspace info in the status bar:

```tmux
set -g status-right '#(bay status-line full)'
```

### Global

```
bay ls                                      # full repo/dock/workspace tree
bay ls --json                               # machine-readable JSON output
bay recover                                 # reconstruct after reboot
bay doctor                                  # health checks
bay setup                                   # first-time setup
bay version                                 # show version + commit
bay monitor start|stop|status               # manage pane monitor
bay add-prompt                              # capture waiting pattern
bay completion bash|zsh|fish                # generate completion script
```

## Listing views

`bay ls` shows the full hierarchy:

```
repo myproject (~/projects/myproject)
  dock dev
    w1  auth          feature/auth  #42  active  [shell]
    w2  w2            —                  idle    [agent]
  dock staging
    w1  deploy        release/v2    #87  done    [shell]
```

`bay dock ls` shows just docks (narrows to current dock if inside one):

```
dock dev (repo=myproject, agent=claude)
  w1  auth          feature/auth  #42  active
  w2  w2            —                  idle
```

`bay repo ls` shows repos with their docks:

```
myproject  ~/projects/myproject  (worktrees: ~/projects/myproject-worktrees)
  docks: dev, staging
```

## Machine-readable output

Both `bay ls` and `bay ws show` accept a `--json` flag for scripting
and orchestration. The JSON output includes the same information as the
human-readable view but in a structured format suitable for piping to
`jq` or consuming from other tools.

```
bay ls --json | jq '.[] | .workspaces[] | select(.waiting)'
bay ws show auth-fix --json | jq '.branch'
```

## Waiting detection

Bay runs a background monitor that watches for agents waiting for input
(permission prompts, confirmation dialogs, etc.). When detected, the
tmux window is highlighted in the status bar.

### How it works

The monitor captures the last few lines of each agent pane every few
seconds, strips ANSI codes, and matches against regex patterns in
`~/.config/bay/bay-prompts.txt`:

```
# Claude Code
Allow.*Deny
# Codex
\[Y/n\]
# Generic
\(y/n\)
Do you want to proceed
```

### Adding patterns

When you see a prompt the monitor doesn't catch:

1. Press `prefix + P` (configurable) to capture the current pane text
   and add it as a pattern. Edit the file afterward to generalize it
   to a regex.
2. Or run `bay add-prompt` interactively.

### Managing the monitor

```
bay monitor status
bay monitor start
bay monitor stop
```

The monitor is normally started by `bay recover`. You shouldn't need to
manage it directly.

## Recovery

Bay is designed for everything to be reconstructable after a reboot.

### What to do after a reboot

```
bay recover
```

This recreates all tmux sessions, windows, and panes from the manifest.
Worktrees are already on disk (they survive reboot). Bay relaunches
shells, regenerates agent config files for agent windows, starts the
monitor, and prints `tmux attach` commands so you can reconnect.

Recovery is idempotent — you can run it multiple times safely. It
detects and reuses existing tmux state rather than creating duplicates.
Panes with live foreground processes are left alone.

### What to do if you closed your terminal

Same thing: `bay recover`. Closing the terminal detaches tmux — the
sessions may still be alive. If they are, `bay recover` reconnects to
them. If they're gone (e.g., after a full reboot), it recreates them.

## Cross-repo workspaces

A dock has a default repo, but you can override it per workspace with
`--repo`. This lets you work on PRs across two repos in a single dock.

```
# Register both repos
bay repo add frontend ~/projects/frontend
bay repo add backend ~/projects/backend

# Create a dock with a default repo
bay dock new feature-work --repo frontend --agent claude

# Create workspaces in different repos within the same dock
bay ws new feature-work --name ui-changes          # uses frontend (dock default)
bay ws new feature-work --name api-changes --repo backend   # overrides to backend
```

Both workspaces live in the same tmux session, so you can switch
between them with `bay go`, see them side by side in `bay ls`, and
close them independently. Each workspace gets its own worktree from
its respective repo.

## Per-repo setup (agents only)

If you use agents, each repo needs the agent config file in its
`.gitignore`. `bay repo add` checks this automatically and prints
the commands you need to run for any missing entries.

Bay also checks at workspace creation time and will refuse (with a
clear message) if the entry is missing.

For Codex, also add to `~/.codex/config.toml`:

```toml
project_doc_fallback_filenames = ["AGENTS.local.md"]
```

## Shell completions

`bay setup` installs shell completions automatically. Tab completion
covers workspace names and IDs, dock names, repo names, agent types,
status values, and split directions.

To regenerate manually:

```
bay completion bash > /usr/local/etc/bash_completion.d/bay
bay completion zsh > "${fpath[1]}/_bay"
bay completion fish > ~/.config/fish/completions/bay.fish
```

## Tips and troubleshooting

**"I closed my terminal and everything is gone."**
It's not gone. Run `bay recover`. This is by design — bay stores
everything it needs in the manifest and recreates tmux state on demand.
Your worktrees and code are still on disk.

**"My agent doesn't see my template changes."**
Templates are rendered at workspace creation time, not live-reloaded.
Run `bay win restart self` to regenerate the config and relaunch.

**"`bay ws close` refuses and I just want it gone."**
The safety checks prevent losing work. If you're sure (e.g., the
branch was already merged), use `--force`.

**"The waiting indicator isn't working."**
Check `bay monitor status`. If running, the prompt text probably
doesn't match any pattern. Use `prefix + P` or `bay add-prompt` to
capture it, then edit `bay-prompts.txt` to generalize to a regex.

**"I want to use bay with an agent that isn't Claude or Codex."**
Add an agent definition to your config:

```toml
[agents.myagent]
command = "myagent-cli"
config_file = ".myagent.local.md"
```

Then reference it with `--agent myagent` or set it as a dock default.

**"Two docks use the same repo — is that okay?"**
Yes. They share the same worktree directory but each workspace gets
its own subdirectory. No conflicts.

**"I renamed a workspace but the agent still shows the old name."**
`bay ws rename` updates the manifest and tmux immediately, but the
agent's config file contains the old name. Run `bay win restart` to
regenerate.

**"What's the `~` window?"**
That's a placeholder. It keeps the tmux session alive when you have no
workspaces open. It disappears automatically when you create a
workspace, and reappears when you close your last one. If you type in
it, bay treats it as a regular shell and leaves it alone.
