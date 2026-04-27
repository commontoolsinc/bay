# Workspace lifecycle configuration - design plan

Captured 2026-04-26. No code has been written. This doc gathers
the related configuration ideas from the workspace setup discussion:
repo-defined prepare commands, close checks, and optional scoped
storage. It is meant as a review artifact before deciding whether any
of these belong in bay.

This doc intentionally does **not** propose that bay become a package
manager, vendoring tool, or general setup framework. The narrow thesis
is:

> Bay creates and destroys disposable developer workspaces. If a
> workspace needs repo-specific readiness work, bay should be able to
> run it, show its status, and avoid launching surfaces that would fail
> before the workspace is ready.

## Relationship to existing designs

This proposal overlaps with two existing docs:

- `progress-feedback.md`: handles slow operations that make bay feel
  unresponsive, especially `git worktree add` running slow repo hooks.
- `app-model.md`: sketches broader app lifecycle hooks and per-app
  setup/teardown.

The proposal here is smaller than the app model and more general than
window-first progress. If this ships, it should supersede the
`app-model.md` Step 2 `setup_command` / `teardown_command` sketch:
`workspace_prepare` is the v1 workspace-level setup mechanism, and
teardown remains deferred until there is a concrete need.

Later app-model work can still add per-app setup as an additive layer:
workspace prepare runs once for the workspace, then per-app prepare
can run for app-specific requirements. Existing dock-level
`workspace_prepare` config should not need to migrate unless bay
eventually replaces dock config with a repo/app config model.

The pieces do belong together at the level of "repo-defined lifecycle
extension points," but not all should ship together:

1. **Workspace prepare** is the core feature.
2. **Close checks** are adjacent and should only ship when a real repo
   needs a domain-specific close gate.
3. **Scoped storage/cache** is supporting infrastructure and should be
   deferred until repo-owned scripts need a bay-provided storage root.
4. **Window-first progress** remains a separate UX fix for synchronous
   slow operations.

## Problem

Some repos are not usable immediately after `git worktree add`.

The concrete example is loom. Loom has vendored dependencies managed
by `.ops/bin/fetch-vendor.ts`. The script:

- Reads the worktree's `vendors.json`.
- Materializes `vendor/<name>/` as a real git checkout.
- Checks out the pinned ref or an override ref.
- Refuses to overwrite dirty vendor checkouts.
- Regenerates the worktree's `deno.json` from the vendor's config.

That means it is not just an initial clone helper. It is the repo's
authority for which vendor version this worktree requires.

Bay still wants to preserve its current feel:

- `bay ws new` should create workspaces quickly.
- Workspaces should appear immediately in navigation.
- Shells and editors should be available as soon as possible.
- Agents and long-running command surfaces should not start in a
  workspace whose required setup has not finished.
- `bay ws close` should stay simple and snappy.

## Design boundary

Bay should own orchestration and visibility:

- When to run a repo-provided command.
- Where logs go.
- Whether workspace prepare steps are `running`, `ready`, `failed`,
  or `stale`.
- Which surfaces are blocked until readiness.
- How to retry or inspect failed setup.
- How to ask a repo-defined check whether close is safe.

Bay should not own repo-specific setup semantics:

- It should not understand `vendors.json`.
- It should not fetch or update vendored repos directly.
- It should not define dependency versions.
- It should not know whether a cache miss means `git fetch`, `npm
  install`, `deno cache`, or something else.
- It should not auto-fix repo-specific close blockers.

## Workspace prepare

### Configuration

Minimal dock-level configuration:

```toml
[docks.loom]
repo = "loom"

[[docks.loom.workspace_prepare]]
name = "vendors"
command = [".ops/bin/fetch-vendor.ts", "labs"]
blocks = ["agent", "cmd"]
```

The same schema could later move to repo-level config if bay grows
repo config files. Dock-level config is easiest with today's model
because docks already carry defaults and can opt into behavior
independently.

Possible expanded shape:

```toml
[[docks.loom.workspace_prepare]]
name = "vendors"
command = [".ops/bin/fetch-vendor.ts", "labs"]
ready_command = [".ops/bin/loom", "vendors-ready", "labs"]
blocks = ["agent", "cmd"]
run = "auto"
timeout = "10m"
```

Fields:

| Field | Purpose |
|---|---|
| `name` | Stable display/log key, unique within the dock. |
| `command` | Repo-owned argv command run from the workspace path. |
| `ready_command` | Optional cheap check; if omitted, bay trusts the last successful prepare result. |
| `blocks` | Surface classes delayed until prepare succeeds. Initial values: `agent`, `cmd`; likely extensions: `editor`, `all-non-shell`, named apps. |
| `run` | When bay should run it. Initial v1 value should be fixed to `auto` (`new`, `recover`, retry, and blocked-surface launch when stale). |
| `timeout` | Optional guard against stuck setup. |

