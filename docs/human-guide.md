# Bay — User Guide

Bay manages concurrent workspaces for AI coding agents. It handles tmux
sessions, git worktrees, agent config injection, and full session
recovery after reboot — so you can run multiple agents on separate PRs
without managing the plumbing by hand.

## Prerequisites

- **tmux** — bay manages tmux sessions and windows. Install with
  `brew install tmux` (macOS) or your system package manager. If you're
  new to tmux, the key concept is: tmux keeps terminal sessions alive
  in the background. You can detach and reattach without losing state.
  Bay leans on this heavily.

- **An AI coding agent** — bay works with
  [Claude Code](https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/overview),
  [Codex](https://github.com/openai/codex), or any agent that runs in a
  terminal. You can also use bay without agents — just for tmux/worktree
  management with shells.

- **git** — for worktree-based workspaces.

- **fzf** (optional) — enables the fuzzy picker in `bay go`. Install
  with `brew install fzf`. Bay falls back to a plain list without it.

## Installation

```
# Homebrew (macOS)
brew install commontoolsinc/tap/bay

# Pre-built binary (macOS/Linux)
# Download from GitHub releases

# From source
go install github.com/commontoolsinc/bay@latest
```

## Quick start

### 1. Run setup

```
bay setup
```

This walks you through creating your config file, defining agents and
repos, setting up your first dock, installing tmux keybindings, and
shell completions.

### 2. Create a workspace

```
bay ws new labs
```

This creates a git worktree from the dock's default repo and launches
your agent in a new tmux window. You're now in a fresh checkout, ready
to work.

### 3. Do your work

Interact with the agent in the tmux window. When the agent creates a
branch or opens a PR, it should update bay's tracking:

```
bay ws update self --branch fix-auth-header --status active
bay ws update self --pr 347
```

(Your agent template can include instructions to do this automatically.)

### 4. Check on things

From any terminal:

```
bay ls
```

This shows all your workspaces across all docks, with status, branch,
PR, and whether any agent is waiting for input.

### 5. Clean up

```
bay ws close fix-auth-header
```

Bay checks for uncommitted/unpushed work, then removes the worktree and
archives the workspace.

## Concepts

### Docks

A **dock** is a named tmux session that groups related workspaces. You
might have a `labs` dock for one project and a `server` dock for
another. Each dock has defaults: which repo to use, which agent to
launch, and which template to inject.

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

## Configuration

Bay's config lives at `~/.config/bay/config.toml`. It has four sections:
agents, repos, docks, and settings.

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

### Repos

Define repos you work in:

```toml
[repos.labs]
path = "~/projects/labs"
worktree_dir = "~/projects/labs-worktrees"    # optional

[repos.ct-server]
path = "~/projects/ct-server"
# worktree_dir defaults to ~/projects/ct-server-worktrees
```

`worktree_dir` is where bay creates worktrees for this repo. If
omitted, it defaults to `{path}-worktrees`.

### Docks

Define your working environments:

```toml
[docks.labs]
repo = "labs"                                  # references [repos.labs]
agent = "claude"                               # default agent type
agent_args = ["--add-dir", "~/shared-data"]    # extra agent flags
agent_config_template = "~/.config/bay/templates/labs.md"

[docks.research]
repo = "labs"
agent = "claude"
agent_config_template = "~/.config/bay/templates/research.md"
```

`repo` references a `[repos.*]` entry. `agent` sets the default — you
can override per workspace with `--agent`. `agent_args` are appended to
the agent launch command for all workspaces in this dock.

### Monitor and keybindings

```toml
[monitor]
interval_seconds = 3               # how often to check for waiting agents

[keybinding]
add_prompt = "P"                   # prefix + P: capture a waiting pattern
next_waiting = "w"                 # prefix + w: jump to next waiting agent
```

## Templates

Templates inject project context into agent sessions. They're plain
text files (typically markdown) with variable substitution.

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

## Usage

### Creating workspaces

```
bay ws new labs                          # worktree, default repo and agent
bay ws new labs --name auth-fix          # with a display name
bay ws new labs --repo ct-server         # worktree from a different repo
bay ws new labs --dir ~/projects/foo     # external workspace
bay ws new labs --shell                  # shell instead of agent
bay ws new labs --agent codex            # override the default agent
```

### Managing windows and panes

```
bay win open auth-fix --shell            # shell window in same worktree
bay win open auth-fix --agent codex      # second agent, different type
bay win open auth-fix --cmd "npm test --watch"

bay pane add --shell --split v           # shell pane, vertical split
bay pane add --cmd "tail -f app.log"     # log tail pane

bay win close self                       # close just this window
bay win restart self                     # restart all panes in this window
```

### Navigating

```
bay go                                   # fzf picker of all windows
bay go auth-fix                          # jump to workspace by name
bay go 347                               # jump by PR number
bay go --waiting                         # picker filtered to waiting agents
bay go --next-waiting                    # cycle to next waiting agent
```

`bay go --next-waiting` is designed to be bound to a tmux key (e.g.,
`prefix + w`) for rapid triage of waiting agents.

### Updating workspace metadata

```
bay ws update self --branch feature-x --status active
bay ws update self --pr 234
bay ws update self --status done
bay ws update auth-fix --status done     # from outside
```

This updates bay's tracking and refreshes tmux window names. It does
not modify git state — the agent (or you) already did the git work.

### Listing and inspecting

```
bay ls                                   # all workspaces across all docks
bay ws show self                         # detailed view of current workspace
bay ws show auth-fix                     # detailed view by name
bay dock ls                              # list docks and their workspaces
```

### Closing workspaces

```
bay ws close auth-fix                    # close by name
bay ws close self                        # close from inside
bay ws close self --force                # skip safety checks
bay dock close labs                      # close all workspaces in a dock
bay dock close labs --force              # force close entire dock
```

For worktree workspaces, bay checks for uncommitted changes and unpushed
commits before closing. Use `--force` to override.

For external workspaces, bay just closes windows and removes its config
file. The directory is untouched.

### Renaming

```
bay ws rename w3 mem-refactor
bay ws rename self mem-refactor
```

Overrides the auto-abbreviated name permanently. The change shows up
immediately in tmux window titles.

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
bay monitor status                       # check if running
bay monitor start                        # start manually
bay monitor stop                         # stop
```

The monitor is normally started by `bay recover`. You shouldn't need to
manage it directly.

### Shell completions

`bay setup` installs shell completions automatically. If you need to
regenerate them (e.g., after switching shells):

```
bay completion bash                      # or zsh, fish
```

This prints the completion script to stdout. Pipe it to the appropriate
file for your shell, or use `bay setup` to reinstall.

## Recovery

Bay is designed for everything to be reconstructable after a reboot.

### What to do after a reboot

```
bay recover
```

This recreates all tmux sessions, windows, and panes from the manifest.
Worktrees are already on disk (they survive reboot). Bay regenerates
agent config files, relaunches agents, starts the monitor, and prints
`tmux attach` commands so you can reconnect.

Recovery is idempotent — you can run it multiple times safely. It
detects and reuses existing tmux state rather than creating duplicates.

### What to do if you closed your terminal

Same thing: `bay recover`. Closing the terminal detaches tmux — the
sessions may still be alive. If they are, `bay recover` reconnects to
them. If they're gone (e.g., after a full reboot), it recreates them.

## Per-repo setup

Each repo you use with bay needs the agent config file in its
`.gitignore`:

```
# For Claude Code
echo "CLAUDE.local.md" >> .gitignore

# For Codex
echo "AGENTS.local.md" >> .gitignore
```

Bay checks this before writing config files and will refuse (with a
helpful message) if the entry is missing.

For Codex, also add to `~/.codex/config.toml`:

```toml
project_doc_fallback_filenames = ["AGENTS.local.md"]
```

## Tips and troubleshooting

**"I closed my terminal and everything is gone."**
It's not gone. Run `bay recover`. This is by design — bay stores
everything it needs in the manifest and recreates tmux state on demand.
Your worktrees and code are still on disk.

**"My agent doesn't see my template changes."**
Templates are rendered at workspace creation time, not live-reloaded.
Run `bay win restart self` (or `bay win restart <name>` from outside)
to regenerate the config file and relaunch the agent.

**"I edited the template but new workspaces look wrong."**
Check that you edited the external template file (e.g.,
`~/.config/bay/templates/labs.md`), not something else. Verify the
`agent_config_template` path in your dock config points to the right
file.

**"`bay ws close` refuses and I just want it gone."**
The safety checks exist to prevent losing work. If you're sure (e.g.,
the branch was already merged), use `--force`. Otherwise, push your
commits first and try again.

**"The waiting indicator isn't working."**
Check that the monitor is running: `bay monitor status`. If it's
running, the issue is likely that the prompt text doesn't match any
pattern. Switch to the window, see what the prompt looks like, and use
`prefix + P` or `bay add-prompt` to capture it. Then edit
`~/.config/bay/bay-prompts.txt` to generalize the pattern to a regex.

**"I want to use bay with an agent that isn't Claude or Codex."**
Add an agent definition to your config:

```toml
[agents.myagent]
command = "myagent-cli"
config_file = ".myagent.local.md"
```

`command` is what bay runs in the pane. `config_file` is the filename
bay writes the rendered template into — make sure it's in your repos'
`.gitignore`. Then reference it with `--agent myagent` or set it as a
dock's default.

**"Two docks use the same repo — is that okay?"**
Yes. Worktree directories are per-repo (configured in `[repos.*]`), so
workspaces from different docks sharing a repo get separate worktrees
in the same directory. No conflicts.

**"I renamed a workspace but the agent still shows the old name."**
`bay ws rename` updates the manifest and tmux window title immediately,
but the agent's config file contains the old name. Run
`bay win restart` to regenerate the config with the new name.
