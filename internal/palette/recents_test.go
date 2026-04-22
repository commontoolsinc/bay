package palette

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecents_RecordCapsAtWindowSize(t *testing.T) {
	r := &Recents{}
	for i := range recentCap + 5 {
		r.recordAt("cmd", "", int64(i))
	}
	if got := len(r.Recent); got != recentCap {
		t.Fatalf("len=%d after %d records; want cap=%d (log, not dedupe)", got, recentCap+5, recentCap)
	}
	// Newest first: the last recorded TS should be at index 0.
	if r.Recent[0].TS != int64(recentCap+5-1) {
		t.Errorf("Recent[0].TS = %d; want newest = %d", r.Recent[0].TS, recentCap+5-1)
	}
}

func TestRecordAgentType_DedupesAndCaps(t *testing.T) {
	r := &Recents{}
	for i := range AgentTypesCap + 3 {
		r.recordAgentTypeAt("codex", int64(i))
	}
	if got := len(r.AgentTypes); got != 1 {
		t.Errorf("single agent recorded many times → len=%d; want 1 (MRU dedupes)", got)
	}

	r = &Recents{}
	for i := range AgentTypesCap + 3 {
		r.recordAgentTypeAt(string(rune('a'+i)), int64(i))
	}
	if got := len(r.AgentTypes); got != AgentTypesCap {
		t.Errorf("distinct agents × %d → len=%d; want cap=%d", AgentTypesCap+3, got, AgentTypesCap)
	}
}

func TestRecents_TopMRUDedupesFromBody(t *testing.T) {
	r := &Recents{}
	// Three uses of "go-ws", one of "edit-config", one more "go-ws".
	r.recordAt("go-ws", "", 1)
	r.recordAt("go-ws", "", 2)
	r.recordAt("edit-config", "", 3)
	r.recordAt("go-ws", "", 4)

	top := r.TopRecents(nil)
	if len(top) != 2 {
		t.Fatalf("top length=%d; want 2 (go-ws as MRU, edit-config in body)", len(top))
	}
	if top[0].ID != "go-ws" {
		t.Errorf("slot 1 = %q; want go-ws (MRU)", top[0].ID)
	}
	if top[1].ID != "edit-config" {
		t.Errorf("slot 2 = %q; want edit-config", top[1].ID)
	}
}

func TestRecents_TopFrequencyRankingAndTieBreak(t *testing.T) {
	r := &Recents{}
	// MRU: "last". Body: three entries — "a" (3x), "b" (2x), "c" (1x).
	// Frequencies decide order; ties broken by recency.
	r.recordAt("a", "", 1)
	r.recordAt("b", "", 2)
	r.recordAt("a", "", 3)
	r.recordAt("c", "", 4)
	r.recordAt("a", "", 5)
	r.recordAt("b", "", 6)
	r.recordAt("last", "", 7)

	top := r.TopRecents(nil)
	wantOrder := []string{"last", "a", "b", "c"}
	if len(top) != len(wantOrder) {
		t.Fatalf("top length=%d; want %d", len(top), len(wantOrder))
	}
	for i, w := range wantOrder {
		if top[i].ID != w {
			t.Errorf("slot %d = %q; want %q", i, top[i].ID, w)
		}
	}
}

func TestRecents_TopBindsParam(t *testing.T) {
	r := &Recents{}
	// Recents for a sub-picker parametric: (new-agent-pick, "codex") x3, (new-agent-pick, "claude") x1.
	r.recordAt("new-agent-pick", "codex", 1)
	r.recordAt("new-agent-pick", "codex", 2)
	r.recordAt("new-agent-pick", "claude", 3)
	r.recordAt("new-agent-pick", "codex", 4)

	top := r.TopRecents(nil)
	if len(top) != 2 {
		t.Fatalf("top length=%d; want 2", len(top))
	}
	if top[0].ID != "new-agent-pick" || top[0].Param != "codex" {
		t.Errorf("slot 0 = (%q,%q); want (new-agent-pick,codex)", top[0].ID, top[0].Param)
	}
	if top[1].ID != "new-agent-pick" || top[1].Param != "claude" {
		t.Errorf("slot 1 = (%q,%q); want (new-agent-pick,claude)", top[1].ID, top[1].Param)
	}
}

func TestRecents_TopFiltersStale(t *testing.T) {
	r := &Recents{}
	r.recordAt("gone", "", 1)
	r.recordAt("present", "", 2)

	valid := func(id, _ string) bool { return id == "present" }
	top := r.TopRecents(valid)
	if len(top) != 1 || top[0].ID != "present" {
		t.Fatalf("top=%+v; want one entry for 'present'", top)
	}
}

func TestRecents_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "palette-recents.json")

	orig := &Recents{path: path}
	orig.recordAt("go-ws", "", 1)
	orig.recordAt("new-agent-pick", "codex", 2)
	orig.recordAgentTypeAt("codex", 2)
	orig.recordAgentTypeAt("claude", 3)
	if err := orig.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded := LoadRecents(path)
	if len(loaded.Recent) != 2 {
		t.Errorf("len(Recent)=%d; want 2", len(loaded.Recent))
	}
	if len(loaded.AgentTypes) != 2 {
		t.Errorf("len(AgentTypes)=%d; want 2", len(loaded.AgentTypes))
	}
	if loaded.TopAgentTypes(nil)[0] != "claude" {
		t.Errorf("agent MRU[0]=%q; want claude", loaded.TopAgentTypes(nil)[0])
	}
}

func TestRecents_LoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	r := LoadRecents(filepath.Join(dir, "does-not-exist.json"))
	if r == nil {
		t.Fatal("LoadRecents returned nil on missing file; want empty Recents")
	}
	if len(r.Recent) != 0 || len(r.AgentTypes) != 0 {
		t.Errorf("LoadRecents on missing file = %+v; want empty", r)
	}
	// Record then save must still work.
	r.Record("x", "")
	if err := r.Save(); err != nil {
		t.Errorf("Save after load-missing: %v", err)
	}
}

func TestRecents_LoadMalformedReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "palette-recents.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := LoadRecents(path)
	if len(r.Recent) != 0 {
		t.Errorf("malformed file loaded non-empty recents: %+v", r)
	}
}

func TestRecents_LoadUnknownVersionReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "palette-recents.json")
	if err := os.WriteFile(path, []byte(`{"version":999}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := LoadRecents(path)
	if len(r.Recent) != 0 {
		t.Errorf("unknown version loaded non-empty recents: %+v", r)
	}
}

func TestRecents_TopAgentTypesFiltersUnavailable(t *testing.T) {
	r := &Recents{}
	r.recordAgentTypeAt("codex", 1)
	r.recordAgentTypeAt("gone", 2)
	r.recordAgentTypeAt("claude", 3)

	avail := map[string]bool{"codex": true, "claude": true}
	got := r.TopAgentTypes(avail)
	want := []string{"claude", "codex"}
	if len(got) != len(want) {
		t.Fatalf("len=%d; want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("slot %d = %q; want %q", i, got[i], w)
		}
	}
}
