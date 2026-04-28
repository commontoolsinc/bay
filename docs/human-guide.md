# Bay -- Human Guide

Bay manages concurrent workspaces built on git worktrees and tmux. Each
workspace gets its own worktree and tmux surfaces -- so you can work on
multiple branches simultaneously without stash juggling or directory
cloning. Bay handles session recovery after reboot, launches editors
and terminal panes, detects PR merges automatically, and optionally
launches AI coding agents.

## Prerequisites

- **tmux** -- bay manages tmux sessions and surfaces. Install with
  `brew install tmux` (macOS) or your system package manager. If you're
  new to tmux, the key concept is: tmux keeps terminal sessions alive
  in the background. You can detach and reattach without losing state.
  Bay leans on this heavily. See the
  [tmux Getting Started guide](https://github.com/tmux/tmux/wiki/Getting-Started).
  For a recommended starter config, see
  [Recommended tmux setup](tmux-setup.md).

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
completions, and configures your default agent and editor. If Claude Code
or Codex is installed, it also installs hooks so tmux highlights the tab
when an agent needs permission or finishes a turn — Option+R then jumps
straight to it.

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
bay ws new --branch fix-it  # checkout or create a branch
bay ws new --agent          # agent workspace (dock default, no shell)
bay ws new --agent=codex    # agent workspace with specific agent
bay agent                   # launch default agent as a split pane
bay agent claude            # launch a specific agent as a split pane
bay shell                   # open a shell as a split pane
bay edit                    # open the editor
```

### Navigate

```
bay go                      # pick a surface (agent/shell/editor)
bay ws go                   # pick a workspace
```

Or use keybindings: Option+h/l to cycle windows, Option+g for the
workspace picker.

### Check on things

```
bay ls                      # context-sensitive tree view
bay pwd                     # current bay location
```

### Clean up

```
bay ws close w1             # by ID; safety checks for uncommitted work
bay ws close --done         # close workspaces that are not dirty or pending
bay ws close --clean        # close all non-dirty workspaces
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
bay repo add myproject ~/projects/myproject --worktree-dir ~/wt/myproject
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

Each workspace has three identity concepts:

- An **ID** like `w1`, `w2`, `w3` — the stable handle. Set at creation,
  never changes, unique within a dock. Every command that targets a
  workspace takes the ID: `bay ws close w1`, `bay ws show w1`,
  `bay edit w1`. The ID matches the on-disk directory basename for
  worktree workspaces (so `~/projects/myproject-worktrees/w1` is
  workspace `w1`).
- A **Name** like `auth-fix` — a friendly display label. Sticky once
  set. May be empty initially; sync fills it from the branch on first
  detection (`feature/refactor-memory` becomes `refactor-memory`).
  You can rename with `bay rename` (or `bay ws rename`). Names are
  not CLI keys — typing one returns `did you mean "w1"?` with the
  canonical ID.
- An optional **description** (see below) for richer context.

An ID is stable for a workspace's lifetime, but the slot is released
when the workspace closes — the next creation may reuse a freed
trailing slot, while gaps in the middle of the sequence (`w1`, `w3`,
`w7`) stay until you fill them. The picker, `bay ls`, and `bay tree`
all show both the ID and the friendly Name; pick whichever makes
sense for the task at hand.

Workspaces can also carry a **description** — commit-message-style
text with two parts:

- **First line**: a short (~40 character, cap 80) label shown in the
  workspace picker (Option-Shift-G), `bay ls` / `bay tree`, and the
  `Option+/` flash.
- **Body** (optional): trailing lines separated from the first line by
  a blank line. Shown only in the `Option+?` popup — useful for longer
  context when you step back into a workspace after working elsewhere
  ("paused mid-rebase; rename conflict on helper.ts; tests green except
  auth_test.go").

Set it with `bay describe "Login flow fixes"` (or `bay ws describe`).
Shells handle newlines in quoted strings, so you can pass a multi-line
body directly; or use `bay describe --edit` to open `$EDITOR`.
Descriptions don't affect tmux tab names — tabs stay short and truncate
to fit, while descriptions give you a more human-readable hint when
you're scanning for the right workspace.

In `bay ls` / `bay tree`, only the first line is shown and its length
adapts to terminal width (floored at 15 characters, capped at 80). Pipe
the output or set `COLUMNS=200 bay tree` to force a wider layout; piped
output always uses the full 80-character cap.

### Surfaces

A **surface** is anything you can focus and jump to within a workspace.
Surfaces have a type and a backend:

| Type | What it runs |
|------|-------------|
| `agent` | An AI coding agent (Claude Code, Codex, etc.) |
| `shell` | A plain shell in the workspace directory |
| `cmd` | A specific command (`npm test`, `cargo watch`, etc.) |
| `editor` | A terminal editor (nvim, vim) in its own tmux pane |

All surfaces live in tmux (panes within windows). GUI editors (Cursor,
VS Code, Zed) are launched via `bay edit` but are not tracked as
surfaces — they manage their own windows.

Surfaces within a workspace that share a tmux window are in the same
**layout group**. Splitting a pane creates a surface in the same group;
`--window` creates a surface in a new group.

### Dirty and merged flags

Workspaces have two flags instead of a status enum:

- **dirty** (computed) -- the worktree has uncommitted changes or
  untracked files. Computed from git state on every display.
- **merged** (persisted) -- the workspace's branch has been merged
  into the default branch. Set by the background monitor and persisted
  in the manifest.

Workspaces show `st=pending` when they have an unmerged branch (work is
out for review). `bay ws close --done` closes workspaces that are not
dirty and not pending — the truly finished ones. `bay ws close --clean`
is broader, closing anything not dirty. Use `--dry-run` with either to
preview what would be closed.

### Orphan cleanup

If a workspace's last pane disappears — via tmux-native kill
(`Ctrl-B x`, process exit), or via `Option+W`/`bay sf close` (no
`--force`) — bay schedules the workspace for auto-close after a
60-second grace window. You can rescue it during that window by
adding a surface back (`bay sf new --ws <name>`); the pending close
cancels. If you don't act, bay runs `bay ws close` with the normal
safety gates: clean workspaces whose commits are pushed or already
landed on the default branch get cleaned up; dirty workspaces or
workspaces with unlanded commits stay in place.

`bay sf close --force` and direct `bay ws close` are unaffected —
those are explicit user actions and close immediately.

### Last-surface close confirmation

Closing the only surface in a workspace from a non-interactive
invocation, such as the `Option+W` keybinding, requires a second close
attempt to keep a stray keypress from tearing down the visible pane.
The first attempt flashes a status-line message ("press again to close
last surface in …") and leaves the surface intact; the message stays
on screen for exactly as long as the second tap is accepted, so when it
disappears the confirmation window has expired. A second `Option+W`
while the message is still up proceeds normally. Interactive
command-line invocations close the last surface on the first command.

Closes that aren't the last surface in a workspace are unaffected, and
`--force` skips the double-tap entirely.

### Undo-close

`Option+z` (or `bay sf restore`) restores the most recently closed
surface in the current dock. Bay keeps a per-dock LRU queue of the
last 10 bay-initiated closes, retained for 1 hour. `bay sf close`,
`Option+W`, and last-surface closes all push an entry; `bay sf
restore --list` shows the queue without restoring.

Restore recreates the surface in its parent workspace. If the
workspace has since been closed (e.g. the 60s orphan-cleanup grace
window elapsed), the entry is silently discarded — hit `Option+z`
again to reach the next entry.

### Identifiers and references

Workspaces are referenced by **ID** in commands. Bare form is `w1`;
fully qualified is `dev:w1`. The keyword `self` resolves to the
workspace at your current tmux pane and working directory.

A bare ID resolves to the current dock first; if absent there, falls
through to a cross-dock search (which errors on ambiguity). Pass
`--dock <name>` to target a different dock without changing context.

Workspace **Names** must match `[a-zA-Z0-9_-]+` and cannot match the
reserved ID pattern `^w[1-9]\d*$` — that namespace is bay's. If you
type a Name where bay expects an ID, the error names the ID for you:
`workspace "auth-fix" not found; did you mean "w1"?`.

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

### Custom agents (optional)

Add agents beyond the built-in three, or add default args to a built-in:

```toml
[agents.claude]
args = ["--dangerously-skip-permissions"]      # default args for claude

[agents.my-agent]
command = "my-agent-cli"
args = ["--flag"]                              # default args
resume_args = "--resume"
project_file = ".my-agent.md"
```

### Per-dock overrides (optional)

Override the default agent or agent args for a specific dock:

```toml
[docks.dev]
agent = "codex"                                # override default agent
terminal = "ghostty"                           # host terminal app

[docks.dev.agent_args]
codex = ["--model", "o3"]                      # per-dock agent args override
```

## Agent integration

### Bay awareness via `bay repo init`

Instead of generating per-workspace config files, bay uses a lightweight
awareness model. Run `bay repo init` to set up a repo:

```
bay repo init myproject     # by name
bay repo init               # infer from CWD
```

This does two things:
1. For each built-in agent with a project file (e.g., `CLAUDE.local.md`
   for Claude Code), appends a one-line pointer: *"This project uses
   bay. Run `bay agent-guide` for commands."* Uses the local file so
   bay awareness doesn't pollute the shared project config.
2. Creates `.worktreeinclude` if missing, so gitignored files (`.env`,
   etc.) get copied to new worktrees.

`bay repo add` calls `repo init` automatically. `bay doctor` flags
repos missing bay awareness.

### .worktreeinclude

Bay reads `.worktreeinclude` as a **gitignore-format** file — each line
is a pattern (globs, negations, directory rules) resolved by git itself.
At worktree creation time, bay expands the patterns and copies matching
files from the repo root into the new worktree.

To protect against accidentally spraying checked-in files across
worktrees, bay refuses to sync any match that is **tracked in git** or
**not covered by `.gitignore`** — only files that cannot end up in git
get copied. Refusals are printed to stderr, but the worktree is still
created and other valid matches are still copied. Fix by adding the
refused files to `.gitignore` or removing the pattern from
`.worktreeinclude`.

### Agents keep descriptions current

Bay-aware agents treat the workspace description as a context-recall
aid, not just a label. Expect them to:

- Set a first-line label on start if one isn't set.
- Maintain the body as a standing brief — what this workspace is
  for, where it stands now, what's the immediate next move — so
  when you return after working elsewhere, `Option+?` swaps the
  whole picture back into your head without you re-reading the
  diff. They should update when the *situation* changes, not on
  every pause.

`bay repo init` installs the pointer to `bay agent-guide` which tells
agents how. For a stronger nudge, paste the block below into your
user-global agent instructions (for Claude Code, `~/.claude/CLAUDE.md`):

```markdown
## Bay workspace descriptions

If this project uses bay, keep the current workspace's description
current. It's the user's primary context-recall aid when they return
to a workspace after working elsewhere — surfaced in the picker,
`bay ls`/`bay tree`, and the `M-?` popup.

Treat the description as a **standing brief about the workspace**, not
a log of what you just did. Git history already records activity; the
description should let the user swap the workspace's overall context
back into their head in five seconds.

- **First line (≤40 chars):** the workspace's goal or scope. Stable —
  rarely changes once set. Set it on start with `bay describe "..."`.
- **Body (2–5 lines):** the *situation*, written for someone opening
  this workspace cold. What problem is being solved, what shape the
  approach is taking, what's the current state, and what's the
  immediate next move. Update via `bay describe --edit` when the
  situation meaningfully changes — not on every pause.
- **Avoid recency bias.** Don't narrate the last few tool calls. If
  the body would read the same after another hour of similar work,
  it's at the right altitude.
- **Keep it brief.** A reader should grasp it in one glance. If you're
  writing a fourth line, ask whether it belongs in code comments or a
  PR description instead.

Run `bay agent-guide` for the full reference.
```

`bay describe` only writes workspace metadata, so it's safe to allowlist
and skip the permission prompt. For Claude Code, add to
`~/.claude/settings.json`:

```json
{ "permissions": { "allow": ["Bash(bay describe:*)"] } }
```

For Codex, add to `~/.codex/rules/default.rules`:

```
prefix_rule(pattern=["bay", "describe"], decision="allow")
```

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
bay ws new [name]                           # new workspace (positional sets the display Name)
bay ws new [name] --agent                   # first surface is agent, not shell
bay ws new [name] --agent=codex             # specific agent type
bay ws new [name] --dock <d>                # target a specific dock
bay ws new [name] --branch <b>              # checkout or create branch
bay ws new [name] --repo <r>                # override dock's repo
bay ws new [name] --dir <path>              # external workspace
bay ws new [name] --description "<text>"    # set description at creation time
bay ws new [name] -q                        # suppress output (scripting)
bay ws close <id>                           # close + delete pushed branch ('self' for current)
bay ws close <id> --force                   # skip safety checks (keeps unlanded branches)
bay ws close --done                         # close workspaces not dirty or pending
bay ws close --clean                        # close all non-dirty workspaces
bay ws close --done --dry-run               # preview what --done would close
bay ws show [id]                            # detailed view (default: current)
bay ws show [id] --json                     # machine-readable
bay ws show [id] --short                    # one-line summary (id — name — branch — #PR)
bay ws show [id] --short --plain            # same, no ANSI (for tmux display-message etc.)
bay ws show [id] --flash                    # first-line flash in status bar (Option+/)
bay ws show [id] --popup                    # full description in popup (Option+?)
bay ws rename [id] <new-name>               # rename display Name (ID is unchanged)
bay ws describe                             # print current description
bay ws describe [id] [<text>]               # set (first line + optional blank-line-separated body)
bay ws describe --edit                      # open $EDITOR for multi-line editing
bay ws describe --clear                     # clear description
bay describe [id] [<text>]                  # top-level shortcut for the above
bay ws ls                                   # list workspaces in current dock
bay ws ls --json                            # machine-readable workspace list
bay ws tree                                 # tree of current dock (workspaces + surfaces)
bay ws go [query]                           # workspace picker (intra-dock)
bay ws go --waiting                         # filter to waiting workspaces
bay ws go --next-waiting                    # cycle to next waiting workspace
bay ws next                                 # next workspace in dock
bay ws prev                                 # prev workspace in dock
```

### Surface management

`bay surface new` (or `bay sf new`) has subcommands for each surface type:

```
bay surface new shell [name]               # shell as split pane (default)
bay surface new agent [type] [name]        # agent as split pane
bay surface new cmd "<command>" [name]     # command as split pane
bay surface new edit [workspace]           # editor surface as split pane
bay surface new shell [name] --window      # shell in a new tmux window
bay surface new shell [name] --split h     # horizontal split
bay surface new shell [name] --ws <w>      # target a different workspace
bay surface close <name>                   # close a surface (prompts on agents)
bay surface close <name> --force           # skip close confirmations
bay surface restore                        # restore the most recently closed surface
bay surface restore --list                 # show the undo-close queue
bay surface ls                             # list surfaces in current workspace
bay surface ls --json                      # machine-readable surface list
bay surface show [name]                    # show details (defaults to current)
bay surface rename [old] <new>             # rename (defaults to current surface)
bay surface go [query]                     # surface picker (intra-workspace)
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
bay new shell [name]            # shell as split pane
bay new agent [type] [name]     # agent as split pane
bay new cmd "<command>" [name]  # command as split pane
bay new edit [workspace]        # open the editor as split pane
bay shell [name]                # shell as split pane (default)
bay shell [name] --window       # shell in new window
bay agent [type]                # agent as split pane
bay agent --window              # agent in new window
bay edit [workspace]            # open workspace in editor (default)
bay edit --editor vim           # use a specific editor this time
bay edit --window               # editor in new window
bay edit --dock                 # dock editor (all workspaces)
bay close <name>                # alias for bay surface close (prompts on agents)
bay restore                     # alias for bay surface restore (undo-close)
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
bay repo ls --json                          # machine-readable repo list
bay repo tree [name]                        # full tree for a repo (default: current)
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
bay dock ls [name]                          # list dock contents (default: current)
bay dock ls --json                          # machine-readable dock view
bay dock show <name>                        # detailed dock info
bay dock rename [old] <new>                 # rename (defaults to current dock)
bay dock close <name>                       # close all workspaces + kill session
bay dock close <name> --force               # skip safety checks
bay dock recover <name>                     # recover a single dock
```

`dk` is an alias for `dock`.

### Listing and context

```
bay ls                                      # browse from your current scope
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
| `Option+h` / `Option+l` | Previous / next window (tmux-native) |
| `Option+j` / `Option+k` | Select pane down / up (tmux-native) |
| `Option+H` / `Option+L` | Select pane left / right (tmux-native) |
| `Option+J` / `Option+K` | Select pane down / up (mirrors `j`/`k`) |
| `Option+g` | Workspace picker (popup) |

### Creation (lowercase = split pane, Shift = new window)

| Key | Action |
|-----|--------|
| `Option+s` / `Option+S` | Shell pane / shell window |
| `Option+a` / `Option+A` | Agent pane / agent window |
| `Option+e` / `Option+E` | Workspace editor / dock editor |
| `Option+c` | Create workspace in current dock |

### Utility

| Key | Action |
|-----|--------|
| `Option+w` | Close current pane/surface (press twice when it is the last surface in a workspace) |
| `Option+z` | Restore most recently closed surface (undo-close) |
| `Option+/` | Flash current workspace (name — first-line description — branch — #PR) |
| `Option+?` | Popup with full workspace description (including body) |
| `Option+p` | Command palette (Tab inside to flip window/pane mode) |

### Pattern

Navigation is vim-flavored hjkl. `Option+h/l` cycle windows left/right
(tabs are horizontal in the status bar); `Option+j/k` select panes
down/up (most splits stack vertically). `Option+Shift+H/L` handle the
less-common horizontal pane axis; `Option+Shift+J/K` mirror `j/k` so
a sequence like H-J-L keeps the Shift held the whole time. For
creation keys, lowercase opens a split pane in the current window and
Shift opens a new window. The workspace picker (`Option+g`) is usually the fastest way to
jump across workspaces — fuzzy-match by name or description.

### Installing and updating

`bay setup` installs all keybindings in `~/.tmux.conf`. It detects
conflicts with existing bindings and prompts before overwriting. Run
`bay doctor` to check if keybindings are current.

When bay ships a canonical change (e.g. flipping `M-s` from window to
pane), `bay setup` on re-run detects canonical keys bound to a
non-canonical bay command and prompts to update them. Opt out per-key
by adding `# bay-keep: M-s` to the bay block — bay will stop asking
about that key, and `bay doctor` will stop reporting it as missing.

### Optional: personal agent launcher

If you regularly switch between specific agents, add a personal tmux
key table outside the managed `# Bay keybindings` block. This keeps
Bay's default keymap focused on generic actions while giving you
deterministic shortcuts for your own agent workflow.

This example uses `Option+o` as an agent namespace; choose another
unused key if you already bind it. Lowercase opens a split pane,
uppercase opens a new tmux window, and `w` switches to workspace
creation:

```tmux
# BEGIN bay-personal-agent-bindings
# Personal Bay agent launcher. Keep outside the "# Bay keybindings" block.
bind-key -n M-o display-message "agent: c Claude, x Codex, g Gemini | Shift=window | w=workspace" \; switch-client -T bay-agent

bind-key -T bay-agent c run-shell 'bay agent claude --pane || true'
bind-key -T bay-agent C run-shell 'bay agent claude --window || true'
bind-key -T bay-agent x run-shell 'bay agent codex --pane || true'
bind-key -T bay-agent X run-shell 'bay agent codex --window || true'
bind-key -T bay-agent g run-shell 'bay agent gemini --pane || true'
bind-key -T bay-agent G run-shell 'bay agent gemini --window || true'

bind-key -T bay-agent w display-message "workspace: c Claude, x Codex, g Gemini" \; switch-client -T bay-agent-workspace
bind-key -T bay-agent-workspace c run-shell 'bay ws new -q --agent=claude || true'
bind-key -T bay-agent-workspace x run-shell 'bay ws new -q --agent=codex || true'
bind-key -T bay-agent-workspace g run-shell 'bay ws new -q --agent=gemini || true'
# END bay-personal-agent-bindings
```

After editing `~/.tmux.conf`, reload it with `prefix + r` if you use
the recommended reload binding, or run `tmux source-file ~/.tmux.conf`.
Delete the rows for agents you do not use, or replace the built-in
agent names with custom agents from your Bay config.

## Command palette

`Option+p` opens the command palette in a tmux popup: a
fuzzy-searchable list of every bay command that doesn't have a
dedicated hotkey. Commands that *do* have a hotkey show it in the
right-hand column, so the palette doubles as a cheat-sheet.

- **Filtering**: type to narrow the list (substring match on titles).
  The section grouping collapses to a single flat list while a filter
  is active.
- **Recents**: the top "Recent" section shows the most recently used
  command as slot 1 (for `M-p, Enter = redo last`), followed by the
  most frequently used commands from the last ~20 invocations.
- **Mode toggle**: entries that create a surface respect the Mode
  shown in the footer. The palette starts in **pane** mode (split the
  current window); press `Tab` to flip to **window** mode (new tmux
  window).
- **Parametric entries** end in `...`: they chain to a sub-picker
  (e.g., "New agent..." → pick agent type) or a text prompt
  (e.g., "Rename workspace..." prefilled with the current name).
- **Scope**: entries that require a workspace are hidden when you
  open the palette outside one; same for dock-scoped entries.

Every command in the palette can also be run directly from the
shell — the palette is not a new surface, just a discovery and
launcher layer.

## Editor integration

`bay edit` opens your editor on the workspace's root directory. Use
the editor's own file browser to navigate within the project. To edit
individual files, open a shell and launch your editor from there.

```
bay edit                    # current workspace (default)
bay edit auth-fix           # specific workspace
bay edit --editor vim       # use a specific editor this time
bay edit --dock             # dock editor (all workspaces)
bay edit --window           # new window instead of split pane
```

**GUI editors** (Cursor, VS Code, Zed) launch and return — bay does
not track the editor window. Running `bay edit` again focuses the
existing window (these editors reuse the window for an already-open
directory), so `Option+e` works as both "launch" and "switch to."

**Terminal editors** (nvim, vim) run as tracked tmux surfaces,
splitting the current window by default. Use `--window` for a new tmux
window. When you quit the editor, the pane closes and the surface is
cleaned up automatically.

Editor resolution order: `--editor` flag, `default_editor` in config,
`$VISUAL`, `$EDITOR`, then probing for cursor/code/zed/nvim/vim.

Configure with:
```
bay config editor cursor    # save preference
bay config editor           # check current editor
```

## Monitoring

Bay runs a background monitor that provides two services:

### Waiting detection

Bay detects agents that need your attention through three mechanisms,
all unified into the `Option+R` ("next waiting") navigation:

**Bell-based.** Agents that send a terminal bell (`\a`) are detected via
tmux's `window_bell_flag`. For Claude Code, `bay setup` installs a
`PermissionRequest` hook that sends a bell when Claude asks for
permission.

**Turn-complete hooks.** When an agent finishes a response, you usually
want to know — not because it's blocked, but so you can review and move
on. `bay setup` installs hooks for any agent it finds on your PATH:

- Claude Code: `Stop` hook in `~/.claude/settings.json`.
- Gemini CLI: `AfterAgent` hook in `~/.gemini/settings.json`.
- Codex: `notify` entry in `~/.codex/config.toml`.

The hooks set `@bay-waiting=1` on the agent's tmux window without
emitting a terminal bell, so they're silent on the focused window and
visible only as a tab highlight on inactive windows. The flag clears
automatically when you navigate to the window — `bay setup` also
installs an `after-select-window` tmux hook in `~/.tmux.conf` that
mirrors how `window_bell_flag` self-clears on focus. Both pieces are
bundled behind a single setup prompt because they only work as a unit.

**Pattern-based (fallback).** The monitor watches agent panes by
capturing the last few lines of output, stripping ANSI codes, and
matching against regex patterns in
`~/.config/bay/waiting-patterns.txt`:

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
`~/.config/bay/waiting-patterns.txt` and add a regex on its own line.
The monitor reloads patterns on every cycle (default 3s), so no
restart is needed.

### Merge detection

The monitor performs activity-gated `git fetch` for repos with recent
workspace activity (within the last couple of hours). After fetching, it
checks if workspace branches have been merged into main. When a merge is
detected:

- The workspace's `merged` flag is set automatically.
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
    ws deploy    br=release/v2    merged  n=1
```

Use `bay ls -R` to recurse fully. Use `bay ls -l` for tmux IDs.
Use `bay ls -s` for compact output without labels or key names.

## Machine-readable output

`bay pwd`, `bay ls`, and `bay ws show` all accept `--json`.

```
bay pwd --json | jq '.workspace_id'
bay ls --json | jq '.repos[].docks[].workspaces[] | select(.merged)'
bay ls --json --rows | jq '.[] | select(.workspace_waiting)'
bay ws show w1 --json | jq '.branch'
```

## Status line

`bay setup` offers to install bay's tmux status line automatically — it
appends a `# Bay status line` block to your `~/.tmux.conf` if no
`status-right` is already configured. The block looks like this:

```tmux
# Bay status line
set -g status-right-length 40
set -g status-right '#(bay status-line full --window #{window_id} --width #{status-right-length})'
```

You can also add it manually if you skipped the prompt. If your
`.tmux.conf` already has a `status-right` setting, bay won't overwrite
it; setup prints the recommended lines so you can merge them yourself.

The `full` field outputs `repo:branch #PR | status` (e.g.
`bay:fix/login #42 | dirty`). The `--width` flag enables adaptive
truncation — when space is tight, it progressively shortens the branch
name, drops the repo prefix, and abbreviates status indicators. Pass
`#{status-right-length}` so tmux tells bay how much space is available.

**Pass `--window #{window_id}`.** Tmux caches `#()` output per client
keyed by the literal command string, and only refreshes on the
`status-interval` tick (default 15s). Without an interpolated
`#{window_id}`, every window and dock shares a single cache entry, so
you'll see a stale status from another window until the next tick.
Interpolating the window ID gives each window its own cache, and bay
uses the explicit ID instead of asking tmux which window is "current".

Other fields: `id`, `name`, `branch`, `pr`, `status`, `dock`, `merged`.

The `merged` field shows a count of merged workspaces in the current
dock (e.g. "2 merged"). Useful for a status bar reminder to clean up.

### Tab name truncation

Bay automatically shortens tmux tab names when a dock has many
workspaces, so tabs don't overflow the status bar. The full workspace
name is preserved in the manifest for navigation and completion —
only the tmux window title is truncated. Names expand again when
workspaces are closed.

## Recovery

Bay is designed for everything to be reconstructable after a reboot.

### After a reboot

```
bay recover
```

This recreates all tmux sessions, windows, and panes from the manifest.
Worktrees are already on disk. Bay relaunches shells, agents (with
`--continue` for Claude Code), and terminal editor surfaces. GUI
editors are not recovered — reopen them with `bay edit`. Starts the
monitor and prints `tmux attach` commands.

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

## Shell completions

`bay setup` prints a snippet for your shell rc file. Tab completion
covers workspace names, dock names, repo names, agent types, and
surface names.

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
Safety checks prevent losing work. Bay treats pushed branches and
commits included in a merged PR as safe, including multi-commit squash
merges. It also accepts squash-merged/cherry-picked patches on the
default branch. If you're sure despite a refusal, use `--force`. Or use
`bay ws close --done` to batch-close all finished workspaces, or
`--clean` for anything non-dirty. Add `--dry-run` to preview first.

**"The waiting indicator isn't working."**
For bell-based detection: ensure `monitor-bell` is on in tmux (it is by
default). For Claude Code, re-run `bay setup` to install the bell hook.
For pattern-based detection: check `bay monitor status`. If running, the
prompt text probably doesn't match any pattern. Add a regex to
`~/.config/bay/waiting-patterns.txt` — the monitor reloads every cycle.

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

**"My agent lost context after recovery."**
Built-in agents have resume args configured automatically (e.g.,
`--continue` for Claude Code). Bay uses these on recovery.
For custom agents, set `resume_args` in the `[agents]` config section.

**"How do I see my editor in bay go?"**
Terminal editors (nvim, vim) launched via `bay edit` appear as surfaces
in `bay go`. GUI editors (Cursor, VS Code, Zed) are fire-and-forget —
use Cmd+Tab or your OS window manager to switch to them.

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
