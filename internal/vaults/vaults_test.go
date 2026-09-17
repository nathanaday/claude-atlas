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
	if _, err := Create(inner, vault.Options{}, nil, false); err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Fatalf("nested vault: %v", err)
	}
	if _, err := os.Stat(inner); err == nil {
		t.Fatal("the nested vault was created")
	}
}
