package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run health checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, err := newEngine()
			if err != nil {
				return fmt.Errorf("config: %v", err)
			}

			ok := true

			// Check tmux installed
			if _, err := exec.LookPath("tmux"); err != nil {
				fmt.Println("[WARN] tmux not found in PATH")
				ok = false
			} else {
				fmt.Println("[OK] tmux installed")
			}

			// Check fzf installed (optional)
			if _, err := exec.LookPath("fzf"); err != nil {
				fmt.Println("[INFO] fzf not found in PATH (optional, used by bay go)")
			} else {
				fmt.Println("[OK] fzf installed")
			}

			// Check configured editor
			if eng.Config.Editor.Command != "" {
				if _, err := exec.LookPath(eng.Config.Editor.Command); err != nil {
					fmt.Printf("[WARN] configured editor %q not found in PATH\n", eng.Config.Editor.Command)
					ok = false
				} else {
					fmt.Printf("[OK] editor %q available\n", eng.Config.Editor.Command)
				}
			}

			// Check config validation
			errs := eng.Config.Validate()
			if len(errs) > 0 {
				for _, e := range errs {
					fmt.Printf("[WARN] config: %s\n", e)
				}
				ok = false
			} else {
				fmt.Println("[OK] config valid")
			}

			// Check repo paths exist
			for name, repo := range eng.Config.Repos {
				path := config.ExpandPath(repo.Path)
				if _, err := os.Stat(path); err != nil {
					fmt.Printf("[WARN] repo %q path %s not accessible\n", name, path)
					ok = false
				} else {
					fmt.Printf("[OK] repo %q accessible\n", name)
				}
			}



			// Check workspace paths in manifest
			m, loadErr := eng.LoadManifest()
			if loadErr != nil {
				fmt.Printf("[WARN] could not load manifest: %v\n", loadErr)
				ok = false
			} else {
				missingCount := 0
				for i := range m.Docks {
					dock := &m.Docks[i]
					for j := range dock.Workspaces {
						ws := &dock.Workspaces[j]
						if ws.Path == "" {
							continue
						}
						wsPath := config.ExpandPath(ws.Path)
						if _, err := os.Stat(wsPath); err != nil {
							fmt.Printf("[WARN] workspace %s:%s path missing: %s\n", dock.Name, ws.Name, wsPath)
							missingCount++
							ok = false
						}
					}
				}
				if missingCount == 0 {
					fmt.Println("[OK] all workspace paths exist")
				}

				manifestWarnings := checkManifestConsistency(m, eng.Config)
				if len(manifestWarnings) > 0 {
					for _, warning := range manifestWarnings {
						fmt.Printf("[WARN] manifest: %s\n", warning)
					}
					ok = false
				} else {
					fmt.Println("[OK] manifest consistent")
				}
			}

			// Check tmux keybindings
			tmuxConfPath := tmuxConfPath()
			if data, err := os.ReadFile(tmuxConfPath); err != nil {
				fmt.Printf("[WARN] tmux keybindings not installed (%s not readable)\n", tmuxConfPath)
				ok = false
			} else {
				missing := missingKeybindings(string(data))
				if len(missing) > 0 {
					fmt.Printf("[WARN] tmux keybindings missing: %s\n", strings.Join(missing, ", "))
					ok = false
				} else {
					fmt.Println("[OK] tmux keybindings installed")
				}
			}

			// Check monitor
			mon, monCfgErr := newMonitorWithConfig()
			if monCfgErr != nil {
				fmt.Printf("[WARN] monitor: %v\n", monCfgErr)
				ok = false
			} else {
				running, pid, monErr := mon.Status()
				if monErr != nil || !running {
					fmt.Printf("[WARN] monitor not running\n")
					ok = false
				} else {
					fmt.Printf("[OK] monitor running (pid %d)\n", pid)
				}
			}

			if !ok {
				fmt.Println("\nSome checks failed. Run `bay setup` to fix.")
			} else {
				fmt.Println("\nAll checks passed.")
			}

			return nil
		},
	}
}

func missingKeybindings(content string) []string {
	var missing []string
	for _, line := range tmuxKeybindingLines() {
		if !strings.Contains(content, line) {
			missing = append(missing, line)
		}
	}
	return missing
}

func tmuxKeybindingLines() []string {
	var lines []string
	for _, kb := range bayKeybindings {
		lines = append(lines, fmt.Sprintf("bind-key -n %s %s '%s'", kb.key, kb.tmuxVerb, kb.cmd))
	}
	return lines
}

func tmuxConfPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tmux.conf")
}

func checkManifestConsistency(m *manifest.Manifest, cfg *config.Config) []string {
	var warnings []string

	for i := range m.Docks {
		dock := &m.Docks[i]
		seenNames := map[string]bool{}
		for j := range dock.Workspaces {
			ws := &dock.Workspaces[j]
			if ws.Name != "" {
				if seenNames[ws.Name] {
					warnings = append(warnings, fmt.Sprintf("dock %s has duplicate workspace name %q", dock.Name, ws.Name))
				}
				seenNames[ws.Name] = true
			}

			seenSurfaceIDs := map[int]bool{}
			seenSurfaceNames := map[string]bool{}
			for _, s := range ws.Surfaces {
				if seenSurfaceIDs[s.ID] {
					warnings = append(warnings, fmt.Sprintf("workspace %s:%s has duplicate surface id %d", dock.Name, ws.Name, s.ID))
				}
				seenSurfaceIDs[s.ID] = true
				if seenSurfaceNames[s.Name] {
					warnings = append(warnings, fmt.Sprintf("workspace %s:%s has duplicate surface name %q", dock.Name, ws.Name, s.Name))
				}
				seenSurfaceNames[s.Name] = true
				for _, e := range s.Validate() {
					warnings = append(warnings, fmt.Sprintf("workspace %s:%s: %s", dock.Name, ws.Name, e))
				}
			}
		}
	}

	return warnings
}
