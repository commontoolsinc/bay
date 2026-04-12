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

### Create workspaces and surfaces

From inside a dock:
```
bay ws new                  # shell workspace (default)
bay ws new auth-fix         # with a display name
bay ws new --branch fix-it  # create and checkout a branch
bay agent                   # launch default agent (own window)
bay agent claude            # launch a specific agent
bay shell                   # open a shell (own window)
bay edit                    # open the editor
```

### Navigate

```
bay go                      # pick a surface (agent/shell/editor)
bay ws go                   # pick a workspace
```

Or use keybindings: Option+j/k to cycle surfaces, Option+Shift+J/K to cycle
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
`refactor-memory`). You can rename it with `bay rename` (or `bay ws rename`).

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
- **done** -- branch has been merged into the default branch

Status transitions are automatic: bay detects branches via `git
rev-parse` on every display, and detects merges via the monitor
daemon's background `git fetch` plus a fast `merge-base --is-ancestor`
check on each `bay ls`.

### Naming and references

Workspaces can be referenced by name (`auth-fix`), by full qualifier
(`dev:auth-fix`), or by the keyword `self` (resolved from your tmux
pane and working directory).

Names must match `[a-zA-Z0-9_-]+`.

## Configuration

Bay's config lives at `~/.config/bay/config.toml`. It holds user
preferences and optional overrides — not instance state. Repos, docks,
and workspaces are tracked in the manifest (`~/.local/share/bay/manifest.json`).

### Zero-config behavior

No config file is needed. `bay ws new` auto-bootstraps: it detects the
CWD git repo, creates a dock, probes for agents and editors on PATH,
and starts working. A config file is only needed to set preferences.

### Defaults

```toml
default_agent = "claude"
default_editor = "cursor"
```

Set via `bay setup` or `bay config editor <name>`. Built-in agents
(claude, codex, gemini) and editors (cursor, code, zed, nvim, vim)
don't need config — bay knows their commands, resume args, and GUI
detection. Run `bay help config` for the full schema.

### Per-dock overrides (optional)

Override the default agent or add agent args for a specific dock:

```toml
[docks.dev]
agent = "codex"                                # override default agent
agent_args = ["--add-dir", "~/shared-data"]    # extra agent arguments
terminal = "ghostty"                           # host terminal app
```

### Custom agents (optional)

Add agents beyond the built-in three:

