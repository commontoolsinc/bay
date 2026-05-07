package config

// ResolveTrust reports whether dockName trusts its checkout's .bay.toml.
func ResolveTrust(cfg *Config, dockName string) bool {
	if cfg == nil {
		return false
	}
	if dock, ok := cfg.Docks[dockName]; ok && dock.TrustRepoBayToml != nil {
		return *dock.TrustRepoBayToml
	}
	if cfg.TrustRepoBayToml != nil {
		return *cfg.TrustRepoBayToml
	}
	return false
}
