package cli

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractBayBlock(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantFound bool
		wantBlock string
	}{
		{
			name:      "no block",
			content:   "set -g mouse on\nbind r source-file ~/.tmux.conf\n",
			wantFound: false,
		},
		{
			name: "block at end of file",
			content: `set -g mouse on

# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
bind-key -n M-k run-shell 'bay surface prev'
`,
			wantFound: true,
			wantBlock: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
bind-key -n M-k run-shell 'bay surface prev'`,
		},
		{
			name: "block followed by other config separated by blank line",
			content: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'

set -g status-bg blue
`,
			wantFound: true,
			wantBlock: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'`,
		},
		{
			name: "block followed by other config with no blank line",
			content: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
set -g status-bg blue
`,
			wantFound: true,
			wantBlock: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'`,
		},
		{
			name: "block at very start of file",
			content: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
`,
			wantFound: true,
			wantBlock: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'`,
		},
		{
			name: "block contains commented binding (still in block)",
			content: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
# bind-key -n M-k run-shell 'bay surface prev'
bind-key -n M-g display-popup -E 'bay go'
`,
			wantFound: true,
			wantBlock: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
# bind-key -n M-k run-shell 'bay surface prev'
bind-key -n M-g display-popup -E 'bay go'`,
		},
		{
			name: "marker only — no following lines",
			content: `set -g mouse on
# Bay keybindings
`,
			wantFound: true,
			wantBlock: `# Bay keybindings`,
		},
		{
			name: "marker with leading whitespace",
			content: `set -g mouse on
   # Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
`,
			wantFound: true,
			wantBlock: `   # Bay keybindings
bind-key -n M-j run-shell 'bay surface next'`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block, found := extractBayBlock(tt.content)
			if found != tt.wantFound {
				t.Fatalf("found = %v, want %v", found, tt.wantFound)
			}
			if found && block != tt.wantBlock {
				t.Errorf("block mismatch:\ngot:\n%s\nwant:\n%s", block, tt.wantBlock)
			}
		})
	}
}

func TestCommandsInBlock(t *testing.T) {
	block := `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
# bind-key -n M-k run-shell 'bay surface prev'
bind-key -n M-g display-popup -E 'bay go'
bind -n M-x run-shell 'bay shell'
`
	got := commandsInBlock(block)
	want := map[string]bool{
		"bay surface next": true,
		"bay go":           true,
		"bay shell":        true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commandsInBlock() = %v, want %v", got, want)
	}
}

func TestCommandsInBlock_DoubleQuotes(t *testing.T) {
	// Users frequently mix quote styles in tmux configs. The diff
	// must recognize a double-quoted command, otherwise rebound bay
	// commands get falsely flagged as missing every setup run.
	block := `# Bay keybindings
bind-key -n M-j run-shell "bay surface next"
bind-key -n M-k run-shell 'bay surface prev'
`
	got := commandsInBlock(block)
	want := map[string]bool{
		"bay surface next": true,
		"bay surface prev": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commandsInBlock() = %v, want %v", got, want)
	}
}

func TestCommandsInBlock_TmuxNativeUnquoted(t *testing.T) {
	// Tmux-native bindings (like the M-h/l/HJKL nav set bay ships) emit
	// `bind-key -n KEY <tmux-cmd>` with no quoted shell payload. The
	// command extractor must still capture them, otherwise bay falsely
	// reports them as missing every setup run.
	block := `# Bay keybindings
bind-key -n M-h previous-window
bind-key -n M-l next-window
bind-key -n M-H select-pane -L
bind-key -n M-J select-pane -D
bind-key -n M-g run-shell 'bay ws go --pick || true'
`
	got := commandsInBlock(block)
	want := map[string]bool{
		"previous-window":          true,
		"next-window":              true,
		"select-pane -L":           true,
		"select-pane -D":           true,
		"bay ws go --pick || true": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commandsInBlock() = %v, want %v", got, want)
	}
}

