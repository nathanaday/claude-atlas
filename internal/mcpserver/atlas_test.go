package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

func TestAtlasReadsWithoutWritingAndRefreshWrites(t *testing.T) {
	if msg := connect(t, t.TempDir()).call("atlas", map[string]any{}, nil); !strings.Contains(msg, "no atlas") {
		t.Fatalf("without an atlas the tool says so: %q", msg)
	}
	h, cfg, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	var out AtlasOut
	if msg := c.call("atlas", map[string]any{}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Vaults) != 2 || out.Settings.VaultsDir != cfg.VaultsDir || out.Settings.RepoChanges != "commit" {
		t.Fatalf("atlas: %+v", out)
	}
	for _, e := range out.Vaults {
		if e.State == nil {
			t.Fatalf("every entry carries its state: %+v", e)
		}
	}
	if _, err := os.Stat(registry.File(h.StateDir())); !os.IsNotExist(err) {
		t.Fatalf("a plain read writes no registry: %v", err)
	}
	if msg := c.call("atlas", map[string]any{"refresh": true}, &out); msg != "" {
		t.Fatal(msg)
	}
	if _, err := os.Stat(registry.File(h.StateDir())); err != nil {
		t.Fatalf("refresh writes the registry: %v", err)
	}
}

func TestVaultCreateAdoptEditForget(t *testing.T) {
	h, cfg, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	var out VaultToolOut
	// A project with tags that mounts kb for writing.
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "q", "tags": []string{"usc"}, "mount": "kb"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Vault == nil || out.Vault.Path != vaults.PathFor(cfg.VaultsDir, vault.Project, "q") || len(out.Vault.Tags) != 1 || len(out.Vault.Mounts) != 1 || out.Vault.Mounts[0].Effective != vault.AccessWrite {
		t.Fatalf("create project: %+v", out.Vault)
	}
	// A guarded knowledge base with a scope, then a cluster that gathers both knowledge bases.
	// out is reset before each reuse: its Vault field is a pointer json.Unmarshal fills
	// in place, so a field the next response omits (zero-valued, omitempty) would
	// otherwise keep leaking the previous response's value.
	out = VaultToolOut{}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "papers", "scope": "Papers.", "access": "guarded"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Vault.Scope != "Papers." || out.Vault.Access != vault.AccessGuarded {
		t.Fatalf("create knowledge: %+v", out.Vault)
	}
	out = VaultToolOut{}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "domain", "members": []string{"kb", "papers"}}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Vault.Members) != 2 {
		t.Fatalf("create cluster: %+v", out.Vault)
	}
	// Refusals: a taken path, a project as a member, an unknown action, a create with no name.
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "q"}, nil); !strings.Contains(msg, "already exists") {
		t.Fatalf("taken path: %q", msg)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "bad", "members": []string{"q"}}, nil); msg == "" {
		t.Fatal("a project cannot be a member")
	}
	if msg := c.call("vault", map[string]any{"action": "rename"}, nil); !strings.Contains(msg, "action must be") {
		t.Fatalf("unknown action: %q", msg)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project"}, nil); !strings.Contains(msg, "needs name") {
		t.Fatalf("no name: %q", msg)
	}
	// Edit: a rename moves the folder; an empty scope clears it; access stays.
	out = VaultToolOut{}
	if msg := c.call("vault", map[string]any{"action": "edit", "target": "papers", "name": "articles", "scope": ""}, &out); msg != "" {
		t.Fatal(msg)
	}
	if filepath.Base(out.Vault.Path) != "articles" || out.Vault.Scope != "" || out.Vault.Access != vault.AccessGuarded {
		t.Fatalf("edit: %+v", out.Vault)
	}
	// Adopt a plain folder outside the vaults directory and forget it; a vault inside cannot be forgotten.
	// A wiki/ folder is what makes it adoptable at all (vault.IsAdoptable): a bare empty
	// directory has none of .obsidian/, wiki/, or an identity file, and vault.Adopt refuses it.
	outside := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(filepath.Join(outside, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	out = VaultToolOut{}
	if msg := c.call("vault", map[string]any{"action": "adopt", "path": outside, "kind": "knowledge"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Vault.Kind != vault.Knowledge || out.Vault.Name != "notes" {
		t.Fatalf("adopt: %+v", out.Vault)
	}
	out = VaultToolOut{}
	if msg := c.call("vault", map[string]any{"action": "forget", "target": outside}, &out); msg != "" || filepath.Base(out.Forgotten) != "notes" {
		t.Fatalf("forget outside: %q %+v", msg, out)
	}
	if msg := c.call("vault", map[string]any{"action": "forget", "target": "q"}, nil); !strings.Contains(msg, "vaults directory") {
		t.Fatalf("forget inside: %q", msg)
	}
}

func TestVaultCreateInARepository(t *testing.T) {
	h, _, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	repo := filepath.Join(t.TempDir(), "code")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	r := gitx.Repo{Dir: repo}
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	var out VaultToolOut
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "code", "in_repo": repo}, &out); msg != "" {
		t.Fatal(msg)
	}
	if filepath.Base(out.Vault.Path) != vault.InRepoDir || filepath.Base(filepath.Dir(out.Vault.Path)) != "code" || out.Vault.Host == "" {
		t.Fatalf("in repo: %+v", out.Vault)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "x", "in_repo": repo, "path": "/tmp/x"}, nil); !strings.Contains(msg, "not both") {
		t.Fatalf("path with in_repo: %q", msg)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "k", "in_repo": repo}, nil); !strings.Contains(msg, "only a project") {
		t.Fatalf("a knowledge base in a repository: %q", msg)
	}
}

