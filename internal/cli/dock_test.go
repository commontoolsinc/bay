package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDockNameResolvesDotPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myproject")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(root)

	if got := defaultDockName("."); got != "myproject" {
		t.Fatalf("defaultDockName(.) = %q, want myproject", got)
	}
}
