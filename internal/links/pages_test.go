package links

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWikilink(t *testing.T) {
	for in, want := range map[string][2]string{
		"[[repos/code|code]]": {"repos/code", "code"},
		"[[ code ]]":          {"code", ""},
		"[[a/b#heading|B]]":   {"a/b", "B"},
		"not a link":          {"", ""},
		"[[]]":                {"", ""},
	} {
		target, alias, ok := ParseWikilink(in)
		if ok != (want[0] != "") || target != want[0] || alias != want[1] {
			t.Errorf("%q: got %q %q %v", in, target, alias, ok)
		}
	}
}

func TestPageName(t *testing.T) {
	taken := func(name string) bool { return name == "code" || name == "code (work)" }
	if got := PageName("/home/me/other/code", taken); got != "code (other)" {
		t.Fatalf("got %q", got)
	}
	if got := PageName("/home/me/work/code", taken); got != "code (work 2)" {
		t.Fatalf("got %q", got)
	}
	if got := PageName("/x/we:ird[name]", func(string) bool { return false }); got != "we-ird-name" {
		t.Fatalf("got %q", got)
	}
	if got := PageName("/x/fresh", func(string) bool { return false }); got != "fresh" {
		t.Fatalf("got %q", got)
	}
}

func TestCreateWalkAndResolve(t *testing.T) {
	atlas := t.TempDir()
	repo := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repo, 0o755)
	page, err := Create(atlas, Repo, repo, nil)
	if err != nil || page.Name != "code" || page.Rel() != "repos/code" || page.Wikilink() != "[[repos/code|code]]" {
		t.Fatalf("%+v %v", page, err)
	}
	text, _ := os.ReadFile(page.File)
	if !strings.HasPrefix(string(text), "---\nschema: atlas.link.v1\npath: "+repo+"\n---\n") || !strings.Contains(string(text), "# code") {
		t.Fatalf("page text:\n%s", text)
	}
	again, err := Create(atlas, Materials, repo, []Page{page})
	if err != nil || again.File != page.File || again.Kind != Repo {
		t.Fatalf("a folder has one page whatever kind is asked: %+v %v", again, err)
	}
	other := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(other, 0o755)
	second, err := Create(atlas, Repo, other, []Page{page})
	if err != nil || second.Name == page.Name || !strings.HasPrefix(second.Name, "code (") {
		t.Fatalf("second page %+v %v", second, err)
	}
	os.WriteFile(filepath.Join(atlas, "repos", "broken.md"), []byte("no frontmatter\n"), 0o644)
	os.WriteFile(filepath.Join(atlas, "repos", ".hidden.md"), []byte("---\nschema: atlas.link.v1\npath: /x\n---\n"), 0o644)
	os.MkdirAll(filepath.Join(atlas, "materials"), 0o755)
	os.WriteFile(filepath.Join(atlas, "materials", "Slides.md"), []byte("---\nschema: atlas.link.v1\npath: ~/Slides\n---\n"), 0o644)
	pages, problems, err := Walk(atlas)
	if err != nil || len(pages) != 3 || len(problems) != 1 || problems[0].File != "repos/broken.md" {
		t.Fatalf("walk: %+v %+v %v", pages, problems, err)
	}
	if pages[0].Name != "code" || pages[2].Kind != Materials || pages[2].Name != "Slides" || strings.HasPrefix(pages[2].Path, "~") {
		t.Fatalf("order and expansion: %+v", pages)
	}
	if p, isLink := Resolve(pages, Repo, "[[repos/code|code]]"); p == nil || !isLink || p.Path != repo {
		t.Fatalf("resolve by path link: %+v", p)
	}
	if p, _ := Resolve(pages, Materials, "[[Slides]]"); p == nil || p.Kind != Materials {
		t.Fatalf("resolve bare name within the kind: %+v", p)
	}
	if p, _ := Resolve(pages, Repo, "[[materials/Slides|Slides]]"); p == nil || p.Kind != Materials {
		t.Fatal("a link with a folder resolves whatever list it sits in")
	}
	if p, isLink := Resolve(pages, Repo, "[[nothing]]"); p != nil || !isLink {
		t.Fatal("a link to nothing is still a link")
	}
	if p, isLink := Resolve(pages, Repo, repo); p == nil || isLink || p.Name != "code" {
		t.Fatalf("a plain path finds its page: %+v %v", p, isLink)
	}
	if p, _ := Resolve(pages, Repo, "/nowhere"); p != nil {
		t.Fatal("a plain path with no page resolves to nothing")
	}
}

func TestInspectAcceptsAFileAsMaterial(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paper.pdf")
	os.WriteFile(file, []byte("pdf"), 0o644)
	link := Inspect(Materials, file)
	if !link.OK || link.Files == nil || *link.Files != 1 || *link.Bytes != 3 || link.Newest == "" {
		t.Fatalf("%+v", link)
	}
	if repo := Inspect(Repo, file); repo.OK || repo.Error != "not a directory" {
		t.Fatalf("a file is never a repo: %+v", repo)
	}
}
