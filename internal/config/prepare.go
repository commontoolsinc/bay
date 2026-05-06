package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
)

// BayPrepareConfig is one entry in a bay's prepare config. It can come
// from a repo-local .bay.toml at the checkout root or from a dock-level
// [[docks.<name>.bay_prepare]] block in the user's bay config. The two
// sources merge per-field by Name; see MergeBayPrepare. Design:
// docs/design/prepare.md.
type BayPrepareConfig struct {
	Name         string   `toml:"name"`
	Command      []string `toml:"command,omitempty"`
	ReadyCommand []string `toml:"ready_command,omitempty"`
	Blocks       []string `toml:"blocks,omitempty"`
	Run          string   `toml:"run,omitempty"`
	Timeout      string   `toml:"timeout,omitempty"` // duration string, e.g. "10m"
}

// ParsedTimeout parses the Timeout field. Returns (0, nil) when unset.
func (c BayPrepareConfig) ParsedTimeout() (time.Duration, error) {
	if c.Timeout == "" {
		return 0, nil
	}
	return time.ParseDuration(c.Timeout)
}

// RepoLocalConfig is the parsed form of a repo's <root>/.bay.toml.
// The schema mirrors the dock-level fields exposed today; expansion for
// future lifecycle features (close checks, etc.) is additive.
type RepoLocalConfig struct {
	BayPrepare []BayPrepareConfig `toml:"bay_prepare,omitempty"`
}

// RepoLocalPath returns the canonical .bay.toml path for a checkout root.
func RepoLocalPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".bay.toml")
}

// LoadRepoLocal reads .bay.toml from the given checkout root. Returns
// nil silently if the file is absent. Returns an error for I/O or parse
// failures.
//
// This is read live on every bay operation that wants the effective
// prepare config. No caching, no copy into user config — design intent
// is that updating .bay.toml in the repo automatically takes effect on
// the next bay command without any user intervention.
func LoadRepoLocal(repoRoot string) (*RepoLocalConfig, error) {
	path := RepoLocalPath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
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

// MergeBayPrepare combines repo-local and dock-level prepare entries
// into the effective config. Entries with the same Name across sources
// merge per-field with the dock-level value winning where set; the
// repo-local entry's fields fill the rest. Entries appear in
// repo-local declared order followed by dock-level-only entries (those
// that don't shadow a repo-local entry), in dock-level declared order.
// Duplicate names within a single source are not deduplicated here —
// that's a validation concern. See ValidatePrepare.
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

// KnownBlockClasses lists the surface classes a prepare step's `blocks`
// field may reference in v1. Other classes (e.g. "editor", named apps)
// can be added when the surface launch path supports them.
var KnownBlockClasses = map[string]bool{
	"agent": true,
	"cmd":   true,
}

// ValidatePrepare checks both per-source rules (name uniqueness within
// a single source) and effective-config rules (command non-empty, blocks
// valid, ready_command rules, timeout parseable). Returns a slice of
// error strings; an empty slice means valid.
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

// validatePrepareSource enforces rules that must hold within a single
// configuration source: every entry has a non-empty name and names are
// unique within the source. Same-name entries across sources are an
// intentional merge, not an error.
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

// validatePrepareEffective enforces rules that must hold against the
// merged effective config (after repo-local + dock-level overrides
// apply): command must be non-empty, blocks must reference known
// surface classes, ready_command must be present and non-empty when
// blocks is non-empty, timeout must parse as a positive duration when
// set, and run must be either unset or "auto" in v1.
func validatePrepareEffective(merged []BayPrepareConfig) []string {
	var errs []string
	for _, e := range merged {
		if len(e.Command) == 0 {
			errs = append(errs, fmt.Sprintf("bay_prepare entry %q: command is required (set in .bay.toml or dock-level config)", e.Name))
		}
		// Sort blocks for stable error output even though slice order
		// shouldn't matter to the user.
		blocks := append([]string(nil), e.Blocks...)
		sort.Strings(blocks)
		for _, cls := range blocks {
			if !KnownBlockClasses[cls] {
				known := knownBlockClassesSorted()
				errs = append(errs, fmt.Sprintf("bay_prepare entry %q: blocks contains unknown surface class %q (known: %v)", e.Name, cls, known))
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
		if e.Run != "" && e.Run != "auto" {
			errs = append(errs, fmt.Sprintf("bay_prepare entry %q: run = %q not supported in v1 (use \"auto\" or omit)", e.Name, e.Run))
		}
	}
	return errs
}

func knownBlockClassesSorted() []string {
	out := make([]string, 0, len(KnownBlockClasses))
	for k := range KnownBlockClasses {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ResolveTrust returns whether the given dock should honor its
// checkout's repo-local .bay.toml. Resolution order: dock-level
// explicit value, then bay-level explicit value, then built-in default
// false. nil pointers represent "field unset" and fall through.
func (c *Config) ResolveTrust(dockName string) bool {
	if dc, ok := c.Docks[dockName]; ok && dc.TrustRepoBayToml != nil {
		return *dc.TrustRepoBayToml
	}
	if c.TrustRepoBayToml != nil {
		return *c.TrustRepoBayToml
	}
	return false
}
