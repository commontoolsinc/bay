# Bay worktree dir visibility

**Status**: implementation brief for Bay.

## Problem

Bay has two useful identities for a workspace:

- **Workspace name**: logical, user-facing, mutable via `bay rename`.
- **Worktree dir tag**: physical, path-derived, e.g. `w4` from
  `/Users/mike/projects/loom-worktrees/w4`.

When an editor is opened for the current workspace, its root is the
physical directory (`w4`). When an editor is opened for the whole dock,
it shows every workspace directory (`w1`, `w2`, `w4`, ...). If the tmux
window was renamed from `w4` to a semantic workspace name, the visible
mapping between the tmux tab and the editor root disappears.

That mapping should be ambient. Requiring `M-?`, `bay pwd`, or an editor
path inspection is too much ceremony for a high-frequency orientation
task.

## Goal

Make the path-derived worktree dir tag visible in the ordinary tmux UI:

- tab/window label
- status-line `dir` field
- status-line `full` field
- flash/popup details as secondary fallback

This is a display affordance. It should not change Bay's identity model:
workspace names remain the logical identifiers used for commands.

## Terms

Use **dir tag** for the display value derived from the workspace path
basename.

Examples:

| path | workspace name | dir tag | compact label |
| --- | --- | --- | --- |
| `/.../loom-worktrees/w4` | `w4` | `w4` | `w4` |
| `/.../loom-worktrees/w4` | `auth-fix` | `w4` | `w4.auth-fix` |
| `/.../loom-worktrees/w12` | `project-tabs` | `w12` | `w12.project-tabs` |

For non-worktree or external workspaces, use the basename of the stored
workspace path if available. If there is no path, omit the dir tag.

## Display Rules

Add a shared helper for workspace display labels:

```text
dir_tag = basename(workspace.path)

if dir_tag is empty:
  label = workspace.name
else if workspace.name is empty or workspace.name == dir_tag:
  label = dir_tag
else:
  label = dir_tag + "." + workspace.name
```

The separator should be a literal dot (`.`), not a spaced separator, to
keep tmux tabs compact.

## Cropping Rules

When a max width is available, crop the workspace-name portion first and
preserve the dir tag.

Desired behavior:

```text
w4.auth-fix
w4.auth
w4.au
```

Rules:

- If `workspace.name == dir_tag`, crop the single label normally.
- If `workspace.name != dir_tag`, preserve `dir_tag + "."` while there
  is enough space.
- Preserve at least two characters of the workspace name when possible:
  `w4.au`.
- If the width is too small for `dir_tag + "." + 2 chars`, fall back to
  the dir tag before truncating the dir tag itself.
- Never emit a dangling separator.

This keeps the physical editor-root anchor visible under aggressive tab
compression while still preserving a hint of the semantic name.

## Status Line

Add a `dir` field:

```bash
bay status-line dir --window '#{window_id}'
```

It should output only the dir tag (`w4`) for the workspace resolved by
tmux window ID, or an empty string outside a workspace.

Update `full` to include the compact label and stop spending default
space on dock/session when the dock is already visible in tmux's
session-name area.

Current help says:

```text
Fields: name, branch, pr, status, dock, merged, full
The full field outputs repo:branch #PR | status.
```

Target shape:

```text
w4.auth-fix feature/auth-fix #123 dirty
```

Under tight widths, truncate the label using the cropping rules above
before truncating branch/PR/status metadata.

Keep `dock` as a separate field for users who want it explicitly.

## Tmux Window Names

Where Bay currently names a tmux window/tab from `workspace.name`, use
the compact label instead:

```text
w4.auth-fix
```

This should apply on:

- workspace create
- workspace rename
- recovery
- sync/repair paths that reconcile Bay metadata with tmux windows

Do not change command targeting: `bay ws go auth-fix` should still match
the workspace name, not require `w4.auth-fix`.

## Flash And Popup

`M-/` (`bay ws show --flash`) should include the compact label in its
short summary.

`M-?` (`bay ws show --popup`) should include the full path. This is a
secondary detail surface, not the primary answer.

Suggested short flash shape:

```text
w4.auth-fix - feature/auth-fix - #123 - dirty
```

## Suggested Implementation Steps

1. Add a helper near existing workspace/status formatting code:
   `WorkspaceDirTag(workspace)` and `WorkspaceCompactLabel(workspace)`.
2. Add unit tests for worktree, renamed worktree, external path, missing
   path, and name-equals-dir cases.
3. Add width/cropping tests for:
   - `w4.auth-fix`
   - `w4.auth`
   - `w4.au`
   - fallback to `w4` when width is too small
4. Extend `bay status-line` field parsing with `dir`.
5. Update `full` to start with `WorkspaceCompactLabel`.
6. Update tmux window rename/create/recover code to use the compact
   label for managed workspace windows.
7. Update `bay status-line --help` text and any docs that enumerate
   status-line fields.

## Acceptance Checks

From a workspace at `/.../loom-worktrees/w4` initially named `w4`:

```bash
bay status-line dir
# w4

bay status-line full
# includes w4
```

After renaming it:

```bash
bay rename auth-fix

bay status-line dir
# w4

bay status-line name
# auth-fix

bay status-line full
# starts with w4.auth-fix
```

The tmux tab/window label should show:

```text
w4.auth-fix
```

When several renamed workspaces are open in a dock editor and tmux:

```text
editor roots: w1, w2, w4
tmux tabs:    w1.foo, w2.bar, w4.auth-fix
```

The mapping should be visible without opening `M-?`.
