package vaults

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func TestRenameMovesTheFolderToTheNewName(t *testing.T) {
	cfg, h, kb, _ := fixtureEntries(t)
	for _, tc := range []struct {
		entry registry.Entry
		name  string
	}{{kb, "sensor-triage"}} {
		path, err := EditIdentity(h, cfg, tc.entry, Edit{Name: tc.name}, identityNow)
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(filepath.Dir(tc.entry.Path), tc.name)
		if path != want {
			t.Fatalf("returned path %q, want %q", path, want)
		}
		if _, err := os.Stat(tc.entry.Path); !os.IsNotExist(err) {
			t.Fatalf("the old folder should be gone: %v", err)
		}
		v, err := vault.Open(path)
		if err != nil || v.Config.Name != tc.name {
			t.Fatalf("the identity file follows: %+v %v", v, err)
		}
	}
}

func TestRenameRefusesATakenFolderAndWritesNothing(t *testing.T) {
	cfg, h, kb, _ := fixtureEntries(t)
	taken := filepath.Join(filepath.Dir(kb.Path), "taken")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EditIdentity(h, cfg, kb, Edit{Name: "taken"}, identityNow); err == nil {
		t.Fatal("a taken folder should refuse the edit")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("the error should name the folder in the way: %v", err)
	}
	v, err := vault.Open(kb.Path)
	if err != nil || v.Config.Name != "ai-ml" {
		t.Fatalf("the identity file should be untouched: %+v %v", v, err)
	}
}

func TestRenameCleansANameThatCannotBeAFolder(t *testing.T) {
	cfg, h, kb, _ := fixtureEntries(t)
	path, err := EditIdentity(h, cfg, kb, Edit{Name: "CS 566: Robotics"}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "CS 566- Robotics" {
		t.Fatalf("the folder takes the cleaned name, got %q", filepath.Base(path))
	}
	v, err := vault.Open(path)
	if err != nil || v.Config.Name != "CS 566: Robotics" {
		t.Fatalf("the display name is kept whole: %+v %v", v, err)
	}
	if _, err := EditIdentity(h, cfg, *entryAt(t, cfg, path), Edit{Name: "///"}, identityNow); err == nil {
		t.Fatal("a name that cleans to nothing cannot name a folder")
	}
}

func TestRenameRewritesAConfigEntryForAVaultOutsideTheVaultsDir(t *testing.T) {
	cfg, h, _, _ := fixtureEntries(t)
	outside := filepath.Join(t.TempDir(), "side")
	if _, err := vault.Init(outside, vault.Options{Name: "side"}, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(h, cfg, outside); err != nil {
		t.Fatal(err)
	}
	path, err := EditIdentity(h, cfg, *entryAt(t, cfg, outside), Edit{Name: "sideways"}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Knowledge) != 1 || cfg.Knowledge[0] != path {
		t.Fatalf("the config should point at the new folder: %+v", cfg.Knowledge)
	}
	saved, err := h.Load()
	if err != nil || len(saved.Knowledge) != 1 || saved.Knowledge[0] != path {
		t.Fatalf("and it should be saved: %+v %v", saved, err)
	}
}

func TestRenameToTheFolderItAlreadyHasMovesNothing(t *testing.T) {
	cfg, h, kb, _ := fixtureEntries(t)
	path, err := EditIdentity(h, cfg, kb, Edit{Name: "ai-ml"}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if path != kb.Path {
		t.Fatalf("the folder already has that name, got %q", path)
	}
}

func entryAt(t *testing.T, cfg *home.Config, path string) *registry.Entry {
	t.Helper()
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByPath(path)
	if e == nil {
		t.Fatalf("no entry at %s", path)
	}
	return e
}
