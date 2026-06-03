package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestHasUserStatusRight(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "empty config", content: "", want: false},
		{name: "set -g status-right", content: "set -g status-right '#H'\n", want: true},
		{name: "set-option -g status-right", content: "set-option -g status-right '#H'\n", want: true},
		{name: "set without -g", content: "set status-right '#H'\n", want: true},
		{name: "status-right-length only is not status-right", content: "set -g status-right-length 40\n", want: false},
		{name: "status-right-style is not status-right", content: "set -g status-right-style 'bg=blue'\n", want: false},
		{name: "commented status-right is not status-right", content: "# set -g status-right '#H'\n", want: false},
		{name: "indented commented is still skipped", content: "    # set -g status-right '#H'\n", want: false},
		{name: "status-left is not status-right", content: "set -g status-left '#S'\n", want: false},
		{name: "unrelated set", content: "set -g mouse on\n", want: false},
		{name: "mixed config with non-bay status-right", content: "# Some comment\nset -g mouse on\nset -g status-right '#(date)'\n", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasUserStatusRight(tt.content); got != tt.want {
				t.Errorf("hasUserStatusRight(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestInstallStatusRight_FreshInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	confPath := filepath.Join(home, ".tmux.conf")
	// Pre-create empty file so the installer's read returns ""
	_ = os.WriteFile(confPath, []byte("set -g mouse on\n"), 0o644)

	reader := bufio.NewReader(strings.NewReader("\n")) // accept default [Y/n]
	installStatusRight(reader)

	got, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("read tmux.conf: %v", err)
	}
	for _, want := range bayStatusLineBlock {
		if !strings.Contains(string(got), want) {
			t.Errorf("after install, %s missing %q\nfull contents:\n%s", confPath, want, got)
		}
	}
}

func TestInstallStatusRight_AlreadyMarkedSkipsBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	confPath := filepath.Join(home, ".tmux.conf")
	original := "# Bay status line\nset -g status-right 'something custom'\n"
	_ = os.WriteFile(confPath, []byte(original), 0o644)

	reader := bufio.NewReader(strings.NewReader("\n"))
	installStatusRight(reader)

	got, _ := os.ReadFile(confPath)
	if string(got) != original {
		t.Errorf("file changed after no-op install:\nbefore: %q\nafter:  %q", original, got)
	}
}

func TestInstallStatusRight_RespectsUserStatusRight(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	confPath := filepath.Join(home, ".tmux.conf")
	original := "set -g status-right '#(date)'\n"
	_ = os.WriteFile(confPath, []byte(original), 0o644)

	reader := bufio.NewReader(strings.NewReader("\n"))
	installStatusRight(reader)

	got, _ := os.ReadFile(confPath)
	if string(got) != original {
		t.Errorf("user's status-right should not be modified:\nbefore: %q\nafter:  %q", original, got)
	}
}

func TestInstallStatusRight_NoConfigFileCreatesOne(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	confPath := filepath.Join(home, ".tmux.conf")
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Fatalf("expected no tmux.conf at start; got err=%v", err)
	}

	reader := bufio.NewReader(strings.NewReader("\n"))
	installStatusRight(reader)

	got, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("expected installer to create tmux.conf: %v", err)
	}
	if !strings.Contains(string(got), bayStatusLineMarker) {
		t.Errorf("created tmux.conf missing bay marker:\n%s", got)
	}
}

func TestInstallStatusRight_DeclineLeavesFileUntouched(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	confPath := filepath.Join(home, ".tmux.conf")
	original := "set -g mouse on\n"
	_ = os.WriteFile(confPath, []byte(original), 0o644)

	reader := bufio.NewReader(strings.NewReader("n\n"))
	installStatusRight(reader)

	got, _ := os.ReadFile(confPath)
	if string(got) != original {
		t.Errorf("declining the prompt should leave the file unchanged:\nbefore: %q\nafter:  %q", original, got)
	}
}

