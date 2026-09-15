package vaults

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// mountFixture makes a project and two knowledge bases under a temp vaults directory and
// returns the config and their scanned entries.
func mountFixture(t *testing.T) (*home.Config, registry.Entry, registry.Entry, registry.Entry) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{Schema: home.ConfigSchema, VaultsDir: filepath.Join(root, "Vaults")}
	if _, err := vault.Init(filepath.Join(cfg.VaultsDir, "projects", "cs566"), vault.Options{Kind: vault.Project, Name: "cs566"}, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Init(filepath.Join(cfg.VaultsDir, "knowledge", "ai-ml"), vault.Options{Kind: vault.Knowledge, Name: "ai-ml"}, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Init(filepath.Join(cfg.VaultsDir, "knowledge", "cooking"), vault.Options{Kind: vault.Knowledge, Name: "cooking"}, identityNow); err != nil {
		t.Fatal(err)
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var project, kb, kb2 registry.Entry
	for _, e := range ix.Entries {
		switch e.Name {
		case "cs566":
			project = e
		case "ai-ml":
			kb = e
		case "cooking":
			kb2 = e
		}
	}
	if project.ID == "" || kb.ID == "" || kb2.ID == "" {
		t.Fatalf("fixture missing entries: %+v", ix.Entries)
	}
	return cfg, project, kb, kb2
}

func TestMountCreatesTheSymlinkAndRecordsTheMount(t *testing.T) {
	cfg, project, kb, kb2 := mountFixture(t)

	m, err := Mount(project, kb, "", "", identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if m != (vault.Mount{ID: kb.ID, Name: "ai-ml", Access: vault.AccessWrite}) {
		t.Fatalf("mount: %+v", m)
	}
	v, err := vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Mounts) != 1 || v.Config.Mounts[0] != (vault.Mount{ID: kb.ID, Name: "ai-ml", Access: vault.AccessWrite}) {
		t.Fatalf("identity mounts: %+v", v.Config.Mounts)
	}

	link := project.KbDir("ai-ml")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if target != kb.Wiki() {
		t.Fatalf("symlink target: %s, want %s", target, kb.Wiki())
	}

	// A page written into the knowledge base's wiki is readable through the mount.
	if err := os.WriteFile(filepath.Join(kb.Wiki(), "note.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(link, "note.md"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("read through mount: %v %q", err, data)
	}

	// The project's git stays clean: kb/ is ignored.
	dirty, err := (gitx.Repo{Dir: project.Path}).Dirty()
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Fatal("project git should stay clean after a mount")
	}

	project = refreshEntry(t, cfg, project.ID)

	// Mounting the same knowledge base again is refused.
	if _, err := Mount(project, kb, "", "", identityNow); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("mount again: %v", err)
	}

	// A second mount, a chosen name and access.
	m2, err := Mount(project, kb2, vault.AccessRead, "robots", identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Name != "robots" || m2.Access != vault.AccessRead {
		t.Fatalf("second mount: %+v", m2)
	}
	project = refreshEntry(t, cfg, project.ID)

	// A knowledge base entry as project.
	if _, err := Mount(kb, project, "", "", identityNow); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("kb as project: %v", err)
	}

	// A project entry as kb.
	if _, err := Mount(project, project, "", "", identityNow); err == nil || !strings.Contains(err.Error(), "not a knowledge base") {
		t.Fatalf("project as kb: %v", err)
	}

	// An invalid access.
	if _, err := Mount(project, kb2, "sometimes", "", identityNow); err == nil {
		t.Fatal("expected an error for an invalid access value")
	}
}

func TestUnmountRemovesTheSymlinkAndTheMount(t *testing.T) {
	cfg, project, kb, kb2 := mountFixture(t)

	if _, err := Mount(project, kb, "", "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)
	if _, err := Mount(project, kb2, "", "robots", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)

	// Unmount by name.
	if err := Unmount(project, "ai-ml", identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(project.KbDir("ai-ml")); !os.IsNotExist(err) {
		t.Fatalf("symlink should be gone: %v", err)
	}
	project = refreshEntry(t, cfg, project.ID)
	if len(project.Mounts) != 1 || project.Mounts[0].Name != "robots" {
		t.Fatalf("mounts after unmount: %+v", project.Mounts)
	}

	// The knowledge base's own pages are untouched.
	if _, err := os.Stat(kb.Wiki()); err != nil {
		t.Fatalf("kb wiki should still exist: %v", err)
	}

	// Unmount by id.
	if err := Unmount(project, kb2.ID, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(project.KbDir("robots")); !os.IsNotExist(err) {
		t.Fatalf("symlink should be gone: %v", err)
	}
	project = refreshEntry(t, cfg, project.ID)
	if len(project.Mounts) != 0 {
		t.Fatalf("mounts after unmount: %+v", project.Mounts)
	}

	// Unmounting an unknown name.
	if err := Unmount(project, "nope", identityNow); err == nil {
		t.Fatal("expected an error for an unknown mount")
	}
}

func TestGrantAndRevoke(t *testing.T) {
	cfg, project, kb, _ := mountFixture(t)

	// A project entry as kb.
	if err := Grant(project, project, vault.AccessRead, identityNow); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("grant with a project as kb: %v", err)
	}

	if err := Grant(kb, project, vault.AccessRead, identityNow); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(kb.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Grants) != 1 || v.Config.Grants[0] != (vault.Grant{ID: project.ID, Name: project.Name, Access: vault.AccessRead}) {
		t.Fatalf("grants: %+v", v.Config.Grants)
	}
	kb = refreshEntry(t, cfg, kb.ID)

	// Granting again replaces it.
	if err := Grant(kb, project, vault.AccessWrite, identityNow); err != nil {
		t.Fatal(err)
	}
	v, err = vault.Open(kb.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Grants) != 1 || v.Config.Grants[0].Access != vault.AccessWrite {
		t.Fatalf("grants after replace: %+v", v.Config.Grants)
	}
	kb = refreshEntry(t, cfg, kb.ID)

	// An invalid access.
	if err := Grant(kb, project, vault.AccessOpen, identityNow); err == nil {
		t.Fatal("expected an error for an invalid access value")
	}

	if err := Revoke(kb, project, identityNow); err != nil {
		t.Fatal(err)
	}
	v, err = vault.Open(kb.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Grants) != 0 {
		t.Fatalf("grants after revoke: %+v", v.Config.Grants)
	}
	kb = refreshEntry(t, cfg, kb.ID)

	if err := Revoke(kb, project, identityNow); err == nil || !strings.Contains(err.Error(), "no grant") {
		t.Fatalf("revoke again: %v", err)
	}

	// After a grant on a guarded kb, the project's mount Effective follows the grant.
	if err := EditIdentity(kb, Edit{Access: strPtr(vault.AccessGuarded)}, identityNow); err != nil {
		t.Fatal(err)
	}
	kb = refreshEntry(t, cfg, kb.ID)
	if _, err := Mount(project, kb, vault.AccessWrite, "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)
	if len(project.Mounts) != 1 || project.Mounts[0].Effective != vault.AccessRead {
		t.Fatalf("effective before grant: %+v", project.Mounts)
	}
	if err := Grant(kb, project, vault.AccessWrite, identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)
	if project.Mounts[0].Effective != vault.AccessWrite {
		t.Fatalf("effective after grant: %+v", project.Mounts)
	}
}

func TestEnsureMountsRecreatesAndPrunes(t *testing.T) {
	cfg, project, kb, kb2 := mountFixture(t)

	if _, err := Mount(project, kb, "", "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)
	if _, err := Mount(project, kb2, "", "robots", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)

	// Delete one symlink.
	if err := os.Remove(project.KbDir("ai-ml")); err != nil {
		t.Fatal(err)
	}
	// Add a stray symlink that no mount names.
	stray := project.KbDir("stray")
	if err := os.Symlink(kb.Wiki(), stray); err != nil {
		t.Fatal(err)
	}

	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	project = *ix.ByID(project.ID)

	created, removed, missing, err := EnsureMounts(project, ix)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(created, ",") != "ai-ml" {
		t.Fatalf("created: %v", created)
	}
	if strings.Join(removed, ",") != "stray" {
		t.Fatalf("removed: %v", removed)
	}
	if len(missing) != 0 {
		t.Fatalf("missing: %v", missing)
	}
	if target, err := os.Readlink(project.KbDir("ai-ml")); err != nil || target != kb.Wiki() {
		t.Fatalf("recreated symlink: %s %v", target, err)
	}
	if _, err := os.Lstat(stray); !os.IsNotExist(err) {
		t.Fatalf("stray should be gone: %v", err)
	}
	if target, err := os.Readlink(project.KbDir("robots")); err != nil || target != kb2.Wiki() {
		t.Fatalf("robots symlink untouched: %s %v", target, err)
	}

	// A mount to an id not in the index: missing names it and nothing else changes.
	ghost := t.TempDir()
	if _, err := vault.Init(ghost, vault.Options{Kind: vault.Knowledge, Name: "ghost"}, identityNow); err != nil {
		t.Fatal(err)
	}
	ghostVault, err := vault.Open(ghost)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.UpdateConfig(project.Path, "mount ghost", identityNow, func(c *vault.Config) error {
		c.Mounts = append(c.Mounts, vault.Mount{ID: ghostVault.Config.ID, Name: "ghost", Access: vault.AccessWrite})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ix, err = registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	project = *ix.ByID(project.ID)

	created, removed, missing, err = EnsureMounts(project, ix)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 0 || len(removed) != 0 {
		t.Fatalf("nothing else should change: created=%v removed=%v", created, removed)
	}
	if strings.Join(missing, ",") != "ghost" {
		t.Fatalf("missing: %v", missing)
	}

	// A real folder at kb/<name> is an error.
	if err := os.Remove(project.KbDir("robots")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project.KbDir("robots"), 0o755); err != nil {
		t.Fatal(err)
	}
	ix, err = registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	project = *ix.ByID(project.ID)
	if _, _, _, err := EnsureMounts(project, ix); err == nil || !strings.Contains(err.Error(), "robots") {
		t.Fatalf("real folder: %v", err)
	}
}
