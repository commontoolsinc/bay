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

			configDir := config.DefaultConfigDir()
			dataDir := config.DefaultDataDir()
			configPath := config.DefaultConfigPath()

			// Create directories
			for _, dir := range []string{configDir, dataDir, filepath.Join(configDir, "templates")} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("creating directory %s: %w", dir, err)
				}
			}

			// Check if config exists
			if _, err := os.Stat(configPath); err == nil {
				fmt.Printf("Config already exists at %s\n", configPath)
				fmt.Print("Overwrite? (y/N) ")
				answer, _ := reader.ReadString('\n')
				if strings.TrimSpace(strings.ToLower(answer)) != "y" {
					fmt.Println("Keeping existing config.")
					goto prompts
				}
			}

			// Write default config
			{
				defaultConfig := `# Bay configuration
# See: bay doctor

[agents.claude]
command = "claude"
config_file = "CLAUDE.local.md"

[agents.codex]
command = "codex"
config_file = "AGENTS.local.md"

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
next_waiting = "w"
`
				if err := os.WriteFile(configPath, []byte(defaultConfig), 0o644); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Printf("Config written to %s\n", configPath)
			}

		prompts:
			// Write default prompts file
			promptsPath := filepath.Join(configDir, "bay-prompts.txt")
			if _, err := os.Stat(promptsPath); os.IsNotExist(err) {
				defaultPrompts := `# Bay agent-waiting detection patterns
# One regex per line. Comments start with #.

# Claude Code
Allow.*Deny

# Codex
\[Y/n\]

# Generic
\(y/n\)
Do you want to proceed
`
				if err := os.WriteFile(promptsPath, []byte(defaultPrompts), 0o644); err != nil {
					return fmt.Errorf("writing prompts: %w", err)
				}
				fmt.Printf("Prompts written to %s\n", promptsPath)
			}

			// Install shell completions
			installCompletions(cmd.Root(), reader)

			// Install tmux keybindings
			installKeybindings(reader, configPath)

			fmt.Println("\nSetup complete. Next steps:")
			fmt.Println("  1. Edit config to add your repos and docks")
			fmt.Println("  2. Add CLAUDE.local.md to your repos' .gitignore")
			fmt.Println("  3. Run: bay dock new <name> --repo <repo> --agent claude")

			return nil
		},
	}
}

func installCompletions(root *cobra.Command, reader *bufio.Reader) {
	fmt.Println()
	fmt.Print("Install shell completions? [Y/n] ")
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) == "n" {
		return
	}

	shell := os.Getenv("SHELL")
	home, _ := os.UserHomeDir()

	switch {
	case strings.HasSuffix(shell, "/zsh"):
		targets := []string{
			filepath.Join(home, ".zsh", "completions"),
			"/usr/local/share/zsh/site-functions",
		}
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			targets = append([]string{filepath.Join(xdg, "zsh", "completions")}, targets...)
		}

		var target string
		for _, t := range targets {
			if _, err := os.Stat(t); err == nil {
				target = t
				break
			}
		}
		if target == "" {
			target = filepath.Join(home, ".zsh", "completions")
			os.MkdirAll(target, 0o755)
		}

		dest := filepath.Join(target, "_bay")
		f, err := os.Create(dest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not write %s: %v\n", dest, err)
			printManualCompletionInstructions()
			return
		}
		root.GenZshCompletion(f)
		f.Close()
		fmt.Printf("  Zsh completions written to %s\n", dest)
		fmt.Println("  Make sure this directory is in your fpath. Add to .zshrc if needed:")
		fmt.Printf("    fpath=(%s $fpath)\n", target)
		fmt.Println("    autoload -Uz compinit && compinit")

	case strings.HasSuffix(shell, "/bash"):
		targets := []string{
			"/usr/local/etc/bash_completion.d", // macOS + Homebrew
			"/etc/bash_completion.d",            // Linux
		}
		var target string
		for _, t := range targets {
			if _, err := os.Stat(t); err == nil {
				target = t
				break
			}
		}
		if target != "" {
			dest := filepath.Join(target, "bay")
			f, err := os.Create(dest)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not write %s: %v\n", dest, err)
				printManualCompletionInstructions()
				return
			}
			root.GenBashCompletion(f)
			f.Close()
			fmt.Printf("  Bash completions written to %s\n", dest)
		} else {
			printManualCompletionInstructions()
		}

	case strings.HasSuffix(shell, "/fish"):
		target := filepath.Join(home, ".config", "fish", "completions")
		os.MkdirAll(target, 0o755)
		dest := filepath.Join(target, "bay.fish")
		f, err := os.Create(dest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not write %s: %v\n", dest, err)
			printManualCompletionInstructions()
			return
		}
		root.GenFishCompletion(f, true)
		f.Close()
		fmt.Printf("  Fish completions written to %s\n", dest)

	default:
		printManualCompletionInstructions()
	}
}

func printManualCompletionInstructions() {
	fmt.Println("  Install completions manually:")
	fmt.Println("    Bash:  source <(bay completion bash)")
	fmt.Println("    Zsh:   source <(bay completion zsh)")
	fmt.Println("    Fish:  bay completion fish | source")
}

func installKeybindings(reader *bufio.Reader, configPath string) {
	fmt.Println()
	fmt.Print("Install tmux keybindings? [Y/n] ")
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) == "n" {
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not load config: %v\n", err)
		return
	}

	nextWaitingKey := cfg.Keybind.NextWaiting
	if nextWaitingKey == "" {
		nextWaitingKey = "w"
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

	var additions []string

	bayNextWaiting := fmt.Sprintf("bind-key %s run-shell 'bay go --next-waiting'", nextWaitingKey)
	if !strings.Contains(content, "bay go --next-waiting") {
		additions = append(additions, bayNextWaiting)
	}

	bayAddPrompt := fmt.Sprintf("bind-key %s run-shell 'bay add-prompt'", addPromptKey)
	if !strings.Contains(content, "bay add-prompt") {
		additions = append(additions, bayAddPrompt)
	}

	if len(additions) == 0 {
		fmt.Println("  Tmux keybindings already installed.")
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

	fmt.Printf("  Added to %s:\n", tmuxConf)
	for _, line := range additions {
		fmt.Printf("    %s\n", line)
	}
}
