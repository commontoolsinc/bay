// Package config handles loading and saving bay's configuration file.
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config represents the top-level bay configuration.
type Config struct {
	DefaultAgent     string                  `toml:"default_agent,omitempty"`
	DefaultEditor    string                  `toml:"default_editor,omitempty"`
	TrustRepoBayToml *bool                   `toml:"trust_repo_bay_toml,omitempty"`
	Agents           map[string]AgentConfig  `toml:"agents,omitempty"`
	Editors          map[string]EditorConfig `toml:"editors,omitempty"`
	Docks            map[string]DockConfig   `toml:"docks,omitempty"`
	Monitor          MonitorConfig           `toml:"monitor,omitempty"`
	Describe         DescribeConfig          `toml:"describe,omitempty"`
}

// EditorConfig allows marking a custom editor as GUI.
type EditorConfig struct {
	GUI bool `toml:"gui"`
}

// AgentConfig allows overriding built-in agent defaults.
// Most users won't need this — the built-in registry covers claude,
// codex, and antigravity. Use this for custom agents or to override
// resume_args / project_file for a known agent.
type AgentConfig struct {
	Command     string   `toml:"command"`
	Args        []string `toml:"args,omitempty"`
	ResumeArgs  string   `toml:"resume_args,omitempty"`
	ProjectFile string   `toml:"project_file,omitempty"`
}

// AgentInfo describes a known or configured agent.
type AgentInfo struct {
	Command     string
	Args        []string
	ResumeArgs  string
	ProjectFile string
}

// KnownAgents are built-in agent definitions, similar to how editors
// have a built-in GUI detection list. These are used when no config
// override exists for the agent.
var KnownAgents = map[string]AgentInfo{
	"claude":      {Command: "claude", ResumeArgs: "--continue", ProjectFile: "CLAUDE.local.md"},
	"codex":       {Command: "codex", ResumeArgs: "resume --last"},
	"antigravity": {Command: "agy", ResumeArgs: "--continue"},
}

// AgentAliases preserves compatibility with manifests/configs created
// before a built-in agent was renamed or replaced.
var AgentAliases = map[string]string{
	"gemini": "antigravity",
}

var agentAliasLegacyCommands = map[string]string{
	"gemini": "gemini",
}

var knownAgentProbeOrder = []string{"claude", "codex", "antigravity"}

// CanonicalAgentName returns the preferred built-in name for name.
func CanonicalAgentName(name string) string {
	if canonical, ok := AgentAliases[name]; ok {
		return canonical
	}
	return name
}

