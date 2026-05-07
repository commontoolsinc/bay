package configedit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendBlock_EmptyFile(t *testing.T) {
	path := writeConfig(t, "")

	if err := AppendBlock(path, "docks.loom", []string{
		`agent = "codex"`,
		"trust_repo_bay_toml = true",
	}); err != nil {
		t.Fatalf("AppendBlock: %v", err)
	}

	want := "[docks.loom]\n" +
		"agent = \"codex\"\n" +
		"trust_repo_bay_toml = true\n"
	assertConfig(t, path, want)
}

func TestAppendBlock_FileWithoutTrailingNewline(t *testing.T) {
	path := writeConfig(t, "default_agent = \"codex\"")

	if err := AppendBlock(path, "docks.loom", []string{`agent = "codex"`}); err != nil {
		t.Fatalf("AppendBlock: %v", err)
	}

	want := "default_agent = \"codex\"\n\n" +
		"[docks.loom]\n" +
		"agent = \"codex\"\n"
	assertConfig(t, path, want)
}

func TestSetField_ExistingField(t *testing.T) {
	path := writeConfig(t, `# global default
default_agent = "codex"

[docks.loom]
# existing comment
agent = "claude"
  trust_repo_bay_toml=false # old value
args = ["--flag", "value with spaces"]

[agents.codex]
command = "codex"
`)

	if err := SetField(path, "docks.loom", "trust_repo_bay_toml", "true"); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	want := `# global default
default_agent = "codex"

[docks.loom]
# existing comment
agent = "claude"
  trust_repo_bay_toml = true
args = ["--flag", "value with spaces"]

[agents.codex]
command = "codex"
`
	assertConfig(t, path, want)
}

func TestSetField_MissingFieldInExistingBlock(t *testing.T) {
	path := writeConfig(t, `# docks stay first
[docks.loom]
agent = "codex"

[agents.codex]
command = "codex"
`)

	if err := SetField(path, "docks.loom", "trust_repo_bay_toml", "true"); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	want := `# docks stay first
[docks.loom]
agent = "codex"
trust_repo_bay_toml = true

[agents.codex]
command = "codex"
`
	assertConfig(t, path, want)
}

func TestSetField_MissingFieldWithoutTrailingNewline(t *testing.T) {
	path := writeConfig(t, "[docks.loom]\nagent = \"codex\"")

	if err := SetField(path, "docks.loom", "trust_repo_bay_toml", "true"); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	want := "[docks.loom]\n" +
		"agent = \"codex\"\n" +
		"trust_repo_bay_toml = true\n"
	assertConfig(t, path, want)
}

func TestSetField_MissingBlock(t *testing.T) {
	path := writeConfig(t, `# bay config
default_agent = "codex"

[agents.codex]
command = "codex"
`)

	if err := SetField(path, "docks.loom", "trust_repo_bay_toml", "true"); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	want := `# bay config
default_agent = "codex"

[agents.codex]
command = "codex"

[docks.loom]
trust_repo_bay_toml = true
`
	assertConfig(t, path, want)
}

func TestSetField_DoesNotMatchFieldPrefix(t *testing.T) {
	path := writeConfig(t, `[docks.loom]
trust_repo_bay_toml_extra = false
`)

	if err := SetField(path, "docks.loom", "trust_repo_bay_toml", "true"); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	want := `[docks.loom]
trust_repo_bay_toml_extra = false
trust_repo_bay_toml = true
`
	assertConfig(t, path, want)
}

func TestSetField_UsesLocalNewlineForInsertion(t *testing.T) {
	path := writeConfig(t, "[top]\r\nkey = true\r\n\r\n[docks.loom]\nagent = \"codex\"\n")

	if err := SetField(path, "docks.loom", "trust_repo_bay_toml", "true"); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	want := "[top]\r\nkey = true\r\n\r\n[docks.loom]\nagent = \"codex\"\ntrust_repo_bay_toml = true\n"
	assertConfig(t, path, want)
}

func TestSetField_DoesNotOverwriteTmpSibling(t *testing.T) {
	path := writeConfig(t, "[docks.loom]\nagent = \"codex\"\n")
	tmpSibling := path + ".tmp"
	if err := os.WriteFile(tmpSibling, []byte("keep me\n"), 0o644); err != nil {
		t.Fatalf("write tmp sibling: %v", err)
	}

	if err := SetField(path, "docks.loom", "trust_repo_bay_toml", "true"); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	got, err := os.ReadFile(tmpSibling)
	if err != nil {
		t.Fatalf("read tmp sibling: %v", err)
	}
	if string(got) != "keep me\n" {
		t.Fatalf("tmp sibling = %q, want keep me", got)
	}
}

func TestSetField_PreservesSymlinkPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.toml")
	if err := os.WriteFile(target, []byte("[docks.loom]\nagent = \"codex\"\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "config.toml")
	if err := os.Symlink("target.toml", link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	if err := SetField(link, "docks.loom", "trust_repo_bay_toml", "true"); err != nil {
		t.Fatalf("SetField: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("config path is no longer a symlink: mode %s", info.Mode())
	}

	want := "[docks.loom]\n" +
		"agent = \"codex\"\n" +
		"trust_repo_bay_toml = true\n"
	assertConfig(t, target, want)
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func assertConfig(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(got) != want {
		t.Fatalf("config mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}
