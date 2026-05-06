package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRepoLocal_Absent(t *testing.T) {
	dir := t.TempDir()
	rlc, err := LoadRepoLocal(dir)
	if err != nil {
		t.Fatalf("LoadRepoLocal on dir without .bay.toml: %v", err)
	}
	if rlc != nil {
		t.Fatalf("LoadRepoLocal: want nil for missing file, got %#v", rlc)
	}
}

func TestLoadRepoLocal_Present(t *testing.T) {
	dir := t.TempDir()
	body := `
[[bay_prepare]]
name = "vendors"
command = [".ops/bin/fetch-vendor.ts", "labs"]
ready_command = [".ops/bin/loom", "vendors-ready", "labs"]
blocks = ["agent", "cmd"]
timeout = "10m"
`
	writeFile(t, filepath.Join(dir, ".bay.toml"), body)
	rlc, err := LoadRepoLocal(dir)
	if err != nil {
		t.Fatalf("LoadRepoLocal: %v", err)
	}
	if rlc == nil || len(rlc.BayPrepare) != 1 {
		t.Fatalf("LoadRepoLocal: want 1 entry, got %#v", rlc)
	}
	got := rlc.BayPrepare[0]
	if got.Name != "vendors" {
		t.Errorf("name = %q, want vendors", got.Name)
	}
	if len(got.Command) != 2 || got.Command[0] != ".ops/bin/fetch-vendor.ts" {
		t.Errorf("command = %v, want [.ops/bin/fetch-vendor.ts labs]", got.Command)
	}
	if len(got.Blocks) != 2 || got.Blocks[0] != "agent" {
		t.Errorf("blocks = %v, want [agent cmd]", got.Blocks)
	}
	if got.Timeout != "10m" {
		t.Errorf("timeout = %q, want 10m", got.Timeout)
	}
}

