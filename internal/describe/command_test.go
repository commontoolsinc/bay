package describe

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
)

func TestCommandSummarizerFindsRuntimeBesideResolvedSymlink(t *testing.T) {
	commandDir := t.TempDir()
	installDir := t.TempDir()

	runtimePath := filepath.Join(installDir, "bay-test-runtime")
	if err := os.WriteFile(runtimePath, []byte("#!/bin/sh\nprintf 'Recovered description\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	realCommand := filepath.Join(installDir, "bay-test-summarizer")
	if err := os.WriteFile(realCommand, []byte("#!/usr/bin/env bay-test-runtime\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkedCommand := filepath.Join(commandDir, "bay-test-summarizer")
	if err := os.Symlink(realCommand, linkedCommand); err != nil {
		t.Fatal(err)
	}

	// The daemon can find the stable CLI symlink, but not the runtime named
	// by its shebang. summarizerEnv must add the symlink target's directory.
	t.Setenv("PATH", commandDir)
	cfg := &config.Config{Describe: config.DescribeConfig{Command: []string{"bay-test-summarizer"}}}
	got, err := (commandSummarizer{config: cfg}).Summarize(context.Background(), "ignored prompt")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got != "Recovered description\n" {
		t.Fatalf("output = %q, want recovered description", got)
	}
}
