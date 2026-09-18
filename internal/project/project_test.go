package project

import (
	"errors"
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
	if strings.Join(written, ",") != "threads/,threads/archive/,stubs/,specs/,plans/,receipts/,phases/,inbox/,project.json,.obsidian/snippets/claude-atlas.css" {
		t.Fatalf("written %v", written)
	}
	for _, dir := range Folders {
		if info, err := os.Stat(p.Path(dir)); err != nil || !info.IsDir() {
			t.Fatalf("%s should be a folder", dir)
		}
	}
	if !IsProject(work) || p.Folder != "webapp" || p.Atlas() != filepath.Join(work, Dir, "webapp") || p.Rel() != "atlas/webapp" {
		t.Fatalf("layout %+v", p)
	}
	data, _ := os.ReadFile(filepath.Join(work, Dir, "webapp", Marker))
	if !strings.Contains(string(data), `"schema": "claude-atlas.project.v3"`) || strings.Contains(string(data), work) {
		t.Fatalf("the identity file holds no path:\n%s", data)
	}
	// The name comes from the caller when given, and an empty knowledge id is none.
	other := filepath.Join(t.TempDir(), "docs")
	os.MkdirAll(other, 0o755)
	q, _, err := Init(other, Options{Name: " Thesis ", Knowledge: &Knowledge{}}, now)
	if err != nil || q.Name() != "Thesis" || q.Config.Knowledge != nil || q.Folder != "Thesis" {
		t.Fatalf("%+v %v", q.Config, err)
	}
	// The folder is the name cleaned for Obsidian.
	third := filepath.Join(t.TempDir(), "third")
	os.MkdirAll(third, 0o755)
	if r, _, err := Init(third, Options{Name: "Web: v2/beta"}, now); err != nil || r.Folder != "Web- v2-beta" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, _, err := Init(t.TempDir(), Options{Name: "///"}, now); err == nil || !strings.Contains(err.Error(), "no usable folder name") {
		t.Fatalf("a name with no usable folder: %v", err)
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
	os.MkdirAll(filepath.Join(taken, Dir, "taken", "notes"), 0o755)
	if _, _, err := Init(taken, Options{}, now); err == nil || !strings.Contains(err.Error(), "not a project") {
		t.Fatalf("a foreign atlas/<name>/ folder: %v", err)
	}
	if _, _, err := Init(taken, Options{Name: "other"}, now); err != nil {
		t.Fatalf("atlas/ may hold other folders: %v", err)
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
	if _, err := Open(work); !errors.Is(err, ErrNotProject) {
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
	if err != nil || again.Name() != "Renamed" || again.Folder != "Renamed" || again.Config.Description != "Now with words." || again.Config.Knowledge.ID != "kb-2" {
		t.Fatalf("saved %+v %v", again.Config, err)
	}
	again.Config.Name = "  "
	if err := again.Save(); err == nil {
		t.Fatal("a blank name is refused")
	}
	again.Config.Name = "webapp"
	again.Config.Knowledge = &Knowledge{Name: "orphan"}
	if err := again.Save(); err != nil {
		t.Fatal(err)
	}
	if reread, _ := Open(work); reread.Config.Knowledge != nil {
		t.Fatal("a knowledge reference without an id is dropped")
	}
	// A clone without empty folders gets them back.
	os.RemoveAll(again.Path(PhasesDir))
	os.RemoveAll(again.Path(InboxDir))
	if err := again.EnsureFolders(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range Folders {
		if info, err := os.Stat(again.Path(dir)); err != nil || !info.IsDir() {
			t.Fatalf("%s should be back", dir)
		}
	}
	// Bad identity files.
	marker := again.Path(Marker)
	os.WriteFile(marker, []byte("{not json"), 0o644)
	if _, ok := ReadConfig(work); ok {
		t.Fatal("ReadConfig reports a file that is not JSON")
	}
	if _, err := Open(work); err == nil {
		t.Fatal("Open refuses a file that is not JSON")
	}
	os.WriteFile(marker, []byte(`{"schema":"claude-atlas.project.v9","id":"x","name":"x"}`), 0o644)
	if _, err := Open(work); err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("schema: %v", err)
	}
	os.WriteFile(marker, []byte(`{"schema":"`+Schema+`","name":"x"}`), 0o644)
	if _, err := Open(work); err == nil || !strings.Contains(err.Error(), "no id") {
		t.Fatalf("id: %v", err)
	}
	os.WriteFile(marker, []byte(`{"schema":"`+Schema+`","id":"x"}`), 0o644)
	if p, err := Open(work); err != nil || p.Name() != "webapp" {
		t.Fatalf("a nameless project takes the folder's name: %+v %v", p, err)
	}
	// A work folder holds one project.
	os.MkdirAll(filepath.Join(work, Dir, "second"), 0o755)
	os.WriteFile(filepath.Join(work, Dir, "second", Marker), []byte(`{}`), 0o644)
	if _, err := Open(work); err == nil || !strings.Contains(err.Error(), "holds 2 projects") {
		t.Fatalf("two projects: %v", err)
	}
}

func TestRenameMovesTheFolder(t *testing.T) {
	work := filepath.Join(t.TempDir(), "webapp")
	os.MkdirAll(work, 0o755)
	p, _, err := Init(work, Options{}, now)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p.Path("stubs/Fix it.md"), []byte("x"), 0o644)
	p.Config.Name = "Web App"
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	if p.Folder != "Web App" {
		t.Fatalf("folder %q", p.Folder)
	}
	if _, err := os.Stat(filepath.Join(work, Dir, "webapp")); !os.IsNotExist(err) {
		t.Fatal("the old folder is gone")
	}
	if opened, err := Open(work); err != nil || opened.Folder != "Web App" || opened.Name() != "Web App" {
		t.Fatalf("%+v %v", opened, err)
	}
	if _, err := os.Stat(filepath.Join(work, Dir, "Web App", "stubs", "Fix it.md")); err != nil {
		t.Fatal("the pages move with the folder")
	}
	// A change of case alone is a rename, on any filesystem.
	p.Config.Name = "web app"
	if err := p.Save(); err != nil || p.Folder != "web app" {
		t.Fatalf("case: %q %v", p.Folder, err)
	}
	// A taken folder refuses the whole save.
	os.MkdirAll(filepath.Join(work, Dir, "Other"), 0o755)
	p.Config.Name = "Other"
	if err := p.Save(); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("taken: %v", err)
	}
	if opened, _ := Open(work); opened.Name() != "web app" || p.Folder != "web app" {
		t.Fatalf("nothing changed: %+v", opened)
	}
}

func TestUpgradeMovesAFlatProject(t *testing.T) {
	work := filepath.Join(t.TempDir(), "webapp")
	flat := filepath.Join(work, Dir)
	// 2.2.0 and earlier held tasks/, phases/, and inbox/ directly in atlas/. A project
	// named like one of its own folders moves as cleanly as any other.
	for _, dir := range []string{"tasks/archive", "phases", "inbox"} {
		os.MkdirAll(filepath.Join(flat, dir), 0o755)
	}
	os.WriteFile(filepath.Join(flat, Marker), []byte(`{"schema":"`+Schema+`","id":"id-1","name":"tasks"}`), 0o644)
	os.WriteFile(filepath.Join(flat, "tasks", "Fix it.md"), []byte("x"), 0o644)
	if !IsProject(work) || FindAbove(filepath.Join(flat, "tasks")) != work {
		t.Fatal("a flat project is still found, so a session can say what to do")
	}
	if _, err := Open(work); !errors.Is(err, ErrFlat) || !strings.Contains(err.Error(), "claude-atlas upgrade") {
		t.Fatalf("open a flat project: %v", err)
	}
	if _, ok := ReadConfig(work); ok {
		t.Fatal("ReadConfig reads only the current layout")
	}
	moved, err := Upgrade(work)
	if err != nil || !moved {
		t.Fatalf("upgrade: %v %v", moved, err)
	}
	p, err := Open(work)
	if err != nil || p.Folder != "tasks" || p.Config.ID != "id-1" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := os.Stat(p.Path("tasks/Fix it.md")); err != nil {
		t.Fatal("the pages moved")
	}
	if entries, _ := os.ReadDir(flat); len(entries) != 1 {
		t.Fatalf("atlas/ holds the project folder only: %v", entries)
	}
	if moved, err := Upgrade(work); err != nil || moved {
		t.Fatalf("a second upgrade does nothing: %v %v", moved, err)
	}
	if _, err := Upgrade(t.TempDir()); !errors.Is(err, ErrNotProject) {
		t.Fatalf("not a project: %v", err)
	}
}

func TestAFileNamedAtlasIsNotAProject(t *testing.T) {
	work := t.TempDir()
	os.WriteFile(filepath.Join(work, Dir), []byte("a binary"), 0o755)
	if IsProject(work) || FindAbove(work) != "" {
		t.Fatal("a file named atlas made the folder a project")
	}
}
