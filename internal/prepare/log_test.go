package prepare

import (
	"strings"
	"testing"
)

func TestFilterRunsForBay(t *testing.T) {
	log := strings.Join([]string{
		"==== run bay=b1 step=vendors 2026-05-07T10:00:00Z ====",
		"b1 output",
		"==== finished bay=b1 step=vendors 2026-05-07T10:00:01Z status=ready ====",
		"==== run bay=b2 step=vendors 2026-05-07T10:00:02Z ====",
		"b2 output",
		"==== finished bay=b2 step=vendors 2026-05-07T10:00:03Z status=failed ====",
		"",
	}, "\n")

	var b strings.Builder
	if err := FilterRunsForBay(&b, strings.NewReader(log), "b1"); err != nil {
		t.Fatalf("FilterRunsForBay: %v", err)
	}
	got := b.String()
	if !strings.Contains(got, "b1 output") {
		t.Fatalf("filtered log missing b1 run:\n%s", got)
	}
	if strings.Contains(got, "b2 output") {
		t.Fatalf("filtered log included b2 run:\n%s", got)
	}
}

func TestFilterRunsForBay_EmptyBayCopiesAll(t *testing.T) {
	const log = "==== run bay=b1 step=vendors ts ====\nx\n"
	var b strings.Builder
	if err := FilterRunsForBay(&b, strings.NewReader(log), ""); err != nil {
		t.Fatalf("FilterRunsForBay: %v", err)
	}
	if b.String() != log {
		t.Fatalf("FilterRunsForBay copied %q, want %q", b.String(), log)
	}
}

func TestFilterRunsForBay_LongLine(t *testing.T) {
	longLine := strings.Repeat("x", 2*1024*1024)
	log := strings.Join([]string{
		"==== run bay=b1 step=vendors 2026-05-07T10:00:00Z ====",
		longLine,
		"==== run bay=b2 step=vendors 2026-05-07T10:00:02Z ====",
		"b2 output",
	}, "\n")

	var b strings.Builder
	if err := FilterRunsForBay(&b, strings.NewReader(log), "b1"); err != nil {
		t.Fatalf("FilterRunsForBay: %v", err)
	}
	got := b.String()
	if !strings.Contains(got, longLine) {
		t.Fatal("filtered log missing long line")
	}
	if strings.Contains(got, "b2 output") {
		t.Fatalf("filtered log included b2 run")
	}
}
