package refresh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/tree"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

var today = time.Date(2026, 9, 11, 12, 0, 0, 0, time.Local)

func fakeVault(t *testing.T, log, hot string, pages map[string]string) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	os.MkdirAll(filepath.Join(vault, "wiki"), 0o755)
	os.WriteFile(filepath.Join(vault, ".claude-atlas.json"), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	os.WriteFile(filepath.Join(vault, "wiki", "log.md"), []byte(log), 0o644)
	os.WriteFile(filepath.Join(vault, "wiki", "hot.md"), []byte(hot), 0o644)
	for name, text := range pages {
		os.WriteFile(filepath.Join(vault, "wiki", name), []byte(text), 0o644)
	}
	return vault
}

func leaf(vault string) *tree.Project {
	return &tree.Project{Path: "/x.md", Rel: "x", Frontmatter: tree.Frontmatter{Name: "x", Vault: vault, Priority: "normal", State: "active"}}
}

func TestHeat(t *testing.T) {
	for days, want := range map[int]string{0: "hot", 6: "hot", 7: "warm", 29: "warm", 30: "cold"} {
		d := days
		if got := Heat(&d, nil); got != want {
			t.Errorf("%d days: got %s want %s", days, got, want)
		}
	}
	if Heat(nil, p(0)) != "" {
		t.Error("nil idleness should be unknown even when new")
	}
	if Heat(p(0), p(3)) != "new" || Heat(p(40), p(6)) != "new" {
		t.Error("a vault under 7 days old is new whatever its idleness")
	}
	if Heat(p(0), p(7)) != "hot" {
		t.Error("7 days old is no longer new")
	}
}

func TestCreatedDateComesFromTheIndexPage(t *testing.T) {
	vault := fakeVault(t, "", "", map[string]string{
		"index.md":    "---\ntitle: Wiki Index\ncreated: 2026-09-01\nupdated: 2026-09-10\n---\n",
		"overview.md": "---\ncreated: 2020-01-01\n---\n",
	})
	got, ok := CreatedDate(vault)
	if !ok || got.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := CreatedDate(fakeVault(t, "", "", nil)); ok {
		t.Fatal("no created field should report none")
	}
}

func TestDeriveMarksAFreshVaultNew(t *testing.T) {
	vault := fakeVault(t, "", "", map[string]string{"index.md": "---\ncreated: " + time.Now().Format("2006-01-02") + "\n---\n"})
	state := Derive(leaf(vault), time.Now(), "t")
	if state.Heat != "new" || state.Created != time.Now().Format("2006-01-02") {
		t.Fatalf("got heat %q created %q", state.Heat, state.Created)
	}
}

