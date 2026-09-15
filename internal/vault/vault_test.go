package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
)

var now = time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)

func needGit(t *testing.T) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestInitCreatesACompleteVaultWithOneCommit(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "vaults", "fresh")
	res, err := Init(root, Options{Kind: Project, Mode: Generic}, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{Marker, ".gitignore", ".obsidian/app.json", "inbox/.gitkeep", LogPage, HotPage, IndexPage, OverviewPage, LedgerPath} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
	index, _ := os.ReadFile(filepath.Join(root, "wiki", "index.md"))
	if !strings.Contains(string(index), "created: 2026-09-12") {
		t.Fatalf("template not rendered:\n%s", index)
	}
	v, err := Open(root)
	if err != nil || v.Config.Schema != Schema || v.Config.Kind != Project || v.Config.Mode != Generic || v.Config.Created != "2026-09-12" {
		t.Fatalf("open %+v %v", v, err)
	}
	if v.Name() != "fresh" || !uuidPattern.MatchString(v.Config.ID) {
		t.Fatalf("name %q id %q", v.Name(), v.Config.ID)
	}
	repo := v.Repo()
	if !repo.IsRepo() || !repo.HasHead() {
		t.Fatal("init must create a repository with a commit")
	}
	if dirty, _ := repo.Dirty(); dirty {
		t.Fatal("tree should be clean after init")
	}
	commits, _ := repo.Log(1)
	if commits[0].SHA != res.Commit || commits[0].Trailers["atlas-operation"] != res.OperationID || !strings.HasPrefix(commits[0].Subject, "setup: initialize project fresh") {
		t.Fatalf("commit %+v", commits[0])
	}
	if _, err := Init(root, Options{Kind: Project}, now); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("second init should refuse: %v", err)
	}
}

func TestInitRefusesInsideAnotherRepo(t *testing.T) {
	needGit(t)
	outer := gitx.Repo{Dir: t.TempDir()}
	outer.Init()
	if _, err := Init(filepath.Join(outer.Dir, "v"), Options{Kind: Project, Mode: Generic}, now); err == nil || !strings.Contains(err.Error(), "inside another git repository") {
		t.Fatalf("got %v", err)
	}
}

