package cli

import "testing"

func TestKeybindingsUpToDate(t *testing.T) {
	required := []string{
		"bind-key -n M-j run-shell 'bay surface next'",
		"bind-key -n M-k run-shell 'bay surface prev'",
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "all present as full lines",
			content: `# Bay keybindings
bind-key -n M-j run-shell 'bay surface next'
bind-key -n M-k run-shell 'bay surface prev'
`,
			want: true,
		},
		{
			name:    "all present with surrounding whitespace",
			content: "  bind-key -n M-j run-shell 'bay surface next'  \n\tbind-key -n M-k run-shell 'bay surface prev'\n",
			want:    true,
		},
		{
			name:    "empty file",
			content: "",
			want:    false,
		},
		{
			name: "one binding commented out",
			content: `# Bay keybindings
# bind-key -n M-j run-shell 'bay surface next'
bind-key -n M-k run-shell 'bay surface prev'
`,
			want: false,
		},
		{
			name: "all bindings commented out",
			content: `# bind-key -n M-j run-shell 'bay surface next'
# bind-key -n M-k run-shell 'bay surface prev'
`,
			want: false,
		},
		{
			name: "binding embedded as substring of a longer line",
			content: `set-hook -g some-event "bind-key -n M-j run-shell 'bay surface next'"
bind-key -n M-k run-shell 'bay surface prev'
`,
			want: false,
		},
		{
			name: "missing binding entirely",
			content: `bind-key -n M-j run-shell 'bay surface next'
`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := keybindingsUpToDate(tt.content, required)
			if got != tt.want {
				t.Errorf("keybindingsUpToDate() = %v, want %v", got, tt.want)
			}
		})
	}
}
