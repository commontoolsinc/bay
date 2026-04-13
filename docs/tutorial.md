# Bay — Tutorial

You're working on a bug fix when you realize the auth refactor should
really be a separate PR. Or a teammate asks for a review while you've
got uncommitted changes and a dev server running. Either way, you need
multiple copies of the repo, each on its own branch, each with their
own state — without stashing, cloning, or losing your place.

Bay gives each task its own **workspace** — an isolated git worktree
inside a **dock** (a tmux session for one project). Each workspace has
**surfaces**: the shells, AI agents, and editors you work in and
navigate between. Jump between tasks instantly, and pick up right
where you left off — even after a reboot.

This tutorial walks through the full lifecycle in about 10 minutes.

**You'll need:** bay (install with `go install ./cmd/bay`), tmux, and git.

**Cleaning up:** When you're done, `bay repo remove bay-tutorial --force`
removes all workspaces and bay state. Then `rm -rf /tmp/bay-tutorial`
removes the repo itself.

---

## 1. Create your first workspace

Grab a throwaway repo to experiment with. If you're already inside
tmux, open a new terminal window first — bay creates its own tmux
session and you'll attach to it in a moment.

```
git clone https://github.com/octocat/Hello-World.git /tmp/bay-tutorial
cd /tmp/bay-tutorial
bay ws new
```

Bay creates a workspace with a clean copy of your default branch.
Attach to the dock:

```
tmux attach -t bay-tutorial
```

You're now in a tmux session managed by bay, with a shell cd'd into
your workspace's worktree. Run `bay ws ls` to see what bay created:

```
bay ws ls
```

```
rp bay-tutorial
  dk bay-tutorial *
    ws w1 *  n=1
```

One repo, one dock, one workspace with a shell surface. Everything
you do from here — creating more workspaces, adding shells,
navigating — happens inside this session.

> **What just happened?** Bay detected your git repo, created a
> worktree at `/tmp/bay-tutorial-worktrees/<name>`, and started
> a tmux session called `bay-tutorial` with a window for your
> workspace. The worktree starts on detached HEAD — it has the
> default branch's content but isn't on any named branch yet. Bay
> tracks all of this in its state file at
> `~/.local/share/bay/manifest.json` — you don't need to touch it.

## 2. Work in your workspace

Your workspace is a normal directory with a normal git checkout. To
start working on a branch, use `--branch` when creating a workspace:

```
bay ws new --branch fix/login-bug
```

That creates a second workspace on the `fix/login-bug` branch (or
checks it out if it already exists on the remote).
Within a few seconds the tmux tab name updates to `login-bug` — bay
watches your branch in the background and keeps the name in sync.

You can also create a branch manually in an existing workspace with
`git checkout -b`, and bay picks up the change the same way.

Now you realize the auth refactor should be its own PR. Create a third
workspace:

```
bay ws new --branch fix/auth-refactor
```

Each workspace is completely independent — different branch, different
files, different tmux tab. Your login bug work is untouched.

A teammate pings you for a review? Create a workspace for it:

```
bay ws new review
```

That creates a workspace with no branch — useful for scratch work,
exploring code, or running tests against main.

To see all workspaces in the dock, use `bay ws ls`:

```
bay ws ls
```

```
rp bay-tutorial
  dk bay-tutorial *
    ws w1             n=1
    ws login-bug      br=fix/login-bug  n=1
    ws auth-refactor  br=fix/auth-refactor  n=1
    ws review *       n=1
```

Four workspaces in creation order. The two with `--branch` show their
branch; `review` and `w1` have no branch. The `*` marks where you
are now.

Switch between them with `Option+Shift+j` / `Option+Shift+k` (requires
`bay setup`) or `bay ws go`.

## 3. Add tools to your workspace

A workspace starts with one shell, but you can add more surfaces.

Split a second shell (for running tests while you edit):

```
bay shell
```

Run a long-lived command in its own pane:

```
bay new cmd "top" monitor
```

(`bay new cmd` creates a `cmd` surface; the second argument is its display
name.)

Open your editor on the workspace directory:

```
bay edit
```