func TestIdentityFile(t *testing.T) {
	needGit(t)
	kb := filepath.Join(t.TempDir(), "ai-ml")
	if _, err := Init(kb, Options{Kind: Knowledge, Name: "AI and ML"}, now); err != nil {
		t.Fatal(err)
	}
	v, err := Open(kb)
	if err != nil {
		t.Fatal(err)
	}
	if v.Config.Kind != Knowledge || v.Config.Mode != Generic || v.Name() != "AI and ML" || v.Config.Access != AccessOpen {
		t.Fatalf("knowledge base config %+v", v.Config)
	}
	proot := filepath.Join(t.TempDir(), "p")
	if _, err := Init(proot, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	p, _ := Open(proot)
	if p.Config.ID == v.Config.ID || !uuidPattern.MatchString(p.Config.ID) {
		t.Fatalf("ids %q %q", p.Config.ID, v.Config.ID)
	}
	if p.Config.Access != "" || p.Name() != "p" {
		t.Fatalf("project config %+v", p.Config)
	}
	raw, _ := os.ReadFile(filepath.Join(proot, Marker))
	for _, absent := range []string{"scope", "access", "grants", "tags", "mounts", "repos"} {
		if strings.Contains(string(raw), `"`+absent+`"`) {
			t.Errorf("a fresh identity file carries %q:\n%s", absent, raw)
		}
	}
	if _, err := Init(filepath.Join(t.TempDir(), "x"), Options{}, now); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("kind is required: %v", err)
	}
	old := t.TempDir()
	os.WriteFile(filepath.Join(old, Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic","created":"2026-09-12"}`), 0o644)
	if _, err := Open(old); !errors.Is(err, ErrV1) || !strings.Contains(err.Error(), "adopt") {
		t.Fatalf("v1 open: %v", err)
	}
	if cfg, ok := ReadConfig(old); !ok || cfg.Schema != SchemaV1 || cfg.Mode != Generic {
		t.Fatalf("ReadConfig %+v %v", cfg, ok)
	}
	if _, ok := ReadConfig(t.TempDir()); ok {
		t.Fatal("no marker, no config")
	}
}

func TestAdoptKeepsExistingFilesAndFillsGaps(t *testing.T) {
	needGit(t)
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "wiki"), 0o755)
	os.MkdirAll(filepath.Join(root, ".obsidian"), 0o755)
	os.WriteFile(filepath.Join(root, ".claude-obsidian.json"), []byte(`{"schema":"claude-obsidian.workspace.v1","vault":"."}`), 0o644)
	os.WriteFile(filepath.Join(root, "wiki", "index.md"), []byte("---\ntitle: Mine\n---\n# Mine\n"), 0o644)
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".trash/\n"), 0o644)
	os.MkdirAll(filepath.Join(root, ".vault-meta"), 0o755)
	os.WriteFile(filepath.Join(root, ".vault-meta", "mode.json"), []byte(`{"mode":"lyt"}`), 0o644)
	if !IsLegacy(root) {
		t.Fatal("should detect a claude-obsidian vault")
	}
	res, err := Adopt(root, Options{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.WasLegacy || !res.GitInitialized || res.Commit == "" {
		t.Fatalf("result %+v", res)
	}
	index, _ := os.ReadFile(filepath.Join(root, "wiki", "index.md"))
	if string(index) != "---\ntitle: Mine\n---\n# Mine\n" {
		t.Fatal("adopt must not replace an existing page")
	}
	v, err := Open(root)
	if err != nil || v.Config.Mode != LYT {
		t.Fatalf("adopted vault %+v %v", v, err)
	}
	ignore, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if !strings.Contains(string(ignore), ".trash/") || !strings.Contains(string(ignore), ".vault-meta/") || strings.Count(string(ignore), ".trash/") != 1 {
		t.Fatalf("gitignore merge:\n%s", ignore)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "log.md")); err != nil {
		t.Fatal("missing template page should be added")
	}
	if v.Repo().Tracked(".vault-meta/mode.json") {
		t.Fatal("runtime state must not be committed")
	}
	again, err := Adopt(root, Options{}, now)
	if err != nil || !again.AlreadyAdopted || again.Commit != "" || len(again.Added) != 0 {
		t.Fatalf("second adopt should be a no-op: %+v %v", again, err)
	}
}

