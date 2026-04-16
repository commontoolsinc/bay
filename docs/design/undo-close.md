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
- `bay sf close` / Option+W on the last surface in a workspace
  (cascades to workspace close) → single workspace entry, not a
  surface entry plus a workspace entry
- `bay ws close` from the CLI → workspace entry

Out of scope for v1:

- tmux-native kills (`Ctrl-B x`, `kill-pane`, `kill-window`) — bay
  detects these after the fact; capturing pre-close state is
  harder. Fall through unrestored.
- `bay dock close` / dock teardown — recording the full dock is
  out of proportion with the muscle-memory use case, and the
  queue is per-dock so the queue itself goes with the dock.

### The queue

- **Scope:** per dock. Closing in one dock never surfaces in
  another's undo.
- **Storage:** persisted inside the dock's state. Survives tmux
  kill, bay recover, reboot.
- **Retention:** last 10 entries per dock, with a 24-hour wall-clock
  expiry. Entries older than 24h are pruned on next access. When a
  dock is torn down, its queue goes with it.
- **Ordering:** LIFO. `bay sf restore` pops the most recent entry.

### Entry shape

A close entry records enough to recreate the closed thing without
perfect fidelity — we're restoring structure, not in-flight state
that wasn't persistent anyway.

**Surface entry** (single-surface close, workspace survives):

| Field | Purpose |
|---|---|
| `timestamp` | For 24h expiry |
| `kind` | `"surface"` |
| `dock` | Dock name (redundant with queue scope, but explicit) |
| `workspace` | Parent workspace name |
| `surface_type` | `agent` / `shell` / `cmd` / `editor` |
| `surface_config` | Type-specific: agent name + args, cmd string + cwd, editor name, etc. |

**Workspace entry** (direct close or cascade):

| Field | Purpose |
|---|---|
| `timestamp` | For 24h expiry |
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
2. Recreate the surface with the recorded config. Place at end of
   the workspace's surface list for v1; exact position restoration
   is deferred (see step 3).
3. Focus the restored surface.

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
workspace close (direct or cascaded) leaves the worktree in
place because it's dirty, emit a notification. The channel
depends on how the close was invoked:

- **Keystroke-invoked** (Option+W cascading to last-surface
  close): tmux toast via `tmux display-message`. CLI text output
  isn't visible because tmux's `run-shell` detaches stderr.
- **CLI-invoked** (`bay ws close` or `bay sf close`): write the
  warning to stderr. The user is in a terminal and will see it;
  a tmux toast would be redundant (and might be missed if the
  terminal is scrolling).

The message is the same in both cases:

```
foo: worktree preserved (dirty). Option+Z to restore.
```

**Detection heuristic:** branch on whether stderr is a TTY. CLI
invocations in an interactive terminal have a TTY; tmux
`run-shell` doesn't. This keeps the keybinding registrations in
`setup.go` unchanged. Edge case: CLI invocation with stderr
piped elsewhere (script/CI) — neither channel fires visibly,
which is acceptable since scripts rarely care about the toast
content and the command still succeeded.

Rationale: the user asked for teardown and didn't get full
teardown. Silent leave-behind leads to stale worktrees piling up
in the dock directory and surprise name collisions on later
`bay ws new`. This is exceptional-state feedback, not
routine-close feedback — and the undo is what makes the message
actionable ("here's the leftover, here's how to grab it back").

### Lifecycle integration

| Event | Queue effect |
|---|---|
| `bay sf close` | Push surface entry (unless cascade; see next row) |
| Last-surface close | Push a single workspace entry (cascade), not a surface entry |
| `bay ws close` | Push workspace entry |
| `bay sf restore` | Pop most recent, attempt restore |
| `bay dock close` | Drop queue with the dock |
| `bay recover` | Queue survives (lives in dock state) |
| Entry ages past 24h | Pruned on next access |
| Queue exceeds 10 | Oldest entry dropped |

---

## Incremental path

### Step 1: Surface-level undo [MEDIUM — ~250-300 lines]

Build for the stated use case. Record a surface close, restore it
to its parent workspace. No worktree/branch mechanics involved.

Includes the full queue infrastructure: per-dock scoping,
persistence, retention/expiry, `bay recover` integration, CLI,
keybinding. Covers the muscle-memory case directly and ships
independent of workspace-level work.

### Step 2: Workspace-level undo [MEDIUM-LARGE — ~400-600 lines]

Record workspace closes (direct and cascaded). Handle the two
worktree paths (preserved vs removed). Implement branch SHA
recovery via reflog. Branch- and workspace-name collision checks.
Agent session continuity considerations (see corner case 4).

Depends on step 1's queue primitives.

### Step 3: Restore polish [SMALL — incremental]

Things to consider after real usage:

- `--list` output formatting and column choices
- Exact pane-layout restoration (capture tmux layout strings at
  close time, apply on restore)
- A picker for non-most-recent restore, if demand appears
- Toasts, if discoverability turns out to be a problem

---

## Corner cases and known limitations

1. **Branch SHA GC.** Clean-close branch restore relies on the git
   reflog. Default reflog expiry for unreachable commits is 30
   days — well past our 24h undo window. But `git gc --prune=now`,
   aggressive `gc.reflogExpireUnreachable`, or `git maintenance`
   (auto-enabled by GitHub Desktop and newer git defaults) can
   make recorded SHAs unrecoverable sooner. Caveat: documented,
   not defended against.

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

---

## Open questions (to revisit when building)

1. **Storage location.** Inline in the dock manifest, or a
   separate `<dock>/.bay/undo-queue.json`? Inline is simpler;
   separate avoids polluting the manifest with transient state.
   Decide when implementing step 1.

2. **Retention limits as config.** 10 entries + 24h is a
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
