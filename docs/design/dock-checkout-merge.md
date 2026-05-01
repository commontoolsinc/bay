# Repo/dock merge - design plan

Captured 2026-04-29. Updated 2026-04-30 after the
workspace-to-bay UX rename landed. No repo/dock merge code has been
written. This doc proposes collapsing the `Repo` and `Dock` concepts
into a single `Dock` entity that owns its checkout.

Terminology note: this doc uses **bay** for the user-facing concept.
The current Go implementation still has internal names like
`Workspace`, `WsNew`, and `WorktreeAttrs`. Renaming those internals is
cosmetic and does not block this design; this merge should only rename
internal types where doing so reduces confusion in touched code.

## Problem

Bay has two top-level entities that confuse users:

- A **repo** is a record of a local git checkout: `{name, path,
  worktree_dir}`.
- A **dock** is a tmux session containing bays, with an optional
  default repo reference.

The name "repo" implies a remote pointer, but the data is just a local
checkout. The split allows two patterns:

1. A single repo backing multiple docks.
2. A single dock hosting bays from multiple repos via
   `bay new --repo NAME`.

Neither is used in practice. Both add cost:

- The data model is N:M when actual usage is 1:1.
- Multiple docks pointing at the same repo collide on worktree
  directory naming (`w1`, `w2`, ...). Today a disk-presence scan
  keeps collisions from crashing, but the per-dock `claimed` map
  doesn't see sibling docks' bays.
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
- A dock has a tmux session, bays, surfaces, agent defaults,
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
| `bay new --repo NAME` | removed; `--dock DOCK` (already exists) selects a different checkout, since dock now implies checkout. `--dir PATH` still covers external bays. |
| `WorktreeAttrs.Repo` field | removed; parent dock implies it |
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
| not the worktree path of any registered bay | prevents shadowing |

The worktree check protects against a user pointing a new dock at one
of bay's own worktrees. That worktree could be torn down by a
`bay close`, leaving the new dock's checkout orphaned. Refusing it
at create time is much cheaper than detecting and recovering from the
orphan later.

### `bay dock close`

No semantic change. Closes all child bays (today's safety
checks apply), removes the dock from the manifest, kills the tmux
session. The checkout directory at `dock.path` is never deleted by
bay; the user owns it the same as any git repo.

A future "soft close" (kill session, keep manifest entry) is out of
scope. Revisit if the model grows toward dock-as-persistent-identity.

### `bay tree`

Hierarchy collapses from `repo → dock → bay` to `dock → bay`. The
`bay repo tree` command goes away (was 1-deep in the proposed model).
JSON shape simplifies accordingly.

### Auto-bootstrap

`bay new` from outside any dock today auto-creates both a repo and a
dock from CWD. Post-merge it just creates a dock. Fewer branches in
the bootstrap path.

### External bays

Unchanged. External bays (created with `bay new --dir PATH`) live in
a dock but are off the dock's checkout path. They remain a coherent
way to host arbitrary directories inside a dock for organizational
reasons (e.g., docs, notes, sibling tools).

## Migration

Bay's manifest format is versioned. `CurrentVersion = 5` today; v5 is
already used by the workspace-to-bay manifest key rename
(`docks[].workspaces` → `docks[].bays`). This proposal bumps the
schema to **v6**.

Before adding v6, tighten `manifest.Parse()` so migrations are truly
sequential. Today older manifests can jump straight to
`CurrentVersion`; that was tolerable for small additive migrations,
but this migration removes top-level data and needs an explicit
boundary. The v6 migration should be extracted to a named function:

```go
if m.Version < 6 {
    if err := migrateV5ToV6(m); err != nil {
        return nil, err
    }
    m.Version = 6
}
```

This still doesn't require a full migration framework, but the steps
must be ordered (`<3`, `<4`, `<5`, `<6`) so a failed v6 migration
leaves the v5 manifest unchanged.

Strategy:

1. For each `Repo` in the existing manifest, find the docks that
   reference it.
