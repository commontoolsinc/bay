package config

import "path/filepath"

// Paths holds all standard bay file paths, computed once.
type Paths struct {
	ConfigDir    string
	DataDir      string
	ConfigFile   string
	ManifestFile string
	ArchiveFile  string
	PatternsFile string
	PIDFile      string
}

// DefaultPaths returns the standard paths using XDG defaults.
func DefaultPaths() Paths {
	configDir := DefaultConfigDir()
	dataDir := DefaultDataDir()
	return Paths{
		ConfigDir:    configDir,
		DataDir:      dataDir,
		ConfigFile:   DefaultConfigPath(),
		ManifestFile: filepath.Join(dataDir, "manifest.toml"),
		ArchiveFile:  filepath.Join(dataDir, "archive.toml"),
		PatternsFile: filepath.Join(configDir, "bay-prompts.txt"),
		PIDFile:      filepath.Join(dataDir, "monitor.pid"),
	}
}
