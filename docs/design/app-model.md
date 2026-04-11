# App model and lifecycle hooks — design plan

Captured 2026-04-10. No code has been written for any of this. The
design emerged from a discussion about managing agent swarms that need
per-workspace collaboration directories and agent binaries launched
from a different directory than the code worktree.

## The problem

The user runs an agent-swarm framework where:

1. **Agent binary must launch from the swarm repo** (to pick up
   bootstrap config), not from the code worktree.
2. **Each workspace needs a lightweight collaboration directory**
   where agents coordinate. This directory is per-project, shared
   among agents on the same task, and separate from the code repo.
3. **Each workspace should have its own independent agent
   conversation** — no single-chokepoint session managing all
   branches.

Bay's current model assumes agent CWD = workspace path and has no
concept of per-workspace setup/teardown beyond the worktree itself.

### Usage ratios (from actual workflow)

| Use case | Frequency |
|---|---|
| New branch on existing repo | 5 |
| Existing branch on this repo | 2 |
| Review a PR on its branch | ~0 |
| Review a random PR via agent (could come from anywhere) | 4 |
| Scratch space | 2 |

Most invocations (11/13) already pass `--branch` explicitly. The
default behavior (detached HEAD, no branch) primarily serves the
scratch case. This ratio informed the decision to keep the current
worktree default rather than auto-creating branches.

---

## Incremental path

### Step 1: Agent `cwd` override [SMALL — ~30 lines]

**Status:** Designed, not built. Build when the swarm workflow is
actively used.

Add a `cwd` field to `AgentConfig`:

```toml
[agents.swarm]
command = "swarm-agent"
cwd = "~/projects/agent-swarm"
```

When `cwd` is set, bay changes the pane's working directory to `cwd`
before launching the agent command. The workspace's worktree path is
passed via a template variable in `agent_args`:

```toml
agent_args = ["--project-dir", "{workspace_path}"]
```

When `cwd` is not set (the common case), existing behavior is
unchanged — the agent runs in the worktree directory.

**Implementation:** In `engine/surface.go` `launchSurfaceInTmux`,
before `SendKeys`, optionally `SendKeys(paneID, "cd <cwd>", "Enter")`
if the agent config has `cwd` set. Template-expand `{workspace_path}`
in `agent_args` at launch time.

**Why first:** Smallest useful change. Directly unblocks "agent
launches from swarm repo, operates on code worktree." No lifecycle
hooks needed.

### Step 2: Dock-level lifecycle hooks [MEDIUM — ~50-80 lines]

**Status:** Designed, not built. Build when step 1 is in use and the
swarm framework has lightweight project-dir tooling.

Add `setup_command` and `teardown_command` to `DockConfig`:

```toml
[docks.myproject]
repo = "target-code"
agent = "swarm"
setup_command = "swarm init --collab {dock_dir}/collab/{workspace_name} --code {workspace_path}"
teardown_command = "swarm cleanup --collab {dock_dir}/collab/{workspace_name}"
```

**Lifecycle placement:**

```
create worktree
  → run setup_command (worktree exists, can reference it)
  → create tmux window
  → launch surface (setup is complete, agent can use it)
  → [user works]
  → close surfaces
  → run teardown_command (worktree still exists for cleanup)
  → remove worktree
```

**Template variables available:**

| Variable | Value |
|---|---|
| `{workspace_path}` | Worktree directory |
| `{workspace_name}` | Workspace display name |
| `{dock_name}` | Dock name |
| `{dock_dir}` | Dock's worktree parent directory |
| `{repo_path}` | Repo root path |

**Error handling:**
- Setup fails → workspace creation fails, worktree is rolled back,
  clean error to user.
- Teardown fails → log warning, continue with worktree removal.
  Teardown is best-effort because the user chose to close — blocking
  on cleanup failure is worse than leaving an orphan.

