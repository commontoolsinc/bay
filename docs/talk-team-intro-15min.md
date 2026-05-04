# Bay — 15 minute team intro

Audience: ~11 teammates who saw the 5-minute version (see
`talk-team-intro-5min.md`) and want a deeper walkthrough. Most don't
use tmux or git worktrees daily. Live demo throughout, including a
two-part recovery beat as the dramatic close.

This talk presumes exposure to the 5-minute version. The intro is a
recap, not a re-tell.

## Pre-demo setup

Same separate checkout as the 5-minute talk
(`~/projects/bay-demo` or similar). Stage 3 bays:

| Bay | State | Purpose in demo |
|-----|-------|-----------------|
| A | agent in progress | spawn target / waiting-jump / interrupt |
| B | dirty (uncommitted) | safety beat |
| C | stale (week+ old, rich description body) | M-? recall beat |

Pre-rehearse the recovery beat (`tmux kill-server` + `bay recover`) at
least 5 times. Verify recovery completes in <15s and that resumed
agents land on the same conversation. See "Recovery rehearsal" below.

`bay monitor start` ahead of time. Per
`docs/design/waiting-indicator-reliability.md`, the highlight has known
race conditions — trigger an agent turn shortly before going on so the
flag is fresh.

## Bullet outline

**0:00–0:45 · Recap of Talk 1**
- Evolution: terminals → worktrees → tmux → bay; tipping point was 5
  annoying minutes of manual rewiring per reboot
- Thesis reminder: "automate the annoying and painful bits, focus on
  the work"
- This talk: daily workflow + the recovery that started it

**0:45–1:15 · Mental model refresher**
- *Dock* = project; *Bay* = workstream; *Surface* = pane

**1:15–10:15 · Demo (10 beats)**

0. Dashboard — `bay tree` walk-through, `(1 merged)` auto-detected
1. `Option+C` — spawn
2. `Option+C` — second
3. Throwaway 3 + tear down (hoarding framing)
4. `Option+r` — waiting jump (monitor acknowledgment)
5. `Option+a` — second agent in same bay
6. `Option+E` — editor pop-up
7. Slack ping → `Option+C` interrupt bay for code review
8. `Option+g` picker → stale bay → `Option+?` recall
9. `Option+w` on dirty (refused) + `Option+z` undo-close
10. Recovery, two parts:
    - **Part A (vanilla tmux):** close terminal window → `tmux attach`
      → state preserved
    - **Part B (bay recover):** `tmux kill-server` → `bay recover`
      rebuilds session, windows, panes; agents resume with `--continue`

**10:15–11:00 · Loom-specific bonus**
- Loom runtime runs live off a git checkout
- Recent: one-command swing of the pointer to a different worktree
- With bay: live-test any bay's feature in seconds
- Bay's structured worktree paths are what made the swing tractable
- Audience-specific reason to adopt

**11:00–12:00 · Getting started**
- `go install ./cmd/bay`, `bay setup`, `bay new`
- `Option+p` is the cheat-sheet
- Point your agents at `bay agent-guide`

**12:00–12:30 · Honest tradeoffs**
- IDE picky on shared `.git/`
- Multi-agent races on shared dev infra
- `bay close --done` daily

**12:30–13:00 · Close**
- "Automate the annoying and painful bits, focus on the work."

**13:00–15:00 · Q&A**

## Full script

