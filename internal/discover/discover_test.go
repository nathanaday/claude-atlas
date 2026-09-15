package discover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

func TestVaultThroughALinkedFolder(t *testing.T) {
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	if m, c, err := Vault(h, root); m != nil || c != nil || err != nil {
		t.Fatalf("no atlas: %v %v %v", m, c, err)
	}
	cfg := h.Default(filepath.Join(root, "Vaults"), filepath.Join(root, "Atlas"))
	os.MkdirAll(h.Root, 0o755)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(cfg.TreeRoot(), 0o755)
	mk := func(name string) string {
		v := filepath.Join(cfg.VaultsDir, name)
		os.MkdirAll(v, 0o755)
		os.WriteFile(filepath.Join(v, ".claude-atlas.json"), []byte(`{"schema":"claude-atlas.vault.v2","id":"00000000-0000-4000-8000-000000000001","kind":"project","name":"v","mode":"generic","created":"2026-09-12"}`), 0o644)
		return v
	}
	a, _ := vaults.Register(cfg, mk("a"), vaults.RegisterOptions{Name: "A"})
	b, _ := vaults.Register(cfg, mk("b"), vaults.RegisterOptions{Name: "B"})
	repo := filepath.Join(root, "code", "app")
	os.MkdirAll(filepath.Join(repo, "src"), 0o755)
	if _, err := vaults.AddLink(cfg, a, repo, true); err != nil {
		t.Fatal(err)
	}
	m, c, err := Vault(h, filepath.Join(repo, "src"))
	if err != nil || m == nil || m.Project.Name != "A" || m.Folder != repo || len(c) != 0 {
		t.Fatalf("inside the repo: %+v %v %v", m, c, err)
	}
	if m, _, _ := Vault(h, filepath.Join(root, "code")); m != nil {
		t.Fatal("the parent of a linked folder is not inside it")
	}
	if _, err := vaults.AddLink(cfg, b, repo, true); err != nil {
		t.Fatal(err)
	}
	m, c, err = Vault(h, repo)
	if err != nil || m != nil || len(c) != 2 || c[0].Project.Name != "A" {
		t.Fatalf("two projects: %+v %+v %v", m, c, err)
	}
	if got := Describe(c); got == "" {
		t.Fatal("describe")
	}
}
