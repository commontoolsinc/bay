// Package picker provides a built-in interactive fuzzy picker for terminal use.
// It replaces the external fzf dependency for bay's navigation commands.
package picker

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Item is something the picker can display and select.
type Item struct {
	Display string // formatted line for display
	Value   int    // opaque identifier returned on selection
}

// Options configures picker behavior.
type Options struct {
	Prompt   string // filter prompt text (default: "> ")
	Selected int    // initial cursor position (0-based index into items)
}

// Interface defines a fuzzy picker. Implementations include the built-in
// terminal picker and potentially external tools like fzf.
type Interface interface {
	Pick(items []Item, opts Options) (int, error)
}

// Builtin is the built-in terminal picker.
type Builtin struct{}

// Pick runs the built-in interactive picker on stdin/stdout.
func (b *Builtin) Pick(items []Item, opts Options) (int, error) {
	return Run(items, opts, os.Stdin, os.Stdout)
}

// Key constants returned by a KeyReader. Negative values indicate named
// keys; zero-positive values are literal rune codes for printable input.
const (
	KeyEnter     = -1
	KeyEscape    = -2
	KeyCtrlC     = -3
	KeyBackspace = -4
	KeyUp        = -5
	KeyDown      = -6
	KeyEOF       = -7
	KeyLeft      = -8
	KeyRight     = -9
	KeyHome      = -10
	KeyEnd       = -11
	KeyDelete    = -12
	KeyCtrlU     = -13
	KeyTab       = -14
)

// Legacy lowercase aliases (picker.go's own switches pre-date the export).
const (
	keyEnter     = KeyEnter
	keyEscape    = KeyEscape
	keyCtrlC     = KeyCtrlC
	keyBackspace = KeyBackspace
	keyUp        = KeyUp
	keyDown      = KeyDown
	keyEOF       = KeyEOF
	keyLeft      = KeyLeft
	keyRight     = KeyRight
	keyHome      = KeyHome
	keyEnd       = KeyEnd
	keyDelete    = KeyDelete
	keyCtrlU     = KeyCtrlU
	keyTab       = KeyTab
)

// Run displays an interactive picker and returns the selected item's Value.
// Returns -1 if the user cancelled (Escape, Ctrl-C, or EOF) or if items is empty.
func Run(items []Item, opts Options, in *os.File, out *os.File) (int, error) {
	if len(items) == 0 {
		return -1, nil
	}

	prompt := opts.Prompt
	if prompt == "" {
		prompt = "> "
	}

	// Try to enter raw mode (works for real terminals, fails for pipes in tests).
	fd := int(in.Fd())
	oldState, rawErr := term.MakeRaw(fd)
	if rawErr == nil {
		defer term.Restore(fd, oldState)
	}

	// Detect display width so we can truncate long lines. Any line wider
	// than the terminal/popup wraps to a second row, which breaks our
	// cursor math (one-line-per-item) and causes the display to march
	// down the screen as the user navigates.
	width := 0
	if tw, _, err := term.GetSize(fd); err == nil && tw > 0 {
		width = tw
	}

	query := ""
	cursor := opts.Selected
	if cursor < 0 || cursor >= len(items) {
		cursor = 0
	}
	reader := newKeyReader(in)
	prevHeight := 0

	for {
		filtered := filter(items, query)

		if cursor >= len(filtered) {
			cursor = len(filtered) - 1
		}
		if cursor < 0 {
			cursor = 0
		}

		prevHeight = renderPicker(out, filtered, cursor, prompt, query, prevHeight, width)

		key := reader.read()

		switch key {
		case keyEnter:
			if len(filtered) == 0 {
				continue
			}
			clearDisplay(out, prevHeight)
			return filtered[cursor].Value, nil

		case keyEscape, keyCtrlC, keyEOF:
			clearDisplay(out, prevHeight)
			return -1, nil

		case keyDown:
			if cursor < len(filtered)-1 {
				cursor++
			}

		case keyUp:
			if cursor > 0 {
				cursor--
			}

		case keyBackspace:
			if len(query) > 0 {
				query = query[:len(query)-1]
				cursor = 0
			}

		default:
			if key >= 0x20 && key < 0x7f {
				query += string(rune(key))
				cursor = 0
			}
		}
	}
}

