# Per-account agent launching — design plan

Captured 2026-08-31. No code has been written. This doc specifies how bay
selects which Claude account an agent launches against — a capability bay
does not have today in any form.

Related:
- `agent-addressing.md` — accounts shard the Claude session registry,
  which is why that doc's directory must scan every configured account
  dir and why peers in different accounts cannot discover each other.
  This doc owns the mechanism; that one owns the consequence.
- `model-profiles.md` — the `extends` profile machinery, and the
  clients × models matrix it set out to avoid. The argument below against
  making accounts profiles is that argument with one more dimension.
- `scoped-storage.md` / `prepare.md` — the existing `BAY_*` environment
  conventions for repo-defined work, adjacent to the `env` support added
  here but deliberately separate: this is env bay sets *for the agent*,
  not env bay exports to hooks.

## The problem

Bay cannot launch an agent against a chosen account. Two things are
missing, and the second is the reason the first has never come up:

- `AgentConfig` has no way to set environment (`config.go:37`). Its
  fields are `Command`, `Args`, `LaunchArgs`, `ResumeArgs`,
  `ProjectFile`, `Extends`, `Disabled` — all command-line shape, no
  process environment.
- The launch path passes only a command string. `buildAgentCommand`
  (`engine/agent.go:62`) returns a line, and `RespawnPane`
  (`engine.go:285`) hands it to tmux. There is nowhere for an env var to
  enter.

So this is a missing bay capability, not a configuration gap a user can
work around — short of wrapping the agent binary in a shell script per
account, which defeats the built-in agent registry.

## The mechanism

Claude Code reads `CLAUDE_CONFIG_DIR` (verified present in 2.1.251). A
config dir carries its own credentials, so a distinct dir is a distinct
account. There is no `--account` flag. `--settings <file-or-json>` exists
but layers settings onto the *current* credentials — it is not an account
switch.

*Measured 2026-08-31:* a session launched with `CLAUDE_CONFIG_DIR`
pointed at an empty dir created `<dir>/sessions/` and registered itself
there, leaving the default registry untouched. So the session registry
follows the config dir, and **each account has its own registry**. That
fact is what couples this doc to `agent-addressing.md`; the consequences
for discovery live there.

## Config surface

Accounts are an orthogonal axis, not agent profiles:

```toml
[accounts.work]
config_dir = "~/.claude-work"

[docks.labs]
account = "work"          # default for every bay in this dock
```

with `--account` on `bay new` / `bay agent` for the exception.
`DockConfig` (`config.go:366`) already carries `Agent` and `AgentArgs`,
so `Account` sits beside them with no new precedent. Values run through
`config.ExpandPath`, so `~` works.

**Why not `extends` profiles.** The obvious move is
`[agents.work] extends = "claude", env = {...}`, and it holds up until
you want a model pin too: `resolveExtendsBase` (`config.go:275`) rejects
a base that itself extends, so `work-opus extends work extends claude`
does not resolve. Restating the pin on every account profile rebuilds
exactly the matrix `model-profiles.md` set out to avoid, now with an
accounts dimension. As an orthogonal axis, an account composes with any
agent and any model profile, and the dock-level default matches how this
is actually used — this dock is the work repo, so it uses the work
account.

**The general escape hatch.** An `env` map on `AgentConfig` covers
everything an account isn't:

```toml
[agents.claude]
env = { SOME_VAR = "value" }
```

Accounts are then a named, validated, dock-defaultable shorthand for the
one env var that matters most, not the only way to set env.

## Resolution order

Highest first:

1. `--account` passed to the command
2. `Bay.Account` stored in the manifest
3. the parent dock's configured `account`
4. nothing — no env injected; the agent uses whatever `~/.claude` is
   logged into

## The account is stored on the bay

A new optional `Bay.Account` field (additive, `omitempty`, no manifest
version bump needed) records what the bay was created against.

