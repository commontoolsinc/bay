package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/prepare"
)

type prepareBlockDecision struct {
	Blocked  bool
	Dispatch bool
}

func (e *Engine) prepareManager() prepare.Manager {
	return prepare.Manager{
		Config:       e.Config,
		ManifestPath: e.manifestPath,
		DataDir:      filepath.Dir(e.manifestPath),
	}
}

func (e *Engine) prepareBlockDecision(ctx context.Context, dockName, bayID string, kind manifest.SurfaceType) (prepareBlockDecision, error) {
	if kind != manifest.SurfaceTypeAgent && kind != manifest.SurfaceTypeCmd {
		return prepareBlockDecision{}, nil
	}

	opts := prepare.Options{Dock: dockName, Bay: bayID}
	manager := e.prepareManager()
	plan, err := manager.Plan(opts)
	if err != nil {
		return prepareBlockDecision{}, err
	}
	if len(plan.Steps) == 0 {
		return prepareBlockDecision{}, nil
	}
	statuses, err := manager.Status(opts)
	if err != nil {
		return prepareBlockDecision{}, err
	}
	statusByName := prepareStatusByName(statuses)

	decision := prepareBlockDecision{}
	var stale []string
	for _, step := range plan.Steps {
		if !prepareStepBlocks(step, kind) {
			continue
		}
		status := statusByName[step.Name]
		switch status.Status {
		case manifest.PrepareStatusReady:
			if err := runPrepareReadyCommand(ctx, plan.BayPath, step); err != nil {
				stale = append(stale, step.Name)
				decision.Blocked = true
				if prepareStepRunsAuto(step) {
					decision.Dispatch = true
				}
			}
		case manifest.PrepareStatusRunning:
			decision.Blocked = true
		case manifest.PrepareStatusFailed:
			decision.Blocked = true
		default:
			decision.Blocked = true
			if prepareStepRunsAuto(step) {
				decision.Dispatch = true
			}
		}
	}
	if len(stale) > 0 {
		if err := manager.MarkStepsStale(opts, stale); err != nil {
			return prepareBlockDecision{}, err
		}
	}
	return decision, nil
}

// DispatchReadyPrepareSurfaces is called by the prepare worker after a step
// is persisted as ready. It swaps any now-unblocked placeholder panes into
// their requested agent/cmd surfaces.
func (e *Engine) DispatchReadyPrepareSurfaces(ctx context.Context, opts prepare.Options, readyStep config.BayPrepareConfig) error {
	if len(readyStep.Blocks) == 0 {
		return nil
	}
	pending, err := e.pendingPrepareLaunches(opts)
	if err != nil {
		return err
	}
	for _, launch := range pending {
		if !prepareStepBlocks(readyStep, launch.Kind) {
			continue
		}
		ready, err := e.prepareReadyForPendingLaunch(ctx, opts, launch.Kind)
		if err != nil {
			return err
		}
		if !ready {
			continue
		}
		if err := e.launchPendingPrepareSurface(opts, launch); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) pendingPrepareLaunches(opts prepare.Options) ([]manifest.PendingLaunch, error) {
	m, err := e.LoadManifest()
	if err != nil {
		return nil, err
	}
	dock := m.FindDock(opts.Dock)
	if dock == nil {
		return nil, fmt.Errorf("unknown dock %q", opts.Dock)
	}
	bay := dock.FindBayByID(opts.Bay)
	if bay == nil {
		return nil, fmt.Errorf("bay %q not found in dock %q", opts.Bay, opts.Dock)
	}
	return append([]manifest.PendingLaunch(nil), bay.PendingSurfaces...), nil
}

func (e *Engine) prepareReadyForPendingLaunch(ctx context.Context, opts prepare.Options, kind manifest.SurfaceType) (bool, error) {
	manager := e.prepareManager()
	plan, err := manager.Plan(opts)
	if err != nil {
		return false, err
	}
	statuses, err := manager.Status(opts)
	if err != nil {
		return false, err
	}
	statusByName := prepareStatusByName(statuses)

	var failed []string
	for _, step := range plan.Steps {
		if !prepareStepBlocks(step, kind) {
			continue
		}
		if statusByName[step.Name].Status != manifest.PrepareStatusReady {
			return false, nil
		}
		if err := runPrepareReadyCommand(ctx, plan.BayPath, step); err != nil {
			failed = append(failed, step.Name)
		}
	}
	if len(failed) > 0 {
		if err := manager.MarkStepsFailed(opts, failed); err != nil {
			return false, err
		}
		return false, fmt.Errorf("prepare ready check failed after rerun: %s", strings.Join(failed, ","))
	}
	return true, nil
}

func (e *Engine) launchPendingPrepareSurface(opts prepare.Options, pending manifest.PendingLaunch) error {
	if pending.Tmux == nil || pending.Tmux.PaneID == "" {
		return e.removePendingPrepareSurface(opts, pending)
	}

	m, err := e.LoadManifest()
	if err != nil {
		return err
	}
	dock := m.FindDock(opts.Dock)
	if dock == nil {
		return fmt.Errorf("unknown dock %q", opts.Dock)
	}
	bay := dock.FindBayByID(opts.Bay)
	if bay == nil {
		return fmt.Errorf("bay %q not found in dock %q", opts.Bay, opts.Dock)
	}
	cwd := homeSurfaceCWD(dock, bay)
	if pending.Kind == manifest.SurfaceTypeAgent {
		if err := e.validateAgentName(pending.Agent); err != nil {
			return err
		}
	}

	surface := surfaceForPendingLaunch(pending)

	var finalName string
	added := false
	err = e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(opts.Dock)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", opts.Dock)
		}
		bay := dock.FindBayByID(opts.Bay)
		if bay == nil {
			return fmt.Errorf("bay %q not found in dock %q", opts.Bay, opts.Dock)
		}
		index := findPendingLaunchIndex(bay.PendingSurfaces, pending)
		if index == -1 {
			return nil
		}
		surface.Name = uniqueSurfaceName(bay, surface.Name)
		if _, err := bay.AddSurface(surface); err != nil {
			return err
		}
		finalName = surface.Name
		added = true
		bay.PendingSurfaces = append(bay.PendingSurfaces[:index], bay.PendingSurfaces[index+1:]...)
		bay.LastActive = time.Now().Unix()
		bay.PendingCloseAt = 0
		return nil
	})
	if err != nil {
		return err
	}
	if !added {
		return nil
	}

	m2, _ := e.LoadManifest()
	agentArgs := e.resolvedAgentArgs(opts.Dock, pending.Agent, m2)
	if _, err := e.launchSurfaceInTmux(pending.Tmux.PaneID, opts.Dock, pending.Kind, pending.Agent, pending.Command, cwd, agentArgs, false); err != nil {
		_ = e.rollbackPendingPrepareLaunch(opts, pending, finalName)
		return err
	}
	if finalName != "" && pending.Tmux.LayoutGroup > 1 && finalName != pending.Name {
		_ = e.Tmux.RenameWindow(pending.Tmux.WindowID, ":"+finalName)
	}
	return nil
}

