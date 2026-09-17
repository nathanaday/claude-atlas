package repomap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

func TestTakeSnapshot(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git not installed")
	}
	dir := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	os.MkdirAll(filepath.Join(dir, "src/pkg"), 0o755)
	r := gitx.Repo{Dir: dir}
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	repo := registry.Repo{Name: "code", Path: dir, Remote: "git@github.com:me/code.git"}
	if _, err := TakeSnapshot(repo, "", now); err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Fatalf("no commits: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# code\n\nBuild with make.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Readme.md"), []byte("# Code\n\nA thing.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "docs/design.md"), []byte("intro\n\n# The design\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "docs/notes.md"), []byte("no heading\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "src/pkg/a.go"), []byte("package pkg\n"), 0o644)
	r.AddAll()
	first, _ := r.Commit("one")
	s, err := TakeSnapshot(repo, "", now)
	if err != nil {
		t.Fatal(err)
	}
	text := string(s.Content)
	if s.Commit != first || s.Repo != "git@github.com:me/code.git" || s.Since != "" || s.FileName != "code-"+first[:7]+".md" {
		t.Fatalf("snapshot %+v", s)
	}
	for _, want := range []string{"type: repo-snapshot\n", "repo: git@github.com:me/code.git\n", "commit: " + first + "\n", "branch: main\n", "taken: 2026-09-17\n", "since: \"\"\n",
		"## CLAUDE.md\n\n````markdown\n# code\n\nBuild with make.\n````\n", "## Readme.md\n\n````markdown\n# Code\n\nA thing.\n````\n", "## Files\n\n5 tracked files:\n\n- CLAUDE.md\n", "- src/pkg/a.go\n",
		"## Docs\n\n- docs/design.md — The design\n- docs/notes.md\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Changes since") {
		t.Error("no since, no log")
	}
	// A second commit and since: the log appears; since at HEAD or unknown is dropped.
	os.WriteFile(filepath.Join(dir, "src/pkg/b.go"), []byte("package pkg\n"), 0o644)
	r.AddAll()
	second, _ := r.Commit("add b")
	s, err = TakeSnapshot(repo, first, now)
	if err != nil {
		t.Fatal(err)
	}
	text = string(s.Content)
	if s.Since != first || !strings.Contains(text, "since: \""+first+"\"\n") || !strings.Contains(text, "## Changes since "+first[:7]+"\n\n```text\n"+second[:7]+" ") || !strings.Contains(text, "add b\n") || !strings.Contains(text, "src/pkg/b.go") {
		t.Fatalf("log since first:\n%s", text)
	}
	if s, _ = TakeSnapshot(repo, second, now); s.Since != "" {
		t.Fatal("since at HEAD is dropped")
	}
	if s, _ = TakeSnapshot(repo, "0000000", now); s.Since != "" {
		t.Fatal("an unknown since is dropped")
	}
	if s, _ = TakeSnapshot(registry.Repo{Name: "code", Path: dir}, "", now); s.Repo != "code" || !strings.Contains(string(s.Content), "repo: code\n") {
		t.Fatal("no remote: the name")
	}
	if _, err := TakeSnapshot(registry.Repo{Name: "x"}, "", now); err == nil {
		t.Fatal("no folder")
	}
	if _, err := TakeSnapshot(registry.Repo{Name: "x", Path: t.TempDir()}, "", now); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("plain folder: %v", err)
	}
}

func TestFoldersAndReadme(t *testing.T) {
	files := []string{"README", "a/b/c.go", "a/b/d.go", "a/e.go", "f.go", "g/h.md"}
	if got := strings.Join(folders(files), " "); got != "a a/b g" {
		t.Fatalf("folders %q", got)
	}
	if readmeName(files) != "README" || readmeName([]string{"a/README.md", "readme.rst"}) != "readme.rst" || readmeName([]string{"a.go"}) != "" {
		t.Fatal("readme")
	}
}