**The resume paths must read it.** `recover.go:417` and undo-close
rebuild the agent command long after creation, and a recovered agent that
silently reattaches to the *default* account is the worst failure this
design can produce: the session comes back looking correct and is wrong
underneath, spending the wrong account's quota until someone notices.

This is the same shape as two other bugs in this area — `LaunchArgs`
dropped on resume (`engine/agent.go:72`, deliberate and correct for
`--model`), and a session name dropped on resume (`agent-addressing.md`,
Part 1). Three instances make it a mechanism rather than a coincidence:
**anything that is identity rather than launch preference must be
recomputed on every invocation, including resume.** Worth encoding as a
test that resumes a bay and asserts the rebuilt command still carries
account and name.

## Env injection

Two launch paths, two mechanisms:

- **`RespawnPane`** (`engine.go:285`, fresh launch): tmux supports
  `-e VAR=value` on `respawn-pane` and `new-window` (confirmed on tmux
  3.7b). Extend the `tmux.Interface` signatures rather than prefixing the
  command string — it keeps shell quoting out of the picture and leaves
  the env visible to tmux itself.
- **`SendKeys`** (`recover.go:421`, resume types into a live shell): no
  `-e` available, so the env prefix is written into the typed line. Reuse
  `quoteArgs` (`engine/agent.go:88`) for the values.

The mock in `internal/tmux/mock.go` needs the same signature change, and
the integration suite under `internal/tmux/integration_test.go` is the
right place to pin `-e` behavior against real tmux, since that suite
exists precisely to keep the mock honest.

## Reaching it from a keybinding

Selecting an account has to be as fast as selecting a model, or the
default account wins by inertia. Three surfaces, in the order they
matter:

- **Keybindings.** The `bay agent` keybinding family already launches an
  agent into the current bay; an account-qualified variant
  (`bay agent --account personal`) is the alternate-login shortcut.
  Follows the same shape `model-profiles.md` used for model pins, and
  runs into the same objection — a bindings table that is the product of
  clients × models × accounts. Bind only the accounts actually in use,
  which for most people is one alternate.
- **The palette.** `bay palette` is the right home for the full cross
  product, since it costs no keys.
- **`bay setup`.** Where a second account gets registered, if question 1
  below resolves toward bay owning creation.

## Validation and visibility

- An `account` naming no `[accounts.*]` entry is a config error, caught
  where `Validate` already checks `DefaultAgent` (`config.go:538`).
- A missing or unreadable `config_dir` fails at launch with the account
  name in the message. It must not silently fall back — a silent fallback
  spends the wrong account's quota, which is the failure users will find
  hardest to diagnose.
- `bay show` and `bay ls --json` carry the resolved account, so "which
  account is this bay on" never requires reading config.

## Documentation

- `docs/human-guide.md` — the `[accounts]` table, the dock-level default,
  and `--account` on `bay new` (new config options and new CLI flags both
  land here per the docs rules).
- `internal/cli/agent-guide.md` — the `account` field in `bay ls --json`,
  and the note that an agent cannot reach a peer on another account.
- `docs/tutorial.md` — untouched. Multi-account is not a first-run
  concern.

## Staging

1. `env` on `AgentConfig` + injection on both launch paths. Independently
   useful and the foundation for everything else here.
2. The `[accounts]` table, `DockConfig.Account`, `--account`, and
   validation.
3. `Bay.Account` + the resume paths + the resume test.
4. Visibility in `bay show` / `bay ls --json`, and the docs.

## Open questions

1. Should `bay` help *create* an account — a `bay account add work
   --config-dir ~/.claude-work` that makes the dir and runs the
   interactive login — or is first-time setup a documented manual step?
   Nothing on this machine has a second config dir today, so every user
   of this feature hits that setup step first.
2. Does anything else in bay need to become account-aware? The
   auto-description worker shells out to a summarizer
   (`auto-descriptions.md`) and reads Claude transcripts; if transcripts
   are also config-dir-scoped, it is reading only the default account's.
   Worth checking before accounts ship, since it would fail silently.
