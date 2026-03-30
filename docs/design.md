# Bay — Multi-session Workspace Management

A standalone tool for managing concurrent workspaces across tmux windows
and git worktrees. Handles agent sessions, shell access, and tmux
session recovery after reboot. Not tied to any specific project or
workflow — project context is injected via configuration.

**Status**: Design in progress (v2).

## Design principles

- **Filesystem is the source of truth.** Agent conversation memory is
  ephemeral. An agent should be able to restart from scratch and fully
  orient itself from the generated config file and the worktree state.
  Session resume is a nice-to-have, not a dependency.
- **Target repos are untouched.** Nothing is added to target repos to
  support bay. All agent config uses gitignored files.
- **Agent-agnostic.** Bay infrastructure (tmux, worktrees, manifest,
  monitoring) works across agents. Agent-specific behavior is a thin
  config layer.
- **Bay is generic; project context is configuration.** Bay doesn't
  know about crew or any specific project. It generates agent config from
  user-provided templates and manages the infrastructure around it.
- **Worktrees are the central managed entity.** Windows and panes are
  lightweight, disposable views into workspaces. Worktree lifecycle
  (creation, safety checks, cleanup) is bay's core value.
- **Tmux recovery is a first-class feature.** Everything bay manages —
  docks, workspaces, windows, panes — is reconstructable after reboot
  from a single command.

## Problem

Working on multiple PRs/projects concurrently with AI agents requires
managing tmux windows, git worktrees, and agent sessions by hand. Pain
points:

- **Setup is manual and fragile** — creating worktrees, tmux windows, and
  agent sessions by hand. Must redo everything after reboot.
- **Tracking is hard** — agents forget to update tmux window names. PR
  numbers are terse. No easy way to see "what's happening where."
- **No project context** — agent sessions are standalone, without access
  to shared project data, conventions, or coordination state.
- **No multi-access** — a worktree has one tmux window. You can't easily
  have an agent and a shell for the same worktree, or multiple agents.

## Terminology

- **Dock**: A named tmux session grouping related workspaces. Each dock
  has defaults for agent type and optionally a default repo. Dock names
  are stable and do not change.
- **Workspace**: A managed working directory with metadata (branch, PR,
  status). Either backed by a git worktree (bay-created, full lifecycle
  management) or an external directory (user-owned, bay just remembers
  it for recovery). Each workspace has an **ID** (immutable,
  auto-assigned per dock: w1, w2, ...) and a **name** (defaults to
  auto-abbreviated branch name, renameable). The full identifier is
  `dock:id` (e.g., `labs:w3`); the short form `w3` works when
  unambiguous across docks.
- **Window**: A tmux window attached to a workspace. Multiple windows
  can be attached to the same workspace. Bay tracks windows it creates
  for recovery. Each window has a display name shown in the tmux status
  bar.
- **Pane**: A tmux pane within a window. Runs an agent, a shell, or a
  command. Bay tracks panes it creates for recovery (manual splits are
  not tracked). Each pane records its split direction and parent pane
  for layout reconstruction.
- **Manifest**: A persistent record of all active workspaces, windows,
  and panes — used for tracking and reboot recovery.
- **Naming constraints**: Dock names and workspace display names must
  match `[a-zA-Z0-9_-]+` — no spaces, colons, or slashes. Colons
  conflict with `dock:id` syntax; spaces and slashes cause issues in
  tmux titles and filesystem paths.

## Workspace types

| Type | CWD | Bay creates it | Bay deletes it | Safety checks on close |
|------|-----|----------------|----------------|------------------------|
| **Worktree** | git worktree | yes | yes | dirty/unpushed |
| **External** | any user path | no | no | none (just closes windows) |

Both types get tmux recovery, agent config injection (with gitignore
verification), and monitoring.

## Requirements

### Must have (P0)

1. **Fast workspace creation** — new workspace available in <2 seconds.
2. **Reboot recovery** — `bay recover` reconstructs all docks,
   workspaces, windows, and panes. Idempotent — detects and reuses
   existing tmux state. Prints attach commands on completion.
3. **Agent config generation** — bay generates a gitignored config file
   in the workspace's CWD containing a standard preamble (workspace
   identity and essential bay commands) followed by optional per-dock
   template content. The preamble is always written, even without a
   user template.
