package cli

import (
	"fmt"
	"os"

	"github.com/mpsalisbury/bay/internal/config"
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
