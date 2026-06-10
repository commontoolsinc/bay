# Model profiles — design plan

Captured 2026-06-10; steps 1 and 2 implemented in the same PR
(step 3, the picker chord, remains deferred). The design emerged
from a discussion about launching Claude with different models: the
user thinks of running "a fable agent" or "a haiku agent" or "an
opus agent," not just "a claude agent," and wants to choose at
launch time — without complicating the common case where the model
doesn't matter, and without the keybinding table exploding into a
clients × models matrix.

## The problem

Bay launches agents (claude, codex, antigravity) but has no notion
of which *model* an agent runs. Today:

- A bare `claude` launch starts with whatever model was last
  selected via `/model` in *any* session — Claude Code persists
  that choice globally. Fine when you don't care; wrong when you
  specifically want a fable bay next to an opus bay.
- The only way to pin a model through bay is to bake `--model X`
  into `[agents.claude] args` or per-dock `agent_args` — which pins
  *every* claude launch, and (worse, see below) re-pins on resume.
- There's no vocabulary for a model-pinned launch: nothing to put
  in `--agent=`, nothing to bind a key to, nothing for a dock
  default.

Three questions to answer:

1. **Launch:** how do you say which model a new session runs?
2. **Relaunch** (recover, undo-close): which model does a restored
   session use — the one it launched with, or the one the user
   last selected inside it?
3. **Keybindings:** how does richer control avoid multiplying the
   per-client launch chords?

And one constraint: simple stays simple. A user who never thinks
about models must see zero new ceremony and zero behavior change.

---

## Ground truth: what Claude Code already does

These facts shape the design (verified against the official docs,
code.claude.com/docs/en/model-config, 2026-06):

1. **`/model` writes the global default.** Since v2.1.153,
   selecting a model via `/model` saves it to
   `~/.claude/settings.json`. New sessions read it at startup;
   already-running sessions are unaffected. This explains the
   observed "last selected in any session wins" behavior.
2. **Resumed sessions keep their own model.** `claude --continue`
   / `--resume` restores the model the session was last using,
   regardless of the current global default. (If that model has
   been retired, it falls through to the normal precedence.)
3. **`--model` overrides everything**, including on resume:
   `claude --continue --model sonnet` forces sonnet even if the
   session was on opus.

Fact 2 answers question 2 outright: the "detect what the user last
selected in that session" behavior we want **already exists inside
Claude Code** — provided bay does not pass `--model` when resuming.
Bay's job on relaunch is to stay out of the way.

Fact 3 exposes a latent bug in the current code. `buildAgentCommand`
(`internal/engine/agent.go`) assembles
`[command] [resume_args] [args]` — config args are appended *on
resume too*. So a user who pins `--model fable` via `args` today
gets `claude --continue --model fable` on every `bay recover` and
undo-close restore, silently clobbering any in-session model
switch. Whatever else we build, args used to pin a model must be
launch-only.

---

## Design

### Profiles are just agents

A model profile is a config-defined agent that wraps a base client.
The config `Agents` map already accepts arbitrary names —
`ResolveAgent` checks config before the built-in registry, and
`validateAgentName` accepts anything resolvable with a command.
This works *today*, unsung:

```toml
[agents.fable]
command = "claude"
args = ["--model", "fable"]      # see launch_args below
resume_args = "--continue"
project_file = "CLAUDE.local.md"
```

`bay new --agent=fable`, `bay agent fable --pane`, per-dock
defaults (`[docks.X] agent = "fable"`), and per-dock
`agent_args` overrides all compose through existing machinery.
Surfaces already persist the agent name in the manifest, so
recover and undo-close relaunch the right profile with **zero new
manifest state**. The user's mental model — "a fable agent" — is
the literal name of the thing.

Two gaps make this clunky as-is; the design closes them:

### `extends`: inherit from a base agent

Without help, a profile must restate `resume_args` and
`project_file`, because `agentInfoFromConfig` only backfills gaps
when the agent's *name* canonicalizes to a built-in (and `fable`
doesn't). Add one field:

```toml
[agents.fable]
extends = "claude"
launch_args = ["--model", "fable"]
```

Resolution: start from the resolved base (built-in or
config-defined), then overlay the profile's explicitly set fields.
The profile's `launch_args`/`args` are **appended to** the base's,
not replacing them — so `[agents.claude] args = ["--dangerously-skip-permissions"]`
flows through to every claude-based profile, which is the point of
having a base. One level of `extends` only (a profile may not
extend a profile); resolving detects and rejects chains and cycles
with a clear config error.

