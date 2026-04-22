package picker

import (
	"os"
	"testing"
	"time"
)

func feedKeys(t *testing.T, keys string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	go func() {
		defer w.Close()
		// Small delay to let the picker start reading.
		time.Sleep(10 * time.Millisecond)
		w.Write([]byte(keys))
	}()
	return r
}

func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("opening /dev/null: %v", err)
	}
	return f
}

func testItems() []Item {
	return []Item{
		{Display: "agent   claude", Value: 0},
		{Display: "shell", Value: 1},
		{Display: "tests   npm test", Value: 2},
	}
}

func TestRun_SelectFirst(t *testing.T) {
	in := feedKeys(t, "\r") // Enter selects first item
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	result, err := Run(testItems(), Options{}, in, out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != 0 {
		t.Errorf("result = %d, want 0", result)
	}
}

func TestRun_MoveDownAndSelect(t *testing.T) {
	in := feedKeys(t, "\x1b[B\r") // Down arrow + Enter
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	result, err := Run(testItems(), Options{}, in, out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != 1 {
		t.Errorf("result = %d, want 1", result)
	}
}

func TestRun_Escape(t *testing.T) {
	in := feedKeys(t, "\x1b") // Escape
	// Need a small delay then EOF to distinguish bare escape from escape sequence
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	result, err := Run(testItems(), Options{}, in, out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != -1 {
		t.Errorf("result = %d, want -1 (cancelled)", result)
	}
}

func TestRun_CtrlC(t *testing.T) {
	in := feedKeys(t, "\x03") // Ctrl-C
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	result, err := Run(testItems(), Options{}, in, out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != -1 {
		t.Errorf("result = %d, want -1 (cancelled)", result)
	}
}

func TestRun_EmptyList(t *testing.T) {
	in := feedKeys(t, "\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	result, err := Run(nil, Options{}, in, out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != -1 {
		t.Errorf("result = %d, want -1 (no items)", result)
	}
}

func TestRun_FilterAndSelect(t *testing.T) {
	in := feedKeys(t, "she\r") // Type "she" + Enter → matches "shell"
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	result, err := Run(testItems(), Options{}, in, out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != 1 {
		t.Errorf("result = %d, want 1 (shell)", result)
	}
}

func TestRun_FilterNoMatch(t *testing.T) {
	in := feedKeys(t, "zzz\r") // No match, Enter does nothing useful → then Escape
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	// With no matches, Enter should be a no-op; the pipe closes (EOF) which cancels.
	result, err := Run(testItems(), Options{}, in, out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != -1 {
		t.Errorf("result = %d, want -1 (no matches)", result)
	}
}

func TestTruncateDisplay(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"no max", "hello world", 0, "hello world"},
		{"fits with margin", "hello", 10, "hello"},
		// budget = max-1 = 7 cells; prefix 6 runes + "…" = 7 cells
		{"truncates with ellipsis", "hello world", 8, "hello …"},
		{"exact margin boundary", "abcdef", 7, "abcdef"},
		{"no margin, needs truncate", "abcdef", 6, "abcd…"},
		{"tiny budget", "hello", 2, "h"},
		{"zero budget", "hello", 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDisplay(tt.in, tt.max)
			if got != tt.want {
				t.Errorf("truncateDisplay(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func TestPrompt_AcceptsPrefillOnEnter(t *testing.T) {
	in := feedKeys(t, "\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	got, ok, err := Prompt("rename: ", "auth-fix", in, out)
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if !ok {
		t.Fatal("ok=false; want true")
	}
	if got != "auth-fix" {
		t.Errorf("got %q; want prefill", got)
	}
}

func TestPrompt_TypesAfterPrefill(t *testing.T) {
	in := feedKeys(t, "-2\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	got, ok, _ := Prompt("rename: ", "auth", in, out)
	if !ok || got != "auth-2" {
		t.Errorf("got (%q, %v); want (auth-2, true)", got, ok)
	}
}

func TestPrompt_BackspaceDeletes(t *testing.T) {
	in := feedKeys(t, "\x7f\x7f\r") // two backspaces then Enter
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	got, ok, _ := Prompt("> ", "abc", in, out)
	if !ok || got != "a" {
		t.Errorf("got (%q, %v); want (a, true)", got, ok)
	}
}

func TestPrompt_CtrlUClears(t *testing.T) {
	in := feedKeys(t, "\x15new\r") // Ctrl-U, type "new", Enter
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	got, ok, _ := Prompt("> ", "old-name", in, out)
	if !ok || got != "new" {
		t.Errorf("got (%q, %v); want (new, true)", got, ok)
	}
}

func TestPrompt_LeftRightArrows(t *testing.T) {
	// Start with prefill "ac", move left once, insert 'b' → "abc".
	in := feedKeys(t, "\x1b[Db\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	got, ok, _ := Prompt("> ", "ac", in, out)
	if !ok || got != "abc" {
		t.Errorf("got (%q, %v); want (abc, true)", got, ok)
	}
}

func TestPrompt_HomeEndAndCtrlAE(t *testing.T) {
	// Start with "end", Ctrl-A (home), type "X" → "Xend"
	in := feedKeys(t, "\x01X\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	got, ok, _ := Prompt("> ", "end", in, out)
	if !ok || got != "Xend" {
		t.Errorf("got (%q, %v); want (Xend, true)", got, ok)
	}
}

func TestPrompt_DeleteKey(t *testing.T) {
	// Start with "abc", Home, Delete → "bc"
	in := feedKeys(t, "\x01\x1b[3~\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	got, ok, _ := Prompt("> ", "abc", in, out)
	if !ok || got != "bc" {
		t.Errorf("got (%q, %v); want (bc, true)", got, ok)
	}
}

func TestPrompt_EscapeCancels(t *testing.T) {
	in := feedKeys(t, "\x1b")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	got, ok, _ := Prompt("> ", "prefill", in, out)
	if ok || got != "" {
		t.Errorf("got (%q, %v); want ('', false)", got, ok)
	}
}