```toml
[agents.my-agent]
command = "my-agent-cli"
resume_args = "--resume"
project_file = ".my-agent.md"
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

`bay repo add` calls `repo init` automatically. `bay doctor` flags
repos missing bay awareness.

### .worktreeinclude

Bay reads `.worktreeinclude` as a newline-delimited list of repo-root
paths to copy into each new worktree. At worktree creation time, bay
copies any listed files that exist into the new worktree.

### Session resumption

Built-in agents have resume args (e.g., `--continue` for Claude Code).
On recovery (`bay recover`), bay appends these automatically. Claude
Code binds sessions to project directories, so `--continue` in the
same worktree resumes the right conversation.

### Agent protection

Closing agent surfaces prompts for confirmation, since agents carry
valuable conversation context.

## Command reference

### Workspace management

Across all create-verbs the rule is the same: the positional names the
thing being created. Container (dock, workspace) is selected via flags
or, when omitted, inherited from the current tmux session.

```
bay ws new [name]                           # new workspace (shell default)
bay ws new [name] --dock <d>                # target a specific dock
bay ws new [name] --branch <b>              # create and checkout branch
bay ws new [name] --repo <r>                # override dock's repo
bay ws new [name] --dir <path>              # external workspace
bay ws close [name]                         # close + delete pushed branch ('self' for current)
bay ws close [name] --force                 # skip safety checks (keeps unpushed branches)
bay ws close --done                         # close all done workspaces
bay ws close --done --force                 # force close all done
bay ws show [name]                          # detailed view (default: current)
bay ws show [name] --json                   # machine-readable
bay ws rename [name] <new-name>             # rename (defaults to current workspace)
bay ws tree                                 # tree of current dock
bay ws go [query]                           # workspace picker (intra-dock)
bay ws go --waiting                         # filter to waiting workspaces
bay ws go --next-waiting                    # cycle to next waiting workspace
bay ws next                                 # next workspace in dock
bay ws prev                                 # prev workspace in dock
```

### Surface management

`bay surface new` (or `bay sf new`) has subcommands for each surface type:

```
bay surface new shell [name]               # shell in new window (default)
bay surface new agent [type] [name]        # agent in new window
bay surface new cmd "<command>" [name]     # command in new window
bay surface new edit [workspace]           # editor surface
bay surface new shell [name] --pane        # split into current window
bay surface new shell [name] --split h     # horizontal split
bay surface new shell [name] --ws <w>      # target a different workspace
bay surface close <name>                   # close a surface (prompts on agents)
bay surface close <name> --force           # skip the agent confirmation prompt
bay surface show [name]                    # show details (defaults to current)
bay surface rename [old] <new>             # rename (defaults to current surface)
bay surface go [query]                     # surface picker (intra-workspace)
bay surface go --index <n>                 # jump to surface by index
bay surface go --next-waiting              # next waiting surface
bay surface next                           # next surface in workspace
bay surface prev                           # prev surface in workspace
```

`sf` is an alias for `surface`:
```
bay sf new shell
bay sf close shell-2
```

### Top-level shortcuts

`bay new` mirrors `bay surface new`:

```
bay new shell [name]            # shell in new window
bay new agent [type] [name]     # agent in new window
bay new cmd "<command>" [name]  # command in new window
bay new edit [workspace]        # open the editor
bay shell [name]                # shell in new window (default)
bay shell [name] --pane         # shell as split pane
bay agent [type]                # agent in new window
bay agent --pane                # agent as split pane
bay edit [workspace]            # open workspace in editor
bay edit --editor vim           # use a specific editor this time
bay edit --pane                 # editor as split pane
bay edit --all                  # open all workspaces in current dock
bay close <name>                # alias for bay surface close (prompts on agents)
bay show [name]                 # alias for bay surface show (defaults to current)
bay rename [name] <new-name>    # rename workspace (defaults to current)
bay go [query]                  # alias for bay surface go (intra-workspace)
bay go --next-waiting           # next waiting surface
```

### Config

```
bay config                      # show config-format docs (long help)
bay config edit                 # open ~/.config/bay/config.toml in your editor
bay config show                 # print the effective config
bay config path                 # print the config file path
bay config editor               # show the resolved editor command
bay config editor <name>        # set the default editor (e.g. cursor, code, nvim)
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

`rp` is an alias for `repo`.

### Docks

```
bay dock new <name> --repo <r>              # create dock + tmux session
bay dock new <name> --repo <r> --agent <a>  # with default agent
bay dock new <name> --terminal <t>          # with host terminal
bay dock ls                                 # list docks (scoped if inside one)
bay dock show <name>                        # detailed dock info
bay dock rename [old] <new>                 # rename (defaults to current dock)
bay dock close <name>                       # close all workspaces + kill session
bay dock close <name> --force               # skip safety checks
bay dock recover <name>                     # recover a single dock
```

`dk` is an alias for `dock`.

### Listing and context

```
bay ls                                      # context-sensitive tree view
bay ls -R                                   # recurse fully from current focus
bay ls -l                                   # show tmux IDs and extended detail
bay ls -s                                   # compact output (no labels or key names)
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
bay completion bash|zsh|fish                # generate completion script
bay version                                 # show version + commit
```

## Keybindings

Bay installs tmux keybindings using the Option (Meta) key. No tmux
prefix required — just press the key combo directly.

### Navigation

| Key | Action |
|-----|--------|
| `Option+j` / `Option+k` | Next / previous surface in workspace |
| `Option+g` | Surface picker (popup) |
| `Option+Shift+j` / `Option+Shift+k` | Next / previous workspace in dock |
| `Option+Shift+g` | Workspace picker (popup) |

### Creation (lowercase = window, Shift = split pane)

