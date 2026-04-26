# Repo-level lifecycle hooks — design plan

Captured 2026-04-16. No code has been written for any of this. The
design emerged from a discussion about mise's per-path trust model:
every newly-created bay worktree triggers a `mise trust` prompt
because mise's trust state is keyed on absolute paths and bay creates
worktrees under `<repo>-worktrees/`, which mise has never seen
before.

## The problem

Tools that gate execution on path-based trust (mise, direnv, asdf,
etc.) re-prompt for every new worktree bay creates. The user-facing
symptom: friction on every `bay workspace new`. The underlying cause:
these tools want a one-time "I trust code under this directory tree"
declaration, and bay knows exactly where that tree lives — it's the
repo's `worktree_dir`, set at `bay repo add` time.

The naive fixes don't fit:

- **Per-worktree `mise trust`** runs the trust action on every new
  worktree. Works, but bay would be silently bypassing another tool's
  security model on every workspace creation, and accumulates state
  in mise that has to be cleaned up.
- **Global `MISE_TRUSTED_CONFIG_PATHS`** requires a fixed parent
  directory. Bay's worktree dirs are scattered — adjacent to whatever
  checkout the user started from (`~/projects/loom-worktrees`,
  `~/projects/labs-worktrees`, …) — so there's no single path to
  trust.
- **Bake mise-specific logic into bay** solves this case but couples
  bay to one tool. Tomorrow it's direnv (`direnv allow <dir>`), then
  asdf, then whatever comes next.

The shape that fits: a generic, user-defined hook that fires once per
repo at the moments bay is *already* changing repo-level state —
`repo add` and `repo remove`. The user writes the mise (or direnv, or
…) command; bay just runs it with the relevant paths in scope.

## Relationship to existing design

`docs/design/app-model.md` Step 2 describes **dock-level** lifecycle
hooks (`setup_command` / `teardown_command`) that fire on workspace
creation/teardown. Those are per-workspace and per-dock.

This spec is **repo-level** and fires at most once per repo lifetime.
The two are complementary:

| Scope | Fires on | Use case |
|---|---|---|
| Repo (this spec) | `repo add`, `repo remove` | Trust the worktree dir, register with external tools, set up shared cache dirs |
| Dock (app-model.md) | workspace create/close | Per-task collab dirs, agent-specific scaffolding |

Template variable conventions and error-handling philosophy match
app-model.md so the two systems feel like one.

## Background: where repo state currently lives

Repos today live **only in the manifest** (`internal/manifest`),
which is bay-managed JSON state at `~/.local/share/bay/manifest.json`
— not user-edited TOML. There is no `Repos` field in `Config`
(`internal/config/config.go`).

Docks already exhibit a split that this design adopts:

- `Config.Docks map[string]DockConfig` — user-editable TOML in
  `~/.config/bay/config.toml`, holds *behavior* config (agent,
  agent_args, terminal).
- `Manifest.Docks []Dock` — bay-managed JSON, holds *runtime* state
  (workspaces, surfaces, host window).

A repo equivalent doesn't yet exist. Step 1 introduces it.

## Hook execution contract

Applies to every hook defined by this spec (`on_add`, `on_remove`,
and any added later). The contract is stated once here and referred
to from each step.

| Aspect | Behavior |
|---|---|
| Shell | `/bin/sh -c <expanded-command>`. Not `$SHELL` — interactive shells run rc files which are slow and have side effects. Users who need bash/zsh features can write `bash -c '...'` themselves. |
| Working directory | `repo_path` (the repo root). Predictable; matches where most tool CLIs expect to run. |
| Stdin | `/dev/null`. Hooks are non-interactive. |
| Stdout/stderr | Inherited from the bay process so the user sees output and errors live. |
| Environment | Inherited from bay's process. No extra `BAY_*` vars in v1; if needed later, prefix with `BAY_`. |
| Template syntax | `{var_name}` (matches app-model.md). Unknown variables → fail before running the hook with a clear error naming the variable. Literal `{` is `{{` (escape only needed if the user actually wants a literal brace; not expected to be common). |
| Path values | All `*_dir` and `*_path` variables are expanded (no `~`) and absolute. |
| Idempotency | **Hooks must be idempotent.** Bay may run them more than once: on retry, via the explicit re-run command (Step 3), or after an interrupted operation. The user owns this; bay does not de-dupe. |
| Concurrency | Hooks run sequentially in declaration order. If one fails, see per-step error semantics for whether subsequent ones run. |

---

## Incremental path

### Step 1: Introduce `Config.Repos` and `on_add` hook [MEDIUM]

This step is two changes bundled because the hook can't exist
without a place to declare it.

