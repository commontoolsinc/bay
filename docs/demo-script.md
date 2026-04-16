# Bay Demo Script (~5 min)

Live screenshare. Cold open with a live create/destroy, then walk
through a pre-built dock.

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
- A branch `fix/rate-limit` exists in the repo with no workspace
  attached (for the cold-open tab-complete)
- Start focused on a shell in the dock (not inside a workspace)

---

## 1. Cold Open (30s)

No preamble. One sentence of context, then just do it.

> I'm inside a tmux session that bay manages. Let me pick up a
> branch I was working on earlier.

**Do:** Type `bay ws new --branch ` and tab-complete. Let the
audience see the branch list appear, select `fix/rate-limit`.

```
bay ws new --branch fix/rate-limit
```

> New worktree, new tmux window.

**Gesture at the tmux status bar** — a new window appeared.

**Do:** Launch an agent and show the tree.

```
bay agent
bay tree
```

> Agent running. There it is in the tree.

**Do:** Close it.

```
bay ws close rate-limit
bay tree
```

> Gone. Branch, worktree, window, agent — all cleaned up.

---

## 2. Why This Matters (45s)

> Here's how I actually work. I have a bunch of tabs open — one per
> PR, one per workstream. Each tab is its own conversation with its
> own agent. My brain wants separate conversations for separate
> things.
>
> The problem was never the working — it was the setup. Creating a
> worktree, checking out a branch, opening a window, launching an
> agent... that friction adds up. So you start pre-building
> workspaces and maintaining them just in case. You keep more around
> than you need because setting up a new one is a hassle.
>
> Bay makes it cheap enough that you stop doing that. You create a
> workspace when you need one and throw it away when you're done.
> No planning ahead, no hoarding state. And it's all persistently
> managed — reboot your machine, `bay recover`, you're back where
> you were.

---

## 3. At Scale — The Dashboard (60s)

**Do:** Run `bay tree`

> This is my actual working state. I've got four things going on.
> Bay shows me all of them — workspace name, branch, PR number,
> status.

**Walk through the output:**

> `auth-fix` — active, working on an auth header bug. Has an agent
> and a shell.
>
> `cache-ttl` — see that highlight? That means the agent is waiting
> for my input. I didn't check on it. Bay has a background monitor
> that watches all your agents and flags when one needs you.
>
> `dep-update` — status says "done." I didn't set that either. Bay
> noticed the PR merged and marked it automatically. These aren't
> statuses I maintain — bay maintains them for me.
>
> `api-logging` — normal active work.

**Do:** Run `bay pwd`

> Bay always knows where I am — which dock, which workspace, which
> surface. No guessing.

---

## 4. Navigate (45s)

**Do:** Hit `Option+Shift+K` a few times — cycle through workspaces.

> Option-Shift-J and K cycle between workspaces. Each one is its own
> worktree — totally isolated checkout, different branch, different
> state.

**Do:** Within a workspace, hit `Option+J` / `Option+K` to cycle surfaces.

> Within a workspace, Option-J and K cycle between surfaces — agent,
> shell, editor, whatever you've got open.

**Do:** Hit `Option+R`. Pause — let the audience see it land on
`cache-ttl`.

> This is the one I use most. Option-R jumps straight to the next
> agent that's waiting for me. I have six agents running — I don't
> check on them. When one needs me, I hit one key and I'm there.

---

## 5. Bulk Create + Cleanup (60s)

**Do:** Hit `Option+C` three times in quick succession. Let each one
create with default names.

**Do:** Run `bay tree`

> Four new workspaces, just like that. Each one has its own worktree,
> its own branch, its own tmux window. That's real keybindings, not
> demo tricks — Option-C is how you'd actually create workspaces day
> to day.

**Do:** Point at `dep-update` (done status) in the tree.

> This one's done — the PR merged and bay caught it. Let me close it.

**Do:** `bay ws close dep-update` (or Option+W on it).

> Gone. Worktree deleted, branch deleted, tmux window closed.

**Do:** Hit `Option+W` three times to close the three burst-created
workspaces.

**Do:** Run `bay tree`

> All gone. No stale worktrees, no orphaned branches, no leftover
> windows. It's that lightweight — create when you need it, throw
> it away when you're done.

---

## 6. Wrap (20s)

> I've got six agents running right now on six different branches.
> I know exactly which ones need me. And when I'm done, they clean
> up after themselves.
>
> `bay setup` from your terminal, then `bay ws new` from any repo.
> Zero config. Works with Claude, Codex, Gemini, or custom agents.

---

## Timing guide

| Section              | Target |
|----------------------|--------|
| Cold Open            | 0:30   |
| Why This Matters     | 1:15   |
| Dashboard            | 2:15   |
| Navigate             | 3:00   |
| Bulk Create/Cleanup  | 4:00   |
| Wrap                 | 4:20   |

## Notes

- `Option+R` jumps to the next waiting agent ("ready" for input).
- `Option+C` and `Option+W` are real bay keybindings, not aliases.
- If someone asks about editors: "bay edit opens your workspace in
  Cursor, VS Code, nvim, whatever you use. Terminal editors get
  tracked as surfaces; GUI editors just launch."
- If someone asks about multiple repos: "One dock can hold workspaces
  from different repos. `bay ws new --repo backend` alongside your
  frontend workspaces."
