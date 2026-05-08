package engine

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// DispatchPrepareWorker starts a detached same-binary prepare worker for a bay.
func (e *Engine) DispatchPrepareWorker(dockName, bayID string) error {
	if dockName == "" {
		return fmt.Errorf("dock is required")
	}
	if bayID == "" {
		return fmt.Errorf("bay is required")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	args := []string{}
	if e.configPath != "" {
		args = append(args, "--config", e.configPath)
	}
	args = append(args, "prepare-worker", "--dock", dockName, "--bay", bayID)
	cmd := exec.Command(exe, args...)
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting prepare worker: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("releasing prepare worker: %w", err)
	}
	return nil
}
