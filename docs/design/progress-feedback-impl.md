# Progress feedback — implementation plan

Captured 2026-04-17. Companion to `progress-feedback.md` (design).
This doc holds implementation-level detail; the design doc stays
focused on intent and decisions.

**Status:** draft. Current content is the implementation sketch
that was initially captured inline in the design doc. To be
fleshed out as step 1 (window-first `ws new`) implementation work
begins.

---

## Window-first `ws new`

Current flow (grep `engine/workspace.go`):
1. Create worktree at path.
2. Create tmux window at worktree path.
3. Spawn surfaces.

Proposed flow:
1. Open `$LOG_FILE` for writing (creates an empty file on disk);
   keep the FD for later. Then create the tmux window at the
   target repo's toplevel. Placeholder command is inlined into
   `new-window` as a `bash -c` string — no script file needed.
   `WS_NAME` and `LOG_FILE` are passed via tmux's per-window env
   (`new-window -e VAR=VALUE`), not via bay's process environment
   (which would pollute every subsequent tmux spawn):
   ```
   tmux new-window -t <session> -c <repo-toplevel> -n <ws-name> \
     -e "WS_NAME=<name>" -e "LOG_FILE=<path>" \
     bash -c 'printf "Setting up workspace %s...\n\n" "$WS_NAME";
              tail -f "$LOG_FILE" 2>/dev/null || sleep infinity'
   ```
   Ordering matters: the file must exist before the placeholder's
   `tail -f` starts, otherwise it hits the `|| sleep infinity`
   branch and the user sees a static "Setting up..." line with
   no live hook output.
2. Call `positionNewWindow(dockName, placeholderWinID, "", nil)`
   immediately after window creation. The new workspace isn't in
   the manifest yet, so `wsName=""` routes through the "new
   workspace goes after the last existing workspace" branch
   (`workspace.go:967-974`). Window lands at the correct tab
   position; no reposition is needed later, since the
   placeholder-replacement step operates on panes inside the same
   window.
3. Create worktree. Redirect git + hook stdout/stderr to
   `$LOG_FILE`.
4. On success: replace the placeholder pane with the real
   surface via `respawn-pane` (below). For multi-surface
   workspaces, split from the respawned pane for additional
   surfaces.
5. On failure: write the error to `$LOG_FILE` (which the
   placeholder is already tailing), don't tear down the window.
   The workspace is **not** added to bay's manifest on failure —
   the pane with error output is the user's signal, and
   introducing a new "failed-setup" lifecycle state would require
   handling across `ws ls`, `ws close`, recover, etc. Retry =
   user closes the window manually, cleans the worktree dir if
   `git worktree add` got that far, and reruns `bay ws new`.
6. If `tmux new-window` itself fails (tmux unreachable, dock
   session gone), abort `ws new` with a clear error; nothing to
   clean up since no worktree was created.

### Log file location and lifecycle

`$LOG_FILE` lives at
`~/.local/share/bay/logs/ws-new-<dock>-<ws>-<unix-ts>.log`. On
successful placeholder replacement, the file is removed — no
one's tailing it anymore, no reason to keep it. On failure, keep
it for post-mortem; the error message in the pane tells the user
where to find it if they need more. No rotation or eviction in
v1; if this directory ever grows noticeably (unlikely), add a
cleanup on `bay recover`.

### Pane replacement via `respawn-pane`

The placeholder pane is swapped in place using
`tmux respawn-pane -k -c <worktree-path> -t <pane-id>
<surface-command>`. `-k` kills the currently-running command
(the placeholder's `tail -f`); `-c` sets the new cwd to the
worktree path; the pane ID is stable across the swap, which
keeps bay's surface-to-pane tracking clean. For multi-surface
workspaces, the first surface respawns into the placeholder pane
and subsequent surfaces split off it. This avoids any transient
multi-pane layout ("flash") that a split-then-kill approach
would create.

---

## Status-line sentinel

- Sentinel path: `~/.local/share/bay/busy/<dock>`. Concurrent
  ops on the same dock (e.g. CLI close racing with an
  auto-cascade close) are last-write-wins — whichever op updates
  the sentinel most recently is the one shown. No multi-op
  protocol needed; the most recent is the one the user cares
  about.
- Sentinel format:
  ```
  operation=ws-close
  target=my-workspace
  started_at=1713381234
  ```
  `started_at` is set once at first write and never updated —
  it's the baseline used to render the elapsed-time display (e.g.
  "(4s)"). Liveness is tracked separately via the sentinel file's
  mtime, which is touched on every heartbeat.
- `ops-with-busy-sentinel` helper in bay: wraps an op, fires a
  1.5s timer, writes sentinel on fire, heartbeats every 10s while
  the op runs, cleans up on op return.
- `bay status-line` reads `~/.local/share/bay/busy/<dock>` when
  rendering; if the sentinel's mtime was updated within the last
  60s (i.e. a recent heartbeat), show the indicator. If the mtime
  is older than 60s, treat as stale (op's process likely died)
  and ignore.
- `bay recover` cleans the entire `~/.local/share/bay/busy/`
  directory — no legitimate sentinel survives a recover, since
  recover implies bay is restarting from a known state.

### Cleanup

Stale sentinel files (from a crashed bay op) are handled by:
- Heartbeat-based staleness in `bay status-line` (mtime > 60s
  old = stale). A real 30-minute vendor clone continues to show
  because the goroutine keeps heartbeating; a dead bay process
  stops heartbeating and the sentinel goes stale within the
  minute.
- `bay recover` clears the directory.

---

## Gaps to address before implementation

These are the places where this doc is still thin. Fill in as
step 1 work begins; the answers will likely emerge from reading
the current code.

1. **Code-path walkthrough.** `Engine.WsNew` centralises creation
   today. Decide: does window-first live inside `WsNew` (branching
   on "inside dock session" detection), or in a new wrapper that
   calls into the existing `WsNew` after placeholder setup? The
   former keeps one entry point; the latter separates concerns
   more cleanly.

2. **Surface command generation.** The surface-spawn path today
   emits a command (agent binary + args, shell, etc.) that's
   passed to tmux. Need to locate that helper and understand how
   it plugs into `respawn-pane -k ... <surface-command>` cleanly.
   Likely already factored out, but verify.

3. **Testing strategy.** Existing mock tmux tests cover
   deterministic sequences. Window-first adds an async-ish
   pattern (create window → run slow op → replace pane) that
   may need a new test helper. Decide whether to:
   - Fake the slow op to run synchronously in tests (simplest).
   - Add a test seam for "placeholder swap" that can be triggered
     explicitly.

4. **Terminology.** Define "surface command" in terms of current
   code so the doc is self-contained for a reader not already
   in the bay codebase.

5. **`ws new -q` interaction.** Verify that `-q` (keybinding
   suppression, added in #198) plays nicely with the window-first
   flow. Expectation: `-q` affects stdout to the invoking shell;
   the log file is an internal artifact and isn't affected.
   Confirm in code.

6. **Status-line segment placement.** Where the busy indicator
   renders alongside the existing adaptive status-line (#202).
   Likely takes precedence over the workspace name when present.
   Decide alongside status-line implementation in step 2.