func TestUpgradeAndAdoptMoveTheTaskIndexFromItsOldPath(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "older")
	if _, err := Init(root, Options{Kind: Project, Mode: Generic}, now); err != nil {
		t.Fatal(err)
	}
	repo := gitx.Repo{Dir: root}
	oldPath := filepath.Join(root, "wiki", "tasks", "index.md")
	newPath := filepath.Join(root, filepath.FromSlash(TasksIndex))
	board := "---\ntype: meta\ntitle: Tasks\n---\n\n# Tasks\n\n[[Fix it]]\n"
	os.Remove(newPath)
	os.WriteFile(oldPath, []byte(board), 0o644)
	repo.AddAll()
	repo.Commit(CommitMessage("setup", "an older layout", NewOperationID("setup", now)))

	res, err := Upgrade(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 0 || len(res.Moved) != 1 || res.Moved[0] != (Move{From: "wiki/tasks/index.md", To: TasksIndex}) {
		t.Fatalf("upgrade %+v", res)
	}
	if data, _ := os.ReadFile(newPath); string(data) != board {
		t.Fatalf("the index should keep its content:\n%s", data)
	}
	if _, err := os.Stat(oldPath); err == nil {
		t.Fatal("the old path should be gone")
	}
	if dirty, _ := repo.Dirty(); dirty {
		t.Fatal("upgrade should commit the move")
	}
	commits, _ := repo.Log(1)
	if !strings.Contains(commits[0].Subject, "move wiki/tasks/index.md to wiki/tasks/tasks.md") {
		t.Fatalf("commit %q", commits[0].Subject)
	}
	if again, err := Upgrade(root, now); err != nil || len(again.Added)+len(again.Moved) != 0 {
		t.Fatalf("a second upgrade should do nothing: %+v %v", again, err)
	}

	// A stale copy at the old path next to the current index goes; the current index stays.
	os.WriteFile(oldPath, []byte("stale\n"), 0o644)
	repo.AddAll()
	repo.Commit(CommitMessage("manual", "a stale copy", NewOperationID("manual", now)))
	adopted, err := Adopt(root, Options{}, now)
	if err != nil || len(adopted.Moved) != 1 || adopted.Commit == "" {
		t.Fatalf("adopt %+v %v", adopted, err)
	}
	if _, err := os.Stat(oldPath); err == nil {
		t.Fatal("adopt should remove the stale copy")
	}
	if data, _ := os.ReadFile(newPath); string(data) != board {
		t.Fatalf("the current index should stay:\n%s", data)
	}

	// A file at the old path that git does not hold as it is belongs to the user; it stays,
	// untracked or edited.
	keeps := func(which, want string) {
		t.Helper()
		if res, err := Upgrade(root, now); err != nil || len(res.Moved) != 0 {
			t.Fatalf("upgrade should leave %s file alone: %+v %v", which, res, err)
		}
		if data, _ := os.ReadFile(oldPath); string(data) != want {
			t.Fatalf("%s file changed:\n%s", which, data)
		}
	}
	os.WriteFile(oldPath, []byte("my notes\n"), 0o644)
	keeps("an untracked", "my notes\n")
	repo.AddAll()
	repo.Commit(CommitMessage("manual", "the user's page", NewOperationID("manual", now)))
	os.WriteFile(oldPath, []byte("my notes, edited\n"), 0o644)
	keeps("an edited", "my notes, edited\n")
	os.Remove(oldPath)
	repo.AddAll()
	repo.Commit(CommitMessage("manual", "remove the user's page", NewOperationID("manual", now)))
	if _, err := Ignore(root, "wiki/tasks/index.md", now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(oldPath, []byte("my ignored notes\n"), 0o644)
	keeps("an ignored", "my ignored notes\n")
}

func TestAdoptAsKnowledgeRemovesTaskScaffolding(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "old")
	if _, err := Init(root, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"lyt","created":"2026-09-01"}`), 0o644)
	os.WriteFile(filepath.Join(root, "wiki", "tasks", "Do it.md"), []byte("---\ntype: task\ntitle: Do it\n---\n"), 0o644)
	os.WriteFile(filepath.Join(root, "inbox", "tasks", "note.md"), []byte("later\n"), 0o644)
	repo := gitx.Repo{Dir: root}
	repo.AddAll()
	repo.Commit("old state")
	res, err := Adopt(root, Options{Kind: Knowledge}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromV1 || res.Kind != Knowledge || strings.Join(res.Removed, ",") != "ideas,inbox,wiki/meta/ledgers/task-ledger.json,wiki/tasks" {
		t.Fatalf("result %+v", res)
	}
	if res.Baseline != "" {
		t.Fatalf("a committed, clean tree needs no baseline: %q", res.Baseline)
	}
	v, err := Open(root)
	if err != nil || v.Config.Kind != Knowledge || v.Config.Mode != LYT || v.Config.Created != "2026-09-01" || v.Config.ID == "" {
		t.Fatalf("open %+v %v", v, err)
	}
	for _, rel := range res.Removed {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s still exists", rel)
		}
	}
	if dirty, _ := repo.Dirty(); dirty {
		t.Fatal("adopt must leave the tree clean")
	}
	commits, _ := repo.Log(1)
	if !strings.HasPrefix(commits[0].Subject, "setup: adopt v1 vault as knowledge old") {
		t.Fatalf("commit %+v", commits[0])
	}
	if again, err := Adopt(root, Options{}, now); err != nil || again.Commit != "" || !again.AlreadyAdopted {
		t.Fatalf("second adopt %+v %v", again, err)
	}
	if _, err := Adopt(root, Options{Kind: Project}, now); err == nil || !strings.Contains(err.Error(), "does not change") {
		t.Fatalf("kind is fixed: %v", err)
	}

	withSources := filepath.Join(t.TempDir(), "busy")
	if _, err := Init(withSources, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(withSources, Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic","created":"2026-09-01"}`), 0o644)
	os.WriteFile(filepath.Join(withSources, "inbox", "paper.pdf"), []byte("%PDF"), 0o644)
	if _, err := Adopt(withSources, Options{Kind: Knowledge}, now); err == nil || !strings.Contains(err.Error(), "inbox/ holds 1 file") {
		t.Fatalf("an inbox with sources stops the removal: %v", err)
	}
}

