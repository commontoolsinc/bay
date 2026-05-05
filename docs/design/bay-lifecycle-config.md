# Bay lifecycle configuration - design plan

Captured 2026-04-26. No code has been written. This doc gathers
the related configuration ideas from earlier setup discussions:
repo-defined prepare commands, close checks, and optional scoped
storage. It is meant as a review artifact before deciding whether any
of these belong in bay.

This doc intentionally does **not** propose that bay become a package
manager, vendoring tool, or general setup framework. The narrow thesis
is:

> Bay creates and destroys disposable developer bays. If a
> bay needs repo-specific readiness work, bay should be able to
> run it, show its status, and avoid launching surfaces that would fail
> before the bay is ready.

## Relationship to existing designs

This proposal overlaps with two existing docs:

- `progress-feedback.md`: handles slow operations that make bay feel
  unresponsive, especially `git worktree add` running slow repo hooks.
- `app-model.md`: sketches broader app lifecycle hooks and per-app
  setup/teardown.

The proposal here is smaller than the app model and more general than
window-first progress. If this ships, it should supersede the
`app-model.md` Step 2 `setup_command` / `teardown_command` sketch:
`bay_prepare` is the v1 bay-level setup mechanism, and
teardown remains deferred until there is a concrete need.

Later app-model work can still add per-app setup as an additive layer:
bay prepare runs once for the bay, then per-app prepare
can run for app-specific requirements. Existing dock-level
`bay_prepare` config should not need to migrate unless bay
eventually replaces dock config with a repo/app config model.

The pieces do belong together at the level of "repo-defined lifecycle
extension points," but not all should ship together:

1. **Bay prepare** is the core feature.
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

- `bay new` should create bays quickly.
- Bays should appear immediately in navigation.
- Shells and editors should be available as soon as possible.
- Agents and long-running command surfaces should not start in a
  bay whose required setup has not finished.
- `bay close` should stay simple and snappy.

## Design boundary

Bay should own orchestration and visibility:

- When to run a repo-provided command.
- Where logs go.
- Whether bay prepare steps are `running`, `ready`, `failed`,
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

## Bay prepare

### Terminology

- **Step** — one named entry in a bay's prepare config; one
  `[[bay_prepare]]` block. Has a `name`, a `command`, and optional
  `ready_command`, `blocks`, `timeout`. A bay's prepare config is a
  list of steps that run serially in declared order. The realistic
  case is one step (loom has `vendors`); N is supported for future
  repos.
- **Prepare run** — one execution of a step's command by the
  worker. A step may run more than once over its lifetime (initial
  run, retry, post-stale rerun); each is a separate run with its
  own line in the log file's run separators.
- **Worker** — a detached `bay prepare-worker` process spawned per
  bay that iterates the bay's steps serially. One worker process
  per bay's in-flight prepare, regardless of step count.

### Configuration

Prepare config can live in two places, layered:

1. **Repo-local** in `.bay.toml` at the repo root. Owned by the repo
   maintainer, checked in, travels with the code. This is the default
   source of truth for "what does this repo need to be ready."
2. **Dock-level** in the user's bay config. A thin override layer on
   top of repo-local; per-field overrides win where set, repo defaults
   fill in everything else.

A repo's `.bay.toml`:

```toml
[[bay_prepare]]
name = "vendors"
command = [".ops/bin/fetch-vendor.ts", "labs"]
ready_command = [".ops/bin/loom", "vendors-ready", "labs"]
blocks = ["agent", "cmd"]
```

A user's dock config (overrides only):

```toml
[docks.loom]
repo = "loom"

[[docks.loom.bay_prepare]]
name = "vendors"
timeout = "20m"   # override repo default for this user
```

If the user has not opted into repo-local config (see Trust below),
the entire `.bay.toml` is ignored and dock-level is the only source.

The full set of available fields, shown here as a single dock-level
block configuring everything inline (most users would split these
across `.bay.toml` defaults and dock-level overrides):

