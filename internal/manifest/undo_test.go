package manifest

import (
	"testing"
)

func TestPushClosedEntry_EnforcesCap(t *testing.T) {
	d := &Dock{Name: "labs"}
	for i := range ClosedQueueMax + 5 {
		d.PushClosedEntry(ClosedEntry{ClosedAt: int64(i), Kind: ClosedKindSurface})
	}
	if len(d.ClosedEntries) != ClosedQueueMax {
		t.Fatalf("expected queue capped at %d, got %d", ClosedQueueMax, len(d.ClosedEntries))
	}
	// Oldest entries must have been dropped — the first surviving entry
	// should have ClosedAt == 5 (first 5 were dropped).
	if d.ClosedEntries[0].ClosedAt != 5 {
		t.Errorf("expected oldest surviving ClosedAt=5 after overflow, got %d", d.ClosedEntries[0].ClosedAt)
	}
}

func TestPruneClosedEntries_DropsExpired(t *testing.T) {
	d := &Dock{Name: "labs"}
	now := int64(1_000_000)
	// One stale, one fresh.
	d.PushClosedEntry(ClosedEntry{ClosedAt: now - ClosedQueueTTLSeconds - 10, Kind: ClosedKindSurface})
	d.PushClosedEntry(ClosedEntry{ClosedAt: now - 10, Kind: ClosedKindSurface})

	changed := d.PruneClosedEntries(now)
	if !changed {
		t.Error("expected changed=true after pruning stale entry")
	}
	if len(d.ClosedEntries) != 1 {
		t.Fatalf("expected 1 entry after prune, got %d", len(d.ClosedEntries))
	}
	if d.ClosedEntries[0].ClosedAt != now-10 {
		t.Errorf("wrong entry survived prune: ClosedAt=%d", d.ClosedEntries[0].ClosedAt)
	}
}

func TestPruneClosedEntries_NoChangeWhenAllFresh(t *testing.T) {
	d := &Dock{Name: "labs"}
	now := int64(1_000_000)
	d.PushClosedEntry(ClosedEntry{ClosedAt: now - 10, Kind: ClosedKindSurface})
	d.PushClosedEntry(ClosedEntry{ClosedAt: now - 5, Kind: ClosedKindSurface})

	if changed := d.PruneClosedEntries(now); changed {
		t.Error("expected changed=false when nothing expired")
	}
}

func TestPeekClosedEntry_ReturnsMostRecent(t *testing.T) {
	d := &Dock{Name: "labs"}
	now := int64(1_000_000)
	d.PushClosedEntry(ClosedEntry{ClosedAt: now - 100, Kind: ClosedKindSurface})
	d.PushClosedEntry(ClosedEntry{ClosedAt: now - 50, Kind: ClosedKindSurface})

	entry := d.PeekClosedEntry(now)
	if entry == nil {
		t.Fatal("expected non-nil peek")
	}
	if entry.ClosedAt != now-50 {
		t.Errorf("expected most recent ClosedAt=%d, got %d", now-50, entry.ClosedAt)
	}
	// Peek must not remove.
	if len(d.ClosedEntries) != 2 {
		t.Errorf("peek mutated queue: %d entries left", len(d.ClosedEntries))
	}
}

func TestPeekClosedEntry_EmptyAfterPrune(t *testing.T) {
	d := &Dock{Name: "labs"}
	now := int64(1_000_000)
	d.PushClosedEntry(ClosedEntry{ClosedAt: now - ClosedQueueTTLSeconds - 1, Kind: ClosedKindSurface})

	if entry := d.PeekClosedEntry(now); entry != nil {
		t.Errorf("expected nil peek after pruning the only entry, got %+v", entry)
	}
}

func TestRemoveClosedEntryAt_MatchesByTimestamp(t *testing.T) {
	d := &Dock{Name: "labs"}
	d.PushClosedEntry(ClosedEntry{ClosedAt: 100, Kind: ClosedKindSurface})
	d.PushClosedEntry(ClosedEntry{ClosedAt: 200, Kind: ClosedKindSurface})
	d.PushClosedEntry(ClosedEntry{ClosedAt: 300, Kind: ClosedKindSurface})

	if !d.RemoveClosedEntryAt(200) {
		t.Fatal("expected RemoveClosedEntryAt(200) to return true")
	}
	if len(d.ClosedEntries) != 2 {
		t.Fatalf("expected 2 entries remaining, got %d", len(d.ClosedEntries))
	}
	for _, e := range d.ClosedEntries {
		if e.ClosedAt == 200 {
			t.Error("entry with ClosedAt=200 still present after remove")
		}
	}
}

func TestRemoveClosedEntryAt_NoMatchIsNoop(t *testing.T) {
	d := &Dock{Name: "labs"}
	d.PushClosedEntry(ClosedEntry{ClosedAt: 100, Kind: ClosedKindSurface})

	if d.RemoveClosedEntryAt(999) {
		t.Error("expected false when no entry matches")
	}
	if len(d.ClosedEntries) != 1 {
		t.Errorf("unrelated remove mutated the queue: %d entries", len(d.ClosedEntries))
	}
}
