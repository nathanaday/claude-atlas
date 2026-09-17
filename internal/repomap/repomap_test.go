package repomap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

func page(t *testing.T, dir, name, front string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, name), []byte("---\n"+front+"---\n\n# Page\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commits(t *testing.T, dir string, n int) []string {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git not installed")
	}
	r := gitx.Repo{Dir: dir}
	os.MkdirAll(dir, 0o755)
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	var shas []string
	for i := 0; i < n; i++ {
		os.WriteFile(filepath.Join(dir, "a.txt"), []byte{byte('a' + i)}, 0o644)
		r.AddAll()
		sha, err := r.Commit("c")
		if err != nil {
			t.Fatal(err)
		}
		shas = append(shas, sha)
	}
	return shas
}

func TestDescribeFindsThePageAndCountsBehind(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "proj")
	kb := filepath.Join(root, "kb")
	code := filepath.Join(root, "code")
	shas := commits(t, code, 3)
	entry := registry.Entry{Name: "proj", Path: project, Mounts: []registry.Mount{{Name: "tools", Path: filepath.Join(kb, "wiki")}}}
	repo := registry.Repo{Name: "code", Path: code, Remote: "git@github.com:me/code.git"}

	if got := Describe(entry, repo); got != nil {
		t.Fatalf("no pages yet: %+v", got)
	}
	// A page in the project's wiki, matched by name, with a commit that is history.
	page(t, filepath.Join(project, "wiki/entities"), "code.md", "title: code\ntype: entity\nentity_type: repository\nrepo: code\ncommit: "+shas[0]+"\n")
	got := Describe(entry, repo)
	if got == nil || got.Page != "wiki/entities/code.md" || got.In != "proj" || got.Behind != 2 {
		t.Fatalf("project page: %+v", got)
	}
	// A page in a knowledge base wins, matched by the remote in another spelling.
	page(t, filepath.Join(kb, "wiki/entities"), "Code.md", "title: Code\ntype: entity\nentity_type: repository\nrepo: https://github.com/me/code\ncommit: "+shas[2][:7]+"\n")
	got = Describe(entry, repo)
	if got == nil || got.Page != "wiki/entities/Code.md" || got.In != "tools" || got.Behind != 0 {
		t.Fatalf("knowledge base page: %+v", got)
	}
	// Two in the knowledge base: the one nearest HEAD wins; an unknown commit is -1 and loses.
	page(t, filepath.Join(kb, "wiki/entities"), "Old.md", "title: Old\ntype: entity\nentity_type: repository\nrepo: code\ncommit: 0123456\n")
	got = Describe(entry, repo)
	if got == nil || got.Page != "wiki/entities/Code.md" {
		t.Fatalf("nearest wins: %+v", got)
	}
	os.Remove(filepath.Join(kb, "wiki/entities/Code.md"))
	got = Describe(entry, repo)
	if got == nil || got.Page != "wiki/entities/Old.md" || got.Behind != -1 {
		t.Fatalf("unknown commit: %+v", got)
	}
	// Other entities and pages with another repo do not match.
	page(t, filepath.Join(kb, "wiki/entities"), "Other.md", "title: Other\ntype: entity\nentity_type: repository\nrepo: other\ncommit: "+shas[1]+"\n")
	page(t, filepath.Join(kb, "wiki/entities"), "Org.md", "title: Org\ntype: entity\nentity_type: organization\nrepo: code\n")
	if got = Describe(entry, repo); got == nil || got.Page != "wiki/entities/Old.md" {
		t.Fatalf("filters: %+v", got)
	}
	// A repository with no folder is described but cannot be counted.
	if got = Describe(entry, registry.Repo{Name: "code", Remote: repo.Remote}); got == nil || got.Behind != -1 {
		t.Fatalf("no folder: %+v", got)
	}
}

func TestClaudeMD(t *testing.T) {
	dir := t.TempDir()
	if ClaudeMD(registry.Repo{Path: dir}) != "" || ClaudeMD(registry.Repo{}) != "" {
		t.Fatal("none")
	}
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# x"), 0o644)
	if ClaudeMD(registry.Repo{Path: dir}) != filepath.Join(dir, "CLAUDE.md") {
		t.Fatal("found")
	}
}

func TestSameRemote(t *testing.T) {
	for _, pair := range [][2]string{
		{"git@github.com:me/code.git", "https://github.com/me/code"},
		{"https://github.com/Me/Code.git", "github.com/me/code/"},
		{"ssh://git@github.com/me/code", "git@github.com:me/code"},
	} {
		if !sameRemote(pair[0], pair[1]) {
			t.Errorf("%q should equal %q", pair[0], pair[1])
		}
	}
	if sameRemote("github.com/me/code", "github.com/me/other") {
		t.Error("different paths")
	}
}