> [0:00] Quick recap from earlier, for those who weren't there. My
> parallelism setup evolved from terminal windows on full clones, to
> git worktrees, to tmux on top of those worktrees, to bay. The tipping
> point was: rebuilding the tmux side of that setup after every reboot
> took five annoying minutes per slot, which quietly capped how many
> slots I'd maintain. Bay automates the rebuild. Thesis: automate the
> annoying and painful bits, focus on the work.
>
> This talk goes deeper into the daily workflow and ends on the
> recovery story that's the reason bay exists.
>
> [0:45] Vocabulary refresher. A *dock* is a project — a named tmux
> session with one git checkout. A *bay* is one workstream in a dock —
> a worktree, a tmux window, metadata. A *surface* is a pane within a
> bay — agent, shell, editor. Dock, bay, surface.
>
> [1:15] **[DEMO. Demo checkout, 3 bays pre-staged.]**
>
> **[Beat 0 — dashboard]** Before I touch anything, here's where I am.
> **[run `bay tree`]** Three bays: a feature in flight, a stale one
> from last week, a bug fix. Branches, PR numbers, surfaces, dirty and
> merged flags, all in one view. And `(1 merged)` — that PR landed
> yesterday. I didn't mark anything; bay watches every branch and flips
> the merged flag the moment the commit shows up on main. None of this
> is state I hand-maintain.
>
> [1:45] **[Beat 1 — spawn]** `Option+C`. **[press]** One chord. New
> worktree, new branch, new tmux window, new Claude session. The
> manual version was four commands and a habit of typing them in
> order. **[type prompt; enter]** Agent's running.
>
> [2:30] **[Beat 2 — spawn another]** `Option+C` again. **[press,
> prompt, enter]** Two agents on two branches.
>
> [3:00] **[Beat 3 — throwaway, with hoarding framing]** Watch how
> cheap this is. **[Option+C, Option+C, Option+C]** Three more bays in
> five seconds. **[Option+w, Option+w, Option+w]** gone. Back when each
> slot meant manual setup *and* manual rebuild after every reboot, I'd
> hoard pre-built bays just in case — keep a few around so I wouldn't
> pay the setup cost when I needed one. Bay made it cheap enough that I
> stopped hoarding. The cost was on both ends — creation and rebuild —
> and now both are zero. I'll come back to *why* the rebuild end is
> zero, at the end. The cheapness changed *what I do*, not just how I
> do it.
>
> [3:45] **[Beat 4 — waiting jump, with monitor]** **[indicator
> visible]** Agent's waiting. Bay runs a background monitor that
> watches every agent across every bay; the moment one waits for input,
> it flags the bay. `Option+r`. **[press, jumps]** Takes me there
> directly. If three are waiting, it cycles. Really nice to have when
> I've got a few in flight.
>
> [4:30] **[Beat 5 — two agents in one bay]** I want a second
> perspective. `Option+a`. **[press]** Adds another agent pane to this
> *same* bay. Claude on the left, Codex on the right, looking at the
> same diff. They disagree productively.
>
> [5:15] **[Beat 6 — editor]** `Option+E`. **[press]** Pops up an
> editor scoped to the dock. **[browse briefly]** Close. `Option+e` is
> the per-bay version.
>
> [6:00] **[Beat 7 — interrupt bay]** Slack ping. Teammate wants me to
> review their PR. Without bay, I'd find a stopping point first, stash
> if dirty, switch branches, do the review, switch back. With bay:
> `Option+C`. **[prompt: "gh pr checkout 347 and walk me through this";
> enter]** Done. The two agents I had running keep running. Review
> happens. `Option+w`. Back where I was. *I never had to find a
> breaking point in my main work.*
>
> [7:00] **[Beat 8 — recall]** **[Option+g picker, fuzzy by
> description, jump to stale bay]** If I saw this cold, no idea.
> `Option+?`. **[popup]** Agent kept the description current. Goal,
> current state, blocker, next step. Reading, not reconstructing.
>
> [7:45] **[Beat 9 — safety + undo]** **[Switch to dirty bay.]**
> `Option+w`. **[refused]** Won't close a bay with uncommitted work.
> **[Option+w on a clean bay → closes; Option+z → comes back]**
> Undo-close. Press it more than I'd like to admit.
>
> [8:30] **[Beat 10 — recovery, two parts]** Now the closing demo. Two
> parts — both important even if you don't use bay.
>
> **Part one — what tmux already does.** **[Close terminal window with
> cmd-W]** Window's gone. **[Open a new terminal, type `tmux attach`]**
> Back. Everything I had — agents, panes, all of it — still there.
> That's tmux, not bay. Tmux kept the session running while my window
> was closed. **If you remember nothing else from this talk: even
> without bay, just running your shells inside `tmux` will save you the
> next time you close the wrong window.**
>
> [9:15] **Part two — what tmux can't do.** What if tmux *itself* goes
> away? Reboot, OS upgrade, the rare crash. **[`tmux kill-server` from
> a side terminal]** Everything's gone. Tmux is dead. Worktrees on disk
> — git is durable — but the wiring, the windows, the running agents,
> gone.
>
> **[Run `bay recover`]**
>
> Bay is reading its manifest — every bay, every surface, every branch.
> **[tmux session comes back; windows recreated; agents start
> launching]** Sessions back. Windows back. Worktrees pointed at
> correctly. **[show one of the agent panes]** Claude resumed with
> `--continue`. Same conversation as before the kill.
>
> [10:00] **[Pause]** That's maybe 5% of how often I use bay. But its
> existence is what makes the other 95% work the way it does. If
> rebuilding from a reboot cost five minutes per slot, I'd ration my
> slots. Because rebuilding costs nothing, I don't think about it at
> all. Cheap slots aren't enabled by cheap creation alone — they're
> enabled by cheap *re*-creation.
>
> [10:15] **[End demo.]** One thing worth calling out specifically: bay
> makes Loom development meaningfully easier. Loom's runtime runs live
> off a git checkout — processes pointing at the checkout for their
> code. Trying a change used to mean editing your main checkout or
> bringing up a parallel runtime; both annoying. I recently added a
> one-command swing: turn the runtime off, point it at a different
> worktree, turn it back on. Combined with bay: I can live-test
> whatever feature is in any bay in a few seconds. The piece that made
> the swing feature worth building in the first place is bay's
> structured, predictable worktree paths — ad-hoc directories didn't
> make that integration tractable. If you're working on Loom, this is
> a concrete reason to set bay up.
>
> [11:00] To get bay on your machine: `go install ./cmd/bay`, then
> `bay setup`. Setup installs the keybindings into `~/.tmux.conf` and
> configures defaults. Then in any repo: `bay new` bootstraps a dock
> from your CWD. After that, you stay in tmux and use `Option+`-keys.
>
> If you forget a hotkey: `Option+p`. Every command, fuzzy-searchable,
> hotkey on the right. Cheat-sheet.
>
> If you're driving Claude or Codex, point them at `bay agent-guide`.
> They'll learn the conventions, including keeping the description
> current — what makes the M-? popup useful weeks later.
>
> [12:00] Honest about rough edges. Worktrees share `.git/` —
> JetBrains picky, VS Code fine. Multiple agents on shared dev
> infrastructure can race — single dev DB or fixed port and two agents
> collide. Cleanup is real — `bay close --done` daily.
>
> [12:30] One line: automate the annoying and painful bits, focus on
> the work. `bay agent-guide`, `docs/human-guide.md`,
> `docs/tutorial.md`. Questions?

