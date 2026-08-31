# Addressing agents by bay — design plan

Captured 2026-08-31. No code has been written. This doc proposes making
"talk to the agent in `labs:b8`" a reliable operation, by having bay name
the sessions it launches and publish a directory joining bays to live
agent sessions.

Related:
- `agent-accounts.md` — per-account agent launching. That doc owns the
  mechanism (an account is a `CLAUDE_CONFIG_DIR`); this one owns the
  consequence, which is that accounts shard the session registry and so
  bound what any agent can discover. Build accounts first.
- `workspace-identity.md` — ID / Name / Description. Addressing keys on
  **ID**, never Name, so a rename can't change an agent's address.
- `session-identity.md` — "is this session mine?" for tmux sessions.
  The same liveness problem one layer up.
- `auto-descriptions.md` — already reads per-bay Claude and Codex
  transcripts; the same "which agent runs in which bay" join.

## The problem

Claude Code sessions can message each other, and each session publishes
itself to a registry at `<config-dir>/sessions/<pid>.json`:

```json
{"pid":71761,"sessionId":"dfc0d98f-…","cwd":"/Users/mike/projects/labs-worktrees/b4",
 "tmux":"labs:@53.%83","messagingSocketPath":"/tmp/cc-socks/71761.sock",
 "name":"b4-01","nameSource":"derived","status":"busy"}
```

Peers are addressed by `name`. When bay launches the session, that name
is *derived* — Claude guesses it from the working directory basename and
adds a disambiguating suffix, producing `b4-01`, `b11-b8`, and similar.
The result is bay-shaped but wrong in three ways:

- **Not dock-qualified.** Every dock has a `b4`. Two bays in different
  docks produce names differing only by an opaque suffix, so an agent
  choosing between them is guessing.
- **Not stable.** The suffix is assigned per process. The same bay gets a
  different address after a restart or `bay recover`.
- **Not discoverable.** Nothing tells an agent that `b4-01` *is* a bay,
  which dock it belongs to, or how to get from "the bay I want" to a name
  the messaging layer accepts.

Bay holds the authoritative side of this mapping and doesn't publish it.
The manifest records each surface's tmux pane and window
(`internal/manifest/manifest.go:422`), dock name is the tmux session name
(`manifest.go:127`), and bay builds the launch command itself
(`internal/engine/agent.go:62`).

**Codex is a different situation.** Its binary carries
`receiver_agent_nickname` / `receiver_thread_id`, `spawnAgent` /
`sendInput` / `resumeAgent`, and a `thread/name/set` RPC — nicknames
within an app-server spawn hierarchy, not a registry of independently
launched sessions. And *measured 2026-08-31:* a codex TUI sitting at its
prompt publishes **nothing** — no registry entry, no socket, no file
under `~/.codex` until a thread starts. There is no codex analogue to
join against, so the premise that codex agents can address
separately-launched peers is unconfirmed and the evidence points against
it. Part 1 is Claude-only by construction; Part 2 covers codex by
reporting what bay itself knows.

## The model

Two mechanisms, in priority order.

**Name at launch.** For sessions bay starts, bay supplies the name, so
the address *is* the bay coordinate. This fixes the majority case at the
source and needs no new runtime machinery.

**Join at query time.** For everything else — sessions started by hand in
a bay's shell surface, sessions predating this feature, agents with no
name flag — bay reconstructs the mapping by joining its manifest against
the live registry on tmux pane identity. Exact, not fuzzy: bay knows the
pane, and the registry publishes it.

Naming is the fix; the directory is the fallback that makes the fix
optional rather than load-bearing.

## Part 1 — Name the session at launch

Bay appends a name flag to the agent command for agents that support one.

**Address format.** `<dock>:<bayID>` — `labs:b8`. This is bay's existing
dock-qualified vocabulary, the same string `ResolveBay`
(`manifest.go:1032`) already parses, so an address read out of `bay ls`
feeds straight back into any bay command. Keying on `Bay.ID` rather than
`Bay.Name` means `bay rename` cannot change an agent's address. A bay
with more than one agent surface disambiguates with the surface name:
`labs:b8/review`; the first agent surface keeps the bare form.

*Measured 2026-08-31:* `--name` accepts `:`, `/`, and `.`, storing each
verbatim in the registry, so both forms work as written. No separator
fallback is needed.

**Config.** A new optional field on `AgentInfo` / `AgentConfig`
(`internal/config/config.go:37`):

```toml
[agents.claude]
name_flag = "--name"     # built-in default for claude; unset for codex
```

Unset means bay adds nothing — correct for codex and for custom agents.

**Placement in the command.** The name goes in `Args`, not `LaunchArgs`.
`buildAgentCommand` drops launch args on resume (`agent.go:72`) so
recovery doesn't replay session-start flags like `--model`. A name
dropped on resume would restore the derived-name failure after every
`bay recover`. The name is identity, not a launch preference, and belongs
on every invocation — the same rule `agent-accounts.md` applies to
`Bay.Account`, and for the same reason.

