package vaults

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func TestCreateListsEveryFileItWrites(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	var out bytes.Buffer
	c := console.NewWith(true, strings.NewReader(""), &out, false)
	res, err := Create(filepath.Join(t.TempDir(), "kb"), vault.Options{Scope: "Things."}, c, true)
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "will create the knowledge base") || !strings.Contains(text, "inbox/.gitkeep") || strings.Contains(text, "task-ledger") {
		t.Fatalf("knowledge base:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("with %d files", len(res.Files))) {
		t.Fatalf("the count should be %d:\n%s", len(res.Files), text)
	}
	v, err := vault.Open(res.Root)
	if err != nil || v.Config.Scope != "Things." || v.Config.Mode != vault.Generic {
		t.Fatalf("created %+v %v", v, err)
	}
	// A declined preview creates nothing.
	declined := filepath.Join(t.TempDir(), "no")
	c = console.NewWith(false, strings.NewReader("n\n"), &out, true)
	if _, err := Create(declined, vault.Options{}, c, true); err != ErrCancelled {
		t.Fatalf("declined: %v", err)
	}
	if _, err := os.Stat(declined); err == nil {
		t.Fatal("a declined create writes nothing")
	}
}

func TestCreateRefusesATakenPathAndAVaultInsideAVault(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	outer := filepath.Join(t.TempDir(), "work")
	if _, err := Create(outer, vault.Options{}, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(outer, vault.Options{}, nil, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("taken path: %v", err)
	}
	inner := filepath.Join(outer, "notes")
	if _, err := Create(inner, vault.Options{}, nil, false); err == nil || !strings.Contains(err.Error(), "inside the knowledge base") {
		t.Fatalf("nested vault: %v", err)
	}
	if _, err := os.Stat(inner); err == nil {
		t.Fatal("the nested vault was created")
	}
}

func TestCreateInsideAProject(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	cfg, h, _, _ := fixtureEntries(t)
	work := filepath.Join(t.TempDir(), "webapp")
	os.MkdirAll(work, 0o755)
	if _, err := InitProject(h, cfg, work, project.Options{}, "", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	kb := filepath.Join(work, "notes")
	res, err := Create(kb, vault.Options{}, nil, false)
	if err != nil {
		t.Fatalf("a vault inside a project: %v", err)
	}
	real, _ := filepath.EvalSymlinks(work)
	if res.Host != real {
		t.Fatalf("a project that is a repository takes the vault's commits: %q", res.Host)
	}
	if _, err := os.Stat(filepath.Join(kb, ".git")); err == nil {
		t.Fatal("no repository inside the project's")
	}

	plain := filepath.Join(t.TempDir(), "thesis")
	os.MkdirAll(plain, 0o755)
	if _, err := InitProject(h, cfg, plain, project.Options{}, "", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	kb = filepath.Join(plain, "notes")
	if res, err = Create(kb, vault.Options{}, nil, false); err != nil || res.Host != "" {
		t.Fatalf("a project in no repository: %+v %v", res, err)
	}
	if !gitx.At(kb).IsRepo() {
		t.Fatal("the vault gets its own repository when nothing holds it")
	}

	os.WriteFile(filepath.Join(work, ".gitignore"), []byte("private/\n"), 0o644)
	ignored := filepath.Join(work, "private", "kb")
	if err := CheckNewPath(ignored); err == nil || !strings.Contains(err.Error(), "is ignored by the git repository") {
		t.Fatalf("an ignored path is refused before anything is written: %v", err)
	}
	if _, err := Create(ignored, vault.Options{}, nil, false); err == nil {
		t.Fatal("create refuses it too")
	}
	if _, err := os.Stat(filepath.Join(work, "private")); err == nil {
		t.Fatal("a refused create writes nothing")
	}
}
