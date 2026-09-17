package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var now = time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local)

func TestInitWritesTheIdentityFileAndTheFolders(t *testing.T) {
	work := filepath.Join(t.TempDir(), "webapp")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	p, written, err := Init(work, Options{Description: "The web app.", Knowledge: &Knowledge{ID: "kb-1", Name: "product"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "webapp" || p.Config.Schema != Schema || p.Config.ID == "" || p.Config.Created != "2026-09-17" || p.Config.Description != "The web app." {
		t.Fatalf("config %+v", p.Config)
	}
	if p.Config.Knowledge == nil || p.Config.Knowledge.ID != "kb-1" || p.Config.Knowledge.Name != "product" {
		t.Fatalf("knowledge %+v", p.Config.Knowledge)
	}
	if strings.Join(written, ",") != "tasks/,tasks/archive/,phases/,inbox/,project.json" {
		t.Fatalf("written %v", written)
	}
	for _, dir := range Folders {
		if info, err := os.Stat(p.Path(dir)); err != nil || !info.IsDir() {
			t.Fatalf("%s should be a folder", dir)
		}
	}
	if !IsProject(work) || p.Atlas() != filepath.Join(work, Dir) || MarkerPath(work) != filepath.Join(work, Dir, Marker) {
		t.Fatal("layout")
	}
	data, _ := os.ReadFile(MarkerPath(work))
	if !strings.Contains(string(data), `"schema": "claude-atlas.project.v3"`) || strings.Contains(string(data), work) {
		t.Fatalf("the identity file holds no path:\n%s", data)
	}
	// The name comes from the caller when given, and an empty knowledge id is none.
	other := filepath.Join(t.TempDir(), "docs")
	os.MkdirAll(other, 0o755)
	q, _, err := Init(other, Options{Name: " Thesis ", Knowledge: &Knowledge{}}, now)
	if err != nil || q.Name() != "Thesis" || q.Config.Knowledge != nil {
		t.Fatalf("%+v %v", q.Config, err)
	}
}

func TestInitRefusals(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "webapp")
	os.MkdirAll(work, 0o755)
	if _, _, err := Init(filepath.Join(root, "missing"), Options{}, now); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("a missing folder: %v", err)
	}
	file := filepath.Join(root, "file.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	if _, _, err := Init(file, Options{}, now); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("a file: %v", err)
	}
	if _, _, err := Init(work, Options{}, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Init(work, Options{}, now); err == nil || !strings.Contains(err.Error(), "is a project already") {
		t.Fatalf("twice: %v", err)
	}
	nested := filepath.Join(work, "src", "pkg")
	os.MkdirAll(nested, 0o755)
	if _, _, err := Init(nested, Options{}, now); err == nil || !strings.Contains(err.Error(), "inside the project") {
		t.Fatalf("inside another project: %v", err)
	}
	taken := filepath.Join(root, "taken")
	os.MkdirAll(filepath.Join(taken, Dir, "something"), 0o755)
	if _, _, err := Init(taken, Options{}, now); err == nil || !strings.Contains(err.Error(), "not a project") {
		t.Fatalf("a foreign atlas/ folder: %v", err)
	}
	empty := filepath.Join(root, "emptyatlas")
	os.MkdirAll(filepath.Join(empty, Dir), 0o755)
	if _, _, err := Init(empty, Options{}, now); err != nil {
		t.Fatalf("an empty atlas/ folder is fine: %v", err)
	}
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	kb := filepath.Join(root, "kb")
	if _, err := vault.Init(kb, vault.Options{Name: "kb"}, now); err != nil {
		t.Fatal(err)
	}
	inKB := filepath.Join(kb, "wiki", "work")
	os.MkdirAll(inKB, 0o755)
	if _, _, err := Init(inKB, Options{}, now); err == nil || !strings.Contains(err.Error(), "inside the knowledge base") {
		t.Fatalf("inside a knowledge base: %v", err)
	}
}

func TestOpenFindAboveSaveAndEnsureFolders(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "webapp")
	os.MkdirAll(work, 0o755)
	if _, err := Open(work); err == nil || !strings.Contains(err.Error(), "no "+Dir+"/"+Marker) {
		t.Fatalf("open before init: %v", err)
	}
	if FindAbove(work) != "" {
		t.Fatal("nothing above yet")
	}
	p, _, err := Init(work, Options{}, now)
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(work, "src", "deep")
	os.MkdirAll(nested, 0o755)
	deepFile := filepath.Join(nested, "main.go")
	os.WriteFile(deepFile, []byte("package main"), 0o644)
	if FindAbove(nested) != work || FindAbove(deepFile) != work || FindAbove(work) != work {
		t.Fatalf("FindAbove: %q %q", FindAbove(nested), FindAbove(deepFile))
	}
	if FindAbove(root) != "" {
		t.Fatal("the parent is not inside the project")
	}
	opened, err := Open(work)
	if err != nil || opened.Config.ID != p.Config.ID || opened.Root != work {
		t.Fatalf("%+v %v", opened, err)
	}
	opened.Config.Name = " Renamed "
	opened.Config.Description = " Now with words. "
	opened.Config.Knowledge = &Knowledge{ID: "kb-2", Name: "notes"}
	if err := opened.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(work)
	if err != nil || again.Name() != "Renamed" || again.Config.Description != "Now with words." || again.Config.Knowledge.ID != "kb-2" {
		t.Fatalf("saved %+v %v", again.Config, err)
	}
	again.Config.Name = "  "
	if err := again.Save(); err == nil {
		t.Fatal("a blank name is refused")
	}
	again.Config.Name = "x"
	again.Config.Knowledge = &Knowledge{Name: "orphan"}
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	if reread, _ := Open(work); reread.Config.Knowledge != nil {
		t.Fatal("a knowledge reference without an id is dropped")
	}
	// A clone without empty folders gets them back.
	os.RemoveAll(filepath.Join(work, Dir, PhasesDir))
	os.RemoveAll(filepath.Join(work, Dir, InboxDir))
	if err := again.EnsureFolders(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range Folders {
		if info, err := os.Stat(again.Path(dir)); err != nil || !info.IsDir() {
			t.Fatalf("%s should be back", dir)
		}
	}
	// Bad identity files.
	os.WriteFile(MarkerPath(work), []byte("{not json"), 0o644)
	if _, ok := ReadConfig(work); ok {
		t.Fatal("ReadConfig reports a file that is not JSON")
	}
	if _, err := Open(work); err == nil {
		t.Fatal("Open refuses a file that is not JSON")
	}
	os.WriteFile(MarkerPath(work), []byte(`{"schema":"claude-atlas.project.v9","id":"x","name":"x"}`), 0o644)
	if _, err := Open(work); err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("schema: %v", err)
	}
	os.WriteFile(MarkerPath(work), []byte(`{"schema":"`+Schema+`","name":"x"}`), 0o644)
	if _, err := Open(work); err == nil || !strings.Contains(err.Error(), "no id") {
		t.Fatalf("id: %v", err)
	}
	os.WriteFile(MarkerPath(work), []byte(`{"schema":"`+Schema+`","id":"x"}`), 0o644)
	if p, err := Open(work); err != nil || p.Name() != "webapp" {
		t.Fatalf("a nameless project takes the folder's name: %+v %v", p, err)
	}
}
