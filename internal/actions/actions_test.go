package actions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

var now = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

// atlas builds an atlas home whose vaults directory holds one knowledge base, kb.
func atlas(t *testing.T) (home.Home, *home.Config, registry.Entry) {
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
	kbPath := vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "kb")
	if _, err := vault.Init(kbPath, vault.Options{Kind: vault.Knowledge, Name: "kb"}, now); err != nil {
		t.Fatal(err)
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	kb := ix.ByPath(kbPath)
	if kb == nil {
		t.Fatal("the scan did not find kb")
	}
	return h, cfg, *kb
}

func TestBindSetsEveryField(t *testing.T) {
	a := Bind(home.Home{Root: t.TempDir()}, &home.Config{}, nil)
	v := reflect.ValueOf(a)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() == reflect.Func && f.IsNil() {
			t.Errorf("Bind leaves %s nil", v.Type().Field(i).Name)
		}
	}
}

func TestCreateRecordsFactsMountsAndGathers(t *testing.T) {
	h, cfg, kb := atlas(t)
	a := Bind(h, cfg, nil)
	// A project with tags that mounts kb at creation.
	path, err := a.Create(AddVault{Kind: vault.Project, Name: "p", Path: vaults.PathFor(cfg.VaultsDir, vault.Project, "p"), Mode: "generic", Tags: []string{"usc"}, MountID: kb.ID})
	if err != nil {
		t.Fatal(err)
	}
	ix, err := a.Scan()
	if err != nil {
		t.Fatal(err)
	}
	p := ix.ByPath(path)
	if p == nil || p.Kind != vault.Project || len(p.Tags) != 1 || p.Tags[0] != "usc" || len(p.Mounts) != 1 || p.Mounts[0].Name != "kb" || p.State == nil {
		t.Fatalf("project: %+v", p)
	}
	// A guarded knowledge base with a scope.
	kb2, err := a.Create(AddVault{Kind: vault.Knowledge, Name: "papers", Path: vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "papers"), Mode: "generic", Scope: "Papers.", Access: vault.AccessGuarded})
	if err != nil {
		t.Fatal(err)
	}
	ix, _ = a.Scan()
	if e := ix.ByPath(kb2); e == nil || e.Scope != "Papers." || e.Access != vault.AccessGuarded {
		t.Fatalf("knowledge base: %+v", e)
	}
	// A cluster that gathers kb.
	cl, err := a.Create(AddVault{Kind: vault.Knowledge, Name: "domain", Path: vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "domain"), Mode: "generic", Cluster: true, MemberIDs: []string{kb.ID}})
	if err != nil {
		t.Fatal(err)
	}
	ix, _ = a.Scan()
	if e := ix.ByPath(cl); e == nil || len(e.Members) != 1 || e.Members[0].Name != "kb" {
		t.Fatalf("cluster: %+v", e)
	}
}

func TestCreateInARepositoryAndEditAndForget(t *testing.T) {
	h, cfg, _ := atlas(t)
	a := Bind(h, cfg, nil)
	repo := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repo, 0o755)
	r := gitx.Repo{Dir: repo}
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	path, err := a.Create(AddVault{Kind: vault.Project, Name: "code", InRepo: repo, Mode: "generic"})
	if err != nil || path != filepath.Join(repo, vault.InRepoDir) {
		t.Fatalf("in repo: %q %v", path, err)
	}
	ix, _ := a.Scan()
	e := ix.ByPath(path)
	if e == nil || e.Host == "" || filepath.Base(e.Host) != "code" {
		t.Fatalf("the host repository is the project's first repository: %+v", e)
	}
	moved, err := a.Edit(*e, vaults.Edit{Name: "Code"})
	if err != nil || moved != path {
		t.Fatalf("a project at REPO/atlas keeps its folder: %q %v", moved, err)
	}
	// A vault outside the vaults directory is registered and can be forgotten; one inside cannot.
	if err := a.Unregister(*e); err != nil {
		t.Fatalf("forget outside: %v", err)
	}
	inside, err := a.Create(AddVault{Kind: vault.Project, Name: "q", Path: vaults.PathFor(cfg.VaultsDir, vault.Project, "q"), Mode: "generic"})
	if err != nil {
		t.Fatal(err)
	}
	ix, _ = a.Scan()
	if err := a.Unregister(*ix.ByPath(inside)); err == nil || !strings.Contains(err.Error(), "vaults directory") {
		t.Fatalf("forget inside: %v", err)
	}
}
