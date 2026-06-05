package engine

import (
	"fmt"
	"os"
)

// DispatchDescribeWorker starts a detached same-binary describe worker
// for a bay, which summarizes the bay's agent conversation into its
// description. Mirrors DispatchPrepareWorker; the monitor calls this on
// its slow cadence. See docs/design/auto-descriptions.md.
func (e *Engine) DispatchDescribeWorker(dockName, bayID string) error {
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
	args = append(args, "describe-worker", "--dock", dockName, "--bay", bayID)
	startWorker := e.startWorker
	if startWorker == nil {
		startWorker = startWorkerProcess
	}
	return startWorker(exe, args)
}
