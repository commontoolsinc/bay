package config

import "path/filepath"

// Paths holds all standard bay file paths, computed once.
type Paths struct {
	ConfigDir      string
	DataDir        string
	ConfigFile     string
	ManifestFile   string
	ArchiveFile    string
	PatternsFile   string
	PIDFile        string
	MonitorStatus  string
	PaletteRecents string
	CloseConfirm   string
}

// DefaultPaths returns the standard paths using XDG defaults.
func DefaultPaths() Paths {
	configDir := DefaultConfigDir()
	dataDir := DefaultDataDir()
	return Paths{
		ConfigDir:      configDir,
		DataDir:        dataDir,
		ConfigFile:     DefaultConfigPath(),
		ManifestFile:   filepath.Join(dataDir, "manifest.json"),
		ArchiveFile:    filepath.Join(dataDir, "archive.json"),
		PatternsFile:   filepath.Join(configDir, "waiting-patterns.txt"),
		PIDFile:        filepath.Join(dataDir, "monitor.pid"),
		MonitorStatus:  filepath.Join(dataDir, "monitor-status.json"),
		PaletteRecents: filepath.Join(dataDir, "palette-recents.json"),
		CloseConfirm:   filepath.Join(dataDir, "close-confirm"),
	}
}
