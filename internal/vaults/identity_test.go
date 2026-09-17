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

// fixtureEntries makes two knowledge bases under a temp vaults directory and one
// project in a work folder outside it, and returns the config, the home, and the
// scanned entries of ai-ml and the project.
func fixtureEntries(t *testing.T) (*home.Config, home.Home, registry.Entry, registry.Entry) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := &home.Config{Schema: home.ConfigSchema, VaultsDir: filepath.Join(root, "Vaults")}
	if _, err := vault.Init(filepath.Join(cfg.VaultsDir, "ai-ml"), vault.Options{Name: "ai-ml"}, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Init(filepath.Join(cfg.VaultsDir, "robotics"), vault.Options{Name: "robotics"}, identityNow); err != nil {
		t.Fatal(err)
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
	kb := ix.ByPath(filepath.Join(cfg.VaultsDir, "ai-ml"))
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

func TestPathForAndResolvePath(t *testing.T) {
	dir := filepath.FromSlash("/vaults")
	if got := PathFor(dir, "ai-ml"); got != filepath.Join(dir, "ai-ml") {
		t.Fatalf("PathFor: %s", got)
	}
	if got, err := ResolvePath("ai-ml", dir); err != nil || got != filepath.Join(dir, "ai-ml") {
		t.Fatalf("ResolvePath bare name: %s, %v", got, err)
	}
	rel, err := ResolvePath("./x", dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(rel) != "x" || strings.HasPrefix(rel, dir) {
		t.Fatalf("ResolvePath relative: %s", rel)
	}
	tilde, err := ResolvePath("~/x", dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(tilde) != "x" || strings.HasPrefix(tilde, "~") {
		t.Fatalf("ResolvePath tilde: %s", tilde)
	}
}

func TestRegisterAndUnregister(t *testing.T) {
	cfg, h, kb, _ := fixtureEntries(t)

	// A vault under VaultsDir needs no entry.
	changed, err := Register(h, cfg, kb.Path)
	if err != nil || changed {
		t.Fatalf("register inside: changed=%v err=%v", changed, err)
	}
	if len(cfg.Knowledge) != 0 {
		t.Fatalf("cfg.Knowledge should stay empty: %v", cfg.Knowledge)
	}
	if _, err := Register(h, cfg, t.TempDir()); err == nil {
		t.Fatal("a folder that is not a vault cannot be registered")
	}

	outside := t.TempDir()
	if _, err := vault.Init(outside, vault.Options{Name: "outside"}, identityNow); err != nil {
		t.Fatal(err)
	}
	changed, err = Register(h, cfg, outside)
	if err != nil || !changed {
		t.Fatalf("register outside: changed=%v err=%v", changed, err)
	}
	reloaded, err := h.Load()
	if err != nil || len(reloaded.Knowledge) != 1 || reloaded.Knowledge[0] != outside {
		t.Fatalf("outside vault not saved: %+v %v", reloaded, err)
	}
	changed, err = Register(h, cfg, outside)
	if err != nil || changed {
		t.Fatalf("register again: changed=%v err=%v", changed, err)
	}
	if err := Unregister(h, cfg, outside); err != nil {
		t.Fatal(err)
	}
	if reloaded, _ := h.Load(); len(reloaded.Knowledge) != 0 {
		t.Fatalf("outside vault still registered: %v", reloaded.Knowledge)
	}
	if err := Unregister(h, cfg, outside); err == nil {
		t.Fatal("unregistering twice is an error")
	}
	if err := Unregister(h, cfg, kb.Path); err == nil || !strings.Contains(err.Error(), "vaults directory") {
		t.Fatalf("unregister inside: %v", err)
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
