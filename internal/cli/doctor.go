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

			// Check gitignore for each repo+agent combo
			for dockName, dock := range eng.Config.Docks {
				if dock.Agent == "" || dock.Repo == "" {
					continue
				}
				agentCfg, aok := eng.Config.Agents[dock.Agent]
				repoCfg, rok := eng.Config.Repos[dock.Repo]
				if !aok || !rok {
					continue
				}
				if agentCfg.ConfigFile == "" {
					continue
				}
				repoPath := config.ExpandPath(repoCfg.Path)
				ignored, gitErr := eng.Git.IsIgnored(repoPath, agentCfg.ConfigFile)
				if gitErr != nil {
					fmt.Printf("[WARN] dock %q: could not check gitignore for %s in %s\n",
						dockName, agentCfg.ConfigFile, repoPath)
					ok = false
				} else if !ignored {
					fmt.Printf("[WARN] dock %q: %s not in .gitignore for %s\n",
						dockName, agentCfg.ConfigFile, repoPath)
					ok = false
				} else {
					fmt.Printf("[OK] dock %q: %s gitignored in %s\n",
						dockName, agentCfg.ConfigFile, repoPath)
				}
			}

			// Check workspace paths in manifest
			m, loadErr := eng.LoadManifest()
			if loadErr != nil {
				fmt.Printf("[WARN] could not load manifest: %v\n", loadErr)
				ok = false
			} else {
				missingCount := 0
				for dockName, ds := range m.Docks {
					for wsID, ws := range ds.Workspaces {
						if ws.Path == "" {
							continue
						}
						wsPath := config.ExpandPath(ws.Path)
						if _, err := os.Stat(wsPath); err != nil {
							fmt.Printf("[WARN] workspace %s:%s path missing: %s\n", dockName, wsID, wsPath)
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

	for dockName, ds := range m.Docks {
		seenNames := map[string]string{}
		for wsID, ws := range ds.Workspaces {
			if ws.Name != "" {
				if other, ok := seenNames[ws.Name]; ok {
					warnings = append(warnings, fmt.Sprintf("dock %s has duplicate workspace name %q (%s, %s)", dockName, ws.Name, other, wsID))
				} else {
					seenNames[ws.Name] = wsID
				}
			}
			if ws.AgentOverride != "" {
				if _, ok := cfg.Agents[ws.AgentOverride]; !ok {
					warnings = append(warnings, fmt.Sprintf("workspace %s:%s references unknown agent override %q", dockName, wsID, ws.AgentOverride))
				}
			}

			seenWindowIDs := map[int]bool{}
			for _, win := range ws.Windows {
				if seenWindowIDs[win.ID] {
					warnings = append(warnings, fmt.Sprintf("workspace %s:%s has duplicate window id %d", dockName, wsID, win.ID))
				}
				seenWindowIDs[win.ID] = true
				if len(win.Panes) == 0 {
					warnings = append(warnings, fmt.Sprintf("workspace %s:%s window %d has no panes", dockName, wsID, win.ID))
					continue
				}
				seenPaneIDs := map[int]bool{}
				for _, pane := range win.Panes {
					if seenPaneIDs[pane.ID] {
						warnings = append(warnings, fmt.Sprintf("workspace %s:%s window %d has duplicate pane id %d", dockName, wsID, win.ID, pane.ID))
					}
					seenPaneIDs[pane.ID] = true
				}
			}
		}
	}

	return warnings
}
