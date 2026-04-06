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
	Prompt string // filter prompt text (default: "> ")
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

// key constants returned by readKey.
const (
	keyEnter     = -1
	keyEscape    = -2
	keyCtrlC     = -3
	keyBackspace = -4
	keyUp        = -5
	keyDown      = -6
	keyEOF       = -7
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

	query := ""
	cursor := 0
	reader := newKeyReader(in)

	for {
		filtered := filter(items, query)

		if cursor >= len(filtered) {
			cursor = len(filtered) - 1
		}
		if cursor < 0 {
			cursor = 0
		}

		render(out, filtered, cursor, prompt, query)

		key := reader.read()

		switch key {
		case keyEnter:
			if len(filtered) == 0 {
				continue
			}
			clearDisplay(out, len(filtered)+1)
			return filtered[cursor].Value, nil

		case keyEscape, keyCtrlC, keyEOF:
			clearDisplay(out, len(filtered)+1)
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

// keyReader reads one logical keystroke at a time from a byte stream.
type keyReader struct {
	in  io.Reader
	buf []byte // unprocessed bytes from previous read
}

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

	case b == 0x03: // Ctrl-C
		r.buf = r.buf[1:]
		return keyCtrlC

	case b == 0x7f: // Backspace
		r.buf = r.buf[1:]
		return keyBackspace

	case b == 0x0e: // Ctrl-N
		r.buf = r.buf[1:]
		return keyDown

	case b == 0x10: // Ctrl-P
		r.buf = r.buf[1:]
		return keyUp

	case b == 0x1b: // Escape or escape sequence
		if len(r.buf) >= 3 && r.buf[1] == '[' {
			arrow := r.buf[2]
			r.buf = r.buf[3:]
			switch arrow {
			case 'A':
				return keyUp
			case 'B':
				return keyDown
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

// render draws the picker state to the output.
func render(out io.Writer, items []Item, cursor int, prompt, query string) {
	fmt.Fprintf(out, "\r\x1b[K%s%s\r\n", prompt, query)
	for i, item := range items {
		if i == cursor {
			fmt.Fprintf(out, "\x1b[7m%s\x1b[0m\x1b[K\r\n", item.Display)
		} else {
			fmt.Fprintf(out, "%s\x1b[K\r\n", item.Display)
		}
	}
	lines := len(items) + 1
	fmt.Fprintf(out, "\x1b[%dA", lines)
	fmt.Fprintf(out, "\r\x1b[%dC", len(prompt)+len(query))
}

// clearDisplay clears the picker display area.
func clearDisplay(out io.Writer, lines int) {
	fmt.Fprint(out, "\r")
	for i := 0; i < lines; i++ {
		fmt.Fprint(out, "\x1b[K\r\n")
	}
	fmt.Fprintf(out, "\x1b[%dA", lines)
}