func TestNewestLogDate(t *testing.T) {
	vault := fakeVault(t, "# Log\n\n## 2026-09-04 — a\n\n## 2026-09-10 — b\n\n## not-a-date\n", "", nil)
	got, ok := NewestLogDate(vault)
	if !ok || got.Format("2006-01-02") != "2026-09-10" {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := NewestLogDate(filepath.Join(t.TempDir(), "missing")); ok {
		t.Fatal("missing vault should report no date")
	}
}

func TestActiveThreadsJoinsContinuationLines(t *testing.T) {
	vault := fakeVault(t, "", "# R\n\n## Recent Changes\n\n- ignored\n\n## Active Threads\n\n- First thread\n  continues here.\n- Second\n\n## Later\n\n- no\n", nil)
	got := ActiveThreads(vault)
	if strings.Join(got, "|") != "First thread continues here.|Second" {
		t.Fatalf("got %v", got)
	}
}

func TestSeedPagesCountsFrontmatterStatus(t *testing.T) {
	vault := fakeVault(t, "", "", map[string]string{
		"a.md": "---\ntitle: A\nstatus: seed\n---\n# A\n",
		"b.md": "---\nstatus: \"seed\"\n---\n",
		"c.md": "---\nstatus: evergreen\n---\n",
		"d.md": "no frontmatter\nstatus: seed\n",
	})
	if got := SeedPages(vault); got != 2 {
		t.Fatalf("got %d", got)
	}
}

func TestPlainTextStripsWikilinks(t *testing.T) {
	if got := PlainText("See [[Spec]] and [[Long Name|alias]]."); got != "See Spec and alias." {
		t.Fatalf("got %q", got)
	}
}

func TestDeriveMarksMissingVault(t *testing.T) {
	state := Derive(leaf(filepath.Join(t.TempDir(), "nope")), today, "t")
	if state.VaultOK || state.VaultError != "not found" || state.Heat != "" {
		t.Fatalf("got %+v", state)
	}
	plain := t.TempDir()
	if state := Derive(leaf(plain), today, "t"); state.VaultError != "not a claude-atlas vault" {
		t.Fatalf("got %+v", state)
	}
	legacy := t.TempDir()
	os.MkdirAll(filepath.Join(legacy, "wiki"), 0o755)
	os.WriteFile(filepath.Join(legacy, ".claude-obsidian.json"), []byte("{}"), 0o644)
	state = Derive(leaf(legacy), today, "t")
	if !state.VaultOK || !state.Legacy {
		t.Fatalf("legacy vault should read: %+v", state)
	}
	if notes := strings.Join(Signals(leaf(legacy), state, today), "\n"); !strings.Contains(notes, "claude-obsidian vault; adopt it") {
		t.Fatalf("signals %q", notes)
	}
}

func TestDeriveTakesLaterOfLogAndMtime(t *testing.T) {
	vault := fakeVault(t, "## 2026-08-01 — old\n", "", nil)
	state := Derive(leaf(vault), time.Now(), "t")
	if state.LastOperation != "2026-08-01" || state.LastTouched != time.Now().Format("2006-01-02") {
		t.Fatalf("got %+v", state)
	}
	if state.DaysIdle == nil || *state.DaysIdle != 0 || state.Heat != "hot" {
		t.Fatalf("idle %v heat %s", state.DaysIdle, state.Heat)
	}
	if !state.VaultOK || state.Pages == nil || *state.Pages != 2 {
		t.Fatalf("got %+v", state)
	}
}

func p(v int) *int { return &v }

func TestSignals(t *testing.T) {
	node := leaf("/v")
	node.Priority, node.ReviewAfter = "high", "2026-01-01"
	notes := strings.Join(Signals(node, &tree.State{VaultOK: true, Heat: "cold", DaysIdle: p(45)}, today), "\n")
	if !strings.Contains(notes, "priority high") || !strings.Contains(notes, "45 days") || !strings.Contains(notes, "review date 2026-01-01") {
		t.Fatalf("got %q", notes)
	}
	blocked := leaf("/v")
	blocked.State, blocked.BlockedOn = "blocked", "hardware"
	if got := Signals(blocked, &tree.State{VaultOK: true, Heat: "hot"}, today); len(got) != 1 || got[0] != "blocked on: hardware" {
		t.Fatalf("got %v", got)
	}
}

func TestRenderListsRowsAndSignals(t *testing.T) {
	node := leaf("/Users/me/v")
	node.Rel, node.Purpose = "work/v", "Why."
	state := &tree.State{VaultOK: true, LastTouched: "2026-08-01", DaysIdle: p(41), Heat: "cold", Pages: p(3), OpenThreads: []string{"thread [[one]]"}, Unfinished: tree.Unfinished{EmptySections: p(1), SeedPages: p(2), DeadLinks: p(0)}}
	res := &Result{Rows: []Row{{node, state}}, Problems: []tree.Problem{{Rel: "stray", Reason: "missing frontmatter"}}}
	page := Render(res, "2026-09-11T20:00:00Z", today)
	for _, want := range []string{
		"| ❄️ cold | [[tree/work/v\\|x]] | [[categories/work\\|work]] | normal | active | 41d | 3 | 1 | 3 |",
		"| Vault | `/Users/me/v` |",
		"## Signals", "> [!failure] tree/stray.md\n> Not a project: missing frontmatter.", "> [!warning] work/v", "cold for 41 days",
		"## work\n\n### x\n\n> [!abstract] Purpose\n> Why.", "> - thread one",
		"| Unfinished | 1 empty sections · 2 seed pages · 0 dead links |",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q in:\n%s", want, page)
		}
	}
}

