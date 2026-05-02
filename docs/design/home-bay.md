# Home bay - design plan

Captured 2026-05-01. No code has been written for this feature. This
doc records the design for a reserved `home` bay that represents a
dock's canonical checkout.

The discussion was triggered by a recurring workflow: while working in
one or more worktree bays, the user often opens a separate shell in the
main checkout that backs the dock. That checkout is where checked-in
documentation is easiest to inspect, and it is the place the user wants
to keep current as worktree branches land. Bay owns the surrounding
tmux session and navigation model, so the canonical checkout should be
reachable through bay instead of managed as an unrelated shell.

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

Tmux sessions need at least one window. A placeholder window named `~`
is hard to explain because it is not part of the Dock > Bay > Surface
model. Home gives empty dock sessions a useful, honest surface: a shell
in the dock checkout.

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
- Is discoverable through direct commands and the command palette even
  when empty.
- Never has worktree lifecycle behavior. Closing home surfaces must
  never delete `dock.path`.

Conceptually every dock always has a logical `home` target. It becomes
visible when it has live or restorable surfaces, or when bay needs an
empty-dock surface to keep the tmux session useful.

### Why not just an external bay at `dock.path`?

An external bay can approximate home by pointing at `dock.path`; most
of the proposed behavior — surfaces, navigation, recovery — would work.
The reasons to make home its own type rather than leaving it to users:

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
and home should not carry user-authored context like normal bays.

`bay edit --dock` means edit the dock's worktree parent directory so
all worktree bays are visible. Home is separate:
`bay edit --bay home` opens the canonical checkout itself.

## Visibility and navigation

Empty dock sessions use home rather than a `~` placeholder. This makes
an explicit `bay dock new` land in a useful shell at `dock.path`.

Auto-bootstrap through `bay new foo` creates only `foo`. The requested
bay itself keeps the tmux session alive, so home remains
unmaterialized.

When home has no surfaces:

- `bay go` does not include it.
- `bay ls` does not include it.
- next/previous bay cycling skips it.
- direct commands such as `bay home`, `bay go home`, and
  `bay shell --bay home` can still materialize it.
- the command palette still offers home actions when the current
  context is a dock.

When home has surfaces:

- It appears in `bay go`, `bay ls`, `bay tree`, and next/previous bay
  cycling.
- It participates in `bay surface go --next-waiting` and waiting-agent
  keybinding behavior.
- It appears as a normal tmux tab/window group, labeled `home`.

On first creation, bay places home at tmux tab index 0. After that,
user movement is respected; bay should not keep forcing home back to
index 0 on every recover or focus.

If the last non-home surface in a dock closes and the dock still needs
a tmux window to remain usable, create or focus a home shell.

## Repository commands

Home's update workflow stays explicit and repo-owned. Once the user
can jump to a shell at `dock.path`, ordinary commands like `git pull`,
`git fetch --prune`, or project-specific sync scripts are one command
away. Baking a pull command into bay adds policy around branches,
fast-forward behavior, dirty state, credentials, and
repository-specific workflows.

Repository update commands run from the home shell, or from a generic
command surface targeted at home when the user wants a visible
one-shot command:

```
bay surface new cmd "git pull --ff-only" --bay home
```

## Keybindings and palette

The command palette should include home actions. The exact menu names
can follow palette conventions, but the actions should cover:

- Go to home
- Home shell
- Home editor
- Home agent
- Home agent...

Add home actions under the `M-o` chord family. `M-o h` is a prefix
only; the default home action is on Enter so tmux does not have to
distinguish between "run home now" and "wait for a more specific home
command."

| Key | Action |
|---|---|
| `M-o h Enter` | focus/create home shell |
| `M-o h s` | home shell |
| `M-o h e` | home editor |
| `M-o h c` | Claude in home |
| `M-o h x` | Codex in home |
| `M-o h g` | Gemini in home |

## Lifecycle rules

Home is not a worktree bay.

- Closing the last home surface hides home when other bays still have
  live surfaces.
- `bay close home` closes all home surfaces.
- `bay close --all` closes home surfaces after worktree bays.
- No home close path deletes, removes, prunes, or otherwise mutates
  `dock.path`.
- Home is not subject to orphan-workspace auto-close. Empty home is
  just hidden.

If the user closes the last live surface in the whole dock and that
surface is home, bay must not recreate a new home shell behind it.
That case means the user is trying to dismiss the dock UI:

- First `M-w`/surface-close attempt warns that home is the last surface
  in the dock.
- A second close within the confirmation window dismisses the dock's
  tmux UI by killing the tmux session or terminal host.
- The dock remains registered in the manifest.
- The checkout at `dock.path` remains untouched.
- This is not `bay dock close`.

Full dock close is structural: it unregisters the dock from bay after
closing child bays/surfaces. That action should stay explicit via
`bay dock close` and has no default keybinding.

Native tmux closes should follow the same intent:

- If bay discovers a dead home surface while other live surfaces remain
  in the dock, remove that home surface from the manifest and hide home
  when it becomes empty.
- If the user kills the last home window through tmux and the dock UI
  disappears, keep the dock registered and do not recreate home behind
  the user.

Recovery should recreate home surfaces if they were recorded before
the tmux session died. Empty logical home has no surfaces to restore
and should not create a tab during recovery.

## Placeholder replacement

Bay should not create `~` placeholder windows for docks with checkouts.