// KeyReader reads one logical keystroke at a time from a byte stream. It
// handles raw-mode terminal input including escape sequences and
// pre-defined Ctrl-combinations. Use NewKeyReader to construct one.
type KeyReader = keyReader

// keyReader reads one logical keystroke at a time from a byte stream.
type keyReader struct {
	in  io.Reader
	buf []byte // unprocessed bytes from previous read
}

// NewKeyReader returns a KeyReader that reads from in. Callers that need
// raw-mode input should wrap the underlying tty with term.MakeRaw before
// reading.
func NewKeyReader(in io.Reader) *KeyReader {
	return newKeyReader(in)
}

// Read returns the next key event. Printable chars return their rune
// value; special keys return negative Key* constants.
func (r *keyReader) Read() int { return r.read() }

func newKeyReader(in io.Reader) *keyReader {
	return &keyReader{in: in}
}

// read returns the next key event. Printable chars return their rune value.
// Special keys return negative constants (keyEnter, keyUp, etc.).
func (r *keyReader) read() int {
	// Refill buffer if empty.
	if len(r.buf) == 0 {
		tmp := make([]byte, 64)
		n, err := r.in.Read(tmp)
		if err != nil || n == 0 {
			return keyEOF
		}
		r.buf = tmp[:n]
	}

	b := r.buf[0]

	switch {
	case b == '\r' || b == '\n':
		r.buf = r.buf[1:]
		return keyEnter

	case b == '\t':
		r.buf = r.buf[1:]
		return keyTab

	case b == 0x03: // Ctrl-C
		r.buf = r.buf[1:]
		return keyCtrlC

	case b == 0x7f: // Backspace
		r.buf = r.buf[1:]
		return keyBackspace

	case b == 0x15: // Ctrl-U — clear line (readline convention).
		r.buf = r.buf[1:]
		return keyCtrlU

	case b == 0x0e: // Ctrl-N
		r.buf = r.buf[1:]
		return keyDown

	case b == 0x10: // Ctrl-P
		r.buf = r.buf[1:]
		return keyUp

	case b == 0x01: // Ctrl-A
		r.buf = r.buf[1:]
		return keyHome

	case b == 0x05: // Ctrl-E
		r.buf = r.buf[1:]
		return keyEnd

	case b == 0x1b: // Escape or escape sequence
		if len(r.buf) >= 3 && r.buf[1] == '[' {
			seq := r.buf[2]
			// Some terminals emit three-byte sequences (ESC [ A), others
			// four (ESC [ 3 ~). Handle the four-byte Delete/Home/End cases.
			if len(r.buf) >= 4 && r.buf[3] == '~' {
				switch seq {
				case '3':
					r.buf = r.buf[4:]
					return keyDelete
				case '1', '7':
					r.buf = r.buf[4:]
					return keyHome
				case '4', '8':
					r.buf = r.buf[4:]
					return keyEnd
				}
			}
			r.buf = r.buf[3:]
			switch seq {
			case 'A':
				return keyUp
			case 'B':
				return keyDown
			case 'C':
				return keyRight
			case 'D':
				return keyLeft
			case 'H':
				return keyHome
			case 'F':
				return keyEnd
			default:
				return -100 // unknown sequence, ignore
			}
		}
		// Bare escape
		r.buf = r.buf[1:]
		return keyEscape

	case b >= 0x20 && b < 0x7f: // Printable
		r.buf = r.buf[1:]
		return int(b)

	default:
		r.buf = r.buf[1:]
		return -100 // unknown, ignore
	}
}

// filter returns items whose Display contains query (case-insensitive).
func filter(items []Item, query string) []Item {
	if query == "" {
		return items
	}
	q := strings.ToLower(query)
	var result []Item
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Display), q) {
			result = append(result, item)
		}
	}
	return result
}

