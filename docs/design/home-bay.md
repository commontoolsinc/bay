# Home bay - design plan

Captured 2026-05-01. No code has been written for this feature. This
doc records the design for a reserved `home` bay that represents a
dock's canonical checkout.

The discussion was triggered by a recurring workflow: while working in
one or more worktree bays, the user often opens a separate shell in the
main checkout that backs the dock. That checkout is where checked-in
documentation is easiest to inspect, and it is the place the user wants
to keep current with `git pull` as worktree branches land. Bay already
owns the surrounding tmux session and navigation model, so the
canonical checkout should be reachable through bay instead of managed
as an unrelated shell.

## Problem

A normal bay is disposable: it is backed by a worktree, and closing the
bay can remove the worktree directory after safety checks pass. The
canonical dock checkout is the opposite:

- It is not a worktree.
- Bay must never delete it.
- It should be easy to reach from the same tmux session as the worktree
  bays.
- It should support the same working surfaces users expect inside a
  bay: shell, agent, editor, and command surfaces.
- It should stay visually out of the way when unused.

Keeping this checkout outside bay leaves repeated manual work:
creating a tmux tab, naming it, opening an editor or agent there, and
remembering that this tab has different lifecycle rules from worktree
bays.

## Proposal

Add a reserved `home` pseudo-bay to every dock. User-facing behavior is
"another bay"; lifecycle behavior is dock-owned and non-destructive.

The home bay:

- Has stable ID `home` and label `home`.
- Is backed by the dock checkout at `dock.path`.
- Supports normal bay surfaces: shell, agent, editor, and command.
- Uses the dock's default agent and per-agent args.
- Participates in navigation, recovery, and waiting-agent routing when
  it has surfaces.
- Is hidden from normal lists and bay navigation when it has no
  surfaces.
- Is shown by `--all` views even when empty.
- Never has worktree lifecycle behavior. Closing home surfaces must
  never delete `dock.path`.

Conceptually every dock always has a logical `home` target. It becomes
visible only when it has live or restorable surfaces.

### Why not just an external bay at `dock.path`?

Bay already supports `bay new --dir PATH`, so a user could approximate
home today by running `bay new --dir $(dock.path)`. Most of the
proposed behavior — surfaces, navigation, recovery — would work. The
reasons to make home its own type rather than leaving it to users:

- **Automatic across every dock, no setup.** Home exists implicitly
  the moment a dock exists. Users don't need to remember the recipe,
  and it works the same way in every dock without per-dock manual
  setup.
- **Lifecycle-protected.** An external bay's close path is allowed to
  remove its directory after safety checks. Pointing one at
  `dock.path` puts the canonical checkout one wrong confirmation away
  from deletion. A reserved type makes "never delete `dock.path`" a
  static guarantee instead of a runtime hope.
- **Hidden when empty.** A normal external bay is always listed.
  Home should disappear from `bay ls`, `bay go`, and cycling when
  nothing is open against it, so the dock checkout doesn't add noise
  to the bay list for users who rarely touch it.

These three properties are what make home worth a third bay type
rather than a documented convention.

## User-facing commands

`home` is addressable anywhere a bay target is accepted:

```
bay home
bay shell --bay home
bay agent --bay home
bay agent codex --bay home
bay edit --bay home
bay close home
bay go home
bay home status
bay home pull
```

`bay home` is the shortcut for "focus the most recent home surface; if
none exists, create a home shell." The home shell runs at `dock.path`.

`bay close home` closes all home surfaces and leaves the dock checkout
untouched.

`bay new home` must fail with clear guidance. Accepting it as an alias
would make users think bay is creating a new worktree:

```
home is reserved for the dock checkout; use `bay home`
```

`bay rename home` and `bay describe home` must fail. The label is fixed,
and the status-like information shown for home is computed rather than
user-authored context.

`bay edit --dock` keeps its current meaning: edit the dock's worktree
parent directory so all worktree bays are visible. Home is separate:
`bay edit --bay home` opens the canonical checkout itself.

## Visibility and navigation

When home has no surfaces:

- `bay go` does not include it.
- `bay ls` does not include it.
- next/previous bay cycling skips it.
- `bay ls --all` and `bay tree --all` show it as empty.

When home has surfaces:

- It appears in `bay go`, `bay ls`, `bay tree`, and next/previous bay
  cycling.
- It participates in `bay surface go --next-waiting` and the existing
  waiting-agent keybinding behavior.
- It appears as a normal tmux tab/window group, labeled `home`.

On first creation, bay places home at tmux tab index 0. After that,
user movement is respected; bay should not keep forcing home back to
index 0 on every recover or focus.

## Pull and status

