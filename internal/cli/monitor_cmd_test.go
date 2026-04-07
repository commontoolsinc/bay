package cli

import (
	"path/filepath"
	"testing"
)

func TestNewMonitorWithConfig_MissingConfigUsesDefaults(t *testing.T) {
	oldCfgPath := cfgPath
	t.Cleanup(func() {
		cfgPath = oldCfgPath
	})

	cfgPath = filepath.Join(t.TempDir(), "missing-config.toml")

	mon, err := newMonitorWithConfig()
	if err != nil {
		t.Fatalf("newMonitorWithConfig: %v", err)
	}
	if mon == nil {
		t.Fatal("expected monitor instance")
	}
}
