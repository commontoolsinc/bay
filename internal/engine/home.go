package engine

import (
	"fmt"
	"time"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

func homePath(dock *manifest.Dock) (string, error) {
	if dock == nil {
		return "", fmt.Errorf("unknown dock")
	}
	if dock.Path == "" {
		return "", fmt.Errorf("dock %q has no checkout configured", dock.Name)
	}
	return dock.Path, nil
}

func homeSurfaceCWD(bay *manifest.Bay) string {
	if bay != nil && bay.Type == manifest.BayTypeHome {
		return config.ExpandPath(bay.Path)
	}
	if bay == nil {
		return ""
	}
	return bay.Path
}

func newHomeBay(dock *manifest.Dock) (manifest.Bay, error) {
	if _, err := homePath(dock); err != nil {
		return manifest.Bay{}, err
	}
	home := manifest.SynthesizeHomeBay(dock)
	home.LastActive = time.Now().Unix()
	return home, nil
}

func ensureHomeBay(dock *manifest.Dock) (*manifest.Bay, error) {
	if _, err := homePath(dock); err != nil {
		return nil, err
	}
	if existing := dock.FindBayByID(manifest.HomeBayID); existing != nil {
		if err := validateResolvableHome(dock); err != nil {
			return nil, err
		}
		return existing, nil
	}
	if existing := dock.FindBay(manifest.HomeBayID); existing != nil {
		return nil, fmt.Errorf("dock %q has a non-home bay named %q; rename or close it before using home", dock.Name, manifest.HomeBayID)
	}
	home, err := newHomeBay(dock)
	if err != nil {
		return nil, err
	}
	if err := dock.AddBay(home); err != nil {
		return nil, err
	}
	return dock.FindBayByID(manifest.HomeBayID), nil
}

func validateResolvableHome(dock *manifest.Dock) error {
	m := &manifest.Manifest{Docks: []manifest.Dock{*dock}}
	_, _, err := m.ResolveBay(dock.Name + ":" + manifest.HomeBayID)
	return err
}

func removeHomeBayIfEmpty(dock *manifest.Dock) bool {
	if dock == nil {
		return false
	}
	home := dock.FindBayByID(manifest.HomeBayID)
	if home == nil || home.Type != manifest.BayTypeHome || len(home.Surfaces) > 0 {
		return false
	}
	_ = dock.RemoveBay(manifest.HomeBayID)
	return true
}

func lastHomeCloseError(dockName string) error {
	return fmt.Errorf("closing the last home surface in dock %q is not implemented until the home-bay close confirmation phase; use `bay dock close %s` to close the dock", dockName, dockName)
}

func (e *Engine) rejectIfLastHomeWindowClose(dockName string, closingWindowIDs []string) error {
	if len(closingWindowIDs) == 0 {
		return nil
	}
	closing := map[string]bool{}
	for _, id := range closingWindowIDs {
		if id != "" {
			closing[id] = true
		}
	}
	windows, err := e.Tmux.ListWindows(dockName)
	if err != nil {
		return fmt.Errorf("checking remaining dock windows: %w", err)
	}
	for _, win := range windows {
		if closing[win.ID] {
			continue
		}
		val, _ := e.Tmux.GetWindowOption(win.ID, "@bay-placeholder")
		if val != "1" {
			return nil
		}
	}
	return lastHomeCloseError(dockName)
}

// Home focuses the most recent home surface in a dock, creating a home shell
// at the dock checkout when no focusable home surface exists.
func (e *Engine) Home(dockName string) error {
	e.SyncAll()

	m, err := e.LoadManifest()
	if err != nil {
		return err
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	if _, err := homePath(dock); err != nil {
		return err
	}
	if home := dock.FindBayByID(manifest.HomeBayID); home != nil && len(home.Surfaces) > 0 {
		if e.focusHomeSurface(home) {
			return nil
		}
	}
	return e.SurfaceAdd(SurfaceAddOptions{
		DockName: dockName,
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
		SplitDir: "",
	})
}

func (e *Engine) focusHomeSurface(home *manifest.Bay) bool {
	if home == nil {
		return false
	}
	if home.LastFocused != 0 {
		if s := home.FindSurfaceByID(home.LastFocused); e.selectSurface(s) {
			return true
		}
	}
	for i := len(home.Surfaces) - 1; i >= 0; i-- {
		if e.selectSurface(&home.Surfaces[i]) {
			return true
		}
	}
	return false
}

func (e *Engine) selectSurface(s *manifest.Surface) bool {
	if s == nil || s.Tmux == nil || s.Tmux.WindowID == "" {
		return false
	}
	if err := e.Tmux.SelectWindow(s.Tmux.WindowID); err != nil {
		return false
	}
	if s.Tmux.PaneID != "" {
		_ = e.Tmux.SelectPane(s.Tmux.PaneID)
	}
	return true
}

func (e *Engine) ensureDockSessionForHomeSurface(dockName string, dock *manifest.Dock) error {
	if dockHasRecordedTmuxSurfaces(dock) {
		_, _, err := e.ensureSessionForBay(dockName, dock.SessionID)
		return err
	}
	sessionID, err := e.ensureSession(dockName, dock.SessionID)
	if err != nil {
		return err
	}
	return e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		dock := m.FindDock(dockName)
		if dock == nil {
			return false, fmt.Errorf("unknown dock %q", dockName)
		}
		if dockHasRecordedTmuxSurfaces(dock) || dock.SessionID == sessionID {
			return false, nil
		}
		dock.SessionID = sessionID
		return true, nil
	})
}

// ensureHomeIfLastWindow creates a home shell when a non-home window close is
// about to leave a checkout-backed dock without any real tmux windows.
func (e *Engine) ensureHomeIfLastWindow(dockName, closingWindowID string) {
	windows, err := e.Tmux.ListWindows(dockName)
	if err != nil {
		return
	}
	remaining := 0
	for _, win := range windows {
		if win.ID == closingWindowID {
			continue
		}
		val, _ := e.Tmux.GetWindowOption(win.ID, "@bay-placeholder")
		if val != "1" {
			remaining++
		}
	}
	if remaining > 0 {
		return
	}
	m, err := e.LoadManifest()
	if err != nil {
		return
	}
	dock := m.FindDock(dockName)
	if dock == nil || dock.Path == "" {
		e.ensurePlaceholderIfLastWindow(dockName, closingWindowID)
		return
	}
	_ = e.SurfaceAdd(SurfaceAddOptions{
		DockName: dockName,
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
		SplitDir: "",
	})
}