`extends` must name a resolvable agent. The base also remains
launchable as itself — defining `fable` doesn't change what
`bay agent claude` does.

### `launch_args`: args that don't survive resume

New `AgentConfig` field alongside `args`:

- `args` — appended always, launch and resume (today's behavior).
  For flags that describe *how the client runs*:
  `--dangerously-skip-permissions`, sandbox flags, etc.
- `launch_args` — appended only when `resume == false`. For flags
  that describe *how the session starts*: `--model`, an initial
  prompt, `--append-system-prompt`.

`buildAgentCommand` becomes
`[command] [resume_args if resume] [args] [launch_args if !resume]`.

The model arg goes in `launch_args`. On recover/undo-close, the
profile contributes only `--continue` plus its persistent `args`,
and Claude Code restores the session's own model (ground-truth
fact 2). This is the semantically correct answer to question 2:
the session resumes on whatever the user last selected *in that
session*, with the launch-time pin as its starting point — and bay
never has to track or detect models itself.

`launch_args` also lands in per-dock config
(`[docks.X.launch_agent_args]` mirroring `agent_args`) — or we
fold both into a per-dock agent table; decide at implementation
time (open question 3).

This piece is worth shipping even if profiles weren't: it fixes
the existing re-pin-on-resume hazard for anyone using `args` to
set a model today.

### What bay does *not* do

- **No per-surface model tracking.** The manifest stores the
  profile name; the model is a property of the profile (config)
  and, after launch, of the agent's own session state. Storing a
  resolved model per surface would invite re-asserting it on
  resume — exactly the wrong behavior.
- **No transcript sniffing.** The Claude transcript JSONL does
  record which model produced each assistant message, so "what was
  this session last using" is detectable — but there's no
  correctness need for it (resume handles it), it's
  claude-specific, and it couples bay to an undocumented schema.
  If we ever want it, it's for *display* (e.g. `bay show` listing
  the model), not for launch decisions. Deferred.
- **No first-class `--model` flag on `bay new`/`bay agent`** in
  v1. It would need per-agent knowledge of each client's model
  flag, a place to persist the choice, and a resume story — all
  things profiles get structurally. If ad-hoc one-off pinning
  proves wanted, it can land later as sugar that expands to
  launch_args (open question 2).

### Keybindings

The explosion fear assumes binding the clients × models cross
product. Profiles change the unit: you bind the two or three
*identities you actually think in* — a short flat list that grows
linearly with taste, not combinatorially with the model catalog.

- **Defaults untouched.** `M-o c/x/g` (and the `M-o b`/`M-o h`
  submenus) keep launching bare `claude`/`codex`/`agy`. Unpinned
  launch → Claude's global last-selected default, exactly today's
  behavior. Simple stays simple.
- **Personal profile chords.** Users add bindings for their own
  profiles in `~/.tmux.conf` the same way the built-in agent
  chords work (`bay agent fable --pane`, etc.), pinned with
  `# bay-keep:` so `installKeybindings` leaves them alone. Bay's
  canonical binding list does not grow per profile — profiles are
  user vocabulary, not bay vocabulary.
- **One picker chord caps growth (step 3).** `M-o m` → a tmux
  `display-menu` generated from configured agents + profiles,
  launching the chosen one in a pane. Any future profile is then
  reachable with zero new bindings. The command palette already
  serves as the fully general fallback.

Rejected: a two-level chord (client key, then model key). It taxes
every launch interaction to defend against a matrix nobody
populates.

### Amendment (same PR): seeded built-in profiles

Bay ships four built-in profiles — `fable`, `opus`, `sonnet`,
`haiku` — so the core identities work with zero config
(`bay agent opus` out of the box). They're claude-based only:
Claude Code maintains those aliases so the pins don't rot, while
codex/antigravity model names churn and stay user-defined. The
seeds are `extends = "claude"` entries, not static commands, so a
user's `[agents.claude]` args flow through them. A same-named user
config entry adjusts the profile field-wise — unless it sets
`command` or `extends`, in which case it defines its own identity
and replaces the profile (protects pre-existing custom agents that
happen to share a seeded name). `disabled = true` removes one.
Probe, hook installation, and setup still consider only the three
base clients — profiles share their base's binary and hooks.

