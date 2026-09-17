package refresh

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// oneVault builds an atlas home whose vaults directory holds one project.
func oneVault(t *testing.T) (home.Home, *home.Config) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default(filepath.Join(root, "Vaults"))
	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	if _, err := vault.Init(vaults.PathFor(cfg.VaultsDir, vault.Project, "p"), vault.Options{Kind: vault.Project, Name: "p"}, now); err != nil {
		t.Fatal(err)
	}
	return h, cfg
}

func TestDerivedWritesNothing(t *testing.T) {
	h, cfg := oneVault(t)
	ix, err := Derived(cfg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Entries) != 1 || ix.Entries[0].State == nil {
		t.Fatalf("one derived entry expected, got %+v", ix.Entries)
	}
	if _, err := os.Stat(registry.File(h.StateDir())); !os.IsNotExist(err) {
		t.Fatalf("Derived must not write the registry: %v", err)
	}
}

func TestEntriesWritesTheRegistryOnceThenReadsIt(t *testing.T) {
	h, cfg := oneVault(t)
	entries, err := Entries(h, cfg, time.Now())
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: %v %v", entries, err)
	}
	if _, err := os.Stat(registry.File(h.StateDir())); err != nil {
		t.Fatalf("Entries writes the registry when none exists: %v", err)
	}
	again, err := Entries(h, cfg, time.Now())
	if err != nil || len(again) != 1 {
		t.Fatalf("second read: %v %v", again, err)
	}
}

func TestAllReturnsTheIndexAndTheChanges(t *testing.T) {
	h, cfg := oneVault(t)
	entries, ix, changes, err := All(h, cfg, time.Now())
	if err != nil || len(entries) != 1 || ix == nil || len(ix.Entries) != 1 {
		t.Fatalf("all: %d entries, ix %v, err %v", len(entries), ix, err)
	}
	if len(changes) != 0 {
		t.Fatalf("a fresh project needs no change, got %+v", changes)
	}
}
