# Bay — Agent Guide

Bay manages concurrent workspaces with surfaces across tmux and GUI
applications. This guide is for AI agents (Claude Code, Codex, etc.)
that create, monitor, and manage workspaces — either from an
orchestrating session or from within a workspace.

## Key concepts

A **workspace** is a managed working directory with metadata (branch,
PR, dirty/merged flags). Each workspace has:
- A **name** — the primary identifier. Defaults to auto-abbreviated
  branch name (stripping prefixes like `feature/`, `fix/`). Can be
  overridden with `bay rename` (or `bay ws rename`), which sticks permanently.
- A **full reference** — `dock:name` (e.g., `labs:auth-fix`). Bare
  name (`auth-fix`) resolves to the current dock first; if absent
  there, falls through to a cross-dock search (which errors on
  ambiguity).
- An optional **description** — commit-message-style text. The first
  line is a short free-form label (cap 80) shown in the picker,
  `bay ls`/`bay tree`, and the `M-/` flash. Optional trailing lines
  (separated from the first line by a blank line) are a richer body
  for context recall — shown only in the `M-?` popup and JSON output.
  Set with `bay ws describe` (or `bay describe`). Does not affect
  tmux tab names.

  **Agents should keep the description current.** Update the first
  line when the workspace's purpose shifts, and update the body at
  natural checkpoints — pauses, context switches, end-of-task — so
  that when the user returns to the workspace after working elsewhere,
  `M-?` shows "where I left off, what's blocked, what's next" without
  re-reading the diff. The body is the whole reason the field is
  richer than a label; an unset body defeats the feature.
- A **path** — the on-disk working directory. For worktree workspaces
  bay assigns sequential subdirs (`w1`, `w2`, `w3`, ...) under the
  repo's worktree dir. The path is independent of the name: renaming
  a workspace does not move its directory. Identify workspaces by name
  (or `dock:name`); the path basename is not a stable identifier.

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
- A **backend** — interaction model: `tmux-pane`.

### Workspace types

| Type | CWD | Bay manages the directory |
|------|-----|--------------------------|
| **Worktree** | git worktree | yes — creates and deletes it, safety checks on close |
| **External** | any user path | no — bay remembers it for recovery |

### Dirty and merged

- `dirty` — computed from git (uncommitted changes); shown in `bay ls`, `bay tree`, statusline
- `merged` — persisted; auto-detected when branch is merged into default

### Surface types and backends

| Type | Backend | Description |
|------|---------|-------------|
| `agent` | `tmux-pane` | AI agent (Claude Code, Codex, etc.) |
| `shell` | `tmux-pane` | Interactive shell |
| `cmd` | `tmux-pane` | One-off command |
| `editor` | `tmux-pane` | Terminal editor (nvim, vim) |

## Self resolution

Most commands accept `self` or default to it when no target is given.

**Workspace resolution**: `self` matches CWD against known workspace
paths. If CWD is inside a workspace directory, that workspace is
resolved.

**Surface resolution**: For surface-scoped commands (`bay surface
close`), `self` additionally matches the current tmux pane ID to
identify which surface within the workspace.

Commands an agent inside a workspace typically uses:
- `bay pwd` — confirm bay context (repo, dock, workspace, surface)
- `bay ws show self` — check own workspace metadata
- `bay ls` — see what other workspaces are doing
- `bay ws close self` — shut down when work is complete

## Operational model

Bay is a coordination layer over:
- git worktree lifecycle
- tmux session/window/pane lifecycle
- editor launching (terminal editors as surfaces, GUI editors fire-and-forget)
- workspace metadata (branch, PR, dirty/merged flags)
- recovery after tmux or host restart

Important invariants:
- A **workspace** is the durable unit. Surfaces are views onto it.
- Branch, PR number, dirty, and merged are all auto-detected. Branch
  comes from the filesystem on every display. PR is looked up via `gh pr
  view` once and cached. Merged is set to true automatically when bay
  detects a merge.
