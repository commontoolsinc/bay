package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
			for _, dir := range []string{p.ConfigDir, p.DataDir} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("creating directory %s: %w", dir, err)
				}
			}

			// Ensure config file exists (empty is fine — defaults are built-in).
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				header := "# Bay config — run 'bay help config' for documentation.\n"
				if err := os.WriteFile(configPath, []byte(header), 0o644); err != nil {
					return fmt.Errorf("writing config: %w", err)
				}
				fmt.Printf("Config created at %s\n", configPath)
			} else {
				fmt.Printf("Config at %s\n", configPath)
			}

			// Write default waiting-patterns file.
			// Migrate old name if present.
			promptsPath := filepath.Join(configDir, "waiting-patterns.txt")
			if oldPath := filepath.Join(configDir, "bay-prompts.txt"); promptsPath != oldPath {
				if _, err := os.Stat(oldPath); err == nil {
					if _, err := os.Stat(promptsPath); os.IsNotExist(err) {
						_ = os.Rename(oldPath, promptsPath)
					}
				}
			}
			if _, err := os.Stat(promptsPath); os.IsNotExist(err) {
				defaultPrompts := `# Bay agent-waiting detection patterns
# One regex per line. Comments start with #.

# Claude Code
Allow.*Deny
Enter to select.*Esc to cancel
shift\+tab to approve

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

			// Claude Code integration (only if claude is installed)
			if _, err := exec.LookPath("claude"); err == nil {
				installBaySkill()
				installClaudeHooks(reader)
			}

			// Install shell completions
			installCompletions(cmd.Root(), reader)

			// Install tmux keybindings
			installKeybindings(reader)

			// Configure defaults
			configureAgent(reader, configPath)
			configureEditor(reader, configPath)

			fmt.Println("\nSetup complete. Next steps:")
			fmt.Println("  bay ws new             create a workspace in any git repo")
			fmt.Println("  bay help               see all commands")
			fmt.Println("  bay help <command>      detailed help for a command")
			fmt.Println()
			fmt.Println("Tutorial: https://github.com/commontoolsinc/bay/blob/main/docs/tutorial.md")

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
	data, _ := os.ReadFile(rcFile)
	if strings.Contains(string(data), "bay completion") {
		fmt.Printf("Shell completions already configured in %s\n", rcFile)
		return
	}
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
//
// Most bindings invoke bay itself (tmuxVerb="run-shell", cmd="bay ..."), but
// bay also ships native tmux navigation bindings (next-window, swap-window)
// to give users a convenient navigation layout. For those, isTmuxCommand=true
// and cmd is the raw tmux command, not a shell invocation.
type bayKeybinding struct {
	key           string
	cmd           string
	desc          string
	tmuxVerb      string // "run-shell" or "display-popup -E" (ignored when isTmuxCommand)
	isTmuxCommand bool   // cmd is a raw tmux command (e.g., "next-window")
}

// bayKeybindings defines all bay tmux keybindings.
//
// canonicalLine() appends "|| true" to bay-invoking bindings so they are
// silent in non-bay tmux sessions. Without it, tmux run-shell displays
// 'bay ... returned 1' in the status line when bay exits with an error
// (e.g., "not in a bay workspace"). Stderr is already invisible in
// run-shell, so only the exit code needs masking.
//
// Navigation bindings are tmux-native so they work everywhere, including
// non-bay tmux sessions. The scheme is vim-flavored hjkl:
//   - h/l: previous/next window (tabs run horizontally in the status bar)
//   - j/k: pane down/up (most pane splits stack vertically)
//   - H/L: pane left/right (horizontal splits are the secondary axis)
//   - J/K: pane down/up — duplicates j/k on purpose, so a user holding
//     Shift for a sequence like H, J, L can keep the modifier down
//     without releasing it mid-chord.
//
// Installing these is opt-in via conflict detection: if the user already
// has a binding on one of these keys, bay asks first.
var bayKeybindings = []bayKeybinding{
	// Window navigation (tabs run left-right across the status bar)
	{key: "M-h", cmd: "previous-window", desc: "Option+h: previous window", isTmuxCommand: true},
	{key: "M-l", cmd: "next-window", desc: "Option+l: next window", isTmuxCommand: true},

	// Pane navigation within the current window.
	{key: "M-j", cmd: "select-pane -D", desc: "Option+j: select pane down", isTmuxCommand: true},
	{key: "M-k", cmd: "select-pane -U", desc: "Option+k: select pane up", isTmuxCommand: true},
	{key: "M-H", cmd: "select-pane -L", desc: "Option+H: select pane left", isTmuxCommand: true},
	{key: "M-L", cmd: "select-pane -R", desc: "Option+L: select pane right", isTmuxCommand: true},
	{key: "M-J", cmd: "select-pane -D", desc: "Option+J: select pane down (mirror of j)", isTmuxCommand: true},
	{key: "M-K", cmd: "select-pane -U", desc: "Option+K: select pane up (mirror of k)", isTmuxCommand: true},

	// Workspace picker
	{key: "M-g", cmd: "bay ws go --pick", desc: "Option+g: pick workspace in dock", tmuxVerb: "run-shell"},

	// Creation
	{key: "M-c", cmd: "bay ws new -q", desc: "Option+c: create workspace in current dock", tmuxVerb: "run-shell"},
	{key: "M-C", cmd: "bay ws new -q --agent", desc: "Option+C: create workspace with agent in current dock", tmuxVerb: "run-shell"},
	{key: "M-s", cmd: "bay shell --pane", desc: "Option+s: shell as split pane", tmuxVerb: "run-shell"},
	{key: "M-S", cmd: "bay shell --window", desc: "Option+S: shell in a new window", tmuxVerb: "run-shell"},
	{key: "M-a", cmd: "bay agent --pane", desc: "Option+a: agent as split pane", tmuxVerb: "run-shell"},
	{key: "M-A", cmd: "bay agent --window", desc: "Option+A: agent in a new window", tmuxVerb: "run-shell"},
	{key: "M-e", cmd: "bay edit --ws", desc: "Option+e: workspace editor", tmuxVerb: "run-shell"},
	{key: "M-E", cmd: "bay edit --dock", desc: "Option+E: dock editor", tmuxVerb: "run-shell"},

	// Navigation (dock-wide)
	{key: "M-r", cmd: "bay ws go --next-waiting", desc: "Option+r: jump to next waiting workspace", tmuxVerb: "run-shell"},

	// Utility
	{key: "M-w", cmd: "bay sf close self", desc: "Option+w: close current surface (or pane)", tmuxVerb: "run-shell"},
	{key: "M-?", cmd: "display-message -d 5000 '#(bay pwd --plain)'", desc: "Option+?: flash current bay context", isTmuxCommand: true},

	// Command palette
	{key: "M-p", cmd: "bay palette", desc: "Option+p: command palette (Tab inside to flip mode)", tmuxVerb: "display-popup -w 80% -h 80% -E"},
}

const bayKeybindingsMarker = "# Bay keybindings"

// canonicalLine returns the literal line bay would write for this binding.
func (kb bayKeybinding) canonicalLine() string {
	if kb.isTmuxCommand {
		// Raw tmux command: no shell wrapper, no "|| true" — tmux commands
		// don't fail with distracting status-line messages the way shell
		// commands do via run-shell.
		return fmt.Sprintf("bind-key -n %s %s", kb.key, kb.cmd)
	}
	// Ensure exit 0 so keybindings are silent in non-bay sessions.
	// tmux run-shell displays "returned N" for non-zero exits.
	return fmt.Sprintf("bind-key -n %s %s '%s || true'", kb.key, kb.tmuxVerb, kb.cmd)
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

// parseBindLine extracts the key and command portion of a tmux bind
// line. Handles both active lines and commented lines (leading `#`).
// Returns "", "", false if the line isn't a bind-key/bind binding.
//
// For bay-invoking bindings the returned command is the quoted shell
// contents (e.g. `bay ws new -q || true`); for tmux-native bindings
// it's the raw tmux command (e.g. `previous-window`, `select-pane -L`).
func parseBindLine(line string) (key, cmd string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	}
	if !strings.HasPrefix(trimmed, "bind-key") && !strings.HasPrefix(trimmed, "bind ") {
		return "", "", false
	}
	parts := strings.SplitN(trimmed, " ", 4)
	if len(parts) < 4 || parts[1] != "-n" {
		return "", "", false
	}
	rest := parts[3]
	if open := strings.IndexAny(rest, "'\""); open >= 0 {
		close := strings.LastIndexByte(rest, rest[open])
		if close > open {
			return parts[2], rest[open+1 : close], true
		}
	}
	return parts[2], rest, true
}

