package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// fixture makes a vaults directory with two knowledge bases and two projects, one vault
// outside it, one v1 vault, and one unreadable identity file.
func fixture(t *testing.T) (*home.Config, map[string]*vault.Vault) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{VaultsDir: filepath.Join(root, "Vaults")}
	vs := map[string]*vault.Vault{}
	mk := func(rel string, opts vault.Options) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := vault.Init(path, opts, now); err != nil {
			t.Fatal(err)
		}
		v, _ := vault.Open(path)
		vs[opts.Name] = v
	}
	mk("Vaults/knowledge/ai-ml", vault.Options{Kind: vault.Knowledge, Name: "ai-ml"})
	mk("Vaults/knowledge/deep/nested/robotics", vault.Options{Kind: vault.Knowledge, Name: "robotics"})
	mk("Vaults/projects/cs566", vault.Options{Kind: vault.Project, Name: "cs566"})
	mk("Vaults/projects/self-study", vault.Options{Kind: vault.Project, Name: "self-study"})
	mk("Elsewhere/side", vault.Options{Kind: vault.Project, Name: "side"})
	cfg.Vaults = []string{filepath.Join(root, "Elsewhere", "side")}
	old := filepath.Join(root, "Vaults", "old")
	os.MkdirAll(filepath.Join(old, "wiki"), 0o755)
	os.WriteFile(filepath.Join(old, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	bad := filepath.Join(root, "Vaults", "bad")
	os.MkdirAll(bad, 0o755)
	os.WriteFile(filepath.Join(bad, vault.Marker), []byte(`{not json`), 0o644)
	os.MkdirAll(filepath.Join(root, "Vaults", ".hidden", "v"), 0o755)
	os.WriteFile(filepath.Join(root, "Vaults", ".hidden", "v", vault.Marker), []byte(`{}`), 0o644)
	return cfg, vs
}

func TestScanFindsEveryVaultAndSortsThem(t *testing.T) {
	cfg, vs := fixture(t)
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ix.Entries {
		if e.Error != "" {
			continue
		}
		names = append(names, string(e.Kind)+":"+e.Name)
	}
	if got := strings.Join(names, ","); got != "project:cs566,project:self-study,project:side,knowledge:ai-ml,knowledge:robotics" {
		t.Fatalf("entries %s", got)
	}
	if len(ix.Problems) != 2 {
		t.Fatalf("problems %+v", ix.Problems)
	}
	if len(ix.Entries) != 7 {
		t.Fatalf("entries len %d: %+v", len(ix.Entries), ix.Entries)
	}
	last := ix.Entries[len(ix.Entries)-2:]
	if last[0].Error == "" || last[1].Error == "" || filepath.Base(last[0].Path) != "bad" || filepath.Base(last[1].Path) != "old" {
		t.Fatalf("error entries %+v", last)
	}
	if _, err := ix.Find("old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find old: %v", err)
	}
	for _, e := range ix.Projects() {
		if e.Error != "" {
			t.Fatalf("projects contains an error entry: %+v", e)
		}
	}
	for _, e := range ix.Knowledge() {
		if e.Error != "" {
			t.Fatalf("knowledge contains an error entry: %+v", e)
		}
	}
	for _, p := range ix.Problems {
		switch filepath.Base(p.Path) {
		case "old":
			if !strings.Contains(p.Reason, "v1") {
				t.Errorf("old: %s", p.Reason)
			}
		case "bad":
			if !strings.Contains(p.Reason, "not JSON") && !strings.Contains(p.Reason, "unreadable") {
				t.Errorf("bad: %s", p.Reason)
			}
		default:
			t.Errorf("unexpected problem %+v", p)
		}
	}
	if e := ix.ByID(vs["ai-ml"].Config.ID); e == nil || e.Path != vs["ai-ml"].Root || e.Access != vault.AccessOpen {
		t.Fatalf("by id %+v", e)
	}
	if e := ix.ByPath(vs["side"].Root); e == nil || e.Name != "side" {
		t.Fatalf("by path %+v", e)
	}
	if e, err := ix.Find("CS566"); err != nil || e.Name != "cs566" {
		t.Fatalf("find by name %+v %v", e, err)
	}
	if e, err := ix.Find(vs["robotics"].Config.ID[:8]); err != nil || e.Name != "robotics" {
		t.Fatalf("find by id prefix %+v %v", e, err)
	}
	if e, err := ix.Find(vs["self-study"].Root); err != nil || e.Name != "self-study" {
		t.Fatalf("find by path %+v %v", e, err)
	}
	if _, err := ix.Find("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find missing: %v", err)
	}
	if e := ix.ByID(vs["cs566"].Config.ID); e.Rel() != "projects/cs566" || e.Wiki() != filepath.Join(e.Path, "wiki") || e.RepoDir("hw") != filepath.Join(e.Path, "repos", "hw") {
		t.Fatalf("rel %q wiki %q repodir %q", e.Rel(), e.Wiki(), e.RepoDir("hw"))
	}
}

