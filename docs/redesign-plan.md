# Bay v3 — Surface Model Redesign

## What and why

Bay currently models workspaces as Dock → Workspace → Window → Pane, with
navigation as a flat global jumper across all tmux windows. This redesign
replaces Window and Pane with **Surface** — a unified navigable entity that
can be a tmux pane or a GUI application (editor, terminal). Navigation
becomes scoped: jump between surfaces within a workspace, or between
workspaces within a dock.

The motivation is threefold:
1. GUI editors (Cursor, VS Code) should be first-class workspace entities,
   not fire-and-forget launches.
2. Navigation should match the user's mental model: "switch tools within
   my current task" vs "switch to a different task."
3. The Window/Pane hierarchy adds a level of indirection that doesn't
   correspond to how users think.

## Design principles (updated)

Existing principles carry forward, plus:

- **Surfaces are the unit of navigation.** Anything you can focus and jump
  to is a surface. The user doesn't think in tmux windows and panes — they
  think "agent," "editor," "shell."
- **Invisible when you don't need it.** Automatic metadata, serendipitous
  notifications, background work. Bay never interrupts.
- **Incremental investment, incremental benefit.** Zero config gets you
  basic workspaces. Adding config unlocks templates, GUI integration,
  Space switching.
- **macOS-optimized, Linux-compatible.** GUI surfaces and Space switching
  are macOS enhancements. The core works everywhere.

## Data model

```
Dock → Workspace → Surface
```

### Manifest (JSON, machine-managed)

```go
type Manifest struct {
    Version int    `json:"version"`
    Docks   []Dock `json:"docks"`
}

type Dock struct {
    Name       string      `json:"name"`
    Host       *GUIAttrs   `json:"host,omitempty"`
    Workspaces []Workspace `json:"workspaces"`
}

type Workspace struct {
    Name        string          `json:"name"`
    Type        WorkspaceType   `json:"type"`
    Path        string          `json:"path,omitempty"`
    Status      WorkspaceStatus `json:"status"`
    LastFocused int             `json:"last_focused,omitempty"`
    Surfaces    []Surface       `json:"surfaces"`
    Worktree    *WorktreeAttrs  `json:"worktree,omitempty"`
}

type WorktreeAttrs struct {
    Repo   string `json:"repo"`
    Branch string `json:"branch"`
    PR     string `json:"pr,omitempty"`
}

type Surface struct {
    ID      int            `json:"id"`
    Name    string         `json:"name"`
    Type    SurfaceType    `json:"type"`
    Backend SurfaceBackend `json:"backend"`
    Agent   *string        `json:"agent,omitempty"`
    Command *string        `json:"command,omitempty"`
    Tmux    *TmuxAttrs     `json:"tmux,omitempty"`
    GUI     *GUIAttrs      `json:"gui,omitempty"`
}

type TmuxAttrs struct {
    PaneID      string `json:"pane_id,omitempty"`
    WindowID    string `json:"window_id,omitempty"`
    LayoutGroup int    `json:"layout_group"`
    SplitFrom   int    `json:"split_from,omitempty"`
    SplitDir    string `json:"split_dir,omitempty"`
}

type GUIAttrs struct {
    AppCommand string `json:"app_command"`
    BundleID   string `json:"bundle_id,omitempty"`
    PID        int    `json:"pid,omitempty"`
}
```

Key decisions:
- All collections are slices (not maps) for consistency and ordering.
- Surface has ID (for cross-references: SplitFrom, LastFocused) + Name.
- Dock and Workspace identified by Name only.
- Type and Backend are symmetric axes with corresponding sub-structs.
- TmuxAttrs.PaneID and WindowID are ephemeral (cleared on recover).
- LayoutGroup is stable (surfaces sharing the same value share a tmux window).
- Terminal is a dock-level Host, not a surface.
- Manifest is JSON (not TOML) for better nesting support and stdlib.
- Config stays TOML (human-edited, flat).

### Config additions (TOML)

```toml
[agents.claude-code]
command = "claude"
resume_args = "--continue"
project_file = "CLAUDE.md"

[templates.default]
surfaces = ["agent", "shell"]

[templates.full]
surfaces = ["agent", "editor", "shell"]

[docks.bay]
repo = "bay"
agent = "claude-code"
terminal = "ghostty"
template = "default"

[editor]
command = "cursor"
```

## CLI command structure

### Top-level shorthands

```
bay go [query]         surface go — navigate among surfaces
bay tree               full hierarchy view
bay restart [name]     surface restart
bay shell              surface new shell
bay edit               surface new editor
```

### Noun + verb commands

```
bay surface {go, ls, show, new, close, rename, restart}   (alias: sf)
bay workspace {go, ls, tree, show, new, close, rename}    (alias: ws)
bay dock {ls, tree, show, new, close, rename, recover}
bay repo {ls, show, add, remove, init}                     (new/close as aliases)
```

