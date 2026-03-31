# Bay — Tutorial

Bay manages multiple git worktrees and tmux windows so you can work on
several tasks at once without the mess. If you've ever had three PRs in
flight, you know the pain: creating worktrees by hand, opening tmux
windows, keeping track of which is where, and redoing it all after a
reboot. Bay handles all of that — and if you want an AI agent in any of
those windows, that's one flag away.

This tutorial walks through the full lifecycle in about 10 minutes
using a throwaway repo. By the end you'll have created workspaces,
opened shells and editors, navigated between them, and cleaned
everything up.

**You'll need:**

- **bay** — `go install github.com/commontoolsinc/bay/cmd/bay@latest`
- **tmux** — `brew install tmux`
- **git**
- **fzf** (optional) — `brew install fzf` for the fuzzy picker

---

## 1. First-time setup

```
bay setup
```

This creates bay's config file at `~/.config/bay/config.toml` with
default agent definitions (Claude Code, Codex, and Gemini). It also offers to
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
Each dock has defaults — which repo to use and which agent to launch
when one is requested — so you don't repeat yourself.

```
bay dock new tutorial --repo tutorial --agent claude
```

This creates a tmux session called `tutorial` and saves the dock
config. The `--agent claude` sets the *default* agent for when you
explicitly ask for one — it doesn't mean every window gets an agent.

You should see:
```
Dock "tutorial" created.
```

## 4. Attach to the dock

Tmux sessions run in the background. To interact with one, you attach
your terminal to it:

```
tmux attach -t tutorial
```

