package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const RepoLocalFilename = ".bay.toml"

// RepoLocalConfig is the checked-in prepare config loaded from .bay.toml.
type RepoLocalConfig struct {
	BayPrepare []BayPrepareConfig `toml:"bay_prepare,omitempty"`
}

// LoadRepoLocal reads .bay.toml from checkoutRoot. A missing file is not an
// error; callers receive nil so unconfigured repos behave like today.
func LoadRepoLocal(checkoutRoot string) (*RepoLocalConfig, error) {
	path := filepath.Join(checkoutRoot, RepoLocalFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading repo-local config: %w", err)
	}
	cfg, err := ParseRepoLocal(string(data))
	if err != nil {
		return nil, fmt.Errorf("parsing repo-local config: %w", err)
	}
	return cfg, nil
}

// ParseRepoLocal parses .bay.toml contents.
func ParseRepoLocal(data string) (*RepoLocalConfig, error) {
	var cfg RepoLocalConfig
	if _, err := toml.Decode(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
