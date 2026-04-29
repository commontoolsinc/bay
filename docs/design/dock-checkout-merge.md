# Repo/dock merge - design plan

Captured 2026-04-29. No code has been written. This doc proposes
collapsing the `Repo` and `Dock` concepts into a single `Dock` entity
that owns its checkout. It is meant as a review artifact before
deciding whether to land any of these changes.

## Problem

Bay has two top-level entities that confuse users:

- A **repo** is a record of a local git checkout: `{name, path,
  worktree_dir}`.
- A **dock** is a tmux session containing workspaces, with an optional
  default repo reference.

The name "repo" implies a remote pointer, but the data is just a local
checkout. The split allows two patterns:

1. A single repo backing multiple docks.
2. A single dock hosting workspaces from multiple repos via
   `bay ws new --repo NAME`.

Neither is used in practice. Both add cost:

- The data model is N:M when actual usage is 1:1.
- Multiple docks pointing at the same repo collide on worktree
  directory naming (`w1`, `w2`, ...). Today a disk-presence scan
  keeps collisions from crashing, but the per-dock `claimed` map
  doesn't see sibling docks' workspaces.
- Several code paths conditional on `dock.Repo != ""`, with edge
  cases for "scratch" docks (a dock with no repo).
- Two cardinality stories to learn before bay's mental model clicks.

The proposal is to collapse the entities into a single `Dock` that
owns its checkout.

## Proposal

A dock becomes the single top-level entity:

- A dock has a `path` — the git checkout it manages.
- A dock has a `worktree_dir` — defaults to `{path}-worktrees`. This
  default is kept deliberately: a hidden sibling (`.{name}-worktrees`)
  hides real working code behind dotfile semantics and breaks tools
  that skip hidden dirs; a bay-owned location
  (`~/.local/share/bay/worktrees/<dock>/`) breaks the "worktrees are
  ordinary git checkouts you can always poke at directly" property
  bay leans on. Users with strong filesystem opinions can override
  per-dock with `--worktree-dir`, and a global default pattern in
  user config is a possible additive future change.
- A dock has a tmux session, workspaces, surfaces, agent defaults,
  closed-entries queue (unchanged).
- **Every dock has a checkout.** No scratch docks.
- **A given checkout is owned by at most one dock.** No
  multi-dock-per-checkout.
- **A dock's path must be the main checkout, not a worktree.**
  Stat-check rejects `.git` files (worktree marker) and accepts only
  `.git` directories.
- `Repo` is removed from the manifest entirely.

## What goes away

| Today | Post-merge |
|---|---|
| `Repo` struct in manifest | removed |
| `bay repo add` | removed; `bay dock new` covers the case |
| `bay repo ls` | removed; `bay dock ls` covers it |
| `bay repo show` | removed; `bay dock show` covers it |
| `bay repo remove` | removed; `bay dock close` covers it |
| `bay repo tree` | removed; was 1-deep in the proposed model |
| `bay repo init` | folded into `bay dock new` |
| `bay repo sync` | renamed to `bay dock sync` |
| `bay ws new --repo NAME` | removed; `--dock DOCK` (already exists) selects a different checkout, since dock now implies checkout. `--dir PATH` still covers external workspaces. |
| `Workspace.WorktreeAttrs.Repo` field | removed; parent dock implies it |
| `Dock.Repo` field | replaced by `Dock.Path` and `Dock.WorktreeDir` |
| Scratch dock support | removed; every dock has a checkout |

## What changes

### `bay dock new`

```
bay dock new [name] [--path DIR] [--worktree-dir DIR] [--agent TYPE]
```

- `path` defaults to CWD when CWD is a git checkout (with a `.git`
  *directory*, not a file).
- If CWD is not a checkout and `--path` is omitted, error with
  guidance: "use `--path DIR` or run from a git checkout".
- `name` defaults to the path's basename.
- The path must pass validation (below).

### Path validation

A dock's `path`:

| Check | Reason |
|---|---|
| exists on disk | basic |
| contains a `.git` *directory* (not a `.git` file) | rejects worktrees as docks |
| not already owned by another dock | enforces 1:1 |
| not the worktree path of any registered workspace | prevents shadowing |

The worktree check protects against a user pointing a new dock at one
of bay's own worktrees. That worktree could be torn down by a
`bay ws close`, leaving the new dock's checkout orphaned. Refusing it
at create time is much cheaper than detecting and recovering from the
orphan later.

### `bay dock close`

No semantic change. Closes all child workspaces (today's safety
checks apply), removes the dock from the manifest, kills the tmux
session. The checkout directory at `dock.path` is never deleted by
bay; the user owns it the same as any git repo.

A future "soft close" (kill session, keep manifest entry) is out of
scope. Revisit if the model grows toward dock-as-persistent-identity.

### `bay tree`

Hierarchy collapses from `repo → dock → workspace` to
`dock → workspace`. The `bay repo tree` command goes away (was 1-deep
in the proposed model). JSON shape simplifies accordingly.

