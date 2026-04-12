package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Real implements Interface by executing tmux commands.
type Real struct{}

// NewReal creates a new Real tmux implementation.
func NewReal() *Real {
	return &Real{}
}

func run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func runSilent(args ...string) error {
	_, err := run(args...)
	return err
}

// --- Sessions ---

func (r *Real) HasSession(name string) (bool, error) {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	err := cmd.Run()
	if err != nil {
		// tmux returns exit code 1 if the session does not exist; that is not an error for us.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("tmux has-session -t %s: %w", name, err)
	}
	return true, nil
}

func (r *Real) NewSession(name string) error {
	return runSilent("new-session", "-d", "-s", name)
}

func (r *Real) KillSession(name string) error {
	return runSilent("kill-session", "-t", name)
}

func (r *Real) RenameSession(oldName string, newName string) error {
	return runSilent("rename-session", "-t", oldName, newName)
}

func (r *Real) ListSessions() ([]Session, error) {
	out, err := run("list-sessions", "-F", "#{session_name}")
	if err != nil {
		// If no server is running, treat as empty list.
		if strings.Contains(err.Error(), "no server running") || strings.Contains(err.Error(), "no current") {
			return nil, nil
		}
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var sessions []Session
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			sessions = append(sessions, Session{Name: line})
		}
	}
	return sessions, nil
}

// --- Windows ---

func (r *Real) NewWindow(session string, name string, cwd string) (string, error) {
	out, err := run("new-window", "-a", "-t", session, "-n", name, "-c", cwd, "-P", "-F", "#{window_id}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Real) KillWindow(windowID string) error {
	return runSilent("kill-window", "-t", windowID)
}

func (r *Real) RenameWindow(windowID string, name string) error {
	return runSilent("rename-window", "-t", windowID, name)
}

func (r *Real) SetWindowOption(windowID string, option string, value string) error {
	return runSilent("set-option", "-w", "-t", windowID, option, value)
}

func (r *Real) GetWindowOption(windowID string, option string) (string, error) {
	out, err := run("show-options", "-w", "-t", windowID, "-v", option)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Real) WaitingWindowIDs(session string) (map[string]bool, error) {
	out, err := run("list-windows", "-t", session, "-F", "#{window_id}\t#{@bay-waiting}")
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 && parts[1] == "1" {
			result[parts[0]] = true
		}
	}
	return result, nil
}

func (r *Real) ListWindows(session string) ([]Window, error) {
	out, err := run("list-windows", "-t", session, "-F", "#{window_id}\t#{window_name}\t#{window_index}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var windows []Window
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected list-windows output: %q", line)
		}
		idx, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("parsing window index %q: %w", parts[2], err)
		}
		windows = append(windows, Window{
			ID:    parts[0],
			Name:  parts[1],
			Index: idx,
		})
	}
	return windows, nil
}

func (r *Real) WindowExists(windowID string) (bool, error) {
	cmd := exec.Command("tmux", "list-windows", "-t", windowID, "-F", "#{window_id}")
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("tmux list-windows -t %s: %w", windowID, err)
	}
	return true, nil
}

func (r *Real) FindWindowByName(session string, name string) (string, error) {
	windows, err := r.ListWindows(session)
	if err != nil {
		return "", err
	}
	for _, w := range windows {
		if w.Name == name {
			return w.ID, nil
		}
	}
	return "", fmt.Errorf("window %q not found in session %q", name, session)
}

func (r *Real) SelectWindow(windowID string) error {
	return runSilent("select-window", "-t", windowID)
}

// --- Panes ---

func (r *Real) SelectPane(paneID string) error {
	return runSilent("select-pane", "-t", paneID)
}

func (r *Real) SplitWindow(targetID string, dir string, cwd string) (string, error) {
	flag := "-v"
	if dir == "h" {
		flag = "-h"
	}
	out, err := run("split-window", "-t", targetID, flag, "-c", cwd, "-P", "-F", "#{pane_id}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Real) KillPane(paneID string) error {
	return runSilent("kill-pane", "-t", paneID)
}

func (r *Real) SendKeys(paneID string, keys string) error {
	return runSilent("send-keys", "-t", paneID, keys, "Enter")
}

func (r *Real) CapturePane(paneID string, lines int) (string, error) {
	out, err := run("capture-pane", "-t", paneID, "-p", "-S", fmt.Sprintf("-%d", lines))
	if err != nil {
		return "", err
	}
	return out, nil
}

func (r *Real) RespawnPane(paneID string, cwd string, command string) error {
	// -k kills the pane's process if still alive before respawning.
	// Without -k, respawn-pane fails on live panes with "pane still active".
	args := []string{"respawn-pane", "-k", "-t", paneID, "-c", cwd}
	if command != "" {
		args = append(args, command)
	}
	return runSilent(args...)
}

func (r *Real) ListPanes(windowID string) ([]Pane, error) {
	out, err := run("list-panes", "-t", windowID, "-F", "#{pane_id}\t#{pane_pid}\t#{pane_active}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var panes []Pane
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected list-panes output: %q", line)
		}
		pid, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("parsing pane PID %q: %w", parts[1], err)
		}
		panes = append(panes, Pane{
			ID:     parts[0],
			PID:    pid,
			Active: parts[2] == "1",
		})
	}
	return panes, nil
}

func (r *Real) GetPanePID(paneID string) (int, error) {
	out, err := run("display-message", "-t", paneID, "-p", "#{pane_pid}")
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parsing pane PID %q: %w", out, err)
	}
	return pid, nil
}

func (r *Real) PaneExists(paneID string) (bool, error) {
	out, err := run("display-message", "-t", paneID, "-p", "#{pane_dead}")
	if err != nil {
		return false, nil // pane doesn't exist at all
	}
	// pane_dead is "1" for dead panes (remain-on-exit), "0" for live
	return strings.TrimSpace(out) == "0", nil
}

func (r *Real) GetPaneCursorY(paneID string) (int, error) {
	out, err := run("display-message", "-t", paneID, "-p", "#{cursor_y}")
	if err != nil {
		return 0, err
	}
	y, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parsing cursor_y %q: %w", out, err)
	}
	return y, nil
}

// --- Current context ---

func (r *Real) CurrentSession() (string, error) {
	out, err := run("display-message", "-p", "#{session_name}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Real) CurrentWindowID() (string, error) {
	out, err := run("display-message", "-p", "#{window_id}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Real) CurrentPaneID() (string, error) {
	out, err := run("display-message", "-p", "#{pane_id}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// --- Client display ---

// DisplayMessage shows a transient message in the tmux status line. The
// message is rendered with tmux format strings, so it may contain attribute
// codes like #[bold]name#[default]. The display duration is governed by the
// user's tmux display-time setting.
func (r *Real) DisplayMessage(msg string) error {
	return runSilent("display-message", msg)
}

// DisplayPopup opens a tmux popup running the given command.
func (r *Real) DisplayPopup(cmd string) error {
	return runSilent("display-popup", "-E", cmd)
}

// ClientWidth returns the width in cells of the attached tmux client.
// Returns 100 (a reasonable default) if tmux can't report a width — e.g.
// no client attached, or the value can't be parsed.
func (r *Real) ClientWidth() (int, error) {
	out, err := run("display-message", "-p", "#{client_width}")
	if err != nil {
		return 100, nil
	}
	w, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || w <= 0 {
		return 100, nil
	}
	return w, nil
}