func TestMountAccessGrantRevokeUnmount(t *testing.T) {
	h, _, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	// A second project, and kb made guarded so grants decide what it may do.
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "project", "name": "q"}, nil); msg != "" {
		t.Fatal(msg)
	}
	if msg := c.call("vault", map[string]any{"action": "edit", "target": "kb", "access": "guarded"}, nil); msg != "" {
		t.Fatal(msg)
	}
	var out MountToolOut
	if msg := c.call("mount", map[string]any{"action": "mount", "project": "q", "knowledge": "kb", "access": "read"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Mount == nil || out.Mount.Access != vault.AccessRead || out.Mount.Effective != vault.AccessRead {
		t.Fatalf("mount: %+v", out.Mount)
	}
	out = MountToolOut{}
	if msg := c.call("mount", map[string]any{"action": "access", "project": "q", "knowledge": "kb", "access": "write"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Mount.Access != vault.AccessWrite || out.Mount.Effective != vault.AccessRead {
		t.Fatalf("guarded with no grant reads: %+v", out.Mount)
	}
	out = MountToolOut{}
	if msg := c.call("mount", map[string]any{"action": "grant", "knowledge": "kb", "project": "q", "access": "write"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Grants) != 1 || out.Grants[0].Access != vault.AccessWrite || out.Grants[0].Name != "q" {
		t.Fatalf("grant: %+v", out.Grants)
	}
	out = MountToolOut{}
	if msg := c.call("mount", map[string]any{"action": "access", "project": "q", "knowledge": "kb", "access": "read"}, &out); msg != "" || out.Mount.Effective != vault.AccessRead {
		t.Fatalf("asking for read reads: %q %+v", msg, out.Mount)
	}
	out = MountToolOut{}
	if msg := c.call("mount", map[string]any{"action": "access", "project": "q", "knowledge": "kb", "access": "write"}, &out); msg != "" || out.Mount.Effective != vault.AccessWrite {
		t.Fatalf("after the grant the mount writes: %q %+v", msg, out.Mount)
	}
	out = MountToolOut{}
	if msg := c.call("mount", map[string]any{"action": "revoke", "knowledge": "kb", "project": "q"}, &out); msg != "" || len(out.Grants) != 0 {
		t.Fatalf("revoke: %q %+v", msg, out.Grants)
	}
	if msg := c.call("mount", map[string]any{"action": "grant", "knowledge": "kb", "project": "q", "access": "all"}, nil); !strings.Contains(msg, "read or write") {
		t.Fatalf("bad access: %q", msg)
	}
	if msg := c.call("mount", map[string]any{"action": "mount", "project": "kb", "knowledge": "q"}, nil); msg == "" {
		t.Fatal("a knowledge base mounts nothing")
	}
	out = MountToolOut{}
	if msg := c.call("mount", map[string]any{"action": "unmount", "project": "q", "knowledge": "kb"}, &out); msg != "" || len(out.Mounts) != 0 {
		t.Fatalf("unmount: %q %+v", msg, out.Mounts)
	}
}

func TestClusterAddAndRemove(t *testing.T) {
	h, _, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	if msg := c.call("vault", map[string]any{"action": "create", "kind": "knowledge", "name": "domain"}, nil); msg != "" {
		t.Fatal(msg)
	}
	var out ClusterToolOut
	if msg := c.call("cluster", map[string]any{"action": "add", "cluster": "domain", "knowledge": "kb"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Cluster != "domain" || len(out.Members) != 1 || out.Members[0].Name != "kb" {
		t.Fatalf("add: %+v", out)
	}
	if msg := c.call("cluster", map[string]any{"action": "add", "cluster": "domain", "knowledge": "p"}, nil); msg == "" {
		t.Fatal("a project is not a member")
	}
	if msg := c.call("cluster", map[string]any{"action": "remove", "cluster": "domain", "knowledge": "nobody"}, nil); !strings.Contains(msg, "no member") {
		t.Fatalf("unknown member: %q", msg)
	}
	if msg := c.call("cluster", map[string]any{"action": "remove", "cluster": "domain", "knowledge": "kb"}, &out); msg != "" || len(out.Members) != 0 {
		t.Fatalf("remove: %q %+v", msg, out)
	}
}
