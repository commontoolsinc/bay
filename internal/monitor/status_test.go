package monitor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeStatusReadWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "monitor-status.json")
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	status := RuntimeStatus{
		Schema:     runtimeStatusSchema,
		PID:        123,
		Version:    "v1 (abc12345)",
		StartedAt:  now.Add(-time.Hour),
		LastSeenAt: now,
		Generation: 2,
		BinaryIdentity: BinaryIdentity{
			Path:          "/tmp/bay",
			MTimeUnixNano: 100,
			Size:          42,
			Dev:           1,
			Ino:           2,
		},
	}

	if err := WriteRuntimeStatusAtomic(path, status); err != nil {
		t.Fatalf("WriteRuntimeStatusAtomic: %v", err)
	}
	got, err := ReadRuntimeStatus(path)
	if err != nil {
		t.Fatalf("ReadRuntimeStatus: %v", err)
	}
	if got.PID != status.PID || got.Version != status.Version || got.Generation != status.Generation {
		t.Fatalf("status round trip mismatch: got %+v want %+v", got, status)
	}
	if got.Path != status.Path || got.MTimeUnixNano != status.MTimeUnixNano || got.Size != status.Size {
		t.Fatalf("binary identity mismatch: got %+v want %+v", got.BinaryIdentity, status.BinaryIdentity)
	}
	if !got.StartedAt.Equal(status.StartedAt) || !got.LastSeenAt.Equal(status.LastSeenAt) {
		t.Fatalf("times mismatch: got started=%s seen=%s", got.StartedAt, got.LastSeenAt)
	}
}

func TestAssessRuntimeStatusStates(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	current := BinaryIdentity{Path: "/bin/bay", MTimeUnixNano: 100, Size: 10, Dev: 1, Ino: 2}
	base := &RuntimeStatus{
		Schema:         runtimeStatusSchema,
		PID:            123,
		Version:        "v1 (abc12345)",
		StartedAt:      now.Add(-time.Hour),
		LastSeenAt:     now,
		Generation:     1,
		BinaryIdentity: current,
	}

	tests := []struct {
		name       string
		status     *RuntimeStatus
		statusErr  error
		current    BinaryIdentity
		version    string
		wantFresh  string
		wantReason string
	}{
		{
			name:      "current",
			status:    cloneRuntimeStatus(base),
			current:   current,
			version:   "v1 (abc12345)",
			wantFresh: StatusFreshnessCurrent,
		},
		{
			name:       "missing metadata",
			statusErr:  os.ErrNotExist,
			current:    current,
			version:    "v1 (abc12345)",
			wantFresh:  StatusFreshnessUnknown,
			wantReason: StatusReasonMetadataMissing,
		},
		{
			name:       "invalid metadata",
			statusErr:  errors.New("bad json"),
			current:    current,
			version:    "v1 (abc12345)",
			wantFresh:  StatusFreshnessUnknown,
			wantReason: StatusReasonMetadataInvalid,
		},
		{
			name: "pid mismatch",
			status: func() *RuntimeStatus {
				s := cloneRuntimeStatus(base)
				s.PID = 999
				return s
			}(),
			current:    current,
			version:    "v1 (abc12345)",
			wantFresh:  StatusFreshnessUnknown,
			wantReason: StatusReasonMetadataPIDMismatch,
		},
		{
			name: "heartbeat stale",
			status: func() *RuntimeStatus {
				s := cloneRuntimeStatus(base)
				s.LastSeenAt = now.Add(-time.Hour)
				return s
			}(),
			current:    current,
			version:    "v1 (abc12345)",
			wantFresh:  StatusFreshnessCurrent,
			wantReason: StatusReasonHeartbeatStale,
		},
		{
			name:       "version mismatch",
			status:     cloneRuntimeStatus(base),
			current:    current,
			version:    "v2 (def67890)",
			wantFresh:  StatusFreshnessStale,
			wantReason: StatusReasonVersionMismatch,
		},
		{
			name: "same path binary changed",
			status: func() *RuntimeStatus {
				s := cloneRuntimeStatus(base)
				s.MTimeUnixNano = 90
				return s
			}(),
			current:    current,
			version:    "v1 (abc12345)",
			wantFresh:  StatusFreshnessStale,
			wantReason: StatusReasonBinaryChangedSamePath,
		},
		{
			name: "different executable same version",
			status: func() *RuntimeStatus {
				s := cloneRuntimeStatus(base)
				s.Path = "/opt/bay"
				return s
			}(),
			current:    current,
			version:    "v1 (abc12345)",
			wantFresh:  StatusFreshnessDifferentExecutable,
			wantReason: StatusReasonDifferentExecutable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AssessRuntimeStatus(123, tt.version, tt.current, tt.status, tt.statusErr, now, RuntimeStatusHeartbeat)
			if got.Freshness != tt.wantFresh || got.Reason != tt.wantReason {
				t.Fatalf("AssessRuntimeStatus freshness=%q reason=%q, want %q/%q", got.Freshness, got.Reason, tt.wantFresh, tt.wantReason)
			}
		})
	}
}

func TestMonitorInitializeRuntimeStatusIncrementsGeneration(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bay")
	if err := os.WriteFile(bin, []byte("fake bay"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	statusPath := filepath.Join(dir, "monitor-status.json")
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

	first := New(nil, "", "", "", 1)
	first.SetRuntimeStatus(statusPath, "v1 (abc12345)")
	first.initializeRuntimeStatus(bin, now)
	written, err := ReadRuntimeStatus(statusPath)
	if err != nil {
		t.Fatalf("ReadRuntimeStatus first: %v", err)
	}
	if written.Generation != 1 {
		t.Fatalf("first generation = %d, want 1", written.Generation)
	}

	second := New(nil, "", "", "", 1)
	second.SetRuntimeStatus(statusPath, "v2 (def67890)")
	second.initializeRuntimeStatus(bin, now.Add(time.Minute))
	reloaded, err := ReadRuntimeStatus(statusPath)
	if err != nil {
		t.Fatalf("ReadRuntimeStatus second: %v", err)
	}
	if reloaded.Generation != 2 {
		t.Fatalf("reload generation = %d, want 2", reloaded.Generation)
	}
	if !reloaded.StartedAt.Equal(written.StartedAt) {
		t.Fatalf("started_at changed across same-pid reload: got %s want %s", reloaded.StartedAt, written.StartedAt)
	}
	if reloaded.Version != "v2 (def67890)" {
		t.Fatalf("version = %q, want reloaded version", reloaded.Version)
	}
}

func cloneRuntimeStatus(status *RuntimeStatus) *RuntimeStatus {
	clone := *status
	return &clone
}
