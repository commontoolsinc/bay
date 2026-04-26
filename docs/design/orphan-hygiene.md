# Orphan workspace hygiene

Captured 2026-04-20.

## The problem

When a workspace's panes die outside of bay — user runs `Ctrl-B x`
in tmux, the pane's process exits, the tmux window is closed
directly, etc. — bay's sync pass detects the dead `PaneID`s and
strips the corresponding surfaces from the manifest
(`sync.go:284-298`). The workspace itself is left behind as a
zero-surface entry.

These orphan workspaces collect invisibly. The worktree is still
on disk, the branch is still around, but nothing is running. The
user sees them only when they explicitly look (`bay tree`,
`bay ws ls`). Pattern observed on one dock: three accumulated
orphans that the user didn't remember creating and had no signal
were there.

Bay's auto-close cascade today only fires when bay *itself*
closes the last surface (`surface.go:282-289`). The sync-detect
path deliberately doesn't cascade, on the historical reasoning of
"don't nuke work the user might want to keep." That caution makes
sense for dirty / unlanded workspaces, but for clean workspaces
it just leaves clutter.

## The fix

Two-step cascade with a grace window between strip and finalize:

1. **Schedule.** When a sync pass strips a workspace's surfaces and
   the workspace ends up empty, set
   `Workspace.PendingCloseAt = now + 60s` on the workspace. The
   surface list is empty but the workspace still exists.
2. **Finalize.** On each subsequent sync pass, scan for workspaces
   with `PendingCloseAt > 0`:
   - If a surface was re-added (e.g. via `bay sf new --ws <name>`)
     the cancel path has already cleared `PendingCloseAt` to 0; nothing
     to do.
   - If `PendingCloseAt <= now` and surfaces are still empty:
     attempt `WsClose(force=false)`. On success the workspace is
     gone. On refusal (dirty or unlanded) clear `PendingCloseAt`
     so the workspace isn't re-tried every sync — it becomes a
     permanent orphan for the user to resolve.

The grace window gives the user a chance to rescue a workspace
they accidentally emptied (e.g. muscle-memory `Ctrl-B x` or
`Option+W` misfire) before any destructive work happens. Commit
work, run `bay sf new --ws <name>`, or just wait it out — if the
workspace shouldn't live, 60s passes and it cleans up.

The existing `WsClose(force=false)` gates on both `IsDirty` and
`HasUnpushedCommits` (`workspace.go:357-371`):

- Clean + landed → close succeeds; workspace and branch cleaned up.
- Dirty or unlanded → close refuses; `PendingCloseAt` gets cleared
  so we stop retrying. Workspace stays as a permanent orphan.

We reuse those gates as-is. No new policy beyond the grace window.

### Grace window applies to three paths

- **Sync-detected strip** (tmux-native kill): the primary driver.
- **`bay sf close` on the last surface, no `--force`** (e.g.
  `Option+W`): muscle-memory-class accident. The cascade to
  workspace close now schedules `PendingCloseAt` instead of
  calling `WsClose` directly. Same 60s rescue window.
- **`bay sf close --force` on the last surface**: explicit user
  intent. No grace — `WsClose` fires immediately (same as today).

Direct `bay ws close <name>`, `--done`, and `--clean` are
unaffected. They're explicit user requests and continue to
close immediately.

### Scope

- **In scope:** newly-emptied workspaces in this sync pass (at
  least one surface was just stripped, result is empty, workspace
  is clean + landed).
- **Not in scope:** pre-existing orphans from before this change
  shipped. They persist until the user closes them manually. No
  retroactive sweep — keeps the behavior predictable and avoids
  surprise auto-closes on deploy.
- **Not in scope:** dirty orphans. Stay untouched; user commits
  or `--force`-closes. Same policy as bay-initiated close.

### Why not do more

- **Undo on sync-detect.** Considered and rejected (see earlier
  discussion around undo-close design). The user's pain is orphans
  collecting, not "I want to restore a pane I intentionally
  killed with Ctrl-B x." Auto-close addresses the pain directly
  without the queue machinery.
- **Aggressive sweep of existing orphans.** Tempting but riskier:
  a workspace someone legitimately has as zero-surface (e.g. a
  just-created workspace that hasn't had a surface spawned yet)
  would get nuked. Stick to "fire only when we just stripped the
  last surface."

## Implementation

One insertion point in `sync.go`:

- `applyWorkspaceSyncUpdate` currently returns `changed bool`.
  Extend to also signal "this workspace just went empty via strip."
- In `SyncAll`'s apply closure, collect `(dock, ws)` pairs for
  these. After the manifest lock releases, call
  `e.WsClose(dock, ws, false)` on each; swallow errors (a failure
  means the gates refused — workspace stays as an orphan, which
  is the right outcome).

The call happens **after** the manifest lock is released because
`WsClose` acquires its own lock. Doing it inside would deadlock.
This mirrors how other bay commands are structured.

### Dirty-check staleness

`IsDirty` and `HasUnpushedCommits` are evaluated inside
`WsClose`, not during the sync probe. So the check is fresh
against current git state, not a cached field. No staleness
concern between probe and close.

### Race with concurrent ops

If the user runs `bay ws new` on the same dock simultaneously,
the manifest-lock serialization handles it: the auto-close either
fires before the new workspace is registered, or it fires on a
workspace that has since gained surfaces (in which case
`ws.Surfaces` is non-empty and `WsClose` proceeds normally with
its own gates, or the auto-close attempt is a no-op).

If a second sync pass runs concurrently, one acquires the lock
first; whichever pass emptied the workspace first gets to
attempt the close. The other pass sees the already-closed
workspace gone from the manifest and skips.

## Corner cases

1. **Workspace emptied, but `WsClose` fails.** Unpushed commits,
   or repo moved, or some other transient error. Orphan stays;
   the user handles it manually. No retry loop — next sync pass
   would re-attempt if it strips surfaces again, but typically
   there are no surfaces left to strip.

2. **Last workspace in a dock is auto-closed.** Same behavior as
   a user manually running `bay ws close` on the last workspace:
   dock stays, placeholder window (`placeholder.go`) kicks in to
   keep the tmux session alive.

3. **User had dock-level surfaces on that dock.** Orthogonal —
   dock surfaces aren't workspace surfaces. Not affected.

4. **Session is dead.** Sync already skips surface-stripping when
   `!sessionAlive` (`sync.go:198-201`). No cascade fires during
   recover-ish states, by construction.

5. **`bay recover` and in-flight `PendingCloseAt`.** Recover does
   not clear `PendingCloseAt`. If a workspace was mid-grace when
   bay exited, the pending close survives the restart and the next
   sync after recover finalizes it (if the grace has elapsed) or
   continues waiting (if not). Rationale: recover reconciles tmux
   state against a manifest the user hasn't touched — there's no
   affirmation that previously-emptied workspaces should be kept.
   If the user cares, they `bay sf new --ws <name>` post-recover
   just like before the crash.

## Incremental path

Single PR. ~20-30 lines in `sync.go` plus a test. No config
knobs, no CLI surface, no docs beyond this design note.

### Follow-ups (not this PR)

- Visibility for dirty orphans: `bay tree` could mark them, or
  status line could count them. Separate concern — this PR
  only addresses clean orphans going away on their own.
- Retroactive one-off sweep (e.g. `bay recover --clean-orphans`):
  add only if users ask. Most active users' existing orphans
  are few enough to clean manually.
