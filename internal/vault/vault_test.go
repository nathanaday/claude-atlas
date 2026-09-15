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
	if !strings.Contains(string(ignore), "/kb/") {
		t.Fatalf("a project ignores its mounts:\n%s", ignore)
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