- Bay does not generate config files into worktrees. Project
  instructions go in the repo's `CLAUDE.local.md` (or equivalent).
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
  "path": "/Users/dev/projects/labs-worktrees/w1"
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
              "path": "~/projects/labs-worktrees/w1",
              "branch": "feature/auth-fix",
              "dirty": false,
              "merged": false,
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
    "workspace_dirty": false,
    "workspace_merged": false,
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
  "description": "Login flow fixes",
  "repo": "labs",
  "dock": "labs",
  "type": "worktree",
  "path": "/Users/dev/projects/labs-worktrees/w1",
  "branch": "feature/auth-fix",
  "pr": "347",
  "dirty": false,
  "merged": false,
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

Workspace fields:
- `description` — commit-message-style text. First line (before the
  first `\n`) is the label shown in lists and flash; anything after
  a blank line is the optional body shown only in the popup.

Surface fields:
- `type` — `agent`, `editor`, `shell`, or `cmd`.
- `backend` — `tmux-pane`.
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
| `bay ws ls` | workspaces in current dock (errors outside a dock) |
| `bay ws show [name]` | current workspace |
| `bay ws close [name]` | required (no default; use `--done`/`--clean` for batch) |
| `bay ws close --done` | workspaces not dirty or pending in current dock |
| `bay ws close --clean` | all non-dirty workspaces in current dock |
| `bay pwd` | current bay context |
| `bay surface new <kind> [name]` | current workspace |
| `bay surface ls` | surfaces in current workspace |
| `bay surface close <name>` | required (use `self` for current pane) |
| `bay surface show [name]` | current pane's surface |
| `bay surface tree` (none) | n/a — surfaces are leaves; use `bay surface show` |
| `bay dock ls [name]` | current dock from tmux session, or all docks |
| `bay dock tree [name]` | current dock from tmux session |
| `bay repo tree [name]` | repo at current CWD |
| `bay go [query]` | surfaces in current workspace |
| `bay ws go [query]` | workspaces in current dock |
| `bay edit [workspace]` | current workspace |
| `bay shell [name]` | current workspace, surface auto-named "shell" |

## Commands

Commands use explicit nouns: `ws` (workspace), `surface`/`sf`
(surface), `dock`, `repo`. Top-level shorthands exist for frequent
operations.

### Workspace commands

#### `bay ws new [name] [--dock DOCK] [--repo NAME] [--dir PATH] [--branch NAME] [--agent [TYPE]] [--description TEXT]`

Create a workspace with its first surface. The positional names the
new workspace; `--dock` selects which dock to create it in. `--dock`
defaults to the current tmux session if it is a bay dock, or
auto-bootstraps from CWD (creates a dock and repo automatically).

Default behavior opens a shell. Use `--agent` for the dock's default
agent, or `--agent TYPE` for a specific one. Or create the workspace
first and add an agent with `bay agent`.

`--description` is optional and can be set or changed later via
`bay ws describe`.

```
bay ws new                                  # auto-bootstrap from CWD
bay ws new auth-fix                         # named workspace in current dock
bay ws new auth-fix --dock labs             # named workspace in a specific dock
bay ws new auth-fix --repo ct-server        # using a different repo
bay ws new auth-fix --dir ~/projects/foo    # external workspace
bay ws new auth-fix --branch fix-auth       # checkout or create branch
bay ws new auth-fix --agent                 # with dock's default agent
bay ws new auth-fix --description "Login flow fixes"
```

#### `bay ws close [name] [--force] [--done] [--clean] [--dry-run]`

Close a workspace and all its surfaces. For worktree workspaces,
checks for uncommitted changes and unpushed commits. Refuses if dirty
unless `--force` is used. If the branch has been pushed, bay deletes
the local branch on close — no stale branches left behind. Pass
`self` to close the current workspace.

Batch flags (without a name):
- `--done`: close workspaces that are not dirty and not pending (have
  no unmerged branch). The conservative default for cleanup.
- `--clean`: close all non-dirty workspaces regardless of merge status.
- `--dry-run`: preview what would be closed without closing anything.

```
bay ws close auth-fix
bay ws close self
bay ws close self --force
bay ws close --done
bay ws close --done --dry-run
bay ws close --clean
```

