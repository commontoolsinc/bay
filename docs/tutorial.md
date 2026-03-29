# Bay — Tutorial

Bay automates the plumbing of running multiple AI coding agents at the
same time. If you've ever had three PRs in flight with Claude Code or
Codex, you know the pain: creating git worktrees by hand, opening tmux
windows, keeping track of which agent is where, and redoing it all
after a reboot. Bay handles all of that.

This tutorial walks through the full lifecycle in about 10 minutes
using a throwaway repo. By the end you'll have created workspaces,
navigated between them, and cleaned everything up.

**You'll need**: bay installed (`go install github.com/commontoolsinc/bay/cmd/bay@latest`),
tmux (`brew install tmux`), and git. Optionally fzf (`brew install fzf`)
for the fuzzy picker.

---

## 1. First-time setup

```
bay setup
```

This creates bay's config file at `~/.config/bay/config.toml` with
default agent definitions (Claude Code and Codex). It also offers to
install shell tab completions and tmux keybindings.

Accept the defaults for now — you can change everything later.

## 2. Add a repo

Bay needs to know about the git repos you work in, so it can create
isolated worktrees from them. A **repo** in bay is just a pointer to a
local git checkout on disk.

Let's clone a throwaway repo and register it:

```
bay repo add tutorial ~/projects/bay-tutorial --url https://github.com/octocat/Hello-World.git
```

This clones the repo to `~/projects/bay-tutorial` and tells bay about
it. Bay will create worktrees in `~/projects/bay-tutorial-worktrees/`
(a sibling directory it manages automatically).

You should see:
```
Cloning https://github.com/octocat/Hello-World.git into /Users/you/projects/bay-tutorial...
Repo "tutorial" added (~/projects/bay-tutorial)
```

You can also register a repo you've already cloned:
`bay repo add myproject ~/projects/myproject`

## 3. Create a dock

A **dock** is a tmux session that groups workspaces for a project. Think
of it as a named environment: one dock per project or per workflow.
Each dock has defaults — which repo to use and which agent to launch —
so you don't repeat yourself every time you create a workspace.

```
bay dock new tutorial --repo tutorial --agent claude
```

This creates a tmux session called `tutorial` and saves the dock
config. You should see:
```
Dock "tutorial" created.
```

## 4. Attach to the dock

Tmux sessions run in the background. To interact with one, you attach
your terminal to it:

```
tmux attach -t tutorial
```

You're now inside the `tutorial` tmux session. Look at the status bar
at the bottom — you'll see a single window named `~`. That's a
**placeholder** that keeps the session alive while you have no
workspaces yet. It disappears automatically in the next step.

*Everything below happens inside this tmux session.*

## 5. Create a workspace

A **workspace** is an isolated copy of your repo — a git worktree —
with its own tmux window. Each workspace gets its own branch, its own
files, and its own agent session. This is why bay exists: instead of
switching branches in one checkout, you have multiple independent
checkouts running in parallel.

```
bay ws new
```

Bay infers the dock from your current tmux session, creates a worktree
from the repo, opens a new tmux window, and (in a real setup) launches
your agent. The `~` placeholder disappears because you now have a real
window.

```
Workspace w1 created in dock tutorial (path: ~/projects/bay-tutorial-worktrees/w1)
```

The workspace got the ID `w1` — bay assigns these sequentially per
dock. The tmux window is also named `w1` for now. That name will
update automatically when you set a branch.

## 6. See what you have

```
bay ls
```

This shows the full hierarchy — repos, docks, and workspaces:

```
repo tutorial (~/projects/bay-tutorial)
  dock tutorial
    w1    w1                   —                        idle    claude
```

The workspace is `idle` (no branch yet), using `claude` as the agent.
The dashes mean no branch and no PR number are set.

## 7. Update workspace metadata

When an agent (or you) creates a branch or opens a PR, bay should know
about it. Bay tracks this metadata separately from git — it's used for
window naming, navigation, and listing. It doesn't modify git state.

