# Architecture hardening plan

Captured 2026-04-26. No code has been written for this plan.

## Summary

Bay's current architecture is sound enough to keep. The core shape -
`dock -> workspace -> surface`, a manifest as the source of truth,
tmux/git adapters, Cobra CLI commands, recovery, sync, and a monitor -
is the right foundation for the product.

This plan does not propose a rewrite. It proposes staged cleanup
around the places where bugs are most likely:

- external side effects
- tmux launch and recovery
- git safety checks
- config/process boundaries
- command metadata drift across CLI, palette, docs, and completions

The practical target is to make the lifecycle paths explicit,
well-tested, and less stringly typed without changing the user-facing
model.

## Current assessment

The implementation is solid but not fully hardened.

Strong parts:

- Clear domain model: docks, workspaces, surfaces, repos.
- Useful package seams: `cli`, `engine`, `manifest`, `tmux`, `git`,
  `config`, `nav`, `palette`, `picker`, `monitor`.
- Adapter interfaces for tmux and git make the core behavior testable.
- Good unit coverage for many tricky behaviors.
- Real-tmux integration tests already exist for layout fidelity.
- Comments capture a lot of the operational reasoning behind edge
  cases.

Weak parts:

- Surface launch behavior is spread across engine, CLI, editor, and
  recovery paths.
- Command construction is mostly string-based, so quoting and argument
  boundaries are fragile.
- Some operations perform external side effects before all validation
  is complete.
- Recovery is case-driven rather than a uniform reconciliation pass.
- Some smoke/workflow tests have drifted from the current CLI.
- Some configuration and command metadata is duplicated across CLI,
  palette, docs, completions, and tests.

The conclusion: refactor in place. Do not rewrite from scratch unless
the product model changes substantially.

## Goals

- Make workspace and surface lifecycle behavior easier to reason about.
- Ensure tmux, git, config, and monitor side effects either succeed
  visibly or fail cleanly.
- Reduce duplicated command behavior across CLI, palette, docs,
  completions, and recovery.
- Improve confidence through workflow-level tests, not only unit tests.
- Keep the current user-facing command model stable.

## Non-goals

- No full rewrite.
- No large CLI redesign.
- No broad package reshuffle unless a specific seam demands it.
- No speculative app/plugin framework unless later phases prove it is
  needed.
- No attempt to make Bay perfectly abstract over every possible
  terminal multiplexer or editor.

## Proposed work

| Area | Benefit | Cost | Value | Recommendation |
| --- | --- | --- | --- | --- |
| Concrete bug fixes from review | Removes known correctness holes | Low | Very high | Do first |
| Workflow/smoke tests | Prevents regression in daily flows | Low-medium | Very high | Do first |
| Typed launch layer | Centralizes agent/cmd/editor launch, resume, quoting, and validation | Medium | High | Do early |
| Side-effect sequencing cleanup | Reduces partial state and orphan bugs | Medium | High | Do after launch |
| Recovery reconciliation model | Makes reboot and manual tmux drift recovery more reliable | High | Medium-high | Do if recovery matters |
| Stable internal IDs | Reduces name/path ambiguity long term | Medium-high | Medium | Defer unless needed |
| Command metadata registry | Reduces docs, palette, completion, and hotkey drift | Medium-high | Medium | Defer |
| Full app model | More extensible surface system | High | Medium | Skip for now |

## Phase 0: Correctness fixes

Fix the known review findings before any architectural cleanup:

- `bay recover` should restore dock-level surfaces, matching
  `bay dock recover`.
- Do not ignore `RespawnPane` or other launch errors.
- Support local-only git repos as a first-class workflow.
- Propagate `--config` into monitor child processes.
- Normalize external workspace paths.
- Make default agent probing deterministic.
- Fix editor-surface manifest validation.
- Replace undo-close second-resolution timestamp identity with a
  collision-resistant ID.
- Repair `scripts/smoke-test.sh`.

