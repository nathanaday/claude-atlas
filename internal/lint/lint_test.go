package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		"wiki/concepts/Beta.md→Al:target-not-found",
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
	if r.Summary.IssuesFound != 3+1+1+3+4+1+1+2 {
		t.Fatalf("issues %d: %s", r.Summary.IssuesFound, r.Markdown())
	}
	md := r.Markdown()
	if !strings.Contains(md, "## Dead links (3)") || !strings.Contains(md, "`wiki/index.md:") {
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

func TestTaskErrors(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "wiki", "tasks", "archive"), 0o755)
	write := func(rel, text string) {
		os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(text), 0o644)
	}
	task := func(status, id, extra string) string {
		return "---\ntype: task\ntitle: T\nstatus: " + status + "\npriority: normal\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags:\n  - task\ntask_id: " + id + "\n---\n\n# T\n\n## Idea\n\nx\n" + extra
	}
	write("wiki/tasks/index.md", "---\ntype: meta\ntitle: Tasks\nstatus: evergreen\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags:\n  - meta\n---\n\n[[ok]] [[stale]] [[noplan]] [[wrong]]\n")
	write("wiki/tasks/ok.md", task("planted", "task-20260801-aaaa", ""))
	write("wiki/tasks/stale.md", task("active", "task-20260801-bbbb", "\n## Plan\n\n1. Go.\n"))
	write("wiki/tasks/noplan.md", task("planned", "task-20260801-cccc", ""))
	write("wiki/tasks/wrong.md", task("done", "task-20260801-dddd", ""))
	report, err := Run(root, Options{AsOf: time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range report.TaskErrors {
		got[f.Path] = f.Message
	}
	if len(got) != 3 || !strings.Contains(got["wiki/tasks/stale.md"], "untouched since 2026-08-01") || !strings.Contains(got["wiki/tasks/noplan.md"], "without a Plan section") || !strings.Contains(got["wiki/tasks/wrong.md"], "moves to wiki/tasks/archive/") {
		t.Fatalf("task errors %+v", got)
	}
	if report.Summary.CategoryCounts["task_errors"] != 3 || len(report.UnindexedPages) != 0 || !strings.Contains(report.Markdown(), "## Tasks (3)") {
		t.Fatalf("summary %+v unindexed %v", report.Summary, report.UnindexedPages)
	}
}