### Infrastructure and other

```
bay setup              first-time setup (zero-config capable)
bay recover            reconstruct all state after reboot
bay doctor             health checks and platform capability report
bay monitor {start, stop, status}
bay pwd                current context
bay status-line        tmux status line output
bay completion {bash, zsh, fish}
bay version
```

### Help structure

Default `bay help` shows only the core workflow:

```
Quick start:
  bay ws new          create a workspace
  bay go [query]      jump to a surface
  bay ws go [query]   jump to a workspace
  bay tree            see everything
  bay edit            open editor
  bay shell           open shell

Run 'bay help --all' for all commands.
```

## Keybindings

All option-key (Meta), no tmux prefix required:

```
option-j / option-k       next/prev surface (intra-workspace)
option-J / option-K       next/prev workspace (intra-dock)
option-g                  picker: surfaces in current workspace
option-G                  picker: workspaces in current dock
option-a                  next waiting surface
option-A                  next waiting workspace
option-1..9               jump to surface by index
```

Lowercase = intra-workspace. Uppercase = intra-dock.

### Visual cycling popup

Each option-j/k press immediately switches and shows a tmux display-popup
with the full surface list and current position highlighted. Hold option,
tap j/j/j to cycle. Popup auto-dismisses ~1s after last press. Same
pattern for option-J/K with workspaces.

## Focus mechanics

### Focus matrix

| From → To                | Mechanism                                    |
|--------------------------|----------------------------------------------|
| tmux → tmux (same dock)  | tmux select-window + select-pane             |
| tmux → editor            | Editor CLI (`code /path`) — switches Spaces  |
| editor → tmux            | bay-focus helper + activate terminal         |
| tmux → tmux (cross-dock) | bay-focus helper + activate host + tmux select|

### bay-focus Swift helper (optional, macOS only)

~60 lines of Swift. Finds which Space a window is on
(CGSCopySpacesForWindows), switches to that Space (CGEvent keyboard
simulation of Ctrl+N), and activates the target app (NSWorkspace).

Requirements: Accessibility permission, Ctrl+1..9 Space shortcuts enabled.
Does NOT require: Screen Recording, SIP disabled, window title matching.

Validated with working prototypes (2026-04-03).

### Graceful degradation

Without the helper: all tmux navigation works, editor CLIs handle their
own Space switching, terminal activation works but may not switch Spaces.
User switches Spaces manually — minor friction, nothing breaks.

## Automatic workspace metadata

No manual `workspace update` command. All metadata is auto-detected:

- **Branch**: live detection via `git rev-parse` (local, free).
- **PR number**: detected once via `gh pr view`, cached forever.
- **Status**: idle → active (branch detected) → done (merge detected).

### Background merge detection

The monitor daemon handles this with activity-gated fetching:

1. Only `git fetch` repos with recent workspace activity (last 1-2 hours).
2. After fetch, check if branch is merged into main (local git check).
3. Update manifest status to `done`.

No fetches for idle repos or overnight.

### Merge notifications

Serendipitous, not intrusive:
- Status line shows count of merged workspaces.
- `bay tree` / `bay ws ls` highlights merged workspaces.
- Picker shows "merged" tag.
- Navigating to a merged workspace shows a one-line suggestion to close.

## Agent handling

### No more generated config files

Bay stops generating `CLAUDE.local.md` (or equivalent) into worktrees.
The current machinery — `generateAgentConfig`, `cleanupAgentConfig`,
`config_file` field, `agent_config_template`, gitignore checks, variable
substitution — is all eliminated.

Instead:
- **Project instructions** go in the repo's `CLAUDE.md` (tracked, shared
  across all worktrees via git).
- **Bay awareness** is a one-line addition to `CLAUDE.md`:
  `"This project uses bay for workspace management. Run bay agent-guide
  for commands."`
- **Gitignored files** (`.env`, `CLAUDE.local.md` if the user has one)
  are copied to worktrees via `.worktreeinclude` support.
- **Per-workspace context** (dock, workspace name, path) is discoverable
  via `bay pwd --json`. The agent doesn't need it injected.

This eliminates a significant amount of code and config surface area.
Per-dock template variations are dropped (no demonstrated need). Can be
re-added later if needed.

### .worktreeinclude support

Bay respects `.worktreeinclude` files (same format as Claude Code uses).
At worktree creation time, bay reads `.worktreeinclude` from the repo
root, finds matching gitignored files, and copies them into the new
worktree. Uses `.gitignore` pattern syntax.

### `bay repo init` — agent-agnostic project setup

Idempotent command that sets up bay awareness in a repo:

