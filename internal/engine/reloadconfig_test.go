package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReloadConfig(t *testing.T) {
	eng, dir := testEngine(t)
	cp := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(cp, []byte("default_agent = \"codex\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := eng.ReloadConfig(); err != nil {
		t.Fatal(err)
	}
	if eng.Config.DefaultAgent != "codex" {
		t.Errorf("DefaultAgent = %q, want codex after reload", eng.Config.DefaultAgent)
	}

	// A missing/unreadable config keeps the existing config rather than
	// clobbering it.
	if err := os.Remove(cp); err != nil {
		t.Fatal(err)
	}
	_ = eng.ReloadConfig()
	if eng.Config.DefaultAgent != "codex" {
		t.Errorf("DefaultAgent = %q, want unchanged after failed reload", eng.Config.DefaultAgent)
	}
}