**1a. Add `Repos` to `Config`.**

```go
// internal/config/config.go
type Config struct {
    DefaultAgent  string                  `toml:"default_agent,omitempty"`
    DefaultEditor string                  `toml:"default_editor,omitempty"`
    Agents        map[string]AgentConfig  `toml:"agents,omitempty"`
    Editors       map[string]EditorConfig `toml:"editors,omitempty"`
    Docks         map[string]DockConfig   `toml:"docks,omitempty"`
    Repos         map[string]RepoConfig   `toml:"repos,omitempty"` // new
    Monitor       MonitorConfig           `toml:"monitor,omitempty"`
}

type RepoConfig struct {
    OnAdd    []string `toml:"on_add,omitempty"`
    OnRemove []string `toml:"on_remove,omitempty"` // see Step 2
}
```

`RepoConfig` holds *behavior*; `manifest.Repo` keeps *state* (Path,
WorktreeDir, Name). They're keyed by the same name. A `RepoConfig`
entry is allowed to exist without a corresponding manifest entry
(user pre-declares hooks before adding the repo) and vice versa
(repos added without hooks). Neither side is authoritative for the
other's existence.

**1b. Wire `on_add` into `Engine.RepoAdd`.**

```toml
# ~/.config/bay/config.toml
[repos.loom]
on_add = ["mise settings add trusted_config_paths {worktree_dir}"]
```

In `internal/engine/repo.go` `RepoAdd`, after the manifest save
(line 69):

1. Compute the effective worktree dir (`manifest.Repo.EffectiveWorktreeDir()`).
2. **Create the worktree dir if it doesn't already exist** (`mkdir -p`).
   Today bay creates it lazily on first `workspace new`; for hooks
   to be useful (mise/direnv need the dir to exist before they can
   trust it) we eagerly create it here.
3. Look up `e.Config.Repos[name]`. If present and `OnAdd` is
   non-empty, run each command per the execution contract.

**Template variables:**

| Variable | Value |
|---|---|
| `{repo_name}` | Name passed to `bay repo add` |
| `{repo_path}` | Repo root path (absolute, expanded) |
| `{worktree_dir}` | Effective worktree parent directory (absolute, expanded) |

**Error handling:**
- Hook command exits non-zero → print "hook N of M failed: <cmd>",
  print captured stderr, **stop running remaining hooks**, but
  **leave the repo entry in place**. The manifest write already
  happened; rolling back would compound the failure (now the user
  has neither the repo nor the trust setup). They can fix the issue
  and use the re-run command (Step 3).
- Template expansion fails (unknown variable) → fail *before*
  running anything; don't half-execute.
- Hook not configured → no-op, no message.

**Clone path (`bay repo add --url`):** hooks run after the clone
succeeds and after the manifest save, same as the local-repo path.
A clone failure means no manifest entry was written, so no hook
runs.

**Why first:** Solves the mise case end-to-end with the smallest
useful surface area. Doesn't depend on any other change. Generic
enough to also cover direnv (`direnv allow {worktree_dir}`) and any
future tool.

**Risk to call out:** introducing user-editable repo config is a
schema change with discoverability implications. `bay repo show`
should display configured hooks alongside the manifest data so users
can see what will run. `bay doctor` should flag `Config.Repos`
entries with no matching manifest entry (probably typo).

### Step 2: `on_remove` hook on `bay repo remove` [SMALL]

`OnRemove` field already exists in `RepoConfig` from Step 1; this
step wires it in.

```toml
[repos.loom]
on_remove = ["my-cleanup-script {worktree_dir}"]
```

**Critical asymmetry with `on_add`:** by default `bay repo remove`
does *not* delete the worktree directory — it only removes the
manifest entry. The worktrees still exist on disk and the user may
still `cd` into them. Auto-tearing-down external state at that point
creates a confusing "why is mise prompting me again?" moment with no
clear cause.

Therefore:

- `bay repo remove` (no flags): run `on_remove` hooks, leave
  worktrees on disk. Users who define `on_remove` are explicitly
  opting into "treat removal as a full external unregister, even
  though the dir still exists."
- `bay repo remove --purge`: delete the worktree directory, *then*
  run `on_remove`. This is the case where cleanup hooks make
  unambiguous sense.

The hook itself is the same in both cases; only the on-disk state
differs. Document the asymmetry in `bay repo remove --help`.

**Template variables:** same as `on_add`. Even with `--purge`,
`{worktree_dir}` resolves to the path the worktree dir *was at*; the
hook may be cleaning up references to it, so the value must be
present.