```toml
[[docks.loom.bay_prepare]]
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
| `name` | Stable display/log key. Used to merge repo-local and dock-level entries (same name = the dock-level entry overrides the repo-local entry per field). Within a single source (repo-local or one dock block), names must be unique. |
| `command` | argv command run from the bay path. May be set in repo-local `.bay.toml`, dock-level config, or both (dock-level overrides per field). |
| `ready_command` | Cheap check that decides whether prepare needs to rerun. **Required** when `blocks` is non-empty, since blocked surfaces depend on a fresh readiness signal; optional otherwise (bay then trusts the last successful prepare result). |
| `blocks` | Surface classes delayed until prepare succeeds. Initial values: `agent`, `cmd`; likely extensions: `editor`, `all-non-shell`, named apps. |
| `run` | When bay should run it. Initial v1 value should be fixed to `auto` (`new`, `recover`, retry, and blocked-surface launch when stale). |
| `timeout` | Optional guard against stuck setup. |

Commands are argv arrays in v1. Shell-string commands are intentionally
not part of the first design because quoting and template expansion
would become compatibility constraints immediately.

### Configuration validation

Bay validates `bay_prepare` blocks at config load and refuses to
start with a clear error if any are malformed:

- `name` must be non-empty. Within a single source (one repo-local
  `.bay.toml` or one dock-level config block), names must be unique.
  Same-name entries across repo-local and dock-level are not a
  collision; they merge per field with dock-level winning.
- `command` must be a non-empty argv array in the effective merged
  config. (A dock-level override that sets only `timeout` is fine if
  the matching repo-local entry supplies `command`.)
- `blocks` may only contain known surface classes (initial v1 set:
  `agent`, `cmd`).
- `ready_command`, when present, must be a non-empty argv array.
- `ready_command` is required in the effective merged config when
  `blocks` is non-empty.
- `timeout`, when present, must parse as a positive duration.

Validation runs against the **effective config** — the result of
merging repo-local and dock-level. So if a user's dock-level override
introduces a name collision (within the dock block), an empty
command, or leaves a blocking step without a `ready_command`, that is
caught the same way as a malformed repo-local file.

These checks fire before any bay operation runs, so a typo
never causes a half-prepared bay.

### Repo-local config and trust

Bay reads `.bay.toml` from the repo root **live** on every bay
operation. It does not copy the contents into the user's dock config
or cache a snapshot. This makes the maintenance story trivial: when
the repo updates `.bay.toml`, the next bay operation picks up
the change automatically. Where the user has dock-level overrides,
those continue to win; everything else propagates.

Discovery:

- Bay looks for `.bay.toml` in the repo root the dock points at.
- If absent, bay behaves as it does today (dock-level only).
- If present but trust is not granted at any level (see below), bay
  ignores the file and prints a one-line hint on `bay dock new` and
  `bay new`: `repo provides .bay.toml; set trust_repo_bay_toml = true
  on the dock or repo to enable.`

Bay-level commands (`bay dock new`, `bay new`) never prompt
interactively. CLI commands are expected to be scriptable; an
interactive prompt mid-flow is friction in the common case (CI, agent
runs, `bay new` auto-creating a dock). Trust is configured before
the fact, not negotiated at the moment of use.

#### Hierarchical trust

`trust_repo_bay_toml` can be set at two levels, with the dock-level
value overriding the bay-level default:

```toml
# 1. Bay-level (top of the user's bay config)
trust_repo_bay_toml = false    # default; affects every dock

# 2. Dock-level
[docks.loom]
trust_repo_bay_toml = true     # this dock trusts its checkout's .bay.toml

