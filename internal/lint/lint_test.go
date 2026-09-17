package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

const front = "---\ntitle: %s\ntype: concept\nstatus: seed\ncreated: 2026-01-01\nupdated: 2026-01-01\ntags:\n  - x\n---\n"

func mkpage(title, body string) string {
	return strings.Replace(front, "%s", title, 1) + body
}

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, text := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(text), 0o644)
	}
	return root
}

func TestLinksOrphansAndIndex(t *testing.T) {
	root := fixture(t, map[string]string{
		"wiki/index.md":           mkpage("Index", "# Index\n\n- [[Alpha]]\n- [[Gone]]\n- [[Shared]]\n"),
		"wiki/log.md":             mkpage("Log", "## 2026-01-01 — op\n\n- [[Beta]]\n"),
		"wiki/concepts/Alpha.md":  mkpage("Alpha", "# Alpha\n\nSee [[Beta]] and [[Alpha#Missing]] and [[Beta#^blk]]. Code: `[[NotALink]]`.\n\n```\n[[AlsoNot]]\n```\n\n## Empty\n\n## Filled\n\ntext\n"),
		"wiki/concepts/Beta.md":   mkpage("Beta", "# Beta\n\nA line. ^blk\n\n[md](Alpha.md) and [ext](https://x.y/z) and [[Al|alias]]\n"),
		"wiki/concepts/Shared.md": mkpage("Shared", "# Shared\n\ntext\n"),
		"wiki/entities/Shared.md": mkpage("Shared", "# Shared\n\ntext\n"),
		"wiki/concepts/Orphan.md": "# Orphan\n\nno frontmatter\n",
		"wiki/meta/x.md":          mkpage("Meta", "# Meta\n\ntext\n"),
		".raw/hidden.md":          "[[Alpha]]",
	})
	r, err := Run(root, Options{AsOf: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.PagesScanned != 8 {
		t.Fatalf("pages %d", r.Summary.PagesScanned)
	}
	var dead []string
	for _, f := range r.DeadLinks {
		dead = append(dead, f.Source+"→"+f.Target+":"+f.Reason)
	}
	want := []string{
		"wiki/concepts/Alpha.md→Alpha#Missing:heading-not-found",
		"wiki/index.md→Gone:target-not-found",
	}
	if strings.Join(dead, "|") != strings.Join(want, "|") {
		t.Fatalf("dead links %v", dead)
	}
	if len(r.AmbiguousTargets) != 1 || r.AmbiguousTargets[0].Target != "Shared" || len(r.AmbiguousTargets[0].Candidates) != 2 {
		t.Fatalf("ambiguous %+v", r.AmbiguousTargets)
	}
	if len(r.DuplicateBasenames) != 1 || r.DuplicateBasenames[0].Basename != "Shared" {
		t.Fatalf("duplicates %+v", r.DuplicateBasenames)
	}
	if len(r.StaleIndexEntries) != 2 {
		t.Fatalf("stale %+v", r.StaleIndexEntries)
	}
	var orphans []string
	for _, f := range r.Orphans {
		orphans = append(orphans, f.Path)
	}
	// Beta is linked from Alpha (and log, which does not count); Orphan and both Shared pages have nothing but the ambiguous index link.
	if strings.Join(orphans, ",") != "wiki/concepts/Orphan.md,wiki/concepts/Shared.md,wiki/entities/Shared.md" {
		t.Fatalf("orphans %v", orphans)
	}
	var unindexed []string
	for _, f := range r.UnindexedPages {
		unindexed = append(unindexed, f.Path)
	}
	if strings.Join(unindexed, ",") != "wiki/concepts/Beta.md,wiki/concepts/Orphan.md,wiki/concepts/Shared.md,wiki/entities/Shared.md" {
		t.Fatalf("unindexed %v", unindexed)
	}
	if len(r.MissingFrontmatter) != 1 || r.MissingFrontmatter[0].Path != "wiki/concepts/Orphan.md" || r.MissingFrontmatter[0].HasFrontmatter || len(r.MissingFrontmatter[0].MissingFields) != 6 {
		t.Fatalf("frontmatter %+v", r.MissingFrontmatter)
	}
	if len(r.EmptySections) != 1 || r.EmptySections[0].Heading != "Empty" || r.EmptySections[0].Path != "wiki/concepts/Alpha.md" {
		t.Fatalf("empty %+v", r.EmptySections)
	}
	if len(r.WantedPages) != 1 || r.WantedPages[0].Title != "Al" || r.WantedPages[0].Links[0].Source != "wiki/concepts/Beta.md" {
		t.Fatalf("wanted %+v", r.WantedPages)
	}
	if r.Summary.IssuesFound != 2+1+1+3+4+1+1+2 {
		t.Fatalf("issues %d: %s", r.Summary.IssuesFound, r.Markdown())
	}
	md := r.Markdown()
	if !strings.Contains(md, "## Dead links (2)") || !strings.Contains(md, "`wiki/index.md:") {
		t.Fatalf("markdown:\n%s", md)
	}
}

func TestOverlayAndProblems(t *testing.T) {
	root := fixture(t, map[string]string{
		"wiki/index.md": mkpage("Index", "# Index\n\n- [[Alpha]]\n"),
		"wiki/Alpha.md": mkpage("Alpha", "# Alpha\n\ntext\n"),
	})
	overlay := map[string][]byte{
		"wiki/Alpha.md": nil,
		"wiki/New.md":   []byte(mkpage("New", "# New\n\n[[Alpha]] [[Index]]\n\n## Todo\n")),
	}
	r, err := Run(root, Options{Overlay: overlay})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.PagesScanned != 2 {
		t.Fatalf("pages %d", r.Summary.PagesScanned)
	}
	problems := r.Problems([]string{"wiki/New.md"})
	joined := strings.Join(problems, "\n")
	for _, want := range []string{`links to "Alpha"`, `section "Todo" is empty`, "not linked from any index"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	if strings.Contains(joined, "index.md") {
		t.Fatal("problems must be limited to the given paths")
	}
}

func TestWantedPagesAndNearMatches(t *testing.T) {
	root := fixture(t, map[string]string{
		"wiki/index.md":                mkpage("Index", "# Index\n\n- [[Atlas]]\n- [[Gradient Clipping]]\n"),
		"wiki/log.md":                  mkpage("Log", "## 2026-01-01 — op\n\n- [[Deleted Page]]\n"),
		"wiki/folds/Fold.md":           mkpage("Fold", "# Fold\n\n[[Deleted Page]]\n"),
		"wiki/concepts/CNN.md":         mkpage("CNN", "# CNN\n\ntext\n"),
		"wiki/concepts/Transformer.md": mkpage("Transformer", "# T\n\n[[Chapter 1]] [[Atlas]] [[CNN]]\n"),
		"wiki/concepts/Chapter 1.md":   mkpage("Chapter 1", "# Chapter 1\n\n[[vanishing gradient problem|VGP]] [[Transformers]]\n"),
		"wiki/entities/Atlas.md":       mkpage("Atlas", "# Atlas\n\n[[vanishing gradient problem]] and [[Vanishing Gradient Problem#Causes]].\n\n[[Atals]] [[Chapter 2]] [[RNN]] ![[missing.png]] [[notes/Elsewhere]] [[What? Why]]\n"),
	})
	r, err := Run(root, Options{AsOf: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	var dead []string
	for _, f := range r.DeadLinks {
		dead = append(dead, f.Source+"→"+f.Target+":"+f.Suggestion)
	}
	wantDead := []string{
		"wiki/concepts/Chapter 1.md→Transformers:Transformer",
		"wiki/entities/Atlas.md→Atals:Atlas",
		"wiki/entities/Atlas.md→missing.png:",
		"wiki/entities/Atlas.md→notes/Elsewhere:",
		"wiki/entities/Atlas.md→What? Why:",
		"wiki/folds/Fold.md→Deleted Page:",
		"wiki/index.md→Gradient Clipping:",
		"wiki/log.md→Deleted Page:",
	}
	if strings.Join(dead, "|") != strings.Join(wantDead, "|") {
		t.Fatalf("dead links\n got %v\nwant %v", dead, wantDead)
	}
	var wanted []string
	for _, w := range r.WantedPages {
		wanted = append(wanted, w.Title)
	}
	// Short names match only when equal, and names with different digits never match.
	if strings.Join(wanted, "|") != "Chapter 2|RNN|vanishing gradient problem" {
		t.Fatalf("wanted %v", wanted)
	}
	if len(r.WantedPages[2].Links) != 3 {
		t.Fatalf("every link to a wanted page is kept: %+v", r.WantedPages[2])
	}
	if r.Summary.WantedPages != 3 || r.Summary.CategoryCounts["dead_links"] != 8 {
		t.Fatalf("summary %+v", r.Summary)
	}
	if _, counted := r.Summary.CategoryCounts["wanted_pages"]; counted {
		t.Fatal("wanted pages are not findings")
	}
	md := r.Markdown()
	for _, want := range []string{"## Wanted pages (3)", "- vanishing gradient problem ← `wiki/concepts/Chapter 1.md:12`", `did you mean "Atlas"?`} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in markdown:\n%s", want, md)
		}
	}
	if !strings.Contains(string(r.JSON()), `"suggestion": "Atlas"`) {
		t.Fatal("the suggestion is in the JSON report")
	}
	problems := strings.Join(r.Problems([]string{"wiki/entities/Atlas.md"}), "\n")
	for _, want := range []string{`links to "vanishing gradient problem", which has no page yet`, `did you mean "Atlas"?`} {
		if !strings.Contains(problems, want) {
			t.Errorf("missing %q in problems:\n%s", want, problems)
		}
	}
}

func TestStubs(t *testing.T) {
	skeleton := "---\ntitle: Seed\ntype: concept\nstatus: seed\ncreated: 2026-01-01\nupdated: 2026-01-01\ntags:\n  - concept\n---\n\n# Seed\n\n## Definition\n\n<!-- later -->\n\n## Sources\n\n"
	root := fixture(t, map[string]string{
		"wiki/index.md":                mkpage("Index", "# Index\n\n- [[Written]]\n"),
		"wiki/log.md":                  mkpage("Log", "## 2026-01-01 — op\n\n- [[Only Logged]]\n"),
		"wiki/concepts/Written.md":     mkpage("Written", "# Written\n\n[[Seed]] [[Clicked]] [[Filled]]\n"),
		"wiki/concepts/Seed.md":        skeleton,
		"wiki/concepts/Filled.md":      strings.Replace(skeleton, "## Sources\n\n", "## Sources\n\nA paper.\n", 1),
		"wiki/concepts/Lonely Seed.md": strings.ReplaceAll(skeleton, "Seed", "Lonely Seed"),
		"wiki/Clicked.md":              "",
		"wiki/Only Logged.md":          "\n",
	})
	r, err := Run(root, Options{AsOf: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	var stubs []string
	for _, s := range r.Stubs {
		stubs = append(stubs, fmt.Sprintf("%s empty=%v from=%s", s.Path, s.Empty, strings.Join(s.LinkedFrom, ",")))
	}
	wantStubs := []string{
		"wiki/Clicked.md empty=true from=wiki/concepts/Written.md",
		"wiki/concepts/Lonely Seed.md empty=false from=",
		"wiki/concepts/Seed.md empty=false from=wiki/concepts/Written.md",
	}
	if strings.Join(stubs, "|") != strings.Join(wantStubs, "|") {
		t.Fatalf("stubs\n got %v\nwant %v", stubs, wantStubs)
	}
	paths := func(findings []PathFinding) string {
		var out []string
		for _, f := range findings {
			out = append(out, f.Path)
		}
		return strings.Join(out, ",")
	}
	// A stub is not unindexed and has no empty sections; a filled page is and has.
	if got := paths(r.UnindexedPages); got != "wiki/concepts/Filled.md,wiki/Only Logged.md" {
		t.Fatalf("unindexed %s", got)
	}
	if len(r.EmptySections) != 1 || r.EmptySections[0].Path != "wiki/concepts/Filled.md" || r.EmptySections[0].Heading != "Definition" {
		t.Fatalf("empty sections %+v", r.EmptySections)
	}
	// An empty file a page links to needs no frontmatter yet; one only the log links to does.
	if len(r.MissingFrontmatter) != 1 || r.MissingFrontmatter[0].Path != "wiki/Only Logged.md" {
		t.Fatalf("missing frontmatter %+v", r.MissingFrontmatter)
	}
	// A stub nothing links to is still an orphan.
	if got := paths(r.Orphans); got != "wiki/concepts/Lonely Seed.md,wiki/Only Logged.md" {
		t.Fatalf("orphans %s", got)
	}
	if r.Summary.Stubs != 3 || len(r.WantedPages) != 0 {
		t.Fatalf("summary %+v wanted %+v", r.Summary, r.WantedPages)
	}
	if _, counted := r.Summary.CategoryCounts["stubs"]; counted {
		t.Fatal("stubs are not findings")
	}
	md := r.Markdown()
	for _, want := range []string{"## Stubs to fill (3)", "- `wiki/Clicked.md` (empty file) ← wiki/concepts/Written.md"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in markdown:\n%s", want, md)
		}
	}
}

// A link that names a file, not a page, stays a dead link: a page named "note.md" would
// be the file "note.md.md", which does not resolve the link.
func TestALinkWithAFileExtensionIsNotAWantedPage(t *testing.T) {
	root := fixture(t, map[string]string{
		"wiki/index.md":          mkpage("Index", "# Index\n\n- [[Alpha]]\n"),
		"wiki/concepts/Alpha.md": mkpage("Alpha", "# Alpha\n\n[[note.md]]\n\n[[board.canvas]]\n\n[[table.base]]\n\n[[plain]]\n"),
	})
	r, err := Run(root, Options{AsOf: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	var wanted []string
	for _, w := range r.WantedPages {
		wanted = append(wanted, w.Title)
	}
	if strings.Join(wanted, ",") != "plain" {
		t.Fatalf("wanted %v", wanted)
	}
	var dead []string
	for _, f := range r.DeadLinks {
		dead = append(dead, f.Target)
	}
	if strings.Join(dead, ",") != "note.md,board.canvas,table.base" {
		t.Fatalf("dead links %v", dead)
	}
}

// A seed page with nothing under its headings is a stub, and a stub the user wrote by hand
// still owes its frontmatter; only an empty file is excused.
func TestASeedStubKeepsItsMissingFrontmatterFinding(t *testing.T) {
	root := fixture(t, map[string]string{
		"wiki/index.md":          mkpage("Index", "# Index\n\n- [[Half]]\n- [[Empty]]\n"),
		"wiki/concepts/Half.md":  "---\ntitle: Half\ntype: concept\nstatus: seed\n---\n\n# Half\n\n## Definition\n",
		"wiki/concepts/Empty.md": "",
	})
	r, err := Run(root, Options{AsOf: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	var stubs []string
	for _, s := range r.Stubs {
		stubs = append(stubs, s.Path)
	}
	if strings.Join(stubs, ",") != "wiki/concepts/Empty.md,wiki/concepts/Half.md" {
		t.Fatalf("stubs %v", stubs)
	}
	if len(r.MissingFrontmatter) != 1 || r.MissingFrontmatter[0].Path != "wiki/concepts/Half.md" ||
		!r.MissingFrontmatter[0].HasFrontmatter ||
		strings.Join(r.MissingFrontmatter[0].MissingFields, ",") != "created,updated,tags" {
		t.Fatalf("missing frontmatter %+v", r.MissingFrontmatter)
	}
}

func TestLedgerErrors(t *testing.T) {
	root := fixture(t, map[string]string{
		"wiki/index.md": mkpage("Index", "# I\n"),
		"wiki/meta/ledgers/source-ledger.json": `{"schema":"claude-atlas.source-ledger.v1","generated_at":"2026-01-01T00:00:00Z","sources":{
			"src-a":{"title":"A","origin":{"kind":"file","locator":".raw/captured/a.pdf"},"authority":"unknown","review_status":"active","pages":["wiki/sources/A.md"]}}}`,
	})
	r, _ := Run(root, Options{})
	if len(r.LedgerErrors) != 2 {
		t.Fatalf("ledger errors %+v", r.LedgerErrors)
	}
	os.WriteFile(filepath.Join(root, "wiki", "meta", "ledgers", "source-ledger.json"), []byte("{"), 0o644)
	r, _ = Run(root, Options{})
	if len(r.LedgerErrors) != 1 {
		t.Fatalf("broken ledger %+v", r.LedgerErrors)
	}
}

// A finding in a new vault is the layout's own fault, and the user can do nothing about it.
func TestNewVaultHasNoFindings(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	asOf := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	for _, mode := range vault.Modes {
		root := filepath.Join(t.TempDir(), string(mode))
		if _, err := vault.Init(root, vault.Options{Mode: mode}, asOf); err != nil {
			t.Fatal(err)
		}
		r, err := Run(root, Options{AsOf: asOf})
		if err != nil {
			t.Fatal(err)
		}
		if r.Summary.IssuesFound != 0 || r.Summary.WantedPages != 0 || r.Summary.Stubs != 0 {
			t.Errorf("knowledge base in %s mode:\n%s", mode, r.Markdown())
		}
	}
}

func TestKindErrors(t *testing.T) {
	asOf := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	kb := fixture(t, map[string]string{
		".claude-atlas.json":  `{"schema":"claude-atlas.vault.v3","id":"1","kind":"knowledge","name":"kb","mode":"generic","created":"2026-09-14"}`,
		"wiki/index.md":       mkpage("Index", "# Index\n"),
		"inbox/paper.md":      "x",
		"ideas/note.md":       "x",
		"wiki/tasks/tasks.md": mkpage("Tasks", "# Tasks\n"),
		"wiki/questions/Q.md": mkpage("Q", "# Q\n"),
		"kb/x/index.md":       "x",
		"repos/x/README.md":   "x",
	})
	r, err := Run(kb, Options{AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range r.KindErrors {
		got = append(got, f.Path)
	}
	if strings.Join(got, ",") != "kb,repos,wiki/questions,wiki/tasks" {
		t.Fatalf("kind errors %v", got)
	}
	if r.Summary.CategoryCounts["kind_errors"] != 4 || r.Version != 3 || !strings.Contains(r.Markdown(), "## Kind (4)") {
		t.Fatalf("summary %+v\n%s", r.Summary, r.Markdown())
	}
	if _, ok := r.Summary.CategoryCounts["mount_errors"]; ok {
		t.Fatal("no mount category")
	}
	if _, ok := r.Summary.CategoryCounts["task_errors"]; ok {
		t.Fatal("no task category")
	}
	project := fixture(t, map[string]string{
		".claude-atlas.json": `{"schema":"claude-atlas.vault.v2","id":"2","kind":"project","name":"p","mode":"generic","created":"2026-09-14"}`,
		"wiki/index.md":      mkpage("Index", "# Index\n"),
	})
	r, _ = Run(project, Options{AsOf: asOf})
	if len(r.KindErrors) != 1 || r.KindErrors[0].Path != ".claude-atlas.json" || !strings.Contains(r.KindErrors[0].Message, "claude-atlas init") {
		t.Fatalf("v2 project kind errors %+v", r.KindErrors)
	}
	plain := fixture(t, map[string]string{
		"wiki/index.md":   mkpage("Index", "# Index\n"),
		"wiki/tasks/x.md": mkpage("x", "# x\n"),
	})
	r, _ = Run(plain, Options{AsOf: asOf})
	if len(r.KindErrors) != 0 {
		t.Fatalf("no identity file, no kind checks: %+v", r.KindErrors)
	}
}

func TestFolderIndexPagesCatalogAndAreNotOrphans(t *testing.T) {
	root := fixture(t, map[string]string{
		"wiki/index.md":             mkpage("Index", "# Index\n\n- [[Alpha]]\n"),
		"wiki/concepts/Alpha.md":    mkpage("Alpha", "# Alpha\n\ntext\n"),
		"wiki/concepts/Beta.md":     mkpage("Beta", "# Beta\n\n[[Alpha]]\n"),
		"wiki/canvases/canvases.md": mkpage("Canvases", "# Canvases\n\n- [[Beta]]\n"),
	})
	r, err := Run(root, Options{AsOf: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Orphans) != 0 || len(r.UnindexedPages) != 0 {
		t.Fatalf("orphans %v unindexed %v", r.Orphans, r.UnindexedPages)
	}
}

func TestExcludeAndFrontmatterYAMLError(t *testing.T) {
	root := fixture(t, map[string]string{
		"wiki/index.md":           mkpage("Index", "# I\n\n[[Scratch]]\n"),
		"wiki/scratch/Scratch.md": "---\ntitle: [unclosed\n---\n# S\n",
	})
	r, _ := Run(root, Options{})
	if len(r.ReadErrors) != 1 || len(r.DeadLinks) != 0 {
		t.Fatalf("yaml error should be a read error, not missing frontmatter: %+v %+v", r.ReadErrors, r.MissingFrontmatter)
	}
	r, _ = Run(root, Options{Exclude: []string{"wiki/scratch/*"}})
	if r.Summary.PagesScanned != 1 || len(r.DeadLinks) != 1 {
		t.Fatalf("exclude: %+v", r.Summary)
	}
}

// Pages under wiki/tasks/ are ordinary pages: nothing checks them as tasks, and the
// index catalogs them like any other.
func TestTaskPagesAreOrdinaryPages(t *testing.T) {
	root := fixture(t, map[string]string{
		"wiki/index.md":          mkpage("Index", "# Index\n\n- [[Old task]]\n"),
		"wiki/tasks/Old task.md": "---\ntype: task\ntitle: Old task\nstatus: done\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags:\n  - task\ntask_id: task-20260801-aaaa\n---\n\n# Old task\n\nx\n",
	})
	r, err := Run(root, Options{AsOf: time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Orphans) != 0 || len(r.UnindexedPages) != 0 || len(r.DeadLinks) != 0 {
		t.Fatalf("orphans %v unindexed %v dead %v", r.Orphans, r.UnindexedPages, r.DeadLinks)
	}
}
