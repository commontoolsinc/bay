# Command palette — design plan

Captured 2026-04-20. No code has been written. The design emerged from a
discussion about extending bay's tmux keybindings. The user wanted a
lightweight way to launch a non-default agent (codex, gemini) without
dropping to a shell. That conversation made it clear the keybinding space
is already dense — every new feature argues for "just one more key" and
the argument never ends. A VS Code-style command palette solves the
agent-picking case and takes the long tail of rare-but-useful commands
off the keystroke budget permanently.

## The problem

Bay has ~13 tmux keybindings today, covering the frequent operations
(surface/workspace navigation, create shell/agent, open editor, close
surface). Several useful commands have no keybinding at all (rename,
describe, close --done, config edit, doctor). Adding new bindings is
getting expensive:

- The unshifted/shifted pairs for "window vs. pane" have already
  consumed the shift modifier on `M-a` / `M-s` / `M-e`.
- Adding "launch a specific agent" needs at minimum another keystroke,
  and ideally per-axis variants for (surface vs. workspace) × (window
  vs. pane) × (which agent) — that's a combinatorial explosion.
- Users have to memorize all of this from `bay agent-guide` or
  `docs/human-guide.md`. Nothing is discoverable from within bay.

The palette is a second tier: keep fast keystrokes for the handful of
daily-use commands, route everything else through a fuzzy-searchable
picker. New commands land in the palette at zero keystroke cost, and
the parametric ones (pick agent type, pick workspace) get a natural UI.

---

## Design

### Invocation

- `M-p` opens the palette in **pane** mode (split the current window
  for creates that can split).
- Mode only affects the 5 create-surface entries; everything else is
  mode-agnostic.
- Mode can be flipped mid-palette with `Tab`.

The binding uses tmux `display-popup -E` so the palette floats over
the current pane and returns cleanly, regardless of whether the user
is in an agent surface, a shell, or an editor.

### Out-of-bay behavior

If the user hits `M-p` in a tmux session that isn't a bay dock,
`bay palette` exits 0 silently — same pattern as the other bay
bindings that `|| true` to stay invisible outside bay.

### UI

Popup hosts a fuzzy picker built on the existing `internal/picker`
package. Plain text rendering, no TUI framework.

```
>

Recent
  Rename workspace...                                      M-?
  Go to workspace...                                       M-G
  New agent...
──────
Navigation
  Go to surface...                                         M-g
  Go to workspace...                                       M-G
  Show current context
Create — surface                        [mode: window · Tab: flip]
  New shell                                                M-s
  New agent                                                M-a
  New agent...
  New cmd...
  Open editor (workspace)                                  M-e
  Open editor (dock)                                       M-E
Create — workspace
  New workspace                                            M-c
  New workspace with default agent                         M-C
  New workspace with agent...
...

Esc: close
```