// commandsInBlock returns the set of commands found on any active
// (non-comment) bind-key/bind line in the block.
func commandsInBlock(block string) map[string]bool {
	cmds := map[string]bool{}
	for _, l := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if _, cmd, ok := parseBindLine(l); ok {
			cmds[cmd] = true
		}
	}
	return cmds
}

// commentedCommandsInBlock returns the set of commands found on commented-
// out bind lines in the block.
func commentedCommandsInBlock(block string) map[string]bool {
	cmds := map[string]bool{}
	for _, l := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		if _, cmd, ok := parseBindLine(l); ok {
			cmds[cmd] = true
		}
	}
	return cmds
}

// activeKeysInBlock returns the set of keys bound on active (non-comment)
// bind-key/bind lines in the block.
func activeKeysInBlock(block string) map[string]bool {
	keys := map[string]bool{}
	for _, l := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if k, _, ok := parseBindLine(l); ok {
			keys[k] = true
		}
	}
	return keys
}

// commentedKeysInBlock returns the set of keys from commented-out bind
// lines. Bay treats these as opt-out signals: a user who removed a
// binding and left a commented stub is saying "don't re-add this."
func commentedKeysInBlock(block string) map[string]bool {
	keys := map[string]bool{}
	for _, l := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		if k, _, ok := parseBindLine(l); ok {
			keys[k] = true
		}
	}
	return keys
}