func TestAdoptAsKnowledgeCommitsABaselineBeforeRemoving(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "plain")
	for rel, body := range map[string]string{
		"wiki/index.md":       "---\ntitle: Index\n---\n\n# Index\n",
		"wiki/tasks/Do it.md": "the task text\n",
		"ideas/note.md":       "an idea\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Adopt(root, Options{Kind: Knowledge}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Baseline == "" || res.Commit == "" || res.Baseline == res.Commit {
		t.Fatalf("baseline %q commit %q", res.Baseline, res.Commit)
	}
	repo := gitx.Repo{Dir: root}
	commits, err := repo.Log(2)
	if err != nil || len(commits) != 2 {
		t.Fatalf("log %+v %v", commits, err)
	}
	if commits[0].SHA != res.Commit || !strings.HasPrefix(commits[0].Subject, "setup: adopt knowledge plain") {
		t.Fatalf("adoption commit %+v", commits[0])
	}
	if commits[1].SHA != res.Baseline || commits[1].Subject != "setup: baseline before adopting as knowledge base" {
		t.Fatalf("baseline commit %+v", commits[1])
	}
	saved, err := repo.ShowFile(res.Baseline, "wiki/tasks/Do it.md")
	if err != nil || string(saved) != "the task text\n" {
		t.Fatalf("the baseline must hold the removed page: %q %v", saved, err)
	}
	for _, rel := range []string{"wiki/tasks", "ideas"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s still exists", rel)
		}
	}
	if dirty, _ := repo.Dirty(); dirty {
		t.Fatal("adopt must leave the tree clean")
	}
	project := filepath.Join(t.TempDir(), "ready")
	if _, err := Init(project, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	again, err := Adopt(project, Options{Kind: Project}, now)
	if err != nil || again.Baseline != "" {
		t.Fatalf("a clean vault with history gets no baseline: %+v %v", again, err)
	}
}

func TestAdoptRepairsADamagedIdentityFile(t *testing.T) {
	needGit(t)
	broken := filepath.Join(t.TempDir(), "broken")
	if _, err := Init(broken, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(broken, Marker), []byte("{not json"), 0o644)
	res, err := Adopt(broken, Options{Kind: Project}, now)
	if err != nil || !res.Repaired || res.Kind != Project {
		t.Fatalf("unreadable json: %+v %v", res, err)
	}
	v, err := Open(broken)
	if err != nil || v.Config.Kind != Project || v.Config.ID == "" {
		t.Fatalf("open after the repair: %+v %v", v, err)
	}
	commits, _ := gitx.Repo{Dir: broken}.Log(1)
	if len(commits) == 0 || !strings.Contains(commits[0].Subject, "repaired identity file") {
		t.Fatalf("commit %+v", commits)
	}

	noID := filepath.Join(t.TempDir(), "kb")
	if _, err := Init(noID, Options{Kind: Knowledge, Name: "Old KB"}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(noID, Marker), []byte(`{"schema":"claude-atlas.vault.v2","kind":"knowledge","name":"Old KB","mode":"lyt","created":"2026-01-02"}`), 0o644)
	res, err = Adopt(noID, Options{}, now)
	if err != nil || !res.Repaired || res.Kind != Knowledge {
		t.Fatalf("a file with no id: %+v %v", res, err)
	}
	v, err = Open(noID)
	if err != nil || v.Config.ID == "" || v.Config.Name != "Old KB" || v.Config.Created != "2026-01-02" || v.Config.Mode != LYT {
		t.Fatalf("repaired knowledge base %+v %v", v, err)
	}

	noKind := filepath.Join(t.TempDir(), "nokind")
	if _, err := Init(noKind, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(noKind, Marker), []byte(`{"schema":"claude-atlas.vault.v2","id":"1","kind":"","name":"nokind","mode":"generic"}`), 0o644)
	if _, err := Adopt(noKind, Options{}, now); err == nil || !strings.Contains(err.Error(), "pass --as") {
		t.Fatalf("a repair without a kind must ask for one: %v", err)
	}

	future := filepath.Join(t.TempDir(), "future")
	if _, err := Init(future, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`{"schema":"claude-atlas.vault.v3","id":"1","kind":"project","name":"future"}`)
	os.WriteFile(filepath.Join(future, Marker), marker, 0o644)
	if _, err := Adopt(future, Options{}, now); err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("an unknown schema is refused: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(future, Marker)); string(after) != string(marker) {
		t.Fatalf("the identity file must be untouched:\n%s", after)
	}
}