2. **One dock per repo (common case):** fold the repo's `path` and
   `worktree_dir` into the dock; drop `Repo`.
3. **Multiple docks per repo (rare):** block the migration with an
   actionable error. Do not create pathless or primary-less docks,
   because the target model requires every dock to own exactly one
   checkout. The error should list the conflicting docks and tell the
   user to close one or recreate it against a separate checkout before
   retrying.
4. **Repo with no docks (orphan):** drop with a warning.
5. For each worktree bay with `WorktreeAttrs.Repo` set, validate the
   bay path lives under the parent dock's worktree dir; warn on
   mismatch.
6. Remove `Repo` from `WorktreeAttrs`.

A `--dry-run` mode on the migration shows what bay would do before
mutating the manifest. Past migrations were unconditional and didn't
need this; this one can find ambiguous state (multi-dock-per-repo),
so the user should be able to inspect bay's plan before committing.
Likely surface: `bay doctor migrate --dry-run` (or similar — the
exact command is a small UX choice deferred to implementation). The
dry-run path must read the raw v5 manifest without forcing the normal
startup migration, so users can inspect and fix conflicts even when
normal command startup would reject the manifest.

## Lifecycle

| Phase | Behavior |
|---|---|
| Dock create | validate path; record manifest entry; create tmux session; lazy-create worktree dir on first worktree bay |
| Dock close | unchanged from today (bays close, manifest entry removed, tmux killed); checkout dir untouched |
| Dock recovery | unchanged; `path` and `worktree_dir` are persisted, so recovery has everything it needs |

Future dock-level setup/teardown hooks (the proposal in
`repo-hooks.md`) move from repo to dock. Cadence stays "once per
checkout lifetime," which now collapses cleanly to "once per dock
lifetime."

## Resolved

- **Multi-dock-per-repo migration:** the conflict is exceedingly rare
  (effectively zero usage today), but it should block rather than
  create invalid docks. The target invariant is simpler and stronger:
  every dock has a checkout, and each checkout has one dock.
- **Cross-checkout investigation users:** none. `--repo NAME` is
  removed without a successor; users who want a bay from a
  different checkout open a different dock.
- **Naming:** keep "dock". Other names ("project", "workbench")
  carry baggage and the dock metaphor (bays attach to docks) is
  consistent with the rest of bay's vocabulary.

## Relationship to other designs

- `repo-hooks.md`: the proposed `on_add`/`on_remove` hooks move from
  repo to dock. Cadence simplifies (one event per checkout lifetime).
  That doc should be updated to reflect this when this proposal lands.
- `workspace-lifecycle-config.md`: bay/workspace prepare commands are
  already dock-level, so this change is neutral for that proposal.
- A separate **home bay** design (the canonical-checkout bay concept)
  was the trigger for this discussion. The home-bay design is
  decoupled and works against either model;
  it gets simpler post-merge because every dock has exactly one
  canonical checkout.

## Incremental path

### Step 1: Manifest schema migration [MEDIUM]

Bump schema version to v6. First make migration steps sequential, then
implement `Repo`-to-`Dock` fold-in with conflict detection. Provide
`bay doctor migrate --dry-run` (or equivalent) against the raw
manifest. Normal startup migration should block on conflicts and
leave the v5 manifest unchanged.

### Step 2: CLI consolidation [MEDIUM]

Remove `bay repo` commands. Update `bay dock new` to require or
default the path. Remove `--repo` flag from `bay new`. Rename
`bay repo sync` → `bay dock sync`. Update `bay tree` and JSON
formatters.

### Step 3: Field cleanup [SMALL]

Drop `WorktreeAttrs.Repo` and `Dock.Repo`. Audit and update internal
code that reads them. Most reads collapse to "look at the parent
dock."

### Step 4: Validation [SMALL]

Add `bay dock new` path checks: exists, `.git` is a directory, not
already owned, not a registered worktree path.

### Step 5: Documentation [MEDIUM]

README, tutorial, human-guide, agent-guide, examples. The merged
model makes the docs shorter — one fewer concept to introduce.
