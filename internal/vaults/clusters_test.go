package vaults

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// clusterFixture makes three knowledge bases and one project under a temp vaults
// directory and returns the config and the scanned entries, keyed by name.
func clusterFixture(t *testing.T) (*home.Config, map[string]registry.Entry) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{Schema: home.ConfigSchema, VaultsDir: filepath.Join(root, "Vaults")}
	for _, name := range []string{"p3", "software", "people"} {
		path := filepath.Join(cfg.VaultsDir, "knowledge", name)
		if _, err := vault.Init(path, vault.Options{Kind: vault.Knowledge, Name: name}, identityNow); err != nil {
			t.Fatal(err)
		}
	}
	vision := filepath.Join(cfg.VaultsDir, "projects", "vision")
	if _, err := vault.Init(vision, vault.Options{Kind: vault.Project, Name: "vision"}, identityNow); err != nil {
		t.Fatal(err)
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]registry.Entry{}
	for _, e := range ix.Entries {
		found[e.Name] = e
	}
	for _, name := range []string{"p3", "software", "people", "vision"} {
		if found[name].ID == "" {
			t.Fatalf("fixture missing %s: %+v", name, ix.Entries)
		}
	}
	return cfg, found
}

// AddMember and RemoveMember edit the cluster's own identity file and refuse what the
// scan says is wrong.
func TestAddAndRemoveMembers(t *testing.T) {
	cfg, all := clusterFixture(t)
	p3, software, people, vision := all["p3"], all["software"], all["people"], all["vision"]

	if IsCluster(p3) {
		t.Fatal("a knowledge base with no members is not a cluster")
	}
	if err := AddMember(p3, software, identityNow); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(p3.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Members) != 1 || v.Config.Members[0] != (vault.Member{ID: software.ID, Name: "software"}) {
		t.Fatalf("members: %+v", v.Config.Members)
	}
	p3 = refreshEntry(t, cfg, p3.ID)
	if !IsCluster(p3) {
		t.Fatal("a knowledge base with a member is a cluster")
	}

	// The same member twice.
	if err := AddMember(p3, software, identityNow); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("add the same member again: %v", err)
	}
	// A project is no member.
	if err := AddMember(p3, vision, identityNow); err == nil || !strings.Contains(err.Error(), "not a knowledge base") {
		t.Fatalf("a project as a member: %v", err)
	}
	// A project holds no members.
	if err := AddMember(vision, people, identityNow); err == nil || !strings.Contains(err.Error(), "not a knowledge base") {
		t.Fatalf("a project as a cluster: %v", err)
	}
	// Its own member.
	if err := AddMember(p3, p3, identityNow); err == nil || !strings.Contains(err.Error(), "its own member") {
		t.Fatalf("a cluster holding itself: %v", err)
	}
	// A cluster as a member.
	if err := AddMember(software, p3, identityNow); err == nil || !strings.Contains(err.Error(), "a cluster") {
		t.Fatalf("a cluster as a member: %v", err)
	}

	// Remove by name.
	if err := RemoveMember(p3, "software", identityNow); err != nil {
		t.Fatal(err)
	}
	if v, err = vault.Open(p3.Path); err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Members) != 0 {
		t.Fatalf("members after remove: %+v", v.Config.Members)
	}
	p3 = refreshEntry(t, cfg, p3.ID)
	if IsCluster(p3) {
		t.Fatal("the last member gone, the knowledge base is ordinary again")
	}
	if err := RemoveMember(p3, "software", identityNow); err == nil || !strings.Contains(err.Error(), "no member") {
		t.Fatalf("remove again: %v", err)
	}

	// A member the scan lost still drops, by its id.
	if err := vault.UpdateConfig(p3.Path, "member gone", identityNow, func(c *vault.Config) error {
		c.Members = append(c.Members, vault.Member{ID: "gone-0000", Name: "gone"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p3 = refreshEntry(t, cfg, p3.ID)
	if err := RemoveMember(p3, "gone-0000", identityNow); err != nil {
		t.Fatalf("remove a member the scan lost: %v", err)
	}
	if v, err = vault.Open(p3.Path); err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Members) != 0 {
		t.Fatalf("members after remove by id: %+v", v.Config.Members)
	}

	// A project holds no members to remove.
	if err := RemoveMember(vision, "software", identityNow); err == nil || !strings.Contains(err.Error(), "not a knowledge base") {
		t.Fatalf("remove from a project: %v", err)
	}
}
