package discover

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

func TestVaultThroughARepository(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	if m, c, err := Vault(h, root); m != nil || c != nil || err != nil {
		t.Fatalf("no atlas: %v %v %v", m, c, err)
	}
	cfg := h.Default(filepath.Join(root, "Vaults"))
	os.MkdirAll(h.Root, 0o755)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	aRoot := filepath.Join(cfg.VaultsDir, "projects", "a")
	if _, err := vault.Init(aRoot, vault.Options{Kind: vault.Project, Name: "A"}, now); err != nil {
		t.Fatal(err)
	}
	bRoot := filepath.Join(cfg.VaultsDir, "projects", "b")
	if _, err := vault.Init(bRoot, vault.Options{Kind: vault.Project, Name: "B"}, now); err != nil {
		t.Fatal(err)
	}

	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a := ix.ByPath(aRoot)
	if a == nil {
		t.Fatal("project a not scanned")
	}
	if _, _, err := vaults.CreateRepo(h, cfg, *a, "code", "", now); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(aRoot, "repos", "code", "src"), 0o755)

	outside := filepath.Join(root, "outside")
	os.MkdirAll(outside, 0o755)
	ix, err = registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b := ix.ByPath(bRoot)
	if b == nil {
		t.Fatal("project b not scanned")
	}
	if _, _, err := vaults.AddRepo(h, cfg, *b, outside, true, now); err != nil {
		t.Fatal(err)
	}

	m, c, err := Vault(h, filepath.Join(aRoot, "repos", "code", "src"))
	if err != nil || m == nil || m.Project.Name != "A" || m.Repo.Name != "code" || len(c) != 0 {
		t.Fatalf("inside the repo: %+v %v %v", m, c, err)
	}
	if m, _, _ := Vault(h, aRoot); m != nil {
		t.Fatal("the project's own root is not inside its repository")
	}

	m, c, err = Vault(h, outside)
	if err != nil || m == nil || m.Project.Name != "B" || m.Repo.Name != "outside" || len(c) != 0 {
		t.Fatalf("the outside repository: %+v %v %v", m, c, err)
	}

	// A second project mounts the very same folder as the first project's repository:
	// the two candidates tie on path length, and there is no single match.
	ix, err = registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b = ix.ByPath(bRoot)
	shared := filepath.Join(aRoot, "repos", "code")
	if _, _, err := vaults.AddRepo(h, cfg, *b, shared, true, now); err != nil {
		t.Fatal(err)
	}
	m, c, err = Vault(h, shared)
	if err != nil || m != nil || len(c) != 2 {
		t.Fatalf("two projects sharing a folder: %+v %+v %v", m, c, err)
	}
	if got := Describe(c); got == "" {
		t.Fatal("describe")
	}

	if m, _, _ := Vault(h, t.TempDir()); m != nil {
		t.Fatal("an unrelated folder matches nothing")
	}
	if m, c, err := Vault(home.Home{Root: t.TempDir()}, root); m != nil || c != nil || err != nil {
		t.Fatalf("a home with no config: %v %v %v", m, c, err)
	}
}

// TestAFolderInsideTheHostRepositoryFindsTheProject covers a project that lives at
// REPO/atlas: nothing links the repository, so the match comes from the folder itself.
func TestAFolderInsideTheHostRepositoryFindsTheProject(t *testing.T) {
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
	code := filepath.Join(root, "code")
	src := filepath.Join(code, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := gitx.Repo{Dir: code}
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	res, err := vault.InitIn(code, vault.Options{Kind: vault.Project, Name: "Notes"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddVault(res.Root)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}

	m, c, err := Vault(h, src)
	if err != nil || m == nil || len(c) != 0 {
		t.Fatalf("inside the host repository: %+v %+v %v", m, c, err)
	}
	if m.Project.Name != "Notes" || m.Repo.Name != "code" || m.Repo.Path != code {
		t.Fatalf("match: %+v %+v", m.Project, m.Repo)
	}
	if repos, err := Repos(h, res.Root); err != nil || len(repos) != 1 || repos[0].Name != "code" {
		t.Fatalf("repos: %+v %v", repos, err)
	}
}