If you're new to tmux, the
[official Getting Started guide](https://github.com/tmux/tmux/wiki/Getting-Started)
covers the basics — windows, panes, and key bindings. Bay handles most
tmux operations for you, but knowing `Ctrl-b n` (next window) and
`Ctrl-b p` (previous window) is useful.

You're now inside the `tutorial` tmux session. Look at the status bar
at the bottom — you'll see a single window named `~`. That's a
**placeholder** that keeps the session alive while you have no
workspaces yet. It disappears automatically in the next step.

*Everything below happens inside this tmux session.*

## 5. Create a workspace

A **workspace** is an isolated copy of your repo — a git worktree —
with its own tmux window. Each workspace gets its own branch and its
own files. This is why bay exists: instead of switching branches in one
checkout, you have multiple independent checkouts running in parallel.

```
bay ws new
```

Bay infers the dock from your current tmux session, creates a worktree
from the repo, and opens a new tmux window with a **shell** in it.
The `~` placeholder disappears because you now have a real window.

```
Workspace w1 created in dock tutorial (path: ~/projects/bay-tutorial-worktrees/w1)
```

The workspace got the ID `w1` — bay assigns these sequentially per
dock. The tmux window is also named `w1` for now. That name will
update automatically when you set a branch.

You're dropped into a regular shell, cd'd into the worktree. You can
run git commands, build, test — whatever you'd normally do.

## 6. Open it in your editor

```
bay edit
```

This opens the current workspace's worktree in your editor. Bay
auto-detects cursor, VS Code, zed, nvim, or vim — or you can set
one explicitly:

```
bay edit --set cursor       # save preference
bay edit --show             # see which editor would be used
```

You can also target a specific workspace: `bay edit w1`.

## 7. See what you have

```
bay ls
```

This shows the full hierarchy — repos, docks, and workspaces:

```
repo tutorial (~/projects/bay-tutorial)
  dock tutorial
    ID NAME BRANCH PR STATUS AGENT
    w1 w1   —         idle   shell
```

The column headers make the layout clear: `w1` is both the ID and the
display name (they match until you set a branch). The workspace is
`idle` (no branch yet), and `shell` shows it's running a shell, not
an agent.

## 8. Create a branch

Bay automatically detects your git branch. Create one in the
worktree:

```
git checkout -b test-branch
```

Now look at the tmux window tab at the bottom — the name changed from
`w1` to `test-branch`. Bay detected the new branch and updated it
automatically (stripping prefixes like `feature/`).

Run `bay ls` to confirm:

```
repo tutorial (~/projects/bay-tutorial)
  dock tutorial
    ID NAME        BRANCH      PR STATUS AGENT
    w1 test-branch test-branch    active shell
```

The status changed from `idle` to `active` automatically because a
branch appeared. No `bay ws update` needed — bay reads the branch
from git directly.

You can still manually set a PR number or status:

```
bay ws update self --pr 1
bay ws update self --status done
```

PR numbers and status aren't in git, so those stay manual.

## 9. Open a shell in a split pane

Need a quick shell alongside what you're doing? Use `bay shell`:

```
bay shell
```

This opens a split pane in the current window, cd'd into the same
worktree. Great for running tests or git commands while keeping your
main session visible.

To open a shell as a full new window instead:

```
bay shell test-branch
```

Passing a workspace name creates a new tmux window rather than a
split pane.

## 10. Open a second window

A workspace can have multiple **windows** — separate tmux tabs that
share the same worktree. `bay shell` is the quick way; `bay win open`
gives you more control:

```
bay win open w1 --shell
```

The status bar now shows `test-branch` and `test-branch:2`. Both
windows are in the same worktree directory. Switch between them with
the normal tmux keys (`Ctrl-b n` for next, `Ctrl-b p` for previous).

## 11. Navigate with bay go

As you accumulate workspaces across docks, tmux's built-in navigation
gets unwieldy. Bay provides fuzzy search:

```
bay go
```

If you have fzf installed, this opens a picker showing all windows
across all docks, with type tags so you can see what's running:

```
  tutorial/test-branch       [shell]
  tutorial/test-branch:2     [shell]
  tutorial/test-branch:3     [shell]
```

Tags include `[agent]`, `[shell]`, and `[cmd]` so you can tell at a
glance what each window is doing.

You can also jump directly: `bay go test-branch`.

## 12. Inspect a workspace

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
  Windows: 3
    [1] test-branch (tmux: @N, panes: 1)
    [2] test-branch:2 (tmux: @N, panes: 1)
    [3] test-branch:3 (tmux: @N, panes: 1)
```

## 13. Rename a workspace

Display names default to the abbreviated branch, but you can override:

```
bay ws rename w1 my-feature
```

All tmux windows for this workspace update immediately. This name
sticks — future branch updates won't overwrite it.

## 14. Launch an agent (opt-in)

Agents are one of the things you can run in a workspace. To create a
workspace with the dock's default agent:

```
bay ws new --agent
```

This creates workspace `w2` with its own worktree and launches the
agent (in this case, `claude` as set in the dock defaults). You can
also specify a different agent: `bay ws new --agent codex`.

The agent runs in its own tmux window just like a shell — you can
switch to it, open additional shell windows alongside it, or use
`bay edit` to work on the same files in your editor.

## 15. Create a second workspace (shell)

```
bay ws new
```

Creates `w3` with its own worktree and a shell. The status bar now
shows windows for both `my-feature` and the new workspace. Each is
completely independent — different worktree, different branch,
different files.

```
Workspace w3 created in dock tutorial (path: ~/projects/bay-tutorial-worktrees/w3)
```

## 16. Close workspaces

```
bay ws close w3
```

Bay checks for uncommitted changes and unpushed commits before
removing a worktree. This one is clean, so it closes immediately —
the worktree is deleted and the tmux window disappears.

If it refused (because you had unsaved work), you'd use
`bay ws close w3 --force` to override.

Close the remaining workspaces:

```
bay ws close w2 --force
bay ws close w1 --force
```

`--force` skips the safety checks. All windows for each workspace
close together.

Look at the status bar — there's a `~` window again. When you close
the last workspace in a dock, bay creates this placeholder to keep the
tmux session alive.

## 17. Tear down the dock and repo

```
bay dock close tutorial
```

This closes all workspaces (there are none), kills the tmux session,
and you'll be detached back to your regular terminal.

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

## Tips

- **`bay status-line`** — outputs workspace info formatted for your
  tmux status bar. Add `#(bay status-line)` to your
  `status-right` in `~/.tmux.conf` to always see which workspace
  you're in.

## Bonus: cross-repo workspaces

A dock has a default repo, but you can create workspaces from different
repos in the same dock using `--repo`:

```
bay repo add backend ~/projects/backend
bay ws new tutorial --name api-work --repo backend
```

This creates a worktree from the `backend` repo inside the `tutorial`
dock. Both workspaces appear in the same tmux session and `bay ls`
output. This is handy when a feature spans multiple repositories.

## What's next

- **[User Guide](human-guide.md)** — full command reference,
  configuration, templates, waiting detection, recovery, and
  troubleshooting.
- **[Agent Reference](agent-reference.md)** — comprehensive reference
  for AI agents operating inside bay workspaces.