func TestRunAgainstARealVault(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{Schema: home.ConfigSchema, VaultsDir: filepath.Join(root, "Vaults"), AtlasVault: filepath.Join(root, "atlas")}
	os.MkdirAll(cfg.TreeRoot(), 0o755)
	fresh := filepath.Join(cfg.VaultsDir, "fresh")
	if _, err := vault.Init(fresh, vault.Generic, time.Now()); err != nil {
		t.Fatal(err)
	}
	project, err := vaults.Register(cfg, fresh, vaults.RegisterOptions{Category: "area"})
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(stateDir, "stale.json"), []byte("{}"), 0o644)
	page, res, err := Run(cfg, stateDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	state, err := tree.ReadState(stateDir, project.Rel)
	if err != nil {
		t.Fatal(err)
	}
	if !state.VaultOK || *state.Pages != 4 || state.Heat != "new" || len(state.OpenThreads) != 1 || *state.Unfinished.EmptySections != 0 || state.Project != "area/fresh" {
		t.Fatalf("state %+v", state)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "stale.json")); err == nil {
		t.Fatal("stale state should be pruned")
	}
	text, _ := os.ReadFile(page)
	if filepath.Base(page) != "Overview.md" || !strings.Contains(string(text), "[[tree/area/fresh\\|fresh]]") || len(res.Rows) != 1 {
		t.Fatalf("page:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(cfg.AtlasVault, ".obsidian", "graph.json")); err != nil {
		t.Fatal("refresh should write graph defaults when there are none")
	}
	// A plain path in the page is upgraded to a page on the next run, and links.json lists it.
	code := filepath.Join(root, "code")
	os.MkdirAll(filepath.Join(code, ".git"), 0o755)
	if err := tree.UpdateFrontmatter(project.Path, map[string]any{"repos": []string{code}}); err != nil {
		t.Fatal(err)
	}
	_, res, err = Run(cfg, stateDir, time.Now())
	if err != nil || len(res.Upgraded) != 1 || len(res.Links) != 1 || res.Links[0].Page.Name != "code" || len(res.Links[0].Projects) != 1 {
		t.Fatalf("second run: err=%v upgraded=%v links=%+v", err, res.Upgraded, res.Links)
	}
	ls, err := ReadLinksState(stateDir)
	if err != nil || len(ls.Links) != 1 || ls.Links[0].Page.Name != "code" || ls.Links[0].Projects[0] != project.Rel {
		t.Fatalf("links state %+v %v", ls, err)
	}
	state, _ = tree.ReadState(stateDir, project.Rel)
	if len(state.Links) != 1 || state.Links[0].Name != "code" {
		t.Fatalf("project state links %+v", state.Links)
	}
}

func TestLinksCountAsActivityAndMissingOnesSignal(t *testing.T) {
	vault := fakeVault(t, "## 2020-01-01 — old\n", "", nil)
	docs := filepath.Join(t.TempDir(), "docs")
	os.MkdirAll(docs, 0o755)
	os.WriteFile(filepath.Join(docs, "slides.pdf"), []byte("x"), 0o644)
	node := leaf(vault)
	node.Materials = []string{docs}
	node.Repos = []string{filepath.Join(t.TempDir(), "gone")}
	tree.ResolveLinks(node, nil)
	state := Derive(node, time.Now(), "t")
	if len(state.Links) != 2 || !state.Links[1].OK || state.Links[0].OK {
		t.Fatalf("links %+v", state.Links)
	}
	if state.LastTouched != time.Now().Format("2006-01-02") {
		t.Fatalf("material activity should count: last touched %s", state.LastTouched)
	}
	notes := strings.Join(Signals(node, state, time.Now()), "\n")
	if !strings.Contains(notes, "repo ") || !strings.Contains(notes, "not found") {
		t.Fatalf("signals %q", notes)
	}
	page := Render(&Result{Rows: []Row{{node, state}}}, "2026-09-12T18:00:00Z", time.Now())
	if !strings.Contains(page, "| Materials | `") || !strings.Contains(page, "1 file") || !strings.Contains(page, "> [!failure] x") {
		t.Fatalf("page:\n%s", page)
	}
	// With a page, the link renders as a wikilink and the name shows in the signal.
	node.Linked[0].Name = "gone"
	state = Derive(node, time.Now(), "t")
	if state.Links[0].Name != "gone" {
		t.Fatalf("name should carry: %+v", state.Links)
	}
	notes = strings.Join(Signals(node, state, time.Now()), "\n")
	page = Render(&Result{Rows: []Row{{node, state}}}, "2026-09-12T18:00:00Z", time.Now())
	if !strings.Contains(notes, "repo gone (") || !strings.Contains(page, "| Repo | [[repos/gone\\|gone]] · not found |") {
		t.Fatalf("notes %q page:\n%s", notes, page)
	}
}