func surfaceForPendingLaunch(pending manifest.PendingLaunch) manifest.Surface {
	surface := manifest.Surface{
		Name:    pendingSurfaceName(pending),
		Type:    pending.Kind,
		Backend: manifest.SurfaceBackendTmux,
		Tmux:    copyTmuxAttrs(pending.Tmux),
	}
	switch pending.Kind {
	case manifest.SurfaceTypeAgent:
		surface.Agent = &pending.Agent
	case manifest.SurfaceTypeCmd:
		surface.Command = &pending.Command
	}
	return surface
}

func (e *Engine) rollbackPendingPrepareLaunch(opts prepare.Options, pending manifest.PendingLaunch, surfaceName string) error {
	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(opts.Dock)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", opts.Dock)
		}
		bay := dock.FindBayByID(opts.Bay)
		if bay == nil {
			return fmt.Errorf("bay %q not found in dock %q", opts.Bay, opts.Dock)
		}
		if surfaceName != "" {
			_ = bay.RemoveSurface(surfaceName)
		}
		if findPendingLaunchIndex(bay.PendingSurfaces, pending) == -1 {
			bay.PendingSurfaces = append(bay.PendingSurfaces, pending)
		}
		return nil
	})
}

func (e *Engine) removePendingPrepareSurface(opts prepare.Options, pending manifest.PendingLaunch) error {
	return e.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(opts.Dock)
		if dock == nil {
			return fmt.Errorf("unknown dock %q", opts.Dock)
		}
		bay := dock.FindBayByID(opts.Bay)
		if bay == nil {
			return fmt.Errorf("bay %q not found in dock %q", opts.Bay, opts.Dock)
		}
		index := findPendingLaunchIndex(bay.PendingSurfaces, pending)
		if index != -1 {
			bay.PendingSurfaces = append(bay.PendingSurfaces[:index], bay.PendingSurfaces[index+1:]...)
		}
		return nil
	})
}

func prepareStatusByName(statuses []prepare.StepStatus) map[string]prepare.StepStatus {
	byName := map[string]prepare.StepStatus{}
	for _, status := range statuses {
		byName[status.Name] = status
	}
	return byName
}

func prepareStepBlocks(step config.BayPrepareConfig, kind manifest.SurfaceType) bool {
	for _, block := range step.Blocks {
		if block == string(kind) {
			return true
		}
	}
	return false
}

func prepareStepRunsAuto(step config.BayPrepareConfig) bool {
	return step.Run == "" || step.Run == config.PrepareRunAuto
}

func runPrepareReadyCommand(ctx context.Context, bayPath string, step config.BayPrepareConfig) error {
	if len(step.ReadyCommand) == 0 {
		return nil
	}
	runCtx := ctx
	cancel := func() {}
	if timeout, err := step.ParsedTimeout(); err != nil {
		return err
	} else if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, step.ReadyCommand[0], step.ReadyCommand[1:]...)
	if bayPath != "" {
		cmd.Dir = bayPath
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("ready_command timed out after %s: %w", step.Timeout, err)
		}
		if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
			return fmt.Errorf("ready_command failed: %w: %s", err, trimmed)
		}
		return fmt.Errorf("ready_command failed: %w", err)
	}
	return nil
}

func prepareLogFollowerCommand(configPath, dockName, bayID string) string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "bay"
	}
	args := []string{exe}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	args = append(args, "prepare", bayID, "--dock", dockName, "--log", "-f")
	return shellJoin(args)
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

func pendingSurfaceName(pending manifest.PendingLaunch) string {
	if pending.Name != "" {
		return pending.Name
	}
	if pending.Kind == manifest.SurfaceTypeAgent && pending.Agent != "" {
		return pending.Agent
	}
	return string(pending.Kind)
}

func copyTmuxAttrs(attrs *manifest.TmuxAttrs) *manifest.TmuxAttrs {
	if attrs == nil {
		return nil
	}
	return &manifest.TmuxAttrs{
		PaneID:      attrs.PaneID,
		WindowID:    attrs.WindowID,
		LayoutGroup: attrs.LayoutGroup,
		SplitFrom:   attrs.SplitFrom,
		SplitDir:    attrs.SplitDir,
	}
}
