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

					eng, engErr := newEngine()
					if engErr == nil {
						docks, listErr := eng.List()
						if listErr == nil && len(docks) > 0 {
							fmt.Print(FormatFullTree(existingCfg, docks))
						} else {
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
					fmt.Print("Type 'delete all' to confirm, or anything else to keep your config: ")
					answer, _ := reader.ReadString('\n')
					if strings.TrimSpace(answer) != "delete all" {
						fmt.Println("Keeping existing config.")
						writeConfig = false
					}
				} else {
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
			installKeybindings(reader)

			// Configure editor
			configureEditor(reader, configPath)

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

// bayKeybindings defines all bay tmux keybindings.
var bayKeybindings = []struct {
	key      string
	cmd      string
	desc     string
	tmuxVerb string // "run-shell" or "display-popup -E" (for interactive commands)
}{
	{"M-w", "bay close-pane", "Option+w: close current pane (or window if only pane)", "run-shell"},
	{"M-s", "bay shell", "Option+s: split a shell pane in the current workspace", "run-shell"},
	{"M-g", "bay go", "Option+g: fuzzy-pick any workspace window", "display-popup -E"},
	{"M-a", "bay go --next-waiting", "Option+a: jump to the next agent waiting for input", "run-shell"},
}

func installKeybindings(reader *bufio.Reader) {
	fmt.Println()

	home, _ := os.UserHomeDir()
	tmuxConf := filepath.Join(home, ".tmux.conf")

	existing, _ := os.ReadFile(tmuxConf)
	content := string(existing)

	// Build the full bay keybindings block
	var lines []string
	for _, kb := range bayKeybindings {
		line := fmt.Sprintf("bind-key -n %s %s '%s'", kb.key, kb.tmuxVerb, kb.cmd)
		lines = append(lines, line)
	}

	// Check if all bindings are present with the correct format
	allPresent := true
	for _, line := range lines {
		if !strings.Contains(content, line) {
			allPresent = false
			break
		}
	}
	if allPresent {
		fmt.Println("Tmux keybindings already up to date.")
		return
	}

	// Show what we'll add
	fmt.Printf("These tmux keybindings will be added to %s:\n\n", tmuxConf)
	for i, kb := range bayKeybindings {
		fmt.Printf("  %s\n", lines[i])
		fmt.Printf("    %s\n\n", kb.desc)
	}
	fmt.Print("Add these keybindings? [Y/n] ")
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) == "n" {
		return
	}

	// Remove any existing bay keybinding block
	if idx := strings.Index(content, "\n# Bay keybindings"); idx >= 0 {
		// Find the end of the bay block (next blank line or non-bind line)
		rest := content[idx+1:]
		endIdx := len(rest)
		inBlock := false
		for i, line := range strings.Split(rest, "\n") {
			if i == 0 {
				inBlock = true
				continue
			}
			trimmed := strings.TrimSpace(line)
			if inBlock && trimmed != "" && !strings.HasPrefix(trimmed, "bind-key") && !strings.HasPrefix(trimmed, "#") {
				endIdx = strings.Index(rest, line)
				break
			}
			if inBlock && trimmed == "" {
				endIdx = strings.Index(rest, line)
				break
			}
		}
		content = content[:idx] + content[idx+1+endIdx:]
	}

	// Write the new block
	block := "\n# Bay keybindings\n"
	for _, line := range lines {
		block += line + "\n"
	}

	if err := os.WriteFile(tmuxConf, []byte(content+block), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", tmuxConf, err)
		return
	}

	fmt.Println("Added.")
}

func configureEditor(reader *bufio.Reader, configPath string) {
	fmt.Println()

	cfg, err := config.Load(configPath)
	if err != nil {
		return
	}
	current, _ := resolveEditor(cfg)

	if current != "" {
		fmt.Printf("Set default editor for 'bay edit' (common options: cursor, code, zed, nvim, vim)\n")
		fmt.Printf("Editor [%s]: ", current)
	} else {
		fmt.Println("Set default editor for 'bay edit' (common options: cursor, code, zed, nvim, vim)")
		fmt.Print("Editor: ")
	}

	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)

	if answer == "" {
		return
	}

	cfg.Editor.Command = answer
	if err := config.Save(configPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save editor setting: %v\n", err)
		return
	}
	fmt.Printf("Editor set to %q\n", answer)
}
