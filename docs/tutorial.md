# Bay -- Hands-on Tutorial

Walk through the full bay lifecycle in about 10 minutes using a
throwaway repo.

## What you'll learn

- Adding a repo and creating a dock
- Creating, inspecting, and navigating workspaces
- Updating metadata, renaming, closing, and cleaning up

## Prerequisites

bay (`go install github.com/commontoolsinc/bay/cmd/bay@latest`),
tmux (`brew install tmux`), and git.

## 1. Run setup

```
bay setup
```
Creates `~/.config/bay/config.toml` with default agent definitions and
prompt patterns. Accept the defaults or customize as you go.
```
Config written to /Users/you/.config/bay/config.toml
Setup complete.
```

## 2. Add a repo

```
bay repo add tutorial ~/projects/bay-tutorial --url https://github.com/octocat/Hello-World.git
```
Clones the public Hello-World repo and registers it in bay's config.
```
Cloning https://github.com/octocat/Hello-World.git into ~/projects/bay-tutorial...
Repo "tutorial" added (~/projects/bay-tutorial)
```

## 3. Create a dock

```
bay dock new tutorial --repo tutorial --agent claude
```
Adds a `[docks.tutorial]` entry to config and starts a tmux session.
```
Dock "tutorial" created.
```

## 4. Attach to the dock

```
tmux attach -t tutorial
```
You are now inside the `tutorial` tmux session. The status bar shows a
single placeholder window named `~`. Everything below happens inside
tmux.

## 5. Create a workspace

```
bay ws new
```
Creates a git worktree and opens a window. Bay infers the dock from
your current tmux session.
```
Workspace w1 created in dock tutorial (path: ~/projects/bay-tutorial-worktrees/w1)
```
The `~` placeholder disappears and you land in `w1`.

## 6. Check the listing

```
bay ls
```
```
repo tutorial (~/projects/bay-tutorial)
  dock tutorial
    w1    w1                    --                             --     idle     claude
```

## 7. Update workspace metadata

```
bay ws update self --branch test-branch --pr 1
```
Sets branch and PR in bay's manifest (metadata only -- no git changes).
The tmux window title changes from `w1` to `test-branch`.

## 8. Open a second window

```
bay win open w1 --shell
```
Opens a shell sharing the same worktree. The status bar now shows
`test-branch` and `test-branch:2`.

## 9. Navigate

```
bay go
```
Opens an fzf picker of all windows. You can also jump directly:
`bay go test-branch`.

## 10. Inspect the workspace

```
bay ws show self
```
```
Workspace: test-branch (w1)
  Dock:   tutorial
  Type:   worktree
  Path:   ~/projects/bay-tutorial-worktrees/w1
  Branch: test-branch
  PR:     1
  Status: idle
  Windows: 2
    [1] test-branch (tmux: @1, panes: 1)
    [2] test-branch:2 (tmux: @2, panes: 1)
```

## 11. Rename the workspace

```
bay ws rename w1 my-feature
```
Overrides the auto-generated name. Window titles update immediately to
`my-feature` and `my-feature:2`.

## 12. Create a second workspace

```
bay ws new
```
Creates `w2` with its own worktree. The status bar now shows three
windows: `my-feature`, `my-feature:2`, and `w2`.
```
Workspace w2 created in dock tutorial (path: ~/projects/bay-tutorial-worktrees/w2)
```

## 13. Close the second workspace

```
bay ws close w2
```
Removes the worktree and closes the window. Clean checkout, so no
`--force` needed.

## 14. Close the first workspace

```
bay ws close w1 --force
```
`--force` skips safety checks. Both windows for `my-feature` close.

## 15. Notice the placeholder

After closing the last workspace, bay creates a `~` placeholder window
to keep the tmux session alive. This is expected.

## 16. Remove the dock

```
bay dock close tutorial
```
Tears down the tmux session. You will be detached from tmux.

## 17. Remove the repo

```
bay repo remove tutorial
```
Removes the repo from config.
```
Repo "tutorial" removed.
```

## 18. Verify everything is clean

```
bay ls
```
Should show nothing. Optionally delete the clone:
```
rm -rf ~/projects/bay-tutorial ~/projects/bay-tutorial-worktrees
```

## What's next

- **[User Guide](human-guide.md)** -- commands, config, templates,
  monitoring, and recovery.
- **[Agent Guide](agent-guide.md)** -- how AI agents use bay from
  inside workspaces.