func TestScanResolvesMountsReposAndGrants(t *testing.T) {
	cfg, vs := fixture(t)
	kb, p := vs["ai-ml"], vs["cs566"]
	// A guarded knowledge base that grants cs566 read; cs566 asks for write; self-study asks for write with no grant.
	if err := vault.UpdateConfig(kb.Root, "guard", now, func(c *vault.Config) error {
		c.Access = vault.AccessGuarded
		c.Grants = []vault.Grant{{ID: p.Config.ID, Name: "cs566", Access: vault.AccessRead}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := vault.UpdateConfig(p.Root, "mount", now, func(c *vault.Config) error {
		c.Mounts = []vault.Mount{{ID: kb.Config.ID, Name: "ai-ml", Access: vault.AccessWrite}, {ID: "00000000-0000-4000-8000-000000000009", Name: "gone", Access: vault.AccessRead}}
		c.Repos = []vault.Repo{{Name: "hw", Changes: "commit"}, {Name: "paper", Remote: "git@x:y/paper.git"}, {Name: "lost"}}
		c.Tags = []string{"usc", "fall"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	self := vs["self-study"]
	if err := vault.UpdateConfig(self.Root, "mount", now, func(c *vault.Config) error {
		c.Mounts = []vault.Mount{{ID: kb.Config.ID, Name: "ai-ml", Access: vault.AccessWrite}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(p.Root, "repos", "hw"), 0o755)
	elsewhere := t.TempDir()
	cfg.Repos = map[string]string{p.Config.ID + "/paper": elsewhere}
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByID(p.Config.ID)
	if e.Rel() != "projects/usc/cs566" {
		t.Fatalf("rel %q", e.Rel())
	}
	if len(e.Mounts) != 2 || e.Mounts[0].Effective != vault.AccessRead || e.Mounts[0].Path != filepath.Join(kb.Root, "wiki") || e.Mounts[1].Error == "" || e.Mounts[1].Effective != "" {
		t.Fatalf("mounts %+v", e.Mounts)
	}
	if len(e.Repos) != 3 || e.Repos[0].Path != filepath.Join(p.Root, "repos", "hw") || e.Repos[1].Path != elsewhere || e.Repos[2].Path != "" || e.Repos[2].Error == "" {
		t.Fatalf("repos %+v", e.Repos)
	}
	k := ix.ByID(kb.Config.ID)
	if len(k.MountedBy) != 2 || k.MountedBy[0].Name != "cs566" || k.MountedBy[0].Access != vault.AccessRead || k.MountedBy[1].Name != "self-study" || k.MountedBy[1].Access != vault.AccessRead {
		t.Fatalf("mounted by %+v", k.MountedBy)
	}
	open := ix.ByID(vs["robotics"].Config.ID)
	if len(open.MountedBy) != 0 {
		t.Fatalf("robotics mounted by %+v", open.MountedBy)
	}
}

func TestStateFileRoundTrips(t *testing.T) {
	cfg, _ := fixture(t)
	ix, _ := Scan(cfg)
	dir := filepath.Join(t.TempDir(), "state")
	if _, _, err := Read(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read before write: %v", err)
	}
	four := 4
	ix.Entries[0].State = &State{GeneratedAt: "2026-09-15T12:00:00Z", VaultOK: true, Pages: &four, Heat: "new", OpenThreads: []string{}}
	if err := Write(dir, ix.Entries, "2026-09-15T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	entries, generated, err := Read(dir)
	if err != nil || generated != "2026-09-15T12:00:00Z" || len(entries) != len(ix.Entries) || entries[0].State == nil || *entries[0].State.Pages != 4 {
		t.Fatalf("round trip %v %s %+v", err, generated, entries)
	}
	data, _ := os.ReadFile(File(dir))
	if !strings.Contains(string(data), `"schema": "claude-atlas.registry.v1"`) {
		t.Fatalf("file:\n%s", data)
	}
}
