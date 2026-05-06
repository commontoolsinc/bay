package config

// Merge returns the effective prepare steps after applying dock-level
// per-field overrides on top of repo-local defaults.
func Merge(repoLocal, dockLevel []BayPrepareConfig) []BayPrepareConfig {
	return MergePrepare(repoLocal, dockLevel)
}

// MergePrepare returns the effective prepare steps after applying dock-level
// per-field overrides on top of repo-local defaults.
func MergePrepare(repoLocal, dockLevel []BayPrepareConfig) []BayPrepareConfig {
	merged := make([]BayPrepareConfig, 0, len(repoLocal)+len(dockLevel))
	index := map[string]int{}

	for _, step := range repoLocal {
		copied := copyPrepareStep(step)
		merged = append(merged, copied)
		if copied.Name != "" {
			index[copied.Name] = len(merged) - 1
		}
	}

	for _, override := range dockLevel {
		if i, ok := index[override.Name]; ok && override.Name != "" {
			merged[i] = mergePrepareStep(merged[i], override)
			continue
		}
		copied := copyPrepareStep(override)
		merged = append(merged, copied)
		if copied.Name != "" {
			index[copied.Name] = len(merged) - 1
		}
	}

	return merged
}

func mergePrepareStep(base, override BayPrepareConfig) BayPrepareConfig {
	out := copyPrepareStep(base)
	if prepareCommandPresent(override) {
		out.Command = cloneStringSlice(override.Command)
		out.fields.Command = true
	}
	if prepareReadyCommandPresent(override) {
		out.ReadyCommand = cloneStringSlice(override.ReadyCommand)
		out.fields.ReadyCommand = true
	}
	if prepareBlocksPresent(override) {
		out.Blocks = cloneStringSlice(override.Blocks)
		out.fields.Blocks = true
	}
	if prepareRunPresent(override) {
		out.Run = override.Run
		out.fields.Run = true
	}
	if prepareTimeoutPresent(override) {
		out.Timeout = override.Timeout
		out.fields.Timeout = true
	}
	return out
}

func copyPrepareStep(step BayPrepareConfig) BayPrepareConfig {
	step.Command = cloneStringSlice(step.Command)
	step.ReadyCommand = cloneStringSlice(step.ReadyCommand)
	step.Blocks = cloneStringSlice(step.Blocks)
	return step
}

func cloneStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
