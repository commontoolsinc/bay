//go:build integration

package prepare

import (
	"os"
	"os/exec"
	"testing"
)

// TestStepLockAcrossProcesses exercises the prepare flock path across a real
// process boundary. Run with:
//
//	go test -tags=integration ./internal/prepare -run TestStepLockAcrossProcesses
func TestStepLockAcrossProcesses(t *testing.T) {
	if os.Getenv("BAY_PREPARE_LOCK_CHILD") == "1" {
		path := os.Getenv("BAY_PREPARE_LOCK_PATH")
		lock, acquired, err := tryStepLock(path)
		if err != nil {
			t.Fatalf("child tryStepLock: %v", err)
		}
		if acquired {
			lock.Unlock()
			t.Fatal("child acquired lock while parent held it")
		}
		return
	}

	path := stepLockPath(t.TempDir(), "labs", "b1", "vendors")
	lock, acquired, err := tryStepLock(path)
	if err != nil {
		t.Fatalf("parent tryStepLock: %v", err)
	}
	if !acquired {
		t.Fatal("parent did not acquire lock")
	}
	defer lock.Unlock()

	cmd := exec.Command(os.Args[0], "-test.run=TestStepLockAcrossProcesses")
	cmd.Env = append(os.Environ(),
		"BAY_PREPARE_LOCK_CHILD=1",
		"BAY_PREPARE_LOCK_PATH="+path,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child failed: %v\n%s", err, out)
	}
}