This opens your editor on the workspace's root directory — use the
editor's file browser to navigate within the project. Terminal editors
(nvim, vim) run as tracked surfaces in tmux. GUI editors (Cursor,
VS Code, Zed) launch and return — run `bay edit` (or `Option+e`)
again to switch back to the editor window.

Run `bay ls` to see the current workspace's surfaces:

```
bay ls
```

```
rp bay-tutorial
  dk bay-tutorial *
    ws login-bug *  br=fix/login-bug
      sf shell *    ty=shell
      sf shell-2    ty=shell
      sf monitor    ty=cmd
```

Three surfaces, all navigable. (`bay ws ls` shows all workspaces;
`bay ls` shows the one you're in.)

## 4. Navigate

**Within a workspace**, jump between surfaces:

- `Option+j` / `Option+k` — next / previous surface
- `Option+g` — fuzzy picker (type to filter, Enter to select)

**Between workspaces**:

- `Option+Shift+j` / `Option+Shift+k` — next / previous workspace
- `Option+Shift+g` — fuzzy picker for workspaces

The pattern: **without Shift = within your workspace, with Shift =
between workspaces.**

All of these work without the Option-key shortcuts too:

```
bay go shell        # jump to the shell surface
bay ws go review    # jump to the review workspace
```

## 5. Add an AI agent

```
bay agent
```

This launches your default agent (bay auto-detects Claude Code, Codex,
or Gemini on your PATH) in its own window. You can also create a
workspace with an agent as the first surface: `bay ws new --agent`.

Add a shell alongside it
with `Option+s` (or `bay shell`).

When the agent waits for your input, bay highlights it. Use `Option+j`
/ `Option+k` to cycle between surfaces, or `Option+g` to pick one.

## 6. Inspect

To see every workspace and surface at once, use `bay tree`:

```
bay tree
```

```
rp bay-tutorial
  dk bay-tutorial *
    ws login-bug *  br=fix/login-bug
      sf shell    ty=shell
      sf monitor  ty=cmd
    ws review
      sf agent    ty=agent ag=claude
      sf shell    ty=shell
```

Other useful commands:

- `bay pwd` — where am I? (repo, dock, workspace, surface)
- `bay ws show` — detailed info about the current workspace
- `bay doctor` — health checks (keybindings, agents, repos)

## 7. Clean up

Close a workspace when you're done with it:

```
bay ws close review
```

Bay checks for uncommitted changes and unpushed commits first. If
everything is pushed, bay also deletes the local git branch — no
stale branches left behind. Use `--force` to skip the safety checks.

Closing an *individual* agent surface (`bay close agent`) prompts for
confirmation, since agents carry valuable conversation context. Use
`--force` (or `-f`) to skip the prompt.

When a PR is merged, bay detects it in the background. Workspaces with
unmerged branches show `st=pending`. Clean up all finished workspaces:

```
bay ws close --done             # close workspaces not dirty or pending
bay ws close --done --dry-run   # preview first
```

## 8. Recovery

Reboot? Tmux sessions are gone, but bay remembers everything:

```
bay recover
```

Recreates all tmux sessions and surfaces. Agents resume with
`--continue`. Terminal editor surfaces relaunch. GUI editors are
not tracked — reopen them with `bay edit`.

---

## Going further

**Full setup:** `bay setup` installs keybindings and shell completions.
It's optional — everything works without it, but the keybindings make
navigation instant.

**Multiple repos:** `bay repo add backend ~/projects/backend` registers
another repo. Create workspaces from it with `bay ws new --repo backend`.

**Agent awareness:** `bay repo init` adds a one-liner to your project's
`CLAUDE.local.md` (or equivalent) so agents know about bay commands.

**Explicit control:** The zero-config flow creates docks automatically.
For more control: `bay dock new myproject --repo myproject`.

**Status line:** Add `#(bay status-line full)` to your tmux `status-right`
to always see your current workspace. Other field names: `name`, `branch`,
`pr`, `dirty`, `dock`, `merged`.

---

**Next:**

- **[User Guide](human-guide.md)** — full command reference, config
  options, and advanced features.
- **[Agent Reference](agent-reference.md)** — reference for AI agents
  operating inside bay workspaces.
