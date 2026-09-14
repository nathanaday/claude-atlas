package vaults

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/tree"
)

func fakeVault(t *testing.T, dir string) string {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, ".claude-obsidian.json"), []byte("{}"), 0o644)
	return dir
}

func setup(t *testing.T) (*home.Config, *tree.Project) {
	root := t.TempDir()
	cfg := &home.Config{Schema: home.ConfigSchema, VaultsDir: filepath.Join(root, "Vaults"), AtlasVault: filepath.Join(root, "Atlas")}
	os.MkdirAll(cfg.TreeRoot(), 0o755)
	vault := fakeVault(t, filepath.Join(cfg.VaultsDir, "a"))
	p, err := Register(cfg, vault, RegisterOptions{Name: "A", Purpose: "why", Category: "work"})
	if err != nil {
		t.Fatal(err)
	}
	return cfg, p
}

func TestUpdateFieldsAndCategory(t *testing.T) {
	cfg, p := setup(t)
	top := ""
	if err := Update(cfg, p, Edit{Name: "A2", ClearPurpose: true, Priority: "high", State: "paused", Category: &top}); err != nil {
		t.Fatal(err)
	}
	projects, problems, _ := tree.Walk(cfg.TreeRoot())
	if len(problems) != 0 || len(projects) != 1 {
		t.Fatalf("walk: %v %v", projects, problems)
	}
	got := projects[0]
	if got.Rel != "a" || got.Name != "A2" || got.Purpose != "" || got.Priority != "high" || got.State != "paused" {
		t.Fatalf("got %+v", got.Frontmatter)
	}
}

