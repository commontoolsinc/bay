// Package config handles loading and saving bay's configuration file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config represents the top-level bay configuration.
type Config struct {
	Agents  map[string]AgentConfig `toml:"agents"`
	Docks   map[string]DockConfig  `toml:"docks"`
	Editor  EditorConfig           `toml:"editor"`
	Monitor MonitorConfig          `toml:"monitor"`
}

// EditorConfig configures the editor launched by `bay edit`.
type EditorConfig struct {
	Command string `toml:"command"`
	GUI     *bool  `toml:"gui,omitempty"` // nil = auto-detect from command name
}

// AgentConfig defines an agent type.
type AgentConfig struct {
	Command     string `toml:"command"`
	ResumeArgs  string `toml:"resume_args,omitempty"`
	ProjectFile string `toml:"project_file,omitempty"`
}

// DockConfig holds optional per-dock overrides.
// Fields are only set when the user explicitly overrides manifest defaults.
type DockConfig struct {
	Agent     string   `toml:"agent"`
	AgentArgs []string `toml:"agent_args"`
	Terminal  string   `toml:"terminal,omitempty"`
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

// DefaultConfig returns an empty config with initialized maps.
func DefaultConfig() *Config {
	return &Config{
		Agents: make(map[string]AgentConfig),
		Docks:  make(map[string]DockConfig),
		Monitor: MonitorConfig{
			// Match the runtime default in EffectiveInterval so
			// `bay config show` on a fresh install displays the
			// interval bay actually uses (3s), not the Go zero
			// value. EffectiveInterval still substitutes 0→3 at
			// read time so users who hand-edit the file to 0 get
			// the default behavior.
			IntervalSeconds: 3,
		},
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
	for name, dock := range c.Docks {
		if dock.Agent != "" {
			if _, ok := c.Agents[dock.Agent]; !ok {
				errs = append(errs, fmt.Sprintf("dock %q references unknown agent %q", name, dock.Agent))
			}
		}
	}
	return errs
}

// ResolvedDockAgent returns the effective agent for a dock,
// checking config overrides first, then the manifest default.
func (c *Config) ResolvedDockAgent(dockName, manifestDefault string) string {
	if dc, ok := c.Docks[dockName]; ok && dc.Agent != "" {
		return dc.Agent
	}
	return manifestDefault
}

// ResolvedDockAgentArgs returns the effective agent args for a dock,
// checking config overrides first, then the manifest default.
func (c *Config) ResolvedDockAgentArgs(dockName string, manifestDefault []string) []string {
	if dc, ok := c.Docks[dockName]; ok && len(dc.AgentArgs) > 0 {
		return dc.AgentArgs
	}
	return manifestDefault
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
