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
	for _, kind := range vault.Kinds {
		for _, mode := range vault.Modes {
			root := filepath.Join(t.TempDir(), string(kind)+"-"+string(mode))
			if _, err := vault.Init(root, vault.Options{Kind: kind, Mode: mode}, asOf); err != nil {
				t.Fatal(err)
			}
			r, err := Run(root, Options{AsOf: asOf})
			if err != nil {
				t.Fatal(err)
			}
			if r.Summary.IssuesFound != 0 || r.Summary.WantedPages != 0 || r.Summary.Stubs != 0 {
				t.Errorf("%s vault in %s mode:\n%s", kind, mode, r.Markdown())
			}
		}
	}
}

func TestKindErrors(t *testing.T) {
	asOf := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	kb := fixture(t, map[string]string{
		".claude-atlas.json":                 `{"schema":"claude-atlas.vault.v2","id":"1","kind":"knowledge","name":"kb","mode":"generic","created":"2026-09-14","mounts":[{"id":"2","name":"p","access":"write"}]}`,
		"wiki/index.md":                      mkpage("Index", "# Index\n"),
		"inbox/paper.md":                     "x",
		"wiki/tasks/tasks.md":                mkpage("Tasks", "# Tasks\n"),
		"wiki/questions/Q.md":                mkpage("Q", "# Q\n"),
		"wiki/meta/ledgers/task-ledger.json": `{"schema":"claude-atlas.task-ledger.v1","tasks":[]}`,
	})
	r, err := Run(kb, Options{AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range r.KindErrors {
		got = append(got, f.Path)
	}
	if strings.Join(got, ",") != ".claude-atlas.json,inbox,wiki/meta/ledgers/task-ledger.json,wiki/questions,wiki/tasks" {
		t.Fatalf("kind errors %v", got)
	}
	if r.Summary.CategoryCounts["kind_errors"] != 5 || r.Version != 3 || !strings.Contains(r.Markdown(), "## Kind (5)") {
		t.Fatalf("summary %+v\n%s", r.Summary, r.Markdown())
	}
	project := fixture(t, map[string]string{
		".claude-atlas.json": `{"schema":"claude-atlas.vault.v2","id":"2","kind":"project","name":"p","mode":"generic","created":"2026-09-14","scope":"x"}`,
		"wiki/index.md":      mkpage("Index", "# Index\n"),
	})
	r, _ = Run(project, Options{AsOf: asOf})
	if len(r.KindErrors) != 1 || r.KindErrors[0].Path != ".claude-atlas.json" || !strings.Contains(r.KindErrors[0].Message, "scope") {
		t.Fatalf("project kind errors %+v", r.KindErrors)
	}
	plain := fixture(t, map[string]string{
		"wiki/index.md": mkpage("Index", "# Index\n"),
		"inbox/x.md":    "x",
	})
	r, _ = Run(plain, Options{AsOf: asOf})
	if len(r.KindErrors) != 0 {
		t.Fatalf("no identity file, no kind checks: %+v", r.KindErrors)
	}
}

// A project reads a mounted knowledge base through kb/<name>, the way Obsidian follows
// the symlink, so a link to one of its pages resolves.
func TestLinksResolveThroughMounts(t *testing.T) {
	asOf := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	aiml := fixture(t, map[string]string{
		"wiki/index.md":                    mkpage("Index", "# Index\n\n- [[Backpropagation]]\n"),
		"wiki/hot.md":                      mkpage("Hot", "# Hot\n"),
		"wiki/concepts/Backpropagation.md": "---\ntitle: Backpropagation\ntype: concept\nstatus: developing\ncreated: 2026-01-01\nupdated: 2026-01-01\naliases:\n  - backprop\ntags:\n  - x\n---\n\n# Backpropagation\n\n## Causes\n\ntext\n",
		"wiki/concepts/Shared.md":          mkpage("Shared", "# Shared\n\nthe knowledge base's page\n"),
		"wiki/concepts/Twice.md":           mkpage("Twice", "# Twice\n\ntext\n"),
	})
	other := fixture(t, map[string]string{
		"wiki/concepts/Twice.md": mkpage("Twice", "# Twice\n\ntext\n"),
	})
	root := fixture(t, map[string]string{
		"wiki/index.md":           mkpage("Index", "# Index\n\n- [[Own]]\n- [[Shared]]\n"),
		"wiki/concepts/Own.md":    mkpage("Own", "# Own\n\ntext\n"),
		"wiki/concepts/Shared.md": mkpage("Shared", "# Shared\n\nthe project's page\n"),
		"wiki/concepts/Notes.md": mkpage("Notes", "# Notes\n\n[[Backpropagation]] [[backprop]] [[Backpropogation]] [[Shared]] [[Twice]]\n"+
			"[[Backpropagation#Causes]] [[Nowhere]] [[kb/ai-ml/concepts/Shared]]\n"),
	})

	check := func(what string, r *Report) {
		t.Helper()
		var dead []string
		for _, f := range r.DeadLinks {
			dead = append(dead, f.Source+"→"+f.Target+":"+f.Reason+":"+f.Suggestion)
		}
		if strings.Join(dead, "|") != "wiki/concepts/Notes.md→Backpropogation:target-not-found:Backpropagation" {
			t.Fatalf("%s: dead links %v", what, dead)
		}
		if len(r.AmbiguousTargets) != 1 || r.AmbiguousTargets[0].Target != "Twice" ||
			strings.Join(r.AmbiguousTargets[0].Candidates, ",") != "kb/ai-ml/concepts/Twice.md,kb/other/concepts/Twice.md" {
			t.Fatalf("%s: ambiguous %+v", what, r.AmbiguousTargets)
		}
		var dups []string
		for _, d := range r.DuplicateBasenames {
			dups = append(dups, d.Basename+":"+strings.Join(d.Paths, ","))
		}
		wantDups := []string{
			"Shared:kb/ai-ml/concepts/Shared.md,wiki/concepts/Shared.md",
			"Twice:kb/ai-ml/concepts/Twice.md,kb/other/concepts/Twice.md",
		}
		if strings.Join(dups, "|") != strings.Join(wantDups, "|") {
			t.Fatalf("%s: duplicates\n got %v\nwant %v", what, dups, wantDups)
		}
		var wanted []string
		for _, w := range r.WantedPages {
			wanted = append(wanted, w.Title)
		}
		if strings.Join(wanted, ",") != "Nowhere" {
			t.Fatalf("%s: wanted %v", what, wanted)
		}
		if r.Summary.PagesScanned != 4 || r.Summary.LinksScanned != 10 {
			t.Fatalf("%s: a mount's pages and links are not the project's: %+v", what, r.Summary)
		}
		if len(r.MountErrors) != 0 {
			t.Fatalf("%s: mount errors %+v", what, r.MountErrors)
		}
		var named []string
		for _, f := range r.Orphans {
			named = append(named, f.Path)
		}
		for _, f := range r.UnindexedPages {
			named = append(named, f.Path)
		}
		for _, f := range r.MissingFrontmatter {
			named = append(named, f.Path)
		}
		for _, f := range r.EmptySections {
			named = append(named, f.Path)
		}
		for _, f := range r.ReadErrors {
			named = append(named, f.Path)
		}
		for _, f := range r.StaleIndexEntries {
			named = append(named, f.Source)
		}
		for _, s := range r.Stubs {
			named = append(named, s.Path)
		}
		for _, f := range r.DeadLinks {
			named = append(named, f.Source, f.ResolvedPath)
		}
		for _, w := range r.WantedPages {
			for _, l := range w.Links {
				named = append(named, l.Source)
			}
		}
		if joined := strings.Join(named, ","); strings.Contains(joined, vault.KbDir+"/") {
			t.Fatalf("%s: a mount's page is a finding: %s", what, joined)
		}
	}

	// Given mounts, before the project has a kb/ folder at all.
	given := map[string]string{"ai-ml": filepath.Join(aiml, "wiki"), "other": filepath.Join(other, "wiki")}
	r, err := Run(root, Options{AsOf: asOf, Mounts: given})
	if err != nil {
		t.Fatal(err)
	}
	check("given mounts", r)

	if err := os.MkdirAll(filepath.Join(root, vault.KbDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ai-ml", "other"} {
		if err := os.Symlink(given[name], filepath.Join(root, vault.KbDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	r, err = Run(root, Options{AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	check("symlinks", r)
	issues := r.Summary.IssuesFound

	// A map of no mounts leaves the symlinks unread.
	r, err = Run(root, Options{AsOf: asOf, Mounts: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	var wanted []string
	for _, w := range r.WantedPages {
		wanted = append(wanted, w.Title)
	}
	if strings.Join(wanted, ",") != "backprop,Backpropagation,Backpropogation,Nowhere,Twice" {
		t.Fatalf("no mounts, nothing resolves: %v", wanted)
	}

	if err := os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, vault.KbDir, "gone")); err != nil {
		t.Fatal(err)
	}
	r, err = Run(root, Options{AsOf: asOf})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.MountErrors) != 1 || r.MountErrors[0].Path != "kb/gone" || !strings.Contains(r.MountErrors[0].Message, "points at nothing") {
		t.Fatalf("mount errors %+v", r.MountErrors)
	}
	if r.Summary.CategoryCounts["mount_errors"] != 1 || r.Summary.IssuesFound != issues+1 {
		t.Fatalf("summary %+v", r.Summary)
	}
	if !strings.Contains(r.Markdown(), "## Mounts (1)") {
		t.Fatalf("markdown:\n%s", r.Markdown())
	}
	// The mounts that do resolve still do: only the typo is dead.
	if len(r.DeadLinks) != 1 || r.DeadLinks[0].Target != "Backpropogation" {
		t.Fatalf("dead links beside the broken mount: %+v", r.DeadLinks)
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

func TestTaskErrors(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "wiki", "tasks", "archive"), 0o755)
	write := func(rel, text string) {
		os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(text), 0o644)
	}
	task := func(status, id, extra string) string {
		return "---\ntype: task\ntitle: T\nstatus: " + status + "\npriority: normal\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags:\n  - task\ntask_id: " + id + "\n---\n\n# T\n\n## Idea\n\nx\n" + extra
	}
	write("wiki/tasks/tasks.md", "---\ntype: meta\ntitle: Tasks\nstatus: evergreen\ncreated: 2026-08-01\nupdated: 2026-08-01\ntags:\n  - meta\n---\n\n[[ok]] [[stale]] [[noplan]] [[wrong]]\n")
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
