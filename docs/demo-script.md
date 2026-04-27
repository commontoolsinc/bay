# Bay Demo Script (~5 min)

Live screenshare. Cold open with a live create/destroy, then walk
through a pre-built dock.

## Pre-demo setup

Have a dock (e.g., `labs-demo`) with 3-4 workspaces ready:

| Workspace      | Branch              | Status   | Surfaces        | Notes                          |
|----------------|---------------------|----------|-----------------|--------------------------------|
| `auth-fix`     | `fix/auth-header`   | active   | agent, shell    | Agent running, not waiting     |
| `cache-ttl`    | `fix/cache-ttl`     | active   | agent           | Agent waiting for input (highlighted) |
| `dep-update`   | `chore/dep-update`  | merged   | shell           | PR merged, auto-detected       |
| `api-logging`  | `feat/api-logging`  | active   | agent, shell    | Normal active work             |

- Monitor running (`bay monitor start`)
- tmux status line configured
- A branch `fix/rate-limit` exists in the repo with no workspace
  attached (for the cold-open tab-complete)
- Start focused on a shell in the demo dock (not inside a workspace)
  — the demo dock must be the *current* dock so `bay dk tree` defaults
  to it

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
bay dk tree
```

> Agent running. There it is in the tree.

**Do:** Close it.

```
bay ws close rate-limit
bay dk tree
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

## 3. At Scale — The Dashboard (90s)

**Do:** Run `bay dk tree`

> This is my actual working state. Four things going on. Bay shows
> me each one — workspace name, branch, PR number, surfaces.

**Walk through the output:**

> `auth-fix` — active, working on an auth header bug. Has an agent
> and a shell.
>
> `cache-ttl` — see the hourglass next to it? That means the agent
> stopped to ask me something. I didn't go check. Bay runs a
> background monitor that watches every agent across every
> workspace, and the moment one waits for input it gets flagged
> here and its tab in the tmux status bar lights up. I have six
> agents running at any given time — I don't poll them. They tell
> me when they need me.
>
> `api-logging` — normal active work.
>
> And then `dep-update`. Look at the dock summary line — `(1 merged)`.
> That's a fix I shipped a while back; the PR landed on main. I
> didn't mark anything. Bay watches the merge state of every branch
> and flips that flag the moment the commit lands upstream. These
> aren't statuses I maintain — they're statuses bay maintains for
> me, so I can just look and know what's safe to throw away.

**Do:** Run `bay pwd`

> And bay always knows where I am — which dock, which workspace,
> which surface. No guessing.

---

## 4. Navigate (45s)

**Do:** Hit `Option+h` a few times — cycle back through windows.

> Option-h and l cycle through tmux windows (vim-style left/right —
> tabs run horizontally across the status bar). Every surface — agent,
> shell, editor — is a window, so this walks through all of them
> regardless of which workspace they belong to.

**Do:** Hit `Option+g` — the workspace picker pops up.

> Once you have more than a few workspaces, cycling gets tedious.
> Option-g is the fuzzy picker — filter by name or description, Enter
> to jump.

**Do:** Hit `Option+r`. Pause — let the audience see it land on
`cache-ttl`.

> This is the one I use most. Option-r jumps straight to the next
> agent that's waiting for me. I have six agents running — I don't
> check on them. When one needs me, I hit one key and I'm there.

---

## 5. Bulk Create + Throw Away (30s)

**Do:** Hit `Option+C` three times in quick succession. Let each one
create with default names.

```
bay dk tree
```

> Three new workspaces. Each has its own worktree, branch, and tab.
> Option-C is real keybindings, not demo tricks — that's how I
> actually create workspaces day to day.

**Do:** Close them — `Option+w` twice on each tab (first tap triggers
the last-surface confirmation, second confirms), or `bay ws close
<name>`. Then close the merged one too: `bay ws close dep-update`.

```
bay dk tree
```

> All gone. Worktrees deleted, branches deleted, tabs closed. Bay's
> light enough that I can create when I need it and throw it away
> when I'm done.

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
| Dashboard            | 2:45   |
| Navigate             | 3:30   |
| Bulk Create/Throw Away | 4:00 |
| Wrap                 | 4:20   |

## Notes

- `Option+r` jumps to the next waiting agent ("ready" for input).
- `Option+C` (create) and `Option+w` (close current surface) are real
  bay keybindings, not aliases. `Option+w` on the only surface in a
  workspace tears down the workspace itself, after a one-tap-to-confirm
  guard.
- If someone asks about editors: "bay edit opens your workspace in
  Cursor, VS Code, nvim, whatever you use. Terminal editors get
  tracked as surfaces; GUI editors just launch."
- If someone asks about multiple repos: "One dock can hold workspaces
  from different repos. `bay ws new --repo backend` alongside your
  frontend workspaces."
