# Waiting indicator reliability — design plan

Captured 2026-05-04. Investigation memo. The user reports that the
tmux waiting highlight (the visual marker on a bay's tmux window when
its agent is waiting for input) often does not appear when it should.
This document captures what we found and the candidate fixes; no code
has been written.

## The problem

The waiting indicator is the connective tissue between long-running
agents and reattaching attention: an agent in bay B finishes its turn,
the tab for bay B highlights, the user hits `Option+r` and is there.
When the highlight fails to appear, the entire flow degrades silently
— the user discovers waiting agents by other cues (sound, peripheral
attention, manual checking) and the keybinding's value collapses.

The user reports the failure is common in active bouncing — exactly
the workflow the indicator is meant to support.

## How the highlight is supposed to work

Three signals can mark a bay as waiting:

1. **Turn-complete hooks** (Claude `Stop`, Codex `notify`, Gemini
   notification settings) run a shell snippet at end-of-turn that sets
   `@bay-waiting=1` on the current tmux window. Installed by
   `bay setup`. Code: `internal/cli/setup.go:1128`.
2. **Regex patterns** in `~/.config/bay/waiting-patterns.txt`. The
   monitor scans pane content on each tick; matches set the flag.
   Code: `internal/monitor/monitor.go:323-345`.
3. **Bell flag** (`#{window_bell_flag}`), used as a complementary
   signal. Code: `internal/tmux/real.go:179`.

The flag (`@bay-waiting`) is *not* what produces the visual highlight.
The visual highlight is `window-status-style`, which is set only by
the monitor's `setHighlight` (`internal/monitor/monitor.go:348-359`).
The clear-on-focus hook (`internal/cli/setup.go:335`) sets
`@bay-waiting=0` whenever the user selects a window. The monitor on
its next tick sees the flag is 0 and clears the style.

## Likely cause: hook/monitor race

The agent's turn-complete hook sets the flag immediately. The visual
highlight requires the monitor to *separately* set
`window-status-style`. The monitor polls on its tick interval (default
3s). In the gap between hook-set and the next monitor tick:

- If the user focuses the window (because they noticed via some other
  signal), `after-select-window` fires `@bay-waiting=0`. The monitor's
  next tick sees 0 and never applies the style.
- The highlight was never visible despite the agent really being in a
  waiting state.

This race window is *exactly* the workflow the indicator is meant to
serve: a user actively bouncing between bays, attentive to agents
finishing.

## A claim that turned out to be wrong

Initial automated analysis flagged `agentReadyCommand` as broken
because it passes `$TMUX_PANE` (a pane id like `%134`) to
`set-window-option`. Empirical test:

```
$ tmux set-window-option -t %134 @bay-waiting 1
$ tmux show-window-options @bay-waiting
@bay-waiting 1
```

tmux resolves a pane target to its containing window for window-scoped
options. The hook works correctly. Recording this here so we don't
chase it again.

## Verifying empirically

Two checks before changing code:

1. With `bay monitor stop`, trigger an agent turn-complete and inspect
   `tmux show-window-options @bay-waiting` — the flag should be set,
   but no highlight appears. Confirms style application is
   monitor-only.
2. Check `~/.config/bay/waiting-patterns.txt` against current Claude
   permission prompt strings. Permission prompts are mid-turn and
   don't fire turn-complete hooks; they depend entirely on regex. If
   the patterns drift from real Claude output, the regex path silently
   misses.

## Candidate fixes

These are not yet decisions. They are the design space.

### Option A: hook applies style directly

Have the turn-complete hook set both `@bay-waiting=1` *and*
`window-status-style=<highlightStyle>` in one tmux command. The
highlight appears at the moment of turn-complete, no monitor latency.
Monitor still owns clearing logic and regex-based detection for
non-hook cases.

Pros:
- Eliminates the race for the hook path (the common case).
- Keeps the monitor as the central authority for theming.

Cons:
- Two places now know about the style string. Drift risk if the
  monitor's `highlightStyle` constant changes.
- Hook does slightly more work at turn-complete (negligible).

Mitigation for drift: have the hook source the style from a single
place (e.g., `bay status-style waiting`) rather than embedding it
inline.

### Option B: monitor responds to flag changes synchronously

Subscribe the monitor to tmux events (e.g., a hook on
`alert-activity`/`alert-bell` or a custom hook bay sets), or poll at a
much higher rate. The monitor reacts to `@bay-waiting=1` within ~100ms
instead of up to 3s.

Pros:
- Keeps style application centralized in the monitor.
- Improves regex-detection latency too.

Cons:
- More complex monitor loop.
- Higher CPU baseline.
- Still has *some* race window, just narrower.

### Option C: shorter poll interval

Drop the default tick from 3s to ~500ms.

Pros:
- Trivial.

Cons:
- Doesn't actually fix the race (still possible, just less frequent).
- Increases monitor's tmux command load proportionally.

## Adjacent concerns (not blocking)

- **No health surface for the monitor.** If `bay monitor` dies
  silently (crash, tmux server restart killing the parent pane), no
  detection works. `bay doctor` should warn loudly. Worth checking
  whether it does.
- **Regex patterns vs current Claude prompts.** The patterns ship in
  `bay setup` but Claude's prompts evolve. A regression test fixture
  against real prompt strings would prevent silent drift.
- **Permission-prompt detection.** Mid-turn permission prompts ("Allow
  / Deny") don't fire turn-complete hooks at all. Only regex covers
  them. This is the most fragile part of the system.

## Recommended next step

Start with the empirical verification (`monitor stop` repro). If it
confirms the race, implement Option A — it's the smallest change that
fixes the reported workflow. Options B and C can be considered later
if Option A leaves gaps.
