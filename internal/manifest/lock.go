package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WithLock runs fn while holding the manifest's process-wide file lock.
func WithLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating manifest dir: %w", err)
	}
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return fmt.Errorf("acquiring manifest lock: %w", err)
	}
	defer unlock()
	return fn()
}

func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
