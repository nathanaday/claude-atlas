package vault

import (
	"encoding/json"
	"errors"
	"os"
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
	res, err := Init(root, Options{Mode: Generic}, now)
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
	if err != nil || v.Config.Schema != Schema || v.Config.Kind != Kind || v.Config.Mode != Generic || v.Config.Created != "2026-09-12" {
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
	if commits[0].SHA != res.Commit || commits[0].Trailers["atlas-operation"] != res.OperationID || !strings.HasPrefix(commits[0].Subject, "setup: initialize knowledge base fresh") {
		t.Fatalf("commit %+v", commits[0])
	}
	if _, err := Init(root, Options{}, now); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("second init should refuse: %v", err)
	}
}

func TestInitRefusesInsideAnotherRepo(t *testing.T) {
	needGit(t)
	outer := gitx.Repo{Dir: t.TempDir()}
	outer.Init()
	if _, err := Init(filepath.Join(outer.Dir, "v"), Options{Mode: Generic}, now); err == nil || !strings.Contains(err.Error(), "inside another git repository") {
		t.Fatalf("got %v", err)
	}
}

func TestIdentityFile(t *testing.T) {
	needGit(t)
	kb := filepath.Join(t.TempDir(), "ai-ml")
	if _, err := Init(kb, Options{Name: "AI and ML", Scope: " Machine learning. "}, now); err != nil {
		t.Fatal(err)
	}
	v, err := Open(kb)
	if err != nil {
		t.Fatal(err)
	}
	if v.Config.Kind != Kind || v.Config.Mode != Generic || v.Name() != "AI and ML" || v.Config.Scope != "Machine learning." || !uuidPattern.MatchString(v.Config.ID) {
		t.Fatalf("knowledge base config %+v", v.Config)
	}
	raw, _ := os.ReadFile(filepath.Join(kb, Marker))
	for _, absent := range []string{"access", "grants", "members", "tags", "mounts", "repos"} {
		if strings.Contains(string(raw), `"`+absent+`"`) {
			t.Errorf("a fresh identity file carries %q:\n%s", absent, raw)
		}
	}
	other := filepath.Join(t.TempDir(), "p")
	if _, err := Init(other, Options{}, now); err != nil {
		t.Fatal(err)
	}
	o, _ := Open(other)
	if o.Config.ID == v.Config.ID || o.Name() != "p" || o.Config.Scope != "" {
		t.Fatalf("other %+v", o.Config)
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
	// A v2 knowledge base opens as it is; a v2 project vault is refused by name.
	v2 := t.TempDir()
	os.WriteFile(filepath.Join(v2, Marker), []byte(`{"schema":"claude-atlas.vault.v2","id":"k-1","kind":"knowledge","name":"old","mode":"lyt","created":"2026-09-01","scope":"Old.","access":"guarded","grants":[{"id":"p","name":"p","access":"write"}]}`), 0o644)
	if v, err := Open(v2); err != nil || v.Config.Schema != SchemaV2 || v.Name() != "old" || v.Config.Mode != LYT || v.Config.Scope != "Old." {
		t.Fatalf("v2 knowledge base: %+v %v", v, err)
	}
	pv := t.TempDir()
	os.WriteFile(filepath.Join(pv, Marker), []byte(`{"schema":"claude-atlas.vault.v2","id":"p-1","kind":"project","name":"work","mode":"generic"}`), 0o644)
	if _, err := Open(pv); !errors.Is(err, ErrProjectVault) || !strings.Contains(err.Error(), "claude-atlas init") {
		t.Fatalf("v2 project vault: %v", err)
	}
	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, Marker), []byte(`{"schema":"`+Schema+`","id":"x","kind":"project","name":"x"}`), 0o644)
	if _, err := Open(bad); err == nil || !strings.Contains(err.Error(), "kind must be") {
		t.Fatalf("a v3 file with another kind: %v", err)
	}
	if _, err := Open(bad + "/nope"); !errors.Is(err, ErrNotVault) {
		t.Fatalf("no marker: %v", err)
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
			if _, err := Init(root, Options{}, now); err != nil {
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

func TestUpgradeRaisesAV2IdentityFile(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "older")
	if _, err := Init(root, Options{Name: "older", Scope: "Old things."}, now); err != nil {
		t.Fatal(err)
	}
	v, _ := Open(root)
	repo := gitx.Repo{Dir: root}
	v2 := `{"schema":"claude-atlas.vault.v2","id":"` + v.Config.ID + `","kind":"knowledge","name":"older","mode":"generic","created":"2026-09-12","scope":"Old things.","access":"guarded","grants":[{"id":"p","name":"p","access":"write"}]}` + "\n"
	os.WriteFile(filepath.Join(root, Marker), []byte(v2), 0o644)
	os.Remove(filepath.Join(root, "inbox", ".gitkeep"))
	os.Remove(filepath.Join(root, "inbox"))
	repo.AddAll()
	repo.Commit(CommitMessage("setup", "a v2 layout", NewOperationID("setup", now)))

	res, err := Upgrade(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Schema || strings.Join(res.Added, ",") != "inbox/.gitkeep" {
		t.Fatalf("upgrade %+v", res)
	}
	again, err := Open(root)
	if err != nil || again.Config.Schema != Schema || again.Config.ID != v.Config.ID || again.Config.Scope != "Old things." {
		t.Fatalf("raised %+v %v", again, err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, Marker))
	if strings.Contains(string(raw), "access") || strings.Contains(string(raw), "grants") {
		t.Fatalf("access and grants are gone:\n%s", raw)
	}
	if dirty, _ := repo.Dirty(); dirty {
		t.Fatal("upgrade commits what it changed")
	}
	commits, _ := repo.Log(1)
	if !strings.Contains(commits[0].Subject, "raise the identity file to "+Schema) || !strings.Contains(commits[0].Subject, "add inbox/.gitkeep") {
		t.Fatalf("commit %q", commits[0].Subject)
	}
	if second, err := Upgrade(root, now); err != nil || second.Schema || len(second.Added) != 0 {
		t.Fatalf("a second upgrade does nothing: %+v %v", second, err)
	}
}

func TestAdoptRepairsRaisesAndRefusesIdentityFiles(t *testing.T) {
	needGit(t)
	broken := filepath.Join(t.TempDir(), "broken")
	if _, err := Init(broken, Options{}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(broken, Marker), []byte("{not json"), 0o644)
	res, err := Adopt(broken, Options{}, now)
	if err != nil || !res.Repaired {
		t.Fatalf("unreadable json: %+v %v", res, err)
	}
	v, err := Open(broken)
	if err != nil || v.Config.Kind != Kind || v.Config.ID == "" {
		t.Fatalf("open after the repair: %+v %v", v, err)
	}
	commits, _ := gitx.Repo{Dir: broken}.Log(1)
	if len(commits) == 0 || !strings.Contains(commits[0].Subject, "repaired identity file") {
		t.Fatalf("commit %+v", commits)
	}

	noID := filepath.Join(t.TempDir(), "kb")
	if _, err := Init(noID, Options{Name: "Old KB"}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(noID, Marker), []byte(`{"schema":"claude-atlas.vault.v2","kind":"knowledge","name":"Old KB","mode":"lyt","created":"2026-01-02","scope":"Old."}`), 0o644)
	res, err = Adopt(noID, Options{}, now)
	if err != nil || !res.Repaired || res.FromV2 {
		t.Fatalf("a v2 file with no id is rebuilt: %+v %v", res, err)
	}
	v, err = Open(noID)
	if err != nil || v.Config.ID == "" || v.Config.Name != "Old KB" || v.Config.Created != "2026-01-02" || v.Config.Mode != LYT || v.Config.Scope != "Old." {
		t.Fatalf("repaired knowledge base %+v %v", v, err)
	}

	fromV2 := filepath.Join(t.TempDir(), "v2")
	if _, err := Init(fromV2, Options{Name: "v2"}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(fromV2, Marker), []byte(`{"schema":"claude-atlas.vault.v2","id":"kb-2","kind":"knowledge","name":"v2","mode":"generic","created":"2026-01-02","access":"open"}`), 0o644)
	res, err = Adopt(fromV2, Options{Scope: "Now scoped."}, now)
	if err != nil || !res.FromV2 || res.Repaired || res.Commit == "" {
		t.Fatalf("a v2 file is raised: %+v %v", res, err)
	}
	v, err = Open(fromV2)
	if err != nil || v.Config.Schema != Schema || v.Config.ID != "kb-2" || v.Config.Scope != "Now scoped." {
		t.Fatalf("raised %+v %v", v.Config, err)
	}
	commits, _ = gitx.Repo{Dir: fromV2}.Log(1)
	if !strings.HasPrefix(commits[0].Subject, "setup: adopt v2 vault as knowledge base v2") {
		t.Fatalf("commit %+v", commits[0])
	}

	v2project := filepath.Join(t.TempDir(), "work")
	if _, err := Init(v2project, Options{}, now); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(v2project, Marker), []byte(`{"schema":"claude-atlas.vault.v2","id":"p-1","kind":"project","name":"work","mode":"generic"}`), 0o644)
	if _, err := Adopt(v2project, Options{}, now); !errors.Is(err, ErrProjectVault) {
		t.Fatalf("a v2 project vault is refused: %v", err)
	}

	future := filepath.Join(t.TempDir(), "future")
	if _, err := Init(future, Options{}, now); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`{"schema":"claude-atlas.vault.v9","id":"1","kind":"knowledge","name":"future"}`)
	os.WriteFile(filepath.Join(future, Marker), marker, 0o644)
	if _, err := Adopt(future, Options{}, now); err == nil || !strings.Contains(err.Error(), "unsupported schema") {
		t.Fatalf("an unknown schema is refused: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(future, Marker)); string(after) != string(marker) {
		t.Fatalf("the identity file must be untouched:\n%s", after)
	}
	inner := filepath.Join(t.TempDir(), "outer", "inner")
	os.MkdirAll(filepath.Join(inner, WikiDir), 0o755)
	gitx.Repo{Dir: filepath.Dir(inner)}.Init()
	if _, err := Adopt(inner, Options{}, now); err == nil || !strings.Contains(err.Error(), "keeps its own history") {
		t.Fatalf("a folder inside another repository: %v", err)
	}
}

func TestResolveOrder(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "v")
	Init(root, Options{Mode: Generic}, now)
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
	Init(root, Options{Mode: Generic}, now)
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

func TestRoutableTypesByMode(t *testing.T) {
	needGit(t)
	if got := strings.Join(RoutableTypes(Generic), ","); got != "source,entity,concept" {
		t.Fatalf("generic: %s", got)
	}
	if got := strings.Join(RoutableTypes(LYT), ","); got != "note,moc,source,entity,concept" {
		t.Fatalf("lyt: %s", got)
	}
	kb := filepath.Join(t.TempDir(), "kb")
	Init(kb, Options{}, now)
	v, _ := Open(kb)
	if _, err := v.RouteFor("question", "Why", now); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("questions are not filed: %v", err)
	}
	r, err := v.RouteFor("concept", "Backpropagation", now)
	if err != nil || r.Path != "wiki/concepts/Backpropagation.md" {
		t.Fatalf("route %+v %v", r, err)
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

func TestInitLayout(t *testing.T) {
	needGit(t)
	kb := filepath.Join(t.TempDir(), "kb")
	if _, err := Init(kb, Options{}, now); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"wiki/tasks", "wiki/questions", "wiki/sessions", "kb", "repos", "wiki/meta/ledgers/task-ledger.json"} {
		if _, err := os.Stat(filepath.Join(kb, filepath.FromSlash(rel))); err == nil {
			t.Errorf("a knowledge base has %s", rel)
		}
	}
	for _, rel := range []string{Marker, ".gitignore", AppFile, AppearanceFile, "inbox/.gitkeep", "ideas/.gitkeep", LogPage, HotPage, IndexPage, OverviewPage, LedgerPath, ".obsidian/snippets/claude-atlas.css"} {
		if _, err := os.Stat(filepath.Join(kb, filepath.FromSlash(rel))); err != nil {
			t.Errorf("a knowledge base lacks %s", rel)
		}
	}
	index, _ := os.ReadFile(filepath.Join(kb, "wiki", "index.md"))
	if strings.Contains(string(index), "Questions") || !strings.Contains(string(index), "## Concepts") {
		t.Fatalf("index:\n%s", index)
	}
	hot, _ := os.ReadFile(filepath.Join(kb, "wiki", "hot.md"))
	if strings.Contains(string(hot), "Mount") || !strings.Contains(string(hot), "inbox/") {
		t.Fatalf("the hot cache points at the inbox and not at mounts:\n%s", hot)
	}
	ignore, _ := os.ReadFile(filepath.Join(kb, ".gitignore"))
	if strings.Contains(string(ignore), "/kb/") || strings.Contains(string(ignore), "/repos/") || !strings.Contains(string(ignore), ".vault-meta/") {
		t.Fatalf("gitignore:\n%s", ignore)
	}
	files := TemplateFiles()
	if len(files) == 0 || files[0] != ".gitignore" {
		t.Fatalf("template files %v", files)
	}
	for _, f := range files {
		if strings.HasPrefix(f, "wiki/tasks/") {
			t.Errorf("the template lists %s", f)
		}
	}
}

func TestNewNotesGoUnderTheWikiUnlessTheUserChose(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "v")
	if _, err := Init(root, Options{Mode: Generic}, now); err != nil {
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
	if _, err := Init(root, Options{}, now); err != nil {
		t.Fatal(err)
	}
	unlock, err := Lock(root)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- UpdateConfig(root, "scope", now, func(c *Config) error { c.Scope = "x"; return nil })
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
	if v.Config.Scope != "x" {
		t.Fatalf("scope %+v", v.Config)
	}
}

func TestUpdateConfigCommitsOnceAndValidates(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "p")
	if _, err := Init(root, Options{}, now); err != nil {
		t.Fatal(err)
	}
	err := UpdateConfig(root, "scope ml", now, func(c *Config) error { c.Scope = " Machine learning. "; return nil })
	if err != nil {
		t.Fatal(err)
	}
	v, _ := Open(root)
	if v.Config.Scope != "Machine learning." {
		t.Fatalf("scope %+v", v.Config)
	}
	repo := v.Repo()
	commits, _ := repo.Log(1)
	if commits[0].Subject != "setup: scope ml" || commits[0].Trailers["atlas-operation"] == "" {
		t.Fatalf("commit %+v", commits[0])
	}
	if dirty, _ := repo.Dirty(); dirty {
		t.Fatal("tree should be clean")
	}
	if err := UpdateConfig(root, "scope ml", now, func(c *Config) error { c.Scope = "Machine learning."; return nil }); err != nil {
		t.Fatal(err)
	}
	if again, _ := repo.Log(1); again[0].SHA != commits[0].SHA {
		t.Fatal("an unchanged file makes no commit")
	}
	if err := UpdateConfig(root, "bad", now, func(c *Config) error { c.Kind = "project"; return nil }); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("kind: %v", err)
	}
	if err := UpdateConfig(root, "bad", now, func(c *Config) error { c.ID = "other"; return nil }); err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("id: %v", err)
	}
	if err := UpdateConfig(root, "bad", now, func(c *Config) error { c.Name = " "; return nil }); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("name: %v", err)
	}
	if err := UpdateConfig(root, "bad", now, func(c *Config) error { c.Mode = "other"; return nil }); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("mode: %v", err)
	}
	if err := UpdateConfig(root, "bad", now, func(c *Config) error { return errors.New("no") }); err == nil {
		t.Fatal("change's error is returned")
	}
	if v, _ := Open(root); v.Config.Scope != "Machine learning." || v.Config.Kind != Kind {
		t.Fatalf("a refused update writes nothing: %+v", v.Config)
	}
	// A v2 file goes to v3 on its first edit.
	os.WriteFile(filepath.Join(root, Marker), []byte(`{"schema":"claude-atlas.vault.v2","id":"`+v.Config.ID+`","kind":"knowledge","name":"p","mode":"generic","created":"2026-09-12","access":"open"}`), 0o644)
	repo.AddAll()
	repo.Commit(CommitMessage("manual", "v2", NewOperationID("manual", now)))
	if err := UpdateConfig(root, "rename", now, func(c *Config) error { c.Name = "Renamed"; return nil }); err != nil {
		t.Fatal(err)
	}
	if v, _ := Open(root); v.Config.Schema != Schema || v.Name() != "Renamed" {
		t.Fatalf("raised on edit: %+v", v.Config)
	}
}