```
bay ws update self --branch test-branch --pr 1
```

`self` means "the workspace I'm currently in." Look at the tmux status
bar — the window name changed from `w1` to `test-branch` (bay
auto-abbreviates branch names, stripping prefixes like `feature/`).

Run `bay ls` again to see the updated metadata:

```
repo tutorial (~/projects/bay-tutorial)
  dock tutorial
    w1    test-branch          test-branch       #1     active  claude
```

The status changed from `idle` to `active` automatically because you
set a branch.

## 8. Open a second window

A workspace can have multiple **windows** — separate tmux tabs that
share the same worktree. This is useful when you want an agent in one
window and a shell in another, both looking at the same code.

```
bay win open w1 --shell
```

The status bar now shows `test-branch` and `test-branch:2`. Both
windows are in the same worktree directory. Switch between them with
the normal tmux keys (`Ctrl-b n` for next, `Ctrl-b p` for previous).

## 9. Navigate with bay go

As you accumulate workspaces across docks, tmux's built-in navigation
gets unwieldy. Bay provides fuzzy search:

```
bay go
```

If you have fzf installed, this opens a picker showing all windows
across all docks. You can also jump directly: `bay go test-branch`.

## 10. Inspect a workspace

```
bay ws show self
```

Shows the full details:

```
Workspace: test-branch (w1)
  Dock:   tutorial
  Type:   worktree
  Path:   ~/projects/bay-tutorial-worktrees/w1
  Branch: test-branch
  PR:     1
  Status: active
  Windows: 2
    [1] test-branch (tmux: @N, panes: 1)
    [2] test-branch:2 (tmux: @N, panes: 1)
```

## 11. Rename a workspace

Display names default to the abbreviated branch, but you can override:

```
bay ws rename w1 my-feature
```

Both tmux windows update immediately to `my-feature` and
`my-feature:2`. This name sticks — future branch updates won't
overwrite it.

## 12. Create a second workspace

```
bay ws new
```

Creates `w2` with its own worktree. The status bar now shows three
windows: `my-feature`, `my-feature:2`, and `w2`. Each workspace is
completely independent — different worktree, different branch, different
agent session.

```
Workspace w2 created in dock tutorial (path: ~/projects/bay-tutorial-worktrees/w2)
```

## 13. Close the second workspace

```
bay ws close w2
```

Bay checks for uncommitted changes and unpushed commits before
removing a worktree. This one is clean, so it closes immediately —
the worktree is deleted and the tmux window disappears.

If it refused (because you had unsaved work), you'd use
`bay ws close w2 --force` to override.

## 14. Close the first workspace

```
bay ws close w1 --force
```

`--force` skips the safety checks. Both windows for `my-feature`
close.

## 15. The placeholder window

Look at the status bar — there's a `~` window again. When you close
the last workspace in a dock, bay creates this placeholder to keep the
tmux session alive. Without it, tmux would destroy the session and
your terminal would disconnect.

The placeholder disappears automatically the next time you create a
workspace. If you type in it, bay leaves it alone as a regular shell.

## 16. Tear down the dock

```
bay dock close tutorial
```

This closes all workspaces (there are none), kills the tmux session,
and you'll be detached back to your regular terminal.

## 17. Remove the repo

```
bay repo remove tutorial
```

Removes the repo from bay's config. Since the dock is already gone,
this succeeds cleanly.

```
Repo "tutorial" removed.
```

## 18. Verify everything is clean

```
bay ls
```

Should show nothing (or just your other projects if you have any).

Optionally delete the cloned files:
```
rm -rf ~/projects/bay-tutorial ~/projects/bay-tutorial-worktrees
```

---

## What's next

- **[User Guide](human-guide.md)** — full command reference,
  configuration, templates, waiting detection, recovery, and
  troubleshooting.
- **[Agent Reference](agent-reference.md)** — comprehensive reference
  for AI agents operating inside bay workspaces.
