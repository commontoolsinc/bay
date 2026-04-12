package manifest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupIfNeeded_CreatesBackupOnSecondWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	// No backup when file doesn't exist.
	BackupIfNeeded(path)
	backups, _ := ListBackups(path)
	if len(backups) != 0 {
		t.Fatal("no backup expected when manifest doesn't exist")
	}

	// Create the manifest.
	os.WriteFile(path, []byte(`{"version":1}`), 0o644)

	// First backup.
	BackupIfNeeded(path)
	backups, _ = ListBackups(path)
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}
}

func TestBackupIfNeeded_SkipsWithinInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	os.WriteFile(path, []byte(`{"version":1}`), 0o644)

	BackupIfNeeded(path)
	BackupIfNeeded(path) // should be skipped (within 60s)
	BackupIfNeeded(path) // should be skipped

	backups, _ := ListBackups(path)
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup (interval not elapsed), got %d", len(backups))
	}
}

func TestBackupIfNeeded_CreatesReadme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	os.WriteFile(path, []byte(`{"version":1}`), 0o644)

	BackupIfNeeded(path)

	readme := filepath.Join(dir, backupDir, "README.md")
	if _, err := os.Stat(readme); err != nil {
		t.Fatal("README.md should be created in backups directory")
	}
}

func TestParseBackupTime(t *testing.T) {
	name := "manifest-2026-04-12T15-30-00.json"
	got, ok := parseBackupTime(name)
	if !ok {
		t.Fatal("should parse valid backup name")
	}
	if got.Hour() != 15 || got.Minute() != 30 {
		t.Errorf("parsed time = %v, want 15:30", got)
	}

	// Invalid names.
	if _, ok := parseBackupTime("README.md"); ok {
		t.Error("should not parse non-backup file")
	}
	if _, ok := parseBackupTime("manifest-bad-time.json"); ok {
		t.Error("should not parse malformed timestamp")
	}
}

func TestPruneBackups_KeepsRetentionPolicy(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// Create backups spanning different retention windows.
	times := []time.Duration{
		-1 * time.Minute,  // recent — keep
		-2 * time.Minute,  // recent — keep
		-3 * time.Minute,  // recent — keep
		-2 * time.Hour,    // hourly — keep
		-3 * time.Hour,    // hourly — keep
		-25 * time.Hour,   // expired — prune
		-48 * time.Hour,   // expired — prune
	}
	for _, d := range times {
		ts := now.Add(d)
		name := backupPrefix + ts.Format(backupTimeFormat) + backupSuffix
		os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644)
	}

	pruneBackups(dir, now)

	remaining, _ := os.ReadDir(dir)
	// Should keep: 3 minute-level + 2 hour-level = 5
	if len(remaining) != 5 {
		names := make([]string, len(remaining))
		for i, e := range remaining {
			names[i] = e.Name()
		}
		t.Errorf("expected 5 backups after prune, got %d: %v", len(remaining), names)
	}
}

func TestPruneBackups_DeduplicatesPerBucket(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// Two backups in the same minute — only one should survive.
	// Use a fixed time to avoid crossing minute boundaries.
	t1 := time.Date(2026, 4, 12, 14, 30, 10, 0, time.Local)
	t2 := time.Date(2026, 4, 12, 14, 30, 40, 0, time.Local)
	now = time.Date(2026, 4, 12, 14, 32, 0, 0, time.Local)
	for _, ts := range []time.Time{t1, t2} {
		name := backupPrefix + ts.Format(backupTimeFormat) + backupSuffix
		os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644)
	}

	pruneBackups(dir, now)

	remaining, _ := os.ReadDir(dir)
	if len(remaining) != 1 {
		t.Errorf("expected 1 backup (deduplicated per minute), got %d", len(remaining))
	}
}

func TestListBackups_SortsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	os.MkdirAll(filepath.Join(dir, backupDir), 0o755)

	now := time.Now()
	for _, d := range []time.Duration{-3 * time.Minute, -1 * time.Minute, -5 * time.Minute} {
		ts := now.Add(d)
		name := backupPrefix + ts.Format(backupTimeFormat) + backupSuffix
		os.WriteFile(filepath.Join(dir, backupDir, name), []byte("{}"), 0o644)
	}

	backups, err := ListBackups(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(backups))
	}
	// Newest first.
	for i := 1; i < len(backups); i++ {
		if backups[i] > backups[i-1] {
			t.Errorf("backups not sorted newest-first: %v", backups)
			break
		}
	}
}
