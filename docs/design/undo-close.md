# Undo close — design plan

Captured 2026-04-16. No code has been written. The design emerged
from a discussion about accidental Option+W presses: the user
frequently closes a surface via muscle memory that they didn't mean
to close. A Chrome-style "recently closed" queue, scoped per dock,
lets you recover without ceremony.

## The problem

Bay makes workspaces disposable on purpose — the thesis of the tool
is that create and teardown should be so cheap you stop planning
around them. But "cheap" only works if accidental teardown is
recoverable. Today:

- Option+W (bound to `bay sf close self`) removes the current pane
  immediately. Muscle-memory misfires happen regularly.
- If the closed surface was the last in its workspace, the close
  cascades: the workspace's worktree, branch (if clean), tmux
  window, and bay record all go away.
- `bay ws close` from the CLI does the same workspace teardown
  directly.
- There is no way to get any of this back except `bay recover`
  (which only reconstructs sessions lost to tmux/reboot, not user
  closes).

Chrome's Ctrl+Shift+T is the pattern we're copying: keep a small
LRU queue of closed things, let the user pop the most recent one
off with a single keystroke.

---

## Design

### Scope of "close" (v1)

Only bay-initiated closes are eligible for undo:

- `bay sf close` / Option+W on a surface (workspace survives) →
  surface entry