Right-aligned column shows the keystroke that would launch the same
command directly, detected live from `tmux list-keys` (see
[Live hotkey detection](#live-hotkey-detection)).

### Escape semantics

`Esc` always closes the palette, from any depth. No "back one step" —
the user re-opens with `M-p` if they picked wrong. Uniform and simple;
matches VS Code.

### Command list (v1)

24 entries, 6 parametric. Parametric entries are marked with `...` in
the title and chain to a sub-picker or inline text prompt.

#### Navigation (3)

| # | Title | Needs scope | Parametric |
|---|-------|-------------|------------|
| 1 | Go to surface... | in workspace | pick surface |
| 2 | Go to workspace... | in dock | pick workspace |
| 3 | Show current context | anywhere | — |

#### Create — surface (6, mode-sensitive)

| # | Title | Needs scope | Parametric |
|---|-------|-------------|------------|
| 4 | New shell | in workspace | — |
| 5 | New agent | in workspace | — |
| 6 | New agent... | in workspace | pick agent type |
| 7 | New cmd... | in workspace | text: command |
| 8 | Open editor (workspace) | in workspace | — |
| 9 | Open editor (dock) | in dock | — |

#### Create — workspace (3)

| # | Title | Needs scope | Parametric |
|---|-------|-------------|------------|
| 10 | New workspace | in dock | — |
| 11 | New workspace with default agent | in dock | — |
| 12 | New workspace with agent... | in dock | pick agent type |

#### Current workspace (5)

| # | Title | Needs scope | Parametric |
|---|-------|-------------|------------|
| 13 | Rename workspace... | in workspace | text: new name (prefilled) |
| 14 | Describe workspace... | in workspace | text: description (prefilled) |
| 15 | Clear workspace description | in workspace | — |
| 16 | Show workspace details | in workspace | — |
| 17 | Close workspace | in workspace | — |

#### Current surface (3)

| # | Title | Needs scope | Parametric |
|---|-------|-------------|------------|
| 18 | Rename surface... | in workspace | text: new name (prefilled) |
| 19 | Close surface | in workspace | — |
| 20 | Show surface details | in workspace | — |

#### Admin (4)

| # | Title | Needs scope | Parametric |
|---|-------|-------------|------------|
| 21 | Edit config | anywhere | — |
| 22 | Show config | anywhere | — |
| 23 | Run doctor | anywhere | — |
| 24 | Monitor status | anywhere | — |

#### Explicitly excluded from v1

These were considered and dropped. Revisit if real usage demands
them.

- **Recover** — by the time you're inside a bay session (which is
  when the palette is reachable), state is already reconstructed.
  No inside-bay use case exists.
- **Batch close** (`close --done`, `--dry-run`, `--clean`) — useful
  at end-of-day but niche; easy to add later.
- **Cross-workspace actions** (rename/describe/close another ws) —
  `ws go` then act covers it.
- **Parametric workspace creates** (`--branch`, `--dir`, named
  workspaces, `--description` at create time) — drop to shell for
  these edge cases.
- **Dock / repo management** (`dock new`, `repo add`, etc.) — shell
  territory. Too many flags for a palette flow.
- **Set default editor / start or stop monitor / show config path /
  clear description** — too niche.

### Hidden-when-inapplicable

Entries whose `Needs` scope isn't met are hidden entirely. Simpler
than grey-out, and the palette stays short. The only awkward case is
"in a dock but not a workspace," which happens when the user is in
a dock's "host terminal" surface — at that point workspace-scoped
entries hide and the palette shows the dock-scoped and anywhere
entries. Clean single rule.

### Parametric entry UX

Six entries chain to further input. Two flavors.

#### Sub-picker (entries 6, 12)

Selecting "New agent..." replaces the item list with agent types
drawn from `KnownAgents` (claude, codex, gemini) plus any entries
from `config.Agents`. Prompt changes to reflect depth:

```
agent type >
  codex                 ← most recent (sub-picker MRU, cap 5)
  claude
  ──
  claude
  codex
  gemini
  my-claude-sonnet
```

- Same picker widget, new items. Fuzzy filter works.
- Mode (window/pane) locks at sub-picker entry; Tab still flips it.
- Enter launches. Esc closes the palette entirely.

#### Inline text (entries 7, 13, 14, 18)

Prompt line becomes editable. The item list disappears — nothing
to pick once you've committed to an action.

```
rename workspace: auth-fix█
```

Controls:
- Printable chars insert at cursor
- Backspace deletes
- **Ctrl-U clears the line** (critical for rename — lets you type-over
  the prefilled current name)
- Left/Right arrows move cursor (stretch; skip for v1 if not already
  in `picker.go`)
- Enter submits
- Esc closes palette

Prefill conventions:
- **Rename workspace** — current workspace name, cursor at end
- **Describe workspace** — current description (or empty), cursor at end
- **Rename surface** — current surface name, cursor at end
- **New cmd** — empty

#### Errors

Validation stays in the CLI (names match `[a-zA-Z0-9_-]+`, description
≤ 80 chars, etc.). If the forwarded CLI call fails, the popup shows
the error on a line below the prompt and waits for any key, then
closes. No double-validation in the palette code.

#### Two-field case

`bay surface new cmd` accepts an optional surface name as a second
positional. The palette's v1 text-entry widget is single-field; the
surface gets auto-named. If this bites, add a second prompt step
later.

### Mode (window vs. pane)

Window/pane only applies to create-surface entries (4–9). For all
others the mode is a no-op, but the footer still shows it for
consistency:

```
[mode: window · Tab: flip]
```

- `M-p` starts in pane.
- Tab flips live during the main list and any sub-picker.
- Mode locks when text entry begins (Tab during text entry is
  reserved for a possible future completion UX).

Terminals do not report modifier press/release as separate events, so
"rewrite options when Shift is held" is not possible via stdin. The
invoke-keystroke + Tab combo is the practical alternative.

### Recents

Hybrid: strictly MRU for the top slot (1 item), frequency-among-last-20
for the rest (4 items shown).

```
Recent
  New agent (codex)            ← latest (MRU, 1 slot)
  Go to workspace...           ← most frequent in last 20
  New agent (claude)
  Go to surface...
  Edit config
──────
Navigation
  Go to surface...             ← still here too
  ...
```

Design rules:

- **Top slot = MRU.** Whatever the user last completed lands here.
  Muscle memory for "M-p, Enter = redo last." Also the
  first-use-visibility story: a freshly-used command is always
  reachable at the top even without frequency history.
- **Slots 2–5 = frequency-among-last-20**, ties broken by recency.
  Stable across sessions. One-off commands don't displace daily ones.
- **Dedupe** — if the latest is also the top-frequent (common when
  the user is in a task flow), it shows only in the top slot; skip
  it below. List may collapse to 4 entries, which is fine.
- **Categorical list always shows every entry too**, including ones
  in the recents section. No bouncing around when an entry enters
  or leaves recents.

#### Parametric commands in recents

For the "redo last" gesture to actually work, sub-picker parametrics
are stored *fully bound*. Text-input parametrics are stored unbound.

| Command kind | Recent stores | Rendered as | Invoke behavior |
|---|---|---|---|
| Non-parametric | `id` | "Go to workspace..." | launches |
| Sub-picker parametric | `id + param` | "New agent (codex)" | launches bound |
| Text-input parametric | `id` | "Rename workspace..." | re-prompts with live prefill |

Rationale: "Rename workspace to auth-fix" is meaningless as a
persistent recent — after the rename, that text is stale. The prompt's
own prefill from current state is the right source of truth. For
sub-picker parametrics the chosen value is durable intent ("I always
launch codex"), so binding it in the recent preserves "M-p, Enter =
actually redo."

Frequency is counted per unique `(id, param)` pair. Launching claude
once and codex twice yields two distinct recent entries with separate
frequency counts.

#### Sub-picker recency

The agent-type sub-picker also has its own small MRU (cap 5) shown at
the top of its list. With fully-bound top-level recents, this is
secondary — top-level recents already make "repeat codex" a two-key
gesture. The sub-picker MRU only helps when the user enters the
sub-picker via the categorical "New agent..." entry, typically to try
a different agent than usual. Keep it; it's cheap polish.

Other parametric inputs don't get recency:
- **Rename / describe / rename surface** — prefill from *current*
  state, not history (live beats stale).
- **New cmd** — freeform text, recency would be noise.

#### What counts as "used"

Successful completion. Canceled sub-pickers, validation failures, and
CLI errors do not touch the recents file. Keeps recents from filling
with mistakes.

#### Filter mode

Once the user types a filter query, recents/categorical grouping
collapses: show a single flat list of all matches, deduped by
command ID. Section headers only appear at empty filter.

### Live hotkey detection

At palette startup, run `tmux list-keys -T root`, parse bindings whose
command starts with `bay `, and annotate each palette entry with its
matching keystroke.

#### Why live

If the user customizes their tmux bindings (e.g., rebinds `M-g` to
`bay go --pick` in `.tmux.conf`), the palette should teach that, not
whatever bay installed. Using our canonical `bayKeybindings` table
would lie in that case.

#### Matching

Each `Command` declares a signature:

```go
type Command struct {
    ...
    HotkeyMatch  string             // "bay ws go --pick"
    HotkeyByMode map[Mode]string    // for mode-sensitive entries
}
```

Lookup is exact-match of the signature against the quoted command in
each `bind-key` line (after stripping trailing `|| true`). Exact match
avoids false positives from prefix matching.

Mode-sensitive entries (e.g., "New shell") declare two signatures:

```go
HotkeyByMode: map[Mode]string{
    ModeWindow: "bay shell --window",
    ModePane:   "bay shell --pane",
},
```

Rendered hotkey follows current palette mode; Tab flipping updates
annotations in-place, teaching "shift toggles the split variant."

#### Rendering

Right-aligned column at a fixed offset (say 60 chars from left).
Entries without a matching binding leave the column blank — no
placeholder text.

#### Edge cases

- Not under tmux — skip the lookup, no annotations.
- `tmux list-keys` fails for any reason — silent fallback to no
  annotations. Don't fail the palette.
- User has a custom binding `bay agent codex` — won't match any
  current palette signature. Could be extended later to detect
  "argument-bearing" variants, but not for v1.

#### Cost

One subprocess call per palette invocation (~5ms). Not cached —
customizations picked up without a restart.

---

## Implementation

### Package layout

```
internal/palette/
  palette.go        Run entry point, command dispatch
  commands.go       the 24 Command definitions
  recents.go        MRU-latest + frequency storage
  hotkeys.go        tmux list-keys parser
  palette_test.go
  recents_test.go
  hotkeys_test.go
```

Extend `internal/picker` with one new entry point:

```go
// Prompt shows an editable text line and returns the submitted value.
// Returns (value, true, nil) on Enter, ("", false, nil) on Esc.
func Prompt(label, prefill string, in *os.File, out *os.File) (string, bool, error)
```

Same raw-mode loop as `Run`, but with cursor tracking and Ctrl-U/A/E
support. The filter-picker stays untouched.

Add one new file path in `config.Paths`:

```go
PaletteRecents: filepath.Join(dataDir, "palette-recents.json"),
```

Atomic write (temp + rename), same pattern as `manifest.json` and
`archive.json`.

### Command registry

Static table. Each command delegates to an existing runner in
`internal/cli` — no new engine logic:

```go
type Command struct {
    ID          string          // stable — used by recents ("go-workspace")
    Title       string          // "Go to workspace..."
    Section     Section
    Needs       Scope           // Anywhere | InDock | InWorkspace
    CanSplit    bool            // honors Mode
    HotkeyMatch string
    HotkeyByMode map[Mode]string
    Run         func(*RunContext) error
}
```

Example entry:

```go
{
    ID:      "new-agent-pick",
    Title:   "New agent...",
    Section: SectionCreateSurface,
    Needs:   InWorkspace,
    CanSplit: true,
    HotkeyByMode: map[Mode]string{
        // no default key for "pick agent" today
    },
    Run: func(c *RunContext) error {
        agent, ok := c.PickAgent()
        if !ok {
            return nil
        }
        return cli.RunAgentNew(c.Engine, agent, c.Mode)
    },
},
```

This requires a small refactor per command to expose each CLI runner
as a plain function callable from outside cobra. Cheap, and improves
the CLI's testability too.

### RunContext

```go
type RunContext struct {
    Engine  *engine.Engine
    Mode    Mode
    Paths   config.Paths
    in      *os.File
    out     *os.File
    Recents *Recents
}

func (c *RunContext) PickAgent() (string, bool)
func (c *RunContext) Prompt(label, prefill string) (string, bool)
```

### CLI subcommand

```
internal/cli/palette.go
```

Adds a hidden `bay palette [--split window|pane]` command. Loads the
engine, builds a `RunContext`, calls `palette.Run`. Prints nothing and
exits 0 if the current tmux context isn't a bay session.

### Keybindings

Added to `bayKeybindings` in `internal/cli/setup.go`:

```go
{"M-p", "bay palette --split pane", "Option+p: command palette", "display-popup -w 80% -h 80% -E"},
```

The existing `canonicalLine` format produces:

```
bind-key -n M-p display-popup -w 80% -h 80% -E 'bay palette --split pane || true'
```

### Build order

1. `palette.Recents` — pure logic, easy to TDD.
2. `picker.Prompt` — extend picker for text entry.
3. `palette.Hotkeys` — parse `tmux list-keys`.
4. `palette.Run` + 2 commands end-to-end (Go to workspace, New
   agent...) to validate shape.
5. Fill out the remaining 22 commands.
6. CLI subcommand + keybinding wiring.
7. Docs: `docs/human-guide.md` (keybindings + palette section),
   `internal/cli/agent-guide.md` (mention palette under navigation),
   `README.md` (feature summary).

### Recents file format

```json
{
  "version": 1,
  "recent": [
    {"id": "new-agent-pick", "param": "codex",  "ts": 1713628800},
    {"id": "go-workspace",                      "ts": 1713628700},
    {"id": "new-agent-pick", "param": "claude", "ts": 1713627600}
  ],
  "agent_types": [
    {"value": "codex",  "ts": 1713628800},
    {"value": "claude", "ts": 1713627600}
  ]
}
```

- `recent` — top-level entries. `param` is present only for
  sub-picker parametrics (fully bound) and omitted otherwise. Cap 20
  stored, 5 shown. Frequency counts are keyed on `(id, param)`.
  Ordering logic (MRU-latest + frequency) happens at read time, not
  in the file.
- `agent_types` — MRU for the agent-type sub-picker, shown inside
  the sub-picker itself. Cap 5 stored and shown.
- `version` — schema bump lets future changes skip-and-rewrite old
  files rather than error.

Staleness: recent IDs that no longer exist in the palette (removed in
a future version) are skipped silently on read. Recent `param` values
that no longer resolve (e.g., an agent removed from config) are also
skipped. No warning, no crash.

Concurrency: multiple bay sessions racing to write is fine under
atomic rename — last writer wins. Best-effort, not canonical state.

---

## Deferred / open details

Decide before or during implementation:

- **Popup sizing.** `60x20` is a guess. Might want `-w 80% -h 80%`
  for adaptive. Defer until rendered.
- **Cursor controls in text input.** Left/Right arrow movement is
  nice but extra code. Ctrl-U alone is enough for v1. Add arrows if
  rename UX feels clumsy.
- **Custom-argument hotkey matching.** A user binding like
  `bay agent codex` could theoretically annotate a parametric entry
  ("New agent... → codex"). Not in v1; revisit if anyone does this.
- **Clear-recents UI.** No in-palette entry. `rm <PaletteRecents>`
  works; add `bay palette clear-recents` if it becomes a real need.
- **Two-field cmd entry.** v1 is single-field (command only,
  auto-named surface). Add a name field later if missed.

Explicitly **not** open — settled above:

- 24 entries in v1 (the excluded-from-v1 list stays excluded unless
  real usage argues otherwise).
- Hidden-when-inapplicable, not grey-out.
- Esc always closes the palette (no multi-level back).
- Recents = MRU top + frequency body, 5 slots.
- Parametric recents: sub-picker entries fully bound; text-input
  entries unbound and re-prompt.
- Hotkey detection is live via `tmux list-keys`, not read from our
  canonical table.
