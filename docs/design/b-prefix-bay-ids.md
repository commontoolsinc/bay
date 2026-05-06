# Rename generated bay IDs to b-prefix

**Status**: implemented.

## Problem

Bay's generated worktree bay IDs and child worktree directories used to use
the `w<N>` shape:

```text
ID:   w1
Path: /Users/dev/projects/labs-worktrees/w1
```

The `w` prefix came from the older workspace model. The product language is
now bays, and the generated handle should match that language:

```text
ID:   b1
Path: /Users/dev/projects/labs-worktrees/b1
```

This change is only about generated bay IDs and the per-bay child directory
under a dock's worktree parent. The directories are still git worktrees, and
the dock-level parent remains the dock's `worktree_dir` such as
`labs-worktrees`.

## Goal

New worktree bays should use `b<N>` for both:

- the stable bay ID
- the basename of the created git worktree path

Existing `w<N>` bays should keep working without moving on disk and without
manifest migration.

## Model

Use two generated ID shapes:

| Shape | Meaning |
| --- | --- |
| `b<N>` | current generated bay ID and new worktree directory basename |
| `w<N>` | legacy generated bay ID and legacy worktree directory basename |

Both shapes are lowercase prefix plus a positive integer with no leading
zeros. `b0`, `b01`, `w0`, and `w01` are not generated ID shapes.

There is no command-resolution compatibility mode. Resolvers already look up
bay IDs by exact persisted string, so an existing manifest entry with
`id: "w1"` remains addressable as `w1`, while a new entry with `id: "b1"` is
addressable as `b1`.

The only places that need to know about the old shape are helper paths that
classify generated IDs, not the resolver itself.

## Allocation

`nextBayDir` should allocate only `b<N>` candidates.

It should keep the existing collision checks:

- skip names already present as the basename of any bay path in the dock
- skip names that already exist on disk under the dock's worktree dir

The numeric sequence is prefix-local. If a dock already has `w1` and `w2`,
the next created bay may be `b1`; there is no collision because IDs and path
basenames differ by prefix.

## Manifest Loading

Current manifests already store bay IDs, so most existing `w<N>` bays need no
work. They load as ordinary IDs.

`AssignBayIDs` still matters for older or malformed manifests where `id` is
empty. It should:

1. Claim the path basename when it is a generated ID shape, accepting both
   `w<N>` and `b<N>`.
2. Fill any remaining empty IDs with the next free `b<N>`.

This preserves old path-derived IDs for legacy rows while making all newly
invented IDs use the new prefix.

## Validation

Bay Names should remain separate from generated ID-shaped strings. Reserve
both current and legacy generated shapes:

```text
^b[1-9]\d*$
^w[1-9]\d*$
```

This is not for command compatibility. It keeps live old IDs from becoming
display names on other bays, keeps `did you mean` hints simple, and preserves
the existing rule that generated handles are not user-facing Names.

`home` remains separately reserved.

Implementation-wise, avoid overloading one helper for every purpose:

- `IsBayID` or `IsGeneratedBayID`: true for both `b<N>` and `w<N>` when the
  caller is asking "does this look like a bay-generated handle?"
- `nextBayDir`: hard-coded to produce only `b<N>`
- any helper that needs the current canonical prefix should use a named
  constant, for example `CurrentBayIDPrefix = "b"`

## Display

Existing display helpers can stay path-derived:

- `BayDirTag` returns `filepath.Base(bay.Path)`, so it naturally returns
  `b1` for new bays and `w1` for old bays.
- `BayCompactLabel` continues to combine dir tag and semantic Name, such as
  `b1.auth-fix` or `w4.auth-fix`.
- Status-line `dir` and `full` fields need no special compatibility logic if
  they call these helpers.

Docs and examples should switch to `b1`, `b2`, etc. when showing new bays.
Historical design docs can keep old examples unless they would actively
mislead an implementation.

## Implementation Steps

1. Introduce generated-ID helper constants and parsing:
   - current prefix: `b`
   - legacy prefix: `w`
   - no leading zeros
2. Update `nextBayDir` to produce `b<N>`.
3. Update `AssignBayIDs` to claim existing `w<N>` or `b<N>` basenames, then
   synthesize missing IDs as `b<N>`.
4. Update `ValidateBayName`, placeholder-name detection, and branch-name
   abbreviation guards to reserve both generated shapes.
5. Update comments and command help examples from `w1` to `b1` where they
   describe newly created bays.
6. Update tests:
   - new bay creation returns `b1`
   - `nextBayDir` ignores existing `w<N>` for b-number allocation
   - existing manifest rows with `id: "w1"` still resolve and close
   - load-time ID assignment preserves legacy path basenames
   - Names matching either generated shape are rejected

## Non-goals

- Do not rename existing `w<N>` worktree directories.
- Do not rewrite existing manifest IDs from `w<N>` to `b<N>`.
- Do not change the dock worktree parent directory name or default
  `worktree_dir`.
- Do not change git worktree lifecycle behavior.