**Sites to change.**
- `internal/config/config.go` — the field, its merge behavior in
  `resolveExtendedAgent` (profiles inherit the base's flag), and the
  `claude` entry in `KnownAgents` (`config.go:68`).
- `internal/engine/agent.go:62` — `buildAgentCommand` gains the bay
  coordinate and emits `<name_flag> <address>`. Both callers
  (`engine.go:281`, `recover.go:417`) already hold the bay.

**Verification.** The registry records `nameSource`. A bay-named session
reads `user`; a session Claude named for itself reads `derived`. So
`bay doctor` can report sessions that fell back without guessing. (The
binary also contains the string `explicit`, but a `--name` launch
measurably writes `user`.)

## Part 2 — The directory

Two commands over one join.

```
bay whois <bay>        # address for the agent in a bay
bay whois --self       # what other agents call me
bay agents [--json]    # full directory
```

**The join.** For each registry entry: parse `tmux`
(`session:@win.%pane`) and match `%pane` against `Surface.Tmux.PaneID` —
exact, because bay assigned the pane. Fall back to `cwd` under `Bay.Path`
when the tmux field is absent, which resolves a bay but not a surface.
Drop entries whose pid is dead; a session removes its own entry on clean
exit (measured), so dead entries only come from crashes. `status` and
`updatedAt` come from the registry, so liveness needs no further probing.

**Which registries to scan.** The default config dir plus every dir
configured by `agent-accounts.md`, deduplicated by pid. Because the
registry follows `CLAUDE_CONFIG_DIR`, **agents in different accounts
cannot discover each other at all** — and bay, scanning every dir it
configured, is the only component on the machine with the cross-account
view. That makes this directory strictly more capable than any agent's
native peer list rather than a convenience wrapper over it.

**Cross-account peers are reported, never bridged.** `bay whois` lists a
peer found in another account's registry with `reachable: false` and the
reason, rather than omitting it — a silent omission looks identical to
"no agent there", the failure this design exists to remove. Bay does not
attempt delivery itself; relaying through `tmux send-keys` would
contradict the non-goal below and trade a truthful answer for a racy
channel. If cross-account delivery turns out to work natively, the only
change is `reachable` becoming true.

```
$ bay whois labs:b8
address   labs:b8
agent     claude
account   work
status    idle
reachable false — peer is in account 'work', you are in 'personal';
          registries do not overlap
```

**Codex rows come from the manifest.** With nothing published to join
against, a codex surface is reported from what bay already knows — it
launched the pane — carrying `reachable: false` and reason
`no peer channel`. The directory stays a complete picture of what is
running where, which is its purpose, without pretending codex has an
address. Revisit when codex's `use_agent_identity` leaves development;
driving `app-server` / `remote-control` to enumerate and name threads is
possible but is far larger scope on an explicitly experimental surface,
and is out of scope here.

**JSON shape**, mirroring `bay ls --json` conventions:

```json
[
  {
    "dock": "labs",
    "bay_id": "b8",
    "surface": "agent",
    "address": "labs:b8",
    "agent": "claude",
    "name_source": "user",
    "session_id": "dfc0d98f-…",
    "status": "busy",
    "pane": "%83",
    "account": "work",
    "reachable": true
  }
]
```

`name_source` tells an agent whether the address is bay-assigned or a
guess. A `false` `reachable` is always accompanied by `reachable_reason`
(`different account: work`, `no peer channel`), since an unexplained
false sends the caller back to guessing.

## Part 3 — Make it discoverable

The mapping is useless to an agent that doesn't know it exists.

- `internal/cli/agent-guide.md` — an "Addressing other agents" section:
  how to get from a bay to an address, the rule that addresses come from
  `bay whois` rather than pattern-matching a peer list, and the note that
  a `reachable: false` peer should be reported to the human rather than
  retried, because no retry crosses an account boundary.
- A `peer` field on `bay ls --json`, so an agent already listing bays
  gets the address without a second call — the most common path by far.
- `docs/human-guide.md` — the two commands and `name_flag`.

## Non-goals

- **Bay does not send messages.** Each agent has a native channel; bay
  resolves the address and gets out of the way. `tmux send-keys` as a
  delivery path is racy and bypasses the real channel.
- **Bay does not write to `<config-dir>/sessions/`.** That file is
  Claude-owned and undocumented. Read it; never author it.

## Staging

Accounts (`agent-accounts.md`) land first — they decide which registry a
session publishes to, and they are the only part of this work that makes
a currently impossible thing possible.

1. **`name_flag` + naming** (Part 1). Self-contained, fixes the majority
   of addressing failures, no new commands.
2. **Agent guide + `peer` in `bay ls --json`** (Part 3). Small, and
   without it agents won't use step 1.
3. **`bay whois` / `bay agents`** (Part 2). The fallback for sessions bay
   didn't launch, and where the cross-account reporting lands.

## Open questions

1. Can a session in one account *deliver* to a peer in another, given the
   address? Discovery is known not to cross. Non-blocking: the reporting
   behavior is decided either way, and a yes only flips `reachable` to
   true. Needs two authenticated config dirs, a live session in each, and
   an attempted send.
2. Can two separately launched codex TUIs address each other at all? The
   evidence says probably not. Decided not to block on it: codex is
   report-only, and this is a revisit trigger, not a prerequisite. Watch
   codex's `use_agent_identity` flag.

Resolved 2026-08-31:
- `--name` accepts `:` / `/` / `.` verbatim, so the separator is `:`.
- The session registry follows `CLAUDE_CONFIG_DIR`, so accounts shard it
  and the directory scans every configured dir.
- Peer discovery therefore never crosses accounts.
- A codex TUI publishes no session state at launch, so there is nothing
  to join against and bay's manifest is the only record.
