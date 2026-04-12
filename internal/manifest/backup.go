package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	backupDir        = "backups"
	backupPrefix     = "manifest-"
	backupSuffix     = ".json"
	backupTimeFormat = "2006-01-02T15-04-05" // filesystem-safe ISO
	backupInterval   = 60 * time.Second      // at most one backup per minute
	retainMinutes    = 10                    // one per minute for last 10 minutes
	retainHours      = 24                    // one per hour for last 24 hours
)

const backupReadme = `# Bay Manifest Backups

Bay automatically backs up the manifest (workspace/surface state) here.

## Retention policy

- One backup per minute for the last 10 minutes
- One backup per hour for the last 24 hours
- Older backups are pruned automatically

## Restoring from a backup

If your manifest gets corrupted or loses data:

1. Stop the monitor:  bay monitor stop
2. cd to bay data:    cd ~/.local/share/bay
3. Pick a backup:     ls -lt backups/
4. Restore:           cp backups/manifest-YYYY-MM-DDTHH-MM-SS.json manifest.json
5. Recover:           bay recover

The manifest tracks workspace names, surface layout, branches, and
PR state. Worktree directories on disk are not affected by restoring
a manifest backup.
`

// BackupIfNeeded copies the current manifest into the backups directory
// if enough time has passed since the last backup. Called before each
// manifest write.
func BackupIfNeeded(manifestPath string) {
	dir := filepath.Join(filepath.Dir(manifestPath), backupDir)

	// Check if the manifest exists (nothing to back up on first run).
	if _, err := os.Stat(manifestPath); err != nil {
		return
	}

	// Ensure backup directory exists.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}

	// Write README on first use.
	readmePath := filepath.Join(dir, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		_ = os.WriteFile(readmePath, []byte(backupReadme), 0o644)
	}

	// Check if a backup is needed (at most once per minute).
	entries, _ := os.ReadDir(dir)
	var newest time.Time
	for _, e := range entries {
		if t, ok := parseBackupTime(e.Name()); ok {
			if t.After(newest) {
				newest = t
			}
		}
	}
	if !newest.IsZero() && time.Since(newest) < backupInterval {
		return // too soon
	}

	// Copy current manifest to backup.
	now := time.Now()
	backupName := backupPrefix + now.Format(backupTimeFormat) + backupSuffix
	backupPath := filepath.Join(dir, backupName)
	_ = copyFile(manifestPath, backupPath)

	// Prune old backups.
	pruneBackups(dir, now)
}

// parseBackupTime extracts the timestamp from a backup filename.
func parseBackupTime(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, backupSuffix) {
		return time.Time{}, false
	}
	timeStr := strings.TrimPrefix(name, backupPrefix)
	timeStr = strings.TrimSuffix(timeStr, backupSuffix)
	t, err := time.ParseInLocation(backupTimeFormat, timeStr, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// pruneBackups removes backups outside the retention windows.
// Keeps one per minute for the last 10 minutes, one per hour for the
// last 24 hours.
func pruneBackups(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type backup struct {
		name string
		t    time.Time
	}
	var backups []backup
	for _, e := range entries {
		if t, ok := parseBackupTime(e.Name()); ok {
			backups = append(backups, backup{e.Name(), t})
		}
	}

	// Sort newest first.
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].t.After(backups[j].t)
	})

	// Decide what to keep. Walk from newest to oldest, tracking
	// which minute/hour buckets we've already kept.
	keptMinutes := map[string]bool{}
	keptHours := map[string]bool{}
	cutoffMinutes := now.Add(-time.Duration(retainMinutes) * time.Minute)
	cutoffHours := now.Add(-time.Duration(retainHours) * time.Hour)

	for _, b := range backups {
		keep := false

		if b.t.After(cutoffMinutes) {
			// Within last 10 minutes — keep one per minute.
			key := b.t.Format("2006-01-02T15-04")
			if !keptMinutes[key] {
				keptMinutes[key] = true
				keep = true
			}
		} else if b.t.After(cutoffHours) {
			// Within last 24 hours — keep one per hour.
			key := b.t.Format("2006-01-02T15")
			if !keptHours[key] {
				keptHours[key] = true
				keep = true
			}
		}

		if !keep {
			_ = os.Remove(filepath.Join(dir, b.name))
		}
	}
}

// RestoreBackup lists available backups for display.
func ListBackups(manifestPath string) ([]string, error) {
	dir := filepath.Join(filepath.Dir(manifestPath), backupDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading backup directory: %w", err)
	}

	var names []string
	for _, e := range entries {
		if _, ok := parseBackupTime(e.Name()); ok {
			names = append(names, e.Name())
		}
	}
	// Sort newest first.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}