func TestResolveOrder(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "v")
	Init(root, Options{Kind: Project, Mode: Generic}, now)
	nested := filepath.Join(root, "wiki", "concepts")
	os.MkdirAll(nested, 0o755)
	if v, err := Resolve("", "", nested); err != nil || v.Root != root {
		t.Fatalf("walk up: %v %v", v, err)
	}
	if v, err := Resolve("", root, t.TempDir()); err != nil || v.Root != root {
		t.Fatalf("env: %v %v", v, err)
	}
	if _, err := Resolve("", "", t.TempDir()); err == nil {
		t.Fatal("no vault should fail closed")
	}
	if _, err := Resolve(t.TempDir(), root, root); err == nil {
		t.Fatal("explicit non-vault should fail even when env names one")
	}
}

func TestRouteAndSkeleton(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "v")
	Init(root, Options{Kind: Project, Mode: Generic}, now)
	v, _ := Open(root)
	r, err := v.RouteFor("concept", "Contextual Retrieval: a/b?", now)
	if err != nil || r.Path != "wiki/concepts/Contextual Retrieval a b.md" || r.Exists {
		t.Fatalf("route %+v %v", r, err)
	}
	if !strings.HasPrefix(r.Skeleton, "---\ntype: concept\ntitle: \"Contextual Retrieval a b\"\nstatus: seed\ncreated: 2026-09-12\n") || !strings.Contains(r.Skeleton, "## Definition") {
		t.Fatalf("skeleton:\n%s", r.Skeleton)
	}
	os.MkdirAll(v.Path("wiki/concepts"), 0o755)
	os.WriteFile(v.Path(r.Path), []byte("x"), 0o644)
	if r, _ := v.RouteFor("concept", "Contextual Retrieval: a/b?", now); !r.Exists {
		t.Fatal("existing page should be reported")
	}
	if _, err := v.RouteFor("moc", "Maps", now); err == nil {
		t.Fatal("moc is not a generic type")
	}
	v.Config.Mode = LYT
	if r, _ := v.RouteFor("moc", "AI", now); r.Path != "wiki/mocs/AI.md" {
		t.Fatalf("lyt moc %+v", r)
	}
	if r, _ := v.RouteFor("source", "Paper", now); r.Path != "wiki/notes/Paper.md" {
		t.Fatalf("lyt note %+v", r)
	}
	if SanitizeTitle("  ...  ") != "Untitled" || SanitizeTitle("a\x00b") != "ab" {
		t.Fatal("sanitize edge cases")
	}
}

func TestRoutableTypesByKind(t *testing.T) {
	needGit(t)
	if got := strings.Join(RoutableTypes(Knowledge, Generic), ","); got != "source,entity,concept" {
		t.Fatalf("knowledge generic: %s", got)
	}
	if got := strings.Join(RoutableTypes(Project, LYT), ","); got != "note,moc,source,entity,concept,question,session" {
		t.Fatalf("project lyt: %s", got)
	}
	kb := filepath.Join(t.TempDir(), "kb")
	Init(kb, Options{Kind: Knowledge}, now)
	v, _ := Open(kb)
	if _, err := v.RouteFor("question", "Why", now); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("questions belong to a project: %v", err)
	}
	r, err := v.RouteFor("concept", "Backpropagation", now)
	if err != nil || r.Path != "wiki/concepts/Backpropagation.md" {
		t.Fatalf("route %+v %v", r, err)
	}
	if Knowledge.Noun() != "knowledge base" || Project.Noun() != "project" {
		t.Fatal("nouns")
	}
}