### Auto-bootstrap

`bay ws new` from outside any dock today auto-creates both a repo and
a dock from CWD. Post-merge it just creates a dock. Fewer branches in
the bootstrap path.

### External workspaces

Unchanged. External workspaces (created with `bay ws new --dir PATH`)
live in a dock but are off the dock's checkout path. They remain a
coherent way to host arbitrary directories inside a dock for
organizational reasons (e.g., docs, notes, sibling tools).

## Migration

Bay's manifest format is versioned (`CurrentVersion = 4` today, bumps
to 5). Migrations are sequential `if m.Version < N` blocks in
`manifest.Parse()`. This one is bigger than past migrations
(structural fold-in, conflict detection, dropped top-level field), so
it should be extracted to a named function rather than added inline:

```go
if m.Version < 5 {
    if err := migrateV4ToV5(m); err != nil {
        return nil, err
    }
    m.Version = CurrentVersion
}
```

This doesn't introduce a migration framework — the existing inline
pattern still fits for the smaller ones. When migrations pile up to
where `Parse()` is hard to read (somewhere around 6+ versions), the
natural refactor is a slice of registered migrators iterated in
order. Cheap to add later.

Strategy:

1. For each `Repo` in the existing manifest, find the docks that
   reference it.
2. **One dock per repo (common case):** fold the repo's `path` and
   `worktree_dir` into the dock; drop `Repo`.
3. **Multiple docks per repo (rare):** pick the first dock as the
   primary owner. Conflicting docks are listed and the user is asked
   to either rename them away from the path (move workspaces out) or
   accept that they become primary-less. Bay does not silently
   reassign or delete them.
4. **Repo with no docks (orphan):** drop with a warning.
5. For each `Workspace` with `WorktreeAttrs.Repo` set, validate the
   workspace path lives under the parent dock's worktree dir; warn
   on mismatch.
6. Remove `Repo` field from workspace attrs.

A `--dry-run` mode on the migration shows what bay would do before
mutating the manifest. Past migrations were unconditional and didn't
need this; this one can find ambiguous state (multi-dock-per-repo),
so the user should be able to inspect bay's plan before committing.
Likely surface: `bay doctor migrate --dry-run` (or similar — the
exact command is a small UX choice deferred to implementation).

## Lifecycle

| Phase | Behavior |
|---|---|
| Dock create | validate path; record manifest entry; create tmux session; lazy-create worktree dir on first worktree workspace |
| Dock close | unchanged from today (workspaces close, manifest entry removed, tmux killed); checkout dir untouched |
| Dock recovery | unchanged; `path` and `worktree_dir` are persisted, so recovery has everything it needs |

Future dock-level setup/teardown hooks (the proposal in
`repo-hooks.md`) move from repo to dock. Cadence stays "once per
checkout lifetime," which now collapses cleanly to "once per dock
lifetime."

## Resolved

- **Multi-dock-per-repo migration:** the conflict is exceedingly rare
  (effectively zero usage today). Migrate with warnings and let users
  clean up afterwards rather than blocking. The conflict-handling
  code mostly serves as the convention going forward.
- **Cross-checkout investigation users:** none. `--repo NAME` is
  removed without a successor; users who want a workspace from a
  different checkout open a different dock.
- **Naming:** keep "dock". Other names ("project", "workbench")
  carry baggage and the dock metaphor (bays attach to docks) is
  consistent with the rest of bay's vocabulary.

## Relationship to other designs

- `repo-hooks.md`: the proposed `on_add`/`on_remove` hooks move from
  repo to dock. Cadence simplifies (one event per checkout lifetime).
  That doc should be updated to reflect this when this proposal lands.
- `workspace-lifecycle-config.md`: workspace prepare commands are
  already dock-level, so this change is neutral for that proposal.
- A separate **home-workspace** design (the canonical-checkout
  workspace concept) was the trigger for this discussion. The
  home-workspace design is decoupled and works against either model;
  it gets simpler post-merge because every dock has exactly one
  canonical checkout.

## Incremental path

### Step 1: Manifest schema migration [MEDIUM]

Bump schema version. Implement `Repo`-to-`Dock` fold-in with conflict
detection. Provide `--dry-run`. Migration runs on next bay startup.

### Step 2: CLI consolidation [MEDIUM]

Remove `bay repo` commands. Update `bay dock new` to require or
default the path. Remove `--repo` flag from `bay ws new`. Rename
`bay repo sync` → `bay dock sync`. Update `bay tree` and JSON
formatters.

### Step 3: Field cleanup [SMALL]

Drop `WorktreeAttrs.Repo`. Audit and update internal code that reads
it. Most reads collapse to "look at the parent dock."

### Step 4: Validation [SMALL]

Add `bay dock new` path checks: exists, `.git` is a directory, not
already owned, not a registered worktree path.

### Step 5: Documentation [MEDIUM]

README, tutorial, human-guide, agent-guide, examples. The merged
model makes the docs shorter — one fewer concept to introduce.