[docks.loom-untrusted]
trust_repo_bay_toml = false    # this dock does not
```

Resolution order:

1. Dock-level explicit value wins if present.
2. Otherwise, bay-level explicit value.
3. Otherwise, built-in default (`false`).

The built-in default is `false` because trusting an arbitrary
checkout's `.bay.toml` runs repo-defined commands on bay creation. A
user who has not configured trust at any level sees only the hint,
never silent execution.

Setting bay-level `trust_repo_bay_toml = true` means every new dock
trusts its checkout's `.bay.toml` automatically. This is convenient
for a single developer who knows all the repos they work with, but it
is a real footgun: creating a dock against a strange checkout
immediately grants trust. Dock-level trust is the recommended path —
it lets a user say "this specific checkout is safe" without granting
blanket trust to unfamiliar code.

(Earlier drafts proposed a separate repo-level tier. After
dock-checkout-merge (#281) collapsed `Repo` and `Dock` into a single
1:1 entity, the repo tier no longer has a coherent home — every
checkout already has a unique dock, so dock-level trust covers the
case the repo tier would have addressed.)

Once trust is granted, future updates to `.bay.toml` propagate
automatically. This matches the existing trust posture: bay already
runs repo-defined commands (tests, agents, fetch-vendor) once a
checkout is set up, so subsequent changes to `.bay.toml` are no
broader than the existing grant.

#### Setting the field

The field name and the layered semantics are not something users
should have to remember. `bay setup` is the discoverable home:

- `bay setup` already walks first-time configuration (config, docks,
  keybindings, completions). It gains a step that iterates the docks
  bay tracks; for each one that **(a)** has a `.bay.toml` in its
  checkout, **(b)** has no `trust_repo_bay_toml` set on the dock,
  and **(c)** has not been dismissed before, setup summarizes the
  prepare entries and prompts: `Trust this checkout's .bay.toml?
  [y/N/skip]`.
  - **Yes** writes `[docks.<name>] trust_repo_bay_toml = true`.
  - **No** writes `[docks.<name>] trust_repo_bay_toml = false`.
  - **Skip** writes nothing to user config but records a
    `trust_prompt_dismissed: true` flag against the dock in the
    manifest. Subsequent setup runs honor that flag and skip the
    prompt; the trust state remains absent (so the dock inherits
    from bay-level or default `false`).
- `bay setup --reset-trust-prompts` clears the manifest dismissal
  flags, surfacing the prompt again on the next setup run for docks
  that still have no trust value in config. A user who has already
  written `true` or `false` is unaffected — config wins, the prompt
  doesn't reappear.
- Bay-level trust (the global footgun) is **not** prompted by setup.
  A user who wants to trust every future dock by default has to edit
  the config directly. This is intentional friction.
- Creation-time on a dock: `bay dock new --trust-repo-bay-toml /
  --no-trust-repo-bay-toml` writes the field into the dock block.
  Default if neither flag is given is to omit the field, so the dock
  inherits from the bay-level value or default `false`.
- Creation-time via auto-create: `bay new` auto-creates a dock
  when one does not already exist for the repo. It accepts the same
  `--trust-repo-bay-toml` / `--no-trust-repo-bay-toml` flags, which
  apply only to the auto-created dock. This makes the first-run
  experience self-contained: a user cloning a new repo can run
  `bay new my-fix --trust-repo-bay-toml` and have prepare run
  immediately, without detouring through `bay setup`. If a dock
  already exists, the flag is ignored with a printed warning (TTY
  only); use `bay setup` or `bay config edit` to change trust on an
  existing dock.
- Manual edit via `bay config edit` opens the file in the user's
  editor and works at any level. Always available as a fallback.

V1 does not ship dedicated `bay dock trust` / `bay repo trust`
commands. The combination of setup + creation-time flag + config edit
covers the discoverable path for the common case; if managing-by-edit
becomes painful in practice, we can add typed setters then.

A future additive UX feature can surface "the repo's `.bay.toml` has
changed since you last looked" for visibility, but it is not a v1
requirement.

### Implementation note: config writes

`bay dock new` and `bay setup` are the v1 commands that modify the
user's TOML config for prepare-related settings. Today bay reads
config but does not write it; this introduces the first writes. The
implementer should keep it surgical at the file layer rather than
round-tripping through marshal.

Round-tripping TOML through `BurntSushi/toml` (or any of the common
Go libraries) loses comments, blank lines, and ordering, because
those are not part of the parsed in-memory representation. Users care
about these — a config file with their own annotations should not get
silently rewritten when bay touches it.

V1 writes:

- `bay dock new` appends a new `[docks.<name>]` block to the end of
  the config file. No existing content is touched, so comments are
  preserved by default. The block includes `trust_repo_bay_toml`
  only when `--trust-repo-bay-toml` or `--no-trust-repo-bay-toml`
  was given; otherwise the field is omitted and the dock inherits.
- `bay setup` writes `trust_repo_bay_toml = true|false` into the
  matching `[docks.<name>]` block based on user answers. If the
  block already exists, surgical edit: locate the header, find the
  field line, replace or insert just that line. Everything else in
  the block stays byte-for-byte identical. If the block doesn't
  exist, append at the end of the file like `bay dock new` does.

Manual edit via `bay config edit` is the always-available escape
hatch and the only way to set bay-level trust.

### Runtime environment

Bay runs prepare commands from the bay path and sets simple env
vars:

```sh
BAY_DOCK_NAME=loom
BAY_NAME=vendor-fix
BAY_PATH=/Users/mike/projects/loom-worktrees/w7
BAY_TYPE=worktree
```

If scoped storage later ships, bay can add:

```sh
BAY_DOCK_STORE=...
BAY_STORE=...
```

Those variables should not be required for v1.

Close checks receive the same environment. V1 should use environment
variables rather than template expansion in command strings. That keeps
prepare and close-check commands consistent and avoids path-quoting
rules becoming part of the public API.

### Bay lifecycle

For a new bay:

1. Bay creates the git worktree.
2. Bay records the bay in the manifest immediately.
3. If invoked from a TTY, bay prints a one-line dispatch summary
   noting that prepare started in the background and the commands to
   follow or stop it. This is best-effort operability, not a security
   gate; in non-TTY invocation paths (most notably tmux keybindings),
   the message is omitted without ceremony. Trust is the gate that
   matters; see Security and trust.
4. Bay starts the configured prepare commands in the background.
5. Surfaces not listed in `blocks` may open immediately. Shells are
   expected to stay unblocked in the common case.
6. Surface classes listed in `blocks` wait until prepare succeeds. When
   a blocked surface is requested, bay creates a placeholder pane
   immediately and tails the prepare log into it. The user sees that
   work is happening rather than wondering whether the request was
   lost; without this they would re-issue the request.
7. On success, bay marks the prepare step `ready` and swaps the
   placeholder for the real blocked surface.
8. On failure, bay marks the prepare step `failed`, keeps the log,
   and leaves the bay open. The placeholder pane stays visible
   with the failure summary and retry instructions. List/picker output
   may summarize that as bay setup failed.

### Prepare job ownership

"Background" still needs a real owner. V1 reuses the `bay` binary
itself: the parent CLI re-execs `bay prepare-worker --dock <name>
--bay <id>` as a detached child, then returns. `prepare-worker` is
a hidden top-level cobra subcommand (`Hidden: true`) — invisible to
`bay --help` and tab completion, reachable only by name. Re-execing
the same binary structurally rules out version skew between parent
and worker (no separate binary to forget to update).

A single **controller** worker handles all of a bay's steps,
iterating them serially in declared order. One pid per bay, one
heartbeat to track. This keeps lock granularity simple, avoids
inter-step dependency declarations, and matches loom (one step,
`vendors`). Parallel execution can be added later by relaxing the
ordering constraint; per-step locks are already compatible with
that.

The worker:

1. For each step in declared order, acquires a per-step file lock
   at `<bay-data>/locks/prepare/<dock>/<bay>/<step>.lock` using
   `flock(LOCK_EX | LOCK_NB)`. The OS releases the lock when the
   process exits, clean or crashed.
2. Writes `pid`, `started_at`, and an initial `heartbeat_at` to the
   manifest entry for the current step, then refreshes
   `heartbeat_at` every 5 seconds while the step runs.
3. Streams stdout/stderr to the day's prepare log (see Logging).
4. On command exit, atomically writes the final status (`ready` or
   `failed`) and `finished_at` to the manifest (write-tmp +
   rename).
5. If the step succeeded, releases the lock and proceeds to the
   next step. If it failed, stops the worker (later steps are not
   run).
6. After the final step succeeds, dispatches any queued blocked
   surfaces for this bay before exiting.

Lock and heartbeat together cover four observable states for any
other bay invocation:

| Lock held | Heartbeat fresh (≤30s) | Interpretation | Action |
|---|---|---|---|
| yes | yes | Worker running normally | Report as `running`; `--wait` blocks |
| yes | no | Worker stuck (rare) | Display warning with `pid`; `--force` may kill |
| no | n/a, manifest says `running` | Crashed worker | Mark `stale`, allow retry |
| no | n/a, manifest says terminal | Idle | Use stored status |

Heartbeat freshness threshold is 30 seconds (6× the write interval),
which tolerates ordinary scheduling jitter without making genuinely
stuck workers invisible for long.

For recovery:

- If a bay has prepare state `running` with no held lock or with
  a stale heartbeat, treat it as `stale` and offer retry.
- If `ready_command` exists and fails at blocked-surface launch time,
  mark the step `stale` and rerun prepare once under `run = "auto"`. A
  second consecutive failure (either the rerun fails, or the new
  `ready_command` still fails after rerun) marks the step `failed` and
  stops; bay does not loop. The user must run `bay prepare --retry`
  to try again.
- If there is no `ready_command`, keep the last successful state.

For ordinary use after a `git pull`:

- Bay does not initially watch arbitrary files such as `vendors.json`.
- The repo's own hooks can still run on pull/merge.
- For prepare steps with `blocks` set, `ready_command` is required, so
  bay always runs it before launching a blocked surface; failure marks
  the prepare step `stale` and reruns prepare under `run = "auto"`.
- For non-blocking prepare steps, `ready_command` is optional. Without
  it, bay can only trust the last successful prepare state until the
  user runs `bay prepare --retry`.

The non-blocking case is a known v1 caveat: a post-pull dependency
change can make a bay stale without bay knowing. Repos that need
stronger guarantees should either keep their own post-pull hooks or
provide a cheap `ready_command`. Blocking prepare steps avoid this
entirely because their ready_command is mandatory.

### Logging

Prepare logs live under bay's data directory:

```
<bay-data>/logs/<dock>/YYYY-MM-DD/prepare.log
```

One file per dock per day. All bays' prepare runs for that dock
on that day go in the same file, distinguished by run separators
written by the worker:

```
==== run bay=w7 step=vendors 2026-05-04T10:23:14 ====
fetched vendor labs at sha abc...
==== finished bay=w7 step=vendors 2026-05-04T10:23:18 status=ready ====
```

The directory layout generalizes to other bay-spawned background
processes (`logs/<dock>/<date>/close-check.log` for Step 3, etc.).

Bay names alone are insufficient to separate uses because IDs are
reused — closing `w7` and creating a new bay can reassign the same
ID. The run separator is the unit of record, not the filename.

`bay prepare --log [bay]` reads today's `prepare.log`, filtering by
bay if specified (grep by `bay=<id>`). `--log -f` for an in-flight
worker tails from a `run_log_offset` recorded in the manifest entry
when the worker writes its run-start separator.

#### Retention

Logs age out by directory:

- Monitor runs a daily prune that walks `logs/*/`, parses
  `YYYY-MM-DD` from each subdirectory name, and `rm -rf`'s any
  directory more than 14 days old. Throttled to once per 23h via a
  `last-prune-at` sentinel so concurrent monitor invocations don't
  re-walk.
- `bay dock close <name>` removes `logs/<name>/` entirely.
- `bay close <bay>` does not touch logs. The bay's history persists
  in the dock's day files until age-pruning catches up.

### User-visible commands

Proposed commands:

```sh
bay prepare              # show current bay prepare status
bay prepare --retry      # retry failed/stale steps
bay prepare --wait       # block until prepare finishes
bay prepare --log        # show or tail prepare logs
bay prepare --kill       # SIGTERM running prepare worker(s)
```

`--kill` reads the worker `pid` from the manifest and sends `SIGTERM`,
the same mechanism `bay close` uses to stop a worker before
teardown. The worker exits, releases its lock, and bay marks the step
`stale`. Re-running prepare is a single `bay prepare --retry` away.

List and picker output should show a compact status:

```text
w7  vendor-fix  setup=running:vendors
w8  auth-fix    setup=failed:vendors
```

`bay show` can include the full step status and log path.

### Failure behavior

Prepare failure does not delete the bay. The bay is still
valuable for debugging setup.

CLI output should include:

```text
bay vendor-fix setup failed: vendors
log: ~/.local/share/bay/logs/<dock>/2026-05-04/prepare.log
retry: bay prepare --retry
```

If a blocked agent was requested, bay should leave the placeholder
pane in place with the failure summary and retry instructions, rather
than launching the agent into a broken checkout. Re-issuing the agent
request while the placeholder is visible should be a no-op (or a
prompt to retry) rather than spawning a duplicate.

### Closing during prepare

Closing a bay while prepare is running should be supported.

For `bay close`, bay first asks its own prepare worker to stop:

1. Read the worker `pid` from the manifest and send `SIGTERM`.
2. Wait up to 5 seconds for the worker to exit (lock released +
   manifest reaches a terminal status).
3. If the worker exits, continue normal close.
4. If it does not exit within the timeout:
   - Without `--force`: refuse close, print the worker `pid` and log
     path, and suggest `bay close --force`.
   - With `--force`: send `SIGKILL`, wait briefly for lock release,
     then continue close. Bay marks the step `stale` since the worker
     did not write a terminal status.

This cancellation happens before dirty/unpushed checks, because a
running prepare job may still be mutating ignored files in the worktree.

### Interaction with window-first progress

Window-first progress and bay prepare solve different problems.

Window-first progress is still useful if `git worktree add` itself is
slow because a repo uses synchronous git hooks. In that case bay opens
a placeholder tmux window and tails setup output while the synchronous
operation runs.

Bay prepare is better for expensive work that can run after the
worktree exists. Repos that want snappy bay bay creation should
move expensive setup out of `post-checkout` and into bay-visible
prepare commands.

## Close checks

Close checks are a separate but related feature. They answer: "Is it
safe for bay to close this bay?"

They are useful when a repo has external state bay cannot infer. For
example, a Loom instance might be bound to a worktree. Removing that
worktree could leave the instance pointing at a missing path.

### Configuration

```toml
[[docks.loom.bay_close_check]]
name = "loom-instance-binding"
command = [".ops/bin/loom", "close-check"]
force = "allowed"
```

Fields:

| Field | Purpose |
|---|---|
| `name` | Stable display/log key, unique within the dock. |
| `command` | Repo-owned argv command run from the bay path. |
| `force` | Whether `bay close --force` may bypass a blocked result. Values: `allowed`, `denied`. |

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

For `bay close`:

1. Stop any active prepare worker for the bay.
2. Run today's cheap built-in dirty/unpushed safety gates.
3. Run bay close checks only if the bay is otherwise
   closeable.
4. If any check blocks, refuse close and print its output.
5. `--force` bypasses checks only when the check config says force is
   allowed. Failed check processes are also bypassable with `--force`
   because bay received no valid domain-specific rule.

For `bay dock close`:

- v1 should probably not add dock-level close checks.
- If needed later, dock close can run each bay close check and
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

1. Hide/remove the bay from normal navigation immediately.
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
BAY_DOCK_STORE=...
BAY_STORE=...
```

Two scopes, matching the post-dock-checkout-merge entity model
(every dock owns one checkout 1:1, so there is no separate "repo"
tier above dock):

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

Bay should not fetch or update that mirror itself. It only provides a
stable storage root if repo scripts need one.

### Stable IDs

Today's manifest uses names:

- Dock names are globally unique.
- Bay names are unique within a dock.
- A bay is effectively identified by `dock:bay`.

Scoped storage should not use renameable display names as durable
directory names. If scoped storage ships, bay should first add opaque
manifest IDs:

```json
{
  "docks": [{ "id": "d_...", "name": "loom" }],
  "bays": [{ "id": "w_...", "name": "vendor-fix" }]
}
```

Storage paths would use IDs, while metadata files inside the store can
record current human names for inspection.

## Manifest shape

Bay prepare state should be persisted so status survives CLI
invocations and recovery. The manifest gains a top-level
`schema_version` bump, each bay gains a `prepare` array, and each
dock gains a `trust_prompt_dismissed` flag used by `bay setup`:

```json
{
  "schema_version": 7,
  "docks": [
    {
      "name": "loom",
      "trust_prompt_dismissed": true,
      "bays": [
        {
          "id": "w7",
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
              "run_log_offset": 0
            }
          ]
        }
      ]
    }
  ]
}
```

`trust_prompt_dismissed` is set when the user answers "Skip" in
`bay setup`'s trust prompt for this dock. It is cleared by
`bay setup --reset-trust-prompts`. It has no effect on bay's runtime
behavior — only on whether `bay setup` re-asks. Trust itself lives
in user config (`[docks.<name>] trust_repo_bay_toml`); this flag is
purely UI state about whether the user wants to be re-prompted.

The `run_log_offset` records the byte offset of the current run's
start separator in today's `prepare.log` when the worker writes it.
`bay prepare --log -f` seeks to this offset to follow only the
in-flight run rather than the whole day's file. Cleared (set to 0)
on terminal status. Log filenames are not stored in the manifest;
they are computed from the current date, which avoids cross-midnight
ambiguity and removes the need to invalidate paths on rename.

Schema-version handling:

- Bay's manifest is at `CurrentVersion = 6` today (post
  dock-checkout-merge). This change bumps to **v7** with a
  sequential `migrateV6ToV7` that adds `prepare: []` to every
  existing bay and leaves `trust_prompt_dismissed` unset on every
  existing dock. Migration is purely additive — no existing fields
  are removed or reshaped.
- Old bay reading a new manifest: bay does not aggressively rewrite
  manifests it does not understand. Single-user version skew is
  expected to be brief in practice; a stronger compatibility story
  can come if bay grows multi-user use.

Statuses:

| Status | Meaning |
|---|---|
| `pending` | Step has not run yet. |
| `running` | Step is currently running. |
| `ready` | Last run succeeded. |
| `failed` | Last run failed. |
| `stale` | Step was running when bay exited, heartbeat expired, definition changed, or readiness check failed. |

Legal transitions:

| From | To | Trigger |
|---|---|---|
| `pending` | `running` | Worker starts (initial run, or after retry from `failed`/`stale`) |
| `running` | `ready` | Worker writes terminal status after command exit 0 |
| `running` | `failed` | Worker writes terminal status after command exit non-zero or timeout |
| `running` | `stale` | Lock not held / heartbeat expired (worker crashed or was killed) |
| `ready` | `stale` | `definition_hash` mismatch on next access, or `ready_command` fails |
| `ready` | `running` | Auto-rerun after `ready_command` failure, or explicit retry |
| `failed` | `running` | Explicit `bay prepare --retry` |
| `stale` | `running` | Auto-rerun under `run = "auto"`, or explicit retry |

Worker death between command exit and the manifest write is benign:
the lock is released, the manifest still says `running`, and the next
bay invocation observes "lock not held + status running" and marks
`stale`. The atomic write (tmp + rename) means the manifest is never
left half-updated.

The status model stays per-step. Avoid a single bay-level status
enum; bay already has other bay state such as dirty, pending,
merged, waiting, and missing.

`definition_hash` is sha256 over a canonical JSON encoding of
`{command, ready_command, blocks, timeout}` for the step. It is
computed at config load time and stored in the manifest at each
successful run. On every status check, bay compares the current config
hash to the stored hash; mismatch marks the step `stale`. Environment
variables and unrelated dock config do not affect the hash, so an
unrelated dock edit does not invalidate prepare state.

## Security and trust

Prepare and close-check commands execute repo-defined code. This is
not a new trust category for bay users who already run tests, hooks,
and agent commands in the repo, but bay should still avoid surprising
execution.

Trust posture summary for v1:

- All prepare config is opt-in. Dock-level config is explicit; repo-
  local `.bay.toml` is gated by hierarchical `trust_repo_bay_toml`
  with a default of `false` at every level. See "Repo-local config
  and trust."
- **Trust is the gate.** Once trust is granted at any level, prepare
  runs in the background like every other build tool. There is no
  per-run confirmation, no countdown, and no claim that runtime
  visibility is a security safety net. Bay creation is often
  invoked from tmux keybindings or other non-TTY paths where any
  printout would be invisible anyway, so designing visibility as a
  security feature would mislead users about the protection it
  actually provides.
- Operability features stand on their own merits, separate from
  trust: a TTY dispatch summary, the placeholder pane that tails the
  prepare log when a surface is blocked, `bay prepare --log`, and
  `bay prepare --kill` exist to help the user inspect or stop
  work, not to gate it.
- Once trust is granted, future updates to `.bay.toml` propagate
  automatically. This matches the existing posture: tests, hooks,
  agents, and build scripts all already run repo-defined code under
  the user's credentials, so subsequent `.bay.toml` changes are no
  broader than the existing grant.
- Commands are argv arrays. Bay does not interpret shell syntax in
  lifecycle config. This avoids quoting problems with paths and keeps
  template variables out of the v1 contract:

  ```toml
  command = [".ops/bin/fetch-vendor.ts", "labs"]
  ```

### The PR review caveat

The sharpest edge is **checking out an untrusted branch into a
worktree.** If the dock has trust granted, and a PR (or a fork's
branch) modifies `.bay.toml`, the next `bay new` against that
branch will run the modified version. Same mechanism as if the PR
modified `Makefile` or a test file, but easier to miss because
prepare runs in the background.

Mitigations available to the user:

- For one-off review of an untrusted branch, set
  `trust_repo_bay_toml = false` on the dock used for review, or use
  a separate dock with trust off.
- For ongoing work in repos with active untrusted contributors,
  prefer per-dock trust over a global bay-level grant so each
  checkout opts in individually.
- When invoked from a TTY (not a tmux keybinding), the dispatch
  summary names the prepare commands about to run. This is
  best-effort visibility, not a security gate, but it can catch a
  one-off "wait, what?" before the worker has done much.

Bay should document this caveat in the user guide so it is not
surprising. It does not need a code-level mitigation in v1.

## Open questions

1. **Future foreground mode.** V1 should run prepare in the background.
   Is there a real repo that needs `mode = "foreground"` later, where
   setup must complete before any surface appears?

2. **Existing git hooks.** Should bay advise users to move expensive
   `post-checkout` work into prepare hooks, or support both equally?
   The pragmatic answer is support both, but only prepare hooks can
   keep `bay new` truly snappy.

3. **Close-check config home.** Bay prepare config now lives in
   both repo-local `.bay.toml` and dock-level overrides. Should close
   checks follow the same model when they ship in Step 3? Likely yes,
   but defer the decision until close checks are actually built.

## Incremental path

### Step 1: Bay prepare status [MEDIUM-LARGE]

Add bay prepare commands sourced from layered repo-local and
dock-level config, hierarchical `trust_repo_bay_toml` gating for
repo-local config (including `bay setup`'s trust-prompt iteration with
manifest-tracked dismissal flag and `--reset-trust-prompts`), trust
flags on `bay dock new` and `bay new` for first-run flows,
persisted per-step status, logs, retry/wait/log/kill commands, blocked
agent/cmd launch with placeholder pane, and close-time prepare-worker
termination.

This is the smallest feature that directly supports Loom's vendored
setup without bay understanding Loom, but it is not a small patch: it
touches config parsing, manifest schema, process ownership, logs,
surface launch, recovery, close flow, `bay setup` interactive flow,
and formatting.

These pieces should ship together rather than as separate slices:

- Shipping the runner without blocked-surface launch leaves agents
  free to spawn into a half-prepared bay, which is the exact
  bug prepare is meant to prevent.
- Shipping without close-time worker termination leaves a race where
  bay can begin removing a worktree while a prepare worker is still
  writing to it.
- Shipping without repo-local config makes the feature a usability
  dead end: every user on every machine has to remember the right
  command paths and ready checks for every repo, and they will get
  it wrong.

The runner, blocked surfaces, close handling, and layered config with
trust are mutually load-bearing for the feature's value, so the unit
of work for v1 is all four.

The TTY dispatch summary and `bay prepare --log` / `--kill`
commands are operability features that ship alongside but are not
load-bearing for the trust posture.

### Step 2: Window-first progress [MEDIUM]

Implement the existing progress-feedback design for slow synchronous
`bay new` operations. This can happen before or after Step 1, but it
solves a different part of the UX.

### Step 3: Close checks [SMALL-MEDIUM]

Add bay close checks only when a real repo has an external state
blocker that bay cannot see. Require checks to provide a useful
message and remediation.

### Step 4: Scoped storage [DEFER]

Do not build this until repo scripts need a bay-owned storage root.
If it ships, add stable opaque IDs first and expose store paths through
environment variables and `bay store path`.
