package engine

import (
	"errors"
	"fmt"
	"time"

	"github.com/commontoolsinc/bay/internal/config"
	"github.com/commontoolsinc/bay/internal/manifest"
)

const homeReservedMessage = "home is reserved for the dock checkout; use `bay home`"

func isHomeBayID(id string) bool {
	return manifest.IsHomeBayID(id)
}

func homeReservedError() error {
	return errors.New(homeReservedMessage)
}

func homePath(dock *manifest.Dock) (string, error) {
	if dock == nil {
		return "", fmt.Errorf("unknown dock")
	}
	if dock.Path == "" {
		return "", fmt.Errorf("dock %q has no checkout configured", dock.Name)
	}
	return config.ExpandPath(dock.Path), nil
}

func newHomeBay(dock *manifest.Dock) (manifest.Bay, error) {
	path, err := homePath(dock)
	if err != nil {
		return manifest.Bay{}, err
	}
	return manifest.Bay{
		ID:         manifest.HomeBayID,
		Name:       manifest.HomeBayID,
		Type:       manifest.BayTypeHome,
		Path:       path,
		LastActive: time.Now().Unix(),
		Surfaces:   []manifest.Surface{},
	}, nil
}

func ensureHomeBay(dock *manifest.Dock) (*manifest.Bay, error) {
	if dock == nil {
		return nil, fmt.Errorf("unknown dock")
	}
	path, err := homePath(dock)
	if err != nil {
		return nil, err
	}
	if existing := dock.FindBayByID(manifest.HomeBayID); existing != nil {
		if existing.Type != manifest.BayTypeHome {
			return nil, fmt.Errorf("dock %q has a non-home bay with reserved ID %q", dock.Name, manifest.HomeBayID)
		}
		existing.Name = manifest.HomeBayID
		existing.Path = path
		existing.Worktree = nil
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

func removeHomeBayIfEmpty(dock *manifest.Dock) bool {
	home := dock.FindBayByID(manifest.HomeBayID)
	if home == nil || home.Type != manifest.BayTypeHome || len(home.Surfaces) > 0 {
		return false
	}
	_ = dock.RemoveBay(manifest.HomeBayID)
	return true
}

func dockHasSurfacesExcept(dock *manifest.Dock, bayID string) bool {
	if dock == nil {
		return false
	}
	if len(dock.Surfaces) > 0 {
		return true
	}
	for i := range dock.Bays {
		bay := &dock.Bays[i]
		if bay.ID == bayID {
			continue
		}
		if len(bay.Surfaces) > 0 {
			return true
		}
	}
	return false
}

// Home focuses the most recent home surface in a dock, creating a home shell
// at the dock checkout when no home surface exists.
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

func (e *Engine) ensureSessionForDock(dockName string) error {
	m, err := e.LoadManifest()
	if err != nil {
		return err
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		return fmt.Errorf("unknown dock %q", dockName)
	}
	sessionID, err := e.ensureSession(dockName, dock.SessionID)
	if err != nil {
		return err
	}
	if sessionID == dock.SessionID {
		return nil
	}
	return e.withManifestMaybe(func(m *manifest.Manifest) (bool, error) {
		dock := m.FindDock(dockName)
		if dock == nil {
			return false, fmt.Errorf("unknown dock %q", dockName)
		}
		if dock.SessionID == sessionID {
			return false, nil
		}
		dock.SessionID = sessionID
		return true, nil
	})
}

// ensureHomeIfLastWindow makes sure a dock with a checkout still has a tmux
// surface when a non-home close is about to remove its last real window.
func (e *Engine) ensureHomeIfLastWindow(dockName, closingWindowID string, closingHome bool) {
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
	if remaining > 0 || closingHome {
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
