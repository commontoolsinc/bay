# Workspace → bay rename - design plan

Captured 2026-04-29. No code has been written. This doc proposes
renaming the "workspace" concept to "bay" throughout bay's
user-facing surface — CLI, JSON, docs, and Go types. It is meant as
a review artifact before deciding whether to land any of these
changes.

The rename is independent of the dock/repo merge (`docs/design/dock-checkout-merge.md`)
and can land before, after, or in parallel.

## Problem

The tool is called `bay`. The central concept users manipulate is a
`workspace`. The two names don't reinforce each other, and "workspace"
is generic enough that users carry baggage into it from VS Code
workspaces, GitHub workspaces, generic editor terminology, etc.

A *bay* (the metaphor) — a workshop bay you pull a project into to
work on — is a concrete fit:

- A *dock* is where bays attach (docking-bay metaphor).
- A *bay* is the discrete work area for one task or feature.
- A *surface* is a workstation/view inside the bay.

The implementation already maps cleanly:

- Dock → 1 tmux session
- Workspace → 1 tmux window
- Surface → 1 tmux pane

Renaming workspace to bay aligns vocabulary with both metaphor and
implementation, and makes the tool's name and central noun reinforce
each other. "I have five bays open" reads better than "I have five
workspaces open" and is more memorable for new users.

## Proposal

Rename "workspace" → "bay" wherever it appears user-facing:

- CLI nouns and command names
- JSON output keys
- Help text, error messages, picker labels, status flashes
- Documentation (README, tutorial, human-guide, agent-guide)
- Go types (Workspace → Bay) for internal consistency

Subordinate concepts (dock, surface) keep their names. Only the
central concept changes.

## CLI grammar shift

Today's CLI grammar uses explicit nouns at the second position
(`bay dock new`, `bay ws new`, `bay surface new`) plus a small set of
top-level shortcuts (`bay shell`, `bay edit`, `bay ls`, `bay go`,
`bay new <kind>` for surfaces).

Post-rename, the central concept moves to top-level grammar. The
asymmetry — bare verb for the central concept, explicit noun for
subordinate ones — reflects reality and matches established CLI
patterns (`git commit` vs. `git remote add`).

### Top-level commands (operate on bays)

| Today | Post-rename | Notes |
|---|---|---|
| `bay ws new [name]` | `bay new [name]` | Creates a bay |
| `bay ws close [name]` | `bay close [name]` | |
| `bay ws show [name]` | `bay show [name]` | |
| `bay ws rename [name] <new>` | `bay rename [name] <new>` | |
| `bay ws describe [...]` | `bay describe [...]` | Already an alias today |
| `bay ws ls` | `bay ls` | Replaces today's *global* ls |
| `bay ws go [query]` | `bay go [query]` | Replaces today's *surface* go |
| `bay ws next` / `bay ws prev` | `bay next` / `bay prev` | |

### Names that need to be renegotiated

Three top-level names are taken today by other concepts. They move:

| Today's `bay X` | What X means today | Post-rename |
|---|---|---|
| `bay ls` | global hierarchy (everything) | becomes `bay tree` (or `bay ls --all`) — new name for global view |
| `bay go` | surface picker (intra-bay) | becomes `bay surface go`; top-level shortcut retired |
| `bay new <kind>` | surface creation | becomes `bay surface new <kind>`; top-level shortcut retired |

The convenience surface-creation shortcuts stay because they're
high-frequency and read naturally:

- `bay shell` — unchanged
- `bay agent` — unchanged
- `bay edit` — unchanged

These are exceptions to "subordinate ops use explicit grammar"
because they're convenience commands, not generic surface CRUD.

### Subordinate ops (unchanged grammar)

- `bay dock new|close|ls|show|tree|sync` — unchanged
- `bay surface new|close|ls|show|rename|next|prev|restore` — unchanged
- `bay surface go` — replaces today's top-level `bay go`
- `bay surface new <kind>` — replaces today's top-level `bay new <kind>`

### Globals (unchanged)

- `bay pwd`, `bay recover`, `bay doctor`, `bay monitor`, `bay config`, `bay restore`, `bay tree`

`bay tree` is now the global hierarchy command (today's `bay ls`
semantics). `bay restore` continues to mean "undo most recent close"
— if bay-level closes also enter the undo queue (open question), it
would handle both surface and bay restores transparently.

## Vocabulary changes

| Old | New |
|---|---|
| workspace | bay |
| workspace name | bay name |
| workspace path | bay path |
| workspace ID | bay ID |
| `Workspace` (Go type) | `Bay` |
| `WorkspaceType` | `BayType` |
| `WorktreeAttrs` (on workspace) | `WorktreeAttrs` (on bay) — name unchanged; refers to the git worktree, not the bay |
| `bay ws ...` | `bay ...` (no `ws` prefix) |
| `--ws WS` flag | `--bay BAY` (or just `--bay`) |
| "workspace picker" | "bay picker" |

`WorktreeAttrs` keeps its name because "worktree" is the git concept;
the bay is the bay-level concept. A bay of type `worktree` has
`WorktreeAttrs`. A bay of type `external` does not.

## JSON output