// activeBindings returns the map of key → command for every active
// (non-comment) bind line in the block.
func activeBindings(block string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if k, cmd, ok := parseBindLine(l); ok {
			out[k] = cmd
		}
	}
	return out
}

// keptKeys returns the set of keys the user has pinned via
// `# bay-keep: KEY [KEY...]` comments. Those keys are exempt from the
// mismatch check so bay stops re-asking about a non-canonical binding
// the user has decided to keep.
func keptKeys(block string) map[string]bool {
	keys := map[string]bool{}
	for _, l := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(l)
		rest, ok := strings.CutPrefix(trimmed, "# bay-keep:")
		if !ok {
			continue
		}
		for _, k := range strings.Fields(rest) {
			keys[k] = true
		}
	}
	return keys
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
// command in kbs that is not bound in the block. Diff is by bay
// command prefix (ignoring shell suffixes like `|| true`), not by
// key, so a user who rebound a bay command to a different key is not
// flagged as missing it. Commands present only as commented-out
// bindings are ALSO treated as bound — a commented stub is an opt-out
// signal from the user ("I removed this on purpose; don't re-add").
func missingCanonicalLines(block string, kbs []bayKeybinding) []string {
	missing := missingBindings(block, kbs)
	lines := make([]string, 0, len(missing))
	for _, kb := range missing {
		lines = append(lines, kb.canonicalLine())
	}
	return lines
}

// missingBindings returns bay keybindings that aren't bound in the
// block. Matching is primarily by key:
//
//   - If the canonical key is bound (active or commented) → present.
//   - Else, if the canonical command is UNIQUE within kbs AND appears
//     bound at a different key (active or commented) → present. This
//     preserves rebind tolerance for bindings with a 1:1 key↔command
//     mapping (e.g. M-c → `bay ws new -q`).
//   - Else → missing.
//
// The uniqueness guard matters because bay ships intentional command-
// duplicates for ergonomics (M-j and M-J both run `select-pane -D`,
// so Shift-held chords don't lose the modifier). For duplicated
// commands, the command-match path is ambiguous, so we fall through
// to key-only matching — otherwise bay would silently consider M-j
// "present" whenever M-J is bound (and vice versa).
func missingBindings(block string, kbs []bayKeybinding) []bayKeybinding {
	activeKeys := activeKeysInBlock(block)
	commentedKeys := commentedKeysInBlock(block)
	activeCmds := commandsInBlock(block)
	commentedCmds := commentedCommandsInBlock(block)

	cmdCount := map[string]int{}
	for _, kb := range kbs {
		cmdCount[kb.cmd]++
	}

	var missing []bayKeybinding
	for _, kb := range kbs {
		if activeKeys[kb.key] || commentedKeys[kb.key] {
			continue
		}
		if cmdCount[kb.cmd] == 1 {
			if commandMatches(activeCmds, kb.cmd) || commandMatches(commentedCmds, kb.cmd) {
				continue
			}
		}
		missing = append(missing, kb)
	}
	return missing
}

// commandMatches returns true if want is present in cmds, either
// exactly or as a prefix of a longer string (to tolerate shell
// suffixes like `|| true` on bay-invoking bindings).
func commandMatches(cmds map[string]bool, want string) bool {
	for cmd := range cmds {
		if cmd == want || strings.HasPrefix(cmd, want+" ") {
			return true
		}
	}
	return false
}

// bayKeybindingMismatch records a canonical key whose active binding
// runs a bay command that exists in the canonical set but isn't the
// canonical one for that key — the silent-drift case that appears
// when bay ships a canonical change and the user hasn't re-run setup.
type bayKeybindingMismatch struct {
	canonical bayKeybinding
	userCmd   string
}

