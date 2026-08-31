package engine

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

func (e *Engine) validateAgentName(agentName string) error {
	if agentName == "" {
		return fmt.Errorf("agent name is required")
	}
	info, ok := e.Config.ResolveAgent(agentName)
	if !ok {
		return fmt.Errorf("unknown agent %q", agentName)
	}
	if strings.TrimSpace(info.Command) == "" {
		return fmt.Errorf("agent %q has no command configured", agentName)
	}
	return nil
}

// DefaultAgent returns the effective default agent for a dock, or "" if none.
func (e *Engine) DefaultAgent(dockName string) string {
	m, err := e.LoadManifest()
	if err != nil {
		return ""
	}
	return e.resolvedDockAgent(dockName, m)
}

func (e *Engine) resolveBayAgent(dockName string, m *manifest.Manifest, requested string, requireAgent bool) (string, error) {
	if requested != "" {
		if err := e.validateAgentName(requested); err != nil {
			return "", err
		}
		return requested, nil
	}

	agentName := e.resolvedDockAgent(dockName, m)
	if agentName == "" {
		if requireAgent {
			return "", fmt.Errorf("dock %q has no default agent configured", dockName)
		}
		return "", nil
	}
	if err := e.validateAgentName(agentName); err != nil {
		return "", err
	}
	return agentName, nil
}

// buildAgentCommand assembles the agent launch command. When resume is
// true, the agent's configured resume_args are spliced in (e.g.
// "--continue" for Claude Code) so the prior session is picked up;
// used by recovery and undo-close. launchArgs are dropped on resume:
// they carry session-start flags like --model, and replaying those
// would override state the resumed session restores itself (Claude
// keeps a resumed session on its own last-selected model).
func (e *Engine) buildAgentCommand(agentName string, agentArgs, launchArgs []string, resume bool) (string, error) {
	if err := e.validateAgentName(agentName); err != nil {
		return "", err
	}
	info, _ := e.Config.ResolveAgent(agentName)
	parts := []string{info.Command}
	if resume && info.ResumeArgs != "" {
		parts = append(parts, strings.Fields(info.ResumeArgs)...)
	}
	parts = append(parts, quoteArgs(agentArgs)...)
	if !resume {
		parts = append(parts, quoteArgs(launchArgs)...)
	}
	return strings.Join(parts, " "), nil
}

// agentEnv returns the agent's configured environment as KEY=VALUE
// strings, sorted by key. Sorting keeps the tmux command line and the
// typed resume prefix byte-identical across runs — Go map iteration
// order is randomized, so without it two otherwise-identical launches
// would differ and assertions on them would flake.
//
// Values are expanded with config.ExpandPath so a leading ~ works in
// paths like CLAUDE_CONFIG_DIR. Unlike launch args, this applies on
// resume too: see the AgentInfo doc comment.
func (e *Engine) agentEnv(agentName string) []string {
	info, ok := e.Config.ResolveAgent(agentName)
	if !ok || len(info.Env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(info.Env))
	for k := range info.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+config.ExpandPath(info.Env[k]))
	}
	return env
}

// envPrefix renders env as an `env` command prefix, for the resume path
// where the command is typed into a live shell instead of being handed
// to tmux (which takes -e and needs no quoting).
//
// It emits `env K=V cmd` rather than the shorter `K=V cmd` because the
// bare form is a POSIX-shell assignment that fish rejects outright, and
// this line is typed into whatever interactive shell the pane happens to
// be running. Only the value is quoted: quoting the whole KEY=VALUE word
// would make the shell read it as a command name, not an assignment.
func envPrefix(env []string) string {
	parts := make([]string, 0, len(env))
	for _, kv := range env {
		key, value, found := strings.Cut(kv, "=")
		if !found || key == "" {
			continue
		}
		parts = append(parts, key+"="+quoteArgs([]string{value})[0])
	}
	if len(parts) == 0 {
		return ""
	}
	return "env " + strings.Join(parts, " ") + " "
}

// shellSafeRe matches args that need no quoting when joined into a
// shell command line. Conservative: anything outside this set gets
// single-quoted. Deliberately keeps ~ unquoted so leading-tilde paths
// still expand.
var shellSafeRe = regexp.MustCompile(`^[a-zA-Z0-9@%+=:,./_^~-]+$`)

// quoteArgs shell-quotes each configured arg as a single word. Args
// come from TOML lists where one element is one argument, so an
// element containing spaces or quotes (e.g. an initial prompt in
// launch_args) must survive the shell as one word.
func quoteArgs(args []string) []string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if a != "" && shellSafeRe.MatchString(a) {
			quoted[i] = a
			continue
		}
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return quoted
}