4. **Manifest tracking** — persistent record of each workspace (id,
   name, type, repo, path, branch, PR, status) and its windows/panes.
5. **Clean shutdown** — closing a worktree workspace checks for
   uncommitted changes and unpushed commits. Refuses if dirty (override
   with `--force`). Cleans up worktree, config files, and all attached
   windows/panes. Closing an external workspace just closes windows
   and cleans up config files. Manifest entry moves to archive.
6. **Agent-agnostic** — works with Claude Code, Codex, or other agents.
   Agent-specific behavior (launch command, config filename) is
   configured per agent type. Per-dock `agent_args` extend the base
   command.
7. **Gitignore safety** — before writing a config file into any
   workspace CWD, bay verifies that the repo's `.gitignore` includes
   the agent's config filename. Refuses if not, offers to add it.
8. **Workspace naming** — each workspace has an immutable ID (w1,
   w2, ..., max+1 within the dock) and a renameable display name.
   Default name is auto-abbreviated from the branch name (stripping
   common prefixes like `feature/`, `fix/`). `bay ws rename` overrides
   the auto name; the override sticks and is not recalculated.
9. **Multiple windows per workspace** — a workspace can have multiple
   tmux windows (e.g., agent window + shell window). Each is
   independently openable and closable. Closing a window does not
   affect the workspace.
10. **Multiple panes per window** — a window can have bay-managed panes
    (agent, shell, or command). Pane layout (split direction and parent)
    is stored for recovery. Manual tmux splits are not tracked.

### Should have (P1)

11. **Automatic window naming** — auto-abbreviated branch name in tmux
    status bar, managed by bay, not the agent.
12. **Workspace/window listing** — `bay ls` shows all active workspaces,
    windows, and waiting status across all docks.
13. **Multiple docks** — separate tmux sessions with independent defaults.
14. **Shell tab completion** — workspace IDs, display names, branch
    names, PR numbers, and dock names autocomplete in normal shell
    (bash, zsh, fish). Flag values (`--agent`, `--repo`, `--status`,
    `--split`) also complete. Won't work inside agent `!` escapes —
    use `bay ls`. `bay setup` offers to install completions; `bay
    completion [bash|zsh|fish]` generates scripts manually.
15. **Pane monitoring** — background process detects when an agent is
    waiting for user input and highlights that window in the tmux status
    bar.
16. **Fuzzy navigation** — `bay go [query]` fuzzy-matches workspace
    names, branch names, and PR numbers. One match jumps directly;
    multiple matches open an fzf picker. `bay go --waiting` filters to
    waiting windows. `bay go --next-waiting` cycles through them.
17. **Health checks** — `bay doctor` verifies monitor is running, config
    files are valid, gitignore settings correct, tmux keybinding
    installed, manifest integrity.
18. **Manifest updates from agents** — `bay ws update self --branch <x>
    --pr <n>` lets agents update workspace metadata via `!`. Updates
    manifest and window names only — does not modify git state.

### Nice to have (P2)

19. **Seed a workspace** — spin up pre-loaded with context for a
    specific issue, task, or prompt.
20. **Session resume on recovery** — store session IDs in manifest, pass
    to agent on recovery (`claude --resume`, `codex resume`).

## Design areas

### 1. Agent config generation

Each workspace gets a gitignored config file in its CWD, generated from
a per-dock template file at workspace creation time.

**Mechanism per agent**:
- **Claude Code**: `CLAUDE.local.md` in CWD root. Must be in repo's
  `.gitignore`.
- **Codex**: `AGENTS.local.md` in CWD root. Registered via
  `project_doc_fallback_filenames` in `~/.codex/config.toml`. Must be
  in repo's `.gitignore`.
- **Others**: configured per agent type.

**Templates** are external files referenced by path in the dock config.
They can reference variables: `{workspace_id}`, `{workspace_name}`,
`{workspace_type}`, `{dock}`, `{workspace_path}`, `{dock_repo}`,
`{dock_worktree_dir}`. Bay performs variable substitution and writes the
result to the agent's config filename.

Docks without a template are valid — bay still writes the standard
preamble (workspace identity + essential bay commands) to the agent
config file, so agents always know how to update bay. The agent
additionally uses the repo's own config.