// mismatchedBindings returns canonical kbs whose key is actively bound
// to a non-canonical-for-that-key bay command. Keys pinned via
// `# bay-keep:` are skipped; truly-missing keys are left for
// missingBindings. We only flag drift against bay's own canonical set
// so unrelated user rebinds aren't touched.
func mismatchedBindings(block string, kbs []bayKeybinding) []bayKeybindingMismatch {
	active := activeBindings(block)
	kept := keptKeys(block)
	canonicalCmds := map[string]bool{}
	for _, kb := range kbs {
		canonicalCmds[kb.cmd] = true
	}

	var out []bayKeybindingMismatch
	for _, kb := range kbs {
		if kept[kb.key] {
			continue
		}
		userCmd, ok := active[kb.key]
		if !ok {
			continue
		}
		if commandMatches(map[string]bool{userCmd: true}, kb.cmd) {
			continue
		}
		for c := range canonicalCmds {
			if c == kb.cmd {
				continue
			}
			if commandMatches(map[string]bool{userCmd: true}, c) {
				out = append(out, bayKeybindingMismatch{canonical: kb, userCmd: userCmd})
				break
			}
		}
	}
	return out
}

// replaceBindingInBlock rewrites the first active bind line in the bay
// block that matches kb.key with kb's canonical line. No-op if the
// block or key isn't found.
func replaceBindingInBlock(content string, kb bayKeybinding) string {
	block, found := extractBayBlock(content)
	if !found {
		return content
	}
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, _, ok := parseBindLine(l)
		if !ok || k != kb.key {
			continue
		}
		lines[i] = kb.canonicalLine()
		break
	}
	return strings.Replace(content, block, strings.Join(lines, "\n"), 1)
}

// appendToBayBlock inserts newLines at the end of the existing bay
// keybindings block in content. Returns content unchanged if no block
// is present (installKeybindings guards this before calling).
func appendToBayBlock(content string, newLines []string) string {
	block, found := extractBayBlock(content)
	if !found || len(newLines) == 0 {
		return content
	}
	updated := block + "\n" + strings.Join(newLines, "\n")
	return strings.Replace(content, block, updated, 1)
}

