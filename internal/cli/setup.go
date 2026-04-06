package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/focus"
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
			for _, dir := range []string{p.ConfigDir, p.DataDir} {
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
				cfg := defaultSetupConfig()
				if err := config.Save(configPath, cfg); err != nil {
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

			// Install Claude Code skill
			installBaySkill()

			// Install shell completions
			installCompletions(cmd.Root(), reader)

			// Install tmux keybindings
			installKeybindings(reader)

			// Configure editor
			configureEditor(reader, configPath)

			// Compile bay-focus helper (macOS only)
			compileFocusHelper()

			fmt.Println("\nSetup complete. Next steps:")
			fmt.Println("  Run bay ws new in any git repo to create your first workspace.")
			fmt.Println("  Or add repos explicitly:  bay repo add <name> <path>")

			return nil
		},
	}
}

// defaultSetupConfig returns the default config for new installations.
func defaultSetupConfig() *config.Config {
	return &config.Config{
		Agents: map[string]config.AgentConfig{
			"claude": {Command: "claude", ProjectFile: "CLAUDE.md"},
			"codex":  {Command: "codex"},
			"gemini": {Command: "gemini"},
		},
		Repos: map[string]config.RepoConfig{},
		Docks: map[string]config.DockConfig{},
		Monitor: config.MonitorConfig{
			IntervalSeconds: 3,
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
	// Surface navigation (intra-workspace)
	{"M-j", "bay surface next", "Option+j: next surface in workspace", "run-shell"},
	{"M-k", "bay surface prev", "Option+k: prev surface in workspace", "run-shell"},
	{"M-g", "bay go", "Option+g: pick surface in workspace", "display-popup -E"},
	{"M-a", "bay go --next-waiting", "Option+a: next waiting surface", "run-shell"},

	// Workspace navigation (intra-dock)
	{"M-J", "bay ws next", "Option+J: next workspace in dock", "run-shell"},
	{"M-K", "bay ws prev", "Option+K: prev workspace in dock", "run-shell"},
	{"M-G", "bay ws go", "Option+G: pick workspace in dock", "display-popup -E"},
	{"M-A", "bay ws go --next-waiting", "Option+A: next waiting workspace", "run-shell"},

	// Surface by index
	{"M-1", "bay go --index 1", "Option+1: surface 1", "run-shell"},
	{"M-2", "bay go --index 2", "Option+2: surface 2", "run-shell"},
	{"M-3", "bay go --index 3", "Option+3: surface 3", "run-shell"},
	{"M-4", "bay go --index 4", "Option+4: surface 4", "run-shell"},
	{"M-5", "bay go --index 5", "Option+5: surface 5", "run-shell"},
	{"M-6", "bay go --index 6", "Option+6: surface 6", "run-shell"},
	{"M-7", "bay go --index 7", "Option+7: surface 7", "run-shell"},
	{"M-8", "bay go --index 8", "Option+8: surface 8", "run-shell"},
	{"M-9", "bay go --index 9", "Option+9: surface 9", "run-shell"},

	// Utility
	{"M-w", "bay sf close", "Option+w: close current surface (or pane)", "run-shell"},
	{"M-s", "bay shell", "Option+s: split a shell pane", "run-shell"},
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

	// Check for conflicting non-bay bindings
	// Strip out the bay block to check only user bindings
	userContent := content
	if idx := strings.Index(userContent, "\n# Bay keybindings"); idx >= 0 {
		rest := userContent[idx+1:]
		endIdx := len(rest)
		for i, line := range strings.Split(rest, "\n") {
			if i == 0 {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "bind") && !strings.HasPrefix(trimmed, "#") {
				endIdx = strings.Index(rest, line)
				break
			}
			if trimmed == "" && i > 1 {
				endIdx = strings.Index(rest, line)
				break
			}
		}
		userContent = userContent[:idx] + userContent[idx+1+endIdx:]
	}

	var conflicts []string
	for _, kb := range bayKeybindings {
		// Look for bind-key -n <key> in non-bay content
		pattern := "bind-key -n " + kb.key + " "
		altPattern := "bind -n " + kb.key + " "
		if strings.Contains(userContent, pattern) || strings.Contains(userContent, altPattern) {
			conflicts = append(conflicts, kb.key)
		}
	}

	if len(conflicts) > 0 {
		fmt.Printf("Warning: these keys are already bound in %s: %s\n", tmuxConf, strings.Join(conflicts, ", "))
		fmt.Println("Bay will add its bindings, but yours will take precedence if they appear later in the file.")
		fmt.Println("Consider removing the conflicting bindings or choosing different keys.")
		fmt.Println()
	}

	// Show what we'll add
	fmt.Printf("These tmux keybindings will be added to %s:\n\n", tmuxConf)
	for i, kb := range bayKeybindings {
		conflict := ""
		for _, c := range conflicts {
			if c == kb.key {
				conflict = " (conflicts with existing binding)"
				break
			}
		}
		fmt.Printf("  %s%s\n", lines[i], conflict)
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

const baySkillContent = `---
name: bay
description: Bay workspace management — git worktrees and tmux windows. Use when creating, managing, or navigating workspaces, docks, or repos.
---

!` + "`bay agent-guide`" + `
`

func compileFocusHelper() {
	if runtime.GOOS != "darwin" {
		return
	}

	fmt.Println()
	helperPath := focus.HelperPath()
	sourcePath := focus.SourcePath()

	if _, err := os.Stat(sourcePath); err != nil {
		fmt.Println("bay-focus helper source not found — skipping compilation.")
		return
	}

	fmt.Printf("Compiling bay-focus helper → %s\n", helperPath)
	if err := focus.Compile(sourcePath, helperPath); err != nil {
		fmt.Printf("Warning: failed to compile bay-focus: %v\n", err)
		fmt.Println("Space switching will require manual Cmd+Tab. Run bay doctor for details.")
		return
	}
	fmt.Println("bay-focus helper compiled.")

	// Check Accessibility.
	cmd := exec.Command(helperPath, "--check")
	if err := cmd.Run(); err != nil {
		fmt.Println("Note: Accessibility permission not yet granted.")
		fmt.Println("Enable in: System Settings → Privacy & Security → Accessibility")
	}
}

func installBaySkill() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	skillDir := filepath.Join(home, ".claude", "skills", "bay")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create skill directory: %v\n", err)
		return
	}

	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(baySkillContent), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write skill file: %v\n", err)
		return
	}

	fmt.Printf("Claude Code skill installed at %s\n", skillPath)
}
