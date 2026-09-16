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
	if last[0].Reason != ReasonUnreadable || last[1].Reason != ReasonV1 {
		t.Fatalf("reasons %q %q", last[0].Reason, last[1].Reason)
	}
	if last[1].Rel() != "problems/old" {
		t.Fatalf("rel of an unreadable vault %q", last[1].Rel())
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

func TestAGrantForAnUnknownProjectCarriesAnError(t *testing.T) {
	cfg, vs := fixture(t)
	kb, p := vs["ai-ml"], vs["cs566"]
	if err := vault.UpdateConfig(kb.Root, "guard", now, func(c *vault.Config) error {
		c.Access = vault.AccessGuarded
		c.Grants = []vault.Grant{
			{ID: p.Config.ID, Name: "old name", Access: vault.AccessRead},
			{ID: "gone-0000", Name: "gone", Access: vault.AccessWrite},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByID(kb.Config.ID)
	if len(e.Grants) != 2 {
		t.Fatalf("grants %+v", e.Grants)
	}
	if e.Grants[0].Name != p.Config.Name || e.Grants[0].Error != "" {
		t.Fatalf("grant for a scanned project: %+v", e.Grants[0])
	}
	if e.Grants[1].Error != "no project with id gone-0000" {
		t.Fatalf("grant for an unknown project: %+v", e.Grants[1])
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
	os.WriteFile(File(dir), []byte(`{"schema":"claude-atlas.registry.v9","entries":[]}`), 0o644)
	if _, _, err := Read(dir); err == nil || !strings.Contains(err.Error(), "claude-atlas.registry.v9") {
		t.Fatalf("read another schema: %v", err)
	}
}

// TestScanMakesAMissingRegisteredVaultAnEntry proves a registered path whose folder is
// gone is an entry like every other unreadable vault, so list, doctor, and remove see it.
func TestScanMakesAMissingRegisteredVaultAnEntry(t *testing.T) {
	cfg, _ := fixture(t)
	gone := filepath.Join(t.TempDir(), "gone")
	cfg.Vaults = append(cfg.Vaults, gone)
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByPath(gone)
	if e == nil || e.Reason != ReasonMissing || !strings.Contains(e.Error, "not found") || !strings.Contains(e.Error, "remove") {
		t.Fatalf("missing entry %+v", e)
	}
	matched := false
	for _, p := range ix.Problems {
		if p.Path == gone && p.Reason == e.Error {
			matched = true
		}
	}
	if !matched {
		t.Fatalf("a missing vault keeps its problem, with the same reason: %+v", ix.Problems)
	}
	if _, err := ix.Find(gone); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find a missing vault: %v", err)
	}
	if e.Rel() != "problems/gone" {
		t.Fatalf("rel %q", e.Rel())
	}
}

// TestEffectiveAndKbDir covers what phase 3 reuses: the access two vaults agree on, and
// where a mounted knowledge base's folder sits.
func TestEffectiveAndKbDir(t *testing.T) {
	cases := []struct{ request, grant, want string }{
		{vault.AccessWrite, vault.AccessWrite, vault.AccessWrite},
		{vault.AccessWrite, vault.AccessRead, vault.AccessRead},
		{vault.AccessRead, vault.AccessWrite, vault.AccessRead},
		{vault.AccessRead, vault.AccessRead, vault.AccessRead},
		// A value neither side recognizes reads only: a hand-edited identity file is not
		// validated on the way in, so Effective must fail closed.
		{"", vault.AccessWrite, vault.AccessRead},
		{vault.AccessWrite, "", vault.AccessRead},
		{"WRITE", vault.AccessWrite, vault.AccessRead},
		{vault.AccessWrite, "readwrite", vault.AccessRead},
	}
	for _, c := range cases {
		if got := Effective(c.request, c.grant); got != c.want {
			t.Errorf("Effective(%q, %q) = %q, want %q", c.request, c.grant, got, c.want)
		}
	}
	kb := Entry{Path: filepath.FromSlash("/vaults/knowledge/ai-ml")}
	if got := GrantedAccess(kb, "p1"); got != vault.AccessRead {
		t.Errorf("a knowledge base with no access grants read, got %q", got)
	}
	open := Entry{Access: vault.AccessOpen}
	if got := GrantedAccess(open, "p1"); got != vault.AccessWrite {
		t.Errorf("an open knowledge base grants write, got %q", got)
	}
	guarded := Entry{Access: vault.AccessGuarded, Grants: []Grant{{ID: "p1", Access: vault.AccessWrite}}}
	if got := GrantedAccess(guarded, "p1"); got != vault.AccessWrite {
		t.Errorf("a guarded knowledge base grants what it granted, got %q", got)
	}
	if got := GrantedAccess(guarded, "p2"); got != vault.AccessRead {
		t.Errorf("a project with no grant gets read, got %q", got)
	}
	e := Entry{Path: filepath.FromSlash("/vaults/projects/cs566")}
	if got, want := e.KbDir("ai-ml"), filepath.Join(e.Path, "kb", "ai-ml"); got != want {
		t.Errorf("KbDir = %q, want %q", got, want)
	}
}

// TestScanMountedByFollowsSortedOrder proves MountedBy comes out in the entries' final
// sorted order, not the order the walk happened to visit them in. "z/apple" and
// "a/zebra" put the walk in the opposite order of the projects' names.
func TestScanMountedByFollowsSortedOrder(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{VaultsDir: filepath.Join(root, "Vaults")}
	mk := func(rel string, opts vault.Options) *vault.Vault {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := vault.Init(path, opts, now); err != nil {
			t.Fatal(err)
		}
		v, _ := vault.Open(path)
		return v
	}
	kb := mk("Vaults/knowledge/kb", vault.Options{Kind: vault.Knowledge, Name: "kb"})
	apple := mk("Vaults/z/apple", vault.Options{Kind: vault.Project, Name: "apple"})
	zebra := mk("Vaults/a/zebra", vault.Options{Kind: vault.Project, Name: "zebra"})
	for _, p := range []*vault.Vault{apple, zebra} {
		if err := vault.UpdateConfig(p.Root, "mount", now, func(c *vault.Config) error {
			c.Mounts = []vault.Mount{{ID: kb.Config.ID, Name: "kb", Access: vault.AccessRead}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	k := ix.ByID(kb.Config.ID)
	if len(k.MountedBy) != 2 || k.MountedBy[0].Name != "apple" || k.MountedBy[1].Name != "zebra" {
		t.Fatalf("mounted by order %+v", k.MountedBy)
	}
}

// TestScanDepthLimit proves the boundary: a vault root five directory levels below the
// vaults directory is found, one six levels down is not.
func TestScanDepthLimit(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{VaultsDir: filepath.Join(root, "Vaults")}
	five := filepath.Join(cfg.VaultsDir, "d1", "d2", "d3", "d4", "d5")
	if _, err := vault.Init(five, vault.Options{Kind: vault.Knowledge, Name: "five"}, now); err != nil {
		t.Fatal(err)
	}
	six := filepath.Join(cfg.VaultsDir, "e1", "e2", "e3", "e4", "e5", "d6")
	if _, err := vault.Init(six, vault.Options{Kind: vault.Knowledge, Name: "six"}, now); err != nil {
		t.Fatal(err)
	}
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if e := ix.ByPath(five); e == nil {
		t.Fatalf("a vault root five levels down was not found")
	}
	if e := ix.ByPath(six); e != nil {
		t.Fatalf("a vault root six levels down was found: %+v", e)
	}
}

// TestFindAmbiguousIDPrefix proves Find reports every entry an id prefix matches,
// not just the first.
func TestFindAmbiguousIDPrefix(t *testing.T) {
	ix := &Index{Entries: []Entry{
		{ID: "abcdefgh1111", Kind: vault.Project, Name: "one", Path: "/vaults/one"},
		{ID: "abcdefgh2222", Kind: vault.Project, Name: "two", Path: "/vaults/two"},
	}}
	if _, err := ix.Find("abcdefgh"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("find ambiguous id prefix: %v", err)
	}
}
