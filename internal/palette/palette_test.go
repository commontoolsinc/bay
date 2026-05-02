package palette

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func feedKeys(t *testing.T, keys string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	go func() {
		defer w.Close()
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte(keys))
	}()
	return r
}

func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("dev null: %v", err)
	}
	return f
}

// mkEntry is a small builder for tests.
func mkEntry(id, title string, sec Section, needs Scope, hotkey string, action func() (string, error)) Entry {
	return Entry{
		ID:      id,
		Title:   title,
		Section: sec,
		Needs:   needs,
		Hotkey:  hotkey,
		Action:  action,
	}
}

func TestRun_EscapeClosesWithoutAction(t *testing.T) {
	called := false
	entries := []Entry{
		mkEntry("go-ws", "Go to bay", SectionNavigation, ScopeInDock, "M-G",
			func() (string, error) { called = true; return "", nil }),
	}
	in := feedKeys(t, "\x1b")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	err := Run(RunOptions{
		Scope:   ScopeInBay,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		In:      in,
		Out:     out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if called {
		t.Error("Action fired on Esc")
	}
}

func TestRun_EnterRunsSelectedEntry(t *testing.T) {
	calls := []string{}
	entries := []Entry{
		mkEntry("one", "Entry one", SectionNavigation, ScopeAnywhere, "",
			func() (string, error) { calls = append(calls, "one"); return "", nil }),
		mkEntry("two", "Entry two", SectionNavigation, ScopeAnywhere, "",
			func() (string, error) { calls = append(calls, "two"); return "", nil }),
	}
	// Down arrow to select "two", Enter.
	in := feedKeys(t, "\x1b[B\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	err := Run(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		In:      in,
		Out:     out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 1 || calls[0] != "two" {
		t.Errorf("calls = %v; want [two]", calls)
	}
}

func TestRun_FilterCollapsesToFlatList(t *testing.T) {
	calls := []string{}
	entries := []Entry{
		mkEntry("go-sf", "Go to surface", SectionNavigation, ScopeAnywhere, "",
			func() (string, error) { calls = append(calls, "go-sf"); return "", nil }),
		mkEntry("shell", "New shell", SectionCreateSurface, ScopeAnywhere, "",
			func() (string, error) { calls = append(calls, "shell"); return "", nil }),
		mkEntry("agent", "New agent", SectionCreateSurface, ScopeAnywhere, "",
			func() (string, error) { calls = append(calls, "agent"); return "", nil }),
	}
	// Type "shel" then Enter → matches only "New shell".
	in := feedKeys(t, "shel\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	err := Run(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		In:      in,
		Out:     out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 1 || calls[0] != "shell" {
		t.Errorf("calls = %v; want [shell]", calls)
	}
}

func TestRun_HidesEntriesBelowScope(t *testing.T) {
	calls := []string{}
	entries := []Entry{
		mkEntry("edit-config", "Edit config", SectionAdmin, ScopeAnywhere, "",
			func() (string, error) { calls = append(calls, "edit-config"); return "", nil }),
		mkEntry("go-sf", "Go to surface", SectionNavigation, ScopeInBay, "",
			func() (string, error) { calls = append(calls, "go-sf"); return "", nil }),
	}
	// Just Enter — should land on the only visible entry (edit-config)
	// because "go-sf" is scope-hidden at ScopeAnywhere.
	in := feedKeys(t, "\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	err := Run(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		In:      in,
		Out:     out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 1 || calls[0] != "edit-config" {
		t.Errorf("calls = %v; want [edit-config]", calls)
	}
}

func TestRun_TabFlipsMode(t *testing.T) {
	// Entries function returns different titles based on mode; we assert
	// Tab reaches the ModePane variant.
	calls := []string{}
	build := func(m Mode) []Entry {
		label := "window-mode"
		if m == ModePane {
			label = "pane-mode"
		}
		return []Entry{
			mkEntry("one", label, SectionNavigation, ScopeAnywhere, "",
				func() (string, error) { calls = append(calls, label); return "", nil }),
		}
	}
	// Tab to flip, then Enter.
	in := feedKeys(t, "\t\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	err := Run(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: build,
		In:      in,
		Out:     out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 1 || calls[0] != "pane-mode" {
		t.Errorf("calls = %v; want [pane-mode]", calls)
	}
}

func TestRun_RecordsSuccessToRecents(t *testing.T) {
	dir := t.TempDir()
	rec := LoadRecents(filepath.Join(dir, "recents.json"))

	entries := []Entry{
		mkEntry("go-ws", "Go to bay", SectionNavigation, ScopeAnywhere, "",
			func() (string, error) { return "", nil }),
	}
	in := feedKeys(t, "\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	if err := Run(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		Recents: rec,
		In:      in,
		Out:     out,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.Recent) != 1 || rec.Recent[0].ID != "go-ws" {
		t.Errorf("recents = %+v; want one go-ws entry", rec.Recent)
	}
}

func TestRun_BindsReturnedParamInRecents(t *testing.T) {
	dir := t.TempDir()
	rec := LoadRecents(filepath.Join(dir, "recents.json"))

	entries := []Entry{
		mkEntry("new-agent-pick", "New agent...", SectionCreateSurface, ScopeAnywhere, "",
			func() (string, error) { return "codex", nil }),
	}
	in := feedKeys(t, "\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	if err := Run(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		Recents: rec,
		In:      in,
		Out:     out,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.Recent) != 1 {
		t.Fatalf("recents len=%d; want 1", len(rec.Recent))
	}
	if rec.Recent[0].Param != "codex" {
		t.Errorf("recents[0].Param=%q; want codex", rec.Recent[0].Param)
	}
}

func TestRun_BoundRecentInvokesActionWithParam(t *testing.T) {
	dir := t.TempDir()
	rec := LoadRecents(filepath.Join(dir, "recents.json"))
	rec.Record("new-agent-pick", "codex")

	calls := []string{}
	entries := []Entry{
		{
			ID:      "new-agent-pick",
			Title:   "New agent...",
			Section: SectionCreateSurface,
			Needs:   ScopeAnywhere,
			Action: func() (string, error) {
				calls = append(calls, "prompt")
				return "", nil
			},
			ActionWithParam: func(param string) (string, error) {
				calls = append(calls, "bound:"+param)
				return param, nil
			},
			TitleWithParam: func(param string) string {
				return "New agent (" + param + ")"
			},
		},
	}
	in := feedKeys(t, "\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	if err := Run(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		Recents: rec,
		In:      in,
		Out:     out,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 1 || calls[0] != "bound:codex" {
		t.Fatalf("calls=%v; want [bound:codex]", calls)
	}
	if rec.Recent[0].ID != "new-agent-pick" || rec.Recent[0].Param != "codex" {
		t.Fatalf("latest recent=(%q,%q); want (new-agent-pick,codex)", rec.Recent[0].ID, rec.Recent[0].Param)
	}
}

func TestRun_RendersDistinctBoundRecentsForSameEntry(t *testing.T) {
	rec := &Recents{}
	rec.recordAt("new-agent-pick", "claude", 1)
	rec.recordAt("new-agent-pick", "codex", 2)

	entries := []Entry{
		{
			ID:      "new-agent-pick",
			Title:   "New agent...",
			Section: SectionCreateSurface,
			Needs:   ScopeAnywhere,
			Action:  func() (string, error) { return "", nil },
			TitleWithParam: func(param string) string {
				return "New agent (" + param + ")"
			},
		},
	}
	state := newRunState(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		Recents: rec,
	})
	_, _ = state.render(80)

	got := []string{}
	for _, row := range state.rendered {
		if row.entryIdx >= 0 && row.param != "" {
			got = append(got, row.param)
		}
	}
	want := []string{"codex", "claude"}
	if len(got) != len(want) {
		t.Fatalf("bound recent params=%v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bound recent params=%v; want %v", got, want)
		}
	}
}

func TestRun_FiltersStaleBoundRecents(t *testing.T) {
	rec := &Recents{}
	rec.Record("new-agent-pick", "retired")

	entries := []Entry{
		{
			ID:      "new-agent-pick",
			Title:   "New agent...",
			Section: SectionCreateSurface,
			Needs:   ScopeAnywhere,
			Action:  func() (string, error) { return "", nil },
			ParamValid: func(param string) bool {
				return param == "codex"
			},
			TitleWithParam: func(param string) string {
				return "New agent (" + param + ")"
			},
		},
	}
	state := newRunState(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		Recents: rec,
	})
	_, _ = state.render(80)

	for _, row := range state.rendered {
		if strings.Contains(row.text, "retired") || row.param == "retired" {
			t.Fatalf("rendered stale bound recent row: %+v", row)
		}
	}
}

func TestRun_RecentsEntryAlsoShownInItsSection(t *testing.T) {
	// When an entry is in the Recent section it must still appear under
	// its own category header. Otherwise promoting something to recents
	// makes it disappear from the section — the design calls this out
	// as "no bouncing around when an entry enters or leaves recents."
	dir := t.TempDir()
	rec := LoadRecents(dir + "/r.json")
	// Record once so "go-ws" appears in recents.
	rec.Record("go-ws", "")

	entries := []Entry{
		mkEntry("go-ws", "Go to bay", SectionNavigation, ScopeAnywhere, "",
			func() (string, error) { return "", nil }),
		mkEntry("other", "Other", SectionNavigation, ScopeAnywhere, "",
			func() (string, error) { return "", nil }),
	}

	state := newRunState(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		Recents: rec,
	})
	_, _ = state.render(80)

	// Count how many rendered rows resolve to the "go-ws" entry: one in
	// Recent, one in Navigation.
	count := 0
	for _, r := range state.rendered {
		if r.entryIdx < 0 {
			continue
		}
		if entries[r.entryIdx].ID == "go-ws" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("go-ws rendered %d times; want 2 (once in Recent, once in Navigation)", count)
	}
}

func TestRun_EntryRowsFillWidthEvenWithoutHotkey(t *testing.T) {
	// rightAlign must pad entry rows to the full width so the cursor
	// highlight covers the whole line for hotkey-less entries.
	entries := []Entry{
		mkEntry("bare", "Bare entry", SectionNavigation, ScopeAnywhere, "",
			func() (string, error) { return "", nil }),
		mkEntry("with-key", "With key", SectionNavigation, ScopeAnywhere, "M-x",
			func() (string, error) { return "", nil }),
	}

	state := newRunState(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
	})
	const w = 40
	rows, _ := state.render(w)

	for i, r := range rows {
		if state.rendered[i].entryIdx < 0 {
			continue
		}
		if len(r) < w {
			t.Errorf("row %d (%q) width=%d; want >= %d for highlight uniformity",
				i, r, len(r), w)
		}
	}
}

func TestRun_DoesNotRecordOnActionError(t *testing.T) {
	dir := t.TempDir()
	rec := LoadRecents(filepath.Join(dir, "recents.json"))

	entries := []Entry{
		mkEntry("fails", "Will fail", SectionAdmin, ScopeAnywhere, "",
			func() (string, error) { return "", errSentinel }),
	}
	in := feedKeys(t, "\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	err := Run(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		Recents: rec,
		In:      in,
		Out:     out,
	})
	if err != errSentinel {
		t.Fatalf("Run err=%v; want errSentinel", err)
	}
	if len(rec.Recent) != 0 {
		t.Errorf("recents touched on error: %+v", rec.Recent)
	}
}

var errSentinel = errSentinelType{}

type errSentinelType struct{}

func (errSentinelType) Error() string { return "sentinel" }

func TestFuzzyScore_RequiresAllCharsInOrder(t *testing.T) {
	cases := []struct {
		target, query string
		want          bool
	}{
		{"New shell", "ns", true},
		{"New shell", "sn", false},           // out of order
		{"New shell", "newshell", true},      // contiguous ignoring space
		{"New shell", "xyz", false},          // missing chars
		{"Rename workspace...", "rw", true},  // initials
		{"Rename workspace...", "rws", true}, // initials + within
		{"Edit config", "ec", true},
		{"Edit config", "ed c", true}, // spaces in query still match
	}
	for _, c := range cases {
		_, ok := fuzzyScore(c.target, c.query)
		if ok != c.want {
			t.Errorf("fuzzyScore(%q, %q) ok=%v; want %v", c.target, c.query, ok, c.want)
		}
	}
}

func TestFuzzyScore_AdjacencyBeatsBoundary(t *testing.T) {
	// "ns" matches both; adjacent in "ns-cmd" beats boundary match in "New shell".
	adj, ok1 := fuzzyScore("ns-cmd", "ns")
	bnd, ok2 := fuzzyScore("New shell", "ns")
	if !ok1 || !ok2 {
		t.Fatalf("both should match; got ok1=%v ok2=%v", ok1, ok2)
	}
	if adj <= bnd {
		t.Errorf("adjacent score %d should beat boundary score %d", adj, bnd)
	}
}

func TestFuzzyScore_BoundaryBeatsWithinWord(t *testing.T) {
	// Both contain the same letters but "sh" is at the start of a word in
	// "New shell" vs mid-word in "foo-shellish".
	bnd, _ := fuzzyScore("New shell", "sh")
	mid, _ := fuzzyScore("ashelb", "sh") // no word boundary before "sh"
	if bnd <= mid {
		t.Errorf("word-boundary %d should beat within-word %d", bnd, mid)
	}
}

func TestFuzzyScore_StartBonusBeatsBoundary(t *testing.T) {
	// "pw" → "pwd" (prefix) should beat "pw" → "Show pw".
	prefix, _ := fuzzyScore("pwd", "pw")
	later, _ := fuzzyScore("Show pw", "pw")
	if prefix <= later {
		t.Errorf("prefix %d should beat later-match %d", prefix, later)
	}
}

func TestFuzzyScore_CaseInsensitive(t *testing.T) {
	lower, _ := fuzzyScore("New shell", "ns")
	upper, _ := fuzzyScore("New shell", "NS")
	if lower != upper {
		t.Errorf("case matters: lower=%d upper=%d", lower, upper)
	}
}

func TestFuzzyScore_EmptyQueryMatchesEverything(t *testing.T) {
	score, ok := fuzzyScore("anything", "")
	if !ok || score != 0 {
		t.Errorf("empty query: score=%d ok=%v; want 0,true", score, ok)
	}
}

func TestFuzzyScore_PrefersWordBoundaryOverFirstMatch(t *testing.T) {
	// "nw" against "New worktree": greedy takes the 'w' in "New" and
	// misses the word-boundary 'w' in "worktree". DP must find the
	// better alignment.
	wt, ok1 := fuzzyScore("New worktree", "nw")
	sh, ok2 := fuzzyScore("New shell", "nw")
	if !ok1 || !ok2 {
		t.Fatalf("both should match; ok1=%v ok2=%v", ok1, ok2)
	}
	if wt <= sh {
		t.Errorf("New worktree (%d) should outrank New shell (%d) for 'nw'", wt, sh)
	}
}

func TestRun_FilterRanksFuzzyMatches(t *testing.T) {
	// With query "nc", "New cmd..." should outrank "Run doctor" (which
	// also contains n-...-c via the 'c' in 'doctor').
	calls := []string{}
	entries := []Entry{
		mkEntry("run-doctor", "Run doctor", SectionAdmin, ScopeAnywhere, "",
			func() (string, error) { calls = append(calls, "run-doctor"); return "", nil }),
		mkEntry("new-cmd", "New cmd...", SectionCreateSurface, ScopeAnywhere, "",
			func() (string, error) { calls = append(calls, "new-cmd"); return "", nil }),
	}
	in := feedKeys(t, "nc\r")
	out := devNull(t)
	defer in.Close()
	defer out.Close()

	err := Run(RunOptions{
		Scope:   ScopeAnywhere,
		Mode:    ModeWindow,
		Entries: func(Mode) []Entry { return entries },
		In:      in,
		Out:     out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(calls) != 1 || calls[0] != "new-cmd" {
		t.Errorf("calls = %v; want [new-cmd] (nc matches New cmd best)", calls)
	}
}
