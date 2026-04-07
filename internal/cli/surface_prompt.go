package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// agentClosePrompt asks the user whether to close an agent surface.
//
// Returns true if the user typed "y" or "yes" (case-insensitive). Any other
// input — empty line, "n", "no", EOF — returns false. Reads from in, writes
// the prompt to out.
func agentClosePrompt(in io.Reader, out io.Writer, surfaceName string) bool {
	fmt.Fprintf(out, "Close agent surface %q? (y/N): ", surfaceName)
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// stdinIsTTY reports whether stdin is connected to a terminal. Used to
// suppress interactive prompts when bay is invoked from a script or pipe.
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