Home needs fast, manual update affordances. Bay should not
automatically pull the checkout after worktree merges, because that is
too easy to get wrong around dirty state, local branches, credentials,
and repository-specific workflows.

Add:

```
bay home status
bay home pull
bay home pull --popup
```

`bay home status` is computed status, not a mutable description. It
should include:

- current branch
- default branch
- dirty state
- ahead/behind relative to the upstream
- a clear marker when the current branch is not the default branch

When home is focused, the status line may show a compact form if there
is room.

`bay home pull` runs:

```
git pull --ff-only
```

at `dock.path`.

Before running pull, bay refuses unless the current branch matches the
default branch. Determine the default branch from `origin/HEAD`. If the
checkout is dirty, diverged, missing credentials, or otherwise cannot
pull cleanly, let git produce its normal output and exit status.

`bay home pull --popup` is intended for keybindings. It always runs in a
tmux popup rather than a tracked command surface:

- On success, keep the popup open briefly so the user sees what
  happened, then close it.
- On failure, leave the popup open with the full git output and a
  shell/prompt so the user can inspect and recover.

## Keybindings and palette

The command palette should include home actions. The exact menu names
can follow palette conventions, but the actions should cover:

- Go to home
- Home shell
- Home editor
- Home agent
- Home agent...
- Home status
- Home pull

Add home actions under the existing `M-o` chord family:

| Key | Action |
|---|---|
| `M-o h` | focus/create home shell |
| `M-o h s` | home shell |
| `M-o h e` | home editor |
| `M-o h c` | Claude in home |
| `M-o h x` | Codex in home |
| `M-o h g` | Gemini in home |
| `M-o h p` | home pull popup |

## Lifecycle rules

Home is not a worktree bay.

- Closing the last home surface only hides home from normal navigation.
- `bay close home` closes all home surfaces.
- `bay close --all` closes home surfaces after worktree bays.
- No home close path deletes, removes, prunes, or otherwise mutates
  `dock.path`.
- Home is not subject to orphan-workspace auto-close. Empty home is
  just hidden.

Recovery should recreate home surfaces if they were recorded before
the tmux session died. Empty logical home has no surfaces to restore
and should not create a tab during recovery.

## Data model

Implementation should make `home` a reserved bay ID. The simplest
shape is likely a new bay type:

```go
BayTypeHome BayType = "home"
```

with:

- `ID: "home"`
- `Name: "home"`
- `Type: "home"`
- `Path: dock.Path`
- `Worktree: nil`

This lets existing surface, navigation, tree, recovery, and waiting
agent code operate through the bay-shaped APIs. Lifecycle-sensitive
paths must switch on type and skip worktree cleanup for `home`.

Alternatives such as storing home as dock-level surfaces are less
attractive because the feature should behave like a bay for targeting,
surface grouping, cycling, and agent routing. Dock-level surfaces are
still useful for the existing dock editor concept, but home is a richer
surface group with bay-like navigation semantics.

`home` is reserved. Worktree/external bay creation and rename must
reject it. Existing manifests with a normal bay named `home` are not a
supported migration target for this design; if encountered, bay should
fail clearly rather than guess.

## Relationship to other designs

- `dock-checkout-merge.md`: home is simpler after the dock/repo merge
  because every dock has exactly one canonical checkout at `dock.path`.
- `command-palette.md`: home actions should be palette entries, not
  only keybindings.
- `orphan-hygiene.md`: home is explicitly outside orphan auto-close
  because it has no worktree lifecycle.
- `undo-close.md`: individual home surfaces can use surface undo if the
  implementation supports it, but restoring a home surface must not
  imply restoring or recreating a checkout.

## Incremental path

### Step 1: Reserved home target

Reserve `home` as a bay ID. Add resolver support so `--bay home`,
`bay go home`, and `bay close home` can target the dock checkout.
Reject `bay new home`, `bay rename home`, and `bay describe home`.

### Step 2: Home surfaces and navigation

Create/focus home surfaces at `dock.path`. Include visible home in
`bay ls`, `bay tree`, `bay go`, next/previous cycling, and waiting-agent
navigation. Show empty home only with `--all`.

### Step 3: Lifecycle and recovery

Make close/recover/sync paths home-aware. Home surface close should
hide empty home without scheduling orphan cleanup. `bay close --all`
should close home after worktree bays. Recovery should restore recorded
home surfaces only.

### Step 4: Pull and status

Add `bay home status`, `bay home pull`, and `bay home pull --popup`.
Use `origin/HEAD` to determine the default branch. Refuse pull when
home is not on that branch, then run `git pull --ff-only` and surface
git output clearly.

### Step 5: Keybindings and docs

Add command-palette entries, `M-o h` keybindings, human-guide updates,
and agent-guide updates.
