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
