package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	StatusFreshnessNotRunning          = "not_running"
	StatusFreshnessCurrent             = "current"
	StatusFreshnessStale               = "stale"
	StatusFreshnessUnknown             = "unknown"
	StatusFreshnessDifferentExecutable = "different_executable"

	StatusReasonMetadataMissing       = "metadata_missing"
	StatusReasonMetadataInvalid       = "metadata_invalid"
	StatusReasonMetadataPIDMismatch   = "metadata_pid_mismatch"
	StatusReasonHeartbeatStale        = "heartbeat_stale"
	StatusReasonVersionMismatch       = "version_mismatch"
	StatusReasonBinaryChangedSamePath = "binary_changed_same_path"
	StatusReasonDifferentExecutable   = "different_executable"
)

const runtimeStatusSchema = 1

// RuntimeStatusHeartbeat is how often the monitor refreshes its runtime
// metadata heartbeat. Version freshness is written on startup/reload;
// this slower cadence is only health evidence.
const RuntimeStatusHeartbeat = 5 * time.Minute

// BinaryIdentity captures enough file identity to compare the monitor's
// loaded executable against the currently invoked bay binary.
type BinaryIdentity struct {
	Path          string `json:"exe_path"`
	MTimeUnixNano int64  `json:"exe_mtime_unix_nano"`
	Size          int64  `json:"exe_size"`
	Dev           uint64 `json:"exe_dev,omitempty"`
	Ino           uint64 `json:"exe_ino,omitempty"`
}

// RuntimeStatus is the monitor-owned status file schema.
type RuntimeStatus struct {
	Schema     int       `json:"schema"`
	PID        int       `json:"pid"`
	Version    string    `json:"version"`
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Generation int       `json:"generation"`
	BinaryIdentity
}

// StatusAssessment is the fully assessed status returned to CLI callers.
// It combines PID liveness, monitor self-report metadata, and the identity
// of the bay binary running the status command.
type StatusAssessment struct {
	Running   bool              `json:"running"`
	PID       int               `json:"pid"`
	Freshness string            `json:"freshness"`
	Reason    string            `json:"reason"`
	Monitor   *RuntimeStatus    `json:"monitor,omitempty"`
	Current   CurrentBayRuntime `json:"current"`
}

// CurrentBayRuntime describes the bay binary currently running the status
// command.
type CurrentBayRuntime struct {
	Version string `json:"version"`
	BinaryIdentity
}

// CurrentBinaryIdentity stats path and returns a comparable binary identity.
func CurrentBinaryIdentity(path string) (BinaryIdentity, error) {
	info, err := os.Stat(path)
	if err != nil {
		return BinaryIdentity{}, err
	}
	id := BinaryIdentity{
		Path:          path,
		MTimeUnixNano: info.ModTime().UnixNano(),
		Size:          info.Size(),
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		id.Dev = uint64(st.Dev)
		id.Ino = uint64(st.Ino)
	}
	return id, nil
}

// ReadRuntimeStatus reads monitor runtime metadata from path.
func ReadRuntimeStatus(path string) (*RuntimeStatus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var status RuntimeStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	if status.Schema != runtimeStatusSchema {
		return nil, fmt.Errorf("unsupported monitor status schema %d", status.Schema)
	}
	return &status, nil
}

// WriteRuntimeStatusAtomic writes monitor runtime metadata using a temp file
// and rename so readers never observe a partial JSON document.
func WriteRuntimeStatusAtomic(path string, status RuntimeStatus) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating monitor status directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".monitor-status-*.tmp")
	if err != nil {
		return fmt.Errorf("creating monitor status temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(status); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("encoding monitor status: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing monitor status: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing monitor status: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("installing monitor status: %w", err)
	}
	cleanup = false
	return nil
}

// StatusAssessment checks monitor liveness and assesses any runtime metadata.
func (m *Monitor) StatusAssessment(currentVersion string) (StatusAssessment, error) {
	exe, err := os.Executable()
	if err != nil {
		return StatusAssessment{}, fmt.Errorf("finding executable: %w", err)
	}
	current, err := CurrentBinaryIdentity(exe)
	if err != nil {
		return StatusAssessment{}, fmt.Errorf("stat executable: %w", err)
	}
	running, pid, err := m.Status()
	if err != nil {
		return StatusAssessment{}, err
	}
	if !running {
		return StatusAssessment{
			Running:   false,
			PID:       pid,
			Freshness: StatusFreshnessNotRunning,
			Current: CurrentBayRuntime{
				Version:        currentVersion,
				BinaryIdentity: current,
			},
		}, nil
	}
	var status *RuntimeStatus
	var statusErr error
	if m.runtimeStatusPath == "" {
		statusErr = os.ErrNotExist
	} else {
		status, statusErr = ReadRuntimeStatus(m.runtimeStatusPath)
	}
	return AssessRuntimeStatus(pid, currentVersion, current, status, statusErr, time.Now(), RuntimeStatusHeartbeat), nil
}

// AssessRuntimeStatus classifies runtime metadata against the currently
// invoked bay binary.
func AssessRuntimeStatus(pid int, currentVersion string, current BinaryIdentity, status *RuntimeStatus, statusErr error, now time.Time, heartbeat time.Duration) StatusAssessment {
	assessment := StatusAssessment{
		Running:   true,
		PID:       pid,
		Freshness: StatusFreshnessCurrent,
		Current: CurrentBayRuntime{
			Version:        currentVersion,
			BinaryIdentity: current,
		},
	}

	if statusErr != nil {
		assessment.Freshness = StatusFreshnessUnknown
		if errors.Is(statusErr, os.ErrNotExist) {
			assessment.Reason = StatusReasonMetadataMissing
		} else {
			assessment.Reason = StatusReasonMetadataInvalid
		}
		return assessment
	}
	assessment.Monitor = status
	if status == nil {
		assessment.Freshness = StatusFreshnessUnknown
		assessment.Reason = StatusReasonMetadataMissing
		return assessment
	}
	if status.PID != pid {
		assessment.Freshness = StatusFreshnessUnknown
		assessment.Reason = StatusReasonMetadataPIDMismatch
		return assessment
	}

	staleAfter := 3 * heartbeat
	if staleFloor := 15 * time.Minute; staleAfter < staleFloor {
		staleAfter = staleFloor
	}
	heartbeatStale := !status.LastSeenAt.IsZero() && now.Sub(status.LastSeenAt) > staleAfter

	if status.Version != currentVersion {
		assessment.Freshness = StatusFreshnessStale
		assessment.Reason = StatusReasonVersionMismatch
		return assessment
	}
	if status.Path != "" && current.Path != "" && status.Path != current.Path {
		assessment.Freshness = StatusFreshnessDifferentExecutable
		assessment.Reason = StatusReasonDifferentExecutable
		return assessment
	}
	if status.Path == current.Path && !sameBinaryIdentity(status.BinaryIdentity, current) {
		assessment.Freshness = StatusFreshnessStale
		assessment.Reason = StatusReasonBinaryChangedSamePath
		return assessment
	}
	if heartbeatStale {
		assessment.Reason = StatusReasonHeartbeatStale
	}
	return assessment
}

func sameBinaryIdentity(a, b BinaryIdentity) bool {
	if a.MTimeUnixNano != b.MTimeUnixNano || a.Size != b.Size {
		return false
	}
	if a.Dev != 0 && b.Dev != 0 && a.Dev != b.Dev {
		return false
	}
	if a.Ino != 0 && b.Ino != 0 && a.Ino != b.Ino {
		return false
	}
	return true
}