## Recovery rehearsal (beat 10)

This beat is the dramatic close and the riskiest live thing. Test:

1. **Part A timing**: closing a terminal and `tmux attach`-ing in a
   fresh one should be ~5 seconds. Verify your terminal app actually
   closes the tmux client cleanly on cmd-W (some configs detach
   instead).
2. **Part B timing**: `tmux kill-server` then `bay recover` — time it.
   If recovery takes >20s, the talk dead-airs. Acceptable up to ~15s
   with a good filler line ("bay's reading the manifest now…"). If
   slower, file an issue first and demo something else.
3. **Resume verification**: in the rehearsal checkout, actually have a
   Claude session with a few turns of history *before* killing the
   server, then verify the recovered pane shows continuity. If
   `--continue` lands on a different conversation, the punchline
   misfires.
4. **Visible state for the audience**: have one bay's agent pane in a
   state that's *visibly* in-progress before the kill (e.g. a tool
   result on screen). When it comes back, the audience can see the
   same content.
5. **Bail-out plan**: if recovery glitches in front of the audience,
   you have two lines ready: "This is what bay does when it works, and
   yes, I am opening an issue right after this talk." Honesty plays
   better than scrambling.

## Other rehearsal notes

- **Pre-stage descriptions**: agents take minutes to write good ones.
  Have them write the descriptions in the demo checkout *before* the
  talk. The M-? popup is a recurring punchline.
- **Verify the waiting indicator** — per
  `docs/design/waiting-indicator-reliability.md`, known to be flaky.
  Trigger an agent turn shortly before stage. Fallback: switch via
  `Option+g` if the highlight doesn't appear, narrate the value prop
  verbally.
- **Throwaway-bays beat (3)**: practice the `Option+C, Option+C,
  Option+C` cadence and the matching close cadence.
- **Live `gh pr checkout` in beat 7** can fail (auth, network, missing
  PR). Pre-pick a real PR number you've verified works in the demo
  checkout, or replace the prompt with a self-contained "review this
  diff: …" task.
- **Font/window size.** Demo terminal at 18pt minimum. Verify on the
  projector before starting.