func TestParsedTimeout(t *testing.T) {
	cases := []struct {
		in      string
		want    string // duration.String() of the parsed value, or "" for unset
		wantErr bool
	}{
		{"", "", false},
		{"10m", "10m0s", false},
		{"1h30m", "1h30m0s", false},
		{"not-a-duration", "", true},
	}
	for _, c := range cases {
		got, err := BayPrepareConfig{Timeout: c.in}.ParsedTimeout()
		if c.wantErr {
			if err == nil {
				t.Errorf("ParsedTimeout(%q): want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParsedTimeout(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.String() != c.want && !(c.want == "" && got == 0) {
			t.Errorf("ParsedTimeout(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseDockConfigBayPrepare(t *testing.T) {
	body := `
[docks.loom]
agent = "claude"
trust_repo_bay_toml = false

[[docks.loom.bay_prepare]]
name = "vendors"
timeout = "20m"
`
	cfg, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	dc, ok := cfg.Docks["loom"]
	if !ok {
		t.Fatalf("dock loom not parsed")
	}
	if dc.TrustRepoBayToml == nil || *dc.TrustRepoBayToml != false {
		t.Errorf("TrustRepoBayToml = %v, want explicit false", dc.TrustRepoBayToml)
	}
	if len(dc.BayPrepare) != 1 || dc.BayPrepare[0].Name != "vendors" {
		t.Errorf("BayPrepare = %#v, want [vendors]", dc.BayPrepare)
	}
	if dc.BayPrepare[0].Timeout != "20m" {
		t.Errorf("Timeout = %q, want 20m", dc.BayPrepare[0].Timeout)
	}
}

func TestParseBayLevelTrust(t *testing.T) {
	body := `trust_repo_bay_toml = true`
	cfg, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.TrustRepoBayToml == nil || *cfg.TrustRepoBayToml != true {
		t.Errorf("TrustRepoBayToml = %v, want explicit true", cfg.TrustRepoBayToml)
	}
}

func TestMergeBayPrepare_PerFieldOverride(t *testing.T) {
	repoLocal := []BayPrepareConfig{
		{
			Name:         "vendors",
			Command:      []string{".ops/bin/fetch-vendor.ts", "labs"},
			ReadyCommand: []string{".ops/bin/loom", "vendors-ready"},
			Blocks:       []string{"agent", "cmd"},
			Timeout:      "10m",
		},
	}
	dockLevel := []BayPrepareConfig{
		{
			Name:    "vendors",
			Timeout: "20m",
		},
	}
	merged := MergeBayPrepare(repoLocal, dockLevel)
	if len(merged) != 1 {
		t.Fatalf("merged len = %d, want 1", len(merged))
	}
	got := merged[0]
	if got.Timeout != "20m" {
		t.Errorf("dock-level Timeout did not override: got %q, want 20m", got.Timeout)
	}
	if len(got.Command) != 2 || got.Command[0] != ".ops/bin/fetch-vendor.ts" {
		t.Errorf("repo-local Command not preserved: got %v", got.Command)
	}
	if len(got.Blocks) != 2 {
		t.Errorf("repo-local Blocks not preserved: got %v", got.Blocks)
	}
}

func TestMergeBayPrepare_DockOnlyAndOrder(t *testing.T) {
	repoLocal := []BayPrepareConfig{
		{Name: "first", Command: []string{"x"}},
	}
	dockLevel := []BayPrepareConfig{
		{Name: "first", Timeout: "5m"}, // shadows
		{Name: "second", Command: []string{"y"}},
	}
	merged := MergeBayPrepare(repoLocal, dockLevel)
	if len(merged) != 2 {
		t.Fatalf("merged len = %d, want 2", len(merged))
	}
	if merged[0].Name != "first" || merged[0].Timeout != "5m" || merged[0].Command[0] != "x" {
		t.Errorf("merged[0] = %#v", merged[0])
	}
	if merged[1].Name != "second" || merged[1].Command[0] != "y" {
		t.Errorf("merged[1] = %#v", merged[1])
	}
}

func TestValidatePrepare_Valid(t *testing.T) {
	repoLocal := []BayPrepareConfig{
		{
			Name:         "vendors",
			Command:      []string{".ops/bin/fetch-vendor.ts"},
			ReadyCommand: []string{".ops/bin/loom", "vendors-ready"},
			Blocks:       []string{"agent", "cmd"},
			Timeout:      "10m",
		},
	}
	if errs := ValidatePrepare(repoLocal, nil); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidatePrepare_DockOnlyOverrideIsValid(t *testing.T) {
	// Repo-local supplies command; dock overrides only timeout. Effective
	// config has command, so validation should pass.
	repoLocal := []BayPrepareConfig{
		{
			Name:    "vendors",
			Command: []string{".ops/bin/fetch-vendor.ts"},
		},
	}
	dockLevel := []BayPrepareConfig{
		{Name: "vendors", Timeout: "5m"},
	}
	if errs := ValidatePrepare(repoLocal, dockLevel); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidatePrepare_RejectionCases(t *testing.T) {
	cases := []struct {
		name      string
		repoLocal []BayPrepareConfig
		dockLevel []BayPrepareConfig
		wantSub   string
	}{
		{
			name:      "missing name",
			repoLocal: []BayPrepareConfig{{Command: []string{"x"}}},
			wantSub:   "missing a name",
		},
		{
			name: "duplicate name within source",
			repoLocal: []BayPrepareConfig{
				{Name: "x", Command: []string{"a"}},
				{Name: "x", Command: []string{"b"}},
			},
			wantSub: "names must be unique within a source",
		},
		{
			name:      "missing command in effective",
			repoLocal: []BayPrepareConfig{{Name: "x", Timeout: "5m"}},
			wantSub:   "command is required",
		},
		{
			name: "unknown blocks class",
			repoLocal: []BayPrepareConfig{{
				Name:         "x",
				Command:      []string{"a"},
				Blocks:       []string{"editor"},
				ReadyCommand: []string{"r"},
			}},
			wantSub: "unknown surface class",
		},
		{
			name: "blocks without ready_command",
			repoLocal: []BayPrepareConfig{{
				Name:    "x",
				Command: []string{"a"},
				Blocks:  []string{"agent"},
			}},
			wantSub: "ready_command is required when blocks",
		},
		{
			name: "invalid timeout",
			repoLocal: []BayPrepareConfig{{
				Name:    "x",
				Command: []string{"a"},
				Timeout: "ten minutes",
			}},
			wantSub: "is not a valid duration",
		},
		{
			name: "negative timeout",
			repoLocal: []BayPrepareConfig{{
				Name:    "x",
				Command: []string{"a"},
				Timeout: "-1m",
			}},
			wantSub: "must be positive",
		},
		{
			name: "non-auto run",
			repoLocal: []BayPrepareConfig{{
				Name:    "x",
				Command: []string{"a"},
				Run:     "manual",
			}},
			wantSub: "not supported in v1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := ValidatePrepare(c.repoLocal, c.dockLevel)
			if len(errs) == 0 {
				t.Fatalf("want error containing %q, got none", c.wantSub)
			}
			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, c.wantSub) {
				t.Errorf("errors do not contain %q:\n%s", c.wantSub, joined)
			}
		})
	}
}

func TestResolveTrust(t *testing.T) {
	ptr := func(b bool) *bool { return &b }
	cases := []struct {
		name string
		bay  *bool
		dock *bool
		want bool
	}{
		{name: "all unset → false", want: false},
		{name: "bay-level true", bay: ptr(true), want: true},
		{name: "bay-level false", bay: ptr(false), want: false},
		{name: "dock-level true overrides bay false", bay: ptr(false), dock: ptr(true), want: true},
		{name: "dock-level false overrides bay true", bay: ptr(true), dock: ptr(false), want: false},
		{name: "dock-level only true", dock: ptr(true), want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{
				TrustRepoBayToml: c.bay,
				Docks: map[string]DockConfig{
					"loom": {TrustRepoBayToml: c.dock},
				},
			}
			if got := cfg.ResolveTrust("loom"); got != c.want {
				t.Errorf("ResolveTrust(loom) = %v, want %v", got, c.want)
			}
		})
	}
}

func TestResolveTrust_UnknownDock(t *testing.T) {
	ptr := func(b bool) *bool { return &b }
	cfg := &Config{TrustRepoBayToml: ptr(true)}
	if !cfg.ResolveTrust("not-in-config") {
		t.Errorf("ResolveTrust unknown-dock with bay-level true: got false, want true (falls through to bay-level)")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