func TestHasMarkerLine_IgnoresMarkerInCommentText(t *testing.T) {
	// strings.Contains would have matched this — the line-aware check
	// must not, since the marker appears inside a longer comment.
	content := "# I tried the # Bay status line block once but reverted.\nset -g mouse on\n"
	if hasMarkerLine(content, bayStatusLineMarker) {
		t.Error("hasMarkerLine should require the marker on its own line")
	}
	// Same string on its own line (with surrounding whitespace) does match.
	content2 := "set -g mouse on\n   # Bay status line   \nset -g status-right 'x'\n"
	if !hasMarkerLine(content2, bayStatusLineMarker) {
		t.Error("hasMarkerLine should match a marker on its own (trimmed) line")
	}
}

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
bind-key -n M-g run-shell 'bay go --pick || true'
`
	got := commandsInBlock(block)
	want := map[string]bool{
		"previous-window":       true,
		"next-window":           true,
		"select-pane -L":        true,
		"select-pane -D":        true,
		"bay go --pick || true": true,
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
		{key: "M-e", cmd: "bay edit", tmuxVerb: "run-shell"},
		{key: "M-E", cmd: "bay edit --dock", tmuxVerb: "run-shell"},
	}

	t.Run("swap-case drift is flagged", func(t *testing.T) {
		// User has the old canonical: M-s=window, M-S=pane. Canonical
		// has since flipped. Both keys are bound, so the "missing" path
		// sees nothing — mismatch is what catches this.
		block := `# Bay keybindings
