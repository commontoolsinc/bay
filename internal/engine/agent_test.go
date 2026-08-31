package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/tmux"
)

func TestBuildAgentCommand_QuotesArgs(t *testing.T) {
	eng, _ := testEngine(t)
	eng.Config.Agents["claude"] = config.AgentConfig{
		Command:    "claude",
		Args:       []string{"--dangerously-skip-permissions"},
		LaunchArgs: []string{"--append-system-prompt", "be brief; don't ask"},
	}

	got, err := eng.buildAgentCommand("claude", eng.Config.Agents["claude"].Args,
		eng.Config.Agents["claude"].LaunchArgs, false)
	if err != nil {
		t.Fatalf("buildAgentCommand: %v", err)
	}
	want := `claude --dangerously-skip-permissions --append-system-prompt 'be brief; don'\''t ask'`
	if got != want {
		t.Errorf("command = %q, want %q", got, want)
	}

	// Plain flags stay unquoted; tilde paths keep their leading word form.
	got, err = eng.buildAgentCommand("claude", []string{"--config", "~/x.toml"}, nil, false)
	if err != nil {
		t.Fatalf("buildAgentCommand: %v", err)
	}
	if got != "claude --config ~/x.toml" {
		t.Errorf("command = %q, want unquoted plain args", got)
	}
}

// TestRecover_LaunchArgsNotReplayed asserts the reboot-recovery path
// directly (recoverSurfaceLaunch → SendKeys), complementing the
// undo-close restore test: launch_args must not be replayed when the
// agent resumes, so the session keeps its own model.
func TestRecover_LaunchArgsNotReplayed(t *testing.T) {
	eng, _ := testEngine(t)
	eng.Config.Agents["fable"] = config.AgentConfig{
		Extends:    "claude",
		LaunchArgs: []string{"--model", "fable"},
	}

	bay, err := eng.BayNew(BayNewOptions{Dock: "labs", Agent: "fable"})
	if err != nil {
		t.Fatalf("BayNew: %v", err)
	}
	if err := os.MkdirAll(bay.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Simulate reboot.
	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	if _, err := eng.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	resumed := false
	for _, c := range mockTmux.Calls {
		if c.Method != "SendKeys" || len(c.Args) < 2 {
			continue
		}
		if strings.Contains(c.Args[1], "--model") {
			t.Errorf("recover replayed launch args: %q", c.Args[1])
		}
		if strings.Contains(c.Args[1], "claude --continue") {
			resumed = true
		}
	}
	if !resumed {
		t.Errorf("expected recover to send claude --continue; calls=%+v", mockTmux.Calls)
	}
}

func TestAgentEnv_SortedAndExpanded(t *testing.T) {
	eng, _ := testEngine(t)
	eng.Config.Agents["claude"] = config.AgentConfig{
		Command: "claude",
		Env: map[string]string{
			"ZED":               "last",
			"CLAUDE_CONFIG_DIR": "~/.claude-work",
			"ALPHA":             "first",
		},
	}

	got := eng.agentEnv("claude")
	if len(got) != 3 {
		t.Fatalf("agentEnv = %v, want 3 entries", got)
	}
	// Sorted by key, so the rendered command line is stable across runs.
	if got[0] != "ALPHA=first" || got[2] != "ZED=last" {
		t.Errorf("agentEnv = %v, want sorted by key", got)
	}
	if strings.HasPrefix(got[1], "CLAUDE_CONFIG_DIR=~/") {
		t.Errorf("agentEnv = %v, want ~ expanded", got)
	}
	if !strings.HasSuffix(got[1], "/.claude-work") {
		t.Errorf("agentEnv[1] = %q, want it to end at the configured dir", got[1])
	}
}

func TestEnvPrefix_QuotesValueNotAssignment(t *testing.T) {
	// A value needing quotes must be quoted *inside* the assignment.
	// Quoting the whole word ('K=v') would make the shell treat it as a
	// command name and the agent would never launch. The leading `env`
	// keeps the line valid in fish, which rejects bare K=V assignments.
	got := envPrefix([]string{"K=two words", "PLAIN=/tmp/x"})
	want := `env K='two words' PLAIN=/tmp/x `
	if got != want {
		t.Errorf("envPrefix = %q, want %q", got, want)
	}
	if envPrefix(nil) != "" {
		t.Errorf("envPrefix(nil) = %q, want empty", envPrefix(nil))
	}
}

func TestLaunch_AgentSurfaceCarriesEnv(t *testing.T) {
	eng, _ := testEngine(t)
	eng.Config.Agents["claude"] = config.AgentConfig{
		Command: "claude",
		Env:     map[string]string{"CLAUDE_CONFIG_DIR": "/tmp/acct"},
	}

	if _, err := eng.BayNew(BayNewOptions{Dock: "labs", Agent: "claude"}); err != nil {
		t.Fatalf("BayNew: %v", err)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	found := false
	for _, c := range mockTmux.Calls {
		if c.Method != "RespawnPane" || len(c.Args) < 4 {
			continue
		}
		if !strings.Contains(c.Args[2], "claude") {
			continue
		}
		for _, arg := range c.Args[3:] {
			if arg == "CLAUDE_CONFIG_DIR=/tmp/acct" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected agent launch to carry env; calls=%+v", mockTmux.Calls)
	}
}

// TestRecover_EnvReplayed is the counterpart to
// TestRecover_LaunchArgsNotReplayed: launch args must NOT survive a
// resume, but env must. An agent that resumes without its configured
// CLAUDE_CONFIG_DIR silently reattaches to the default account.
func TestRecover_EnvReplayed(t *testing.T) {
	eng, _ := testEngine(t)
	eng.Config.Agents["claude"] = config.AgentConfig{
		Command: "claude",
		Env:     map[string]string{"CLAUDE_CONFIG_DIR": "/tmp/acct"},
	}

	bay, err := eng.BayNew(BayNewOptions{Dock: "labs", Agent: "claude"})
	if err != nil {
		t.Fatalf("BayNew: %v", err)
	}
	if err := os.MkdirAll(bay.Path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	mockTmux := eng.Tmux.(*tmux.Mock)
	mockTmux.Reset()

	if _, err := eng.Recover(); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	resumedWithEnv := false
	for _, c := range mockTmux.Calls {
		if c.Method != "SendKeys" || len(c.Args) < 2 {
			continue
		}
		if strings.Contains(c.Args[1], "claude --continue") &&
			strings.HasPrefix(c.Args[1], "env CLAUDE_CONFIG_DIR=/tmp/acct ") {
			resumedWithEnv = true
		}
	}
	if !resumedWithEnv {
		t.Errorf("expected resume to carry the env prefix; calls=%+v", mockTmux.Calls)
	}
}
