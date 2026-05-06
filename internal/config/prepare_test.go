package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_PrepareDockConfig(t *testing.T) {
	cfg, err := Parse(`
trust_repo_bay_toml = true

[docks.loom]
trust_repo_bay_toml = false

[[docks.loom.bay_prepare]]
name = "vendors"
command = [".ops/bin/fetch-vendor.ts", "labs"]
ready_command = [".ops/bin/loom", "vendors-ready", "labs"]
blocks = ["agent", "cmd"]
run = "auto"
timeout = "10m"
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.TrustRepoBayToml == nil || *cfg.TrustRepoBayToml != true {
		t.Fatalf("top-level trust_repo_bay_toml = %v, want true", cfg.TrustRepoBayToml)
	}
	dock := cfg.Docks["loom"]
	if dock.TrustRepoBayToml == nil || *dock.TrustRepoBayToml != false {
		t.Fatalf("dock trust_repo_bay_toml = %v, want false", dock.TrustRepoBayToml)
	}
	if len(dock.BayPrepare) != 1 {
		t.Fatalf("bay_prepare len = %d, want 1", len(dock.BayPrepare))
	}
	step := dock.BayPrepare[0]
	if step.Name != "vendors" || step.Command[0] != ".ops/bin/fetch-vendor.ts" || step.ReadyCommand[0] != ".ops/bin/loom" {
		t.Fatalf("step parsed incorrectly: %#v", step)
	}
	if len(step.Blocks) != 2 || step.Blocks[0] != "agent" || step.Blocks[1] != "cmd" {
		t.Fatalf("blocks = %v, want [agent cmd]", step.Blocks)
	}
	if step.Run != "auto" || step.Timeout != "10m" {
		t.Fatalf("run/timeout = %q/%q, want auto/10m", step.Run, step.Timeout)
	}
}

func TestRepoLocal_LoadAbsentReturnsNil(t *testing.T) {
	cfg, err := LoadRepoLocal(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRepoLocal: %v", err)
	}
	if cfg != nil {
		t.Fatalf("LoadRepoLocal absent = %#v, want nil", cfg)
	}
}

func TestRepoLocal_ParseAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, RepoLocalFilename)
	if err := os.WriteFile(path, []byte(`
[[bay_prepare]]
name = "vendors"
command = ["fetch"]
timeout = "10m"
`), 0o644); err != nil {
		t.Fatalf("write repo-local config: %v", err)
	}
	cfg, err := LoadRepoLocal(dir)
	if err != nil {
		t.Fatalf("LoadRepoLocal: %v", err)
	}
	if cfg == nil || len(cfg.BayPrepare) != 1 {
		t.Fatalf("BayPrepare = %#v, want one step", cfg)
	}
	if cfg.BayPrepare[0].Name != "vendors" || cfg.BayPrepare[0].Command[0] != "fetch" {
		t.Fatalf("step = %#v, want vendors/fetch", cfg.BayPrepare[0])
	}
}

func TestMergePrepare_PerFieldOverride(t *testing.T) {
	repo, err := ParseRepoLocal(`
[[bay_prepare]]
name = "vendors"
command = ["fetch"]
ready_command = ["ready"]
blocks = ["agent"]
run = "auto"
timeout = "10m"

[[bay_prepare]]
name = "db"
command = ["db"]
`)
	if err != nil {
		t.Fatalf("ParseRepoLocal: %v", err)
	}
	user, err := Parse(`
[docks.loom]

[[docks.loom.bay_prepare]]
name = "vendors"
blocks = []
timeout = "20m"

[[docks.loom.bay_prepare]]
name = "lint"
command = ["go", "test", "./..."]
`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	merged := Merge(repo.BayPrepare, user.Docks["loom"].BayPrepare)
	if len(merged) != 3 {
		t.Fatalf("merged len = %d, want 3: %#v", len(merged), merged)
	}
	if merged[0].Name != "vendors" || merged[0].Command[0] != "fetch" || merged[0].ReadyCommand[0] != "ready" {
		t.Fatalf("vendors inherited fields incorrectly: %#v", merged[0])
	}
	if len(merged[0].Blocks) != 0 || merged[0].Timeout != "20m" {
		t.Fatalf("vendors override fields incorrectly: %#v", merged[0])
	}
	if merged[1].Name != "db" || merged[2].Name != "lint" {
		t.Fatalf("merged order = [%s %s %s], want [vendors db lint]", merged[0].Name, merged[1].Name, merged[2].Name)
	}
}

func TestValidatePrepare_RejectionCases(t *testing.T) {
	valid := BayPrepareConfig{
		Name:         "vendors",
		Command:      []string{"fetch"},
		ReadyCommand: []string{"ready"},
		Blocks:       []string{"agent"},
		Run:          "auto",
		Timeout:      "10m",
	}
	cases := []struct {
		name  string
		steps []BayPrepareConfig
		want  string
	}{
		{"empty name", []BayPrepareConfig{{Command: []string{"fetch"}}}, "name must be non-empty"},
		{"missing command", []BayPrepareConfig{{Name: "vendors"}}, "command must be a non-empty argv array"},
		{"empty command", []BayPrepareConfig{{Name: "vendors", Command: []string{}}}, "command must be a non-empty argv array"},
		{"empty ready", []BayPrepareConfig{{Name: "vendors", Command: []string{"fetch"}, ReadyCommand: []string{}}}, "ready_command must be a non-empty argv array"},
		{"unknown block", []BayPrepareConfig{{Name: "vendors", Command: []string{"fetch"}, Blocks: []string{"editor"}}}, "unknown surface class"},
		{"blocks require ready", []BayPrepareConfig{{Name: "vendors", Command: []string{"fetch"}, Blocks: []string{"agent"}}}, "ready_command is required"},
		{"invalid timeout", []BayPrepareConfig{{Name: "vendors", Command: []string{"fetch"}, Timeout: "soon"}}, "timeout must be a positive duration"},
		{"zero timeout", []BayPrepareConfig{{Name: "vendors", Command: []string{"fetch"}, Timeout: "0s"}}, "timeout must be a positive duration"},
		{"invalid run", []BayPrepareConfig{{Name: "vendors", Command: []string{"fetch"}, Run: "manual"}}, `run must be "auto"`},
		{"duplicate", []BayPrepareConfig{valid, valid}, "duplicate name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidatePrepare(tc.steps)
			if !containsErr(errs, tc.want) {
				t.Fatalf("ValidatePrepare errors = %v, want containing %q", errs, tc.want)
			}
		})
	}
}

func TestValidatePrepareSource_AllowsPartialOverrides(t *testing.T) {
	errs := ValidatePrepareSource("docks.loom.bay_prepare", []BayPrepareConfig{
		{Name: "vendors", Timeout: "20m"},
	})
	if len(errs) != 0 {
		t.Fatalf("ValidatePrepareSource partial override errors = %v, want none", errs)
	}

	errs = ValidatePrepareSource("docks.loom.bay_prepare", []BayPrepareConfig{
		{Name: "vendors", Command: []string{}},
	})
	if !containsErr(errs, "command must be a non-empty argv array") {
		t.Fatalf("ValidatePrepareSource errors = %v, want empty-command error", errs)
	}

	errs = ValidatePrepareSource("docks.loom.bay_prepare", []BayPrepareConfig{
		{Name: "vendors", Command: []string{"fetch"}},
		{Name: "vendors", Timeout: "20m"},
	})
	if !containsErr(errs, "duplicate name") {
		t.Fatalf("ValidatePrepareSource errors = %v, want duplicate-name error", errs)
	}
}

func TestResolveTrust(t *testing.T) {
	tTrue := true
	tFalse := false
	cases := []struct {
		name      string
		top       *bool
		dock      *bool
		wantTrust bool
	}{
		{"unset", nil, nil, false},
		{"top true", &tTrue, nil, true},
		{"top false", &tFalse, nil, false},
		{"dock true", nil, &tTrue, true},
		{"dock false", nil, &tFalse, false},
		{"dock true overrides top false", &tFalse, &tTrue, true},
		{"dock false overrides top true", &tTrue, &tFalse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				TrustRepoBayToml: tc.top,
				Docks: map[string]DockConfig{
					"loom": {TrustRepoBayToml: tc.dock},
				},
			}
			if got := ResolveTrust(cfg, "loom"); got != tc.wantTrust {
				t.Fatalf("ResolveTrust = %v, want %v", got, tc.wantTrust)
			}
		})
	}
}

func containsErr(errs []string, want string) bool {
	for _, err := range errs {
		if strings.Contains(err, want) {
			return true
		}
	}
	return false
}