func TestCommandsInBlock_IgnoresCommentedBindings(t *testing.T) {
	// Critical: the commented binding must NOT show up. This is the
	// core regression — substring-based detection treated commented
	// bindings as present.
	block := `# Bay keybindings
# bind-key -n M-j run-shell 'bay surface next'
`
	got := commandsInBlock(block)
	if got["bay surface next"] {
		t.Errorf("commented binding should not be in commandsInBlock; got %v", got)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestConflictingKeys(t *testing.T) {
	kbs := []bayKeybinding{
		{key: "M-j", cmd: "bay surface next", tmuxVerb: "run-shell"},
		{key: "M-k", cmd: "bay surface prev", tmuxVerb: "run-shell"},
		{key: "M-g", cmd: "bay go", tmuxVerb: "display-popup -E"},
	}

	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "no conflicts",
			content: "set -g mouse on\n",
			want:    nil,
		},
		{
			name: "user has M-j bound to something else",
			content: `bind-key -n M-j send-keys "echo hi" Enter
`,
			want: []string{"M-j"},
		},
		{
			name: "user has M-k bound via short bind syntax",
			content: `bind -n M-k display-message "hi"
`,
			want: []string{"M-k"},
		},
		{
			name: "commented conflict is NOT a conflict",
			content: `# bind-key -n M-j send-keys "echo hi" Enter
`,
			want: nil,
		},
		{
			name: "two real conflicts",
			content: `bind-key -n M-j send-keys "x" Enter
bind -n M-g display-message "y"
`,
			want: []string{"M-j", "M-g"},
		},
		{
			name: "binding for similar but different key (M-J vs M-j) is not a conflict",
			content: `bind-key -n M-J send-keys "x" Enter
`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := conflictingKeys(tt.content, kbs)
			// Order is keyed off kbs order; sort both for stable compare.
			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("conflictingKeys() = %v, want %v", got, want)
			}
		})
	}
}

func TestMissingCanonicalLines(t *testing.T) {
	kbs := []bayKeybinding{
		{key: "M-j", cmd: "bay surface next", tmuxVerb: "run-shell"},
		{key: "M-k", cmd: "bay surface prev", tmuxVerb: "run-shell"},
		{key: "M-g", cmd: "bay go", tmuxVerb: "display-popup -E"},
	}

	t.Run("user has all canonical commands (even on different keys)", func(t *testing.T) {
		// Diff is by command, not by key. The user rebound everything
		// to different keys but kept the same commands — should report
		// nothing missing.
		block := `# Bay keybindings
bind-key -n C-j run-shell 'bay surface next'
bind-key -n C-k run-shell 'bay surface prev'
bind-key -n C-g display-popup -E 'bay go'
`
		got := missingCanonicalLines(block, kbs)
		if len(got) != 0 {
			t.Errorf("expected no missing lines, got %v", got)
		}
	})

	t.Run("user is missing one canonical command", func(t *testing.T) {
		block := `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
bind-key -n M-k run-shell 'bay surface prev'
`
		got := missingCanonicalLines(block, kbs)
		want := []string{"bind-key -n M-g display-popup -E 'bay go || true'"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("missingCanonicalLines() = %v, want %v", got, want)
		}
	})

	t.Run("user commented out a binding", func(t *testing.T) {
		block := `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
# bind-key -n M-k run-shell 'bay surface prev'
bind-key -n M-g display-popup -E 'bay go'
`
		got := missingCanonicalLines(block, kbs)
		want := []string{"bind-key -n M-k run-shell 'bay surface prev || true'"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("missingCanonicalLines() = %v, want %v", got, want)
		}
	})

	t.Run("empty block reports everything missing", func(t *testing.T) {
		block := `# Bay keybindings`
		got := missingCanonicalLines(block, kbs)
		if len(got) != len(kbs) {
			t.Errorf("expected %d missing lines, got %d: %v", len(kbs), len(got), got)
		}
	})
}
