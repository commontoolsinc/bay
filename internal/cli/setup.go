package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "First-time setup: config, docks, keybindings, completions",
		RunE: func(cmd *cobra.Command, args []string) error {
			reader := bufio.NewReader(os.Stdin)

			p := bayPaths()
			configDir := p.ConfigDir
			configPath := p.ConfigFile

			// Create directories
			for _, dir := range []string{p.ConfigDir, p.DataDir, filepath.Join(p.ConfigDir, "templates")} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("creating directory %s: %w", dir, err)
				}
			}

			// Check if config exists; write default if not (or if user confirms overwrite)
			writeConfig := true
			if _, err := os.Stat(configPath); err == nil {
				// Load existing config to show what would be lost
				existingCfg, loadErr := config.Load(configPath)
				hasContent := loadErr == nil && (len(existingCfg.Repos) > 0 || len(existingCfg.Docks) > 0)

				if hasContent {
					fmt.Printf("Config exists at %s with:\n", configPath)

					// Show what would be lost using the full tree
					eng, engErr := newEngine()
					if engErr == nil {
						docks, listErr := eng.List()
						if listErr == nil && len(docks) > 0 {
							fmt.Print(FormatFullTree(existingCfg, docks))
						} else {
							// No manifest data, just show config counts
							for name := range existingCfg.Repos {
								fmt.Printf("  repo %s (%s)\n", name, existingCfg.Repos[name].Path)
							}
							for name := range existingCfg.Docks {
								fmt.Printf("  dock %s\n", name)
							}
						}
					} else {
						for name := range existingCfg.Repos {
							fmt.Printf("  repo %s (%s)\n", name, existingCfg.Repos[name].Path)
						}
						for name := range existingCfg.Docks {
							fmt.Printf("  dock %s\n", name)
						}
					}

					fmt.Println("\nOverwriting will DELETE all of the above and replace with defaults.")
					fmt.Print("Type 'delete all' to confirm: ")
					answer, _ := reader.ReadString('\n')
					if strings.TrimSpace(answer) != "delete all" {
						fmt.Println("Keeping existing config.")
						writeConfig = false
					}
				} else {
					// Config exists but is empty/default — simple prompt is fine
					fmt.Printf("Config already exists at %s (no repos or docks configured)\n", configPath)
					fmt.Print("Overwrite with defaults? (y/N) ")
					answer, _ := reader.ReadString('\n')
					if strings.TrimSpace(strings.ToLower(answer)) != "y" {
						fmt.Println("Keeping existing config.")
						writeConfig = false
					}
				}
			}

			if writeConfig {
				defaultConfig := `# Bay configuration
# See: bay doctor

[agents.claude]
command = "claude"
config_file = "CLAUDE.local.md"

[agents.codex]
command = "codex"
config_file = "AGENTS.local.md"

[agents.gemini]
command = "gemini"
config_file = "GEMINI.local.md"

# [repos.myproject]
# path = "~/projects/myproject"
# worktree_dir = "~/projects/myproject-worktrees"  # optional

# [docks.dev]
# repo = "myproject"
# agent = "claude"
# agent_config_template = "~/.config/bay/templates/dev.md"

[monitor]
interval_seconds = 3

[keybinding]
add_prompt = "P"
next_waiting = "M-w"
shell = "M-s"
`
				if err := os.WriteFile(configPath, []byte(defaultConfig), 0o644); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Printf("Config written to %s\n", configPath)
			}

			// Write default prompts file
			promptsPath := filepath.Join(configDir, "bay-prompts.txt")
			if _, err := os.Stat(promptsPath); os.IsNotExist(err) {
				defaultPrompts := `# Bay agent-waiting detection patterns
# One regex per line. Comments start with #.

# Claude Code
Allow.*Deny

# Codex
\[Y/n\]

# Gemini
Approve\? \(y/n
Allow command.*\[y/N\]

# Generic
\(y/n\)
Do you want to proceed
`
				if err := os.WriteFile(promptsPath, []byte(defaultPrompts), 0o644); err != nil {
					return fmt.Errorf("writing prompts: %w", err)
				}
				fmt.Printf("Prompts written to %s\n", promptsPath)
			}

			// Install orchestrator guide
			guidePath := filepath.Join(configDir, "orchestrator-guide.md")
			if err := os.WriteFile(guidePath, []byte(orchestratorGuide), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not write orchestrator guide: %v\n", err)
			} else {
				fmt.Printf("Orchestrator guide written to %s\n", guidePath)
			}

			// Install shell completions
			installCompletions(cmd.Root(), reader)

			// Install tmux keybindings
			installKeybindings(reader, configPath)

			fmt.Println("\nSetup complete. Next steps:")
			fmt.Println("  1. Add a repo:  bay repo add <name> <path>")
			fmt.Println("  2. Create a dock:  bay dock new <name> --repo <repo>")
			fmt.Println()
			fmt.Println("To teach an orchestrator agent about bay:")
			fmt.Printf("  claude --add-dir %s\n", configDir)

			return nil
		},
	}
}

