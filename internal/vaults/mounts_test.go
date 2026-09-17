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

	// A knowledge base entry as project. The refusal tests the kind it needs, so an entry
	// with no kind gets the same answer.
	if _, err := Mount(kb, project, "", "", identityNow); err == nil || !strings.Contains(err.Error(), "is not a project") {
		t.Fatalf("kb as project: %v", err)
	}
	if _, err := Mount(registry.Entry{Name: "blank"}, kb, "", "", identityNow); err == nil || err.Error() != "blank is not a project" {
		t.Fatalf("an entry with no kind: %v", err)
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

// TestUnmountRefusesAFolderWithFilesInIt proves Unmount looks at the mount's place
// before it touches the identity file: a folder holding files leaves the mount recorded
// and the folder untouched, and an empty one goes with the mount.
func TestUnmountRefusesAFolderWithFilesInIt(t *testing.T) {
	cfg, project, kb, _ := mountFixture(t)

	if _, err := Mount(project, kb, "", "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)

	link := project.KbDir("ai-ml")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(link, "note.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Unmount(project, "ai-ml", identityNow); err == nil || !strings.Contains(err.Error(), "folder holding 1 file") {
		t.Fatalf("a folder with files in it: %v", err)
	}
	v, err := vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Mounts) != 1 {
		t.Fatalf("the mount should stay recorded: %+v", v.Config.Mounts)
	}
	if info, err := os.Stat(link); err != nil || !info.IsDir() {
		t.Fatalf("the folder should still exist: %v", err)
	}

	// Emptied, the folder is the atlas's own leftover and goes with the mount.
	if err := os.Remove(filepath.Join(link, "note.md")); err != nil {
		t.Fatal(err)
	}
	if err := Unmount(project, "ai-ml", identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("the empty folder should be gone: %v", err)
	}
	v, err = vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Mounts) != 0 {
		t.Fatalf("the mount should be gone: %+v", v.Config.Mounts)
	}
}