Commands are argv arrays in v1. Shell-string commands are intentionally
not part of the first design because quoting and template expansion
would become compatibility constraints immediately.

### Runtime environment

Bay runs prepare commands from the workspace path and sets simple env
vars:

```sh
BAY_REPO_NAME=loom
BAY_DOCK_NAME=loom
BAY_WORKSPACE_NAME=vendor-fix
BAY_WORKSPACE_PATH=/Users/mike/projects/loom-worktrees/w7
BAY_WORKSPACE_TYPE=worktree
```

If scoped storage later ships, bay can add:

```sh
BAY_REPO_STORE=...
BAY_DOCK_STORE=...
BAY_WORKSPACE_STORE=...
```

Those variables should not be required for v1.

Close checks receive the same environment. V1 should use environment
variables rather than template expansion in command strings. That keeps
prepare and close-check commands consistent and avoids path-quoting
rules becoming part of the public API.

### Workspace lifecycle

For a new workspace:

1. Bay creates the git worktree.
2. Bay records the workspace in the manifest immediately.
3. Bay starts configured prepare commands in the background.
4. Surfaces not listed in `blocks` may open immediately. Shells are
   expected to stay unblocked in the common case.
5. Surface classes listed in `blocks` wait until prepare succeeds.
6. On success, bay marks the prepare step `ready` and starts blocked
   surfaces.
7. On failure, bay marks the prepare step `failed`, keeps the log,
   and leaves the workspace open. List/picker output may summarize
   that as workspace setup failed.

This makes setup visible without making workspace creation feel like
a hung keybinding.

### Prepare job ownership

"Background" still needs a real owner. V1 should use a detached bay
worker process per prepare step, not the monitor. The worker:

1. Acquires a per-workspace/per-step lock.
2. Records `pid`, `started_at`, and `heartbeat_at` in the manifest.
3. Streams stdout/stderr to the step log.
4. Updates the manifest to `ready` or `failed`.
5. Starts any blocked surfaces if the step succeeds and all other
   required steps are ready.

If another bay invocation sees an active lock with a fresh heartbeat,
it should report the step as already running or wait when explicitly
asked. If the heartbeat is stale, bay marks the step `stale` and allows
retry.

For recovery:

- If a workspace has prepare state `running` with no fresh heartbeat,
  treat it as stale and offer retry.
- If `ready_command` exists and fails, mark the step `stale` and rerun
  or ask the user to retry depending on `run`.
- If there is no `ready_command`, keep the last successful state.

For ordinary use after a `git pull`:

- Bay does not initially watch arbitrary files such as `vendors.json`.
- The repo's own hooks can still run on pull/merge.
- If `ready_command` exists, bay should run it before launching a
  blocked surface; failure marks the prepare step `stale` and reruns
  prepare under `run = "auto"`.
- Without `ready_command`, bay can only trust the last successful
  prepare state until the user runs `bay ws prepare --force`.

That last case is a known v1 caveat: a post-pull dependency change can
make a workspace stale without bay knowing. Repos that need stronger
guarantees should either keep their own post-pull hooks or provide a
cheap `ready_command`.

### User-visible commands

Proposed commands:

```sh
bay ws prepare              # show current workspace prepare status
bay ws prepare --retry      # retry failed/stale steps
bay ws prepare --wait       # block until prepare finishes
bay ws prepare --log        # show or tail prepare logs
```

List and picker output should show a compact status:

```text
w7  vendor-fix  setup=running:vendors
w8  auth-fix    setup=failed:vendors
```

`bay ws show` can include the full step status and log path.

### Failure behavior

Prepare failure does not delete the workspace. The workspace is still
valuable for debugging setup.

CLI output should include:

```text
workspace vendor-fix setup failed: vendors
log: ~/.local/share/bay/logs/prepare/...
retry: bay ws prepare --retry
```

If a blocked agent was requested, bay should leave an obvious pending
state rather than launching the agent into a broken checkout.

### Closing during prepare

Closing a workspace while prepare is running should be supported.

For `bay ws close`, bay first asks its own prepare worker to stop:

1. Send a graceful termination signal to the prepare worker.
2. Wait for a short timeout.
3. If the worker exits, continue normal close.
4. If it does not exit, refuse close with the log path and a message;
   `--force` may kill the worker and continue.

This cancellation should happen before dirty/unpushed checks, because a
running prepare job may still be mutating ignored files in the worktree.

### Interaction with window-first progress

Window-first progress and workspace prepare solve different problems.

Window-first progress is still useful if `git worktree add` itself is
slow because a repo uses synchronous git hooks. In that case bay opens
a placeholder tmux window and tails setup output while the synchronous
operation runs.

