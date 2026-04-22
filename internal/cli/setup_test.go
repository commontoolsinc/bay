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

	t.Run("commented binding is an opt-out, not missing", func(t *testing.T) {
		// A user who removed a binding and left a commented stub is
		// signaling "don't re-add this." missingCanonicalLines must
		// treat commented bindings as present so bay doesn't nag.
		block := `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
# bind-key -n M-k run-shell 'bay surface prev'
bind-key -n M-g display-popup -E 'bay go'
`
		got := missingCanonicalLines(block, kbs)
		if len(got) != 0 {
			t.Errorf("missingCanonicalLines() = %v, want empty (commented line should be opt-out)", got)
		}
	})

	t.Run("duplicate-command canonical bindings require each key", func(t *testing.T) {
		// M-j and M-J both run `select-pane -D` by design (Shift-held
		// chords shouldn't lose the modifier). If only M-J is bound,
		// M-j must still be reported missing — command match alone is
		// ambiguous for duplicates.
		dupKbs := []bayKeybinding{
			{key: "M-j", cmd: "select-pane -D", isTmuxCommand: true},
			{key: "M-J", cmd: "select-pane -D", isTmuxCommand: true},
			{key: "M-k", cmd: "select-pane -U", isTmuxCommand: true},
			{key: "M-K", cmd: "select-pane -U", isTmuxCommand: true},
		}
		block := `# Bay keybindings
bind-key -n M-J select-pane -D
bind-key -n M-K select-pane -U
`
		got := missingCanonicalLines(block, dupKbs)
		want := []string{
			"bind-key -n M-j select-pane -D",
			"bind-key -n M-k select-pane -U",
		}
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

func TestAppendToBayBlock(t *testing.T) {
	content := `set -g mouse on

# Bay keybindings
bind-key -n M-h previous-window
bind-key -n M-l next-window

# another thing
set -g base-index 1
`
	got := appendToBayBlock(content, []string{
		"bind-key -n M-j select-pane -D",
		"# bind-key -n M-k select-pane -U",
	})
	want := `set -g mouse on

# Bay keybindings
bind-key -n M-h previous-window
bind-key -n M-l next-window
bind-key -n M-j select-pane -D
# bind-key -n M-k select-pane -U

# another thing
set -g base-index 1
`
	if got != want {
		t.Errorf("appendToBayBlock:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestAppendToBayBlock_NoBlock(t *testing.T) {
	// No bay block present — content must be returned unchanged.
	content := "set -g mouse on\n"
	if got := appendToBayBlock(content, []string{"x"}); got != content {
		t.Errorf("expected no change, got %q", got)
	}
}

func TestCommentedCommandsInBlock(t *testing.T) {
	block := `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
# bind-key -n M-k run-shell 'bay surface prev'
#bind-key -n M-h previous-window
bind-key -n M-l next-window
`
	got := commentedCommandsInBlock(block)
	want := map[string]bool{
		"bay surface prev": true,
		"previous-window":  true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commentedCommandsInBlock() = %v, want %v", got, want)
	}
}

func TestMismatchedBindings(t *testing.T) {
	kbs := []bayKeybinding{
		{key: "M-s", cmd: "bay shell --pane", tmuxVerb: "run-shell"},
		{key: "M-S", cmd: "bay shell --window", tmuxVerb: "run-shell"},
		{key: "M-e", cmd: "bay edit --ws", tmuxVerb: "run-shell"},
		{key: "M-E", cmd: "bay edit --dock", tmuxVerb: "run-shell"},
	}

	t.Run("swap-case drift is flagged", func(t *testing.T) {
		// User has the old canonical: M-s=window, M-S=pane. Canonical
		// has since flipped. Both keys are bound, so the "missing" path
		// sees nothing — mismatch is what catches this.
		block := `# Bay keybindings
bind-key -n M-s run-shell 'bay shell --window || true'
bind-key -n M-S run-shell 'bay shell --pane || true'
bind-key -n M-e run-shell 'bay edit --ws || true'
bind-key -n M-E run-shell 'bay edit --dock || true'
`
		got := mismatchedBindings(block, kbs)
		wantKeys := []string{"M-s", "M-S"}
		if len(got) != len(wantKeys) {
			t.Fatalf("mismatchedBindings() returned %d; want %d: %+v", len(got), len(wantKeys), got)
		}
		for i, k := range wantKeys {
			if got[i].canonical.key != k {
				t.Errorf("mismatchedBindings()[%d].key = %q; want %q", i, got[i].canonical.key, k)
			}
		}
	})

	t.Run("canonical bindings report no mismatch", func(t *testing.T) {
		block := `# Bay keybindings
bind-key -n M-s run-shell 'bay shell --pane || true'
bind-key -n M-S run-shell 'bay shell --window || true'
`
		if got := mismatchedBindings(block, kbs); len(got) != 0 {
			t.Errorf("mismatchedBindings() = %+v; want empty", got)
		}
	})

	t.Run("non-bay rebinds are not mismatches", func(t *testing.T) {
		// User rebound M-s to something unrelated. Not drift we own —
		// skip silently.
		block := `# Bay keybindings
bind-key -n M-s run-shell 'my-custom-script || true'
`
		if got := mismatchedBindings(block, kbs); len(got) != 0 {
			t.Errorf("mismatchedBindings() = %+v; want empty", got)
		}
	})

	t.Run("bay-keep marker suppresses mismatch", func(t *testing.T) {
		block := `# Bay keybindings
# bay-keep: M-s
bind-key -n M-s run-shell 'bay shell --window || true'
bind-key -n M-S run-shell 'bay shell --pane || true'
`
		got := mismatchedBindings(block, kbs)
		if len(got) != 1 || got[0].canonical.key != "M-S" {
			t.Errorf("mismatchedBindings() = %+v; want only M-S mismatch", got)
		}
	})

	t.Run("missing key is not flagged as mismatch", func(t *testing.T) {
		// M-s not bound at all → missingBindings handles it; mismatch
		// only fires when the key is actively bound to the wrong thing.
		block := `# Bay keybindings
bind-key -n M-S run-shell 'bay shell --window || true'
`
		if got := mismatchedBindings(block, kbs); len(got) != 0 {
			t.Errorf("mismatchedBindings() = %+v; want empty (M-s unbound is missing, not mismatch)", got)
		}
	})

	t.Run("bay-keep with multiple keys on one line", func(t *testing.T) {
		block := `# Bay keybindings
# bay-keep: M-s M-e
bind-key -n M-s run-shell 'bay shell --window || true'
bind-key -n M-e run-shell 'bay edit --dock || true'
`
		if got := mismatchedBindings(block, kbs); len(got) != 0 {
			t.Errorf("mismatchedBindings() = %+v; want empty (both pinned)", got)
		}
	})
}

func TestReplaceBindingInBlock(t *testing.T) {
	content := `set -g mouse on

# Bay keybindings
bind-key -n M-s run-shell 'bay shell --window || true'
bind-key -n M-S run-shell 'bay shell --pane || true'

set -g base-index 1
`
	kb := bayKeybinding{key: "M-s", cmd: "bay shell --pane", tmuxVerb: "run-shell"}
	got := replaceBindingInBlock(content, kb)
	want := `set -g mouse on

# Bay keybindings
bind-key -n M-s run-shell 'bay shell --pane || true'
bind-key -n M-S run-shell 'bay shell --pane || true'

set -g base-index 1
`
	if got != want {
		t.Errorf("replaceBindingInBlock:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestReplaceBindingInBlock_NoBlock(t *testing.T) {
	content := "set -g mouse on\n"
	kb := bayKeybinding{key: "M-s", cmd: "bay shell --pane", tmuxVerb: "run-shell"}
	if got := replaceBindingInBlock(content, kb); got != content {
		t.Errorf("expected no change, got %q", got)
	}
}

func TestKeptKeys(t *testing.T) {
	block := `# Bay keybindings
# bay-keep: M-s M-e
# bay-keep:  M-a
bind-key -n M-s run-shell 'bay shell --window || true'
`
	got := keptKeys(block)
	want := map[string]bool{"M-s": true, "M-e": true, "M-a": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keptKeys() = %v, want %v", got, want)
	}
}
