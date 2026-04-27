# Workspace identity vs. display

Captured 2026-04-26.

## The problem

Today a workspace's `Name` field plays three jobs at once: it's the
CLI key (`bay ws close auth-fix`), the tmux tab label, and the
display string in `bay ls`/`bay tree`. The job conflict shows up in
two ways:

1. **Auto-rename surprise.** Sync detects a branch change in the
   worktree and rewrites `Name` to the new abbreviated branch
   (`sync.go:288-294`). That's good for keeping the tab label
   semantic, and bad for keeping the CLI key stable — a script that
   ran `bay ws close auth-fix` last week silently breaks if the
   workspace is now `auth-header-fix`.

2. **Generic-name reality.** When the user creates with
   `Option+c`/`Option+C` (no `--branch`), `Name` defaults to the
   path basename (`w1`, `w2`, ...). The auto-rename later upgrades
   it once a branch is set, but until then the dashboard is full of
   undifferentiated `w1`/`w2`/`w3` rows. Today's options to fix
   that are "create with `--branch`" or "wait for sync to detect a
   branch" — neither obvious to the user.

The two pressures pull opposite directions: the CLI key wants
stability, the tab label wants semantic correctness as work
evolves. One field can't honor both.

## The model

Split the field into three distinct concepts with non-overlapping
jobs:

| Concept | Job | Mutability | Surfaces |
|---------|-----|-----------|----------|
| **ID** | CLI key, manifest key | Set at creation, never changes | `bay ls` ID column, `--json`, error messages |
| **Name** | Tab label, display string | Mutable; sticky once set | tmux tab, `bay ls` name column, picker |
| **Description** | Longer-form context | Mutable | `M-?` popup, `--json` |

ID is the workspace's durable handle. Tab equals Name — they're the
same thing, no independence to be confused about. Description is
unchanged from today (the existing field, just no longer competing
with Name for the tab).

### Reserved ID pattern

IDs match `^w[1-9]\d*$` — lowercase `w` followed by a positive
integer with no leading zeros. (`w0` and `w01` are rejected so the
canonical form is unambiguous.) Names are forbidden from matching
that pattern (rejected by `ValidateName`). With disjoint namespaces,
no token a user types is ever ambiguous between the two.

### Resolution: strict ID-only

The CLI accepts only IDs. `bay ws close auth-fix` errors with
"unknown workspace 'auth-fix'; did you mean `bay ws close w1`?"
Tab completion completes Name → ID before submission, so users who
look at the tab and start typing the label still get a working
command after Tab.

This forecloses three otherwise-painful classes of edge case:

- ID/Name shadowing (resolved structurally by the reserved pattern,
  but strict mode means it can't matter at all)
- Same-Name collisions across workspaces — Names don't have to be
  unique, since they're not keys
- Silent target drift if a Name changes between when a script was
  written and when it runs

### Auto-fill: sticky once set

Sync sets `Name` from the abbreviated branch only when `Name` is
currently empty. Once anyone — user, agent, or this first-fill —
has set a Name, sync never rewrites it. Subsequent branch
checkouts don't relabel the tab; the user can `bay rename` if they
want a different label. Live branch info stays available via
`bay status-line full` in the right-status.

This gives:

- Stable tabs: at most one label transition per workspace, and
  that transition only fires when a Name was unset and a branch
  appeared.
- Semantic by default for the common path: `bay ws new --branch
  fix/auth-header` sets Name immediately; `Option+c` followed by a
  branch checkout fills Name on first sync; either way the tab
  becomes meaningful without a manual rename.

## Cascade

### Manifest

Add `ID string` to the workspace struct. Migration on first load:
fill `ID` from the path basename for any workspace where it's
empty. Path basenames are already `w1`/`w2`-shaped
(`workspace.go:103`), so this is a no-op rewrite of the manifest,
not a behavioral change.

`Name` becomes optional. Existing manifests have Name populated
already (often from auto-rename); those values stay.

### Engine

- `nextWorkspaceName` and `nextWorkspaceDir` collapse into a single
  `nextWorkspaceID` since they were always producing the same
  sequence. The path basename and the ID are seeded from the same
  source; they may diverge later if we ever decide to move
  worktrees.
- `uniqueWorkspaceName` goes away. Names can collide.
- `sync.go:288-294` becomes guarded: only fill when
  `ws.Name == ""`.
- `updateWindowNames` reads `ws.Name`, falling back to `ws.ID`
  when Name is empty. This is the *only* place Name gates display
  fallback.

### CLI

- `resolveWorkspaceArg` (and friends) becomes ID-only. Lookup by
  Name is removed. Error messages suggest the right ID.
- `bay rename` validates the new Name against the reserved
  `^w[1-9]\d*$` pattern.
- `bay ls`/`bay tree` formats: ID column, Name column, branch,
  PR, status. When Name is empty, the Name column shows the ID
  dimmed (matching what the tab shows).
- `bay status-line` gains an `id` field; `name` keeps returning
  Name. Both are useful in user tmux configs.
- `bay setup` installs `set -g status-right '#(bay status-line full
  --window #{window_id})'` by default, so live branch/PR info is
  always visible regardless of tab label.
- Tab completion for workspace args resolves Name → ID:
  - **List candidates as IDs with Name annotations.** `bay ws close
    <TAB>` shows `w1   auth-fix   fix/auth-header`, with the ID as
    the inserted candidate and the Name + branch shown as the
    description. The user picks visually.
  - **Treat Name prefixes as valid input.** If the user types
    `bay ws close auth<TAB>`, the completion function matches `auth`
    against Names, finds `auth-fix` on `w1`, and offers `w1` as the
    candidate (replacing the user's partial token). Both match
    paths feed the same candidate set in one completion function;
    no separate code path.
  - Existing branch completion for `--branch` unaffected.

### Tests

Most assertions on `bay ws close <name>` setups switch to IDs.
`bay ls` snapshot tests update for the new column. Estimated 30-50
test changes, mechanical.

### Docs

- `internal/cli/agent-guide.md` — rewrite the "key concepts /
  workspace" section to describe ID, Name, Description as three
  distinct fields.
- `docs/human-guide.md` — same.
- `docs/tutorial.md` — first lifecycle walkthrough mentions the ID
  briefly so new users see it.
- `docs/demo-script.md` — prep instructions reflect that
  semantic-name dashboards require either pre-creation with
  `--branch` or post-creation rename.
- `README.md` — examples switch to ID-keyed commands where
  workspace IDs appear.

## Migration

- Existing manifests get IDs filled from path basenames on first
  load. Silent.
- Existing scripts using Name as the CLI key break under strict
  mode. Hard cut: the resolver errors with "unknown workspace
  '<arg>'; did you mean `bay ws close w1`?" when a Name was
  passed. Documented in the changelog; no transitional warning.
- No worktree or tmux state needs to move.

## Non-goals

- We're not changing where worktrees live on disk. The path
  basename is ID-shaped today; it stays ID-shaped, but the two are
  now formally independent so a future change can decouple them.
- We're not adding a "display label override" separate from Name.
  The user has Name (label) and Description (context); two knobs is
  enough.
- We're not enforcing Name uniqueness. Two `auth-fix` tabs are
  visually unfortunate but harmless; the user disambiguates by ID.
