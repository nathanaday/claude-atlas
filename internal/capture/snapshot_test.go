package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

func TestStageProjectWritesOneSnapshotIntoTheInbox(t *testing.T) {
	kb := newVault(t)
	code := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(code, 0o755)
	r := gitx.Repo{Dir: code}
	r.Init()
	os.WriteFile(filepath.Join(code, "README.md"), []byte("# code\n"), 0o644)
	r.AddAll()
	first, _ := r.Commit("one")
	kbRef := &registry.Ref{ID: kb.Config.ID, Name: kb.Name(), Path: kb.Root}
	project := registry.Entry{ID: "p-1", Kind: registry.Project, Name: "code", Path: code, Knowledge: kbRef}

	if _, err := StageProject(kb, registry.Entry{Kind: registry.Knowledge, Name: "kb", Path: kb.Root}, now); err == nil || !strings.Contains(err.Error(), "not a project") {
		t.Fatalf("a knowledge base: %v", err)
	}
	other := registry.Entry{ID: "p-2", Kind: registry.Project, Name: "other", Path: code, Knowledge: &registry.Ref{ID: "elsewhere", Path: t.TempDir()}}
	if _, err := StageProject(kb, other, now); err == nil || !strings.Contains(err.Error(), "does not use the knowledge base") {
		t.Fatalf("another knowledge base: %v", err)
	}
	out, err := StageProject(kb, project, now)
	if err != nil {
		t.Fatal(err)
	}
	if !out.New || out.To != "inbox/code-"+first[:7]+".md" || out.Commit != first || out.Since != "" || out.Described != nil {
		t.Fatalf("first stage: %+v", out)
	}
	if _, err := os.Stat(kb.Path(out.To)); err != nil {
		t.Fatal("the snapshot should be in the inbox")
	}
	files, _ := ListInbox(kb, now)
	if len(files) != 1 || files[0].Path != out.To || files[0].Captured {
		t.Fatalf("inbox %+v", files)
	}
	// The same commit again: nothing new.
	if again, err := StageProject(kb, project, now); err != nil || again.New || again.To != out.To {
		t.Fatalf("again: %+v %v", again, err)
	}
	if len(Sources(kb)) != 0 {
		t.Fatal("a snapshot remembers no folder")
	}
	// A page describing the project sets since, and a new commit makes a new snapshot.
	os.MkdirAll(kb.Path("wiki/entities"), 0o755)
	os.WriteFile(kb.Path("wiki/entities/code.md"), []byte("---\ntitle: code\ntype: entity\nentity_type: project\nproject: p-1\ncommit: "+first+"\nstatus: developing\ncreated: 2026-09-17\nupdated: 2026-09-17\ntags:\n  - entity\n---\n\n# code\n"), 0o644)
	os.WriteFile(filepath.Join(code, "b.txt"), []byte("b"), 0o644)
	r.AddAll()
	second, _ := r.Commit("two")
	out, err = StageProject(kb, project, now)
	if err != nil {
		t.Fatal(err)
	}
	if !out.New || out.To != "inbox/code-"+second[:7]+".md" || out.Since != first || out.Described == nil || out.Described.Behind != 1 {
		t.Fatalf("second stage: %+v", out)
	}
	if text, _ := os.ReadFile(kb.Path(out.To)); !strings.Contains(string(text), "## Changes since "+first[:7]) {
		t.Fatal("the log since the described commit")
	}
	// A folder that is not a repository snapshots by date.
	docs := filepath.Join(t.TempDir(), "thesis")
	os.MkdirAll(filepath.Join(docs, "atlas"), 0o755)
	os.WriteFile(filepath.Join(docs, "chapter1.md"), []byte("# One\n"), 0o644)
	plain := registry.Entry{ID: "p-3", Kind: registry.Project, Name: "thesis", Path: docs, Knowledge: kbRef}
	out, err = StageProject(kb, plain, now)
	if err != nil || !out.New || out.To != "inbox/thesis-2026-09-12.md" || out.Commit != "" {
		t.Fatalf("plain folder: %+v %v", out, err)
	}
}