bind-key -n M-s run-shell 'bay shell --window || true'
bind-key -n M-S run-shell 'bay shell --pane || true'
bind-key -n M-e run-shell 'bay edit || true'
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
bind-key -n M-e run-shell 'bay edit || true'
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

	t.Run("custom palette window binding is not previous default drift", func(t *testing.T) {
		paletteKbs := []bayKeybinding{
			{key: "M-p", cmd: "bay palette --split pane", tmuxVerb: "display-popup -w 80% -h 80% -E"},
		}
		block := `# Bay keybindings
bind-key -n M-p display-popup -w 80% -h 80% -E 'bay palette --split window || true'
`
		if got := mismatchedBindings(block, paletteKbs); len(got) != 0 {
			t.Fatalf("mismatchedBindings() = %+v; want empty for custom palette binding", got)
		}
	})

	t.Run("previous gemini agent bindings drift to antigravity", func(t *testing.T) {
		agentKbs := []bayKeybinding{
			agentInBay("g", "antigravity", "pane"),
			newBayWithAgent("g", "antigravity"),
			homeAgent("g", "antigravity"),
		}
		block := `# Bay keybindings
bind-key -T bay-agent g run-shell 'bay agent gemini --pane || true'
bind-key -T bay-agent-bay g run-shell 'bay new -q --agent=gemini || true'
bind-key -T bay-home g run-shell 'bay agent gemini --bay home || true'
`
		got := mismatchedBindings(block, agentKbs)
		wantIDs := []string{"bay-agent:g", "bay-agent-bay:g", "bay-home:g"}
		if len(got) != len(wantIDs) {
			t.Fatalf("mismatchedBindings() = %+v; want %v", got, wantIDs)
		}
		for i, want := range wantIDs {
			if got[i].canonical.id() != want {
				t.Errorf("mismatchedBindings()[%d] = %s; want %s", i, got[i].canonical.id(), want)
			}
		}
	})

	t.Run("previousCmds entry catches isTmuxCommand drift", func(t *testing.T) {
		// Regression: when a tmux-command binding's display-message hint
		// changes (e.g. M-o gaining `| h=home` in home-bay phase 6), the
		// user's old line doesn't match any other canonical, so the
		// fallback path can't catch it. previousCmds is the only signal.
		// parseBindLine extracts the quoted hint region, so the entry
		// must be the old hint text, not the full bind line.
		newCmd := `display-message -d 2000 "new hint" \; switch-client -T bay-agent`
		kbs := []bayKeybinding{{
			key:           "M-o",
			cmd:           newCmd,
			previousCmds:  []string{"old hint"},
			isTmuxCommand: true,
		}}
		block := "# Bay keybindings\n" +
			`bind-key -n M-o display-message -d 2000 "old hint" \; switch-client -T bay-agent` + "\n"
		got := mismatchedBindings(block, kbs)
		if len(got) != 1 || got[0].canonical.key != "M-o" {
			t.Fatalf("mismatchedBindings() = %+v; want single M-o mismatch", got)
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

func TestHomeChordKeybindings(t *testing.T) {
	lines := tmuxKeybindingLines()
	joined := strings.Join(lines, "\n")

	for _, want := range []string{
		`bind-key -n M-o display-message -d 2000 "agent: c Claude, x Codex, g Antigravity | Shift=window | b=bay | h=home" \; switch-client -T bay-agent`,
		`bind-key -T bay-agent h display-message -d 2000 "home: Enter home, s shell, e editor, c Claude, x Codex, g Antigravity" \; switch-client -T bay-home`,
		`bind-key -T bay-home Enter run-shell 'bay home || true'`,
		`bind-key -T bay-home s run-shell 'bay shell --bay home || true'`,
		`bind-key -T bay-home e run-shell 'bay edit --bay home || true'`,
		`bind-key -T bay-home c run-shell 'bay agent claude --bay home || true'`,
		`bind-key -T bay-home x run-shell 'bay agent codex --bay home || true'`,
		`bind-key -T bay-home g run-shell 'bay agent antigravity --bay home || true'`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("home chord binding missing %q\nall bindings:\n%s", want, joined)
		}
	}

	prefix := findKeybinding(t, tableAgent, "h")
	if !prefix.isTmuxCommand || strings.Contains(prefix.cmd, "bay home") {
		t.Fatalf("M-o h must be prefix-only, got %+v", prefix)
	}
	if !strings.Contains(prefix.cmd, "switch-client -T "+tableHome) {
		t.Fatalf("M-o h prefix cmd = %q; want switch-client into %s", prefix.cmd, tableHome)
	}
}

func TestMissingBindings_DetectsMissingHomeEnterBinding(t *testing.T) {
	var blockLines []string
	for _, kb := range bayKeybindings {
		if kb.table == tableHome && kb.key == "Enter" {
			continue
		}
		blockLines = append(blockLines, kb.canonicalLine())
	}
	block := bayKeybindingsMarker + "\n" + strings.Join(blockLines, "\n")

	missing := missingBindings(block, bayKeybindings)
	if len(missing) != 1 || missing[0].table != tableHome || missing[0].key != "Enter" {
		t.Fatalf("missingBindings = %+v; want only %s:Enter", missing, tableHome)
	}
}

// parseBindLine has to recognize key-table bindings (`-T <table>`) so
// chord sub-tables (M-o c → bay-agent c) participate in drift
// detection alongside root bindings.
func TestParseBindLine_KeyTable(t *testing.T) {
	cases := []struct {
		line  string
		table string
		key   string
		cmd   string
	}{
		{
			line:  `bind-key -T bay-agent c run-shell 'bay agent claude --pane || true'`,
			table: "bay-agent",
			key:   "c",
			cmd:   "bay agent claude --pane || true",
		},
		{
			line:  `bind-key -T bay-agent-bay g run-shell 'bay new -q --agent=antigravity || true'`,
			table: "bay-agent-bay",
			key:   "g",
			cmd:   "bay new -q --agent=antigravity || true",
		},
		{
			line:  `bind-key -T bay-home Enter run-shell 'bay home || true'`,
			table: "bay-home",
			key:   "Enter",
			cmd:   "bay home || true",
		},
		{
			line:  `bind-key -T bay-agent b display-message -d 2000 "bay: c Claude, x Codex, g Antigravity" \; switch-client -T bay-agent-bay`,
			table: "bay-agent",
			key:   "b",
			cmd:   "bay: c Claude, x Codex, g Antigravity", // quoted region only — matches existing tmux-cmd extraction behavior
		},
		{
			line:  `bind-key -T bay-agent h display-message -d 2000 "home: Enter home, s shell, e editor, c Claude, x Codex, g Antigravity" \; switch-client -T bay-home`,
			table: "bay-agent",
			key:   "h",
			cmd:   "home: Enter home, s shell, e editor, c Claude, x Codex, g Antigravity", // quoted region only — matches existing tmux-cmd extraction behavior
		},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			gotTable, gotKey, gotCmd, ok := parseBindLine(tc.line)
			if !ok {
				t.Fatalf("parseBindLine returned ok=false")
			}
			if gotTable != tc.table || gotKey != tc.key || gotCmd != tc.cmd {
				t.Errorf("parseBindLine = (%q, %q, %q), want (%q, %q, %q)",
					gotTable, gotKey, gotCmd, tc.table, tc.key, tc.cmd)
			}
		})
	}
}

