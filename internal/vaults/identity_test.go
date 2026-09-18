package vaults

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var identityNow = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// fixtureEntries makes two knowledge bases and one project, all listed in the config,
// and returns the config, the home, and the scanned entries of ai-ml and the project.
func fixtureEntries(t *testing.T) (*home.Config, home.Home, registry.Entry, registry.Entry) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := &home.Config{Schema: home.ConfigSchema}
	for _, name := range []string{"ai-ml", "robotics"} {
		path := filepath.Join(root, "Vaults", name)
		if _, err := vault.Init(path, vault.Options{Name: name}, identityNow); err != nil {
			t.Fatal(err)
		}
		cfg.AddKnowledge(path)
	}
	work := filepath.Join(root, "Code", "webapp")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := project.Init(work, project.Options{}, identityNow); err != nil {
		t.Fatal(err)
	}
	cfg.AddProject(work)
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	kb := ix.ByPath(filepath.Join(root, "Vaults", "ai-ml"))
	proj := ix.ByPath(work)
	if kb == nil || proj == nil {
		t.Fatalf("fixture missing entries: %+v", ix.Entries)
	}
	return cfg, h, *kb, *proj
}

// refreshEntry re-scans and returns the entry named id, so a test sees what the last
// mutation wrote.
func refreshEntry(t *testing.T, cfg *home.Config, id string) registry.Entry {
	t.Helper()
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByID(id)
	if e == nil {
		t.Fatalf("entry %s not found after rescan", id)
	}
	return *e
}

func lastCommitSubject(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%s").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %s", out)
	}
	return strings.TrimSpace(string(out))
}

func strPtr(s string) *string { return &s }

func TestResolvePath(t *testing.T) {
	t.Chdir(t.TempDir())
	here, _ := os.Getwd()
	if got, err := ResolvePath("ai-ml"); err != nil || got != filepath.Join(here, "ai-ml") {
		t.Fatalf("a bare name is a folder in the current directory: %s, %v", got, err)
	}
	if got, err := ResolvePath("../x"); err != nil || got != filepath.Join(filepath.Dir(here), "x") {
		t.Fatalf("a relative path: %s, %v", got, err)
	}
	userHome, _ := os.UserHomeDir()
	if got, err := ResolvePath("~/x"); err != nil || got != filepath.Join(userHome, "x") {
		t.Fatalf("a ~ path: %s, %v", got, err)
	}
	if _, err := ResolvePath(" "); err == nil {
		t.Fatal("a blank argument names nothing")
	}
}

func TestRegisterAndUnregister(t *testing.T) {
	cfg, h, kb, _ := fixtureEntries(t)

	changed, err := Register(h, cfg, kb.Path)
	if err != nil || changed {
		t.Fatalf("a listed knowledge base changes nothing: changed=%v err=%v", changed, err)
	}
	if _, err := Register(h, cfg, t.TempDir()); err == nil {
		t.Fatal("a folder that is not a vault cannot be registered")
	}

	other := t.TempDir()
	if _, err := vault.Init(other, vault.Options{Name: "other"}, identityNow); err != nil {
		t.Fatal(err)
	}
	changed, err = Register(h, cfg, other)
	if err != nil || !changed {
		t.Fatalf("register: changed=%v err=%v", changed, err)
	}
	reloaded, err := h.Load()
	if err != nil || !reloaded.HasKnowledge(other) {
		t.Fatalf("the entry is saved: %+v %v", reloaded, err)
	}
	if err := Unregister(h, cfg, kb.Path); err != nil {
		t.Fatalf("any listed knowledge base can be forgotten: %v", err)
	}
	if err := Unregister(h, cfg, other); err != nil {
		t.Fatal(err)
	}
	if reloaded, _ := h.Load(); reloaded.HasKnowledge(other) || reloaded.HasKnowledge(kb.Path) {
		t.Fatalf("still registered: %v", reloaded.Knowledge)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("forgetting keeps the folder")
	}
	if err := Unregister(h, cfg, other); err == nil {
		t.Fatal("unregistering twice is an error")
	}
}

func TestRegisterKnowledgeHeals(t *testing.T) {
	cfg, h, kb, _ := fixtureEntries(t)
	v, err := vault.Open(kb.Path)
	if err != nil {
		t.Fatal(err)
	}
	if heal, err := RegisterKnowledge(h, cfg, v); err != nil || heal != HealNone {
		t.Fatalf("a listed knowledge base needs nothing: %q %v", heal, err)
	}
	// One the config does not list is added.
	clone := filepath.Join(t.TempDir(), "clone")
	if _, err := vault.Init(clone, vault.Options{Name: "clone"}, identityNow); err != nil {
		t.Fatal(err)
	}
	c, _ := vault.Open(clone)
	if heal, err := RegisterKnowledge(h, cfg, c); err != nil || heal != HealAdded || !cfg.HasKnowledge(clone) {
		t.Fatalf("added: %q %v %v", heal, err, cfg.Knowledge)
	}
	if saved, _ := h.Load(); !saved.HasKnowledge(clone) {
		t.Fatal("the heal is saved")
	}
	// A folder moved away is followed by its id.
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(kb.Path, moved); err != nil {
		t.Fatal(err)
	}
	m, _ := vault.Open(moved)
	if heal, err := RegisterKnowledge(h, cfg, m); err != nil || heal != HealMoved || !cfg.HasKnowledge(moved) || cfg.HasKnowledge(kb.Path) {
		t.Fatalf("moved: %q %v %v", heal, err, cfg.Knowledge)
	}
}

func TestEditIdentity(t *testing.T) {
	cfg, h, kb, _ := fixtureEntries(t)
	scope := " machine learning "
	if _, err := EditIdentity(h, cfg, kb, Edit{Scope: &scope}, identityNow); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(kb.Path)
	if err != nil || v.Config.Scope != "machine learning" {
		t.Fatalf("scope: %+v %v", v, err)
	}
	if subject := lastCommitSubject(t, kb.Path); subject != "setup: edit scope" {
		t.Fatalf("commit subject: %q", subject)
	}
	if path, err := EditIdentity(h, cfg, kb, Edit{}, identityNow); err != nil || path != kb.Path {
		t.Fatalf("an empty edit changes nothing: %q %v", path, err)
	}
	if subject := lastCommitSubject(t, kb.Path); subject != "setup: edit scope" {
		t.Fatalf("an empty edit makes no commit: %q", subject)
	}
	// A rename and a scope in one edit is one operation that names both fields.
	empty := ""
	path, err := EditIdentity(h, cfg, kb, Edit{Name: "ai-ml-renamed", Scope: &empty}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	v, err = vault.Open(path)
	if err != nil || v.Config.Name != "ai-ml-renamed" || v.Config.Scope != "" {
		t.Fatalf("renamed: %+v %v", v, err)
	}
	if subject := lastCommitSubject(t, path); subject != "setup: edit name, scope" {
		t.Fatalf("commit subject: %q", subject)
	}
	if e := refreshEntry(t, cfg, kb.ID); e.Path != path || e.Name != "ai-ml-renamed" {
		t.Fatalf("the scan follows the rename: %+v", e)
	}
}
