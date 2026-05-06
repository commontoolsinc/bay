# Monitor version status — design plan

Captured 2026-05-06. Implemented 2026-05-06.

## The problem

Bay's monitor already reloads itself when the installed `bay` binary is
replaced: on each tick it compares the executable mtime observed at
startup with the current executable mtime, then `exec`s itself in place
when the file changes. That preserves the PID, which is operationally
nice but confusing when debugging.

The practical symptom is that `bay monitor status` can only say:

```
Monitor running (pid 21755)
```

That does not answer the question the user usually has after upgrading:
"is this monitor actually running the new bay code, or am I looking at
an old daemon?"

Today we can infer the answer with host tools (`lsof`, inode checks,
`go version -m`), but bay itself should provide the evidence.

## Goals

- Make `bay monitor status` directly report whether the running monitor
  is current, stale, or unknown.
- Show the monitor's self-reported bay version/commit, not just the PID.
- Keep the existing self-exec mechanism as the correctness path.
- Make stale/unknown states actionable: the output should tell the user
  when a manual `bay monitor stop && bay monitor start` is warranted.
- Avoid relying on platform-specific process inspection for normal
  status checks.
- Keep the status surface quiet enough for command palette use.

## Non-goals

- No binary self-update. Users still install or upgrade `bay`; the
  monitor only reloads after the installed binary changes.
- No separate daemon supervisor.
- No hard dependency on `ps`, `lsof`, `/proc`, or macOS-specific APIs.
- No remote version check. This design compares the running monitor
  against the `bay` binary the user invoked locally.

## Proposed approach

Add a small monitor-owned runtime metadata file next to the PID file:

```
~/.local/share/bay/monitor-status.json
```

The monitor writes this file when `bay monitor run` starts, including
after self-exec reloads. It then refreshes only a slow heartbeat, not on
every monitor cycle. After the existing self-exec reload path fires, the
new process image starts the same hidden `monitor run` command again,
writes fresh metadata immediately, and preserves the PID. That gives
`bay monitor status` a direct self-report from the running code image
without turning the monitor's normal 3-second polling loop into constant
disk churn.

The PID file remains the authority for "is there a monitor process?"
The status file is evidence about what that process says it is running.

## Status file schema

Initial schema:

```json
{
  "schema": 1,
  "pid": 21755,
  "version": "v0.0.0-20260506043208-edcf5808a4ed (edcf5808)",
  "exe_path": "/Users/mike/go/bin/bay",
  "exe_mtime_unix_nano": 1778042122000000000,
  "exe_size": 6490338,
  "exe_dev": 16777231,
  "exe_ino": 33699330,
  "started_at": "2026-05-06T04:35:00Z",
  "last_seen_at": "2026-05-06T04:35:09Z",
  "generation": 3
}
```

Field notes:

- `version` is the same version string printed by `bay version`, passed
  into the monitor process by the CLI root.
- `exe_path` is `os.Executable()` from inside the monitor process.
- `exe_mtime_unix_nano` and `exe_size` are portable identity fields.
- `exe_dev` and `exe_ino` are best-effort Unix fields. They are useful
  on macOS/Linux but not required for correctness.
- `started_at` is when this monitor PID first started.
- `last_seen_at` is a slow heartbeat, used only as rough health
  evidence. It is not the mechanism that makes version status current.
- `generation` starts at 1 for a freshly forked monitor. If `monitor run`
  starts and finds an existing status file with the same PID, it
  increments `generation`; this makes self-exec reloads visible even
  though the PID did not change.

Writes should be atomic: write a temp file in the data dir, `fsync` if
practical, then rename. Failure to write metadata is non-fatal for the
monitor; status/doctor can warn about it.

## Write cadence

Do not rewrite the status file on every monitor cycle. The version
question is answered by lifecycle writes:

- On `monitor run` startup.
- On self-exec reload, because the execed process re-enters
  `monitor run` and writes fresh metadata before the loop starts.
- On monitor shutdown/stop, by best-effort removal.

