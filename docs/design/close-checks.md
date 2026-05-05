# Close checks - design plan

Captured 2026-04-26. **Deferred.** Not slated for implementation
until a real repo needs a domain-specific close gate. This doc
preserves the design so the work has a starting point if/when that
day comes.

Sibling of `prepare.md`. Close checks reuse prepare's trust model,
config layering (repo-local `.bay.toml` plus dock-level overrides),
and worker/log infrastructure. Read `prepare.md` first; this doc
describes only the close-side additions.

## What close checks are

Close checks are repo-defined commands that run when the user closes
a bay. They answer: "Is it safe for bay to close this bay?"

They are useful when a repo has external state bay cannot infer. For
example, a Loom instance might be bound to a worktree. Removing that
worktree could leave the instance pointing at a missing path.

## Configuration

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

Same trust gate as prepare (`trust_repo_bay_toml`). Same per-field
override layering between repo-local `.bay.toml` and dock-level
config.

## Contract

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
log: ~/.local/share/bay/logs/<dock>/2026-05-04/close-check.log
use --force to bypass
```

This is a different case from a successful check that returns
blocked. A failed check process means bay did not get domain guidance
and can be bypassed with `--force`; a successful blocked result
follows the check's configured `force` policy.

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
failure every time and keep the log path visible so a flaky check
does not silently weaken close safety.

## Bay behavior

For `bay close`:

1. Stop any active prepare worker for the bay.
2. Run today's cheap built-in dirty/unpushed safety gates.
3. Run bay close checks only if the bay is otherwise closeable.
4. If any check blocks, refuse close and print its output.
5. `--force` bypasses checks only when the check config says force
   is allowed. Failed check processes are also bypassable with
   `--force` because bay received no valid domain-specific rule.

For `bay dock close`:

- v1 should probably not add dock-level close checks.
- If needed later, dock close can run each bay close check and then
  an optional dock close check.

Bay should not run remediation actions automatically. The commands
in the check output are instructions for the user, not implicit
fixes. V1 prints them only; interactive selection is future work.

## Logging

Reuses the prepare logging layout with a different category:

```
<bay-data>/logs/<dock>/YYYY-MM-DD/close-check.log
```

Run separators identify the bay and check by name, same convention
as prepare. Monitor's daily prune and `bay dock close` cleanup cover
this category automatically.

## Teardown hooks (also deferred)

Do not add teardown hooks in v1, even when close checks ship.

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