Workspace prepare is better for expensive work that can run after the
worktree exists. Repos that want snappy bay workspace creation should
move expensive setup out of `post-checkout` and into bay-visible
prepare commands.

## Close checks

Close checks are a separate but related feature. They answer: "Is it
safe for bay to close this workspace?"

They are useful when a repo has external state bay cannot infer. For
example, a Loom instance might be bound to a worktree. Removing that
worktree could leave the instance pointing at a missing path.

### Configuration

```toml
[[docks.loom.workspace_close_check]]
name = "loom-instance-binding"
command = [".ops/bin/loom", "close-check"]
force = "allowed"
```

Fields:

| Field | Purpose |
|---|---|
| `name` | Stable display/log key, unique within the dock. |
| `command` | Repo-owned argv command run from the workspace path. |
| `force` | Whether `bay ws close --force` may bypass a blocked result. Values: `allowed`, `denied`. |

### Contract

V1 should use exit codes plus human-readable stdout/stderr. JSON
actions can come later if an action-rich case appears.

Statuses:

| Exit | Status | Meaning |
|---|---|---|
| `0` | `clear` | Close may proceed. |
| `10` | `blocked` | Close should be refused unless `--force` is set and the check config has `force = "allowed"`. |
| `20` | `warning` | Close may proceed, but bay should print the message. |
| other | `error` | The check failed; close is refused unless `--force` is set. |

The check should print a concise summary and any remediation commands.
For example:

```text
Loom instance 'personal' is bound to this worktree.
Rebind it first:
  loom use main --instance personal
Or stop it:
  loom stop personal
```

If the command fails unexpectedly, bay should treat the check as
blocked by default:

```text
close check failed: loom-instance-binding
log: ~/.local/share/bay/logs/close-check/...
use --force to bypass
```

This is a different case from a successful check that returns blocked.
A failed check process means bay did not get domain guidance and can be
bypassed with `--force`; a successful blocked result follows the
check's configured `force` policy.

Decision table:

| Check result | `--force` | Check `force` | Close? |
|---|---:|---|---|
| clear | no | any | yes |
| warning | no | any | yes, after printing warning |
| blocked | no | any | no |
| blocked | yes | `allowed` | yes |
| blocked | yes | `denied` | no |
| error | no | any | no |
| error | yes | any | yes, after printing check failure |

Bypass of an errored check should be noisy. Bay should print the
failure every time and keep the log path visible so a flaky check does
not silently weaken close safety.

### Bay behavior

For `bay ws close`:

1. Stop any active prepare worker for the workspace.
2. Run today's cheap built-in dirty/unpushed safety gates.
3. Run workspace close checks only if the workspace is otherwise
   closeable.
4. If any check blocks, refuse close and print its output.
5. `--force` bypasses checks only when the check config says force is
   allowed. Failed check processes are also bypassable with `--force`
   because bay received no valid domain-specific rule.

For `bay dock close`:

- v1 should probably not add dock-level close checks.
- If needed later, dock close can run each workspace close check and
  then an optional dock close check.

Bay should not run remediation actions automatically. The commands in
the check output are instructions for the user, not implicit fixes. V1
prints them only; interactive selection is future work.

## Teardown hooks

Do not add teardown hooks in v1.

Most cleanup should be handled by removing the worktree. Teardown
hooks are more complex because they can make close slow, fail after
the user asked to close, or require the worktree to remain on disk
until cleanup finishes.

If a real need appears, use a two-phase model:

1. Hide/remove the workspace from normal navigation immediately.
2. Run teardown in a background cleanup job.
3. Remove the worktree only after teardown if the hook needs files
   present.
4. If teardown fails, keep a cleanup record visible in `bay doctor`
   or `bay cleanup ls`.

This should not be implemented until there is a concrete repo that
cannot rely on worktree deletion.

## Scoped storage and cache

This is deliberately deferred.

The term "cache" is too narrow as the main abstraction. Some data is
safe to delete and regenerate; other data may be durable state owned
by a repo or dock integration.

If bay later needs this, call the umbrella concept scoped storage:

```sh
BAY_REPO_STORE=...
BAY_DOCK_STORE=...
BAY_WORKSPACE_STORE=...
```

Suggested semantics:

| Scope | Lifetime |
|---|---|
| Repo store | Survives workspace and dock close; removed/pruned when repo is removed. |
| Dock store | Shared by all workspaces in a dock; cleaned with the dock if configured. |
| Workspace store | Tied to one workspace; cleaned on workspace close. |
| `cache/` subdir | Safe to delete; deletion may make future prepare slower. |
| `state/` subdir | Durable for the scope; not deleted except by lifecycle operation. |

For Loom, a future cache-aware `fetch-vendor.ts` could use:

```text
$BAY_REPO_STORE/cache/vendors/labs.git
```