func promptMissingBindings(reader *bufio.Reader, tmuxConf, content string, missing []bayKeybinding) {
	fmt.Printf("%d canonical binding(s) not bound in your block:\n", len(missing))
	for _, kb := range missing {
		fmt.Printf("  %s\n", kb.canonicalLine())
	}
	fmt.Print("Add these to your block? [Y/n/c] (y=add, n=skip once, c=add commented stubs so bay stops asking) ")
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	switch answer {
	case "n":
		fmt.Println("Leaving block as-is. Bay will ask again on next setup run.")
	case "c":
		var stubs []string
		for _, kb := range missing {
			stubs = append(stubs, "# "+kb.canonicalLine())
		}
		if err := os.WriteFile(tmuxConf, []byte(appendToBayBlock(content, stubs)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", tmuxConf, err)
			return
		}
		fmt.Printf("Added %d commented stub(s) to %s — bay won't re-ask about these.\n", len(stubs), tmuxConf)
	default: // "" (default) or "y"
		var lines []string
		for _, kb := range missing {
			lines = append(lines, kb.canonicalLine())
		}
		if err := os.WriteFile(tmuxConf, []byte(appendToBayBlock(content, lines)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", tmuxConf, err)
			return
		}
		fmt.Printf("Added %d binding(s) to %s\n", len(lines), tmuxConf)
	}
}

func promptMismatchedBindings(reader *bufio.Reader, tmuxConf, content string, mismatches []bayKeybindingMismatch) {
	fmt.Printf("%d binding(s) in your block don't match the current canonical:\n", len(mismatches))
	for _, m := range mismatches {
		fmt.Printf("  %s is bound to `%s`; canonical: `%s`\n",
			m.canonical.key, m.userCmd, m.canonical.cmd)
	}
	fmt.Print("Update these to canonical? [Y/n/k] (y=update, n=skip once, k=keep yours so bay stops asking) ")
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	switch answer {
	case "n":
		fmt.Println("Leaving block as-is. Bay will ask again on next setup run.")
	case "k":
		keys := make([]string, 0, len(mismatches))
		for _, m := range mismatches {
			keys = append(keys, m.canonical.key)
		}
		marker := "# bay-keep: " + strings.Join(keys, " ")
		if err := os.WriteFile(tmuxConf, []byte(appendToBayBlock(content, []string{marker})), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", tmuxConf, err)
			return
		}
		fmt.Printf("Pinned %d binding(s) with bay-keep marker — bay won't re-ask.\n", len(keys))
	default: // "" (default) or "y"
		updated := content
		for _, m := range mismatches {
			updated = replaceBindingInBlock(updated, m.canonical)
		}
		if err := os.WriteFile(tmuxConf, []byte(updated), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", tmuxConf, err)
			return
		}
		fmt.Printf("Updated %d binding(s) in %s\n", len(mismatches), tmuxConf)
	}
}

func installKeybindings(reader *bufio.Reader) {
	fmt.Println()

	home, _ := os.UserHomeDir()
	tmuxConf := filepath.Join(home, ".tmux.conf")

	existing, _ := os.ReadFile(tmuxConf)
	content := string(existing)

	// If a "# Bay keybindings" block already exists, keep the user's
	// customizations but offer to reconcile drift. Two passes: missing
	// canonical bindings (key never bound) and mismatched bindings
	// (canonical key bound to a non-canonical bay command — silent
	// drift after a canonical change). Opt-outs: commented stubs
	// suppress "missing"; `# bay-keep: KEY` suppresses "mismatched".
	if block, found := extractBayBlock(content); found {
		fmt.Printf("Bay keybindings block found in %s.\n", tmuxConf)

		if missing := missingBindings(block, bayKeybindings); len(missing) > 0 {
			promptMissingBindings(reader, tmuxConf, content, missing)
			existing, _ = os.ReadFile(tmuxConf)
			content = string(existing)
			block, _ = extractBayBlock(content)
		}

		if mismatches := mismatchedBindings(block, bayKeybindings); len(mismatches) > 0 {
			promptMismatchedBindings(reader, tmuxConf, content, mismatches)
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

func configureAgent(reader *bufio.Reader, configPath string) {
	fmt.Println()

	cfg, err := config.Load(configPath)
	if err != nil {
		return
	}

	current := cfg.DefaultAgent
	if current == "" {
		current = probeAgent()
	}

	if current != "" {
		fmt.Printf("Set default agent for 'bay agent' (common options: claude, codex, gemini)\n")
		fmt.Printf("Agent [%s]: ", current)
	} else {
		fmt.Println("Set default agent for 'bay agent' (common options: claude, codex, gemini)")
		fmt.Print("Agent: ")
	}

	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)

	if answer == "" && current != "" {
		// Accept the probed default.
		answer = current
	}
	if answer == "" {
		return
	}

	cfg.DefaultAgent = answer
	if err := config.Save(configPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save agent setting: %v\n", err)
		return
	}
	fmt.Printf("Default agent set to %q\n", answer)
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

	cfg.DefaultEditor = answer
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

// installClaudeHooks adds a PermissionRequest hook to Claude Code's
// settings.json that sends a terminal bell, so tmux highlights the tab
// when Claude needs permission. Merges with existing settings.
func installClaudeHooks(reader *bufio.Reader) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// The exact command the hook will run. Kept as a constant so we show
	// the same string to the user as we write to settings.json.
	const bellCommand = `[ -n "$TMUX" ] && printf '\a'`

	// Check up front whether it's already installed — avoids showing the
	// prompt and wasting user attention.
	if data, err := os.ReadFile(settingsPath); err == nil {
		var existing map[string]any
		if err := json.Unmarshal(data, &existing); err == nil {
			if hooks, ok := existing["hooks"].(map[string]any); ok {
				if _, ok := hooks["PermissionRequest"]; ok {
					fmt.Printf("Claude Code PermissionRequest hook already configured in %s\n", settingsPath)
					return
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("Enable tmux tab highlighting when Claude needs permission?")
	fmt.Printf("This adds a PermissionRequest hook to %s that runs:\n", settingsPath)
	fmt.Printf("    %s\n", bellCommand)
	fmt.Println("Sends a terminal bell so tmux flags the tab; Option+R jumps to it.")
	fmt.Print("Enable? [Y/n] ")
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) == "n" {
		return
	}

	// Load existing settings (or start fresh).
	var settings map[string]any
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not parse %s: %v\n", settingsPath, err)
			return
		}
	} else {
		settings = make(map[string]any)
	}

	// Add the hook. Guard with $TMUX check so non-tmux users don't hear a bell.
	bellHook := []any{
		map[string]any{
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": bellCommand,
				},
			},
		},
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
	}
	hooks["PermissionRequest"] = bellHook
	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not marshal settings: %v\n", err)
		return
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create directory: %v\n", err)
		return
	}
	if err := os.WriteFile(settingsPath, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write %s: %v\n", settingsPath, err)
		return
	}

	fmt.Printf("Claude Code bell hook installed in %s\n", settingsPath)
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
