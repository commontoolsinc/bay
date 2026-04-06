# Bay -- User Guide

Bay manages concurrent workspaces built on git worktrees and tmux. Each
workspace gets its own worktree and tmux surfaces -- so you can work on
multiple branches simultaneously without stash juggling or directory
cloning. Bay handles session recovery after reboot, tracks editor
windows alongside terminal panes, detects PR merges automatically, and
optionally launches AI coding agents.

## Prerequisites

- **tmux** -- bay manages tmux sessions and surfaces. Install with
  `brew install tmux` (macOS) or your system package manager. If you're
  new to tmux, the key concept is: tmux keeps terminal sessions alive
  in the background. You can detach and reattach without losing state.
  Bay leans on this heavily. See the
  [tmux Getting Started guide](https://github.com/tmux/tmux/wiki/Getting-Started).

- **git** -- for worktree-based workspaces.

- **An AI coding agent** (optional) --
  [Claude Code](https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/overview),
  [Codex](https://github.com/openai/codex), or any agent that runs in a
  terminal. Agents are opt-in per workspace.

- **gh** (optional) -- the [GitHub CLI](https://cli.github.com/).
  Enables automatic PR number detection and merge status monitoring.

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

### Zero-config: just run it

Bay works without a config file. Navigate to any git repo and create a
workspace:

```
cd ~/projects/myproject
bay ws new
```

Bay auto-detects the repo, creates a dock (tmux session), probes for an
agent on your PATH, and opens a workspace. If this is your first time,
it writes a minimal config for next time.

### With setup (recommended)

Run setup once to configure keybindings, completions, and your editor:

```
bay setup
```

This creates your config file, installs tmux keybindings, sets up shell
completions, configures your editor for `bay edit`, and compiles the
bay-focus helper (macOS).

### Explicit repo and dock

For more control, register a repo and create a named dock:

```
bay repo add myproject ~/projects/myproject
bay dock new dev --repo myproject --agent claude
tmux attach -t dev
bay ws new
```

### Create a workspace with an agent

From inside a dock:
```
bay ws new                  # shell workspace (default)
bay ws new --agent          # uses dock's default agent
bay ws new --agent codex    # specific agent
bay ws new --name auth-fix  # with a display name
bay ws new --branch fix-it  # create and checkout a branch
```

### Navigate

```
bay go                      # pick a surface (agent/shell/editor)
bay ws go                   # pick a workspace
```

Or use keybindings: Option+j/k to cycle surfaces, Option+J/K to cycle
workspaces.

### Check on things

```
bay ls                      # context-sensitive tree view
bay pwd                     # current bay location
```

### Clean up

```
bay ws close auth-fix       # safety checks for uncommitted work
bay ws close --done         # close all merged/done workspaces
```

For a full interactive walkthrough, see the [Tutorial](tutorial.md).

## Concepts

### The hierarchy: Dock > Workspace > Surface

Bay organizes your work in three levels:

```
Dock (tmux session)
  Workspace (git worktree + metadata)
    Surface (agent pane, shell pane, editor window, command pane)
```

**Surfaces** are the unit of navigation -- anything you can focus and
jump to. You don't think in "tmux windows and panes"; you think "agent",
"editor", "shell". That is what a surface is.

### Repos

A **repo** is a local git checkout that bay creates worktrees from.

```
bay repo add myproject ~/projects/myproject
bay repo add myproject ~/projects/myproject --url git@github.com:org/repo.git
bay repo ls
bay repo show myproject
bay repo remove myproject
bay repo remove myproject --force    # also removes docks using this repo
```

### Docks

A **dock** is a named tmux session that groups related workspaces. You
might have a `dev` dock for one project and an `ops` dock for another.
Each dock has defaults: which repo to use, and optionally which agent to
launch and which terminal to use.

When a dock is created, its tmux session starts with a placeholder `~`
window. This disappears when you create your first workspace and
reappears when you close your last.

```
bay dock new dev --repo myproject
bay dock new dev --repo myproject --agent claude
bay dock new dev --repo myproject --terminal ghostty
bay dock ls
bay dock show dev
bay dock close dev
bay dock close dev --force
```

### Workspaces

A **workspace** is a working directory with metadata. Two types:

- **Worktree** -- bay creates a git worktree from a configured repo.
  Bay owns the lifecycle: creation, safety checks on close, cleanup.
  This is the default.
- **External** -- bay points at an existing directory you own. Bay
  manages tmux surfaces and recovery, but does not create or delete
  the directory.

Each workspace gets a display **name** that defaults to the
auto-abbreviated branch name (`feature/refactor-memory` becomes
`refactor-memory`). You can rename it with `bay ws rename`.

### Surfaces

A **surface** is anything you can focus and jump to within a workspace.
Surfaces have a type and a backend:

| Type | What it runs |
|------|-------------|
| `agent` | An AI coding agent (Claude Code, Codex, etc.) |
| `shell` | A plain shell in the workspace directory |
| `cmd` | A specific command (`npm test`, `cargo watch`, etc.) |
| `editor` | A GUI editor (Cursor, VS Code, Zed) |

Most surfaces live in tmux (panes within windows). Editor surfaces are
GUI applications tracked by bay so they appear in navigation and
pickers.

Surfaces within a workspace that share a tmux window are in the same
**layout group**. Splitting a pane creates a surface in the same group;
`--window` creates a surface in a new group.

### Statuses

- **idle** -- workspace created, no branch yet
- **active** -- branch detected, work in progress
- **done** -- PR merged or manually marked complete

Status transitions are automatic: bay detects branches via `git
rev-parse` and merges via background `git fetch`. You can also set
status manually with `bay ws update`.

### Naming and references

Workspaces can be referenced by name (`auth-fix`), by full qualifier
(`dev:auth-fix`), or by the keyword `self` (resolved from your tmux
pane and working directory).

Names must match `[a-zA-Z0-9_-]+`.

## Configuration

Bay's config lives at `~/.config/bay/config.toml`. You can manage it
entirely through CLI commands or edit it directly.

### Zero-config behavior

If no config file exists, `bay ws new` auto-bootstraps: it detects the
CWD git repo, creates a dock, probes for agents on PATH, and saves a
minimal config. You only need a config file for customization.

### Agents

```toml
[agents.claude]
command = "claude"
resume_args = "--continue"
project_file = "CLAUDE.md"

[agents.codex]
command = "codex"

[agents.gemini]
command = "gemini"
```

Fields:
- `command` -- what bay runs in the terminal.
- `resume_args` -- flags added when restarting an agent (e.g.,
  `--continue` for Claude Code). On first launch: bare command. On
  restart or recovery: command + resume_args.
- `project_file` -- the file bay checks for bay awareness (e.g.,
  `CLAUDE.md`). Used by `bay repo init` to append a pointer to
  `bay agent-guide`.

### Editor

Bay resolves your editor from these sources, in order: `[editor].command`
in config, `$VISUAL`, `$EDITOR`, or by probing for
cursor/code/zed/nvim/vim on `$PATH`.

```toml
[editor]
command = "cursor"
gui = true           # detach from terminal (default: auto-detected)
```

GUI editors (Cursor, VS Code, Zed) are detected automatically. When
`gui` is true, `bay edit` launches the editor and returns immediately,
creating a tracked editor surface. Terminal editors run in the
foreground.

### Repos

```toml
[repos.myproject]
path = "~/projects/myproject"
worktree_dir = "~/projects/myproject-worktrees"    # optional
```

`worktree_dir` is where bay creates worktrees. Defaults to
`{path}-worktrees`.

### Docks

```toml
[docks.dev]
repo = "myproject"
agent = "claude"
agent_args = ["--add-dir", "~/shared-data"]
terminal = "ghostty"
```

Fields:
- `repo` -- default repo for workspaces in this dock.
- `agent` -- default agent for `bay ws new --agent`.
- `agent_args` -- extra arguments appended to the agent command.
- `terminal` -- host terminal app (e.g., `ghostty`, `iterm2`). Bay
  launches this terminal attached to the dock's tmux session on
  `bay dock new` and `bay recover`.

### Monitor

```toml
[monitor]
interval_seconds = 3
```

## Agent integration

### Bay awareness via `bay repo init`

Instead of generating per-workspace config files, bay uses a lightweight
awareness model. Run `bay repo init` to set up a repo:

```
bay repo init myproject     # by name
bay repo init               # infer from CWD
```

This does three things:
1. For each agent with a `project_file` (e.g., `CLAUDE.md`), appends a
   one-line pointer: *"This project uses bay. Run `bay agent-guide` for
   commands."*
2. Creates `.worktreeinclude` if missing, so gitignored files (`.env`,
   etc.) get copied to new worktrees.
3. Ensures `.gitignore` has necessary entries.

`bay repo add` calls `repo init` automatically. `bay doctor` flags
repos missing bay awareness.

### .worktreeinclude

Bay respects `.worktreeinclude` files (same format as `.gitignore`).
At worktree creation time, bay reads `.worktreeinclude` from the repo
root, finds matching gitignored files, and copies them into the new
worktree.

### Session resumption

Agent config has `resume_args` (e.g., `--continue` for Claude Code).
On restart or recovery, bay appends these to the agent command. Claude
Code binds sessions to project directories, so `--continue` in the same
worktree resumes the right conversation.

### Agent protection

Agent surfaces have valuable conversation context:
- `bay surface restart` uses `resume_args` to reconnect, not start
  fresh.
- Closing agent surfaces prompts for confirmation.

## Command reference

### Workspace management

```
bay ws new [dock]                           # new workspace (shell default)
bay ws new [dock] --agent                   # with dock's default agent
bay ws new [dock] --agent codex             # with specific agent
bay ws new [dock] --shell                   # explicit shell
bay ws new [dock] --name <n>                # with display name
bay ws new [dock] --branch <b>              # create and checkout branch
bay ws new [dock] --repo <r>                # override dock's repo
bay ws new [dock] --dir <path>              # external workspace
bay ws close <name|self>                    # close (safety checks)
bay ws close <name|self> --force            # skip safety checks
bay ws close --done                         # close all done workspaces
bay ws close --done --force                 # force close all done
bay ws show [name|self]                     # detailed view (default: self)
bay ws show [name|self] --json              # machine-readable
bay ws update <name|self> --branch <b>      # update metadata
bay ws update <name|self> --pr <n>          # (auto-detected, rarely needed)
bay ws update <name|self> --status <s>      # idle, active, done
bay ws rename <name|self> <new-name>        # permanent rename
bay ws go [query]                           # workspace picker (intra-dock)
bay ws go --waiting                         # filter to waiting workspaces
bay ws go --next-waiting                    # cycle to next waiting workspace
bay ws next                                 # next workspace in dock
bay ws prev                                 # prev workspace in dock
```

### Surface management

```
bay surface new [workspace]                 # shell split (default)
bay surface new --shell                     # explicit shell
bay surface new --agent <type>              # agent surface
bay surface new --cmd "npm test"            # command surface
bay surface new --window                    # new tmux window instead of split
bay surface new --split h                   # horizontal split (default: v)
bay surface new --name <n>                  # with custom name
bay surface close [workspace]               # close current surface
bay surface close --surface <name>          # close specific surface
bay surface restart [workspace]             # restart surface process
bay surface restart --surface <name>        # restart specific surface
bay surface go [query]                      # surface picker (intra-workspace)
bay surface go --index <n>                  # jump to surface by index
bay surface go --next-waiting               # next waiting surface
bay surface next                            # next surface in workspace
bay surface prev                            # prev surface in workspace
```

`sf` is an alias for `surface`:
```
bay sf new --shell
bay sf close
bay sf restart
```

### Top-level shortcuts

```
bay go [query]              # alias for bay surface go (intra-workspace)
bay go --index <n>          # jump to surface by index
bay go --next-waiting       # next waiting surface
bay shell [workspace]       # alias for bay surface new --shell
bay shell --window          # shell in new tmux window
bay edit [name|self]        # open workspace in editor (creates GUI surface)
bay edit --all              # open all workspaces in current dock
bay edit --set <editor>     # set default editor
bay edit --show             # show which editor would be used
bay restart [workspace]     # alias for bay surface restart
```

### Navigation summary

| Scope | Picker | Cycle | Waiting |
|-------|--------|-------|---------|
| Surfaces (intra-workspace) | `bay go` | `bay sf next/prev` | `bay go --next-waiting` |
| Workspaces (intra-dock) | `bay ws go` | `bay ws next/prev` | `bay ws go --next-waiting` |

All pickers are built-in. They support fuzzy
matching: type a query to filter, single match jumps directly, multiple
matches open an interactive picker.

### Repos

```
bay repo add <name> <path>                  # register existing local repo
bay repo add <name> <path> --url <git-url>  # clone then register
bay repo add <name> <path> --force          # skip git repo check
bay repo ls                                 # list repos and their docks
bay repo show <name>                        # detailed repo info
bay repo remove <name>                      # remove (shows what --force would delete)
bay repo remove <name> --force              # remove repo + all its docks
bay repo init [name]                        # set up bay awareness (idempotent)
```

### Docks

```
bay dock new <name> --repo <r>              # create dock + tmux session
bay dock new <name> --repo <r> --agent <a>  # with default agent
bay dock new <name> --terminal <t>          # with host terminal
bay dock ls                                 # list docks (scoped if inside one)
bay dock show <name>                        # detailed dock info
bay dock close <name>                       # close all workspaces + kill session
bay dock close <name> --force               # skip safety checks
bay dock recover <name>                     # recover a single dock
```

### Listing and context

```
bay ls                                      # context-sensitive tree view
bay ls -R                                   # recurse fully from current focus
bay ls -l                                   # show tmux IDs and extended detail
bay ls --json                               # machine-readable tree
bay ls --json --rows                        # denormalized row output
bay ls --dirty                              # only show dirty workspaces
bay pwd                                     # show current bay context
bay pwd --json                              # machine-readable context
bay status-line <field>                     # output for tmux status bar
```

### Infrastructure

```
bay setup                                   # first-time setup
bay recover                                 # reconstruct all state after reboot
bay doctor                                  # health checks
bay monitor start|stop|status               # manage background monitor
bay add-prompt                              # capture waiting detection pattern
bay completion bash|zsh|fish                # generate completion script
bay version                                 # show version + commit
```

## Keybindings

Bay installs 21 tmux keybindings, all using the Option (Meta) key. No
tmux prefix required -- just press the key combo directly.

### Surface navigation (intra-workspace)

| Key | Action |
|-----|--------|
| `Option+j` | Next surface in workspace |
| `Option+k` | Previous surface in workspace |
| `Option+g` | Surface picker (interactive popup) |
| `Option+a` | Next waiting surface |

### Workspace navigation (intra-dock)

| Key | Action |
|-----|--------|
| `Option+J` | Next workspace in dock |
| `Option+K` | Previous workspace in dock |
| `Option+G` | Workspace picker (interactive popup) |
| `Option+A` | Next waiting workspace |

### Index jump

| Key | Action |
|-----|--------|
| `Option+1` through `Option+9` | Jump to surface by index |

### Utility

| Key | Action |
|-----|--------|
| `Option+w` | Close current pane/surface |
| `Option+s` | Split a shell pane |

### Pattern

Lowercase = intra-workspace. Uppercase = intra-dock. The pickers
(`Option+g`, `Option+G`) open in a tmux popup with fuzzy filtering.

### Installing and updating

`bay setup` installs all keybindings in `~/.tmux.conf`. It detects
conflicts with existing bindings and prompts before overwriting. Run
`bay doctor` to check if keybindings are current.

## Editor integration

`bay edit` opens your workspace in a GUI or terminal editor.

```
bay edit                    # current workspace
bay edit auth-fix           # specific workspace
bay edit --all              # all workspaces in dock (multi-root)
```

For GUI editors (Cursor, VS Code, Zed), bay creates a tracked **editor
surface** so the editor appears in `bay go` and the surface picker. Bay
probes editor liveness and removes dead surfaces automatically.

For terminal editors (nvim, vim), bay runs the editor in the foreground.
With `--all`, terminal editors open the worktree parent directory so all
worktrees appear as subdirectories.

Configure with:
```
bay edit --set cursor       # save preference
bay edit --show             # check current editor
```

## Monitoring

Bay runs a background monitor that provides two services:

### Waiting detection

The monitor watches for agents waiting for input (permission prompts,
confirmation dialogs). When detected, the tmux window is highlighted.

It captures the last few lines of each agent pane, strips ANSI codes,
and matches against regex patterns in
`~/.config/bay/bay-prompts.txt`:

```
# Claude Code
Allow.*Deny
# Codex
\[Y/n\]
# Gemini
Approve\? \(y/n
# Generic
\(y/n\)
Do you want to proceed
```

When you see a prompt the monitor does not catch, press `Option+P` or
run `bay add-prompt` to capture the current pane text and add it as a
pattern. Edit the file afterward to generalize it to a regex.

### Merge detection

The monitor performs activity-gated `git fetch` for repos with recent
workspace activity (within the last couple of hours). After fetching, it
checks if workspace branches have been merged into main. When a merge is
detected:

- Workspace status transitions to `done` automatically.
- Status line shows a count of merged workspaces.
- `bay ls` and pickers highlight merged workspaces.
- Navigating to a merged workspace shows a suggestion to close it.

No fetches happen for idle repos or workspaces without recent activity.

### PR detection

The monitor detects PR numbers automatically via `gh pr view` when a
workspace has a branch but no PR recorded. The PR number is cached and
never re-fetched. This replaces manual `bay ws update self --pr <n>`.

### Managing the monitor

```
bay monitor status          # check if running
bay monitor start           # start background monitor
bay monitor stop            # stop monitor
```

The monitor is started automatically by `bay recover`. You rarely need
to manage it directly.

## Listing views

`bay pwd` shows your current location:

```
repo myproject / dock dev / workspace auth-fix / surface agent
```

`bay ls` is context-sensitive. The display adapts to where you run it:

Inside a workspace, it expands surfaces:

```
repo myproject
  dock dev
    workspace auth-fix  branch=feature/auth  status=active
      surface agent  [agent]
      surface shell  [shell]
      surface editor [editor]
```

Inside a dock but outside a workspace, it shows that dock's workspaces:

```
dock dev
  auth-fix    feature/auth     #42   active   surfaces=3
  perf-fix    fix/perf-issue         idle     surfaces=1
```

Outside bay context, it shows everything compactly:

```
repo myproject
  dock dev
    auth-fix  feature/auth  #42  active  surfaces=3
    w2        --                  idle   surfaces=1
  dock staging
    deploy    release/v2         done   surfaces=1
```

Use `bay ls -R` to recurse fully. Use `bay ls -l` for tmux IDs.

## Machine-readable output

`bay pwd`, `bay ls`, and `bay ws show` all accept `--json`.

```
bay pwd --json | jq '.workspace'
bay ls --json | jq '.repos[].docks[].workspaces[] | select(.status == "done")'
bay ls --json --rows | jq '.[] | select(.workspace_waiting)'
bay ws show auth-fix --json | jq '.branch'
```

## Status line

Use `bay status-line` in your tmux config to show workspace info:

```tmux
set -g status-right '#(bay status-line full)'
```

Fields: `name`, `branch`, `pr`, `status`, `dock`, `merged`, `full`.

The `merged` field shows a count of done/merged workspaces in the
current dock (e.g. "2 merged"). Useful for a status bar reminder to
clean up.

## Recovery

Bay is designed for everything to be reconstructable after a reboot.

### After a reboot

```
bay recover
```

This recreates all tmux sessions, windows, and panes from the manifest.
Worktrees are already on disk. Bay relaunches shells and agents (with
`resume_args` for agents that support it), recovers terminal host apps,
starts the monitor, and prints `tmux attach` commands.

Recovery is idempotent -- run it multiple times safely. It detects and
reuses existing tmux state. Panes with live foreground processes are
left alone.

### After closing your terminal

Same thing: `bay recover`. The tmux sessions may still be alive (tmux
survives terminal close). If they are, recovery reconnects. If they're
gone, it recreates them.

### Single-dock recovery

```
bay dock recover dev
```

Or run `bay recover` from inside a dock to recover just that dock.

## Cross-repo workspaces

A dock has a default repo, but you can override per workspace:

```
bay repo add frontend ~/projects/frontend
bay repo add backend ~/projects/backend
bay dock new feature-work --repo frontend --agent claude

bay ws new feature-work --name ui-changes          # uses frontend
bay ws new feature-work --name api-changes --repo backend
```

Both workspaces live in the same tmux session. Navigate between them
with `bay ws go`, see them together in `bay ls`.

## bay-focus helper (macOS)

On macOS, bay can switch between tmux and GUI editors across Spaces
(virtual desktops). The `bay-focus` Swift helper enables this.

`bay setup` compiles the helper automatically. Requirements:
- Accessibility permission (System Settings > Privacy & Security)
- Ctrl+1..9 Space shortcuts enabled in System Settings

Without the helper, everything still works -- you just switch Spaces
manually with Cmd+Tab. Nothing breaks.

Run `bay doctor` to check helper status and Accessibility permission.

## Shell completions

`bay setup` prints a snippet for your shell rc file. Tab completion
covers workspace names, dock names, repo names, agent types, status
values, and surface names.

Add to your `.zshrc` or `.bashrc`:

```bash
if command -v bay > /dev/null ; then
  source <(bay completion zsh)    # or bash
fi
```

For fish, add to `~/.config/fish/config.fish`:

```fish
if command -v bay > /dev/null
  bay completion fish | source
end
```

## Tips and troubleshooting

**"I closed my terminal and everything is gone."**
It's not gone. Run `bay recover`. Bay stores everything in the manifest
and recreates tmux state on demand. Your worktrees and code are on disk.

**"bay ws close refuses and I just want it gone."**
Safety checks prevent losing work. If you're sure (e.g., the PR was
merged), use `--force`. Or use `bay ws close --done` to batch-close all
merged workspaces.

**"The waiting indicator isn't working."**
Check `bay monitor status`. If running, the prompt text probably doesn't
match any pattern. Use `bay add-prompt` to capture it, then edit
`bay-prompts.txt` to generalize to a regex.

**"I want to use bay with an agent that isn't Claude or Codex."**
Add an agent definition to your config:

```toml
[agents.myagent]
command = "myagent-cli"
resume_args = "--resume"
project_file = ".myagent.md"
```

Then reference it with `--agent myagent` or set it as a dock default.

**"Two docks use the same repo -- is that okay?"**
Yes. They share the same worktree directory but each workspace gets its
own subdirectory.

**"How do I update my keybindings after a bay upgrade?"**
Run `bay setup` again. It replaces the bay keybinding block in
`~/.tmux.conf` while preserving your other settings.

**"What's the ~ window?"**
A placeholder that keeps the tmux session alive when no workspaces are
open. It disappears when you create a workspace and reappears when you
close your last one.

**"How does bay know my branch and PR without me telling it?"**
Branch is detected via `git rev-parse` on every sync cycle. PR number
is detected via `gh pr view` once a branch exists. Both are cached in
the manifest. You rarely need `bay ws update` for these fields.

**"My agent restarted and lost context."**
If the agent supports session resumption (like Claude Code's
`--continue`), set `resume_args` in the agent config. Bay uses these on
restart and recovery.

**"How do I see my editor in bay go?"**
Use `bay edit` to open it. This creates a tracked editor surface. If you
opened the editor outside bay, it won't appear in navigation.
