package palette

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// recentCap is the maximum number of recent entries stored. All frequency
// counts are computed over this window, so a larger cap gives more stable
// frequency ranking at the cost of letting old habits linger.
const recentCap = 20

// RecentShown is the number of slots the palette reserves for recents at
// the top of its list.
const RecentShown = 5

// AgentTypesCap bounds the sub-picker MRU for agent types.
const AgentTypesCap = 5

// RecentEntry is one top-level recent usage. Param is populated only for
// sub-picker parametrics (stored fully bound); text-input parametrics and
// non-parametric entries leave it empty.
type RecentEntry struct {
	ID    string `json:"id"`
	Param string `json:"param,omitempty"`
	TS    int64  `json:"ts"`
}

// agentTypeEntry is a sub-picker MRU entry for agent types.
type agentTypeEntry struct {
	Value string `json:"value"`
	TS    int64  `json:"ts"`
}

// Recents tracks MRU and frequency for palette usage. The file format is
// versioned; on-disk files from unknown versions are treated as empty so
// a schema bump never crashes the palette.
type Recents struct {
	path string

	Version    int              `json:"version"`
	Recent     []RecentEntry    `json:"recent,omitempty"`
	AgentTypes []agentTypeEntry `json:"agent_types,omitempty"`
}

// LoadRecents reads the recents file at path. A missing or malformed file
// returns an empty (but still usable) Recents bound to that path.
func LoadRecents(path string) *Recents {
	r := &Recents{path: path, Version: 1}
	data, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	var parsed Recents
	if err := json.Unmarshal(data, &parsed); err != nil {
		return r
	}
	if parsed.Version != 1 {
		return r
	}
	parsed.path = path
	return &parsed
}

// Save writes the recents file atomically (temp + rename), matching the
// manifest.json write pattern. Multiple sessions racing is fine: last
// writer wins — the file is best-effort, not canonical state.
func (r *Recents) Save() error {
	if r.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	r.Version = 1
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// Record adds a successful usage of (id, param) to the top-level MRU.
// Param binds the chosen value for sub-picker parametrics; callers should
// pass "" for non-parametric and text-input entries.
func (r *Recents) Record(id, param string) {
	r.recordAt(id, param, time.Now().Unix())
}

func (r *Recents) recordAt(id, param string, ts int64) {
	// Log-style: every usage prepended, cap at recentCap. Duplicates are
	// intentional — frequency ranking for the body slots reads from this
	// window, so dropping them would flatten everything to equal-weight.
	entry := RecentEntry{ID: id, Param: param, TS: ts}
	next := append([]RecentEntry{entry}, r.Recent...)
	if len(next) > recentCap {
		next = next[:recentCap]
	}
	r.Recent = next
}

// RecordAgentType adds value to the agent-type sub-picker MRU.
func (r *Recents) RecordAgentType(value string) {
	r.recordAgentTypeAt(value, time.Now().Unix())
}

func (r *Recents) recordAgentTypeAt(value string, ts int64) {
	// Pure MRU: dedupe so a single value doesn't dominate the 5 slots.
	entry := agentTypeEntry{Value: value, TS: ts}
	deduped := []agentTypeEntry{entry}
	for _, e := range r.AgentTypes {
		if e.Value == value {
			continue
		}
		deduped = append(deduped, e)
		if len(deduped) >= AgentTypesCap {
			break
		}
	}
	r.AgentTypes = deduped
}

// TopRecents returns up to RecentShown entries for display at the top of
// the palette. Slot 1 is the strict MRU; the rest are ranked by frequency
// across the stored window (ties broken by most-recent use). The MRU is
// deduped from the body so the list never shows the same entry twice.
//
// valid is a staleness filter: callers pass a predicate that returns true
// if (id, param) still resolves to a live command. Entries that fail the
// predicate are silently skipped (e.g., a command removed in a future
// version, or an agent removed from config).
func (r *Recents) TopRecents(valid func(id, param string) bool) []RecentEntry {
	if valid == nil {
		valid = func(string, string) bool { return true }
	}
	type key struct{ id, param string }

	var filtered []RecentEntry
	counts := map[key]int{}
	latest := map[key]int64{}
	for _, e := range r.Recent {
		if !valid(e.ID, e.Param) {
			continue
		}
		filtered = append(filtered, e)
		k := key{e.ID, e.Param}
		counts[k]++
		if e.TS > latest[k] {
			latest[k] = e.TS
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	mru := filtered[0]
	mruKey := key{mru.ID, mru.Param}

	type scored struct {
		k     key
		count int
		ts    int64
	}
	var body []scored
	seen := map[key]bool{mruKey: true}
	for _, e := range filtered {
		k := key{e.ID, e.Param}
		if seen[k] {
			continue
		}
		seen[k] = true
		body = append(body, scored{k: k, count: counts[k], ts: latest[k]})
	}
	sort.SliceStable(body, func(i, j int) bool {
		if body[i].count != body[j].count {
			return body[i].count > body[j].count
		}
		return body[i].ts > body[j].ts
	})

	out := []RecentEntry{mru}
	for i := 0; i < len(body) && len(out) < RecentShown; i++ {
		out = append(out, RecentEntry{
			ID:    body[i].k.id,
			Param: body[i].k.param,
			TS:    body[i].ts,
		})
	}
	return out
}

// TopAgentTypes returns agent-type values from the sub-picker MRU, most
// recent first. Values no longer in available are skipped.
func (r *Recents) TopAgentTypes(available map[string]bool) []string {
	out := make([]string, 0, len(r.AgentTypes))
	for _, e := range r.AgentTypes {
		if available != nil && !available[e.Value] {
			continue
		}
		out = append(out, e.Value)
	}
	return out
}
