package vault

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

func TestInitInCreatesAProjectInsideARepository(t *testing.T) {
	needGit(t)
	repoRoot := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repoRoot, 0o755)
	host := gitx.Repo{Dir: repoRoot}
	if err := host.Init(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n"), 0o644)
	res, err := InitIn(repoRoot, Options{Kind: Project, Name: "Notes"}, now)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repoRoot, InRepoDir)
	if HostRepo(root) != repoRoot {
		t.Fatalf("HostRepo %q", HostRepo(root))
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Fatal("the vault must not have its own .git")
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if v.Repo().Dir != repoRoot || v.Repo().Prefix != "atlas/" || v.Name() != "Notes" || v.Config.Kind != Project {
		t.Fatalf("%+v %+v", v.Repo(), v.Config)
	}
	changed, err := host.ChangedPaths(res.Commit)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range changed {
		if !strings.HasPrefix(p, "atlas/") {
			t.Fatalf("the setup commit touched %s", p)
		}
	}
	if st, _ := host.Status(); len(st) != 1 || st[0].Path != "main.go" {
		t.Fatalf("main.go must stay uncommitted: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal("atlas/.gitignore must exist")
	}
}

func TestInitInRefusals(t *testing.T) {
	needGit(t)
	plain := filepath.Join(t.TempDir(), "plain")
	os.MkdirAll(plain, 0o755)
	if _, err := InitIn(plain, Options{Kind: Project}, now); err == nil || !strings.Contains(err.Error(), "not the top level of a git repository") {
		t.Fatalf("plain folder: %v", err)
	}
	repoRoot := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repoRoot, 0o755)
	gitx.Repo{Dir: repoRoot}.Init()
	if _, err := InitIn(repoRoot, Options{Kind: Knowledge}, now); err == nil || !strings.Contains(err.Error(), "only a project") {
		t.Fatalf("knowledge base: %v", err)
	}
	if _, err := InitIn(repoRoot, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := InitIn(repoRoot, Options{Kind: Project}, now); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("twice: %v", err)
	}
	if HostRepo(filepath.Join(t.TempDir(), "atlas")) != "" {
		t.Fatal("a folder named atlas outside a repository is not inside one")
	}
	ignoring := filepath.Join(t.TempDir(), "ignoring")
	os.MkdirAll(ignoring, 0o755)
	gitx.Repo{Dir: ignoring}.Init()
	os.WriteFile(filepath.Join(ignoring, ".gitignore"), []byte("atlas/\n"), 0o644)
	if _, err := InitIn(ignoring, Options{Kind: Project}, now); err == nil || !strings.Contains(err.Error(), "ignores atlas/") {
		t.Fatalf("an ignored folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ignoring, InRepoDir)); err == nil {
		t.Fatal("a refusal must write nothing")
	}
	// A repository inside a vault: the project would sit in two vaults at once.
	outer := filepath.Join(t.TempDir(), "outer")
	if _, err := Init(outer, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(outer, "code")
	os.MkdirAll(inner, 0o755)
	gitx.Repo{Dir: inner}.Init()
	if _, err := InitIn(inner, Options{Kind: Project}, now); err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Fatalf("a repository inside a vault: %v", err)
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

// Obsidian's settings are the user's file. An empty one holds nothing to lose, so the
// upgrade merges its keys into it; a file that holds something else is the user's to fix,
// and the upgrade names it and stops.
func TestUpgradeFillsAnEmptySettingsFileAndRefusesANonObject(t *testing.T) {
	needGit(t)
	cases := []struct {
		name, text string
		refuse     bool
	}{
		{"empty", "", false},
		{"whitespace", "  \n", false},
		{"array", "[]", true},
		{"null", "null", true},
		{"truncated", "{ \"newFileLocation\": \n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "v")
			if _, err := Init(root, Options{Kind: Project}, now); err != nil {
				t.Fatal(err)
			}
			// A .gitignore missing a template line is what an upgrade would merge first,
			// before it reads the settings; a refusal must leave it alone too.
			ignore := filepath.Join(root, ".gitignore")
			if err := os.WriteFile(ignore, []byte("# claude-atlas runtime state\n.vault-meta/\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			repo := gitx.Repo{Dir: root}
			if err := repo.AddAll(); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Commit(CommitMessage("manual", "the user's ignore file", NewOperationID("manual", now))); err != nil {
				t.Fatal(err)
			}
			app := filepath.Join(root, filepath.FromSlash(AppFile))
			if err := os.WriteFile(app, []byte(c.text), 0o644); err != nil {
				t.Fatal(err)
			}
			before, err := repo.Status()
			if err != nil {
				t.Fatal(err)
			}
			_, err = Upgrade(root, now)
			if c.refuse {
				if err == nil || !strings.Contains(err.Error(), AppFile) || !strings.Contains(err.Error(), "is not a JSON object") {
					t.Fatalf("upgrade: %v", err)
				}
				if data, _ := os.ReadFile(app); string(data) != c.text {
					t.Fatalf("the file stays as the user left it: %q", data)
				}
				after, err := repo.Status()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("a refused upgrade wrote something:\nbefore %+v\nafter  %+v", before, after)
				}
				return
			}
			if err != nil {
				t.Fatalf("upgrade: %v", err)
			}
			var settings map[string]any
			data, readErr := os.ReadFile(app)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if err := json.Unmarshal(data, &settings); err != nil {
				t.Fatalf("settings %q: %v", data, err)
			}
			if settings["newFileLocation"] != "folder" || settings["newFileFolderPath"] != WikiDir {
				t.Fatalf("the merged keys: %+v", settings)
			}
		})
	}
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

func TestAdoptAcceptsAClonedProjectInsideARepository(t *testing.T) {
	needGit(t)
	repoRoot := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repoRoot, 0o755)
	gitx.Repo{Dir: repoRoot}.Init()
	if _, err := InitIn(repoRoot, Options{Kind: Project, Name: "Notes"}, now); err != nil {
		t.Fatal(err)
	}
	clone := filepath.Join(t.TempDir(), "clone")
	if err := (gitx.Repo{Dir: clone}).Clone(repoRoot); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(clone, InRepoDir)
	res, err := Adopt(root, Options{Kind: Project}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != Project || res.FromV1 {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Fatal("adopt must not git init inside the clone")
	}
	v, _ := Open(root)
	if v.Repo().Dir != clone || v.Name() != "Notes" {
		t.Fatalf("%+v", v.Repo())
	}
	// A folder named atlas that is not already a project is not a clone of one: it is a
	// vault someone put inside a repository, and it keeps its own history.
	other := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(filepath.Join(other, InRepoDir, WikiDir), 0o755)
	gitx.Repo{Dir: other}.Init()
	_, err = Adopt(filepath.Join(other, InRepoDir), Options{Kind: Project}, now)
	if err == nil || !strings.Contains(err.Error(), "keeps its own history") || !strings.Contains(err.Error(), "--in REPO") {
		t.Fatalf("a plain folder named atlas inside a repository: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, InRepoDir, Marker)); err == nil {
		t.Fatal("a refusal must write nothing")
	}

	// Only a project lives inside a repository, whatever the identity file says.
	kb := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(filepath.Join(kb, InRepoDir, WikiDir), 0o755)
	gitx.Repo{Dir: kb}.Init()
	marker := []byte(`{"schema":"` + Schema + `","id":"kb-1","kind":"knowledge","name":"notes","mode":"generic"}`)
	os.WriteFile(filepath.Join(kb, InRepoDir, Marker), marker, 0o644)
	if _, err := Adopt(filepath.Join(kb, InRepoDir), Options{}, now); err == nil || !strings.Contains(err.Error(), "only a project lives inside a repository") {
		t.Fatalf("a knowledge base inside a repository: %v", err)
	}

	// A host that ignores the folder is refused by name: git would record nothing.
	ignored := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(filepath.Join(ignored, InRepoDir, WikiDir), 0o755)
	gitx.Repo{Dir: ignored}.Init()
	os.WriteFile(filepath.Join(ignored, ".gitignore"), []byte(InRepoDir+"/\n"), 0o644)
	project := []byte(`{"schema":"` + Schema + `","id":"p-1","kind":"project","name":"notes","mode":"generic"}`)
	os.WriteFile(filepath.Join(ignored, InRepoDir, Marker), project, 0o644)
	if _, err := Adopt(filepath.Join(ignored, InRepoDir), Options{}, now); err == nil || !strings.Contains(err.Error(), "remove that rule") {
		t.Fatalf("an ignored folder: %v", err)
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

func TestFindPageByStemAndAlias(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "wiki/concepts"), 0o755)
	page := "---\ntitle: Backpropagation\ntype: concept\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\naliases:\n  - backprop\n  - \"Back Propagation\"\n---\n\n# Backpropagation\n"
	if err := os.WriteFile(filepath.Join(root, "wiki/concepts/Backpropagation.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	// This name sorts before Backpropagation.md, so the walk reaches it first; a
	// dangling symlink must not stop FindPage from reaching the real page.
	if err := os.Symlink("does-not-exist.md", filepath.Join(root, "wiki/concepts/0gone.md")); err != nil {
		t.Fatal(err)
	}
	m, err := FindPage(root, "backpropagation")
	if err != nil || m == nil || m.Path != "wiki/concepts/Backpropagation.md" || m.ByAlias != "" {
		t.Fatalf("stem match past an unreadable file: %+v %v", m, err)
	}
	m, err = FindPage(root, "Back propagation")
	if err != nil || m == nil || m.Path != "wiki/concepts/Backpropagation.md" || m.ByAlias != "Back Propagation" {
		t.Fatalf("alias match: %+v %v", m, err)
	}
	m, err = FindPage(root, "nope")
	if err != nil || m != nil {
		t.Fatalf("no match: %+v %v", m, err)
	}

	// A title with characters SanitizeTitle changes still stem-matches the sanitized
	// file name RouteFor would have created for it.
	stem := SanitizeTitle("A/B: C")
	sanitizedPage := "---\ntitle: " + stem + "\ntype: concept\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# " + stem + "\n"
	if err := os.WriteFile(filepath.Join(root, "wiki/concepts", stem+".md"), []byte(sanitizedPage), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err = FindPage(root, "A/B: C")
	if err != nil || m == nil || m.Path != "wiki/concepts/"+stem+".md" {
		t.Fatalf("sanitized stem match: %+v %v", m, err)
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

func TestLockIsExclusiveAndUpdateConfigTakesIt(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "p")
	if _, err := Init(root, Options{Kind: Project}, now); err != nil {
		t.Fatal(err)
	}
	unlock, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- UpdateConfig(root, "tag", now, func(c *Config) error { c.Tags = []string{"x"}; return nil })
	}()
	select {
	case err := <-done:
		t.Fatalf("UpdateConfig ran while the vault was locked: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	v, _ := Open(root)
	if len(v.Config.Tags) != 1 {
		t.Fatalf("tags %+v", v.Config.Tags)
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

// TestUpdateConfigInsideARepositoryCommitsOnlyTheIdentityFile covers the vault's second
// rule: an edit of the project's own facts must leave what the user is typing alone, in a
// repository as in a vault of its own.
func TestUpdateConfigInsideARepositoryCommitsOnlyTheIdentityFile(t *testing.T) {
	needGit(t)
	repoRoot := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(repoRoot, 0o755)
	host := gitx.Repo{Dir: repoRoot}
	if err := host.Init(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main\n"), 0o644)
	if err := host.AddAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Commit("code"); err != nil {
		t.Fatal(err)
	}
	if _, err := InitIn(repoRoot, Options{Kind: Project, Name: "Notes"}, now); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repoRoot, InRepoDir)
	hot := filepath.Join(root, filepath.FromSlash(HotPage))
	if err := os.WriteFile(hot, []byte("# hot\n\nhalf a sentence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repoRoot, "main.go"), []byte("package main // typing\n"), 0o644)

	if err := UpdateConfig(root, "edit name", now, func(c *Config) error { c.Name = "Renamed"; return nil }); err != nil {
		t.Fatal(err)
	}
	v, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	repo := v.Repo()
	commits, err := repo.Log(1)
	if err != nil || len(commits) != 1 || commits[0].Subject != "setup: edit name" {
		t.Fatalf("commit %+v %v", commits, err)
	}
	changed, err := repo.ChangedPaths(commits[0].SHA)
	if err != nil || len(changed) != 1 || changed[0] != Marker {
		t.Fatalf("the setup commit recorded %v %v", changed, err)
	}
	st, err := repo.Status()
	if err != nil || len(st) != 1 || st[0].Path != HotPage || st[0].Code != " M" {
		t.Fatalf("the hand edit must still be uncommitted: %+v %v", st, err)
	}
	if data, _ := os.ReadFile(hot); string(data) != "# hot\n\nhalf a sentence\n" {
		t.Fatalf("the page on disk: %q", data)
	}
	if out, _ := host.Status(); len(out) != 2 {
		t.Fatalf("the code must stay as the user left it: %+v", out)
	}
}

func TestInitInWaitsOutAMergeInTheHost(t *testing.T) {
	needGit(t)
	host := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(host, 0o755)
	git := func(args ...string) error {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = host
		return cmd.Run()
	}
	whole := gitx.Repo{Dir: host}
	if err := whole.Init(); err != nil {
		t.Fatal(err)
	}
	write := func(text string) { os.WriteFile(filepath.Join(host, "code.txt"), []byte(text), 0o644) }
	write("base\n")
	whole.AddAll()
	if _, err := whole.Commit("base"); err != nil {
		t.Fatal(err)
	}
	if err := git("checkout", "-q", "-b", "other"); err != nil {
		t.Fatal(err)
	}
	write("other\n")
	whole.AddAll()
	whole.Commit("other")
	if err := git("checkout", "-q", "main"); err != nil {
		t.Fatal(err)
	}
	write("main\n")
	whole.AddAll()
	whole.Commit("main")
	if err := git("merge", "other"); err == nil {
		t.Fatal("the merge must conflict")
	}
	if _, err := InitIn(host, Options{Kind: Project, Name: "Notes"}, now); err == nil || !strings.Contains(err.Error(), "merge") {
		t.Fatalf("InitIn during a merge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(host, InRepoDir)); err == nil {
		t.Fatal("InitIn must write nothing during a merge")
	}
	if err := git("merge", "--abort"); err != nil {
		t.Fatal(err)
	}
	if _, err := InitIn(host, Options{Kind: Project, Name: "Notes"}, now); err != nil {
		t.Fatal(err)
	}
}