Local-only repo policy: support it as a first-class workflow. Bay's
zero-config story starts from the current git repo, and that repo may
not have a remote yet. Worktree creation should use `origin/<default>`
when available, and fall back to the local default branch when it is
not. Close safety for branches without an upstream should compare
against the local or remote default branch and treat commits above that
default as unpushed work. If Bay cannot determine a safe comparison
base, it should fail closed and require `--force`.

Estimated cost: 1-2 days.

Benefit: removes real correctness holes and restores trust in the
existing test and smoke-test story.

Value: highest.

Recommendation: mandatory. This phase should happen before broader
refactoring so the baseline is clean.

Definition of done:

- Each listed issue has a focused regression test.
- `scripts/smoke-test.sh` passes.
- `go test ./...`, `go test ./... -race`, and the real-tmux
  integration slice pass.
- Local-only repo behavior is documented in user-facing docs where
  workspace creation and close safety are explained.

## Phase 1: Workflow test layer

Add scenario tests around actual daily workflows:

- Zero-config local repo workspace creation.
- Remote-backed worktree creation.
- Custom config path plus monitor start.
- Terminal editor surface lifecycle.
- Dock editor recovery.
- Failed launch does not create a live manifest surface.
- Close, restore, recover loops.
- Local-only repo close safety.

These can be a mix of Go tests and shell smoke tests. The key is to
exercise user-level flows rather than only helper functions.

Estimated cost: 1-2 days.

Benefit: catches regressions that unit tests miss, especially command
shape drift and multi-subsystem lifecycle bugs.

Value: very high.

Recommendation: do alongside Phase 0. Fixes without workflow tests are
likely to regress because Bay's tricky behavior spans CLI, manifest,
tmux, git, and monitor state.

Definition of done:

- The smoke test covers the documented quick-start workflow.
- At least one workflow test uses a repo with no remote.
- At least one workflow test uses a remote-backed repo.
- Launch failure, dock editor recovery, and custom config monitor
  behavior are covered by tests that would fail on the current code.

## Phase 2: Typed launch layer

Introduce a small internal launch abstraction. This is not the full app
model. It is a narrow layer that gives all launch paths one place to
build, validate, persist, and resume surfaces.

Possible shape:

```go
type LaunchSpec struct {
    Kind          manifest.SurfaceType
    Name          string
    CWD           string
    Argv          []string
    ShellCommand  string
    ResumeArgs    []string
    Backend       manifest.SurfaceBackend
}
```

The exact fields can change, but the command contract should be
explicit:

- Exactly one of `Argv` or `ShellCommand` may be set.
- `Argv` is for Bay-constructed commands where argument boundaries must
  be preserved. Before passing the command to tmux, Bay converts `Argv`
  to one shell command string with a small, tested shell-quoting helper.
- `ShellCommand` is for intentionally raw shell language, primarily
  user-provided `cmd` surfaces. Bay does not reinterpret or split it.
- Resume behavior must follow the same contract as first launch.
- Persisted manifest state should record user intent, not incidental
  tmux syntax. For example, agent surfaces persist agent name and args;
  command/editor surfaces persist their command string.
- Validation rejects mixed or empty command forms before any tmux
  mutation.

The responsibilities should be clear:

- construct commands once
- preserve argument boundaries where possible
- define when a shell string is intentionally required
- distinguish first launch from resume launch
- define editor command handling in one place
- validate required agent/cmd/editor fields before tmux mutation
- return tmux launch errors to callers
- populate the manifest surface consistently

Initial users:

- workspace initial surface creation
- `SurfaceAdd`
- terminal editor surfaces
- dock editor surface creation
- recovery and restore launch paths

Estimated cost: 1-3 days.

Benefit: removes the most important source of stringly typed behavior
and duplicated launch logic.

Value: high.

Recommendation: do early. This is the best architectural cleanup for
the money.

Definition of done:

- All tmux-backed surface launches pass through one launch helper.
- Launch errors propagate to callers and prevent live manifest entries.
- Agent args and editor paths with spaces are covered by tests.
- Recovery and restore use the same command construction rules as first
  launch.

## Phase 3: Side-effect sequencing

Standardize mutating operations into predictable sequences. There are
two important variants.

For create/update flows:

1. Validate manifest and config inputs.
2. Build an operation plan.
3. Perform external side effects.
4. Persist manifest/config state.
5. Roll back or record cleanup on failure.

For destructive tmux close flows, preserve Bay's existing safety
invariant:

1. Validate the close target and safety gates.
2. Persist manifest/config state that removes or updates Bay-owned
   records.
3. Then kill tmux panes/windows/sessions.

The order is intentionally different because Bay is often invoked from
inside the pane it is about to kill. tmux may SIGHUP Bay immediately
after the kill command. Persisting manifest state first keeps the
user-visible Bay state correct even if the process dies during tmux
cleanup. Any cleanup refactor must keep this invariant.

Apply this first where the blast radius is highest:

- `DockNew`
- `WsNew`
- `SurfaceAdd`
- `RepoRemove`
- close and restore flows where practical

Expected improvements:

- no tmux session created before manifest validation fails
- no config written before a later validation failure
- no surface recorded when launch failed
- clearer rollback behavior for worktree/window creation
- fewer "best effort" side effects hidden from callers

Estimated cost: 2-3 days.

Benefit: makes lifecycle bugs easier to prevent and easier to fix.

Value: high.

Recommendation: do after the typed launch layer, since launch errors
are one of the key side effects this phase should sequence cleanly.

Definition of done:

- `DockNew` validates manifest references before creating sessions,
  writing config, or launching terminals.
- `WsNew` and `SurfaceAdd` have explicit rollback paths for created
  tmux/worktree resources.
- Destructive close paths still persist manifest changes before tmux
  kills, with regression tests for the ordering.
- Best-effort side effects are documented at call sites and do not hide
  primary operation failure.

## Phase 4: Recovery reconciliation

Move recovery toward a reconciliation model:

```text
desired manifest state + actual tmux state -> repair plan -> apply -> persist runtime IDs
```

This would replace scattered case handling with one conceptual model
for:

- missing sessions
- missing windows
- missing panes
- dock-level surfaces
- dead panes
- layout groups
- focus restoration
- already-live panes that should not be relaunched

The existing real-tmux integration tests and tmux mock are good
scaffolding for this. The risk is tmux layout behavior: it has enough
real-world nuance that this phase should be incremental and test-led.

Estimated cost: 3-5 days.

Benefit: makes recovery more reliable after reboot, manual tmux
mutation, and mixed Bay/tmux workflows.

Value: medium-high.

Recommendation: do if recovery is a daily reliability concern. Defer if
Phase 0 fixes are enough and recovery is rarely used.

Definition of done:

- `bay recover` and `bay dock recover` share the same reconciliation
  path for dock-level and workspace-level surfaces.
- Reconciliation emits a repair plan that can be tested without applying
  it.
- Existing real-tmux integration tests still pass.
- New tests cover missing session, missing window, missing pane,
  dock-level editor recovery, and already-live panes.

## Phase 5: Stable internal IDs

Names are user-facing and renameable. Paths can change in meaning over
time. Today Bay often uses names or paths to re-find state after a
manifest reload. That works, but it increases ambiguity as the tool
grows.

Possible future cleanup:

- stable dock/workspace IDs in the manifest
- stable closed-entry IDs for undo-close
- references by ID internally, names for CLI display and lookup
- migrations that preserve existing manifests

Estimated cost: 2-4 days for a careful first pass, more if broadly
threaded through CLI output.

Benefit: reduces ambiguity and makes concurrent or interleaved
operations easier to reason about.

Value: medium.

Recommendation: defer. Do the narrow undo-close ID fix in Phase 0, but
avoid a broad manifest identity migration until name/path ambiguity is a
real source of bugs.