No external consumers exist yet, so JSON keys are renamed without a
compat shim.

`bay pwd --json`:

```json
{
  "dock": "labs",
  "bay": "auth-fix",
  "surface": "agent",
  "surface_id": 1,
  "path": "/Users/dev/projects/labs-worktrees/w1"
}
```

`bay ls --json` (the bay-scoped list — replaces today's
`bay ws ls --json`):

```json
[
  {
    "name": "auth-fix",
    "branch": "feature/auth-fix",
    "dirty": false,
    "merged": false,
    "surfaces": [...]
  }
]
```

`bay tree --json` (the new global view — today's `bay ls --json`
semantics) keeps its hierarchical shape with `bays` replacing
`workspaces`:

```json
{
  "docks": [
    {
      "name": "labs",
      "bays": [
        {
          "name": "auth-fix",
          ...
        }
      ]
    }
  ]
}
```

`bay show --json` returns the same fields as today's
`bay ws show --json`, with `description`, `path`, `branch`, etc.

## Manifest schema

The on-disk manifest stores Go-tagged JSON. Renaming the Go type
`Workspace` → `Bay` propagates to manifest keys (`docks[].workspaces`
→ `docks[].bays`). This is a schema bump.

Two options:

1. **Bundle with the dock-checkout-merge schema bump.** That doc
   already proposes a v4 → v5 migration for `Repo` removal. Adding the
   `Workspace` → `Bay` rename to the same migration is cheap.
2. **Separate bump.** Cleaner if the renames land at different times.

Recommendation: bundle if both ship in the same release; otherwise
separate. The order doesn't matter to the design.

## Documentation

Substantial. All four user-facing docs reference "workspace" as the
core concept:

- `README.md` — feature summary, examples
- `docs/tutorial.md` — full walkthrough
- `docs/human-guide.md` — command reference, configuration, concepts
- `internal/cli/agent-guide.md` — agent reference, JSON schemas

Plus design docs (`docs/design/*.md`) reference "workspace" as the
concept. Update opportunistically — design docs capture intent at a
point in time and don't have to be perfectly current. New design docs
use "bay".

CLAUDE.md and CLAUDE.local.md mention bay's vocabulary and may need
small updates.

## Migration

Bay has no external script consumers and no published JSON API yet.
The rename is a CLI breaking change but the blast radius is limited
to the user's muscle memory.

Strategy:

1. **No CLI alias period.** `bay ws *` is removed in the same
   release. Replacing them with hidden aliases that print a
   deprecation warning is cheap if it turns out to help, but the
   default plan is a clean cut.
2. **JSON keys rename freely.** No consumers.
3. **Manifest migration** runs at startup — schema bump rewrites
   `workspaces` → `bays` in any manifest at the previous version.
4. **CHANGELOG entry** documents the rename and the new top-level
   command names.

## Open questions

1. **`bay restore` scope.** Today it's surface-restore. Post-rename,
   should it also restore closed bays (if bay-close enters the undo
   queue), or stay surface-only? The cleaner UX is "restore most
   recent close, whatever it was." Today's implementation already
   has a per-dock undo queue; the question is whether bay-close
   pushes to the same queue.

2. **Bare `bay <name>` for navigation.** Today, `bay go <query>`
   navigates to a surface. Post-rename, `bay go <query>` navigates
   between bays. Worth considering whether `bay <name>` (without
   `go`) is shorthand for `bay go <name>` — would make
   `bay auth-fix` jump to that bay. Risk: name clashes with future
   verbs. Defer decision; not critical for v1.

3. **`bay tree` vs `bay ls --all`.** The global hierarchy needs a
   name. `bay tree` already exists for showing dock+workspace tree;
   it's the natural fit. `bay ls --all` would be a flag on the
   bay-scoped list, which conflates two operations. Lean `bay tree`.

4. **Should "surface" also rename?** Surface is a fine name — it's
   the surface where you interact with bay (tmux pane). The metaphor
   is independent of "bay" and doesn't need updating. Out of scope.

## Incremental path

The rename is one big mechanical change. It's easier to land in one
sweep than spread across releases.

### Step 1: Go type rename [SMALL]

`Workspace` → `Bay` throughout `internal/manifest`, `internal/engine`,
`internal/cli`. Field renames (`Workspaces` → `Bays`). Most of this
is search-and-replace plus test updates.

### Step 2: CLI command rename [MEDIUM]

Add new top-level commands (`bay new`, `bay close`, `bay show`,
`bay rename`, `bay ls`, `bay go`, `bay next`, `bay prev`). Move
existing top-level meanings out of the way (`bay ls` → `bay tree`;
`bay go` → `bay surface go`; `bay new <kind>` → `bay surface new
<kind>`). Remove `bay ws *` commands.

### Step 3: JSON key rename [SMALL]

Update JSON output keys and tests.

### Step 4: Manifest schema bump [SMALL]

Either bundle with dock-checkout-merge (if shipping together) or
add a standalone bump.

### Step 5: Documentation [MEDIUM]

Update README, tutorial, human-guide, agent-guide. Search for
"workspace" across `docs/` and `internal/cli/agent-guide.md`. Small
updates to CLAUDE.md / CLAUDE.local.md.