func TestFrontmatter(t *testing.T) {
	fields, body, err := Frontmatter("---\ntitle: A\ntags:\n  - x\n---\n\nBody\n")
	if err != nil || fields["title"] != "A" || body != "\nBody\n" || len(StringList(fields, "tags")) != 1 {
		t.Fatalf("%v %q %v", fields, body, err)
	}
	if missing := MissingFrontmatter(fields); len(missing) != 4 {
		t.Fatalf("missing %v", missing)
	}
	if _, _, err := Frontmatter("---\ntitle: A\n"); err == nil {
		t.Fatal("unterminated block should error")
	}
	if fields, _, err := Frontmatter("no block"); err != nil || fields != nil {
		t.Fatal("no block should be nil, nil")
	}
	if _, _, err := Frontmatter("---\n: : :\n  bad: [\n---\n"); err == nil {
		t.Fatal("invalid yaml should error")
	}
}

func TestInitLayoutByKind(t *testing.T) {
	needGit(t)
	kb := filepath.Join(t.TempDir(), "kb")
	if _, err := Init(kb, Options{Kind: Knowledge}, now); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{InboxDir, InboxTasksDir, IdeasDir, TasksDir, TaskLedgerPath} {
		if _, err := os.Stat(filepath.Join(kb, filepath.FromSlash(rel))); err == nil {
			t.Errorf("a knowledge base has %s", rel)
		}
	}
	for _, rel := range []string{Marker, ".gitignore", AppFile, AppearanceFile, LogPage, HotPage, IndexPage, OverviewPage, LedgerPath} {
		if _, err := os.Stat(filepath.Join(kb, filepath.FromSlash(rel))); err != nil {
			t.Errorf("a knowledge base lacks %s", rel)
		}
	}
	index, _ := os.ReadFile(filepath.Join(kb, "wiki", "index.md"))
	if strings.Contains(string(index), "Questions") || !strings.Contains(string(index), "## Concepts") {
		t.Fatalf("knowledge base index:\n%s", index)
	}
	hot, _ := os.ReadFile(filepath.Join(kb, "wiki", "hot.md"))
	if strings.Contains(string(hot), "inbox/") {
		t.Fatalf("a knowledge base's hot cache must not point at an inbox:\n%s", hot)
	}
	for _, f := range TemplateFiles(Knowledge) {
		if strings.HasPrefix(f, "inbox/") || strings.HasPrefix(f, "ideas/") || strings.HasPrefix(f, "wiki/tasks/") {
			t.Errorf("knowledge template lists %s", f)
		}
	}
	p := filepath.Join(t.TempDir(), "p")
	if _, err := Init(p, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"inbox/.gitkeep", "inbox/tasks/.gitkeep", "ideas/.gitkeep", TasksIndex, TaskLedgerPath, LedgerPath, ".obsidian/snippets/claude-atlas.css"} {
		if _, err := os.Stat(filepath.Join(p, filepath.FromSlash(rel))); err != nil {
			t.Errorf("a project lacks %s", rel)
		}
	}
	ignore, _ := os.ReadFile(filepath.Join(p, ".gitignore"))
	if !strings.Contains(string(ignore), "/kb/") || !strings.Contains(string(ignore), "/repos/") {
		t.Fatalf("a project ignores its mounts and repositories:\n%s", ignore)
	}
	kbIgnore, _ := os.ReadFile(filepath.Join(kb, ".gitignore"))
	if strings.Contains(string(kbIgnore), "/kb/") {
		t.Fatalf("a knowledge base has no mounts to ignore:\n%s", kbIgnore)
	}
}

func TestNewNotesGoUnderTheWikiUnlessTheUserChose(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "v")
	if _, err := Init(root, Options{Kind: Project, Mode: Generic}, now); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, filepath.FromSlash(AppFile))
	settings := func() map[string]any {
		t.Helper()
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var s map[string]any
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := settings(); s["newFileLocation"] != "folder" || s["newFileFolderPath"] != "wiki" {
		t.Fatalf("template settings %v", s)
	}
	repo := gitx.Repo{Dir: root}
	commit := func(what string) {
		repo.AddAll()
		repo.Commit(CommitMessage("manual", what, NewOperationID("manual", now)))
	}
	os.WriteFile(file, []byte(`{"newLinkFormat": "absolute"}`), 0o644)
	commit("older settings")
	res, err := Upgrade(root, now)
	if err != nil || len(res.Added) != 1 || res.Added[0] != AppFile {
		t.Fatalf("upgrade %+v %v", res, err)
	}
	if s := settings(); s["newLinkFormat"] != "absolute" || s["newFileLocation"] != "folder" || s["newFileFolderPath"] != "wiki" {
		t.Fatalf("upgrade keeps settings and adds the folder: %v", s)
	}
	os.WriteFile(file, []byte(`{"newFileLocation": "current"}`), 0o644)
	commit("the user's choice")
	if res, err := Adopt(root, Options{}, now); err != nil || len(res.Added) != 0 {
		t.Fatalf("adopt keeps a location the user chose: %+v %v", res, err)
	}
	if s := settings(); s["newFileLocation"] != "current" || s["newFileFolderPath"] != nil {
		t.Fatalf("settings %v", s)
	}
}