// renderPicker draws the picker and returns the total height (for clearing).
// prevHeight is the height of the previous render — any extra lines are erased.
// width is the display width; if > 0, lines are truncated to fit so they
// never wrap (a wrapped line would break our one-line-per-item cursor math).
func renderPicker(out io.Writer, items []Item, cursor int, prompt, query string, prevHeight, width int) int {
	fmt.Fprintf(out, "\r\x1b[K%s%s\r\n", prompt, query)
	for i, item := range items {
		display := truncateDisplay(item.Display, width)
		if i == cursor {
			fmt.Fprintf(out, "\x1b[7m%s\x1b[0m\x1b[K\r\n", display)
		} else {
			fmt.Fprintf(out, "%s\x1b[K\r\n", display)
		}
	}
	height := len(items) + 1
	// Clear leftover lines from a previous longer render.
	for i := height; i < prevHeight; i++ {
		fmt.Fprint(out, "\x1b[K\r\n")
	}
	totalHeight := height
	if prevHeight > height {
		totalHeight = prevHeight
	}
	fmt.Fprintf(out, "\x1b[%dA", totalHeight)
	fmt.Fprintf(out, "\r\x1b[%dC", len(prompt)+len(query))
	return height
}

// truncateDisplay shortens s to fit within maxCells terminal cells, appending
// "…" if shortened. maxCells == 0 means no truncation (width unknown).
// We leave one cell of margin from the true width to avoid edge cases with
// terminals that wrap at exactly column=width.
func truncateDisplay(s string, maxCells int) string {
	if maxCells <= 0 {
		return s
	}
	budget := maxCells - 1
	runes := []rune(s)
	if len(runes) <= budget {
		return s
	}
	if budget <= 1 {
		return string(runes[:budget])
	}
	return string(runes[:budget-1]) + "…"
}

// clearDisplay clears the picker display area.
func clearDisplay(out io.Writer, lines int) {
	fmt.Fprint(out, "\r")
	for range lines {
		fmt.Fprint(out, "\x1b[K\r\n")
	}
	fmt.Fprintf(out, "\x1b[%dA", lines)
}

// Prompt shows a single-line editable text input and returns the submitted
// value. Returns (value, true, nil) on Enter, ("", false, nil) on Esc or
// Ctrl-C. Cursor is positioned at end of prefill when the prompt opens.
//
// Controls:
//
//   - printable chars insert at cursor
//   - Backspace deletes one rune before the cursor
//   - Ctrl-U clears the entire line (critical for rename with prefill —
//     lets users type-over an entire current name without holding
//     Backspace)
//   - Left/Right move the cursor; Home / Ctrl-A jump to start; End /
//     Ctrl-E jump to end; Delete removes the rune at the cursor
//   - Enter submits; Esc / Ctrl-C cancel
func Prompt(label, prefill string, in *os.File, out *os.File) (string, bool, error) {
	fd := int(in.Fd())
	oldState, rawErr := term.MakeRaw(fd)
	if rawErr == nil {
		defer term.Restore(fd, oldState)
	}

	runes := []rune(prefill)
	cursor := len(runes)
	reader := newKeyReader(in)

	for {
		renderPrompt(out, label, runes, cursor)
		key := reader.read()

		switch key {
		case keyEnter:
			clearPrompt(out)
			return string(runes), true, nil

		case keyEscape, keyCtrlC, keyEOF:
			clearPrompt(out)
			return "", false, nil

		case keyBackspace:
			if cursor > 0 {
				runes = append(runes[:cursor-1], runes[cursor:]...)
				cursor--
			}

		case keyDelete:
			if cursor < len(runes) {
				runes = append(runes[:cursor], runes[cursor+1:]...)
			}

		case keyCtrlU:
			runes = runes[:0]
			cursor = 0

		case keyLeft:
			if cursor > 0 {
				cursor--
			}

		case keyRight:
			if cursor < len(runes) {
				cursor++
			}

		case keyHome:
			cursor = 0

		case keyEnd:
			cursor = len(runes)

		case keyTab:
			// Reserved for future completion UX; ignore for now so Tab
			// doesn't accidentally insert a literal tab character.

		default:
			if key >= 0x20 && key < 0x7f {
				runes = append(runes[:cursor], append([]rune{rune(key)}, runes[cursor:]...)...)
				cursor++
			}
		}
	}
}

func renderPrompt(out io.Writer, label string, runes []rune, cursor int) {
	// CR, clear line, label + content, then CR + forward to cursor.
	fmt.Fprintf(out, "\r\x1b[K%s%s", label, string(runes))
	col := len(label) + cursor
	fmt.Fprintf(out, "\r")
	if col > 0 {
		fmt.Fprintf(out, "\x1b[%dC", col)
	}
}

func clearPrompt(out io.Writer) {
	fmt.Fprint(out, "\r\x1b[K")
}