**Orphan auto-close (with grace window).** When a workspace's last
surface goes away — via sync-detected tmux kill or via
`bay sf close` (no `--force`) on the last surface — bay schedules
the workspace for auto-close after a 60-second grace window
(`Workspace.PendingCloseAt` in the manifest). The user (or an
agent) can cancel by re-adding a surface; `SurfaceAdd` clears
`PendingCloseAt` unconditionally. If the grace expires with no
surfaces, sync runs `WsClose(force=false)` — clean + pushed
finalizes the teardown; dirty or unpushed clears `PendingCloseAt`
to stop retries and leaves a permanent orphan for the user to
resolve manually.

Direct `bay ws close <name>`, `--done`, `--clean`, and
`bay sf close --force` close immediately (no grace).

**Undo-close interaction.** `bay sf close` (and `Option+W`) push a
surface entry onto the dock's undo-close queue regardless of whether
the workspace survives. When the close is the last surface, the
surface is queued and `PendingCloseAt` is scheduled; restoring
within the grace window recreates the surface via `SurfaceAdd`,
which clears `PendingCloseAt` as part of its normal cancel path.
Past the grace window, the workspace is gone and a `bay sf
restore` call drops the stale entry silently.

#### `bay ws show [name] [--json|--short|--flash|--popup] [--plain]`

Show workspace details: path, branch, PR, dirty/merged, surfaces. Defaults
to the current workspace; pass `self` explicitly for the same effect.

`--short` produces a one-line summary: `name — description — branch — #PR`,
skipping empty fields. Only the first line of the description is included.
Pair with `--plain` for ANSI-free output.

`--flash` sends the short summary to tmux `display-message` for 5 seconds.
Used by the `M-/` keybinding. Does not print to stdout. No-ops silently
when invoked outside a workspace.

`--popup` opens a tmux popup showing the full description (including any
body). Used by the `M-?` keybinding. Dismiss with any key.

JSON output (`--json`) includes the full description, body and all.

```
bay ws show
bay ws show auth-fix
bay ws show --short                # one-line workspace summary
bay ws show --short --plain        # same, no ANSI colors
bay ws show --flash                # flash first line in tmux status bar (M-/)
bay ws show --popup                # full description in a tmux popup (M-?)
bay ws show self --json
```

#### `bay ws rename [name] <new-name>`

Rename a workspace. Overrides auto-abbreviation permanently. Names
must match `[a-zA-Z0-9_-]+`. With one arg, renames the current
workspace.

```
bay ws rename mem-refactor                 # rename current workspace
bay ws rename auth-fix mem-refactor        # rename by name
```

#### `bay ws describe [name] [<description>]` (also: `bay describe`)

Read or set a free-form description for a workspace. The description
has two parts:

- **First line** — short label (cap 80) shown in the workspace picker,
  `bay ls`, `bay tree`, and the `M-/` flash. Target around 40 characters.
- **Body** (optional) — trailing lines separated from the first line by
  a blank line (commit-message style). Shown in the `M-?` popup and
  JSON output only. Good for "paused mid-rebase; conflicts on helper.ts;
  tests green except foo_test.py" — context the user needs when stepping
  back into the workspace.

With no args and no flags, prints the current description to stdout.
An empty string or `--clear` clears it. `--edit` opens `$EDITOR` for
interactive multi-line editing. Total length cap is 2000 chars.

Multi-line descriptions are set directly via the positional argument;
shells handle newlines in quoted strings natively — no escape
interpretation of literal `\n`.

**Agents should keep both parts current:** set the first line when
starting a task (from the prompt or PR title), and update the body at
natural checkpoints (pauses, context switches, end-of-task) so the
user can flash `M-?` and immediately swap context back in.

```
bay ws describe                              # print current description
bay ws describe "Login flow fixes"           # set first line only
bay ws describe "Login flow fixes

paused mid-rebase on origin/main
rename conflict on helper.ts
tests green except auth_test.go"             # set with body (actual newlines)
bay ws describe --edit                       # open $EDITOR
bay ws describe auth-fix "Login fixes"       # set by workspace name
bay ws describe --clear                      # clear current workspace's description
bay describe "Login flow fixes"              # top-level shortcut
```