as a shared bare git mirror. On a cache miss, Loom's script would
fetch the missing tag/ref into the mirror, then materialize the
current worktree's `vendor/labs` at the required sha.

Bay should not fetch or update that mirror itself. It only provides a
stable storage root if repo scripts need one.

### Stable IDs

Today's manifest uses names:

- Repo names are globally unique.
- Dock names are globally unique.
- Workspace names are unique within a dock.
- A workspace is effectively identified by `dock:workspace`.

Scoped storage should not use renameable display names as durable
directory names. If scoped storage ships, bay should first add opaque
manifest IDs:

```json
{
  "repos": [{ "id": "r_...", "name": "loom" }],
  "docks": [{ "id": "d_...", "name": "loom" }],
  "workspaces": [{ "id": "w_...", "name": "vendor-fix" }]
}
```

Storage paths would use IDs, while metadata files inside the store can
record current human names for inspection.

## Manifest shape

Workspace prepare state should be persisted so status survives CLI
invocations and recovery:

```json
{
  "name": "vendor-fix",
  "prepare": [
    {
      "name": "vendors",
      "status": "ready",
      "started_at": 1777248000,
      "finished_at": 1777248030,
      "heartbeat_at": 0,
      "pid": 0,
      "definition_hash": "sha256:...",
      "log": "logs/prepare/dock-id/workspace-id/vendors.log"
    }
  ]
}
```

`log` is relative to bay's data directory. CLI output should print the
expanded absolute path for usability, but persisted manifest state
should not depend on a home directory string.

Possible statuses:

| Status | Meaning |
|---|---|
| `pending` | Step has not run yet. |
| `running` | Step is currently running. |
| `ready` | Last run succeeded. |
| `failed` | Last run failed. |
| `stale` | Step was running when bay exited, heartbeat expired, definition changed, or readiness check failed. |

The status model should stay per-step. Avoid a single workspace-level
status enum; bay already has other workspace state such as dirty,
pending, merged, waiting, and missing.

The `definition_hash` should include the prepare step's command,
ready command, blocking policy, and timeout. If config changes, bay can
mark old prepare state stale instead of trusting a success from a
different command.

## Security and trust

Prepare and close-check commands execute repo-defined code. This is
not a new trust category for bay users who already run tests, hooks,
and agent commands in the repo, but bay should still avoid surprising
execution.

Initial behavior should be opt-in through bay config, not automatic
discovery of arbitrary files. A future repo-local config file would
need a trust story.

Commands are argv arrays:

```toml
command = [".ops/bin/fetch-vendor.ts", "labs"]
```

Bay should not interpret shell syntax in lifecycle config. This avoids
quoting problems with paths and keeps template variables out of the v1
contract.

## Open questions

1. **Config home.** Should prepare and close-check config live on
   docks, repos, or both? Dock-level is easiest today. Repo-level
   better matches "every workspace for this repo needs this."

2. **Future foreground mode.** V1 should run prepare in the background.
   Is there a real repo that needs `mode = "foreground"` later, where
   setup must complete before any surface appears?

3. **Ready checks.** Should `ready_command` be required for prepare
   steps that block surfaces? The current proposal keeps it optional
   but documents the stale-post-pull caveat.

4. **Blocked surface UX.** If an agent was requested but prepare is
   running, should bay create a placeholder pane for the agent, or
   only start the agent after readiness? Placeholder is more visible;
   delayed creation is simpler.

5. **Existing git hooks.** Should bay advise users to move expensive
   `post-checkout` work into prepare hooks, or support both equally?
   The pragmatic answer is support both, but only prepare hooks can
   keep `ws new` truly snappy.

## Incremental path

### Step 1: Workspace prepare status [MEDIUM-LARGE]

Add dock-configured workspace prepare commands, persisted per-step
status, logs, retry/wait/log commands, and blocked agent/cmd launch.

This is the smallest feature that directly supports Loom's vendored
setup without bay understanding Loom, but it is not a small patch: it
touches config parsing, manifest schema, process ownership, logs,
surface launch, recovery, and formatting.

A smaller implementation slice would be:

1. Prepare runner, logs, persisted step status, and retry/wait/log
   commands.
2. Blocked surface launch once the runner semantics are solid.

### Step 2: Window-first progress [MEDIUM]

Implement the existing progress-feedback design for slow synchronous
`ws new` operations. This can happen before or after Step 1, but it
solves a different part of the UX.

### Step 3: Close checks [SMALL-MEDIUM]

Add workspace close checks only when a real repo has an external state
blocker that bay cannot see. Require checks to provide a useful
message and remediation.

### Step 4: Scoped storage [DEFER]

Do not build this until repo scripts need a bay-owned storage root.
If it ships, add stable opaque IDs first and expose store paths through
environment variables and `bay store path`.
