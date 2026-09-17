package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

func TestStageRepoWritesOneSnapshotIntoTheInbox(t *testing.T) {
	v := newVault(t)
	code := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(code, 0o755)
	r := gitx.Repo{Dir: code}
	r.Init()
	os.WriteFile(filepath.Join(code, "README.md"), []byte("# code\n"), 0o644)
	r.AddAll()
	first, _ := r.Commit("one")
	project := registry.Entry{Name: "p", Path: v.Root, Repos: []registry.Repo{{Name: "code", Path: code}}}

	if _, err := StageRepo(v, project, "nope", now); err == nil || !strings.Contains(err.Error(), "no repository named nope; it has code") {
		t.Fatalf("unknown name: %v", err)
	}
	if _, err := StageRepo(v, registry.Entry{Name: "p", Path: v.Root}, "code", now); err == nil || !strings.Contains(err.Error(), "has no repositories") {
		t.Fatalf("no repositories: %v", err)
	}
	out, err := StageRepo(v, project, "Code", now)
	if err != nil {
		t.Fatal(err)
	}
	if !out.New || out.To != "inbox/code-"+first[:7]+".md" || out.Commit != first || out.Since != "" || out.Described != nil {
		t.Fatalf("first stage: %+v", out)
	}
	if _, err := os.Stat(v.Path(out.To)); err != nil {
		t.Fatal("the snapshot should be in the inbox")
	}
	files, _ := ListInbox(v, nil, now)
	if len(files) != 1 || files[0].Path != out.To || files[0].Captured {
		t.Fatalf("inbox %+v", files)
	}
	// The same commit again: nothing new.
	if again, err := StageRepo(v, project, "code", now); err != nil || again.New || again.To != out.To {
		t.Fatalf("again: %+v %v", again, err)
	}
	if len(Sources(v)) != 0 {
		t.Fatal("a snapshot remembers no folder")
	}
	// A page describing the repository sets since, and a new commit makes a new snapshot.
	os.MkdirAll(v.Path("wiki/entities"), 0o755)
	os.WriteFile(v.Path("wiki/entities/code.md"), []byte("---\ntitle: code\ntype: entity\nentity_type: repository\nrepo: code\ncommit: "+first+"\nstatus: developing\ncreated: 2026-09-17\nupdated: 2026-09-17\ntags:\n  - entity\n---\n\n# code\n"), 0o644)
	os.WriteFile(filepath.Join(code, "b.txt"), []byte("b"), 0o644)
	r.AddAll()
	second, _ := r.Commit("two")
	out, err = StageRepo(v, project, "code", now)
	if err != nil {
		t.Fatal(err)
	}
	if !out.New || out.To != "inbox/code-"+second[:7]+".md" || out.Since != first || out.Described == nil || out.Described.Behind != 1 {
		t.Fatalf("second stage: %+v", out)
	}
	if text, _ := os.ReadFile(v.Path(out.To)); !strings.Contains(string(text), "## Changes since "+first[:7]) {
		t.Fatal("the log since the described commit")
	}
}
