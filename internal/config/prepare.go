package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/BurntSushi/toml"
)

// BayPrepareConfig is one entry in a bay's prepare config. Sourced from
// either repo-local .bay.toml or dock-level [[docks.<name>.bay_prepare]]
// blocks; the two sources merge per-field by Name (see MergeBayPrepare).
// Design: docs/design/prepare.md.
type BayPrepareConfig struct {
	Name         string   `toml:"name"`
	Command      []string `toml:"command,omitempty"`
	ReadyCommand []string `toml:"ready_command,omitempty"`
	Blocks       []string `toml:"blocks,omitempty"`
	Run          string   `toml:"run,omitempty"`
	Timeout      string   `toml:"timeout,omitempty"` // duration string, e.g. "10m"
}

// PrepareRunAuto is the only Run value v1 accepts; missing is treated
// as auto.
const PrepareRunAuto = "auto"

// ParsedTimeout parses Timeout. Returns (0, nil) when unset.
func (c BayPrepareConfig) ParsedTimeout() (time.Duration, error) {
	if c.Timeout == "" {
		return 0, nil
	}
	return time.ParseDuration(c.Timeout)
}

// RepoLocalConfig is the parsed form of a repo's <root>/.bay.toml.
// Schema is additive — future lifecycle features (close checks, etc.)
// will land as new fields here.
type RepoLocalConfig struct {
	BayPrepare []BayPrepareConfig `toml:"bay_prepare,omitempty"`
}

// RepoLocalPath returns the canonical .bay.toml path for a checkout root.
func RepoLocalPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".bay.toml")
}