| Key | Action |
|-----|--------|
| `Option+s` / `Option+S` | Shell window / shell pane |
| `Option+a` / `Option+A` | Agent window / agent pane |
| `Option+e` / `Option+E` | Editor window / editor pane |
| `Option+c` | Create workspace in current dock |

### Utility

| Key | Action |
|-----|--------|
| `Option+w` | Close current pane/surface |

### Pattern

Without Shift = intra-workspace. With Shift = intra-dock. For creation
keys, lowercase opens a new window, Shift opens a split pane. The
pickers (`Option+g`, `Option+Shift+g`) open in a tmux popup.

### Installing and updating

`bay setup` installs all keybindings in `~/.tmux.conf`. It detects
conflicts with existing bindings and prompts before overwriting. Run
`bay doctor` to check if keybindings are current.

## Editor integration

`bay edit` opens your workspace in an editor and creates a tracked
**editor surface** so the editor appears in `bay go`.

```
bay edit                    # current workspace
bay edit auth-fix           # specific workspace
bay edit --editor vim       # use a specific editor this time
bay edit --all              # all workspaces in dock (multi-root)
bay edit --split v          # vertical split instead of new window
```

**GUI editors** (Cursor, VS Code, Zed) launch detached. The surface
persists until you close it with `bay close editor`.

**Terminal editors** (nvim, vim) run in their own tmux pane (a new
window by default, or a split with `--split`). When you quit the
editor, the pane closes and the surface is cleaned up automatically.

Editor resolution order: `--editor` flag, `[editor].command` in config,
`$VISUAL`, `$EDITOR`, then probing for cursor/code/zed/nvim/vim.

Configure with:
```
bay config editor cursor    # save preference
bay config editor           # check current editor
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

When you see a prompt the monitor does not catch, edit
`~/.config/bay/bay-prompts.txt` and add a regex on its own line.
The monitor reloads patterns on every cycle (default 3s), so no
restart is needed.

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

PR numbers are detected automatically via `gh pr view` whenever a
workspace has a branch but no PR recorded. The lookup happens on the
first display after creation (and again on each monitor cycle for
workspaces that haven't been checked yet). The result — including
"this branch has no PR" — is cached so the lookup runs at most once
per workspace.

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
rp myproject
  dk dev
    ws auth-fix  br=feature/auth
      sf agent   ty=agent ag=claude
      sf shell   ty=shell
      sf editor  ty=editor
```

Inside a dock but outside a workspace, it shows that dock's workspaces:

```
rp myproject
  dk dev
    ws auth-fix  br=feature/auth  n=3
    ws perf-fix  br=fix/perf      n=1
```

Outside bay context, it shows everything compactly:

```
rp myproject
  dk dev
    ws auth-fix  br=feature/auth  n=3
    ws w2                         n=1
  dk staging
    ws deploy    br=release/v2    st=done  n=1
```

Use `bay ls -R` to recurse fully. Use `bay ls -l` for tmux IDs.
Use `bay ls -s` for compact output without labels or key names.

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
resume args like `--continue` for Claude Code), recovers terminal host
apps, starts the monitor, and prints `tmux attach` commands.

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

bay ws new ui-changes --dock feature-work          # uses frontend
bay ws new api-changes --dock feature-work --repo backend
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
match any pattern. Add a regex for it to `~/.config/bay/bay-prompts.txt`
(one regex per line) — the monitor reloads patterns on every cycle.

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
the manifest.

**"My agent restarted and lost context."**
Built-in agents have resume args configured automatically (e.g.,
`--continue` for Claude Code). Bay uses these on restart and recovery.
For custom agents, set `resume_args` in the `[agents]` config section.

**"How do I see my editor in bay go?"**
Use `bay edit` to open it. This creates a tracked editor surface. If you
opened the editor outside bay, it won't appear in navigation.

## Appendix: abbreviations

| Short | Long |
|-------|------|
| `rp` | `repo` |
| `dk` | `dock` |
| `ws` | `workspace` |
| `sf` | `surface` |
| `ls` | `list` |
| `mv` | `rename` |
| `rm` | `close` |
| `cat` | `show` |

These work everywhere: `bay sf ls`, `bay ws mv`, `bay dock rm`, etc.