func TestWriteAntigravityHook(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := writeAntigravityHook(path, antigravityReadyCommand); err != nil {
		t.Fatalf("writeAntigravityHook: %v", err)
	}
	if !antigravityHookConfigured(path) {
		t.Fatal("antigravity hook should be configured")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks = %#v, want object", settings["hooks"])
	}
	bayHook, ok := hooks[antigravityBayHookName].(map[string]any)
	if !ok {
		t.Fatalf("%s hook = %#v, want object", antigravityBayHookName, hooks[antigravityBayHookName])
	}
	stop, ok := bayHook["Stop"].([]any)
	if !ok || len(stop) != 1 {
		t.Fatalf("Stop = %#v, want one handler", bayHook["Stop"])
	}
	handler, ok := stop[0].(map[string]any)
	if !ok {
		t.Fatalf("Stop handler = %#v, want object", stop[0])
	}
	if handler["type"] != "command" || handler["command"] != antigravityReadyCommand {
		t.Errorf("handler = %#v, want bay command handler", handler)
	}
}

// canonicalLine and parseBindLine should round-trip for a chord
// sub-table binding: emit, parse, recover the same identity.
func TestChordBindingRoundTrip(t *testing.T) {
	kb := bayKeybinding{
		table:    "bay-agent-bay",
		key:      "c",
		cmd:      "bay new -q --agent=claude",
		tmuxVerb: "run-shell",
	}
	line := kb.canonicalLine()
	t.Logf("canonical line: %s", line)
	gotTable, gotKey, _, ok := parseBindLine(line)
	if !ok {
		t.Fatalf("parseBindLine(%q) failed", line)
	}
	if got := bindID(gotTable, gotKey); got != kb.id() {
		t.Errorf("round-trip id = %q, want %q", got, kb.id())
	}
}

// activeBindings keys binding identities by table+key, so a chord-
// table letter doesn't collide with the same letter at the root.
// (`c` in bay-agent must be distinct from `c` if it ever appeared
// in the root table.)
func TestActiveBindings_TableKeyDistinctFromRootKey(t *testing.T) {
	block := `# Bay keybindings
bind-key -n M-c run-shell 'bay new -q || true'
bind-key -T bay-agent c run-shell 'bay agent claude --pane || true'
bind-key -T bay-agent-bay c run-shell 'bay new -q --agent=claude || true'
bind-key -T bay-home c run-shell 'bay agent claude --bay home || true'
`
	got := activeBindings(block)
	if got["M-c"] != "bay new -q || true" {
		t.Errorf("M-c missing or wrong: %q", got["M-c"])
	}
	if got["bay-agent:c"] != "bay agent claude --pane || true" {
		t.Errorf("bay-agent:c missing or wrong: %q", got["bay-agent:c"])
	}
	if got["bay-agent-bay:c"] != "bay new -q --agent=claude || true" {
		t.Errorf("bay-agent-bay:c missing or wrong: %q", got["bay-agent-bay:c"])
	}
	if got["bay-home:c"] != "bay agent claude --bay home || true" {
		t.Errorf("bay-home:c missing or wrong: %q", got["bay-home:c"])
	}
}

func findKeybinding(t *testing.T, table, key string) bayKeybinding {
	t.Helper()
	for _, kb := range bayKeybindings {
		if kb.table == table && kb.key == key {
			return kb
		}
	}
	t.Fatalf("keybinding %s not found", bindID(table, key))
	return bayKeybinding{}
}
