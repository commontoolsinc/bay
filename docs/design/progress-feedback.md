# Progress feedback for slow operations — design plan

Captured 2026-04-17. No code has been written. Design emerged from
a discussion about `bay ws new` via keybinding possibly taking
10-60s when the repo's `post-checkout` hook does heavy setup (e.g.
loom's 226 MB vendor-dependency clone). Without a signal that the
keystroke registered, users re-press, feel broken, or lose
confidence in bay's keybindings. The problem generalizes beyond
slow hooks — any future slow bay op hits the same wall.

Implementation detail lives in `progress-feedback-impl.md` (TBD
as step 1 work begins).

## The problem

Bay invokes `git worktree add` synchronously, and git runs the
repo's `post-checkout` hook before returning. Common cases:

- Fast hook (no hook, or cheap): `bay ws new` completes in ~1-2s.
- Slow hook (vendor fetches, heavy build): blocks for 15-60s+.

Today, during the slow case:

- Keybinding fires, nothing visibly happens.
- User presses Option+N again (sometimes multiple times).
- Eventually the window appears; things are fine.
- User's mental model of bay becomes "unreliable" or "sometimes
  slow for no reason."

Two constraints on any fix:

1. **No flicker for fast ops.** A spinner that flashes for 50ms
   on a 200ms op is worse than nothing. Feedback must either be
   committed (guaranteed to be part of the flow) or gated behind
   a delay threshold.
2. **No trapping the user.** Modal blocking during a multi-second
   op is worse than silent waiting. Feedback should be visible
   without stealing input.

---

## Design

### Scope (v1)

Two classes of slow op, handled differently:

- **Ops that create a tmux window** — `bay ws new`, and any future
  dock/surface-spawning work that is the user-visible outcome.
  Window itself is the feedback vehicle.
- **Ops that don't create a window** — potential future cases:
  `bay ws close` on a huge dirty-force teardown, `bay dock close`,
  bulk operations. Status-line indicator with a delay threshold.

### Window-first `bay ws new` [primary]

Reorder creation so the window appears immediately:

1. On `bay ws new` invocation (from inside the dock's tmux
   session), create the tmux window at the target repo's toplevel
   with a **placeholder pane** showing "Setting up workspace
   `<name>`..." plus a live tail of the hook log.
2. Switch the triggering client to the new window immediately.
   User sees the window open within ~100-300ms of pressing the key.
3. Run `git worktree add` (which runs the hook) asynchronously;
   its output streams into the placeholder's log.
4. On completion, replace the placeholder pane contents with the
   real surface in the same window. (Mechanism: `respawn-pane -k`
   — see impl doc.)
5. On failure, leave the placeholder with the error output and
   don't register the workspace in bay's manifest.

Why this answers the "modal in front of the one window being
initialized" question: tmux has no per-window popup primitive
(`display-popup` is per-client). Creating the window early with a
placeholder pane gives the same effect by construction — the
window was always going to open; we just make it open sooner with
loading state that later gets replaced. No transient overlay, no
modal trap, and live hook output is visible for debugging slow
setup.

### Status-line sentinel for non-window ops [secondary]

For slow ops that don't create a window, a file-based sentinel
at `~/.local/share/bay/busy/<dock>` drives a status-line
indicator like `busy: closing my-workspace (4s)`. 1.5s delay
before first write (so fast ops never register), heartbeat every
10s, stale if mtime > 60s. Full protocol in the impl doc.

No confirmed slow non-window op exists today; this is defensive
scoping for step 2. Keep the design surface minimal until a real
workload drives it.

---

## Corner cases

1. **Setup fails partway.** Placeholder window exists. Bay writes
   the error to the log file (which the placeholder tails), does
   **not** register the workspace in the manifest, and leaves the
   window in place. User sees the error, closes the window
   manually, cleans any leftover worktree dir if needed, and
   retries. Rationale for "don't add to manifest": avoids a new
   lifecycle state that every other command would have to handle.

2. **`bay ws new` from CLI (not keybinding).** Window-first
   applies only when invoked from inside the dock's tmux session
   (detected via `tmux display-message -p '#{session_name}'`
   against the dock name). When the session matches, window-first
   runs as normal. When it doesn't match (different tmux session,
   or outside tmux entirely), bay falls back to current blocking
   behavior and writes git/hook output to stderr. No orphan
   placeholder window in a session the user isn't looking at.

