package focus

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHelperPath(t *testing.T) {
	path := HelperPath()
	if path == "" {
		t.Error("HelperPath should return a non-empty path")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("HelperPath should be absolute, got %q", path)
	}
}

func TestHelperAvailable_MissingBinary(t *testing.T) {
	if HelperAvailable("/nonexistent/path/bay-focus") {
		t.Error("should return false for missing binary")
	}
}

func TestHelperAvailable_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bay-focus")
	os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)

	if !HelperAvailable(bin) {
		t.Error("should return true for existing executable")
	}
}

func TestSourcePath(t *testing.T) {
	path := SourcePath()
	if path == "" {
		t.Error("SourcePath should return a non-empty path")
	}
}