**Why second:** Depends on the swarm framework having lightweight
project-dir create/destroy tooling (which doesn't exist yet). The
bay side is small, but the value only materializes when the agent-side
tooling is ready.

### Step 3: App model [LARGE — full refactor]

**Status:** Conceptual. Build only when the hook-based approach from
steps 1-2 has been used enough to validate the patterns.

Generalize agents, editors, and commands into a single `[apps.X]`
config namespace. Each app declares:

```toml
[apps.claude]
command = "claude"
resume_args = "--continue"

[apps.swarm]
command = "swarm-agent"
cwd = "~/projects/agent-swarm"
setup = "swarm init --collab {collab_dir} --code {workspace_path}"
teardown = "swarm cleanup --collab {collab_dir}"
project_arg = "--project-dir"

[apps.cursor]
command = "cursor"
gui = true

[apps.test-watch]
command = "npm test -- --watch"
setup = "npm install"
```

**What this replaces:**

| Today | App model |
|---|---|
| `[agents.X]` config section | `[apps.X]` with no special handling |
| `[editor]` config section | `[apps.cursor]` (or whatever) with `gui = true` |
| `bay new cmd "..."` inline commands | Can be named: `[apps.test-watch]` |
| Three surface types in engine code | One launch path for all apps |
| Three sets of launch logic | One set with conditional setup/teardown |

**CLI impact:**

```
bay ws new --app swarm           # launch with swarm app
bay ws new --app claude          # launch with vanilla claude
bay new cursor                   # launch editor app
bay new test-watch               # launch custom app
bay shell                        # stays as-is (shell is not an app)
```

**Surface type in manifest:** Changes from `agent`/`cmd`/`editor` to
a single `app` type with an `app_name` field referencing the config.
Or: keep the existing types for backward compatibility and add `app`
as a new type. Either way, manifest migration needed.

**Why this is the long-term target:**

- Adding a new kind of surface is a config change, not a code change.
- Setup/teardown becomes per-app, not per-dock. Different apps in the
  same dock can have different lifecycle hooks.
- The swarm agent and the vanilla agent coexist naturally — both are
  apps with different configs.
- The editor overload (bay edit does double duty as "open editor" and
  "configure editor") is already partially resolved (#113 moved config
  to `bay config editor`). The app model finishes the job: the editor
  is just an app.

**Why not build it now:**

- The refactor touches engine, manifest, CLI, monitor, completions,
  docs — ~20 files.
- The current model works for the 80% case (one agent, one shell,
  maybe an editor).
- Steps 1 and 2 deliver the swarm value without the refactor cost.
- Real usage of the hooks will reveal which app-model features are
  actually needed vs. speculative.

---

## Relationship to workspace templates

Workspace templates (see `docs/design/` or the workspace-templates
discussion from 2026-04-03) define WHICH surfaces to create. The app
model defines HOW each surface runs. They compose:

```toml
[templates.swarm-dev]
surfaces = [
  { app = "swarm", split = "" },
  { app = "shell", split = "v" },
]

[templates.solo-dev]
surfaces = [
  { app = "claude" },
  { app = "shell", split = "v" },
  { app = "cursor" },
]
```

Templates without the app model: each surface has a `type` field
(`agent`, `shell`, `cmd`, `editor`) and type-specific fields.
Templates WITH the app model: each surface has an `app` field
referencing an `[apps.X]` entry. Cleaner, more extensible.

If templates ship before the app model, they'll use the type-based
schema. If the app model ships first (unlikely given the refactor
size), templates become simpler. Either order works; they're
independent features that compose well.

---

## Dependencies

| Step | Depends on | Bay code size | External dependency |
|---|---|---|---|
| 1. Agent cwd | Nothing | ~30 lines | None |
| 2. Dock lifecycle hooks | Swarm framework's lightweight project-dir tooling | ~50-80 lines | Agent-side tooling |
| 3. App model | Real usage of steps 1-2 to validate patterns | ~500+ lines | None (but informed by usage) |

**Suggested order:** 1 → 2 → use it for a while → 3 if the patterns
hold. Templates can ship at any point independently.

---

## Open questions (to revisit when building)

1. **Template variable syntax.** `{workspace_path}` vs
   `$WORKSPACE_PATH` (env var) vs `{{.WorkspacePath}}` (Go template).
   Env vars are the simplest — bay sets them before running the
   command, no parsing needed. But they pollute the environment.
   Template syntax is cleaner but needs a parser. Decide when
   implementing step 2.

2. **Should setup_command run in the worktree or the dock's CWD?**
   Probably the worktree — it's the most useful context. But the swarm
   use case might want the swarm repo as CWD for the setup command
   too. Could honor `cwd` on the dock config (separate from the
   agent's `cwd`). Decide when implementing.

3. **Teardown on force-close.** `bay ws close --force` skips safety
   checks. Should it also skip teardown? Probably not — teardown is
   cleanup, not a safety check. But if teardown hangs, force-close
   should have a timeout. Decide when implementing.

4. **Multiple apps per workspace.** The app model naturally supports
   this (each surface references a different app). But setup/teardown
   per-app-per-surface is complex — does each surface's app get its
   own setup, or does the workspace run setup once for all apps?
   Probably per-app — an agent surface's setup is different from a
   test-watcher's setup. Decide when implementing step 3.

5. **Manifest migration for the app model.** Current surface types
   (`agent`, `shell`, `cmd`, `editor`) are in the manifest. Switching
   to `app` + `app_name` needs a migration strategy. Options: version
   the manifest and auto-migrate, or keep old types as aliases for
   built-in apps. Decide when implementing step 3.