// KnownAgentNames returns sorted canonical built-in agent names.
func KnownAgentNames() []string {
	names := make([]string, 0, len(KnownAgents))
	for name := range KnownAgents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ResolveAgent returns the effective AgentInfo for a named agent,
// checking config overrides first, then built-in defaults.
func (c *Config) ResolveAgent(name string) (AgentInfo, bool) {
	canonical := CanonicalAgentName(name)
	if ac, ok := c.Agents[name]; ok {
		if canonical != name && aliasConfigUsesLegacyDefault(name, ac) {
			info := c.agentInfoFromConfig(canonical, AgentConfig{
				Args:        ac.Args,
				ProjectFile: ac.ProjectFile,
			})
			return info, info.Command != ""
		}
		info := c.agentInfoFromConfig(name, ac)
		return info, info.Command != ""
	}
	if canonical != name {
		if info, ok := c.resolveConfiguredAgent(canonical); ok {
			return info, info.Command != ""
		}
	}
	if builtin, ok := KnownAgents[canonical]; ok {
		return builtin, builtin.Command != ""
	}
	return AgentInfo{}, false
}

func (c *Config) resolveConfiguredAgent(name string) (AgentInfo, bool) {
	if ac, ok := c.Agents[name]; ok {
		info := c.agentInfoFromConfig(name, ac)
		return info, info.Command != ""
	}
	return AgentInfo{}, false
}

func (c *Config) agentInfoFromConfig(name string, ac AgentConfig) AgentInfo {
	info := AgentInfo(ac)
	// Fill in gaps from built-in if the config only partially overrides.
	if builtin, ok := KnownAgents[CanonicalAgentName(name)]; ok {
		if info.Command == "" {
			info.Command = builtin.Command
		}
		if len(info.Args) == 0 {
			info.Args = builtin.Args
		}
		if info.ResumeArgs == "" {
			info.ResumeArgs = builtin.ResumeArgs
		}
		if info.ProjectFile == "" {
			info.ProjectFile = builtin.ProjectFile
		}
	}
	return info
}

func aliasConfigUsesLegacyDefault(alias string, ac AgentConfig) bool {
	legacyCommand, ok := agentAliasLegacyCommands[alias]
	if !ok {
		return ac.Command == ""
	}
	return ac.Command == "" || (ac.Command == legacyCommand && ac.ResumeArgs == "")
}

// DockConfig holds optional per-dock overrides.
// Fields are only set when the user explicitly overrides manifest defaults.
type DockConfig struct {
	Agent            string              `toml:"agent"`
	AgentArgs        map[string][]string `toml:"agent_args"`
	Terminal         string              `toml:"terminal,omitempty"`
	TrustRepoBayToml *bool               `toml:"trust_repo_bay_toml,omitempty"`
	BayPrepare       []BayPrepareConfig  `toml:"bay_prepare,omitempty"`
}

// BayPrepareConfig describes one configured prepare step.
type BayPrepareConfig struct {
	Name         string   `toml:"name"`
	Command      []string `toml:"command,omitempty"`
	ReadyCommand []string `toml:"ready_command,omitempty"`
	Blocks       []string `toml:"blocks,omitempty"`
	Run          string   `toml:"run,omitempty"`
	Timeout      string   `toml:"timeout,omitempty"`

	fields bayPrepareFields
}

type bayPrepareFields struct {
	Name         bool
	Command      bool
	ReadyCommand bool
	Blocks       bool
	Run          bool
	Timeout      bool
}

// MonitorConfig configures the pane monitor.
type MonitorConfig struct {
	IntervalSeconds int `toml:"interval_seconds"`
}

// EffectiveInterval returns the monitor interval, defaulting to 3 seconds.
func (m MonitorConfig) EffectiveInterval() int {
	if m.IntervalSeconds <= 0 {
		return 3
	}
	return m.IntervalSeconds
}

// DescribeConfig configures the auto-description backstop, which
// summarizes a bay's agent conversation into its description on a slow
// background cadence. See docs/design/auto-descriptions.md.
type DescribeConfig struct {
	// Enabled toggles the backstop. Pointer so an absent key is
	// distinguishable from an explicit false. Default is OFF: the
	// backstop spends model calls and assumes a configured summarizer
	// CLI, so it's opt-in (via this key or `bay setup`).
	Enabled *bool `toml:"enabled,omitempty"`
	// Command is the summarizer argv. The full prompt is appended as the
	// final argument (or substituted for a "{prompt}" element). If an
	// element contains "{out}", bay substitutes a temp file path and
	// reads the answer from it; otherwise it reads stdout. This lets any
	// CLI work — set the model by editing the command. Empty uses the
	// default (codex, cheap model, read-only, low reasoning effort).
	Command []string `toml:"command,omitempty"`
}

// DefaultDescribeCommand is the summarizer argv used when none is
// configured: codex exec (non-interactive) at a cheap model and low
// reasoning effort, sandboxed read-only with session persistence off,
// writing only the final message to {out} (codex's stdout carries
// chatter).
var DefaultDescribeCommand = []string{
	"codex", "exec",
	"--sandbox", "read-only",
	"--skip-git-repo-check",
	"--ephemeral",
	"-c", "model_reasoning_effort=low",
	"-m", "gpt-5.4-mini",
	"-o", "{out}",
}

// EffectiveEnabled reports whether the backstop is on. Default is OFF
// (opt-in) so bay never spends model calls a user didn't ask for.
func (d DescribeConfig) EffectiveEnabled() bool {
	if d.Enabled == nil {
		return false
	}
	return *d.Enabled
}

// EffectiveCommand returns the summarizer argv (a copy), defaulting to
// DefaultDescribeCommand when unset.
func (d DescribeConfig) EffectiveCommand() []string {
	src := d.Command
	if len(src) == 0 {
		src = DefaultDescribeCommand
	}
	return append([]string(nil), src...)
}

// DefaultConfig returns an empty config with initialized maps.
func DefaultConfig() *Config {
	return &Config{
		Agents:  make(map[string]AgentConfig),
		Editors: make(map[string]EditorConfig),
		Docks:   make(map[string]DockConfig),
	}
}

// DefaultConfigDir returns the default config directory.
func DefaultConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "bay")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "bay")
}

// DefaultDataDir returns the default data directory.
func DefaultDataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "bay")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "bay")
}

// DefaultConfigPath returns the default path to config.toml.
func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "config.toml")
}

// Load reads and parses a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	return Parse(string(data))
}

// Parse parses TOML config data.
func Parse(data string) (*Config, error) {
	var cfg Config
	if _, err := toml.Decode(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]AgentConfig)
	}
	if cfg.Editors == nil {
		cfg.Editors = make(map[string]EditorConfig)
	}
	if cfg.Docks == nil {
		cfg.Docks = make(map[string]DockConfig)
	}
	return &cfg, nil
}

// Save writes the config to a file.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating config file: %w", err)
	}
	defer f.Close()
	enc := toml.NewEncoder(f)
	return enc.Encode(cfg)
}