Definition of done, if this phase is ever taken:

- Manifest migration is backward compatible.
- CLI output and user-facing references remain name-based unless a user
  explicitly asks for JSON/internal details.
- Rename and recover flows use stable IDs internally.

## Phase 6: Command metadata registry

Create shared metadata for commands used by:

- Cobra command definitions
- command palette entries
- hotkey display
- completions
- docs generation or docs consistency checks

The current command set is duplicated in several places. That is
manageable while the command surface is stable, but it creates drift
when commands or flags change.

Possible shape:

```go
type CommandMeta struct {
    ID          string
    Use         string
    Short       string
    Scope       Scope
    HotkeySig   string
    Palette     bool
    Completion  CompletionKind
}
```

This does not need to generate everything. A smaller first step could
be a docs/metadata check that verifies known commands and examples
still exist.

Estimated cost: 2-4 days.

Benefit: reduces docs, palette, hotkey, and completion drift.

Value: medium.

Recommendation: defer unless command churn is high or drift keeps
recurring.

Definition of done, if this phase is ever taken:

- At least one source of drift is removed, not merely wrapped.
- Palette entries and Cobra commands share enough metadata that a flag
  or command rename cannot silently leave stale palette/docs examples.
- The registry does not make simple command code harder to read.

## Full app model

A full app model would generalize agents, editors, commands, and custom
tools into a single configured app namespace. It could support per-app
cwd, setup, teardown, resume, GUI behavior, and argument templates.

This is attractive long term, but it is larger than the immediate
hardening need. The typed launch layer should be built first. If that
layer remains small and sufficient, the full app model is unnecessary.

Estimated cost: 1-2+ weeks depending on migration scope.

Benefit: strong extensibility if Bay grows beyond its current agent,
shell, editor, and command surface types.

Value: medium today, potentially high later.

Recommendation: skip for now.

Build it only when:

- agents, editors, commands, and custom tools all need different launch
  policies
- per-app setup/teardown/cwd/resume behavior becomes common
- the typed launch layer starts accumulating app-model-like conditionals
- the user-facing model needs configured apps rather than built-in
  surface types

## Proposed sequencing

Recommended path:

1. Phase 0: correctness fixes.
2. Phase 1: workflow tests.
3. Phase 2: typed launch layer.
4. Phase 3: side-effect sequencing.
5. Stop and reassess.

Conditional follow-up:

6. Phase 4 if recovery remains a meaningful reliability concern.
7. Phase 6 if command metadata drift keeps causing bugs or doc churn.
8. Phase 5 only when broad identity ambiguity becomes a practical
   problem.
9. Full app model only when the product needs configured app behavior.

## Where to draw the line

The practical line should be Phase 3.

At that point Bay should have:

- known correctness bugs fixed
- meaningful workflow regression tests
- centralized launch behavior
- cleaner lifecycle sequencing
- fewer hidden side-effect failures

That is the high-value cleanup. It should make the codebase
substantially easier to maintain without committing to a larger
redesign.

Use these questions to decide whether to continue:

1. Are recovery bugs common enough to justify a reconciliation refactor?
2. Do users often manipulate tmux directly and expect Bay to heal
   itself?
3. Is command/docs/palette/completion drift causing repeated bugs?
4. Are new surface types or app-like behaviors becoming hard to add?
5. Are name/path ambiguities causing real operational bugs?

If the answer is no, stop after Phase 3. The codebase does not need
architectural perfection. It needs the lifecycle paths made explicit
and tested.

## Implementation guidance

- Keep changes incremental and behavior-preserving.
- Prefer improving existing package seams over moving files around.
- Add tests before or alongside lifecycle changes.
- Treat smoke tests as part of the design, not as a separate chore.
- Avoid broad abstractions until two or more real call sites need them.
- Keep CLI compatibility unless a current behavior is clearly broken.
- Reassess after Phase 3 before starting any larger refactor.
