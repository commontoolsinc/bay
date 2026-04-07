// Visual cycling flash. Pressing Option+j/k or Option+J/K flashes a tmux
// display-message that shows the user's new position in the surrounding list:
//
//	[4/8]  monitor  agent  shell  tests  alpha
//
// where the target item ("shell" here) is rendered bold via the tmux format
// codes #[bold]name#[default]. The window of items shown around the target
// slides as the target approaches the start or end of the list, and shrinks
// from 5 → 3 → 1 if the terminal is too narrow.
package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/commontoolsinc/bay/internal/engine"
	"github.com/commontoolsinc/bay/internal/nav"
)

const (
	cycleMaxNameLen = 12
	cycleSeparator  = "  "
)

// formatCycleMessage builds the flash message for a list of names. items is
// the full list (in cycle order), currentIdx is the 0-based target, and
// termWidth sizes the visible window. The result includes tmux format codes
// (#[bold] / #[default]) and is suitable for passing to tmux display-message.
func formatCycleMessage(items []string, currentIdx int, termWidth int) string {
	n := len(items)
	if n == 0 || currentIdx < 0 || currentIdx >= n {
		return ""
	}
	if termWidth <= 0 {
		termWidth = 100
	}

	counter := fmt.Sprintf("[%d/%d]", currentIdx+1, n)

	windowSize := 1
	for _, w := range []int{5, 3, 1} {
		if w > n {
			w = n
		}
		if cycleFits(items, currentIdx, w, termWidth, counter) {
			windowSize = w
			break
		}
	}

	lo, hi := cycleWindowRange(n, currentIdx, windowSize)

	var b strings.Builder
	b.WriteString(counter)
	b.WriteString(cycleSeparator)
	for i := lo; i < hi; i++ {
		if i > lo {
			b.WriteString(cycleSeparator)
		}
		name := escapeTmuxFormat(truncateName(items[i], cycleMaxNameLen))
		if i == currentIdx {
			b.WriteString("#[bold]")
			b.WriteString(name)
			b.WriteString("#[default]")
		} else {
			b.WriteString(name)
		}
	}
	return b.String()
}

// cycleWindowRange returns [lo, hi) of length up to windowSize, centered on
// currentIdx and clamped to [0, n). When currentIdx is near a boundary the
// window slides — its endpoints pin to 0 or n and the cursor is no longer
// centered, giving the user a visible cue that they are at the edge.
func cycleWindowRange(n, currentIdx, windowSize int) (int, int) {
	if windowSize >= n {
		return 0, n
	}
	half := windowSize / 2
	lo := currentIdx - half
	hi := lo + windowSize
	if lo < 0 {
		lo = 0
		hi = lo + windowSize
	}
	if hi > n {
		hi = n
		lo = hi - windowSize
	}
	return lo, hi
}

// cycleFits reports whether the window fits within termWidth. Tmux format
// codes (#[bold] / #[default]) render at zero width and are not counted.
func cycleFits(items []string, currentIdx, windowSize, termWidth int, counter string) bool {
	if windowSize > len(items) {
		windowSize = len(items)
	}
	lo, hi := cycleWindowRange(len(items), currentIdx, windowSize)
	width := utf8.RuneCountInString(counter) + len(cycleSeparator)
	for i := lo; i < hi; i++ {
		if i > lo {
			width += len(cycleSeparator)
		}
		r := utf8.RuneCountInString(items[i])
		if r > cycleMaxNameLen {
			r = cycleMaxNameLen
		}
		width += r
	}
	return width <= termWidth
}

// truncateName truncates s to maxLen visible runes, replacing the trailing
// rune with an ellipsis (…) when truncation occurred.
func truncateName(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	runes := []rune(s)
	return string(runes[:maxLen-1]) + "…"
}

// escapeTmuxFormat escapes `#` to `##` so the name can't inject tmux format
// directives like #[bg=red]. Tmux's format string convention is that `##`
// renders as a literal `#`, which also defangs any `#[...]` directive.
func escapeTmuxFormat(s string) string {
	return strings.ReplaceAll(s, "#", "##")
}

// flashCycleMessage is the chokepoint that builds the message and shells
// out to tmux. Errors are silent: failing to flash should never break
// navigation. Caller must pre-build the names slice.
func flashCycleMessage(eng *engine.Engine, names []string, currentIdx int) {
	width, _ := eng.Tmux.ClientWidth()
	msg := formatCycleMessage(names, currentIdx, width)
	if msg == "" {
		return
	}
	_ = eng.Tmux.DisplayMessage(msg)
}

// flashSurfaceCycle flashes the cycling indicator for surface entries.
// No-ops on lists shorter than 2 (nothing to cycle to).
func flashSurfaceCycle(eng *engine.Engine, entries []nav.SurfaceEntry, currentIdx int) {
	if len(entries) < 2 {
		return
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	flashCycleMessage(eng, names, currentIdx)
}

// flashWsCycle flashes the cycling indicator for workspace entries.
// No-ops on lists shorter than 2.
func flashWsCycle(eng *engine.Engine, entries []nav.Entry, currentIdx int) {
	if len(entries) < 2 {
		return
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.WsName
	}
	flashCycleMessage(eng, names, currentIdx)
}
