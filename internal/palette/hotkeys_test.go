package palette

import "testing"

func TestParseHotkeys_CurrentSyntax(t *testing.T) {
	out := `bind-key -T root M-p display-popup -w 80% -h 80% -E 'bay palette --split pane || true'
bind-key -T root M-s run-shell 'bay shell --window || true'
bind-key -T root M-S run-shell 'bay shell --pane || true'
bind-key -T root C-b send-prefix
`
	h := parseHotkeys(out)
	cases := []struct {
		sig  string
		want string
	}{
		{"bay palette --split pane", "M-p"},
		{"bay shell --window", "M-s"},
		{"bay shell --pane", "M-S"},
	}
	for _, c := range cases {
		if got := h.Lookup(c.sig); got != c.want {
			t.Errorf("Lookup(%q) = %q; want %q", c.sig, got, c.want)
		}
	}
	if got := h.Lookup("bay missing"); got != "" {
		t.Errorf("Lookup missing = %q; want empty", got)
	}
}

func TestParseHotkeys_LegacySyntax(t *testing.T) {
	out := `bind-key -n M-a run-shell 'bay agent --window || true'`
	h := parseHotkeys(out)
	if got := h.Lookup("bay agent --window"); got != "M-a" {
		t.Errorf("legacy -n syntax not parsed: got %q, want M-a", got)
	}
}

func TestParseHotkeys_IgnoresNonBay(t *testing.T) {
	out := `bind-key -T root M-z run-shell 'echo not bay'
bind-key -T root M-y send-keys 'hello'
`
	h := parseHotkeys(out)
	if len(h.byCmd) != 0 {
		t.Errorf("parsed non-bay commands: %v", h.byCmd)
	}
}

func TestParseHotkeys_StripsOrTrueOnly(t *testing.T) {
	out := `bind-key -T root M-e run-shell 'bay edit --ws || true'
bind-key -T root M-x run-shell 'bay edit --dock'
`
	h := parseHotkeys(out)
	if got := h.Lookup("bay edit --ws"); got != "M-e" {
		t.Errorf("|| true not stripped: got %q", got)
	}
	if got := h.Lookup("bay edit --dock"); got != "M-x" {
		t.Errorf("missing || true should still match: got %q", got)
	}
}

func TestParseHotkeys_ExactMatchNoPrefix(t *testing.T) {
	// A user rebinds M-a to invoke a specific agent. We do NOT want that to
	// annotate the "bay agent --window" entry — exact match only.
	out := `bind-key -T root M-a run-shell 'bay agent codex --window || true'`
	h := parseHotkeys(out)
	if got := h.Lookup("bay agent --window"); got != "" {
		t.Errorf("prefix match leaked: %q", got)
	}
	if got := h.Lookup("bay agent codex --window"); got != "M-a" {
		t.Errorf("exact match failed: %q", got)
	}
}

func TestParseHotkeys_EmptyInput(t *testing.T) {
	h := parseHotkeys("")
	if got := h.Lookup("bay palette"); got != "" {
		t.Errorf("empty input yielded binding %q", got)
	}
}
