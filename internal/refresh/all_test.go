package refresh

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// oneVault builds an atlas home whose config lists one knowledge base.
func oneVault(t *testing.T) (home.Home, *home.Config) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	kb := filepath.Join(root, "Vaults", "kb")
	if _, err := vault.Init(kb, vault.Options{Name: "kb"}, now); err != nil {
		t.Fatal(err)
	}
	cfg.AddKnowledge(kb)
	if err := h.Save(cfg); err != nil {
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

func TestAllReturnsTheIndex(t *testing.T) {
	h, cfg := oneVault(t)
	entries, ix, err := All(h, cfg, time.Now())
	if err != nil || len(entries) != 1 || ix == nil || len(ix.Entries) != 1 {
		t.Fatalf("all: %d entries, ix %v, err %v", len(entries), ix, err)
	}
}

func TestEntriesRebuildsAStaleRegistry(t *testing.T) {
	h, cfg := oneVault(t)
	for _, stale := range []string{`{"schema":"claude-atlas.registry.v1","entries":[]}`, `not json`} {
		os.MkdirAll(h.StateDir(), 0o755)
		if err := os.WriteFile(registry.File(h.StateDir()), []byte(stale), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := registry.Read(h.StateDir()); !errors.Is(err, registry.ErrStale) {
			t.Fatalf("Read names %q stale: %v", stale, err)
		}
		entries, err := Entries(h, cfg, time.Now())
		if err != nil || len(entries) != 1 {
			t.Fatalf("Entries rebuilds %q: %v %v", stale, entries, err)
		}
		if _, _, err := registry.Read(h.StateDir()); err != nil {
			t.Fatalf("and writes a current one: %v", err)
		}
	}
}
