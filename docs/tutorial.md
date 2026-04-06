# Bay -- Tutorial

Bay manages multiple git worktrees and tmux surfaces so you can work on
several tasks at once without the mess. If you've ever had three PRs in
flight, you know the pain. Bay handles worktrees, tmux sessions,
navigation, and recovery -- and if you want an AI agent in any of those
workspaces, that's one flag away.

This tutorial walks through the full lifecycle in about 10 minutes.

**You'll need:** bay, tmux, and git. No other dependencies -- bay has a
built-in picker.

---

## 1. Zero-config first use

Bay works out of the box in any git repo:

```
git clone https://github.com/octocat/Hello-World.git ~/projects/bay-tutorial
cd ~/projects/bay-tutorial
bay ws new
```

Bay detects the git repo, auto-creates a dock and repo config entry,
probes your PATH for known agents (claude, codex, gemini), and opens a
workspace with a shell:

```
Auto-configured: repo bay-tutorial, dock bay-tutorial, agent claude
Workspace w1 created in dock bay-tutorial (path: ~/projects/bay-tutorial-worktrees/w1)
```

You're now inside a tmux session with a shell cd'd into a fresh
worktree. The workspace is named `w1` for now -- it updates
automatically when you create a branch.

## 2. Full setup (optional)

Zero-config gets you going. `bay setup` unlocks the full experience:
keybindings, completions, editor config, and the bay-focus helper for
macOS Space switching.

```
bay setup
```

Accept the defaults -- you can change everything later.

### Keybindings

All use the Option key (Meta), no tmux prefix required:

| Key | Action |
|-----|--------|
| `M-j` / `M-k` | Next/prev surface (within workspace) |
| `M-J` / `M-K` | Next/prev workspace (within dock) |
| `M-g` | Surface picker (within workspace) |
| `M-G` | Workspace picker (within dock) |
| `M-a` / `M-A` | Next waiting surface / workspace |
| `M-1`..`M-9` | Jump to surface by index |
| `M-s` | Split a shell pane |
| `M-w` | Close current pane |

Lowercase = within workspace. Uppercase = across workspaces.

## 3. Workspaces

A **workspace** is an isolated copy of your repo -- a git worktree --
with its own surfaces. Each workspace gets its own branch and files.

```
bay ws new
```

Creates `w2` with its own worktree. Both workspaces are completely
independent -- different worktree, different branch, different files.

Create a branch and watch the tmux tab update:

```
git checkout -b test-branch
```

The workspace name changes from `w2` to `test-branch` automatically.
You can manually set metadata too:

```
bay ws update self --pr 42
bay ws update self --status done
```

## 4. Surfaces

A **surface** is anything you can focus and jump to inside a workspace:
a shell, an agent, a command, or an editor. No more thinking in tmux
windows and panes -- just named, navigable surfaces.

### Split a shell

```
bay shell
```

Opens a split pane cd'd into the same worktree. Or `M-s` for the
keybinding. For a new tmux window instead: `bay shell --window`.

### Add a command surface

```
bay sf new --cmd "npm run dev" --name server
```

`sf` is short for `surface`.

## 5. Editing

```
bay edit
```

Opens the worktree in your editor (auto-detects cursor, VS Code, zed,
nvim, vim). Set a preference with `bay edit --set cursor`.

For GUI editors, bay creates a tracked **editor surface** -- the
editor window appears in `bay go` and the surface picker. You can jump
between agent, shell, and editor using the same keybindings.

## 6. Launch an agent

```
bay ws new --agent
```

Creates a workspace with the dock's default agent. Specify a different
one with `bay ws new --agent codex`. The agent runs in its own
surface. Open shells alongside it, or `bay edit` the same files in
your editor.

If the agent needs restarting, `bay restart` reconnects with
`--continue` so you don't lose your conversation.

## 7. Navigation

Two levels: surfaces within a workspace, workspaces within a dock.

### Surface navigation (within workspace)

```
bay go                  # pick from surfaces
bay go shell            # jump to the shell surface
bay go --index 2        # jump to surface #2
bay go --next-waiting   # jump to next waiting agent
```

Or use `M-g` for the picker, `M-j`/`M-k` to cycle.

### Workspace navigation (within dock)

```
bay ws go               # pick from workspaces
bay ws go auth-fix      # jump directly
bay ws go --next-waiting  # next workspace with waiting agent
```

Or use `M-G` for the picker, `M-J`/`M-K` to cycle.

### Waiting detection

When an agent waits for input, bay marks it "WAITING" in pickers and
the status line. `M-a` jumps to the next waiting surface; `M-A` jumps
to the next workspace with a waiting agent.

## 8. See what you have

```
bay ls
```

Shows the hierarchy focused on your current workspace:

```
repo bay-tutorial
  dock bay-tutorial
    workspace test-branch status=active
      surface 1 agent  type=agent
      surface 2 shell  type=shell
    workspace w1 status=idle
      surface 1 shell  type=shell
```

Use `bay tree` for the full tree. Use `bay pwd` for a quick location
check. Use `bay ws show self` for detailed workspace info including
path, branch, PR, status, and all surfaces (`--json` for machine
output).

## 9. Agent awareness

Tell AI agents about bay so they can navigate workspaces:

```
bay repo init
```

Idempotent. For repos with `CLAUDE.md`, appends a one-liner pointing
to `bay agent-guide`. Sets up `.worktreeinclude` so gitignored files
(`.env`) get copied into new worktrees. `bay doctor` flags repos
missing bay awareness.

## 10. Explicit setup

For more control than zero-config:

```
bay repo add myproject ~/projects/myproject
bay dock new myproject --repo myproject --agent claude
tmux attach -t myproject
```

A **dock** is a tmux session grouping workspaces for a project. Each
dock has defaults (repo, agent) so you don't repeat yourself.

Cross-repo workspaces work too -- `bay ws new --repo backend` creates
a workspace from a different repo in the same dock.

## 11. Recovery

After a reboot, your tmux sessions are gone but bay remembers:

```
bay recover
```

Recreates tmux sessions, windows, and panes for every dock and
workspace. Agents restart with `--continue` so conversations resume.
GUI editors and dock host terminals relaunch too.

Inside a dock, `bay recover` scopes to just that dock. Outside tmux,
it recovers everything.

## 12. Cleanup

```
bay ws close w1
```

Bay checks for uncommitted changes first. Override with `--force`.

When a PR is merged, bay auto-detects it and marks the workspace
"done." Clean up all merged workspaces at once:

```
bay ws close --done
```

Tear down everything:

```
bay repo remove bay-tutorial --force
```

This closes all workspaces, removes the dock and repo from config, and
kills the tmux session. Bay leaves the repo on disk -- clean it up
manually: `rm -rf ~/projects/bay-tutorial`.

Verify: `bay ls` should show nothing.

---

## Tips

- **`bay status-line`** -- add `#(bay status-line)` to your tmux
  `status-right` to always see your current workspace.

- **`bay doctor`** -- health checks. Flags missing keybindings,
  unconfigured editors, repos without bay awareness, macOS features.

- **Workspace identity is by name.** Once a branch is detected, the
  name updates to match. Override with `bay ws rename`.

## What's next

- **[User Guide](human-guide.md)** -- full command reference,
  configuration, templates, waiting detection, recovery, and
  troubleshooting.
- **[Agent Reference](agent-reference.md)** -- comprehensive reference
  for AI agents operating inside bay workspaces.