- `bay sf close` / Option+W on the last surface in a workspace →
  surface entry. Orphan-hygiene (PR #237) has since taken over
  the cascade path: last-surface close schedules
  `PendingCloseAt = now + 60s` instead of closing the workspace
  synchronously. Restoring the surface within the grace window
  re-populates the workspace and lets orphan-hygiene's existing
  cancel path clear `PendingCloseAt`. No workspace entry is
  queued at Option+W time — if the user lets the grace window
  elapse and the sync-finalize path closes the workspace, that
  pushes a workspace entry separately.
- `bay ws close` from the CLI → workspace entry
- Sync-finalize cascade (`WsClose` fires after the grace window,
  succeeds) → workspace entry

Out of scope for v1:

- tmux-native kills (`Ctrl-B x`, `kill-pane`, `kill-window`). Bay
  detects these via sync; surfaces get stripped and orphan-hygiene
  handles any resulting empty workspace. Undo-close doesn't cover
  them — orphan-hygiene is the mitigation for that class of
  accident.
- `bay dock close` / dock teardown — recording the full dock is
  out of proportion with the muscle-memory use case, and the
  queue is per-dock so the queue itself goes with the dock.

### The queue

- **Scope:** per dock. Closing in one dock never surfaces in
  another's undo.
- **Storage:** persisted inside the dock's state. Survives tmux
  kill, bay recover, reboot.
- **Retention:** last 10 entries per dock, with a 1-hour wall-clock
  expiry. Entries older than 1h are pruned on next access. When a
  dock is torn down, its queue goes with it. (The retention window
  was originally specced at 24h; shortened to 1h after testing
  revealed that stale entries from earlier sessions ambushed users
  expecting Option+Z to only revive recent closes. 1h matches the
  muscle-memory use case — users notice accidental closes within
  seconds to minutes, not overnight.)
- **Ordering:** LIFO. `bay sf restore` pops the most recent entry.

### Entry shape

A close entry records enough to recreate the closed thing without
perfect fidelity — we're restoring structure, not in-flight state
that wasn't persistent anyway.

**Surface entry** (single-surface close, workspace survives):

| Field | Purpose |
|---|---|
| `timestamp` | For 1h expiry |
| `kind` | `"surface"` |
| `dock` | Dock name (redundant with queue scope, but explicit) |
| `workspace` | Parent workspace name |
| `surface_type` | `agent` / `shell` / `cmd` / `editor` |
| `surface_config` | Type-specific: agent name + args, cmd string + cwd, editor name, etc. |

**Workspace entry** (direct close or cascade):

| Field | Purpose |
|---|---|
| `timestamp` | For 1h expiry |
| `kind` | `"workspace"` |
| `dock` | Dock name |
| `workspace` | Workspace name at close time |
| `repo` | Repo name (docks can hold multiple repos) |
| `branch` | Branch name |
| `branch_sha` | Commit SHA at close time — used to recreate branch if it was deleted |
| `worktree_path` | Path to the worktree |
| `worktree_preserved` | `true` if dirty-close left worktree in place; `false` if clean-close removed it |
| `surfaces` | Ordered list of surface entries (same shape as above, minus redundant fields) |

### Restore mechanics

**Surface restore:**

1. Verify the parent workspace still exists. If not, discard the
   entry and error — the surface-level undo is meaningless without
   its workspace. (Note: if the workspace was closed after the
   surface, that close has its own undo entry which restores the
   workspace *including* the surface.)
2. Recreate the surface with the recorded config. Position logic
   (layout-faithful, added after initial-testing feedback):
   - If any sibling survives in the recorded tmux layout group,
     split against it so the restored pane rejoins its original
     tmux window.
   - If the recorded surface was a split child (SplitDir=h/v),
     use that direction against the sibling.
   - If the recorded surface was a root pane (SplitDir=""), use
     `tmux split-window -fb` to insert at the root position of
     the layout group. Axis is inferred from a sibling's recorded
     SplitDir (or defaults to v).
   - If no sibling survives (the whole layout group is gone),
     create a new tmux window.
3. Focus the restored surface.

Because the queue is LIFO, multi-pane close-then-restore scenarios
compose: closing three panes then restoring three times puts all
three back in the original window, because each successive restore
finds the previous one still alive in the layout group.

**Stacked restores** compose naturally: if you close surface A,
then close the workspace containing A, the queue holds
`[surface A, workspace(with B,C)]`. First restore brings the
workspace back with B and C. Second restore adds A on top. No
special handling needed — each entry is independent.

**Workspace restore (dirty close — worktree preserved):**

1. Verify dock still exists. If not, error.
2. Re-register the workspace in bay's state using the recorded
   name and worktree path.
3. If the recorded name collides with a newer workspace, append a
   disambiguating suffix (`-restored` or `-N`). The bay workspace
   name is cosmetic; the branch and worktree are what matter.
4. Recreate the tmux window over the existing worktree.
5. Recreate surfaces in order from the recorded list.
6. Focus the first surface.

**Workspace restore (clean close — worktree was removed):**

1. Verify dock still exists. If not, error.
2. Branch collision check:
   - If no branch with the recorded name exists → `git branch <name> <sha>` to recreate from reflog, then proceed.
   - If a branch with the recorded name exists and points to the
     recorded SHA → use it as-is.
   - If a branch with the recorded name exists but points
     elsewhere → **abort** with a clear message. Don't try to
     auto-resolve.
3. Create a new worktree from the restored branch.
4. Name-collision check: if a newer workspace has the recorded
   name, rename the restored one on the fly.
5. Recreate tmux window, recreate surfaces, focus first surface.

### Error handling

- Restore is atomic from the user's perspective. If any step fails
  (branch collision, permission denied, repo moved), surface the
  error and leave the entry in the queue so the user can retry
  after fixing the underlying issue.
- Exception: if the parent workspace for a *surface* restore is
  gone, the entry is meaningless — discard it silently on the
  next access.

### Surface of the feature

**Keybinding:** Option+z (lowercase, mirrors Option+W). Single press
restores the most recent entry in the current dock.

**CLI:**

```
bay sf restore            # restore most recent entry from current dock
bay sf restore --list     # show the queue (timestamps, names, surface/workspace)
bay restore               # shorthand for bay sf restore
```

No `bay sf restore <index>` in v1. If the user wants something
that isn't the most recent, they pop the intervening entries
first. (Most-recent-only matches the muscle-memory use case; a
picker can land later if demand is real.)

**Empty queue behavior:**

- `bay sf restore` on an empty queue: exit 0 with an informational
  message ("Nothing to restore in dock `<name>`."). Not an error —
  this is a user-initiated check, and error exit complicates
  keybinding integration.
- `bay sf restore --list` on an empty queue: print header only, or
  a single-line "no recent closes."

**Close toast — routine cases:** deferred. Closes happen in
bursts and a toast every time would be noisy. For v1,
discoverability of Option+z lives in `bay sf --help` and the
human-guide.

**Dirty-leave-behind notification:** included in v1. When a
`bay ws close --force` on a dirty workspace leaves the worktree
in place, write a warning to stderr:

```
foo: worktree preserved (dirty). Option+Z to restore.
```

Rationale: the user asked for teardown and didn't get full
teardown. Silent leave-behind leads to stale worktrees piling up
in the dock directory and surprise name collisions on later
`bay ws new`. This is exceptional-state feedback, not
routine-close feedback — and the undo is what makes the message
actionable ("here's the leftover, here's how to grab it back").

Orphan-hygiene reroutes the Option+W cascade through the
sync-finalize path, so there's no `tmux run-shell` stderr
detachment issue at keystroke time — the keystroke-invoked tmux
toast branch described in earlier revisions of this doc doesn't
apply. The sync-finalize refuse case is covered separately below.

**Refuse-to-close notification:** included in v1. When the
orphan-hygiene sync-finalize path attempts `WsClose(force=false)`
on a workspace whose surfaces died and the close is refused
(dirty or unpushed), `PendingCloseAt` is cleared and the
workspace lingers as a permanent orphan. Pre-undo-close, this
fails silently: the user's Option+W looked like it worked, the
workspace quietly stayed around.

Emit a tmux toast via `tmux display-message -t <session>`
addressed to the dock's tmux session:

```
foo: workspace kept (uncommitted changes).
foo: workspace kept (unpushed commits).
```

The toast fires from the sync goroutine, which has no TTY, so
tmux is the only available channel. No Option+Z hint — the
workspace was never closed, so there's nothing to undo; reopening
a surface (`bay sf new --ws foo`) is the way back in, which the
user already knows.

**CLI-invoked close** (`bay ws close` without `--force`,
`bay sf close` on the last surface) that's refused on dirty state
already prints an error to stderr in the current code path — no
change needed.

### Lifecycle integration

| Event | Queue effect |
|---|---|
| `bay sf close` (workspace survives) | Push surface entry |
| `bay sf close` on last surface (Option+W) | Push surface entry. Orphan-hygiene schedules `PendingCloseAt`; restoring the surface within 60s saves the workspace via orphan-hygiene's cancel path. |
| Sync-finalize cascade (`WsClose` succeeds after grace) | Push workspace entry |
| `bay ws close` | Push workspace entry; suppress surface-entry pushes from the cascading surface closes |
| `bay sf restore` | Pop most recent, attempt restore |
| `bay dock close` | Drop queue with the dock |
| `bay recover` | Queue survives (lives in dock state) |
| Entry ages past 1h | Pruned on next access |
| Queue exceeds 10 | Oldest entry dropped |

---

## Incremental path

### Step 1: Surface-level undo [MEDIUM — ~200-250 lines]

Build for the stated use case. Record a surface close, restore it
to its parent workspace. No worktree/branch mechanics involved.

Includes the full queue infrastructure: per-dock scoping,
persistence, retention/expiry, `bay recover` integration, CLI,
keybinding. Covers the muscle-memory case directly — and, thanks
to orphan-hygiene, also covers the last-surface Option+W case:
surface restore re-populates the workspace before the
`PendingCloseAt` timer elapses, and orphan-hygiene's cancel path
takes care of the rest.

Scope notes:
- Push surface entries on `bay sf close` / Option+W, including
  when the surface is the last one in the workspace.
- Suppress surface-entry pushes when the close is part of a
  workspace-level teardown (the Step 2 `bay ws close` path and
  the sync-finalize cascade).
- Restore verifies the parent workspace still exists; if not
  (grace elapsed, workspace closed), discard the entry silently.
- The dirty-leave-behind and refuse-to-close notifications are
  independent of the queue and can ship before, alongside, or
  after Step 1.

### Step 2: Workspace-level undo [MEDIUM-LARGE — ~400-600 lines]

Record workspace closes (direct and cascaded). Handle the two
worktree paths (preserved vs removed). Implement branch SHA
recovery via reflog. Branch- and workspace-name collision checks.
Agent session continuity considerations (see corner case 4).

Depends on step 1's queue primitives.

Also pick up the **dock-surface close gap** deferred from Step 1:
`Engine.DockSurfaceClose` (used by `bay edit --dock` and any
dock-scoped surface) doesn't currently push a queue entry, so
Option+W on a dock-scoped surface can't be undone. Natural home
here since Step 2 is already widening the entry schema — either
add a `ClosedKindDockSurface` variant or allow `ClosedSurface`
with an empty `Workspace` and dispatch to the dock-add path on
restore. Small addition (~30-50 lines + a test).

### Step 3: Restore polish [SMALL — incremental]

Things to consider after real usage:

- `--list` output formatting and column choices
- Exact pane-layout restoration with pixel fidelity. Step 1 already
  rejoins the original tmux window via `split-window -fb`; Step 3
  would go further by capturing tmux layout strings at close time
  and replaying them on restore to preserve split geometry
  (percentages, orientation mix). Only pursue if layout drift
  becomes a complaint.
- A picker for non-most-recent restore, if demand appears
- Toasts, if discoverability turns out to be a problem

---

## Corner cases and known limitations

1. **Branch SHA GC.** Clean-close branch restore recreates a
   branch at a recorded SHA. When bay deletes the branch and
   removes the worktree, their reflogs go with them — so the
   survival of the commit is governed by `gc.pruneExpire`
   (default 2 weeks) as a dangling object, not by
   `gc.reflogExpireUnreachable`. Two weeks is well past our 1h
   undo window, but `git gc --prune=now`, a tight `pruneExpire`
   setting, or `git maintenance` (auto-enabled by GitHub Desktop
   and newer git defaults) can shorten that window.

   **Hardening option (Step 2):** park each queued SHA under
   `refs/bay/closed/<uuid>` at close time. The ref keeps the
   commit reachable — immune to `gc.pruneExpire` — until the undo
   entry is popped or expires, at which point bay deletes the
   ref. This adds one `git update-ref` per close and one per
   pop/expire, in exchange for durability against any GC
   configuration. Decide at Step 2 implementation time.

2. **Branch-name collision after clean close.** Addressed in the
   restore flow above — abort clearly rather than auto-resolve.
   Documented as expected behavior.

3. **Workspace-name collision.** Auto-disambiguate with a suffix.
   The bay name is cosmetic; the branch and worktree are what
   matter.

4. **Agent conversation state is keyed on working directory.**
   Claude Code (and similar tools) store session state under a
   hash of the CWD — e.g. `~/.claude/projects/<hash-of-cwd>/`.
   This has implications for restore:
   - **Dirty-close restore:** worktree path is preserved, so the
     CWD hash matches and `--continue` resumes the conversation.
     Safe.
   - **Clean-close restore:** if bay re-creates the worktree at
     the *same* path it originally occupied, `--continue` works.
     If a name collision forces a different path (see the
     workspace-name-collision rule above), the CWD hash changes
     and `--continue` silently starts a new conversation.
   - **User-cleaned session files** between close and restore:
     agent starts fresh regardless.

   Worth considering in step 2: when restoring a clean-close, try
   to pin the worktree to its original path first (only
   disambiguate if something else now occupies it). This keeps
   the common case on the safe path.

5. **cmd surfaces with side effects.** Restore re-runs the
   recorded command. If the command had side effects (e.g., `make
   migrate`), re-running is the user's problem — same as if they'd
   closed and recreated manually.

6. **Editor surfaces.** Restore relaunches the editor. For GUI
   editors (`gui = true`), a new window pops. Consistent with the
   "restore what was there" contract.

7. **Partial restore failure.** Atomic: either the whole workspace
   comes back or nothing does. Better than half-state that's
   confusing to reason about.

8. **Repo moved or deleted.** Restore errors with a clear message.
   Entry stays in queue — user can restore after fixing the repo
   path.

9. **Closing in one dock, switching to another.** The queue is
   per-dock, so Option+z in the wrong dock does nothing. The
   informational "nothing to restore" message (see empty queue
   behavior above) is what tells the user they're in the wrong
   dock — otherwise Option+z could feel broken.

10. **Closing the restored thing.** A restored workspace/surface
    is a normal close candidate again — if you close it, a new
    entry goes on the queue. No special handling.

11. **`bay ws close --force` discards uncommitted changes
    permanently.** `--force` on a dirty workspace runs
    `git worktree remove --force`, which drops working-tree edits
    before removing the worktree. The undo entry records
    `branch_sha` — the committed tip at close time — so restore
    recreates the branch and a fresh worktree at that commit. It
    does **not** bring back the uncommitted diff, because nothing
    persists it. Users choosing `--force` should understand the
    tradeoff: restore resurrects the branch, not the working
    tree.

---

## Open questions (to revisit when building)

1. **Storage location.** Inline in the dock manifest, or a
   separate `<dock>/.bay/undo-queue.json`? Inline is simpler;
   separate avoids polluting the manifest with transient state.
   Decide when implementing step 1.

2. **Retention limits as config.** 10 entries + 1h is a
   reasonable default. Worth exposing as dock config? Probably not
   in v1 — YAGNI until someone asks.

3. **Surface position fidelity.** How faithfully do we restore
   pane layout — exact split geometry, or just "append at the
   end"? Append is fine for v1. Exact geometry requires capturing
   tmux layout strings, which is doable but scope creep.

4. **Cross-repo workspace restore.** A dock can hold workspaces
   from multiple repos. The entry records `repo` so restore knows
   which repo to operate in. But if the dock's repo list has
   changed (repo removed from dock) between close and restore,
   what happens? Probably error. Decide when implementing step 2.

5. **Restore after `bay recover`.** The queue survives, but does
   the surviving queue reference workspaces that also survived
   recovery? Likely yes, but worth verifying when the two features
   meet.

6. **Interaction with `bay ws rename`.** If a workspace is renamed
   after a surface close was queued, the entry references the old
   name. Should it update? Probably update on rename — cheaper
   than invalidating. Small engine hook.

7. **Telemetry on undo frequency.** Not for v1, but worth knowing
   later: how often is Option+z used? Informs whether the muscle-
   memory case was the real problem or a symptom of something
   else (e.g., Option+W too easy to mis-press).

8. **Existing dirty-close behavior.** Before implementing the
   dirty-leave-behind notification, verify what `bay ws close`
   currently does against a dirty workspace: does it already warn,
   silently preserve, or refuse without `--force`? If it already
   prints a warning, the new behavior is just appending the
   Option+Z hint. If it's silent, we're adding both the warning
   and the hint. Affects how much existing output to preserve.