```
bay repo init [name]      # by repo name
bay repo init             # infer repo from CWD
```

For each configured agent with a `project_file` (e.g. `CLAUDE.md`),
checks if the file mentions bay. If not, appends a one-liner pointing
to `bay agent-guide`. Also sets up `.worktreeinclude` if missing and
ensures `.gitignore` has necessary entries.

`bay repo add` calls `repo init` automatically after adding.
`bay doctor` flags repos missing bay awareness and suggests `repo init`.

The `project_file` field on agent config makes this agent-agnostic —
bay writes the awareness line to whatever file each agent reads.

### Session resumption

Agent config has a `resume_args` field (e.g. `--continue` for Claude Code).
On first launch: bare command. On restart: command + resume_args.
Claude Code binds sessions to project directories, so `--continue` in the
same worktree resumes the right session automatically.

### Protection

Agent surfaces have valuable conversation context. Protections:
- Confirmation prompt on closing an agent surface.
- `bay surface restart` uses resume_args to reconnect, not start fresh.
- Consider a `precious` flag on surfaces for close protection.

## Implementation plan

### Phase 0: Foundation

Parallel workstreams, no user-facing changes yet.

**0a. Manifest rewrite (TOML → JSON)**
- Replace TOML manifest with JSON. No migration code — delete old
  format, start fresh. No existing users to migrate.
- Version field on manifest for future migrations (once bay has users).
- Atomic writes (temp file + rename) and backup of previous version.

**0b. Built-in picker**
- ~150-200 lines of Go: terminal raw mode, ANSI rendering, fuzzy filter.
- Replace all fzf shelling-out with the built-in picker.
- Bay-aware display: surface types, workspace status, colors.

**0c. Platform abstraction**
- Interface for focus operations: `Focus(surface)`, `Activate(app)`,
  `SwitchSpace(windowID)`.
- macOS implementation using osascript, editor CLIs, bay-focus helper.
- Stub Linux implementation (focus = tmux only, no GUI surfaces).

### Phase 1: Core model change

The big refactor. Lands as one coherent change.

**1a. New data model**
- Replace Window + Pane with Surface in manifest structs.
- Implement Dock, Workspace, Surface, TmuxAttrs, GUIAttrs, WorktreeAttrs.
- Surface ID management (NextSurfaceID, never-reuse).
- Layout group management.
- No migration from old model — clean cut.

**1b. Engine rewrite**
- Workspace creation creates surfaces from templates.
- Surface add/close/rename/restart operations.
- Replace all Window/Pane engine code with Surface equivalents.
- Context resolution: current tmux pane → which surface am I in?
- LastFocused tracking.
- Remove agent config generation (`generateAgentConfig`, `cleanupAgentConfig`,
  `config_file`, `agent_config_template`, gitignore checks, variable substitution).
- Add `.worktreeinclude` support: on worktree creation, read patterns from
  repo root, copy matching gitignored files into the new worktree.

**1c. CLI restructure**
- New noun+verb command structure (surface, workspace, dock, repo).
- Abbreviation aliases (sf, ws).
- Top-level shorthands (go, tree, restart, shell, edit).
- Remove old commands (win, pane, close-pane).
- Help structure: focused default, --all for complete list.

**1d. Navigation rewrite**
- `bay go` / `bay surface go`: intra-workspace fuzzy nav using built-in picker.
- `bay ws go` / `bay workspace go`: intra-dock fuzzy nav.
- `bay surface next/prev`: cycling with popup display.
- `bay workspace next/prev`: same for workspaces.

### Phase 2: GUI integration

Depends on Phase 1. macOS-only features behind capability detection.

**2a. Editor surfaces**
- `bay edit` creates a gui-app surface, launches editor CLI, records PID.
- Liveness probing (is the editor still running?).
- Remove dead editor surfaces from manifest.
- Focus via editor CLI (`code /path`).

**2b. Dock host terminal**
- Config: `terminal = "ghostty"` on dock.
- Track host terminal in Dock.Host (GUIAttrs).
- `bay dock new` launches terminal attached to tmux session.
- Recovery relaunches terminal.

**2c. bay-focus helper**
- Compile Swift helper during `bay setup`.
- `bay doctor` checks: helper present, Accessibility granted,
  Ctrl+N shortcuts enabled.
- Focus operations use helper when available, degrade gracefully.

### Phase 3: Keybindings and popup

Depends on Phase 1 navigation rewrite.

**3a. Keybinding installation**
- `bay setup` installs option-key bindings in tmux config.
- Intra-workspace: option-j/k (next/prev), option-g (picker), option-1..9.
- Intra-dock: option-J/K (next/prev), option-G (picker).
- Waiting: option-a/A (next waiting surface/workspace).