### Navigation

#### `bay go [query]` (alias for `bay surface go`)

Navigate between surfaces within the current workspace. This is
intra-workspace navigation.

- No args: opens a picker showing all surfaces in the workspace.
- With query: fuzzy-matches surface name or type. One match jumps
  directly; multiple opens the picker pre-filtered.
- `--next-waiting`: jump to next waiting surface in the workspace.

```
bay go                      # pick from surfaces
bay go shell                # jump to the shell surface
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

#### `bay palette` (hidden)

Backs the `Option+p` command palette popup. Not an agent-facing
command — it opens an interactive picker inside a tmux popup and is
intended for human use. Agents should invoke the underlying commands
(`bay ws go`, `bay shell`, `bay ws describe`, etc.) directly.

#### `bay ws next` / `bay ws prev`

Cycle to the next or previous workspace within the current dock.

### Surface commands

#### `bay surface new <kind> [args] [--ws WS] [--dock DOCK] [--pane|--split h|v]`

Add a surface to a workspace. `<kind>` is one of `shell`, `agent`,
`cmd`, or `edit`. `--ws` selects which workspace (default: current).
Defaults to a new tmux window. Use `--pane` for a split pane.

Alias: `bay sf new`. Same subcommands as `bay new`.

```
bay surface new shell                       # shell in new window
bay surface new shell tests                 # surface named "tests"
bay surface new agent codex                 # agent in new window
bay surface new cmd "npm test" tests        # named cmd surface
bay surface new shell --pane                # split into current window
bay surface new shell --ws auth-fix         # in a different workspace
bay sf new edit                             # editor surface
```

#### `bay surface close <name> [--ws WS] [--dock DOCK] [--force]`

Close a named surface. Pass `self` to close the current pane's
surface. Two protections may apply (both skipped by `--force`):

- **Last surface in workspace, non-interactive invocation**: requires
  a second close attempt for the same workspace while a tmux status
  message is still displayed (~1.5 seconds; the message duration *is*
  the deadline). The first attempt flashes the message and exits 0
  without closing. Designed to keep a stray `Option+W` from collapsing
  a workspace's only pane. Interactive command-line closes proceed on
  the first command. Agents invoking close programmatically should pass
  `--force`.
- **Agent surface (non-last)**: TTY y/N prompt to protect conversation
  context. Auto-confirms when stdin is not a terminal.

```
bay sf close shell-2
bay sf close self
bay sf close agent --force
```

#### `bay surface restore [--list]`

Restore the most recently closed surface in the current dock
(undo-close). Bay keeps a per-dock LRU queue of the last 10
bay-initiated closes, retained for 1 hour. Pops the top entry and
recreates the surface in its parent workspace. If the parent
workspace is already gone (e.g. the 60s orphan-cleanup grace window
elapsed), the entry is silently discarded — call again to skip past
stale entries.

```
bay sf restore            # restore most recent
bay sf restore --list     # show the queue without restoring
bay restore               # alias
```

Bound to `Option+Z` in tmux (mirror of `Option+W`).

#### `bay surface show [name] [--ws WS] [--dock DOCK]`

Print details for a surface. Defaults to the current pane's surface.

#### `bay surface rename [old] <new> [--ws WS] [--dock DOCK]`

Rename a surface. With one arg, renames the current surface.

### Top-level shorthands

```
bay go [query]         → bay surface go [query]
bay shell [name]       → shell in new window (--pane for split)
bay agent [type]       → agent in new window (--pane for split)
bay edit [workspace]   → workspace editor (--dock for all workspaces)
bay restore            → bay surface restore (undo-close)
bay ls                 → list everything
bay pwd                → show current bay context
bay recover            → reconstruct state after reboot
```

#### `bay edit [workspace] [--dock|--ws] [--editor CMD] [--pane|--split h|v]`

Open the workspace's root directory in an editor. Use the editor's
file browser to navigate within the project. To edit individual files,
open a shell instead.

Terminal editors (nvim, vim) create a tracked surface in their own
tmux window (or pane with `--pane`). The surface is cleaned up when
the editor exits. GUI editors (Cursor, VS Code, Zed) launch and
return — running `bay edit` again focuses the existing window.

Editor resolution: `--editor` flag > `default_editor` in config >
`$VISUAL` > `$EDITOR` > probe (cursor, code, zed, nvim, vim).

```
bay edit                    # open current workspace (default)
bay edit auth-fix           # open specific workspace
bay edit --editor vim       # use a specific editor this time
bay edit --dock             # dock editor (all workspaces)
bay edit --pane             # split into current window
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