func TestUpdateConfigCommitsOnceAndValidates(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "p")
	if _, err := Init(root, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	err := UpdateConfig(root, "tag usc", now, func(c *Config) error { c.Tags = []string{"usc"}; return nil })
	if err != nil {
		t.Fatal(err)
	}
	v, _ := Open(root)
	if len(v.Config.Tags) != 1 || v.Config.Tags[0] != "usc" {
		t.Fatalf("tags %+v", v.Config)
	}
	repo := v.Repo()
	commits, _ := repo.Log(1)
	if commits[0].Subject != "setup: tag usc" || commits[0].Trailers["atlas-operation"] == "" {
		t.Fatalf("commit %+v", commits[0])
	}
	if dirty, _ := repo.Dirty(); dirty {
		t.Fatal("tree should be clean")
	}
	if err := UpdateConfig(root, "tag usc", now, func(c *Config) error { c.Tags = []string{"usc"}; return nil }); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.Log(1); again[0].SHA != commits[0].SHA {
		t.Fatal("an unchanged file makes no commit")
	}
	if err := UpdateConfig(root, "bad", now, func(c *Config) error { c.Kind = "bogus"; return nil }); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("validation: %v", err)
	}
	if err := UpdateConfig(root, "bad", now, func(c *Config) error { return errors.New("no") }); err == nil {
		t.Fatal("change's error is returned")
	}
	// The file decides what is unique, whatever the caller thought it held.
	if err := UpdateConfig(root, "two repos", now, func(c *Config) error {
		c.Repos = []Repo{{Name: "hw"}, {Name: "HW"}}
		return nil
	}); err == nil || !strings.Contains(err.Error(), `two repositories named "HW"`) {
		t.Fatalf("two repositories with one name: %v", err)
	}
	if v, _ := Open(root); len(v.Config.Repos) != 0 {
		t.Fatalf("a refused update writes nothing: %+v", v.Config.Repos)
	}
	if err := UpdateConfig(root, "two mounts", now, func(c *Config) error {
		c.Mounts = []Mount{{ID: "k1", Name: "ai-ml", Access: AccessRead}, {ID: "k1", Name: "robotics", Access: AccessRead}}
		return nil
	}); err == nil || !strings.Contains(err.Error(), "two mounts of k1") {
		t.Fatalf("two mounts with one id: %v", err)
	}
	if err := UpdateConfig(root, "two mounts", now, func(c *Config) error {
		c.Mounts = []Mount{{ID: "k1", Name: "ai-ml", Access: AccessRead}, {ID: "k2", Name: "ai-ml", Access: AccessRead}}
		return nil
	}); err == nil || !strings.Contains(err.Error(), "two mounts of ai-ml") {
		t.Fatalf("two mounts with one name: %v", err)
	}
	kb := filepath.Join(t.TempDir(), "k")
	if _, err := Init(kb, Options{Kind: Knowledge}, now); err != nil {
		t.Fatal(err)
	}
	if err := UpdateConfig(kb, "two grants", now, func(c *Config) error {
		c.Access = AccessGuarded
		c.Grants = []Grant{{ID: "p1", Name: "cs566", Access: AccessRead}, {ID: "p1", Name: "cs566", Access: AccessWrite}}
		return nil
	}); err == nil || !strings.Contains(err.Error(), "two grants for p1") {
		t.Fatalf("two grants for one project: %v", err)
	}
	if !ValidAccess("guarded", true) || ValidAccess("read", true) || !ValidAccess("read", false) || ValidAccess("open", false) {
		t.Fatal("ValidAccess")
	}
}
