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
	cfg := h.Default("~/Docs/Vaults")
	cfg.AtlasVault = "~/Documents/Atlas"
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	back, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	userHome, _ := os.UserHomeDir()
	if back.VaultsDir != filepath.Join(userHome, "Docs", "Vaults") || back.AtlasVault != filepath.Join(userHome, "Documents", "Atlas") {
		t.Fatalf("got %+v", back)
	}
	if back.Plugin.ID == "" || back.TreeRoot() != filepath.Join(userHome, "Documents", "Atlas", "tree") {
		t.Fatalf("got %+v", back)
	}
}

func TestConfigV2FieldsAndV1Upgrade(t *testing.T) {
	h := Home{Root: t.TempDir()}
	cfg := h.Default("~/Vaults")
	cfg.Schema = ConfigSchemaV1
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := h.Load()
	if err != nil || loaded.Schema != ConfigSchema {
		t.Fatalf("v1 config loads as v2: %+v %v", loaded, err)
	}
	if loaded.TreeRoot() != "" {
		t.Fatalf("no atlas vault, no tree root: %q", loaded.TreeRoot())
	}
	if !loaded.AddVault("~/Elsewhere/side") || loaded.AddVault("~/Elsewhere/side") {
		t.Fatal("AddVault dedupes")
	}
	loaded.SetRepoPath("id-1", "paper", "~/Code/paper")
	if err := h.Save(loaded); err != nil {
		t.Fatal(err)
	}
	again, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Vaults) != 1 || again.Vaults[0] != Expand("~/Elsewhere/side") || again.RepoPath("id-1", "paper") != Expand("~/Code/paper") || again.RepoPath("id-1", "nope") != "" {
		t.Fatalf("v2 fields %+v", again)
	}
	if !again.Inside(filepath.Join(again.VaultsDir, "projects", "x")) || again.Inside(again.Vaults[0]) {
		t.Fatal("Inside")
	}
	again.SetRepoPath("id-1", "paper", "")
	if !again.RemoveVault(again.Vaults[0]) || len(again.Repos) != 0 || len(again.Vaults) != 0 {
		t.Fatalf("removal %+v", again)
	}
	data, _ := os.ReadFile(h.ConfigPath())
	if !strings.Contains(string(data), `"schema": "claude-atlas.config.v2"`) {
		t.Fatalf("saved schema:\n%s", data)
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