#### `bay shell [name] [--ws WS] [--dock DOCK] [--pane|--split h|v]`

Open a shell surface in a workspace. Defaults to a new tmux window.
Use `--pane` for a split pane.

```
bay shell                   # shell in new window
bay shell logs              # named "logs"
bay shell --pane            # split into current window
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
resume args (e.g., `--continue`), starts the monitor. Idempotent.

#### `bay doctor`

Health checks: config validity, repo accessibility, agent availability,
manifest consistency, monitor status, tmux keybindings, bay awareness
in repos.

#### `bay monitor start|stop|status`

Manage the background pane monitor. Detects when agents are waiting
for input and highlights those tmux windows. Also handles
activity-gated merge detection.

Waiting detection uses two mechanisms: bell-based (agents that send
`\a`, like codex natively or claude via a PermissionRequest hook
installed by `bay setup`) and pattern-based (regex matching via the
monitor). Both feed into `--next-waiting` navigation.

To add a new pattern-based detection rule, edit
`~/.config/bay/waiting-patterns.txt` directly — one regex per line. The
monitor reloads patterns on every cycle (default 3s), so no restart
is needed.

### Dock management

```
bay dock new <name> [--repo NAME] [--agent TYPE] [--terminal APP]
bay dock ls [name] [--json] [--rows]
bay dock show <name>
bay dock close <name> [--force]
bay dock recover <name>
```

### Repo management

```
bay repo ls [--json]
bay repo tree [name]
bay repo show <name>
bay repo add <name> <path>
bay repo remove <name> [--force]
bay repo init [name]
```

`bay repo init` sets up bay awareness: appends a one-liner to each
agent's project file (e.g., `CLAUDE.local.md`) pointing to `bay agent-guide`,
and creates `.worktreeinclude` if missing.

`.worktreeinclude` uses gitignore syntax. Each pattern is resolved by
git; matching files are copied from the repo root into new worktrees.
Bay refuses to sync matches that are tracked in git or not covered by
`.gitignore` (refusals are logged to stderr; valid matches are still
copied) — only files that cannot be checked in are copied.

## Typical workflows

### Spin up a workspace for a task

```
bay ws new auth-fix --dock labs
```

Creates a worktree workspace. Opens a shell by default. Use `--agent`
to launch the dock's default agent instead. Drop `--dock` to target
the current dock from your tmux session.

### Track progress from inside

Branch, PR number, dirty, and merged are all tracked automatically.
Branch comes from the worktree on every display. PR is looked up via
`gh pr view` and cached. Merged is set to true automatically when bay
detects a merge. Just commit, push, and open a PR — bay sees all of
it.

### Open a shell alongside your agent

```
bay shell
```

Opens in its own window. For a split pane alongside the agent:

```
bay shell --pane
```

### Open an editor for the workspace

```
bay edit
```

Terminal editors appear as surfaces in `bay go`. GUI editors launch
and return — manage them with your OS window manager.

### Check on all active work

```
bay ls
```

Shows every workspace, dirty/merged flags, branch, PR, and waiting indicators.

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

Merged is set to true automatically when bay detects a merge. Then:

```
bay ws close auth-fix
```

Or batch-close finished workspaces (not dirty, not pending):

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

Reconstructs all tmux sessions, surfaces, and agent sessions (with
resume args). Prints attach commands.

### Zero-config quickstart

From any git repo directory, with no prior bay configuration:

```
bay ws new
```

Bay auto-detects the repo, creates a dock, probes for an agent on
PATH, and opens a workspace. No config file needed for basic usage.
