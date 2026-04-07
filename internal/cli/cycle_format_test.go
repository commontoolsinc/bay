package cli

import "testing"

func TestFormatCycleMessage(t *testing.T) {
	tests := []struct {
		name       string
		items      []string
		currentIdx int
		termWidth  int
		want       string
	}{
		{
			name:       "window of 5 in middle of long list",
			items:      []string{"a", "b", "c", "d", "e", "f", "g", "h"},
			currentIdx: 3,
			termWidth:  100,
			want:       "[4/8]  b  c  #[bold]d#[default]  e  f",
		},
		{
			name:       "target at start slides window left",
			items:      []string{"a", "b", "c", "d", "e", "f", "g", "h"},
			currentIdx: 0,
			termWidth:  100,
			want:       "[1/8]  #[bold]a#[default]  b  c  d  e",
		},
		{
			name:       "target at index 1 slides window left",
			items:      []string{"a", "b", "c", "d", "e", "f", "g", "h"},
			currentIdx: 1,
			termWidth:  100,
			want:       "[2/8]  a  #[bold]b#[default]  c  d  e",
		},
		{
			name:       "target at end slides window right",
			items:      []string{"a", "b", "c", "d", "e", "f", "g", "h"},
			currentIdx: 7,
			termWidth:  100,
			want:       "[8/8]  d  e  f  g  #[bold]h#[default]",
		},
		{
			name:       "target at second-to-last slides window right",
			items:      []string{"a", "b", "c", "d", "e", "f", "g", "h"},
			currentIdx: 6,
			termWidth:  100,
			want:       "[7/8]  d  e  f  #[bold]g#[default]  h",
		},
		{
			name:       "list shorter than window shows all items",
			items:      []string{"a", "b", "c"},
			currentIdx: 1,
			termWidth:  100,
			want:       "[2/3]  a  #[bold]b#[default]  c",
		},
		{
			name:       "single item",
			items:      []string{"only"},
			currentIdx: 0,
			termWidth:  100,
			want:       "[1/1]  #[bold]only#[default]",
		},
		{
			name:       "long name truncated to 12 visible chars with ellipsis",
			items:      []string{"this-is-a-very-long-name", "b"},
			currentIdx: 0,
			termWidth:  100,
			want:       "[1/2]  #[bold]this-is-a-v\u2026#[default]  b",
		},
		{
			name:       "narrow width drops window from 5 to 3",
			items:      []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc", "dddddddddddd", "eeeeeeeeeeee", "ffffffffffff", "gggggggggggg"},
			currentIdx: 3,
			termWidth:  50,
			want:       "[4/7]  cccccccccccc  #[bold]dddddddddddd#[default]  eeeeeeeeeeee",
		},
		{
			name:       "very narrow width drops window to 1",
			items:      []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc", "dddddddddddd", "eeeeeeeeeeee"},
			currentIdx: 2,
			termWidth:  25,
			want:       "[3/5]  #[bold]cccccccccccc#[default]",
		},
		{
			name:       "termWidth zero defaults to wide",
			items:      []string{"a", "b", "c"},
			currentIdx: 0,
			termWidth:  0,
			want:       "[1/3]  #[bold]a#[default]  b  c",
		},
		{
			name:       "empty list returns empty",
			items:      nil,
			currentIdx: 0,
			termWidth:  100,
			want:       "",
		},
		{
			name:       "out-of-range index returns empty",
			items:      []string{"a", "b"},
			currentIdx: 5,
			termWidth:  100,
			want:       "",
		},
		{
			name:       "negative index returns empty",
			items:      []string{"a", "b"},
			currentIdx: -1,
			termWidth:  100,
			want:       "",
		},
		{
			name:       "two-item list with target at start",
			items:      []string{"alpha", "beta"},
			currentIdx: 0,
			termWidth:  100,
			want:       "[1/2]  #[bold]alpha#[default]  beta",
		},
		{
			name:       "two-item list with target at end",
			items:      []string{"alpha", "beta"},
			currentIdx: 1,
			termWidth:  100,
			want:       "[2/2]  alpha  #[bold]beta#[default]",
		},
		{
			name:       "position counter handles two-digit totals",
			items:      []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"},
			currentIdx: 9,
			termWidth:  100,
			want:       "[10/12]  h  i  #[bold]j#[default]  k  l",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCycleMessage(tt.items, tt.currentIdx, tt.termWidth)
			if got != tt.want {
				t.Errorf("formatCycleMessage() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

// TestFormatCycleMessage_TruncationBoundary covers the 12-char and 13-char
// boundary: 12 chars shown unmodified, 13 chars truncated to 11 + ellipsis.
func TestFormatCycleMessage_TruncationBoundary(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"abcdefghijkl", "[1/2]  #[bold]abcdefghijkl#[default]  short"},       // 12: kept
		{"abcdefghijklm", "[1/2]  #[bold]abcdefghijk\u2026#[default]  short"}, // 13: truncated
	}
	for _, tc := range cases {
		got := formatCycleMessage([]string{tc.name, "short"}, 0, 100)
		if got != tc.want {
			t.Errorf("name=%q:\n  got  %q\n  want %q", tc.name, got, tc.want)
		}
	}
}

// TestFormatCycleMessage_EscapesTmuxFormatChars verifies that `#` in user
// names is escaped to `##` so tmux doesn't interpret #[...] as a directive.
// Surface names from --name flag aren't validated against tmux format chars.
func TestFormatCycleMessage_EscapesTmuxFormatChars(t *testing.T) {
	got := formatCycleMessage([]string{"a#[bg=red]b", "plain"}, 0, 100)
	want := "[1/2]  #[bold]a##[bg=red]b#[default]  plain"
	if got != want {
		t.Errorf("# should be escaped to ##:\n  got  %q\n  want %q", got, want)
	}

	// Two #s in input should become four #s.
	got = formatCycleMessage([]string{"a##b", "x"}, 1, 100)
	want = "[2/2]  a####b  #[bold]x#[default]"
	if got != want {
		t.Errorf("## should be escaped to ####:\n  got  %q\n  want %q", got, want)
	}
}
