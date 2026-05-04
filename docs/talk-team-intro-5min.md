# Bay — 5 minute team intro

Audience: ~11 teammates at an informal best-practices round-robin on
agentic coding. Most don't use tmux or git worktrees daily. Goal: hook
them on bay enough to want a deeper look. Live demo throughout.

## Pre-demo setup

Use a separate checkout (e.g., `~/projects/bay-demo`) so the demo state
is staged and stable. Don't demo from your live development worktree.

Stage three bays in advance so descriptions are agent-written, not
fresh:

| Bay | State | Purpose in demo |
|-----|-------|-----------------|
| A | agent in progress | spawn target / waiting-jump beat |
| B | dirty (uncommitted) | safety beat |
| C | stale (week+ old, rich description body) | M-? recall beat |

Run `bay monitor start` ahead of time. Verify the waiting indicator
actually highlights — see `docs/design/waiting-indicator-reliability.md`
for known race conditions; if it's flaky, trigger an agent turn just
before going on stage so the highlight is fresh.

## Bullet outline

**0:00–0:45 · Hook (story, audience-respecting)**
- Show of hands: who already runs >1 workstream? *[hands]* — who's
  rebuilt their setup after a tmux crash or reboot? *[hands]*
- My version: row of terminals on separate git worktrees ("parallel
  checkouts of the same repo, one per branch")
- Pain: ~5 minutes of opening windows and restarting agents after every
  reboot. Small but constant; taxed slot count.
- Built a thing that remembers all of that. Called bay.

**0:45–1:00 · Dashboard glance (new beat)**
- `bay tree` — three bays, branches, PRs, surfaces, `(1 merged)`
  auto-detected
- "None of this is state I maintain"

**1:00–4:00 · Demo (4 keybinding beats)**
- `Option+C` spawn × 2
- Throwaway 3 + tear down (hoarding framing)
- Waiting jump (monitor acknowledgment)
- Stale bay → `Option+?` recall

**4:00–4:30 · Recovery as verbal close**
- "Tmux already keeps your sessions alive across closed windows — most
  useful tool you're not using. But when tmux itself goes — reboot, OS
  upgrade — `bay recover` rebuilds the whole setup. That's why I'm not
  reluctant to spin up 10 bays a day."

**4:30–5:00 · Tease + thesis**
- It's called bay. CLI bootstrap once, then `Option+`-keys.
  `bay agent-guide` for your AIs.
- "Automate the annoying and painful bits, focus on the work."

## Full script

> [0:00] Show of hands — who already runs more than one workstream at a
> time? Multiple terminals, multiple checkouts, branch switching,
> anything. *[pause for hands]* Same. My version of that used to be a
> row of terminal windows on separate git worktrees — a worktree is a
> parallel checkout of the same repo, one per branch, so two streams
> don't fight over your working directory. It worked great.
>
> Until I rebooted, or accidentally closed my terminal, and had to
> spend five minutes opening windows, cd-ing into each worktree,
> restarting whatever agent was running. Each time was small. But it
> was constant, and it taxed how many slots I was willing to maintain
> — I'd hesitate to spin up a fifth or sixth because I knew the rebuild
> cost.
>
> [0:45] So I built a thing that remembers all that. It's called bay.
> Three minutes of demo.
>
> **[DEMO. Tmux session, 3 pre-staged bays.]**
>
> Quick look at where I am — `bay tree`. **[run]** Three bays in
> flight: branches, PR numbers, surfaces, all here. And see this —
> `(1 merged)` — that PR landed; bay watches every branch and flipped
> the flag itself. None of this is state I maintain.
>
> [1:15] `Option+C`. **[press]** One keybinding for: new worktree, new
> tmux window, new Claude session. **[type prompt; enter]** Agent's
> running.
>
> [1:50] `Option+C` again. **[press, prompt, enter]** Two agents on two
> branches.
>
> [2:20] And here's the side effect. **[Option+C, Option+C, Option+C]**
> Three more bays in five seconds. **[Option+w, Option+w, Option+w]**
> gone. When setup was manual, I'd hoard pre-built workspaces just in
> case — keep a couple around so I wouldn't pay the setup cost when I
> needed one. Bay made it cheap enough that I stopped hoarding. I make
> a bay when I need one, throw it away when I'm done.
>
> [3:00] Now — **[indicator on tab]** see this marker? Bay runs a
> background monitor that watches every agent across every bay; the
> moment one waits, it flags the bay. `Option+r`. **[press, jumps]**
> Takes me there directly. Really nice to have when several agents are
> running.
>
> [3:35] **[Switch to stale bay via Option+g]** Here's a bay I haven't
> touched in a week. If I saw it cold, no idea. `Option+?`. **[popup]**
> Agents kept the description current as they worked. Goal, current
> state, next step. Reading, not reconstructing.
>
> [4:00] One thing I'm not demoing because it'd take too long: tmux
> already preserves your work when you close a terminal — that alone is
> worth knowing if you're not using it. But when tmux *itself* goes
> away — reboot, OS upgrade — bay rebuilds the whole setup with one
> command. Worktrees, windows, agents resumed mid-conversation. That's
> the thing that got me to build bay, and it's why I'm not reluctant to
> make a bunch of bays in the first place.
>
> [4:30] It's called bay. CLI to bootstrap a project once; after that
> everything is `Option+`-something. Won't let you close a bay with
> uncommitted work. `bay agent-guide` if you want your AI to read about
> it.
>
> The thesis is one line: automate the annoying and painful bits, focus
> on the work. Find me after.

## Rehearsal notes

- **Pre-stage descriptions**: have agents write bay descriptions in the
  demo checkout *before* the talk. Agents take minutes to write a good
  one, and the M-? popup is the punchline — empty/shallow descriptions
  make it land flat.
- **Verify the waiting indicator before the talk.** Per
  `docs/design/waiting-indicator-reliability.md`, the highlight has
  known race conditions. Have a fallback: if no indicator shows up,
  switch via `Option+g` instead of `Option+r` and narrate the value
  prop verbally.
- **Throwaway-bays beat**: practice the `Option+C, Option+C, Option+C`
  cadence. If the third lags or fails, the rhythm joke deflates.
- **Demo terminal at 18pt minimum** so the back row reads it. Verify on
  the projector before starting.
- **If anything glitches**: skip ahead. The hook + the M-? recall beat
  are non-negotiable; everything else is sacrificial.
