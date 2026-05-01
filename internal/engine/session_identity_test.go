package engine

import (
	"testing"

	"github.com/commontoolsinc/bay/internal/manifest"
	"github.com/commontoolsinc/bay/internal/tmux"
)

// seedSurfaceWorkspace adds a workspace with one surface whose recorded
// PaneID does not exist in the mock tmux server. With normal cleanup
// enabled (matching marker), SyncAll would mark the surface dead and
// strip it. The tests below toggle the marker state to verify the
// gate's owned/preserve decisions.
func seedSurfaceWorkspace(t *testing.T, eng *Engine, dockName, wsName string) {
	t.Helper()
	err := eng.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		dock.Workspaces = append(dock.Workspaces, manifest.Workspace{
			Name: wsName,
			Type: manifest.WorkspaceTypeWorktree,
			Surfaces: []manifest.Surface{
				{
					ID:      1,
					Name:    "agent",
					Type:    manifest.SurfaceTypeAgent,
					Backend: manifest.SurfaceBackendTmux,
					Tmux: &manifest.TmuxAttrs{
						WindowID: "@99",
						PaneID:   "%999", // not in the mock — would be flagged dead
					},
				},
			},
		})
		return nil
	})
	if err != nil {
		t.Fatalf("seed surface workspace: %v", err)
	}
}

// setSessionID sets the manifest's Dock.SessionID for a dock.
func setSessionID(t *testing.T, eng *Engine, dockName, sessionID string) {
	t.Helper()
	err := eng.withManifest(func(m *manifest.Manifest) error {
		dock := m.FindDock(dockName)
		dock.SessionID = sessionID
		return nil
	})
	if err != nil {
		t.Fatalf("set session id: %v", err)
	}
}

// surfaceCount returns the surface count for a workspace.
func surfaceCount(t *testing.T, eng *Engine, dockName, wsName string) int {
	t.Helper()
	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	dock := m.FindDock(dockName)
	if dock == nil {
		t.Fatalf("dock %q missing", dockName)
	}
	ws := dock.FindWorkspace(wsName)
	if ws == nil {
		t.Fatalf("workspace %q missing", wsName)
	}
	return len(ws.Surfaces)
}

// --- gate matrix ---

// TestSyncAll_PreservesSurfacesAfterServerRestart simulates the user-
// reported bug: tmux died and a fresh empty session of the same name
// came back up (e.g., user attached, wrapper script started a session).
// The manifest still has the pre-crash SessionID; tmux has no marker.
// SyncAll must NOT strip surfaces — recover hasn't run yet.
func TestSyncAll_PreservesSurfacesAfterServerRestart(t *testing.T) {
	eng, _ := testEngine(t)
	seedSurfaceWorkspace(t, eng, "labs", "auth-fix")
	setSessionID(t, eng, "labs", "old-session-uuid")

	// Tmux has the dock's session name but no @bay-session-id marker
	// (fresh session after a restart).
	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.NewSession("labs"); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	eng.SyncAll()

	if got := surfaceCount(t, eng, "labs", "auth-fix"); got != 1 {
		t.Errorf("surfaces stripped after restart: got %d, want 1 (preserved)", got)
	}
}

