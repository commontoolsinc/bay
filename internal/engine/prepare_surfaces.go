package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
		switch statusByName[step.Name].Status {
		case manifest.PrepareStatusReady:
			if err := runPrepareReadyCommand(ctx, plan.BayPath, step); err != nil {
				stale = append(stale, step.Name)
				decision.Blocked = true
				if prepareStepRunsAuto(step) {
					decision.Dispatch = true
				}
			}
		case manifest.PrepareStatusRunning, manifest.PrepareStatusFailed:
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
	if len(pending) == 0 {
		return nil
	}

	kindsToCheck := map[manifest.SurfaceType]bool{}
	for _, launch := range pending {
		if prepareStepBlocks(readyStep, launch.Kind) {
			kindsToCheck[launch.Kind] = true
		}
	}
	if len(kindsToCheck) == 0 {
		return nil
	}

	readyByKind, err := e.prepareKindReadiness(ctx, opts, kindsToCheck)
	if err != nil {
		return err
	}

	for _, launch := range pending {
		if !readyByKind[launch.Kind] {
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
	_, bay, err := findDockBay(m, opts.Dock, opts.Bay)
	if err != nil {
		return nil, err
	}
	// Copy so the caller can iterate even though launchPendingPrepareSurface
	// will mutate bay.PendingSurfaces via withManifest.
	return append([]manifest.PendingLaunch(nil), bay.PendingSurfaces...), nil
}

// prepareKindReadiness reports, for each kind in kinds, whether all blocking
// steps for that kind are ready. Ready-status steps are re-verified via
// ready_command once per step (regardless of how many launches block on it);
// any whose ready_command fails are marked failed and excluded.
func (e *Engine) prepareKindReadiness(ctx context.Context, opts prepare.Options, kinds map[manifest.SurfaceType]bool) (map[manifest.SurfaceType]bool, error) {
	manager := e.prepareManager()
	plan, err := manager.Plan(opts)
	if err != nil {
		return nil, err
	}
	statuses, err := manager.Status(opts)
	if err != nil {
		return nil, err
	}
	statusByName := prepareStatusByName(statuses)

	// Cache ready_command results so steps shared across kinds run only once.
	verified := map[string]bool{}
	failedSet := map[string]bool{}
	ready := map[manifest.SurfaceType]bool{}
	for kind := range kinds {
		kindReady := true
		for _, step := range plan.Steps {
			if !prepareStepBlocks(step, kind) {
				continue
			}
			if statusByName[step.Name].Status != manifest.PrepareStatusReady {
				kindReady = false
				break
			}
			if failedSet[step.Name] {
				kindReady = false
				break
			}
			if !verified[step.Name] {
				if err := runPrepareReadyCommand(ctx, plan.BayPath, step); err != nil {
					failedSet[step.Name] = true
					kindReady = false
					break
				}
				verified[step.Name] = true
			}
		}
		if kindReady {
			ready[kind] = true
		}
	}
	if len(failedSet) > 0 {
		failed := make([]string, 0, len(failedSet))
		for name := range failedSet {
			failed = append(failed, name)
		}
		if err := manager.MarkStepsFailed(opts, failed); err != nil {
			return ready, err
		}
		return ready, fmt.Errorf("prepare ready check failed after rerun: %s", strings.Join(failed, ","))
	}
	return ready, nil
}

func (e *Engine) launchPendingPrepareSurface(opts prepare.Options, pending manifest.PendingLaunch) error {
	if pending.Tmux == nil || pending.Tmux.PaneID == "" {
		return e.removePendingPrepareSurface(opts, pending)
	}
	if pending.Kind == manifest.SurfaceTypeAgent {
		if err := e.validateAgentName(pending.Agent); err != nil {
			return err
		}
	}

	var cwd, finalName string
	var agentArgs []string
	added := false
	err := e.withManifest(func(m *manifest.Manifest) error {
		dock, bay, err := findDockBay(m, opts.Dock, opts.Bay)
		if err != nil {
			return err
		}
		index := findPendingLaunchIndex(bay.PendingSurfaces, pending)
		if index == -1 {
			return nil
		}
		cwd = homeSurfaceCWD(dock, bay)
		agentArgs = e.resolvedAgentArgs(opts.Dock, pending.Agent, m)

		surface := surfaceForPendingLaunch(pending)
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
		Tmux:    pending.Tmux.Clone(),
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
		_, bay, err := findDockBay(m, opts.Dock, opts.Bay)
		if err != nil {
			return err
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
		_, bay, err := findDockBay(m, opts.Dock, opts.Bay)
		if err != nil {
			return err
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
	return slices.Contains(step.Blocks, string(kind))
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
