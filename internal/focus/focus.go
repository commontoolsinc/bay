// Package focus provides integration with the bay-focus macOS helper.
// The helper is an optional Swift binary that switches macOS Spaces
// and activates applications. Without it, bay degrades gracefully —
// tmux navigation works, editors handle their own Space switching.
package focus

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// HelperPath returns the expected path to the bay-focus binary.
func HelperPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "bay", "bay-focus")
}

// SourcePath returns the path to the bay-focus Swift source file.
// The source is embedded in the bay repository.
func SourcePath() string {
	// Find it relative to the bay executable, or use a well-known path.
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		candidate := filepath.Join(dir, "..", "internal", "focus", "bay-focus.swift")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Fallback: check XDG data dir.
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "bay", "bay-focus.swift")
}

// HelperAvailable checks if the bay-focus binary exists and is executable.
func HelperAvailable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&0o111 != 0
}

// Compile compiles the bay-focus Swift helper from source.
func Compile(sourcePath, outputPath string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("bay-focus helper is macOS only")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("swiftc", "-O", "-o", outputPath, sourcePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ActivateApp uses the helper to switch to the Space containing an app
// and activate it. Returns an error if the helper is not available.
func ActivateApp(helperPath, bundleID string) error {
	if !HelperAvailable(helperPath) {
		return fmt.Errorf("bay-focus helper not available at %s", helperPath)
	}
	cmd := exec.Command(helperPath, "--app", bundleID)
	return cmd.Run()
}