// TestSyncAll_PreservesSurfacesWhenMarkerSetButManifestEmpty covers
// the in-flight recover case: ensureSession has tagged tmux with a new
// UUID, but mergeRecoveredDockState hasn't persisted SessionID yet.
// A probe that fires in this window must not strip surfaces.
func TestSyncAll_PreservesSurfacesWhenMarkerSetButManifestEmpty(t *testing.T) {
	eng, _ := testEngine(t)
	seedSurfaceWorkspace(t, eng, "labs", "auth-fix")
	// Manifest SessionID stays empty (testEngine seeds it that way) —
	// simulates pre-merge state during recover.

	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.NewSession("labs"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := mockTmux.SetSessionOption("labs", sessionIDOption, "fresh-uuid"); err != nil {
		t.Fatalf("set marker: %v", err)
	}

	// But the dock had a prior SessionID — without one, the (empty,
	// empty) → owned legacy branch would fire instead.
	setSessionID(t, eng, "labs", "")

	// Force the (set marker, empty manifest) state for a meaningful test.
	// Manifest is already empty; tmux now has the marker.
	eng.SyncAll()

	if got := surfaceCount(t, eng, "labs", "auth-fix"); got != 1 {
		t.Errorf("surfaces stripped during in-flight recover: got %d, want 1 (preserved)", got)
	}
}

// TestSyncAll_PreservesSurfacesOnMismatchedMarker covers the post-
// crash case where bay's recover ran on a different bay process or
// session: tmux was re-tagged with a new UUID while the manifest
// kept the old one.
func TestSyncAll_PreservesSurfacesOnMismatchedMarker(t *testing.T) {
	eng, _ := testEngine(t)
	seedSurfaceWorkspace(t, eng, "labs", "auth-fix")
	setSessionID(t, eng, "labs", "manifest-uuid")

	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.NewSession("labs"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := mockTmux.SetSessionOption("labs", sessionIDOption, "different-uuid"); err != nil {
		t.Fatalf("set marker: %v", err)
	}

	eng.SyncAll()

	if got := surfaceCount(t, eng, "labs", "auth-fix"); got != 1 {
		t.Errorf("surfaces stripped on mismatched marker: got %d, want 1 (preserved)", got)
	}
}

// TestSyncAll_StripsDeadSurfacesWhenMarkerMatches verifies normal
// operation: when the manifest's SessionID matches tmux's marker, the
// gate opens and dead pane IDs (e.g., panes the user closed manually)
// get stripped as before.
func TestSyncAll_StripsDeadSurfacesWhenMarkerMatches(t *testing.T) {
	eng, _ := testEngine(t)
	seedSurfaceWorkspace(t, eng, "labs", "auth-fix")
	setSessionID(t, eng, "labs", "shared-uuid")

	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.NewSession("labs"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := mockTmux.SetSessionOption("labs", sessionIDOption, "shared-uuid"); err != nil {
		t.Fatalf("set marker: %v", err)
	}

	eng.SyncAll()

	if got := surfaceCount(t, eng, "labs", "auth-fix"); got != 0 {
		t.Errorf("dead surface not stripped on matching marker: got %d, want 0", got)
	}
}

// TestSyncAll_StripsDeadSurfacesInLegacyEmptyState verifies the
// backward-compat branch: pre-rollout manifests have no SessionID and
// live tmux sessions have no marker. The gate must continue to behave
// as today (strip dead surfaces) so existing docks don't regress.
func TestSyncAll_StripsDeadSurfacesInLegacyEmptyState(t *testing.T) {
	eng, _ := testEngine(t)
	seedSurfaceWorkspace(t, eng, "labs", "auth-fix")
	// SessionID stays empty.

	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.NewSession("labs"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	// No marker set.

	eng.SyncAll()

	if got := surfaceCount(t, eng, "labs", "auth-fix"); got != 0 {
		t.Errorf("legacy state did not strip dead surfaces: got %d, want 0", got)
	}
}

// TestSyncAll_PreservesSurfacesWhenSessionFullyDead is today's
// existing protection — verified to still work after the rewrite.
func TestSyncAll_PreservesSurfacesWhenSessionFullyDead(t *testing.T) {
	eng, _ := testEngine(t)
	seedSurfaceWorkspace(t, eng, "labs", "auth-fix")
	setSessionID(t, eng, "labs", "any-uuid")
	// No tmux session at all.

	eng.SyncAll()

	if got := surfaceCount(t, eng, "labs", "auth-fix"); got != 1 {
		t.Errorf("surfaces stripped when session is dead: got %d, want 1 (preserved)", got)
	}
}

// --- ensureSession matrix ---

// withDeterministicSessionID swaps newSessionID for a counter-based
// generator so tests can predict the assigned UUID. Returns a restore
// func to call in t.Cleanup.
func withDeterministicSessionID(t *testing.T, ids ...string) {
	t.Helper()
	prev := newSessionID
	idx := 0
	newSessionID = func() string {
		if idx >= len(ids) {
			t.Fatalf("newSessionID called more times than expected: %d > %d", idx+1, len(ids))
		}
		id := ids[idx]
		idx++
		return id
	}
	t.Cleanup(func() { newSessionID = prev })
}

// TestEnsureSession_TagsNewSession exercises the "no session" branch:
// session is created and tagged with a fresh UUID.
func TestEnsureSession_TagsNewSession(t *testing.T) {
	eng, _ := testEngine(t)
	withDeterministicSessionID(t, "fresh-uuid")

	got, err := eng.ensureSession("new-dock", "")
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if got != "fresh-uuid" {
		t.Errorf("returned id = %q, want fresh-uuid", got)
	}
	mockTmux := eng.Tmux.(*tmux.Mock)
	marker, _ := mockTmux.GetSessionOption("new-dock", sessionIDOption)
	if marker != "fresh-uuid" {
		t.Errorf("tmux marker = %q, want fresh-uuid", marker)
	}
}

// TestEnsureSession_NoOpOnMatchingMarker covers the steady-state case:
// session is alive, marker matches expected — no work, no UUID churn.
func TestEnsureSession_NoOpOnMatchingMarker(t *testing.T) {
	eng, _ := testEngine(t)

	mockTmux := eng.Tmux.(*tmux.Mock)
	_ = mockTmux.NewSession("labs")
	_ = mockTmux.SetSessionOption("labs", sessionIDOption, "stable-uuid")

	got, err := eng.ensureSession("labs", "stable-uuid")
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if got != "stable-uuid" {
		t.Errorf("returned id = %q, want stable-uuid", got)
	}
	// Marker unchanged.
	marker, _ := mockTmux.GetSessionOption("labs", sessionIDOption)
	if marker != "stable-uuid" {
		t.Errorf("marker churned: %q, want stable-uuid", marker)
	}
}

// TestEnsureSession_AdoptsExistingMarker covers (set marker, empty
// expected): tmux already carries a UUID but the manifest hasn't
// recorded it. The function adopts the existing marker rather than
// minting a new one.
func TestEnsureSession_AdoptsExistingMarker(t *testing.T) {
	eng, _ := testEngine(t)

	mockTmux := eng.Tmux.(*tmux.Mock)
	_ = mockTmux.NewSession("labs")
	_ = mockTmux.SetSessionOption("labs", sessionIDOption, "existing-uuid")

	got, err := eng.ensureSession("labs", "")
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if got != "existing-uuid" {
		t.Errorf("returned id = %q, want existing-uuid (adopted)", got)
	}
}

// TestEnsureSession_ClaimsUntaggedSession covers (empty marker, empty
// expected): the legacy state. The function tags the session so future
// probes have something to verify.
func TestEnsureSession_ClaimsUntaggedSession(t *testing.T) {
	eng, _ := testEngine(t)
	withDeterministicSessionID(t, "claimed-uuid")

	mockTmux := eng.Tmux.(*tmux.Mock)
	_ = mockTmux.NewSession("labs")

	got, err := eng.ensureSession("labs", "")
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if got != "claimed-uuid" {
		t.Errorf("returned id = %q, want claimed-uuid", got)
	}
	marker, _ := mockTmux.GetSessionOption("labs", sessionIDOption)
	if marker != "claimed-uuid" {
		t.Errorf("marker = %q, want claimed-uuid", marker)
	}
}

// TestEnsureSession_ReclaimsMismatchedMarker covers the post-restart
// case where bay recovers a session whose marker doesn't match what
// the manifest recorded. The function re-tags with a fresh UUID; the
// caller persists at the right moment.
func TestEnsureSession_ReclaimsMismatchedMarker(t *testing.T) {
	eng, _ := testEngine(t)
	withDeterministicSessionID(t, "reclaimed-uuid")

	mockTmux := eng.Tmux.(*tmux.Mock)
	_ = mockTmux.NewSession("labs")
	_ = mockTmux.SetSessionOption("labs", sessionIDOption, "stranded-uuid")

	got, err := eng.ensureSession("labs", "manifest-uuid")
	if err != nil {
		t.Fatalf("ensureSession: %v", err)
	}
	if got != "reclaimed-uuid" {
		t.Errorf("returned id = %q, want reclaimed-uuid", got)
	}
	marker, _ := mockTmux.GetSessionOption("labs", sessionIDOption)
	if marker != "reclaimed-uuid" {
		t.Errorf("marker = %q, want reclaimed-uuid", marker)
	}
}

// --- DockNew populates SessionID ---

// TestDockNew_PopulatesSessionID locks in that creating a dock writes
// SessionID to the manifest and tags the live session — a downstream
// SyncAll would then see matching marker/manifest and behave normally.
func TestDockNew_PopulatesSessionID(t *testing.T) {
	eng, _ := testEngine(t)
	withDeterministicSessionID(t, "new-dock-uuid")
	checkout := makeCheckout(t, t.TempDir())

	if err := eng.DockNew("new-dock", checkout, "", "", ""); err != nil {
		t.Fatalf("DockNew: %v", err)
	}

	m, err := eng.LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	dock := m.FindDock("new-dock")
	if dock == nil {
		t.Fatalf("new-dock missing from manifest")
	}
	if dock.SessionID != "new-dock-uuid" {
		t.Errorf("dock.SessionID = %q, want new-dock-uuid", dock.SessionID)
	}
	mockTmux := eng.Tmux.(*tmux.Mock)
	marker, _ := mockTmux.GetSessionOption("new-dock", sessionIDOption)
	if marker != "new-dock-uuid" {
		t.Errorf("marker = %q, want new-dock-uuid", marker)
	}
}

// --- mid-recover race ---

// TestSyncAll_NoMidRecoverCleanup is the load-bearing test: it
// reproduces the race we engineered the design around. ensureSession
// has tagged tmux with the new UUID, but mergeRecoveredDockState
// hasn't fired yet — so the manifest still has the pre-crash SessionID
// AND the pre-crash pane IDs. A probe in this window must not
// interpret "marker matches manifest? → cleanup" because manifest's
// pane IDs are stale and would all look dead.
//
// We force the pre-merge state by setting SessionID on the manifest
// to the OLD value while tmux has the NEW value. The gate sees
// mismatched → owned=false → preserve.
func TestSyncAll_NoMidRecoverCleanup(t *testing.T) {
	eng, _ := testEngine(t)
	seedSurfaceWorkspace(t, eng, "labs", "auth-fix")
	setSessionID(t, eng, "labs", "pre-crash-uuid")

	mockTmux := eng.Tmux.(*tmux.Mock)
	if err := mockTmux.NewSession("labs"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	// Tmux has been re-tagged by ensureSession but mergeRecoveredDockState
	// hasn't run yet — manifest still points at the old SessionID AND old
	// (now-dangling) pane IDs.
	_ = mockTmux.SetSessionOption("labs", sessionIDOption, "post-recover-uuid")

	eng.SyncAll()

	if got := surfaceCount(t, eng, "labs", "auth-fix"); got != 1 {
		t.Errorf("mid-recover race: surfaces stripped, got %d, want 1 (preserved)", got)
	}
}