// Validate checks the config for errors.
func (c *Config) Validate() []string {
	var errs []string
	if c.DefaultAgent != "" {
		if _, ok := c.ResolveAgent(c.DefaultAgent); !ok {
			errs = append(errs, fmt.Sprintf("default_agent %q is not a known or configured agent", c.DefaultAgent))
		}
	}
	for name, dock := range c.Docks {
		if dock.Agent != "" {
			if _, ok := c.ResolveAgent(dock.Agent); !ok {
				errs = append(errs, fmt.Sprintf("dock %q references unknown agent %q", name, dock.Agent))
			}
		}
		source := fmt.Sprintf("docks.%s.bay_prepare", name)
		errs = append(errs, ValidatePrepareSource(source, dock.BayPrepare)...)
	}
	return errs
}

// ResolvedDockAgent returns the effective agent for a dock.
// Resolution order: per-dock config override → manifest dock default →
// global default_agent in config → probe PATH.
func (c *Config) ResolvedDockAgent(dockName, manifestDefault string) string {
	if dc, ok := c.Docks[dockName]; ok && dc.Agent != "" {
		return dc.Agent
	}
	if manifestDefault != "" {
		return manifestDefault
	}
	if c.DefaultAgent != "" {
		return c.DefaultAgent
	}
	return ProbeAgent()
}

// ProbeAgent checks PATH for known agents and returns the first found.
func ProbeAgent() string {
	seen := map[string]bool{}
	for _, name := range knownAgentProbeOrder {
		seen[name] = true
		if probeKnownAgent(name) {
			return name
		}
	}
	for _, name := range KnownAgentNames() {
		if seen[name] {
			continue
		}
		if probeKnownAgent(name) {
			return name
		}
	}
	return ""
}

func probeKnownAgent(name string) bool {
	info, ok := KnownAgents[name]
	if !ok || info.Command == "" {
		return false
	}
	_, err := exec.LookPath(info.Command)
	return err == nil
}

// ResolvedAgentArgs returns the effective args for an agent in a dock.
// Resolution order: per-dock agent_args[agent] → manifest dock agent_args[agent]
// → global agent args → nothing.
func (c *Config) ResolvedAgentArgs(dockName, agentName string, manifestDefault map[string][]string) []string {
	if dc, ok := c.Docks[dockName]; ok {
		if args := agentArgsFor(dc.AgentArgs, agentName); len(args) > 0 {
			return args
		}
	}
	if args := agentArgsFor(manifestDefault, agentName); len(args) > 0 {
		return args
	}
	info, _ := c.ResolveAgent(agentName)
	return info.Args
}

func agentArgsFor(argsByAgent map[string][]string, agentName string) []string {
	for _, name := range agentLookupNames(agentName) {
		if args, ok := argsByAgent[name]; ok && len(args) > 0 {
			return args
		}
	}
	return nil
}

func agentLookupNames(agentName string) []string {
	names := []string{agentName}
	canonical := CanonicalAgentName(agentName)
	if canonical != agentName {
		names = append(names, canonical)
	}
	var aliases []string
	for alias, target := range AgentAliases {
		if target == canonical && alias != agentName {
			aliases = append(aliases, alias)
		}
	}
	sort.Strings(aliases)
	return append(names, aliases...)
}

// ResolvedDockTerminal returns the effective terminal for a dock.
func (c *Config) ResolvedDockTerminal(dockName string) string {
	if dc, ok := c.Docks[dockName]; ok {
		return dc.Terminal
	}
	return ""
}

// ExpandPath expands ~ to the user's home directory.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// CanonicalPath expands ~ and resolves symlinks. Falls back to the
// expanded form if EvalSymlinks fails (e.g. the path doesn't exist), so
// callers can still compare nonexistent paths for string equality —
// though that comparison won't bridge symlink representations. Needed on
// macOS where /var → /private/var, /tmp → /private/tmp, etc.
func CanonicalPath(path string) string {
	expanded := ExpandPath(path)
	if resolved, err := filepath.EvalSymlinks(expanded); err == nil {
		return resolved
	}
	return expanded
}

// IsPathUnder returns true if child's canonical form equals parent's, or
// has parent's as a directory prefix. Both paths are canonicalized via
// CanonicalPath, so symlink indirection on either side is handled.
// Returns false if either input is empty.
func IsPathUnder(child, parent string) bool {
	if child == "" || parent == "" {
		return false
	}
	c := CanonicalPath(child)
	p := CanonicalPath(parent)
	if c == p {
		return true
	}
	sep := string(filepath.Separator)
	// Filesystem root: anything non-empty under it qualifies. Without
	// this special case, p+sep would be "//" and never match.
	if p == sep {
		return strings.HasPrefix(c, sep)
	}
	return strings.HasPrefix(c, p+sep)
}

// NormalizePath converts a path to an absolute form suitable for config storage.
// Relative paths are resolved to absolute. Paths under the user's home directory
// are shortened to use ~/. This ensures paths work from any working directory.
func NormalizePath(path string) string {
	// Expand ~ first
	expanded := ExpandPath(path)

	// Make absolute
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return expanded
	}

	// Shorten to ~/ if under home
	home, err := os.UserHomeDir()
	if err != nil {
		return abs
	}
	if strings.HasPrefix(abs, home+"/") {
		return "~/" + abs[len(home)+1:]
	}

	return abs
}
