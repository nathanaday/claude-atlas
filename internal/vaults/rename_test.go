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
	cfg, h, project, kb := fixtureEntries(t)
	for _, tc := range []struct {
		entry registry.Entry
		name  string
	}{{project, "sensor-triage"}, {kb, "robotics"}} {
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
	cfg, h, project, _ := fixtureEntries(t)
	taken := filepath.Join(filepath.Dir(project.Path), "taken")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EditIdentity(h, cfg, project, Edit{Name: "taken"}, identityNow); err == nil {
		t.Fatal("a taken folder should refuse the edit")
	} else if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("the error should name the folder in the way: %v", err)
	}
	v, err := vault.Open(project.Path)
	if err != nil || v.Config.Name != "cs566" {
		t.Fatalf("the identity file should be untouched: %+v %v", v, err)
	}
}

func TestRenameCleansANameThatCannotBeAFolder(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	path, err := EditIdentity(h, cfg, project, Edit{Name: "CS 566: Robotics"}, identityNow)
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
	if _, err := vault.Init(outside, vault.Options{Kind: vault.Project, Name: "side"}, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(h, cfg, outside); err != nil {
		t.Fatal(err)
	}
	path, err := EditIdentity(h, cfg, *entryAt(t, cfg, outside), Edit{Name: "sideways"}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Vaults) != 1 || cfg.Vaults[0] != path {
		t.Fatalf("the config should point at the new folder: %+v", cfg.Vaults)
	}
	saved, err := h.Load()
	if err != nil || len(saved.Vaults) != 1 || saved.Vaults[0] != path {
		t.Fatalf("and it should be saved: %+v %v", saved, err)
	}
}

func TestRenameLeavesTheFolderOfAProjectInsideARepository(t *testing.T) {
	cfg, h, _, project := fixtureInRepo(t, "code")
	path, err := EditIdentity(h, cfg, project, Edit{Name: "Renamed"}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if path != project.Path {
		t.Fatalf("REPO/atlas keeps its folder, got %q", path)
	}
	v, err := vault.Open(project.Path)
	if err != nil || v.Config.Name != "Renamed" {
		t.Fatalf("the name still changes: %+v %v", v, err)
	}
}

func TestRenameToTheFolderItAlreadyHasMovesNothing(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	path, err := EditIdentity(h, cfg, project, Edit{Name: "cs566"}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if path != project.Path {
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
