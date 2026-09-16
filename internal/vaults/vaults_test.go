package vaults

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func TestCreateListsEveryFileItWrites(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	var out bytes.Buffer
	c := console.NewWith(true, strings.NewReader(""), &out, false)
	res, err := Create(filepath.Join(t.TempDir(), "p"), vault.Options{Kind: vault.Project}, c, true)
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "will create the project") || !strings.Contains(text, vault.TaskLedgerPath) {
		t.Fatalf("project:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("with %d files", len(res.Files))) {
		t.Fatalf("the count should be %d:\n%s", len(res.Files), text)
	}
	out.Reset()
	res, err = Create(filepath.Join(t.TempDir(), "kb"), vault.Options{Kind: vault.Knowledge}, c, true)
	if err != nil {
		t.Fatal(err)
	}
	text = out.String()
	if !strings.Contains(text, "will create the knowledge base") || strings.Contains(text, vault.TaskLedgerPath) {
		t.Fatalf("knowledge base:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("with %d files", len(res.Files))) {
		t.Fatalf("the count should be %d:\n%s", len(res.Files), text)
	}
}

func TestCreateRefusesATakenPathAndAVaultInsideAVault(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	outer := filepath.Join(t.TempDir(), "work")
	if _, err := Create(outer, vault.Options{Kind: vault.Project}, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(outer, vault.Options{Kind: vault.Project}, nil, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("taken path: %v", err)
	}
	inner := filepath.Join(outer, "notes")
	if _, err := Create(inner, vault.Options{Kind: vault.Project}, nil, false); err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Fatalf("nested vault: %v", err)
	}
	if _, err := os.Stat(inner); err == nil {
		t.Fatal("the nested vault was created")
	}
}

func TestCreateInMakesTheProjectAtRepoAtlas(t *testing.T) {
	code := initRepo(t, filepath.Join(t.TempDir(), "code"))
	var out bytes.Buffer
	c := console.NewWith(true, strings.NewReader(""), &out, false)

	path, err := CreateIn(code, vault.Options{Kind: vault.Project, Name: "Notes"}, c)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(code, vault.InRepoDir); path != want {
		t.Fatalf("path %s, want %s", path, want)
	}
	v, err := vault.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if v.Config.Name != "Notes" || v.Config.Kind != vault.Project {
		t.Fatalf("identity file: %+v", v.Config)
	}
	if text := out.String(); !strings.Contains(text, "will create the project") || !strings.Contains(text, "code") {
		t.Fatalf("preview:\n%s", text)
	}

	if _, err := CreateIn(code, vault.Options{Kind: vault.Project}, nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a second project in the same repository: %v", err)
	}

	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateIn(plain, vault.Options{Kind: vault.Project}, nil); err == nil || !strings.Contains(err.Error(), "git repository") {
		t.Fatalf("a folder that is not a repository: %v", err)
	}

	// An empty REPO/atlas is not in the way; vault.InitIn fills it.
	empty := initRepo(t, filepath.Join(t.TempDir(), "empty"))
	if err := os.MkdirAll(filepath.Join(empty, vault.InRepoDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateIn(empty, vault.Options{Kind: vault.Project}, nil); err != nil {
		t.Fatalf("an empty folder at REPO/atlas: %v", err)
	}
}

// TestCreateInRefusesARepositoryInsideAVault holds the rule that a vault never goes
// inside another vault: a project's own repository cannot host a second project.
func TestCreateInRefusesARepositoryInsideAVault(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	if _, _, err := CreateRepo(h, cfg, project, "code", "", identityNow); err != nil {
		t.Fatal(err)
	}
	inner := project.RepoDir("code")
	if _, err := CreateIn(inner, vault.Options{Kind: vault.Project, Name: "Nested"}, nil); err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Fatalf("a repository inside a vault: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inner, vault.InRepoDir)); err == nil {
		t.Fatal("the nested vault was created")
	}
}
