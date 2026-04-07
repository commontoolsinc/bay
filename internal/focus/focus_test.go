package focus

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestSwiftSource_TypeChecks(t *testing.T) {
	// The Swift source imports AppKit / CoreGraphics, which only exist
	// in the macOS Swift toolchain. Linux Swift installs (e.g. on the
	// Ubuntu CI runner) would fail with "no such module 'AppKit'".
	if runtime.GOOS != "darwin" {
		t.Skip("AppKit only available on darwin")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
	}

	// Find the Swift source relative to this test file.
	source := filepath.Join("bay-focus.swift")
	if _, err := os.Stat(source); err != nil {
		t.Skipf("Swift source not found at %s", source)
	}

	cmd := exec.Command("swiftc", "-typecheck", source)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Swift type-check failed:\n%s", out)
	}
}