**Agent type per workspace**: A dock has a default agent type. Each
workspace (or window/pane) can override it. The config file written
depends on the agent type — bay takes the dock's template content and
writes it to whatever filename the agent definition specifies (e.g.,
`CLAUDE.local.md` for Claude, `AGENTS.local.md` for Codex).

**Launch command**: assembled as `{agent.command} {dock.agent_args...}`,
run in the workspace's CWD. The agent finds its config file through its
own discovery mechanism.

Config changes (template edits, renames) require agent restart to take
effect. Agents read their config file at startup, not mid-session.

**Gitignore verification**: Before writing any config file to a workspace
CWD, bay checks that the repo's `.gitignore` includes the config
filename. If not: refuse, explain why, offer to add it. This is checked
once per repo, at workspace creation or first agent launch.

**Cleanup**: Bay removes config files it wrote when closing a workspace.
For external workspaces, this prevents leftover config files in
directories bay doesn't own.

### 2. Worktree management

Repos are defined in config with a path and optional worktree directory.
Worktree workspaces get a worktree created under the repo's
`worktree_dir`.

- **Initial state**: detached HEAD at main.
- **Path**: `{worktree_dir}/{workspace_id}/`
- **worktree_dir default**: auto-derived as `{repo_path}-worktrees`
  if not specified in config.
- **Close safety**: check for uncommitted changes (staged/unstaged)
  and unpushed commits. Refuse if dirty unless `--force`.