**Error handling:** hook failures **log a warning and continue to
the next hook**. By the time `repo remove` runs, the user has
decided to remove — blocking on cleanup failure is worse than
leaving stale external state.

**Mise-specific note:** `mise settings unset trusted_config_paths`
removes the entire setting, not a single array element. There is no
clean mise CLI for "remove just this path." Users who want symmetry
will need a small script or to leave stale entries (mise ignores
trusted paths that don't exist on disk, so the cost is just
untidiness). The worked example below shows `on_add` only for this
reason.

### Step 3: `bay repo hooks run <name> <add|remove>` [SMALL]

A debugging / re-run command. Useful when:

- A hook failed during `repo add` and the user has fixed the
  underlying issue (e.g. mise wasn't installed yet).
- The user added `on_add` to an existing repo and wants to apply it
  retroactively without re-adding the repo.
- Debugging a hook command without going through the full
  add/remove flow.

```
bay repo hooks run loom add       # runs on_add for repo "loom"
bay repo hooks run loom remove    # runs on_remove for repo "loom"
```

Same template expansion, same execution contract, same error
semantics as the auto-fired versions. No change to manifest or
config. Requires the manifest entry to exist (so `{worktree_dir}`
can resolve); errors clearly if it doesn't.

### Step 4 (optional): `--purge` ergonomics

Once Step 2 is in place, `--purge` deserves polish.

**Flag-naming concern:** `bay repo remove --force` already exists,
meaning "also close and remove docks that use this repo." Reusing
`--force` for "skip the active-worktree check on purge" would
overload it confusingly. Name the new flag explicitly:

- `--purge`: delete the worktree dir; refuse if it contains tracked
  worktrees (anything in `git worktree list` from the parent repo).
- `--purge-force`: skip that check and delete anyway.

Or fold into a single confirmation flow: `--purge` always prompts
unless `--yes` is also passed; the prompt explains what will be
deleted.

**"Active worktree" definition:** use `git worktree list` from the
parent repo as the source of truth. The manifest can be stale; git
isn't.

This is independent of the hooks system but is the natural follow-up
because `--purge` becomes a more first-class operation once hooks
exist.

---

## Worked examples

### mise (the motivating case)

```toml
[repos.loom]
on_add = ["mise settings add trusted_config_paths {worktree_dir}"]
# no on_remove — mise has no CLI to remove a single trusted path;
# stale entries are harmless.
```

User runs `bay repo add loom ~/projects/loom`. Bay writes the
manifest entry, creates `~/projects/loom-worktrees/`, then runs:

```
mise settings add trusted_config_paths /Users/mike/projects/loom-worktrees
```

(Verified syntax: `mise settings add` exists and "Used with an
array setting, this will append the value to the array." —
`mise settings add --help`.)

Every future worktree bay creates under that path is auto-trusted.
No per-worktree action, no prompts, no accumulated state in mise.

### direnv

```toml
[repos.loom]
on_add = ["direnv allow {worktree_dir}"]
```

Caveat: direnv's trust model is per-`.envrc`-file, not per-tree, so
this only allows an `.envrc` *at* `worktree_dir`. Per-worktree
`.envrc` files would still need their own allow (which is the
dock-level hook's job, not this one).

### Shared cache directory

```toml
[repos.loom]
on_add = ["mkdir -p ~/.cache/bay/{repo_name}"]
on_remove = ["rm -rf ~/.cache/bay/{repo_name}"]
```

Illustrates that the hook system isn't mise-specific — it's generic
enough to absorb whatever per-repo external setup the user has.
Note the symmetric pair works here because the user controls both
sides.

---

## What this is not

- **Not per-worktree.** That's app-model.md's dock-level hooks. This
  spec deliberately fires only on repo-level events to keep the
  blast radius and frequency low.
- **Not a plugin system.** Hooks are shell commands, not Go plugins
  or a DSL. Power comes from the user's existing shell, not from
  bay.
- **Not a replacement for `bay repo init`.** `init` writes in-repo
  files (CLAUDE awareness lines, `.worktreeinclude`); hooks run
  external commands. They're orthogonal.

## Open questions

- **Should `Config.Repos` entries auto-create manifest entries?** No
  — pre-declaring hooks for a repo you haven't added yet is fine,
  but bay shouldn't infer `Path`. The user runs `bay repo add`
  explicitly.
- **Should hooks see git state?** E.g. a `{repo_default_branch}`
  variable. Probably yes eventually, but adds a git call to a
  previously-pure code path. Defer until a real use case shows up.
- **Per-hook timeout?** Currently unspecified — hooks block
  indefinitely. Most external trust commands return in milliseconds,
  so probably fine, but worth revisiting if a slow hook ever blocks
  a `repo add`.
