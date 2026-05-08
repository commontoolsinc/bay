package cli

import (
	"strings"
	"testing"
)

func TestFilterPrepareLogForBay(t *testing.T) {
	log := strings.Join([]string{
		"==== run bay=b1 step=vendors 2026-05-07T10:00:00Z ====",
		"b1 output",
		"==== finished bay=b1 step=vendors 2026-05-07T10:00:01Z status=ready ====",
		"==== run bay=b2 step=vendors 2026-05-07T10:00:02Z ====",
		"b2 output",
		"==== finished bay=b2 step=vendors 2026-05-07T10:00:03Z status=failed ====",
		"",
	}, "\n")

	got := filterPrepareLogForBay(log, "b1")
	if !strings.Contains(got, "b1 output") {
		t.Fatalf("filtered log missing b1 run:\n%s", got)
	}
	if strings.Contains(got, "b2 output") {
		t.Fatalf("filtered log included b2 run:\n%s", got)
	}
}