func installCompletions(_ *cobra.Command, _ *bufio.Reader) {
	fmt.Println()

	shell := os.Getenv("SHELL")
	var shellName, snippet string

	switch {
	case strings.HasSuffix(shell, "/zsh"):
		shellName = "zsh"
		snippet = `if command -v bay > /dev/null ; then
  source <(bay completion zsh)
fi`
	case strings.HasSuffix(shell, "/bash"):
		shellName = "bash"
		snippet = `if command -v bay > /dev/null ; then
  source <(bay completion bash)
fi`
	case strings.HasSuffix(shell, "/fish"):
		shellName = "fish"
		snippet = `if command -v bay > /dev/null
  bay completion fish | source
end`
	default:
		fmt.Println("Could not detect shell. Add one of these to your shell rc file:")
		fmt.Println("  Bash/Zsh:  source <(bay completion bash|zsh)")
		fmt.Println("  Fish:      bay completion fish | source")
		return
	}

	rcFile := shellRCFile(shellName)
	fmt.Printf("Add this to %s for tab completions:\n\n", rcFile)
	fmt.Println("  " + strings.ReplaceAll(snippet, "\n", "\n  "))
	fmt.Println()
}

// shellRCFile returns the conventional rc file path for a shell.
func shellRCFile(shell string) string {
	home, _ := os.UserHomeDir()
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		return filepath.Join(home, ".bashrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	default:
		return "your shell rc file"
	}
}

func installKeybindings(reader *bufio.Reader, configPath string) {
	fmt.Println()

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not load config: %v\n", err)
		return
	}

	nextWaitingKey := cfg.Keybind.NextWaiting
	if nextWaitingKey == "" {
		nextWaitingKey = "M-w"
	}
	addPromptKey := cfg.Keybind.AddPrompt
	if addPromptKey == "" {
		addPromptKey = "P"
	}

	home, _ := os.UserHomeDir()
	tmuxConf := filepath.Join(home, ".tmux.conf")

	// Read existing tmux.conf
	existing, _ := os.ReadFile(tmuxConf)
	content := string(existing)

	shellKey := cfg.Keybind.Shell
	if shellKey == "" {
		shellKey = "M-s"
	}

	var additions []string

	bayNextWaiting := tmuxBindCmd(nextWaitingKey, "bay go --next-waiting")
	if !strings.Contains(content, "bay go --next-waiting") {
		additions = append(additions, bayNextWaiting)
	}

	bayShell := tmuxBindCmd(shellKey, "bay shell")
	if !strings.Contains(content, "bay shell") {
		additions = append(additions, bayShell)
	}

	if len(additions) == 0 {
		fmt.Println("Tmux keybindings already installed.")
		return
	}

	// Show what we'd add with plain-English descriptions
	fmt.Printf("These tmux keybindings will be added to %s:\n\n", tmuxConf)
	for _, line := range additions {
		fmt.Printf("  %s\n", line)
		fmt.Printf("    %s\n\n", describeKeybinding(line))
	}
	fmt.Print("Add these keybindings? [Y/n] ")
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) == "n" {
		return
	}

	f, err := os.OpenFile(tmuxConf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not write %s: %v\n", tmuxConf, err)
		return
	}
	defer f.Close()

	f.WriteString("\n# Bay keybindings\n")
	for _, line := range additions {
		f.WriteString(line + "\n")
	}

	fmt.Println("Added.")
}

// tmuxBindCmd generates a tmux bind-key command. Keys starting with "M-"
// use bind-key -n (no prefix required); others use plain bind-key (prefix required).
func tmuxBindCmd(key, shellCmd string) string {
	if strings.HasPrefix(key, "M-") {
		return fmt.Sprintf("bind-key -n %s run-shell '%s'", key, shellCmd)
	}
	return fmt.Sprintf("bind-key %s run-shell '%s'", key, shellCmd)
}

// describeKeybinding returns a plain-English description of a tmux bind-key line.
func describeKeybinding(line string) string {
	switch {
	case strings.Contains(line, "bay go --next-waiting"):
		key := "Option+w"
		if strings.Contains(line, "bind-key -n M-") {
			// Extract the key after M-
			key = "Option+" + strings.TrimPrefix(
				strings.Fields(line)[2], "M-")
		} else if !strings.Contains(line, "-n") {
			key = "prefix + " + strings.Fields(line)[1]
		}
		return fmt.Sprintf("%s: jump to the next agent waiting for input", key)
	case strings.Contains(line, "bay shell"):
		key := "Option+s"
		if strings.Contains(line, "bind-key -n M-") {
			key = "Option+" + strings.TrimPrefix(
				strings.Fields(line)[2], "M-")
		} else if !strings.Contains(line, "-n") {
			key = "prefix + " + strings.Fields(line)[1]
		}
		return fmt.Sprintf("%s: split a shell pane in the current workspace", key)
	case strings.Contains(line, "bay add-prompt"):
		key := "prefix + P"
		if strings.Contains(line, "-n M-") {
			key = "Option+" + strings.TrimPrefix(
				strings.Fields(line)[2], "M-")
		} else if !strings.Contains(line, "-n") {
			key = "prefix + " + strings.Fields(line)[1]
		}
		return fmt.Sprintf("%s: capture current pane text as a waiting-detection pattern", key)
	default:
		return ""
	}
}

