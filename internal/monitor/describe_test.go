package monitor

import (
	"testing"

	"github.com/commontoolsinc/bay/internal/manifest"
)

func TestSelectDescribeCandidates(t *testing.T) {
	const now = 1_000_000

	mf := &manifest.Manifest{Docks: []manifest.Dock{{
		Name: "labs",
		Bays: []manifest.Bay{
			// Eligible: empty description, recently active.
			{ID: "b1", Type: manifest.BayTypeWorktree, Path: "/p1", LastActive: now - 60},
			// Eligible: auto description, due for refresh (old summary).
			{ID: "b2", Type: manifest.BayTypeWorktree, Path: "/p2", LastActive: now - 60,
				Description: "auto desc", DescriptionSource: manifest.DescriptionSourceAuto, DescriptionSummarizedAt: now - 3600},
			// Skip: user-set description.
			{ID: "b3", Type: manifest.BayTypeWorktree, Path: "/p3", LastActive: now - 60,
				Description: "mine", DescriptionSource: manifest.DescriptionSourceUser},
			// Skip: legacy (non-empty, empty source).
			{ID: "b4", Type: manifest.BayTypeWorktree, Path: "/p4", LastActive: now - 60, Description: "legacy"},
			// Skip: not recently active.
			{ID: "b5", Type: manifest.BayTypeWorktree, Path: "/p5", LastActive: now - activityWindow - 1},
			// Skip: summarized within the min interval.
			{ID: "b6", Type: manifest.BayTypeWorktree, Path: "/p6", LastActive: now - 60,
				DescriptionSource: manifest.DescriptionSourceAuto, DescriptionSummarizedAt: now - 10},
			// Skip: home pseudo-bay.
			{ID: manifest.HomeBayID, Type: manifest.BayTypeHome, Path: "/home", LastActive: now - 60},
			// Skip: no path.
			{ID: "b7", Type: manifest.BayTypeWorktree, LastActive: now - 60},
		},
	}}}

	got := selectDescribeCandidates(mf, now, describeMaxPerCycle)
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(got), got)
	}
	// Oldest-summarized first: b2 (summarized long ago) before b1 (never, 0).
	// b1 has summarizedAt 0, which sorts before b2's now-3600. So order is b1, b2.
	ids := []string{got[0].bay, got[1].bay}
	if ids[0] != "b1" || ids[1] != "b2" {
		t.Errorf("candidate order = %v, want [b1 b2]", ids)
	}
}

func TestSelectDescribeCandidatesCap(t *testing.T) {
	const now = 1_000_000
	var bays []manifest.Bay
	for _, id := range []string{"b1", "b2", "b3", "b4", "b5"} {
		bays = append(bays, manifest.Bay{ID: id, Type: manifest.BayTypeWorktree, Path: "/" + id, LastActive: now - 60})
	}
	mf := &manifest.Manifest{Docks: []manifest.Dock{{Name: "labs", Bays: bays}}}

	if got := selectDescribeCandidates(mf, now, 3); len(got) != 3 {
		t.Errorf("got %d candidates, want cap of 3", len(got))
	}
}
