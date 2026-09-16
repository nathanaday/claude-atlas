package gitx

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func repo(t *testing.T) Repo {
	t.Helper()
	if !Available() {
		t.Skip("git is not installed")
	}
	r := Repo{Dir: t.TempDir()}
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	return r
}

func write(t *testing.T, r Repo, rel, text string) {
	t.Helper()
	path := filepath.Join(r.Dir, rel)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInitCommitLogAndTrailers(t *testing.T) {
	r := repo(t)
	if !r.IsRepo() || r.HasHead() {
		t.Fatal("fresh repo should be a repo without HEAD")
	}
	write(t, r, "wiki/a.md", "a")
	write(t, r, "wiki/sub/b.md", "b")
	entries, err := r.Status()
	if err != nil || len(entries) != 2 {
		t.Fatalf("status %v %v", entries, err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	sha, err := r.Commit("setup: init\n\natlas-operation: setup-1")
	if err != nil || len(sha) != 40 {
		t.Fatalf("commit %q %v", sha, err)
	}
	if dirty, _ := r.Dirty(); dirty {
		t.Fatal("tree should be clean after commit")
	}
	commits, err := r.Log(0)
	if err != nil || len(commits) != 1 {
		t.Fatalf("log %v %v", commits, err)
	}
	c := commits[0]
	if c.SHA != sha || c.Subject != "setup: init" || c.Trailers["atlas-operation"] != "setup-1" || c.Date.IsZero() {
		t.Fatalf("commit %+v", c)
	}
	if !r.Tracked("wiki/a.md") || r.Tracked("wiki/nope.md") {
		t.Fatal("tracked check")
	}
	paths, _ := r.ChangedPaths(sha)
	if len(paths) != 2 {
		t.Fatalf("changed %v", paths)
	}
}

func TestRestoreAndRevert(t *testing.T) {
	r := repo(t)
	write(t, r, "wiki/a.md", "one")
	r.AddAll()
	first, _ := r.Commit("first")
	write(t, r, "wiki/a.md", "two")
	if err := r.RestoreFromHead("wiki/a.md"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "wiki/a.md")); string(data) != "one" {
		t.Fatalf("restore gave %q", data)
	}
	write(t, r, "wiki/a.md", "two")
	r.AddAll()
	second, _ := r.Commit("second")
	if err := r.RevertNoCommit(second); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(r.Dir, "wiki/a.md")); string(data) != "one" {
		t.Fatalf("revert gave %q", data)
	}
	if _, err := r.Commit("undo"); err != nil {
		t.Fatal(err)
	}
	shown, err := r.ShowFile(first, "wiki/a.md")
	if err != nil || string(shown) != "one" {
		t.Fatalf("show %q %v", shown, err)
	}
	// Reverting the first commit now conflicts with nothing but would delete the file; it must not leave a half revert behind.
	write(t, r, "wiki/a.md", "three")
	r.AddAll()
	r.Commit("third")
	if err := r.RevertNoCommit(second); err == nil {
		t.Fatal("reverting a commit whose changes were overwritten should conflict")
	}
	if dirty, _ := r.Dirty(); dirty {
		t.Fatal("a failed revert must be aborted cleanly")
	}
}

func TestPrefixScopesEveryCommand(t *testing.T) {
	if !Available() {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	whole := Repo{Dir: dir}
	if err := whole.Init(); err != nil {
		t.Fatal(err)
	}
	write := func(rel, text string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(text), 0o644)
	}
	write("src/main.go", "package main\n")
	write("atlas/wiki/index.md", "# index\n")
	if err := whole.AddAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := whole.Commit("code and vault"); err != nil {
		t.Fatal(err)
	}
	r := Repo{Dir: dir, Prefix: "atlas/"}
	write("src/main.go", "package main // changed\n")
	write("src/new.go", "package main\n")
	write("atlas/wiki/index.md", "# index\n\nchanged\n")
	write("atlas/wiki/concepts/A.md", "# A\n")
	// Status sees only the vault, with vault-relative paths.
	entries, err := r.Status()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	sort.Strings(paths)
	if strings.Join(paths, ",") != "wiki/concepts/A.md,wiki/index.md" {
		t.Fatalf("status %v", paths)
	}
	// A change outside the vault that the user staged stays staged and uncommitted.
	if _, err := whole.run("add", "src/main.go"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	sha, err := r.Commit("vault only")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := r.ChangedPaths(sha)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(changed)
	if strings.Join(changed, ",") != "wiki/concepts/A.md,wiki/index.md" {
		t.Fatalf("changed %v", changed)
	}
	outside, _ := whole.Status()
	var codes []string
	for _, e := range outside {
		codes = append(codes, e.Code+" "+e.Path)
	}
	sort.Strings(codes)
	if strings.Join(codes, ",") != "?? src/new.go,M  src/main.go" {
		t.Fatalf("outside the vault after the commit: %v", codes)
	}
	// Log lists only commits that touched the vault.
	if _, err := whole.run("commit", "-q", "-m", "code only"); err != nil {
		t.Fatal(err)
	}
	log, err := r.Log(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Subject != "vault only" || log[1].Subject != "code and vault" {
		t.Fatalf("log %+v", log)
	}
	// Paths a method takes are vault-relative.
	if !r.Tracked("wiki/index.md") || r.Tracked("src/main.go") {
		t.Fatal("Tracked must prefix its path")
	}
	if data, err := r.ShowFile("HEAD", "wiki/concepts/A.md"); err != nil || string(data) != "# A\n" {
		t.Fatalf("ShowFile %q %v", data, err)
	}
	if follow, err := r.LogFollow("wiki/index.md"); err != nil || len(follow) != 2 {
		t.Fatalf("LogFollow %+v %v", follow, err)
	}
	write("atlas/wiki/index.md", "scribble\n")
	if err := r.RestoreFromHead("wiki/index.md"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "atlas/wiki/index.md")); string(data) != "# index\n\nchanged\n" {
		t.Fatalf("restore: %q", data)
	}
	if err := r.Add("wiki/concepts/A.md"); err != nil {
		t.Fatal(err)
	}
	if dirty, _ := r.Dirty(); dirty {
		t.Fatal("the vault is clean")
	}
}

func TestNestedRepoIsNotARepo(t *testing.T) {
	r := repo(t)
	inner := Repo{Dir: filepath.Join(r.Dir, "vault")}
	os.MkdirAll(inner.Dir, 0o755)
	if inner.IsRepo() || !inner.InsideOtherRepo() {
		t.Fatal("a directory inside another repo is not its own repo")
	}
}