// LoadRepoLocal reads <repoRoot>/.bay.toml. Returns nil silently when
// absent. Read live on every bay operation; no caching, so repo updates
// take effect on the next bay command.
func LoadRepoLocal(repoRoot string) (*RepoLocalConfig, error) {
	path := RepoLocalPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var rlc RepoLocalConfig
	if _, err := toml.Decode(string(data), &rlc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &rlc, nil
}

// MergeBayPrepare combines repo-local and dock-level entries into the
// effective config. Same-Name entries merge per field, dock-level
// winning. Repo-local order, then dock-only entries in declared order.
// Per-source name uniqueness is a ValidatePrepare concern, not enforced
// here.
func MergeBayPrepare(repoLocal, dockLevel []BayPrepareConfig) []BayPrepareConfig {
	dockByName := make(map[string]BayPrepareConfig, len(dockLevel))
	for _, d := range dockLevel {
		dockByName[d.Name] = d
	}
	seen := make(map[string]bool, len(repoLocal))
	merged := make([]BayPrepareConfig, 0, len(repoLocal)+len(dockLevel))
	for _, r := range repoLocal {
		seen[r.Name] = true
		if d, ok := dockByName[r.Name]; ok {
			r = applyOverrides(r, d)
		}
		merged = append(merged, r)
	}
	for _, d := range dockLevel {
		if !seen[d.Name] {
			merged = append(merged, d)
		}
	}
	return merged
}

func applyOverrides(base, override BayPrepareConfig) BayPrepareConfig {
	if len(override.Command) > 0 {
		base.Command = override.Command
	}
	if len(override.ReadyCommand) > 0 {
		base.ReadyCommand = override.ReadyCommand
	}
	if len(override.Blocks) > 0 {
		base.Blocks = override.Blocks
	}
	if override.Run != "" {
		base.Run = override.Run
	}
	if override.Timeout != "" {
		base.Timeout = override.Timeout
	}
	return base
}

// knownBlockClasses are the surface classes a prepare step's `blocks`
// field may reference. These strings must stay aligned with
// manifest.SurfaceTypeAgent / SurfaceTypeCmd; importing manifest here
// would close the cycle, so they are duplicated by design. Listed in
// canonical order for stable error output.
var knownBlockClasses = []string{"agent", "cmd"}

// IsKnownBlockClass reports whether s is a surface class the prepare
// runner recognizes today.
func IsKnownBlockClass(s string) bool {
	return slices.Contains(knownBlockClasses, s)
}

// ValidatePrepare checks per-source rules (name uniqueness within a
// single source) and effective-config rules (command non-empty, blocks
// valid, ready_command rules, timeout parseable, run = "auto" or
// unset). Returns a slice of error strings; empty means valid.
func ValidatePrepare(repoLocal, dockLevel []BayPrepareConfig) []string {
	var errs []string
	errs = append(errs, validatePrepareSource(repoLocal, ".bay.toml")...)
	errs = append(errs, validatePrepareSource(dockLevel, "dock config")...)
	if len(errs) > 0 {
		return errs
	}
	merged := MergeBayPrepare(repoLocal, dockLevel)
	errs = append(errs, validatePrepareEffective(merged)...)
	return errs
}

// validatePrepareSource: name presence and per-source uniqueness.
// Same-name across sources is the intentional merge semantics.
func validatePrepareSource(entries []BayPrepareConfig, sourceLabel string) []string {
	var errs []string
	seen := make(map[string]int, len(entries))
	for i, e := range entries {
		if e.Name == "" {
			errs = append(errs, fmt.Sprintf("%s: bay_prepare[%d] is missing a name", sourceLabel, i))
			continue
		}
		if prev, ok := seen[e.Name]; ok {
			errs = append(errs, fmt.Sprintf("%s: bay_prepare entry %q appears at indexes %d and %d (names must be unique within a source)", sourceLabel, e.Name, prev, i))
			continue
		}
		seen[e.Name] = i
	}
	return errs
}

func validatePrepareEffective(merged []BayPrepareConfig) []string {
	var errs []string
	for _, e := range merged {
		if len(e.Command) == 0 {
			errs = append(errs, fmt.Sprintf("bay_prepare entry %q: command is required (set in .bay.toml or dock-level config)", e.Name))
		}
		for _, cls := range e.Blocks {
			if !IsKnownBlockClass(cls) {
				errs = append(errs, fmt.Sprintf("bay_prepare entry %q: blocks contains unknown surface class %q (known: %v)", e.Name, cls, knownBlockClasses))
			}
		}
		if e.ReadyCommand != nil && len(e.ReadyCommand) == 0 {
			errs = append(errs, fmt.Sprintf("bay_prepare entry %q: ready_command must be a non-empty argv array when set", e.Name))
		}
		if len(e.Blocks) > 0 && len(e.ReadyCommand) == 0 {
			errs = append(errs, fmt.Sprintf("bay_prepare entry %q: ready_command is required when blocks is non-empty", e.Name))
		}
		if e.Timeout != "" {
			d, err := time.ParseDuration(e.Timeout)
			if err != nil {
				errs = append(errs, fmt.Sprintf("bay_prepare entry %q: timeout %q is not a valid duration: %v", e.Name, e.Timeout, err))
			} else if d <= 0 {
				errs = append(errs, fmt.Sprintf("bay_prepare entry %q: timeout %q must be positive", e.Name, e.Timeout))
			}
		}
		if e.Run != "" && e.Run != PrepareRunAuto {
			errs = append(errs, fmt.Sprintf("bay_prepare entry %q: run = %q not supported in v1 (use %q or omit)", e.Name, e.Run, PrepareRunAuto))
		}
	}
	return errs
}

// ResolveTrust returns whether the given dock should honor its
// checkout's repo-local .bay.toml. Order: dock-level explicit, then
// bay-level explicit, then built-in default false. nil pointers fall
// through.
func (c *Config) ResolveTrust(dockName string) bool {
	if dc, ok := c.Docks[dockName]; ok && dc.TrustRepoBayToml != nil {
		return *dc.TrustRepoBayToml
	}
	if c.TrustRepoBayToml != nil {
		return *c.TrustRepoBayToml
	}
	return false
}