// A knowledge base that moved out of reach still unmounts: the mount is named by id and
// the dangling symlink goes with it.
func TestUnmountAKnowledgeBaseTheScanLost(t *testing.T) {
	cfg, project, kb, _ := mountFixture(t)

	if _, err := Mount(project, kb, "", "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)
	link := project.KbDir("ai-ml")

	// The knowledge base moves away; the symlink dangles and the scan no longer holds it.
	if err := os.Rename(kb.Path, filepath.Join(t.TempDir(), "ai-ml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(link); err == nil {
		t.Fatal("the symlink should dangle now")
	}

	if err := Unmount(project, kb.ID, identityNow); err != nil {
		t.Fatalf("unmount a knowledge base that moved: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("the dangling symlink should be gone: %v", err)
	}
	v, err := vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Mounts) != 0 {
		t.Fatalf("the mount should be gone: %+v", v.Config.Mounts)
	}
}

// A member reached through a cluster is not unmounted on its own.
func TestUnmountRefusesAMemberOfACluster(t *testing.T) {
	cfg, project, cluster, member := mountFixture(t)

	if err := AddMember(cluster, member, identityNow); err != nil {
		t.Fatal(err)
	}
	cluster = refreshEntry(t, cfg, cluster.ID)
	if _, err := Mount(project, cluster, "", "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)
	if len(project.Mounts) != 2 {
		t.Fatalf("the cluster's member should be a derived mount: %+v", project.Mounts)
	}

	err := Unmount(project, member.ID, identityNow)
	if err == nil || !strings.Contains(err.Error(), cluster.Name) {
		t.Fatalf("unmount a member: %v", err)
	}
	v, err := vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Mounts) != 1 || v.Config.Mounts[0].ID != cluster.ID {
		t.Fatalf("the identity file holds the cluster's mount only: %+v", v.Config.Mounts)
	}

	// The cluster itself unmounts, and the member goes with it.
	if err := Unmount(project, cluster.ID, identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)
	if len(project.Mounts) != 0 {
		t.Fatalf("mounts after unmounting the cluster: %+v", project.Mounts)
	}
}

// An explicit mount wins over the one a cluster derives: a project that mounts a cluster
// may still mount one of its members by name.
func TestMountAMemberOfAMountedCluster(t *testing.T) {
	cfg, project, cluster, member := mountFixture(t)

	if err := AddMember(cluster, member, identityNow); err != nil {
		t.Fatal(err)
	}
	cluster = refreshEntry(t, cfg, cluster.ID)
	if _, err := Mount(project, cluster, "", "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)

	if _, err := Mount(project, member, vault.AccessRead, "", identityNow); err != nil {
		t.Fatalf("mount a member the cluster derives: %v", err)
	}
	v, err := vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Mounts) != 2 {
		t.Fatalf("the identity file should hold both mounts: %+v", v.Config.Mounts)
	}
	project = refreshEntry(t, cfg, project.ID)
	found := FindMount(project, member.ID)
	if found == nil || found.Through != "" || found.Access != vault.AccessRead {
		t.Fatalf("the explicit mount stands: %+v", project.Mounts)
	}
}

func TestSetMountAccess(t *testing.T) {
	cfg, project, kb, _ := mountFixture(t)

	if _, err := Mount(project, kb, vault.AccessWrite, "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)

	if _, err := SetMountAccess(project, "ai-ml", "sideways", identityNow); err == nil || !strings.Contains(err.Error(), "access must be") {
		t.Fatalf("an access that is neither: %v", err)
	}
	if _, err := SetMountAccess(project, "nope", vault.AccessRead, identityNow); err == nil || !strings.Contains(err.Error(), "no mount named") {
		t.Fatalf("a mount that is not there: %v", err)
	}

	m, err := SetMountAccess(project, "ai-ml", vault.AccessRead, identityNow)
	if err != nil || m.Access != vault.AccessRead || m.Name != "ai-ml" {
		t.Fatalf("set read: %+v %v", m, err)
	}
	v, err := vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Mounts) != 1 || v.Config.Mounts[0].Access != vault.AccessRead {
		t.Fatalf("identity file: %+v", v.Config.Mounts)
	}

	// The id names the mount too, and the access it already has writes nothing.
	project = refreshEntry(t, cfg, project.ID)
	if _, err := SetMountAccess(project, kb.ID, vault.AccessRead, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := SetMountAccess(project, kb.ID, vault.AccessWrite, identityNow); err != nil {
		t.Fatal(err)
	}
	if v, err = vault.Open(project.Path); err != nil {
		t.Fatal(err)
	}
	if v.Config.Mounts[0].Access != vault.AccessWrite {
		t.Fatalf("back to write: %+v", v.Config.Mounts)
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

func TestRevokeIDDropsAGrantWhoseProjectIsGone(t *testing.T) {
	cfg, project, kb, _ := mountFixture(t)
	if err := EditIdentity(kb, Edit{Access: strPtr(vault.AccessGuarded)}, identityNow); err != nil {
		t.Fatal(err)
	}
	if err := vault.UpdateConfig(kb.Path, "grant gone-0000", identityNow, func(c *vault.Config) error {
		c.Grants = append(c.Grants, vault.Grant{ID: "gone-0000", Name: "gone", Access: vault.AccessWrite})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	kb = refreshEntry(t, cfg, kb.ID)

	if err := RevokeID(kb, "gone-0000", identityNow); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(kb.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Grants) != 0 {
		t.Fatalf("grants after revoke: %+v", v.Config.Grants)
	}
	kb = refreshEntry(t, cfg, kb.ID)

	if err := RevokeID(kb, "gone-0000", identityNow); err == nil || !strings.Contains(err.Error(), "no grant") {
		t.Fatalf("revoke again: %v", err)
	}

	if err := RevokeID(project, "gone-0000", identityNow); err == nil || !strings.Contains(err.Error(), "not a knowledge base") {
		t.Fatalf("revoke on a project entry: %v", err)
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

	rep, err := EnsureMounts(project, ix)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rep.Created, ",") != "ai-ml" {
		t.Fatalf("created: %v", rep.Created)
	}
	if strings.Join(rep.Removed, ",") != "stray" {
		t.Fatalf("removed: %v", rep.Removed)
	}
	if len(rep.Repaired) != 0 || len(rep.Missing) != 0 {
		t.Fatalf("repaired: %v missing: %v", rep.Repaired, rep.Missing)
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

	rep, err = EnsureMounts(project, ix)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Created) != 0 || len(rep.Repaired) != 0 || len(rep.Removed) != 0 {
		t.Fatalf("nothing else should change: %+v", rep)
	}
	if strings.Join(rep.Missing, ",") != "ghost" {
		t.Fatalf("missing: %v", rep.Missing)
	}

	// A symlink that leads elsewhere is repaired, and says so.
	if err := os.Remove(project.KbDir("ai-ml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kb2.Wiki(), project.KbDir("ai-ml")); err != nil {
		t.Fatal(err)
	}
	rep, err = EnsureMounts(project, ix)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Created) != 0 || strings.Join(rep.Repaired, ",") != "ai-ml" {
		t.Fatalf("a wrong target is repaired, not created: %+v", rep)
	}
	if target, err := os.Readlink(project.KbDir("ai-ml")); err != nil || target != kb.Wiki() {
		t.Fatalf("repaired symlink: %s %v", target, err)
	}

	// A real folder at kb/<name> is an error, and it does not stop the project's other
	// mounts: ai-ml comes first and fails, and robots is still made.
	if err := os.Remove(project.KbDir("ai-ml")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project.KbDir("ai-ml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(project.KbDir("robots")); err != nil {
		t.Fatal(err)
	}
	ix, err = registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	project = *ix.ByID(project.ID)
	rep, err = EnsureMounts(project, ix)
	if err == nil || !strings.Contains(err.Error(), "ai-ml") {
		t.Fatalf("real folder: %v", err)
	}
	if strings.Join(rep.Created, ",") != "robots" {
		t.Fatalf("one bad mount stopped the rest: %+v", rep)
	}
	if target, err := os.Readlink(project.KbDir("robots")); err != nil || target != kb2.Wiki() {
		t.Fatalf("robots after the failure: %s %v", target, err)
	}
}
