package home

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	t.Setenv(EnvHome, "/env/home")
	if h := Resolve("/flag"); h.Root != "/flag" {
		t.Fatalf("flag should win, got %s", h.Root)
	}
	if h := Resolve(""); h.Root != "/env/home" {
		t.Fatalf("env should win, got %s", h.Root)
	}
	t.Setenv(EnvHome, "")
	if h := Resolve(""); filepath.Base(h.Root) != ".claude-atlas" {
		t.Fatalf("default wrong: %s", h.Root)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	h := Home{Root: filepath.Join(t.TempDir(), "home")}
	if h.Exists() {
		t.Fatal("should not exist yet")
	}
	if _, err := h.Load(); err == nil {
		t.Fatal("load before setup should fail")
	}
	cfg := h.Default()
	cfg.AddKnowledge("~/Docs/notes")
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	back, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	userHome, _ := os.UserHomeDir()
	if len(back.Knowledge) != 1 || back.Knowledge[0] != filepath.Join(userHome, "Docs", "notes") || !back.HasKnowledge("~/Docs/notes") {
		t.Fatalf("got %+v", back)
	}
	if back.Plugin.ID == "" {
		t.Fatalf("got %+v", back)
	}
}

func TestConfigV3FieldsAndOlderConfigsUpgrade(t *testing.T) {
	h := Home{Root: t.TempDir()}
	cfg := h.Default()
	cfg.Schema = ConfigSchemaV1
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := h.Load()
	if err != nil || loaded.Schema != ConfigSchema {
		t.Fatalf("v1 config loads as v3: %+v %v", loaded, err)
	}
	if !loaded.AddKnowledge("~/Elsewhere/side") || loaded.AddKnowledge("~/Elsewhere/side") {
		t.Fatal("AddKnowledge dedupes")
	}
	if !loaded.AddProject("~/Code/webapp") || loaded.AddProject("~/Code/webapp") {
		t.Fatal("AddProject dedupes")
	}
	if err := h.Save(loaded); err != nil {
		t.Fatal(err)
	}
	again, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Knowledge) != 1 || again.Knowledge[0] != Expand("~/Elsewhere/side") || len(again.Projects) != 1 || again.Projects[0] != Expand("~/Code/webapp") {
		t.Fatalf("v3 fields %+v", again)
	}
	if !again.HasProject("~/Code/webapp") || again.HasProject("~/Code/other") {
		t.Fatal("HasProject")
	}
	if !again.RemoveKnowledge(again.Knowledge[0]) || again.RemoveKnowledge("~/nope") || len(again.Knowledge) != 0 {
		t.Fatalf("knowledge removal %+v", again)
	}
	if !again.RemoveProject("~/Code/webapp") || again.RemoveProject("~/Code/webapp") || len(again.Projects) != 0 {
		t.Fatalf("project removal %+v", again)
	}
	data, _ := os.ReadFile(h.ConfigPath())
	if !strings.Contains(string(data), `"schema": "claude-atlas.config.v3"`) {
		t.Fatalf("saved schema:\n%s", data)
	}
}

func TestV2ConfigVaultsBecomeKnowledge(t *testing.T) {
	h := Home{Root: t.TempDir()}
	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	v2 := `{
  "schema": "claude-atlas.config.v2",
  "vaults_dir": "~/Vaults",
  "vaults": ["~/Elsewhere/kb"],
  "repos": {"id/paper": "~/Code/paper"},
  "default_repo_changes": "pr",
  "plugin": {"id": "claude-atlas@x", "source": "x"},
  "claude_code": {"command": "claude", "session_context": true}
}
`
	if err := os.WriteFile(h.ConfigPath(), []byte(v2), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Schema != ConfigSchema || len(cfg.Knowledge) != 1 || cfg.Knowledge[0] != Expand("~/Elsewhere/kb") || len(cfg.Projects) != 0 {
		t.Fatalf("v2 vaults become knowledge bases: %+v", cfg)
	}
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(h.ConfigPath())
	for _, gone := range []string{"repos", "default_repo_changes", `"vaults"`, "vaults_dir"} {
		if strings.Contains(string(data), gone) {
			t.Fatalf("%s should not survive a save:\n%s", gone, data)
		}
	}
}

func TestDisplayAndExpand(t *testing.T) {
	userHome, _ := os.UserHomeDir()
	if Display(filepath.Join(userHome, "x")) != "~/x" || Display("/opt/x") != "/opt/x" {
		t.Fatal("display wrong")
	}
	if Expand("~/x") != filepath.Join(userHome, "x") || Expand("/abs") != "/abs" {
		t.Fatal("expand wrong")
	}
}
