package engine

import (
	"fmt"
	"time"

	"github.com/commontoolsinc/bay/internal/manifest"
)

// homePath returns dock.Path or an error explaining why home is not available.
// Home is rooted at dock.Path; without one, home cannot be materialized.
func homePath(dock *manifest.Dock) (string, error) {
	if dock == nil {
		return "", fmt.Errorf("unknown dock")
	}
	if dock.Path == "" {
		return "", fmt.Errorf("dock %q has no checkout configured; home requires a dock checkout", dock.Name)
	}
	return dock.Path, nil
}

// ensureHomeBay returns the home Bay in dock, persisting a fresh empty
// entry if none exists. The caller must already hold the manifest write
// lock. On success the returned pointer is valid for the duration of the
// caller's manifest closure.
//
// Returns an error when the dock has no checkout, or when something else
// already occupies the reserved home ID/name.
func ensureHomeBay(dock *manifest.Dock) (*manifest.Bay, error) {
	if _, err := homePath(dock); err != nil {
		return nil, err
	}
	if existing := dock.FindBayByID(manifest.HomeBayID); existing != nil {
		if existing.Type != manifest.BayTypeHome {
			return nil, fmt.Errorf("dock %q has a non-home bay with reserved ID %q", dock.Name, manifest.HomeBayID)
		}
		return existing, nil
	}
	if existing := dock.FindBay(manifest.HomeBayID); existing != nil {
		return nil, fmt.Errorf("dock %q has a non-home bay named %q; rename or close it before using home", dock.Name, manifest.HomeBayID)
	}
	home := manifest.SynthesizeHomeBay(dock)
	home.LastActive = time.Now().Unix()
	if err := dock.AddBay(home); err != nil {
		return nil, err
	}
	return dock.FindBayByID(manifest.HomeBayID), nil
}

// removeHomeBayIfEmpty drops a persisted home Bay entry when it has no
// surfaces left. Empty home is a synthesized concept; the persisted entry
// is only meaningful while it carries surfaces.
func removeHomeBayIfEmpty(dock *manifest.Dock) {
	home := dock.FindBayByID(manifest.HomeBayID)
	if home == nil || home.Type != manifest.BayTypeHome || len(home.Surfaces) > 0 {
		return
	}
	_ = dock.RemoveBay(manifest.HomeBayID)
}

// Home focuses the most recent home surface in the dock, creating a home
// shell at the dock checkout when none exists.
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
		if e.focusBaySurface(home) {
			return nil
		}
	}
	return e.SurfaceAdd(SurfaceAddOptions{
		DockName: dockName,
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	})
}

// focusBaySurface selects the bay's last-focused surface (or the most
// recently added) in tmux. Returns true on success, false if no live
// tmux-backed surface could be focused.
func (e *Engine) focusBaySurface(bay *manifest.Bay) bool {
	if bay == nil {
		return false
	}
	if bay.LastFocused != 0 {
		if s := bay.FindSurfaceByID(bay.LastFocused); e.selectSurfaceWindow(s) {
			return true
		}
	}
	for i := len(bay.Surfaces) - 1; i >= 0; i-- {
		if e.selectSurfaceWindow(&bay.Surfaces[i]) {
			return true
		}
	}
	return false
}

func (e *Engine) selectSurfaceWindow(s *manifest.Surface) bool {
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

// addHomeShell creates a home shell surface in dock. SurfaceAdd handles
// placement (first home surface lands at tmux tab index 0). Used as the
// empty-dock surface in place of the `~` placeholder when the dock has a
// checkout configured.
func (e *Engine) addHomeShell(dockName string) error {
	return e.SurfaceAdd(SurfaceAddOptions{
		DockName: dockName,
		BayName:  manifest.HomeBayID,
		Type:     manifest.SurfaceTypeShell,
		Name:     "shell",
	})
}

// ensureDockSession brings the dock's tmux session up if it's not already
// running, persisting any new SessionID marker. Used by the home surface
// path so `bay home` from outside tmux can stand the dock up on demand.
func (e *Engine) ensureDockSession(dockName string) error {
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