For health diagnostics, refresh `last_seen_at` at a fixed slow cadence,
for example every 5 minutes. This is enough to distinguish "the process
exists" from "the monitor loop has not made progress in a long time"
without producing tens of thousands of writes per day.

With the default 3-second monitor interval:

| Heartbeat cadence | Writes per day |
|---|---:|
| Every cycle | 28,800 |
| Every 60 seconds | 1,440 |
| Every 5 minutes | 288 |

Use a monitor-private constant such as:

```go
const runtimeStatusHeartbeat = 5 * time.Minute
```

Do not make this user-configurable in the first pass. It is diagnostic
plumbing, not behavior users should need to tune.

## Freshness model

`bay monitor status` should combine three sources:

1. PID liveness via the existing PID file and signal-0 check.
2. Monitor self-report from `monitor-status.json`.
3. Current CLI identity from the `bay` binary that is running the
   `status` command.

States:

| State | Meaning | User action |
|---|---|---|
| `not running` | No live PID from the PID file. | Run a normal bay command or `bay monitor start`. |
| `current` | Live PID, metadata PID matches, monitor version matches current CLI version, and binary identity is compatible. | None. |
| `stale` | Live PID, metadata exists, but monitor version differs from current CLI version or the monitor reports an older identity for the same executable path. | Wait one interval; if it persists, restart monitor. |
| `unknown` | Live PID, but metadata is missing, malformed, or belongs to another PID. | Restart monitor if it does not recover. |
| `different executable` | Live PID, metadata is fresh, but monitor is running a different `bay` path than the current CLI. | Informational unless versions differ. |

Heartbeat freshness should avoid false positives from the intentionally
slow write cadence. Use a conservative threshold such as
`max(15m, 3 * runtimeStatusHeartbeat)`. A stale heartbeat should not
turn a version/binary match into `unknown`; status should still report
`current` and surface the stale heartbeat as a separate health warning.

## User-facing output

Keep the default status one-line for palette and casual CLI use:

```
Monitor running (pid 21755, current edcf5808, seen 1s ago)
```

Stale:

```
Monitor running (pid 21755, stale: monitor edcf5808, bay 3a18c42d)
```

Unknown:

```
Monitor running (pid 21755, version unknown: metadata missing)
```

Different executable:

```
Monitor running (pid 21755, current edcf5808, different path: /opt/homebrew/bin/bay)
```

Add a verbose mode for debugging:

```
bay monitor status --verbose
```

Example:

```
running: yes
pid: 21755
freshness: current
monitor version: v0.0.0-20260506043208-edcf5808a4ed (edcf5808)
current bay version: v0.0.0-20260506043208-edcf5808a4ed (edcf5808)
monitor executable: /Users/mike/go/bin/bay
current executable: /Users/mike/go/bin/bay
monitor binary: ino=33699330 mtime=2026-05-06T04:35:22Z size=6490338
current binary: ino=33699330 mtime=2026-05-06T04:35:22Z size=6490338
started: 2026-04-26T22:24:36-07:00
last heartbeat: 2026-05-06T04:35:25Z
reload generation: 3
status file: /Users/mike/.local/share/bay/monitor-status.json
```

Also add structured output:

```
bay monitor status --json
```

The JSON output should expose the same assessed state as the human
output, not merely dump `monitor-status.json`. It should include PID
liveness, freshness state, monitor metadata when available, current CLI
identity, and a machine-readable reason for `stale` or `unknown`.
Agents should not need to reimplement the assessment rules.

Example:

```json
{
  "running": true,
  "pid": 21755,
  "freshness": "current",
  "reason": "",
  "monitor": {
    "version": "v0.0.0-20260506043208-edcf5808a4ed (edcf5808)",
    "exe_path": "/Users/mike/go/bin/bay",
    "exe_mtime_unix_nano": 1778042122000000000,
    "exe_size": 6490338,
    "exe_dev": 16777231,
    "exe_ino": 33699330,
    "started_at": "2026-04-27T05:24:36Z",
    "last_seen_at": "2026-05-06T04:35:25Z",
    "generation": 3
  },
  "current": {
    "version": "v0.0.0-20260506043208-edcf5808a4ed (edcf5808)",
    "exe_path": "/Users/mike/go/bin/bay",
    "exe_mtime_unix_nano": 1778042122000000000,
    "exe_size": 6490338,
    "exe_dev": 16777231,
    "exe_ino": 33699330
  }
}
```

