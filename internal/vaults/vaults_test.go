package vaults

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func TestResolveNewPathFollowsTheCategory(t *testing.T) {
	for _, c := range []struct{ arg, category, want string }{
		{"triage", "", filepath.Join("/vaults", "triage")},
		{"triage", "engineering/usc/", filepath.Join("/vaults", "engineering", "usc", "triage")},
		{"/elsewhere/v", "engineering", "/elsewhere/v"},
	} {
		if got, err := ResolveNewPath(c.arg, "/vaults", c.category); err != nil || got != c.want {
			t.Errorf("ResolveNewPath(%q, %q) = %q, %v; want %q", c.arg, c.category, got, err, c.want)
		}
	}
	got, _ := ResolveNewPath("./rel", "/vaults", "work")
	if filepath.Base(got) != "rel" || strings.HasPrefix(got, "/vaults") {
		t.Fatalf("relative: %s", got)
	}
	if _, err := ResolveNewPath("triage", "/vaults", "../out"); err == nil {
		t.Fatal("a category that climbs out of the tree should fail")
	}
}

func TestCreateRefusesATakenPathAndAVaultInsideAVault(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	outer := filepath.Join(t.TempDir(), "work")
	if _, err := Create(outer, vault.Generic, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(outer, vault.Generic, nil, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("taken path: %v", err)
	}
	inner := filepath.Join(outer, "notes")
	if _, err := Create(inner, vault.Generic, nil, false); err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Fatalf("nested vault: %v", err)
	}
	if _, err := os.Stat(inner); err == nil {
		t.Fatal("the nested vault was created")
	}
}