| Scenario | Behavior |
|---|---|
| `bay dock new` | Create a home shell at `dock.path` instead of `~`. |
| `bay new foo` auto-creates a dock | Create only `foo`; do not also create home. |
| Last non-home surface closes | Create or focus a home shell instead of `~`. |
| Last home surface closes | Confirm, then dismiss the dock tmux UI; do not create `~` or another home shell. |

A tmux window named `~` is not home. Bay may remove or replace a
bay-owned placeholder during sync/recover, but must not infer that an
arbitrary user-created `~` window is equivalent to home.

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

Persist home only while it has surfaces. Empty home is synthesized from
the dock when resolving targets, building palette entries, or handling
`bay home`, so empty pseudo-bays are not stored.

When the first home surface is created, add a `BayTypeHome` entry to
the dock with that surface. When the last home surface closes and other
live surfaces still exist in the dock, remove the empty home entry from
the manifest. When home is serving as the empty-dock surface, it is
persisted because it has a shell surface.

This lets surface, navigation, tree, recovery, and waiting-agent code
operate through the bay-shaped APIs. Lifecycle-sensitive paths must
switch on type and skip worktree cleanup for `home`.

Alternatives such as storing home as dock-level surfaces are less
attractive because the feature should behave like a bay for targeting,
surface grouping, cycling, and agent routing. Dock-level surfaces are
still useful for the dock editor concept, but home is a richer surface
group with bay-like navigation semantics.

`home` is reserved. Worktree/external bay creation and rename must
reject it. Manifests that already contain a normal bay named `home` are
not a supported migration target for this design; if encountered, bay
should fail clearly rather than guess.

## Current context

Inside a home surface, bay's current context is:

| Field | Value |
|---|---|
| dock | current dock |
| bay | `home` |
| surface | current home surface |
| path | `dock.path` |

Default-targeting commands should treat home as the current bay when
they can operate on any bay-like surface group. For example:

- `bay shell` from home adds a home shell.
- `bay agent` from home adds a home agent using the dock default agent.
- `bay edit` from home opens `dock.path`.
- `bay pwd` and the status line show `home`.

Commands that require a worktree must reject home with clear errors.
Branch/PR display, close safety checks, dirty-worktree cleanup,
missing-worktree repair, merge status, and `--clean`/`--done` close
flows must skip or reject home rather than assuming `WorktreeAttrs`
exists.

## Relationship to other designs

- `dock-checkout-merge.md`: home is simpler after the dock/repo merge
  because every dock has exactly one canonical checkout at `dock.path`.
- `command-palette.md`: home actions should be palette entries, not
  only keybindings.
- `orphan-hygiene.md`: home is explicitly outside orphan auto-close
  because it has no worktree lifecycle.
- Docks with checkouts use home as the user-facing empty-dock surface;
  `~` windows are not home.
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
navigation. Keep empty home hidden from normal lists and pickers, but
allow direct commands and palette entries to materialize it. Use a home
shell instead of the `~` placeholder for explicit empty docks and for
docks that become empty after closing the last non-home surface.

### Step 3: Last-home close behavior

When the user confirms closing the last home surface in an otherwise
empty dock, dismiss the dock's tmux UI by killing the tmux session or
terminal host. Keep the dock registered, leave `dock.path` untouched,
and keep full `bay dock close` separate and explicit.

### Step 4: Lifecycle and recovery

Make close/recover/sync paths home-aware. Home surface close should
hide empty home without scheduling orphan cleanup. `bay close --all`
should close home after worktree bays. Recovery should restore recorded
home surfaces only.

### Step 5: Current context and worktree-only guards

Make tmux/context resolution identify home surfaces as `bay=home`.
Ensure commands that can operate on any bay-like surface group work
from home, and commands that require a worktree reject or skip home
cleanly.

### Step 6: Keybindings and docs

Add command-palette entries, `M-o h` keybindings, human-guide updates,
and agent-guide updates.

## Acceptance tests

- `bay home` in a registered dock creates or focuses a shell at
  `dock.path`.
- `bay dock new` creates a home shell instead of a `~` placeholder.
- `bay new foo` auto-bootstrapping a dock creates `foo` only, with no
  home shell.
- `bay shell --bay home` adds a shell surface under home.
- `bay agent --bay home` launches the dock default agent under home.
- `bay edit --bay home` opens `dock.path`; `bay edit --dock` still opens
  the worktree parent/all-bays view.
- Empty home is hidden from normal `bay ls`, `bay tree`, `bay go`, and
  next/previous bay cycling.
- Direct `bay go home` or a palette Home action materializes/focuses
  home even when it was empty.
- Visible home participates in `bay ls`, `bay tree`, `bay go`,
  next/previous cycling, and waiting-agent navigation.
- `bay new home`, `bay rename home`, and `bay describe home` fail with
  clear guidance.
- Closing the last home surface while other bays have live surfaces
  removes/hides home without touching `dock.path`.
- Closing the last non-home surface in a dock creates or focuses a home
  shell instead of creating `~`.
- Confirming close on the last home surface in an otherwise empty dock
  dismisses the dock tmux UI, keeps the dock registered, and leaves
  `dock.path` untouched.
- Direct native tmux close of the last home window does not cause bay
  to recreate home behind the user.
- `bay close --all` closes home after worktree bays and never deletes
  `dock.path`.
- Worktree-only commands skip or reject home rather than dereferencing
  missing worktree metadata.