## Implementation sketch

1. Add `MonitorStatus` to `internal/config.Paths`.
2. Add `internal/monitor/status.go` with:
   - `RuntimeStatus` struct.
   - `BinaryIdentity` struct.
   - `CurrentBinaryIdentity(path string)`.
   - `ReadRuntimeStatus(path string)`.
   - `WriteRuntimeStatusAtomic(path string, status RuntimeStatus)`.
   - `AssessRuntimeStatus(pid, currentVersion, currentExe, status, interval)`.
3. Extend monitor construction without adding more positional
   constructor arguments. Prefer a setter:

   ```go
   mon.SetRuntimeStatus(statusPath, version)
   ```

   This avoids churning existing monitor tests that do not care about
   runtime metadata.
4. Pass the root CLI version into monitor commands. `newMonitorCmd`,
   `newMonitorWithConfig`, and palette monitor-status plumbing need the
   version string that `main.versionString()` already passes to
   `cli.NewRootCmd(version)`.
5. In `bay monitor run`:
   - Write PID as today.
   - Initialize runtime status before entering `Monitor.Run`.
   - Record binary mtime baseline as today.
   - Refresh `last_seen_at` at `runtimeStatusHeartbeat`, not once per
     monitor loop.
6. In the self-exec path:
   - Keep the current `syscall.Exec(exe, os.Args, os.Environ())`.
   - Let the newly execed process re-enter `monitor run`, observe the
     same PID in the previous status file, and increment `generation`.
7. In `monitor stop`:
   - Remove the PID file as today.
   - Best-effort remove `monitor-status.json`.
8. Update `bay monitor status`:
   - Preserve the existing stopped behavior.
   - When running, read and assess runtime metadata.
   - Default to one-line output.
   - Add `--verbose` for detailed diagnostics.
   - Add `--json` for agents and scripts. It should be mutually
     exclusive with `--verbose`.
9. Update `bay doctor` to warn on `stale` or long-lived `unknown`
   monitor metadata, while keeping missing metadata from older monitors
   actionable rather than fatal.

## Compatibility

Older monitors will not write `monitor-status.json`. After this feature
lands, a newly installed `bay monitor status` may therefore report:

```
Monitor running (pid 21755, version unknown: metadata missing)
```

That is the correct behavior. It means bay cannot prove what code image
the existing monitor is running. If the old monitor already includes the
self-exec change, it should reload on the next installed-binary mtime
change and begin writing metadata. If it predates self-exec, a manual
restart is required.

This compatibility story is why the status should say `unknown`, not
`stale`, when metadata is absent.

## Tests

Add focused unit tests for:

- Runtime status atomic write/read round trip.
- Fresh metadata with matching version reports `current`.
- Live PID with missing metadata reports `unknown`.
- Metadata for another PID reports `unknown`.
- Version mismatch reports `stale`.
- Same version but changed same-path binary identity reports `stale`.
- Different executable path with matching version reports
  `different executable`, not `stale`.
- Same PID re-entering `monitor run` increments `generation`.
- `monitor status` one-line output for current, stale, unknown, and not
  running states.
- `monitor status --json` output for current, stale, unknown, and not
  running states.
- `monitor stop` best-effort removes status metadata.

## Decisions

- `bay monitor status --json` should ship in the first implementation.
- `generation` should not be shown in default one-line output. It
  belongs in `--verbose` and `--json`.
- Old missing-metadata monitors should not trigger an automatic
  stop/start. Status should surface the uncertainty; monitor lifecycle
  changes should remain explicit unless they are part of the existing
  self-exec path.
