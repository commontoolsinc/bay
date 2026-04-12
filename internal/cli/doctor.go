package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/focus"
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

			// Check agents on PATH
			for name, agent := range eng.Config.Agents {
				if _, err := exec.LookPath(agent.Command); err != nil {
					fmt.Printf("[INFO] agent %q (%s) not found in PATH\n", name, agent.Command)
				} else {
					fmt.Printf("[OK] agent %q available\n", name)
				}
			}

			// Check configured editor
			if eng.Config.DefaultEditor != "" {
				if _, err := exec.LookPath(eng.Config.DefaultEditor); err != nil {
					fmt.Printf("[WARN] configured editor %q not found in PATH\n", eng.Config.DefaultEditor)
					ok = false
				} else {
					fmt.Printf("[OK] editor %q available\n", eng.Config.DefaultEditor)
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

			// Check repo paths exist and bay awareness
			repos, repoErr := eng.RepoList()
			if repoErr != nil {
				fmt.Printf("[WARN] could not load repos: %v\n", repoErr)
				ok = false
			} else {
				for _, repo := range repos {
					path := config.ExpandPath(repo.Path)
					if _, err := os.Stat(path); err != nil {
						fmt.Printf("[WARN] repo %q path %s not accessible\n", repo.Name, path)
						ok = false
						continue
					}
					fmt.Printf("[OK] repo %q accessible\n", repo.Name)

					// Check bay awareness in agent project files.
					for agentName := range config.KnownAgents {
						info, _ := eng.Config.ResolveAgent(agentName)
						if info.ProjectFile == "" {
							continue
						}
						pf := filepath.Join(path, info.ProjectFile)
						data, readErr := os.ReadFile(pf)
						if readErr != nil {
							fmt.Printf("[INFO] repo %q: %s not found (run bay repo init %s)\n", repo.Name, info.ProjectFile, repo.Name)
						} else if !strings.Contains(string(data), "bay agent-guide") {
							fmt.Printf("[INFO] repo %q: %s missing bay awareness for %s (run bay repo init %s)\n", repo.Name, info.ProjectFile, agentName, repo.Name)
						}
					}
				}
			}

			// Check workspace paths in manifest
			m, loadErr := eng.LoadManifest()
			if loadErr != nil {
				fmt.Printf("[WARN] could not load manifest: %v\n", loadErr)
				ok = false
			} else {
				missingCount := 0
				worktreesWithBranches := 0
				for i := range m.Docks {
					dock := &m.Docks[i]
					for j := range dock.Workspaces {
						ws := &dock.Workspaces[j]
						if ws.Worktree != nil && ws.Worktree.Branch != "" {
							worktreesWithBranches++
						}
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

				// Warn if gh is missing but we have worktrees that would
				// benefit from PR auto-detection.
				if worktreesWithBranches > 0 {
					if _, ghErr := exec.LookPath("gh"); ghErr != nil {
						fmt.Println("[INFO] gh not installed — PR numbers won't be auto-detected for branches")
					} else {
						fmt.Println("[OK] gh available (PR auto-detection enabled)")
					}
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

			// Check bay-focus helper (macOS only)
			if runtime.GOOS == "darwin" {
				helperPath := focus.HelperPath()
				if focus.HelperAvailable(helperPath) {
					fmt.Printf("[OK] bay-focus helper available (%s)\n", helperPath)
					// Check Accessibility.
					cmd := exec.Command(helperPath, "--check")
					if err := cmd.Run(); err != nil {
						fmt.Println("[INFO] bay-focus: Accessibility permission not granted (Space switching disabled)")
					} else {
						fmt.Println("[OK] bay-focus: Accessibility granted")
					}
				} else {
					fmt.Println("[INFO] bay-focus helper not compiled (run bay setup to compile)")
				}
			}

			// Check monitor.
			mon, monCfgErr := newMonitorWithConfig()
			if monCfgErr != nil {
				fmt.Printf("[WARN] monitor: %v\n", monCfgErr)
				ok = false
			} else {
				running, pid, monErr := mon.Status()
				if monErr != nil || !running {
					// Lazy retry: the auto-start hook in
					// PersistentPreRunE forks `bay monitor run` before
					// doctor's RunE runs, but the forked child writes
					// its PID file asynchronously. A first-call check
					// that runs before the child has finished startup
					// will incorrectly report "not running". The retry
					// only fires on the negative path so the steady
					// state (monitor already running, second doctor
					// run, etc.) is still instant.
					time.Sleep(150 * time.Millisecond)
					running, pid, monErr = mon.Status()
				}
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
		lines = append(lines, kb.canonicalLine())
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