**3b. Visual cycling popup**
- `bay surface next --popup` / `bay surface prev --popup`.
- Render surface list in tmux display-popup.
- Current position highlighted, auto-dismiss after ~1s.
- Same for workspace cycling.

### Phase 4: Automatic metadata and monitoring

Depends on Phase 1.

**4a. Automatic branch/PR detection**
- Branch: already implemented (sync.go), verify it works with new model.
- PR: lazy detection via `gh pr view` on first display, cache in
  WorktreeAttrs.PR.

**4b. Merge detection**
- Add `last_active` timestamp to Workspace, updated on bay commands.
- Monitor daemon: activity-gated `git fetch` per repo.
- Post-fetch: check branch merge status, update workspace status to `done`.
- Serendipitous notifications across display surfaces.

**4c. Agent session resumption**
- `resume_args` in agent config.
- `bay surface restart` applies resume_args.
- Confirmation prompt on agent surface close.

### Phase 5: Zero-config and polish

**5a. Zero-config experience**
- `bay ws new` works with no config file: auto-detect CWD as repo,
  create default dock, detect agent on PATH, create workspace with
  default template.
- Config file only needed for customization.

**5b. Workspace templates**
- Template definitions in config.
- Default template per dock.
- `bay ws new --template <name>`.

**5c. `bay repo init`**
- Idempotent project setup: append bay awareness line to each agent's
  `project_file`, set up `.worktreeinclude`, ensure `.gitignore` entries.
- `bay repo add` calls `repo init` automatically.
- `bay doctor` flags repos missing bay awareness.

**5d. Documentation rewrite**
- Rewrite `docs/tutorial.md` for new model (surfaces, new CLI, keybindings).
- Rewrite `docs/human-guide.md` for new commands and workflows.
- Rewrite `docs/agent-reference.md` for new model (surfaces replace
  windows/panes, new JSON output schemas, updated command reference,
  remove `ws update` instructions).
- Update `bay agent-guide` hidden command output to match.

**5e. Setup and doctor updates**
- `bay setup` guides through incremental setup: basic → editor →
  terminal → Space switching.
- `bay doctor` reports platform capabilities and missing optional
  features, including missing `repo init` for configured repos.

### Phase 6: Future work (not planned in detail)

- Cross-dock navigation (dock go) with Space switching.
- Multiple monitors / multiple dock hosts.
- Agent-specific session ID tracking for non-directory-based agents.
- Workspace sharing / pair programming support.

## Migration strategy

No migration code. No existing users to migrate. Clean cut:

1. Delete old TOML manifest code and data model.
2. Start fresh with `bay setup`.
3. Old commands (`win`, `pane`, `close-pane`) are simply removed.

Worktrees on disk are unaffected — they're just git worktrees. Recreate
workspaces as needed.

## Implementation status

**Done (merged to main):**
- Phase 0a+1a: manifest package rewrite (JSON, Surface types, 43 tests) — PR #51
- Phase 1b: engine rewrite (all operations use Surface model) — PR #52
- Phase 1c partial: CLI updated to compile with new types — PR #52
- Nav and monitor packages updated — PR #52
- Test rewrite: all test files updated for Surface model — PR #53
- Config cleanup: removed ConfigFile/AgentConfigTemplate, added
  ResumeArgs/ProjectFile/Terminal/Template — PR #54

- Phase 1c: CLI restructure — surface/sf command, removed win/pane commands — PR #55
- Phase 1d: navigation infrastructure (picker, SelectPane, surface entries) — PR #56
- Phase 1d: navigation commands (surface go/next/prev, ws go/next/prev) — PR #57
- Phase 1d: keybindings (21 bindings: surface/ws nav, index jump, utility) — PR #58

- Phase 4a: automatic PR detection via `gh pr view` during sync — PR #59

- Phase 5a: zero-config (auto-bootstrap from CWD, agent probing) — PR #60

- Phase 5c: bay repo init (bay awareness, .worktreeinclude) — PR #62

- Phase 5e: setup/doctor updates (remove stale fields, add agent/repo checks) — PR #63

- Phase 2a: editor surfaces (GUI surfaces, liveness probing, focus) — PR #64

- Phase 2b: dock host terminal (launch/recover terminal app) — PR #65

- Phase 2c: bay-focus Swift helper (compile, doctor check, Accessibility) — PR #66

- Phase 4b: merge detection (activity-gated fetch, status→done) — PR #68

- Phase 4c: agent session resumption (resume_args on restart/recovery) — PR #69

- Phase 5d: documentation rewrite (tutorial, human guide, agent guide) — PR #70

## What's NOT in scope

- Cross-dock navigation (deferred to Phase 6).
- Browser as a workspace entity.
- Non-macOS GUI surface support (Linux GUI integration).
- Complex agent session management beyond resume_args.
- Multi-user / shared workspace features.