3. **Keybinding invokes `ws new` with an agent surface to
   spawn.** The agent spawn is delayed until setup completes
   (agent needs the worktree). Placeholder covers the gap.

4. **Two keybinding presses in quick succession.** Today, a user
   who doesn't realize the first press worked will press again;
   both invocations block on `git worktree add` and the user ends
   up with two simultaneously-creating workspaces. With
   window-first, the first press creates a window within ~100ms,
   so a second press after that gap is clearly "make another
   one." Accidental sub-100ms double-press still creates two —
   preferable to today's silent double-fire.

5. **Tab-order positioning.** The placeholder window must land at
   the same tab position the current flow produces. Resolved: call
   `positionNewWindow` immediately after creating the placeholder.
   Since the manifest doesn't yet contain the new workspace,
   positioning routes through the "after the last existing
   workspace" branch. Pane replacement happens inside the same
   window, so no further repositioning is needed.

6. **Bay crashes between window creation and surface spawn.** The
   placeholder (a bash process running `tail -f`) persists after
   bay exits. User sees a stuck "Setting up workspace..." pane
   and closes the window manually. Annoying but not broken — no
   data loss since no worktree was ever registered. A step-3
   polish could have `bay recover` detect and clean orphaned
   placeholders via a tmux user option like
   `@bay-placeholder=1` set at window creation.

---

## Design decisions

1. **Primary mechanism: window-first for `ws new`.** Create the
   window immediately with a placeholder pane; replace the
   placeholder contents when setup completes. No threshold
   needed — the window is always going to appear, so there's no
   flicker problem.

2. **Placeholder cwd: the target repo's toplevel.** Always valid,
   matches the user's mental model, gives the placeholder shell
   a sensible context.

3. **Pane replacement via `respawn-pane -k`.** Keeps pane ID
   stable, no transient multi-pane layout. Split only for
   additional surfaces in multi-surface workspaces.

4. **Window-first only inside the dock's tmux session.** CLI from
   elsewhere falls back to current blocking behavior so no orphan
   placeholder appears in a session the user isn't looking at.

5. **No `failed-setup` manifest state.** On failure, the window
   stays with its error output and nothing is registered. Avoids
   a new lifecycle state that every command would have to handle.

6. **Secondary mechanism: status-line sentinel for non-window
   ops.** 1.5s threshold before first write; heartbeat every 10s;
   staleness = mtime > 60s.

7. **No popups for progress.** They're modal and trap the user.
   Popups remain acceptable for confirmation prompts — different
   use case.

8. **No always-on spinners.** Flicker avoidance is a hard
   requirement.

---

## Open questions (to revisit when building)

1. **Placeholder content detail.** Static message + `tail -f` of
   hook log is the plan. Add a spinner frame? Worth trying plain
   text first; add animation only if static feels lifeless.

2. **Repo-customizable placeholder.** Should a repo be able to
   drop a `.bay/ws-new-progress.sh` script that bay runs as the
   placeholder? Would let loom show "fetching vendors..." with
   specific framing. Defer to v2 — default placeholder should be
   fine.

3. **Threshold tuning.** 1.5s for non-window ops is a guess.
   Tune based on usage. A config option is probably YAGNI.

4. **Should window-first be opt-out?** Someone might prefer the
   current behavior (no window until ready). Unlikely to matter
   in practice; don't add a flag until someone asks.

5. **Log file retention.** v1 removes on success, keeps on
   failure, no rotation. If the `logs/` dir grows noticeably, add
   cleanup to `bay recover`.

---

## Incremental path

### Step 1: Window-first `ws new` [MEDIUM]

Refactor the `WsNew` flow to create the tmux window with a
placeholder before running `git worktree add`. Replace the
placeholder with real surfaces on completion. Handle error states
cleanly (keep window, write error to log, don't register in
manifest).

### Step 2: Status-line sentinel [SMALL]

Deferred until there's a confirmed slow non-window op worth
indicating. Add the busy-sentinel protocol and wrap the
offending op; update `bay status-line` to render the indicator;
include staleness handling and `bay recover` cleanup.

### Step 3: Polish [SMALL — incremental]

- Repo-customizable placeholder script
- Better spinner frames / progress visualization
- Threshold tuning for step 2
- `bay recover` cleanup for orphaned placeholders
