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
				hasContent := loadErr == nil && len(existingCfg.Docks) > 0

				// Also check manifest for repos/docks content
				eng, engErr := newEngine()
				if engErr == nil {
					docks, listErr := eng.List()
					repos, repoErr := eng.RepoList()
					if listErr == nil && len(docks) > 0 {
						hasContent = true
					}
					if repoErr == nil && len(repos) > 0 {
						hasContent = true
					}
				}

				if hasContent {
					fmt.Printf("Config exists at %s with:\n", configPath)

					if engErr == nil {
						docks, listErr := eng.List()
						if listErr == nil && len(docks) > 0 {
							fmt.Print(FormatFullTree(docks))
						} else {
							repos, _ := eng.RepoList()
							for _, repo := range repos {
								fmt.Printf("  repo %s (%s)\n", repo.Name, repo.Path)
							}
							for name := range existingCfg.Docks {
								fmt.Printf("  dock %s\n", name)
							}
						}
					} else {
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
				// Prepend a header comment pointing to docs.
				data, _ := os.ReadFile(configPath)
				header := "# Bay config — run 'bay help config' for documentation.\n\n"
				_ = os.WriteFile(configPath, append([]byte(header), data...), 0o644)
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

// bayKeybinding describes one canonical bay tmux binding.
type bayKeybinding struct {
	key      string
	cmd      string
	desc     string
	tmuxVerb string // "run-shell" or "display-popup -E" (for interactive commands)
}

// bayKeybindings defines all bay tmux keybindings.
var bayKeybindings = []bayKeybinding{
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

	// Utility
	{"M-w", "bay sf close self", "Option+w: close current surface (or pane)", "run-shell"},
	{"M-s", "bay shell", "Option+s: split a shell pane", "run-shell"},
}

const bayKeybindingsMarker = "# Bay keybindings"

// canonicalLine returns the literal line bay would write for this binding.
func (kb bayKeybinding) canonicalLine() string {
	return fmt.Sprintf("bind-key -n %s %s '%s'", kb.key, kb.tmuxVerb, kb.cmd)
}

// extractBayBlock returns the bay keybindings block (everything from the
// "# Bay keybindings" marker until the first blank line or first line
// that isn't a bind-key/bind/comment) along with a flag indicating
// whether the block was found. The marker line itself is included.
func extractBayBlock(content string) (string, bool) {
	allLines := strings.Split(content, "\n")
	start := -1
	for i, l := range allLines {
		if strings.TrimSpace(l) == bayKeybindingsMarker {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	blockLines := []string{allLines[start]}
	for i := start + 1; i < len(allLines); i++ {
		trimmed := strings.TrimSpace(allLines[i])
		if trimmed == "" {
			break
		}
		if !strings.HasPrefix(trimmed, "bind-key") &&
			!strings.HasPrefix(trimmed, "bind ") &&
			!strings.HasPrefix(trimmed, "#") {
			break
		}
		blockLines = append(blockLines, allLines[i])
	}
	return strings.Join(blockLines, "\n"), true
}

// commandsInBlock returns the set of quoted commands found on any
// active (non-comment) bind-key/bind line in the block. Both single-
// and double-quoted commands are recognized; whichever quote character
// appears first opens the command, and the last occurrence of that
// same character closes it.
func commandsInBlock(block string) map[string]bool {
	cmds := map[string]bool{}
	for _, l := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, "bind-key") && !strings.HasPrefix(trimmed, "bind ") {
			continue
		}
		open := strings.IndexAny(trimmed, "'\"")
		if open < 0 {
			continue
		}
		close := strings.LastIndexByte(trimmed, trimmed[open])
		if close <= open {
			continue
		}
		cmds[trimmed[open+1:close]] = true
	}
	return cmds
}

// conflictingKeys returns the canonical keys that are already actively
// bound in content. Walks lines and skips blanks and comments, so a
// commented-out binding does not count as a conflict.
func conflictingKeys(content string, kbs []bayKeybinding) []string {
	bound := map[string]bool{}
	for _, l := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, kb := range kbs {
			if strings.HasPrefix(trimmed, "bind-key -n "+kb.key+" ") ||
				strings.HasPrefix(trimmed, "bind -n "+kb.key+" ") {
				bound[kb.key] = true
			}
		}
	}
	var conflicts []string
	for _, kb := range kbs {
		if bound[kb.key] {
			conflicts = append(conflicts, kb.key)
		}
	}
	return conflicts
}

// missingCanonicalLines returns the canonical bind-key lines for any
// command in kbs that is not bound in the block. Diff is by command,
// not by key, so a user who rebound a bay command to a different key
// is not flagged as missing it.
func missingCanonicalLines(block string, kbs []bayKeybinding) []string {
	have := commandsInBlock(block)
	var missing []string
	for _, kb := range kbs {
		if !have[kb.cmd] {
			missing = append(missing, kb.canonicalLine())
		}
	}
	return missing
}

func installKeybindings(reader *bufio.Reader) {
	fmt.Println()

	home, _ := os.UserHomeDir()
	tmuxConf := filepath.Join(home, ".tmux.conf")

	existing, _ := os.ReadFile(tmuxConf)
	content := string(existing)

	// If a "# Bay keybindings" block already exists, leave it alone —
	// the user may have customized it. Print a heads-up listing any
	// canonical commands their block doesn't bind, so they know what
	// they could be missing.
	if block, found := extractBayBlock(content); found {
		fmt.Printf("Bay keybindings block found in %s — leaving as-is.\n", tmuxConf)
		if missing := missingCanonicalLines(block, bayKeybindings); len(missing) > 0 {
			fmt.Printf("Note: %d canonical binding(s) not bound in your block:\n", len(missing))
			for _, line := range missing {
				fmt.Printf("  %s\n", line)
			}
			fmt.Printf("(To re-install the defaults, delete the block from %s and re-run bay setup.)\n", tmuxConf)
		}
		return
	}

	// No existing block. Check for conflicts on canonical keys.
	conflicts := conflictingKeys(content, bayKeybindings)

	if len(conflicts) > 0 {
		fmt.Printf("Warning: these keys are already bound in %s: %s\n", tmuxConf, strings.Join(conflicts, ", "))
		fmt.Println("Bay will add its bindings, but yours will take precedence if they appear later in the file.")
		fmt.Println("Consider removing the conflicting bindings or choosing different keys.")
		fmt.Println()
	}

	// Show what we'll add.
	fmt.Printf("These tmux keybindings will be added to %s:\n\n", tmuxConf)
	for _, kb := range bayKeybindings {
		conflict := ""
		for _, c := range conflicts {
			if c == kb.key {
				conflict = " (conflicts with existing binding)"
				break
			}
		}
		fmt.Printf("  %s%s\n", kb.canonicalLine(), conflict)
		fmt.Printf("    %s\n\n", kb.desc)
	}
	fmt.Print("Add these keybindings? [Y/n] ")
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) == "n" {
		return
	}

	// Append the canonical block. We returned early above if a block
	// already existed, so there's nothing to strip.
	newBlock := "\n" + bayKeybindingsMarker + "\n"
	for _, kb := range bayKeybindings {
		newBlock += kb.canonicalLine() + "\n"
	}
	if err := os.WriteFile(tmuxConf, []byte(content+newBlock), 0o644); err != nil {
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
