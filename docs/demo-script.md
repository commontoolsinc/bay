# Bay Demo Script (~5 min)

Live screenshare. One pre-built dock with workspaces in various
states, plus live creation and cleanup.

## Pre-demo setup

Have a dock (e.g., `labs`) with 3-4 workspaces ready:

| Workspace      | Branch              | Status   | Surfaces        | Notes                          |
|----------------|---------------------|----------|-----------------|--------------------------------|
| `auth-fix`     | `fix/auth-header`   | active   | agent, shell    | Agent running, not waiting     |
| `cache-ttl`    | `fix/cache-ttl`     | active   | agent           | Agent waiting for input (highlighted) |
| `dep-update`   | `chore/dep-update`  | done     | shell           | PR merged, auto-detected       |
| `api-logging`  | `feat/api-logging`  | active   | agent, shell    | Normal active work             |

- Monitor running (`bay monitor start`)
- tmux status line configured
- Start focused on `auth-fix` agent surface

---

## 1. The Problem (45s)

> You're all juggling multiple PRs at once — three, four, six at a
> time. Each one needs a branch, a checkout, a terminal, probably an
> agent. So you end up with a pile of worktrees, a pile of terminal
> tabs, and no way to see what's where.
>
> I was doing this manually for months. Six worktrees, six tmux
> windows, an agent in each. It basically worked — until I'd forget
> which worktree was for what, which ones were free, and setting up
> new ones was a hassle. Then I started working in a second repo and
> just... didn't set it up properly. It became a mess.
>
> Bay fixes this. It makes the workspace the unit — one command gives
> you a branch, a worktree, a tmux window, and an agent. And it
> tracks all of them for you.

---

## 2. The Dashboard (60s)

**Do:** Run `bay tree`

> This is my current dock. I've got four things going on. Bay shows
> me all of them — workspace name, branch, PR number, status.

**Walk through the output:**

> `auth-fix` — active, working on an auth header bug. Has an agent
> and a shell.
>
> `cache-ttl` — see that highlight? That means the agent is waiting
> for my input. Bay's background monitor detects that and flags it.
>
> `dep-update` — status says "done." I didn't set that. Bay detected
> that the PR merged and marked it automatically.
>
> `api-logging` — normal active work.

**Do:** Run `bay pwd`

> Bay always knows where I am — which dock, which workspace, which
> surface. No guessing.

---

## 3. Navigate (45s)

**Do:** Hit `Option+Shift+K` a few times — cycle through workspaces.

> Option-Shift-J and K cycle between workspaces. Each one is its own
> worktree — totally isolated checkout, different branch, different
> state.

**Do:** Within a workspace, hit `Option+J` / `Option+K` to cycle surfaces.

> Within a workspace, Option-J and K cycle between surfaces — agent,
> shell, editor, whatever you've got open.

**Do:** Hit `Option+R`.

> And this jumps straight to the next agent that's ready for me — 
> waiting for input. No hunting through tabs.

---

## 4. Create (60s)

**Do:** Create one workspace live.

```
bay ws new demo-feature --branch demo-feature
```

> One command. Bay created a git worktree, checked out a new branch,
> opened a tmux window, and dropped me into a shell. I'm ready to
> work.

**Do:** Run `bay agent`

> Now I've got an agent running in this workspace.

**Do:** Run `bay tree`

> There it is in the tree — `demo-feature`, active, agent and shell.

**Do:** Hit `Option+C` three times in quick succession. Let each one
create with default names.

**Do:** Run `bay tree`

> Four new workspaces. Each one has its own worktree, its own branch,
> its own tmux window. That's real keybindings, not demo tricks — 
> Option-C is how you'd actually create workspaces day to day.

---

## 5. Cleanup (45s)

**Do:** Point at `dep-update` (done status) in the tree.

> This one's done — the PR merged and bay caught it. Let me close it.

**Do:** `bay ws close dep-update` (or Option+W on it).

> Gone. Worktree deleted, branch deleted, tmux window closed.

**Do:** Hit `Option+W` three times to close the three burst-created
workspaces.

**Do:** Run `bay tree`

> All gone. No stale worktrees, no orphaned branches, no leftover
> windows. It's that lightweight.

**Beat — mention recovery:**

> One more thing. Bay uses tmux, so your sessions survive if you
> close your terminal or disconnect. But even after a full reboot —
> tmux is gone, everything is gone — `bay recover` reconstructs all
> your sessions, relaunches your agents with --continue, and you're
> back where you were.

---

## 6. Wrap (15s)

> To get started: `bay setup` from your terminal. Then `bay ws new`
> from any repo — zero config needed. It works with Claude, Codex,
> Gemini, or custom agents. And it scales to multiple repos in one
> dock.

---

## Timing guide

| Section          | Target |
|------------------|--------|
| Problem          | 0:45   |
| Dashboard        | 1:45   |
| Navigate         | 2:30   |
| Create           | 3:30   |
| Cleanup          | 4:15   |
| Wrap             | 4:30   |

Buffer for fumbles / questions: ~30s.

## Notes

- `Option+R` jumps to the next waiting agent ("ready" for input).
- `Option+C` and `Option+W` are real bay keybindings, not aliases.
- If someone asks about editors: "bay edit opens your workspace in
  Cursor, VS Code, nvim, whatever you use. Terminal editors get
  tracked as surfaces; GUI editors just launch."
- If someone asks about multiple repos: "One dock can hold workspaces
  from different repos. `bay ws new --repo backend` alongside your
  frontend workspaces."
