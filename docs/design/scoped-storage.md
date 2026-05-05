# Scoped storage - design plan

Captured 2026-04-26. **Deferred.** Not slated for implementation
until a repo's prepare or close-check scripts ask for a stable bay-
provided storage root. This doc preserves the design so the work has
a starting point if/when that day comes.

Sibling of `prepare.md`. Scoped storage is supporting infrastructure
that exposes per-dock and per-bay directories to repo-defined
commands via environment variables.

## Why "storage" not "cache"

The term "cache" is too narrow as the main abstraction. Some data is
safe to delete and regenerate; other data may be durable state owned
by a repo or dock integration. The umbrella concept is **scoped
storage**, with `cache/` and `state/` as conventional subdirectories
inside each scope.

## Two scopes

After dock-checkout-merge (#281) every dock owns its checkout 1:1, so
there is no separate "repo" tier above dock. Two scopes survive:

```sh
BAY_DOCK_STORE=...
BAY_STORE=...
```

| Scope | Lifetime |
|---|---|
| Dock store | Shared by all bays in a dock; cleaned when the dock is closed. Same semantics earlier drafts gave to a "repo store" — the merge made them the same scope. |
| Bay store | Tied to one bay; cleaned on bay close. |
| `cache/` subdir | Safe to delete; deletion may make future prepare slower. |
| `state/` subdir | Durable for the scope; not deleted except by lifecycle operation. |

For Loom, a future cache-aware `fetch-vendor.ts` could use:

```text
$BAY_DOCK_STORE/cache/vendors/labs.git
```

as a shared bare git mirror. On a cache miss, Loom's script would
fetch the missing tag/ref into the mirror, then materialize the
current worktree's `vendor/labs` at the required sha.

Bay should not fetch or update that mirror itself. It only provides
a stable storage root if repo scripts need one.

## Stable IDs

Today's manifest uses names:

- Dock names are globally unique.
- Bay names are unique within a dock.
- A bay is effectively identified by `dock:bay`.

Scoped storage should not use renameable display names as durable
directory names. If scoped storage ships, bay should first add
opaque manifest IDs:

```json
{
  "docks": [{ "id": "d_...", "name": "loom" }],
  "bays": [{ "id": "w_...", "name": "vendor-fix" }]
}
```

Storage paths would use IDs, while metadata files inside the store
can record current human names for inspection.

## Why deferred

No concrete repo has asked for this yet. Loom's `fetch-vendor.ts`
manages its own location under the worktree (`vendor/<name>/`),
which works for v1. The case for bay-provided storage emerges only
when:

- Multiple bays in the same dock would benefit from a shared cache
  (e.g., a bare git mirror reused across worktrees), and
- The repo script can't pick a sensible default location itself
  (e.g., outside the worktree, scoped to user or dock).

Until at least one repo script needs both, the right call is to
keep scopes out of bay's contract.

## CLI surface (sketched)

When this ships, a `bay store path [--dock|--bay] [--cache|--state]`
helper would resolve the right directory for scripts and humans.
Exact shape can be settled at implementation time.