func TestUpdateRepointsToAnExistingVault(t *testing.T) {
	cfg, p := setup(t)
	other := fakeVault(t, filepath.Join(cfg.VaultsDir, "elsewhere"))
	if err := Update(cfg, p, Edit{Vault: other}); err != nil {
		t.Fatal(err)
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	if projects[0].VaultPath() != other {
		t.Fatalf("vault %s", projects[0].VaultPath())
	}
	if err := Update(cfg, projects[0], Edit{Vault: t.TempDir()}); err == nil {
		t.Fatal("repointing at a non-vault should fail")
	}
}

func TestUpdateMovesTheVaultDirectoryWhenAsked(t *testing.T) {
	cfg, p := setup(t)
	target := filepath.Join(cfg.VaultsDir, "moved", "a")
	if err := Update(cfg, p, Edit{Vault: target}); err == nil {
		t.Fatal("a missing target without MoveVault should fail")
	}
	if err := Update(cfg, p, Edit{Vault: target, MoveVault: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, ".claude-obsidian.json")); err != nil {
		t.Fatal("vault not moved")
	}
	if _, err := os.Stat(p.VaultPath()); err == nil {
		t.Fatal("old vault still present")
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	if projects[0].VaultPath() != target {
		t.Fatalf("page not repointed: %s", projects[0].VaultPath())
	}
}

func TestUnlinkLeavesTheVault(t *testing.T) {
	cfg, p := setup(t)
	if err := Unlink(p); err != nil {
		t.Fatal(err)
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	if len(projects) != 0 {
		t.Fatal("project still registered")
	}
	if _, err := os.Stat(filepath.Join(p.VaultPath(), ".claude-obsidian.json")); err != nil {
		t.Fatal("vault was touched")
	}
}

func reload(t *testing.T, cfg *home.Config, p *tree.Project) *tree.Project {
	t.Helper()
	projects, _, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		t.Fatal(err)
	}
	q := tree.FindByRel(projects, p.Rel)
	if q == nil {
		t.Fatalf("%s vanished", p.Rel)
	}
	return q
}

func TestAddAndRemoveLinksThroughPages(t *testing.T) {
	cfg, p := setup(t)
	repo := filepath.Join(cfg.VaultsDir, "code")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	docs := filepath.Join(cfg.VaultsDir, "docs")
	os.MkdirAll(docs, 0o755)
	page, err := AddLink(cfg, p, "", repo)
	if err != nil {
		t.Fatal(err)
	}
	if page.Kind != links.Repo || page.Name != "code" || page.File != filepath.Join(cfg.AtlasVault, "repos", "code.md") {
		t.Fatalf("page %+v", page)
	}
	text, _ := os.ReadFile(page.File)
	if !strings.Contains(string(text), "schema: atlas.link.v1") || !strings.Contains(string(text), "path: "+repo) {
		t.Fatalf("page text:\n%s", text)
	}
	p = reload(t, cfg, p)
	if len(p.Repos) != 1 || p.Repos[0] != "[[repos/code|code]]" {
		t.Fatalf("repos entry %v", p.Repos)
	}
	if _, err := AddLink(cfg, p, "", docs); err != nil {
		t.Fatal(err)
	}
	p = reload(t, cfg, p)
	if len(p.Linked) != 2 || p.Linked[0].Name != "code" || p.Linked[0].Path != repo || p.Linked[1].Kind != links.Materials || p.Linked[1].Path != docs {
		t.Fatalf("linked %+v", p.Linked)
	}
	if !p.LinkedTo(repo) || len(p.Paths(links.Materials)) != 1 {
		t.Fatal("helpers should see both links")
	}
	if _, err := AddLink(cfg, p, links.Materials, repo); err == nil {
		t.Fatal("duplicate link should fail")
	}
	if _, err := AddLink(cfg, p, "", filepath.Join(cfg.VaultsDir, "nope")); err == nil {
		t.Fatal("missing path should fail")
	}
	// Another project links the same repo by page name and shares the node.
	other := fakeVault(t, filepath.Join(cfg.VaultsDir, "b"))
	q, err := Register(cfg, other, RegisterOptions{Name: "B"})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := AddLink(cfg, q, "", "code")
	if err != nil || shared.File != page.File {
		t.Fatalf("link by name: %v %+v", err, shared)
	}
	pages, _, _ := links.Walk(cfg.AtlasVault)
	if len(pages) != 2 {
		t.Fatalf("pages %+v", pages)
	}
	if err := RemoveLink(cfg, p, "code"); err != nil {
		t.Fatal(err)
	}
	p = reload(t, cfg, p)
	if len(p.Repos) != 0 || len(p.Materials) != 1 {
		t.Fatalf("after remove: repos=%v materials=%v", p.Repos, p.Materials)
	}
	if _, err := os.Stat(page.File); err != nil {
		t.Fatal("removing a link must not delete the page")
	}
	if err := RemoveLink(cfg, p, repo); err == nil {
		t.Fatal("removing an unlinked path should fail")
	}
	if err := RemoveLink(cfg, p, docs); err != nil {
		t.Fatal(err)
	}
}

func TestLinkPageDecidesTheKind(t *testing.T) {
	cfg, p := setup(t)
	slides := filepath.Join(cfg.VaultsDir, "slides")
	os.MkdirAll(slides, 0o755)
	if _, err := AddLink(cfg, p, links.Materials, slides); err != nil {
		t.Fatal(err)
	}
	other := fakeVault(t, filepath.Join(cfg.VaultsDir, "b"))
	q, _ := Register(cfg, other, RegisterOptions{Name: "B"})
	page, err := AddLink(cfg, q, links.Repo, slides)
	if err != nil || page.Kind != links.Materials {
		t.Fatalf("an existing page keeps its kind: %v %+v", err, page)
	}
	q = reload(t, cfg, q)
	if len(q.Materials) != 1 || len(q.Repos) != 0 {
		t.Fatalf("q lists %v %v", q.Repos, q.Materials)
	}
}

func TestUpdateLinkRenamesMovesAndRepoints(t *testing.T) {
	cfg, p := setup(t)
	docs := filepath.Join(cfg.VaultsDir, "docs")
	os.MkdirAll(filepath.Join(docs, ".git"), 0o755)
	page, err := AddLink(cfg, p, links.Materials, docs)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := Register(cfg, fakeVault(t, filepath.Join(cfg.VaultsDir, "b")), RegisterOptions{Name: "B"})
	if _, err := AddLink(cfg, q, "", "docs"); err != nil {
		t.Fatal(err)
	}
	renamed, err := UpdateLink(cfg, page, LinkEdit{Name: "Course notes"})
	if err != nil || renamed.Name != "Course notes" || renamed.Rel() != "materials/Course notes" {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	if _, err := os.Stat(page.File); err == nil {
		t.Fatal("old page should be gone")
	}
	for _, proj := range []*tree.Project{p, q} {
		proj = reload(t, cfg, proj)
		if len(proj.Materials) != 1 || proj.Materials[0] != "[[materials/Course notes|Course notes]]" || proj.Linked[0].Path != docs {
			t.Fatalf("%s after rename: %v %+v", proj.Name, proj.Materials, proj.Linked)
		}
	}
	moved, err := UpdateLink(cfg, renamed, LinkEdit{Kind: links.Repo})
	if err != nil || moved.Kind != links.Repo || moved.File != filepath.Join(cfg.AtlasVault, "repos", "Course notes.md") {
		t.Fatalf("move: %+v %v", moved, err)
	}
	p = reload(t, cfg, p)
	if len(p.Repos) != 1 || len(p.Materials) != 0 || p.Repos[0] != "[[repos/Course notes|Course notes]]" {
		t.Fatalf("after move: repos=%v materials=%v", p.Repos, p.Materials)
	}
	other := filepath.Join(cfg.VaultsDir, "other")
	os.MkdirAll(other, 0o755)
	repointed, err := UpdateLink(cfg, moved, LinkEdit{Path: other})
	if err != nil || repointed.Path != other {
		t.Fatalf("repoint: %+v %v", repointed, err)
	}
	p = reload(t, cfg, p)
	if p.Linked[0].Path != other {
		t.Fatalf("project should see the new folder: %+v", p.Linked)
	}
	text, _ := os.ReadFile(repointed.File)
	if !strings.Contains(string(text), "path: "+other) {
		t.Fatalf("page text:\n%s", text)
	}
	if _, err := UpdateLink(cfg, repointed, LinkEdit{Path: filepath.Join(cfg.VaultsDir, "nope")}); err == nil {
		t.Fatal("missing folder should fail")
	}
	if _, err := UpdateLink(cfg, repointed, LinkEdit{Name: "///"}); err == nil {
		t.Fatal("unusable name should fail")
	}
	file := filepath.Join(cfg.VaultsDir, "paper.pdf")
	os.WriteFile(file, []byte("x"), 0o644)
	if _, err := UpdateLink(cfg, repointed, LinkEdit{Path: file}); err == nil {
		t.Fatal("a repo cannot be a file")
	}
	if _, err := UpdateLink(cfg, repointed, LinkEdit{Kind: links.Materials, Path: file}); err != nil {
		t.Fatal("material may be a file:", err)
	}
}

func TestUpgradeLinksGivesPlainPathsPages(t *testing.T) {
	cfg, p := setup(t)
	repo := filepath.Join(cfg.VaultsDir, "code")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	docs := filepath.Join(cfg.VaultsDir, "docs")
	os.MkdirAll(docs, 0o755)
	if err := tree.UpdateFrontmatter(p.Path, map[string]any{"repos": []string{repo}, "materials": []string{docs}}); err != nil {
		t.Fatal(err)
	}
	p = reload(t, cfg, p)
	if p.Linked[0].Name != "" || p.Linked[0].Path != repo {
		t.Fatalf("a plain path resolves to itself: %+v", p.Linked)
	}
	upgraded, err := UpgradeLinks(cfg)
	if err != nil || len(upgraded) != 1 || upgraded[0] != p.Rel {
		t.Fatalf("upgraded %v %v", upgraded, err)
	}
	p = reload(t, cfg, p)
	if p.Repos[0] != "[[repos/code|code]]" || p.Materials[0] != "[[materials/docs|docs]]" || p.Linked[1].Path != docs {
		t.Fatalf("after upgrade repos=%v materials=%v linked=%+v", p.Repos, p.Materials, p.Linked)
	}
	if again, _ := UpgradeLinks(cfg); len(again) != 0 {
		t.Fatalf("second upgrade should change nothing: %v", again)
	}
}

func TestRelateAndUnrelate(t *testing.T) {
	cfg, a := setup(t)
	b, _ := Register(cfg, fakeVault(t, filepath.Join(cfg.VaultsDir, "b")), RegisterOptions{Name: "B", Category: "work"})
	if err := Relate(cfg, a, a); err == nil {
		t.Fatal("self relation should fail")
	}
	if err := Relate(cfg, a, b); err != nil {
		t.Fatal(err)
	}
	a, b = reload(t, cfg, a), reload(t, cfg, b)
	if len(a.Related) != 1 || a.Related[0] != "[[tree/work/b|B]]" || len(a.RelatedTo) != 1 || a.RelatedTo[0] != "work/b" {
		t.Fatalf("a: related=%v to=%v", a.Related, a.RelatedTo)
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	if from := tree.RelatedFrom(projects, tree.FindByRel(projects, b.Rel)); len(from) != 1 || from[0].Rel != a.Rel {
		t.Fatalf("b should see a as related from: %v", from)
	}
	if err := Relate(cfg, b, a); err == nil {
		t.Fatal("relating from the other side should say they are related already")
	}
	if err := Unrelate(cfg, b, a); err != nil {
		t.Fatal("unrelate works from either side:", err)
	}
	a = reload(t, cfg, a)
	if len(a.RelatedTo) != 0 {
		t.Fatalf("still related: %v", a.Related)
	}
	if err := Unrelate(cfg, a, b); err == nil {
		t.Fatal("unrelating unrelated projects should fail")
	}
	// Edit replaces the whole list.
	if err := Update(cfg, a, Edit{Related: &[]string{b.Rel}}); err != nil {
		t.Fatal(err)
	}
	a = reload(t, cfg, a)
	if len(a.RelatedTo) != 1 {
		t.Fatalf("edit related: %v", a.Related)
	}
	if err := Update(cfg, a, Edit{Related: &[]string{"nope"}}); err == nil {
		t.Fatal("unknown project should fail")
	}
}

func TestUpdateIntentFields(t *testing.T) {
	cfg, p := setup(t)
	blocked, review, done := "hardware", "2026-10-01", "Ships."
	if err := Update(cfg, p, Edit{BlockedOn: &blocked, ReviewAfter: &review, DefinitionOfDone: &done}); err != nil {
		t.Fatal(err)
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	got := projects[0]
	if got.BlockedOn != "hardware" || got.ReviewAfter != "2026-10-01" || got.DefinitionOfDone != "Ships." {
		t.Fatalf("got %+v", got.Frontmatter)
	}
	bad := "next week"
	if err := Update(cfg, got, Edit{ReviewAfter: &bad}); err == nil || !ValidReviewDate(bad) == false && err == nil {
		t.Fatal("a non-date review_after must be refused")
	}
	if err := Update(cfg, got, Edit{Priority: "urgent"}); err == nil {
		t.Fatal("an unknown priority must be refused")
	}
	empty := ""
	if err := Update(cfg, got, Edit{BlockedOn: &empty, ReviewAfter: &empty}); err != nil {
		t.Fatal(err)
	}
	projects, _, _ = tree.Walk(cfg.TreeRoot())
	if projects[0].BlockedOn != "" || projects[0].ReviewAfter != "" || projects[0].DefinitionOfDone != "Ships." {
		t.Fatalf("clearing: %+v", projects[0].Frontmatter)
	}
}