### Step 1: `launch_args` [SMALL — ~60-100 lines]

`AgentConfig`/`AgentInfo` field, `buildAgentCommand` split,
per-dock plumbing decision (open question 3), config-help text,
docs (human-guide config section, agent-guide). Independently
valuable: fixes the resume re-pin hazard.

### Step 2: `extends` [SMALL-MEDIUM — ~100-150 lines]

Resolution in `agentInfoFromConfig`/`ResolveAgent`: overlay
semantics, args appending, one-level-only validation with clear
errors, interaction with `AgentAliases` (a profile extending
`gemini` resolves to `antigravity`). Document the profile pattern
prominently — this step is mostly about making the recipe
ergonomic and supported rather than accidental.

### Step 3: profile picker [SMALL — optional]

`bay agent --pick` (or `bay palette` integration) producing a
`display-menu` of resolvable agents; one canonical chord (`M-o m`).
Only if step 2 usage shows the personal-chords approach running
out of comfortable keys.

---

## Corner cases and known limitations

1. **Profiles and turn-complete hooks.** `agentHookSpecs`
   (`internal/cli/setup.go`) installs Stop/notify hooks keyed by
   canonical agent name, but the hooks are global per-client
   (`~/.claude/settings.json` etc.) and fire for any process of
   that client — a fable profile launching `claude` triggers the
   claude Stop hook. No per-profile work needed. Setup's
   PATH-probing also only considers built-ins, which is correct:
   profiles share their base's binary.
2. **Auto-descriptions.** Transcript gathering is keyed by cwd,
   not agent name, so profile-launched sessions are summarized the
   same as base-launched ones. The describe worker's summarizer
   command is config-fixed (`codex exec`) and unaffected.
3. **Codex resume semantics differ.** `codex resume --last` may
   not restore a session's model the way Claude does; antigravity
   is unknown. The design doesn't depend on it — `launch_args`
   simply aren't replayed, so the resumed session does whatever
   the client natively does. If a client *forgets* its model on
   resume, the session falls back to that client's default; bay
   re-pinning would be wrong for Claude, so we accept the
   asymmetry rather than special-case per client.
4. **Claude behavior is version-dependent.** Keep-model-on-resume
   and `/model`-writes-global are v2.1.153+ semantics. On older
   versions, resume may behave differently; bay's behavior
   (don't pin on resume) is still the least-wrong choice there.
5. **Stale profiles in the manifest.** If a user deletes
   `[agents.fable]` from config while a fable surface is recorded,
   recover/undo of that surface fails `validateAgentName` with
   "unknown agent". Same failure mode as removing any configured
   agent today; the error message should suggest re-adding the
   profile. No new handling in v1.
6. **Retired models.** A profile pinning a retired model alias
   fails at the client, not in bay (`claude` errors or falls back
   per its own precedence). Bay doesn't validate model names — it
   doesn't know them, and the set changes under us.
7. **`default_agent` can name a profile.** `[default_agent]`
   resolution goes through `ResolveAgent`, so `default_agent =
   "fable"` works. Setup's probe (used when nothing is configured)
   still probes base clients only — probing should pick a client,
   not a model pin; built-in profiles resolve without config but
   are never probe results.

---

## Open questions (to revisit when building)

1. **Field name.** `launch_args` vs `initial_args` vs `new_args`.
   `launch_args` reads best against the existing `resume_args`.
2. **Ad-hoc pinning sugar.** Is `bay agent claude -- --model haiku`
   (args passthrough, recorded as that surface's launch-only args)
   worth having for one-offs that don't merit a profile? Requires
   persisting per-surface launch args in the manifest — small, but
   only add if profiles prove insufficient.
3. **Per-dock launch args shape.** Mirror `agent_args` with a
   second map, or restructure dock agent config into
   `[docks.X.agents.claude]` tables with both fields? The second
   is cleaner but is a config-shape migration; decide in step 1.
4. **Model display.** Should `bay show` / the picker surface which
   model a session is currently on (via transcript metadata)?
   Display-only, claude-only, undocumented schema — defer until
   someone misses it.
5. **Profile chords in `installKeybindings`.** Should setup offer
   to generate chords for configured profiles (e.g. first letter
   of each profile under `M-o`)? Conflicts with the "profiles are
   user vocabulary" stance and risks collisions with built-in
   chord keys; revisit only if hand-maintained bindings prove
   annoying.