func TestWarningsAndLinkSignalsRender(t *testing.T) {
	node := leaf("/v")
	node.Warnings = []string{"related: [[nope]] names no project"}
	notes := Signals(node, &tree.State{VaultOK: true, Heat: "hot"}, today)
	if len(notes) != 1 || notes[0] != node.Warnings[0] {
		t.Fatalf("notes %v", notes)
	}
	res := &Result{Rows: []Row{{node, &tree.State{VaultOK: true}}},
		Links:        []LinkRow{{Page: links.Page{Kind: links.Repo, Name: "lonely", Path: "/r"}, Link: links.Link{Kind: links.Repo, Name: "lonely", Path: "/r", OK: true}, Projects: []string{}}},
		LinkProblems: []links.Problem{{File: "repos/bad.md", Reason: "missing frontmatter"}}}
	got := strings.Join(LinkSignals(res), "\n")
	if !strings.Contains(got, "repos/bad.md is not a link page: missing frontmatter") || !strings.Contains(got, "repos/lonely.md is linked by no project") {
		t.Fatalf("link signals %q", got)
	}
	page := Render(res, "2026-09-12T18:00:00Z", today)
	for _, want := range []string{"> [!warning] x\n> Related: [[nope]] names no project", "> [!info] Repos/lonely.md is linked by no project", "> [!failure] Repos/bad.md is not a link page", "## Repos and materials", "| repo | [[repos/lonely\\|lonely]] | `/r` | — | ok |"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q in:\n%s", want, page)
		}
	}
}

func TestCategoriesMakeTheTreeAGraph(t *testing.T) {
	a := leaf("/a")
	a.Rel, a.Name, a.Purpose = "work/field/a", "A", "First line.\nSecond."
	b := leaf("/b")
	b.Rel, b.Name = "work/b", "B"
	c := leaf("/c")
	c.Rel, c.Name = "c", "C"
	cats := categories([]*tree.Project{a, b, c})
	if len(cats) != 3 || len(cats[""].children) != 1 || cats[""].children[0] != "work" || len(cats[""].projects) != 1 {
		t.Fatalf("cats %+v", cats)
	}
	if countProjects(cats, "work") != 2 || len(cats["work"].projects) != 1 || cats["work"].children[0] != "work/field" {
		t.Fatalf("work %+v", cats["work"])
	}
	field := renderCategory(cats, cats["work/field"])
	for _, want := range []string{"schema: atlas.category.v1", "name: field", "path: work/field", "Part of [[categories/work|work]].", "- [[tree/work/field/a|A]] — First line."} {
		if !strings.Contains(field, want) {
			t.Errorf("missing %q in:\n%s", want, field)
		}
	}
	root := renderCategory(cats, cats[""])
	if !strings.Contains(root, "title: Tree") || !strings.Contains(root, "- [[categories/work|work]] · 2 projects") || !strings.Contains(root, "- [[tree/c|C]]") {
		t.Fatalf("root:\n%s", root)
	}
	cfg := &home.Config{AtlasVault: t.TempDir()}
	os.MkdirAll(filepath.Join(cfg.AtlasVault, CategoriesDir, "stale"), 0o755)
	if err := WriteCategories(cfg, []*tree.Project{a, b, c}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"Tree.md", "categories/work.md", "categories/work/field.md"} {
		if _, err := os.Stat(filepath.Join(cfg.AtlasVault, path)); err != nil {
			t.Errorf("missing %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(cfg.AtlasVault, CategoriesDir, "stale")); err == nil {
		t.Fatal("stale category pages should be removed")
	}
}