- **Recovery**: worktrees survive reboot (they're just directories).
  `bay recover` reattaches them to tmux. If a worktree is missing
  (deleted manually), warn and offer to recreate or skip.

### 3. External workspaces

External workspaces point at a directory bay doesn't own — a raw repo
checkout, a project directory, or any path. Bay remembers the path for
tmux recovery and can inject agent config (with gitignore verification),
but does not create or delete the directory.

Closing an external workspace closes all its windows/panes, removes any
bay-generated config files, and archives the manifest entry. The
directory is untouched.

### 4. Manifest and workspace lifecycle

**Manifest location**: `~/.local/share/bay/manifest.toml`

**Manifest structure** (dock > workspace > window > pane):
```toml
[docks.labs.workspaces.w3]
name = "mem-refactor"
type = "worktree"                      # worktree | external
repo = "labs"
path = "~/projects/labs-worktrees/w3"
branch = "feature/refactor-memory-access"
pr = "234"
status = "active"                      # idle | active | done

[[docks.labs.workspaces.w3.windows]]
id = 1
tmux_window_id = "@4"
name = "mem-refactor"

[[docks.labs.workspaces.w3.windows.panes]]
id = 1
type = "agent"
agent = "claude"

[[docks.labs.workspaces.w3.windows.panes]]
id = 2
type = "shell"
split_from = 1
split_dir = "h"

[[docks.labs.workspaces.w3.windows]]
id = 2
tmux_window_id = "@9"
name = "mem-refactor:2"

[[docks.labs.workspaces.w3.windows.panes]]
id = 1
type = "shell"
```

**Workspace IDs** are per-dock (max+1 within the dock). The full
identifier is `dock:id` (e.g., `labs:w3`). Short form (`w3`) works when
unambiguous. Display names are unique within a dock; cross-dock
collisions require the dock qualifier.

**Window IDs** are per-workspace integers, used internally for manifest
structure. Not user-facing — users see the display name in tmux tabs
and use `self` or `bay go`.

**Pane IDs** are per-window integers, used for `split_from` references
and manifest structure.

**Concurrent writes**: file locking on all manifest writes, since
multiple agents may run `bay ws update self` concurrently.

**Lifecycle**:
1. **Create workspace** — verify gitignore safety, allocate worktree
   (worktree type) or verify path exists (external type), generate
   agent config from template, create tmux window (with
   `remain-on-exit on`), write manifest entry, launch agent or shell.
2. **Open window** — create new tmux window for existing workspace,
   add to manifest, launch agent/shell/command.
3. **Add pane** — split existing window, add pane to manifest, launch
   agent/shell/command.
4. **Update** — agent or user updates branch, PR, status via
   `bay ws update`. Updates manifest and window names only.
5. **Close window** — close the tmux window and its panes. Remove from
   manifest. Workspace and other windows unaffected.
6. **Close workspace** — close all windows/panes. Worktree type:
   safety checks, remove worktree (and config files with it). External
   type: remove config files, leave directory. Move manifest entry to
   archive.
7. **Recover** — read manifest, recreate tmux sessions/windows/panes
   for all non-closed workspaces. Idempotent — matching uses stored
   `tmux_window_id` as primary key (valid when tmux is still running),
   window name as fallback (post-reboot). For each dock, check if tmux
   session exists by name; create if missing. For each workspace and
   window: if stored window ID is valid, reuse; if not, match by name;
   if no match, create new. Recreate panes in order using `split_from`
   and `split_dir`. Skip launch for panes with live foreground
   processes. Update tmux IDs in manifest. Regenerate config files from
   templates. Start monitor if not running. Print attach commands.

**`self` resolution**: `self` resolves to the current **window**.
Matches CWD (or any subdirectory) against known workspace paths from
the manifest, then finds the window by tmux window ID. Workspace-level
operations require the explicit `ws` noun. Errors with a clear message
if CWD doesn't match any workspace.

**Dock session lifecycle**: bay uses a placeholder window (named `~`)
to keep tmux sessions alive. When a dock session is created, the
default tmux window is tagged as a placeholder via the
`@bay-placeholder` window option. This placeholder is cleaned up
automatically when the first real workspace window is created. When
the last workspace in a dock is closed, bay creates a new placeholder
to prevent tmux from destroying the session. If the user has typed in
a placeholder window (detected by checking cursor position), bay
leaves it alone rather than killing it. `bay ws new` reuses the
existing session if it's still alive.

**Archive**: closed workspaces move to
`~/.local/share/bay/archive.toml`. Not pruned automatically.

**Statuses**:
- **idle** — created, no work assigned (fresh worktree on detached main)
- **active** — has a branch, work in progress
- **done** — work complete (PR merged/closed), ready to close. Window
  name dimmed in tmux status bar.

### 5. Tmux window naming

Managed by bay, not the agent. Tracked via tmux window ID (`@N`) in the
manifest, which is robust against manual renames.

**Display name**: defaults to auto-abbreviated branch name. Common
prefixes (`feature/`, `fix/`, `chore/`, etc.) are stripped, and the
remainder is used as the display name. If no branch is set, uses the
workspace ID. `bay ws rename` overrides the auto name permanently.

**Format in status bar**: workspace display name for the primary window,
with `:N` suffix for additional windows:
- `w3` (idle, no branch)
- `mem-refactor` (auto-abbreviated from branch or renamed)
- `mem-refactor:2` (second window for same workspace)

Updated by `bay ws new`, `bay ws update`, `bay ws rename`, and
`bay win open`.

### 6. Navigation

**`bay go [query]`**: The primary navigation command. Fuzzy-matches
against workspace names, branch names, PR numbers, and dock names.

- No args: opens fzf picker showing all windows with full metadata.
- With query: filters matches. One result jumps directly. Multiple
  results open the picker pre-filtered.
- `--waiting`: filters to windows where the monitor has flagged an
  agent as waiting.
- `--next-waiting`: jumps to the next waiting window, cycling through.
  Intended as a tmux keybinding for rapid triage.

**Tmux keybinding**: `bay go --next-waiting` bound to a configurable
tmux key (default `M-w`, i.e., option/alt + w). Installed by `bay setup`.

**Picker display format**:
```
labs  w3  mem-refactor  feature/refactor-memory-access  #234  active
labs  w4  w4            —                                —    idle
```

### 7. Pane monitoring (agent-waiting detection)

Background process that detects when an agent is waiting for user input
and visually flags that window in the tmux status bar.

**How it works**:
1. Launched by `bay recover` or `bay monitor start`. PID stored at
   `~/.local/share/bay/monitor.pid`.
2. Every 2-3 seconds, reads the manifest to find windows with
   agent/cmd panes (skips shell-only windows).
3. For each window, captures last few lines of all panes via
   `tmux capture-pane`.
4. Strips ANSI escape codes, matches against configured patterns.
5. On match: sets tmux window style to highlight (e.g., red/bold)
   and sets a tmux window option flag for `bay go --waiting` to query.
6. When pattern clears: resets style and flag.

**No manifest writes**: The monitor only communicates through tmux
window styles and options. `bay go --waiting` and `bay ls` query tmux
directly. This eliminates write contention with the manifest.

**Pattern configuration**: `~/.config/bay/bay-prompts.txt` — one
regex per line, comments with `#`:
```
# Claude Code
Allow.*Deny
# Codex
\[Y/n\]
# Generic
\(y/n\)
Do you want to proceed
```

Re-read periodically so edits take effect without restart.

**Adding new patterns**:

1. **Tmux keybinding** (configurable, default `prefix + P`): installed
   by `bay setup`. Captures last non-empty lines of current pane,
   appends the most prompt-looking line to `bay-prompts.txt`, shows
   confirmation in tmux status bar.

2. **Command** (`bay add-prompt [name]`): captures last few lines of
   named window's pane (or current pane), lets user pick which line
   to add.

Edit the file afterward to generalize raw text to a regex.

### 8. Commands

All commands use explicit nouns. Abbreviations: `ws` = `workspace`,
`win` = `window`. `ls` = `list`.

| Command | What it does |
|---------|-------------|
| **Dock** | |
| `bay dock new <name> [--repo] [--agent] [--template]` | Create dock (config + tmux session) |
| `bay dock ls` | List docks and their workspaces |
| `bay dock close <name>` | Close all workspaces (safety checks per workspace) |
| `bay dock recover <name>` | Recover a single dock |
| **Workspace** | |
| `bay ws new [dock] [--repo name] [--dir path] [--name] [--agent X] [--shell]` | Create workspace (worktree by default; external with --dir). Opens first window. |
| `bay ws close <name>` | Close workspace + all windows/panes. Safety checks if worktree. |
| `bay ws show <name\|self>` | Show details (paths, branch, PR, windows, panes) |
| `bay ws update <name\|self> [--branch] [--pr] [--status]` | Update workspace metadata, refresh window names |
| `bay ws rename <name\|self> <new>` | Rename workspace display name (overrides auto-abbreviation) |
| **Window** | |
| `bay win open <workspace> [--agent X\|--shell\|--cmd "..."]` | Add window to existing workspace |
| `bay win close [self\|name]` | Close window (not workspace) |
| `bay win restart [self\|name]` | Kill agent, regenerate config, respawn |
| **Pane** | |
| `bay pane add [--agent X\|--shell\|--cmd "..."] [--split h\|v]` | Add pane to current window |
| **Navigation** | |
| `bay go [query]` | Fuzzy find and switch to window |
| `bay go --waiting` | Picker filtered to waiting windows |
| `bay go --next-waiting` | Jump to next waiting window (for keybinding) |
| **Global** | |
| `bay ls` | All workspaces/windows across all docks, with waiting status |
| `bay recover` | Reconstruct everything after reboot, print attach commands |
| `bay doctor` | Health checks |
| `bay setup` | First-time setup: config, docks, keybindings, completions |
| `bay monitor start\|stop\|status` | Manage pane monitor |
| `bay add-prompt [name]` | Capture agent-waiting pattern |
| `bay completion [bash\|zsh\|fish]` | Generate shell completion script |

**`bay win restart` mechanism**: With `remain-on-exit on`, exited agents
leave the pane in "dead" state. `bay win restart` handles both live and
dead panes:
1. If agent is running: send SIGTERM to pane PID, wait for exit.
2. Regenerate config file from current dock template.
3. `tmux respawn-pane -t <window_id> -c <cwd> '<agent command>'`

**`bay dock new`**: Writes a new `[docks.x]` section to config.toml
and creates the tmux session. Errors if a tmux session with that name
already exists and isn't a bay dock.

**`bay dock close` safety**: Runs the full `bay ws close` safety
sequence for each workspace in the dock. Refuses if any workspace fails
its checks (override with `--force`).

**Dock inference for `bay ws new`**: Infers dock from the current tmux
session name. Errors if outside tmux or if the current session is not a
bay dock — both cases require explicit dock argument.

**`bay ls` output**:
```
Dock: labs
  w3  mem-refactor  claude  feature/refactor-memory-access  #234  active  ⏳
  w4  w4            claude  —                                —    idle
Dock: research
  w1  perf-study    claude  —                                —    idle
```

The `⏳` indicator comes from querying tmux window state (set by the
monitor).

**`bay setup`**: Interactive first-time walkthrough — creates
`~/.config/bay/config.toml` with agent definitions and repos, creates
initial docks, seeds `bay-prompts.txt`, installs shell completions
(detects bash/zsh/fish), and installs tmux keybindings.

**`bay doctor`**: Checks config file parses correctly, all repo paths
are accessible, gitignore rules are in place for each repo+agent
combination, monitor is running (checks PID file), tmux keybindings
are installed, manifest is consistent with actual worktree/tmux state.

### 9. Configuration

**Config dir**: `~/.config/bay/`
**Data dir**: `~/.local/share/bay/`

**Config file**: `~/.config/bay/config.toml`

```toml
[agents.claude]
command = "claude"
config_file = "CLAUDE.local.md"

[agents.codex]
command = "codex"
config_file = "AGENTS.local.md"

[repos.labs]
path = "~/projects/labs"
worktree_dir = "~/projects/labs-worktrees"    # optional, defaults to {path}-worktrees

[repos.ct-server]
path = "~/projects/ct-server"
# worktree_dir defaults to ~/projects/ct-server-worktrees

[docks.labs]
repo = "labs"                                  # references [repos.labs]
agent = "claude"                               # default agent for new workspaces
agent_args = ["--add-dir", "~/crew/projects/assistant"]
agent_config_template = "~/.config/bay/templates/labs.md"

[docks.research]
repo = "labs"
agent = "claude"
agent_config_template = "~/.config/bay/templates/research.md"

[monitor]
interval_seconds = 3

[keybinding]
add_prompt = "P"           # prefix + P: capture prompt pattern
next_waiting = "M-w"       # option/alt + w: bay go --next-waiting
```

**Template files** live in `~/.config/bay/templates/`. Example
(`labs.md`):
```markdown
You are a crew worker. You have access to crew data at ~/crew/.

Your workspace is {workspace_name} (ID: {workspace_id}) in the
{dock} dock.

When you create a branch or open a PR, run:
  !bay ws update self --branch <branch-name> --pr <number>
```

**Data dir contents** (`~/.local/share/bay/`):
- `manifest.toml` — active workspace/window/pane state
- `archive.toml` — closed workspace history
- `monitor.pid` — pane monitor PID file

## Crew integration

Bay is generic. Crew-specific behavior is configured entirely through
the dock's template file. The template injects:

- Crew conventions and worker identity
- Paths to crew data (`~/crew/projects/assistant/`)
- Instructions for workspace state management (`bay ws update self`)
- Append-only convention for shared state files

This keeps bay reusable and crew concerns in crew config.

## Distribution

Separate GitHub repo. Go, compiled to a single binary with no runtime
dependencies. Fast startup (~5ms) matters because `bay ls`, tab
completion, and `bay ws update self` from agents run frequently.

**Installation**:
- **Homebrew tap**: `brew install <user>/tap/bay` — primary install
  method for macOS.
- **GitHub releases**: pre-built binaries for macOS (arm64/amd64) and
  Linux. Download or `curl | tar`.
- **`go install`**: `go install github.com/<user>/bay@latest` — for
  users with Go toolchain.

**Release automation**: goreleaser, triggered by `git tag` + push in
GitHub Actions. Cross-compiles, publishes to GitHub releases, and
updates the Homebrew tap formula.

## One-time setup (per target repo)

- Add `CLAUDE.local.md` to `.gitignore` (for Claude Code workspaces)
- Add `AGENTS.local.md` to `.gitignore` (for Codex workspaces)
- Configure Codex: add `project_doc_fallback_filenames = ["AGENTS.local.md"]`
  to `~/.codex/config.toml`

Bay verifies gitignore entries before writing config files and offers to
add them if missing.

## Out of scope

- Dispatching work to workspaces autonomously
- Auto-creating workspaces from issues or notifications
- Managing non-bay agent sessions (e.g., the overseer)
- Moving workspaces between docks
- Archive pruning (future consideration)
- Scratch workspaces (v1 — may revisit if needed)
