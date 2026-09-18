package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// fixture lists three knowledge bases, a v1 vault, a v2 project vault, and an
// unreadable identity file in the config, and leaves one knowledge base out of it; and
// three projects: two on ai-ml, one that names a knowledge base the machine does not
// have.
func fixture(t *testing.T) (*home.Config, map[string]*vault.Vault, map[string]*project.Project) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{}
	vs := map[string]*vault.Vault{}
	mk := func(rel string, opts vault.Options) string {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := vault.Init(path, opts, now); err != nil {
			t.Fatal(err)
		}
		v, _ := vault.Open(path)
		vs[opts.Name] = v
		return path
	}
	cfg.Knowledge = []string{
		mk("Vaults/ai-ml", vault.Options{Name: "ai-ml", Scope: "Machine learning."}),
		mk("Vaults/deep/nested/robotics", vault.Options{Name: "robotics"}),
		mk("Elsewhere/side", vault.Options{Name: "side"}),
	}
	mk("Vaults/unlisted", vault.Options{Name: "unlisted"})
	old := filepath.Join(root, "Vaults", "old")
	os.MkdirAll(filepath.Join(old, "wiki"), 0o755)
	os.WriteFile(filepath.Join(old, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	v2project := filepath.Join(root, "Vaults", "projects", "cs566")
	os.MkdirAll(filepath.Join(v2project, "wiki"), 0o755)
	os.WriteFile(filepath.Join(v2project, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v2","id":"p-old","kind":"project","name":"cs566","mode":"generic"}`), 0o644)
	bad := filepath.Join(root, "Vaults", "bad")
	os.MkdirAll(bad, 0o755)
	os.WriteFile(filepath.Join(bad, vault.Marker), []byte(`{not json`), 0o644)
	cfg.Knowledge = append(cfg.Knowledge, old, v2project, bad)

	ps := map[string]*project.Project{}
	mkp := func(rel string, opts project.Options) {
		work := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(work, 0o755)
		p, _, err := project.Init(work, opts, now)
		if err != nil {
			t.Fatal(err)
		}
		ps[p.Name()] = p
		cfg.Projects = append(cfg.Projects, work)
	}
	aiml := &project.Knowledge{ID: vs["ai-ml"].Config.ID, Name: "ai-ml"}
	mkp("Code/webapp", project.Options{Description: "The web app.", Knowledge: aiml})
	mkp("Code/firmware", project.Options{Knowledge: aiml})
	mkp("Docs/thesis", project.Options{Knowledge: &project.Knowledge{ID: "gone-0000", Name: "papers"}})
	return cfg, vs, ps
}

func TestScanFindsEveryEntryAndSortsThem(t *testing.T) {
	cfg, vs, ps := fixture(t)
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ix.Entries {
		if e.Error != "" {
			continue
		}
		names = append(names, string(e.Kind)+":"+e.Name)
	}
	if got := strings.Join(names, ","); got != "knowledge:ai-ml,knowledge:robotics,knowledge:side,project:firmware,project:thesis,project:webapp" {
		t.Fatalf("entries %s", got)
	}
	if len(ix.Problems) != 3 || len(ix.Entries) != 9 {
		t.Fatalf("problems %+v entries %d", ix.Problems, len(ix.Entries))
	}
	reasons := map[string]string{}
	for _, e := range ix.Entries {
		if e.Error != "" {
			reasons[filepath.Base(e.Path)] = e.Reason
		}
	}
	if reasons["bad"] != ReasonUnreadable || reasons["old"] != ReasonV1 || reasons["cs566"] != ReasonV2Project {
		t.Fatalf("reasons %v", reasons)
	}
	for _, e := range ix.Entries {
		if e.Reason == ReasonV2Project && (!strings.Contains(e.Error, "claude-atlas init") || e.Rel() != "problems/cs566") {
			t.Fatalf("v2 project entry %+v", e)
		}
	}
	if _, err := ix.Find("old", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find old: %v", err)
	}
	if e := ix.ByPath(vs["unlisted"].Root); e != nil {
		t.Fatalf("the scan searches no folder, so a knowledge base the config does not list is not found: %+v", e)
	}
	for _, e := range append(ix.Projects(), ix.Knowledge()...) {
		if e.Error != "" {
			t.Fatalf("a listing contains an error entry: %+v", e)
		}
	}
	if len(ix.Projects()) != 3 || len(ix.Knowledge()) != 3 {
		t.Fatalf("projects %d knowledge %d", len(ix.Projects()), len(ix.Knowledge()))
	}
	aiml := ix.ByID(vs["ai-ml"].Config.ID)
	if aiml == nil || aiml.Path != vs["ai-ml"].Root || aiml.Scope != "Machine learning." || aiml.Mode != vault.Generic || aiml.Kind != Knowledge {
		t.Fatalf("by id %+v", aiml)
	}
	if e := ix.ByPath(vs["side"].Root); e == nil || e.Name != "side" {
		t.Fatalf("by path %+v", e)
	}
	webapp := ix.ByPath(ps["webapp"].Root)
	if webapp == nil || webapp.Kind != Project || webapp.ID != ps["webapp"].Config.ID || webapp.Description != "The web app." || webapp.Created != "2026-09-15" {
		t.Fatalf("project entry %+v", webapp)
	}
	if webapp.Atlas() != filepath.Join(webapp.Path, "atlas") || aiml.Wiki() != filepath.Join(aiml.Path, "wiki") {
		t.Fatal("Atlas and Wiki")
	}
}

func TestScanResolvesKnowledgeAndProjects(t *testing.T) {
	cfg, vs, ps := fixture(t)
	ix, _ := Scan(cfg)
	aiml := ix.ByID(vs["ai-ml"].Config.ID)
	var users []string
	for _, r := range aiml.Projects {
		users = append(users, r.Name+"@"+r.Path)
	}
	if strings.Join(users, ",") != "firmware@"+ps["firmware"].Root+",webapp@"+ps["webapp"].Root {
		t.Fatalf("projects of ai-ml %v", users)
	}
	if len(ix.ByID(vs["robotics"].Config.ID).Projects) != 0 {
		t.Fatal("robotics has no projects")
	}
	webapp := ix.ByPath(ps["webapp"].Root)
	if webapp.Knowledge == nil || webapp.Knowledge.ID != aiml.ID || webapp.Knowledge.Name != "ai-ml" || webapp.Knowledge.Path != aiml.Path || webapp.Knowledge.Error != "" {
		t.Fatalf("webapp's knowledge base %+v", webapp.Knowledge)
	}
	if webapp.KnowledgePath() != aiml.Path || webapp.Rel() != "projects/ai-ml/webapp" {
		t.Fatalf("path %q rel %q", webapp.KnowledgePath(), webapp.Rel())
	}
	thesis := ix.ByPath(ps["thesis"].Root)
	if thesis.Knowledge == nil || thesis.Knowledge.Name != "papers" || !strings.Contains(thesis.Knowledge.Error, "gone-0000") || thesis.KnowledgePath() != "" || thesis.Rel() != "projects/thesis" {
		t.Fatalf("thesis's knowledge base %+v rel %q", thesis.Knowledge, thesis.Rel())
	}
	of := ix.ProjectsOf(aiml.ID)
	if len(of) != 2 || of[0].Name != "firmware" || of[1].Name != "webapp" {
		t.Fatalf("ProjectsOf %+v", of)
	}
	if aiml.Rel() != "knowledge/ai-ml" || Knowledge.Noun() != "knowledge base" || Project.Noun() != "project" {
		t.Fatal("rel and nouns")
	}
	// A project without a knowledge base.
	none := filepath.Join(t.TempDir(), "solo")
	os.MkdirAll(none, 0o755)
	if _, _, err := project.Init(none, project.Options{}, now); err != nil {
		t.Fatal(err)
	}
	cfg.Projects = append(cfg.Projects, none)
	ix, _ = Scan(cfg)
	if e := ix.ByPath(none); e == nil || e.Knowledge != nil || e.KnowledgePath() != "" {
		t.Fatalf("solo %+v", e)
	}
}

func TestFindByNameIDPathAndKind(t *testing.T) {
	cfg, vs, ps := fixture(t)
	ix, _ := Scan(cfg)
	if e, err := ix.Find("WEBAPP", ""); err != nil || e.Name != "webapp" {
		t.Fatalf("find by name %+v %v", e, err)
	}
	if e, err := ix.Find(vs["robotics"].Config.ID[:8], Knowledge); err != nil || e.Name != "robotics" {
		t.Fatalf("find by id prefix %+v %v", e, err)
	}
	if e, err := ix.Find(ps["firmware"].Root, Project); err != nil || e.Name != "firmware" {
		t.Fatalf("find by path %+v %v", e, err)
	}
	if _, err := ix.Find("webapp", Knowledge); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a project is not a knowledge base: %v", err)
	}
	if _, err := ix.Find(ps["firmware"].Root, Knowledge); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a project's path is not a knowledge base: %v", err)
	}
	if _, err := ix.Find("nope", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find missing: %v", err)
	}
	// A knowledge base and a project may share a name; the kind tells them apart.
	twin := filepath.Join(t.TempDir(), "ai-ml")
	os.MkdirAll(twin, 0o755)
	if _, _, err := project.Init(twin, project.Options{}, now); err != nil {
		t.Fatal(err)
	}
	cfg.Projects = append(cfg.Projects, twin)
	ix, _ = Scan(cfg)
	if _, err := ix.Find("ai-ml", ""); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("two entries named ai-ml: %v", err)
	}
	if e, err := ix.Find("ai-ml", Project); err != nil || e.Path != twin {
		t.Fatalf("the project named ai-ml: %+v %v", e, err)
	}
	if e, err := ix.Find("ai-ml", Knowledge); err != nil || e.Path != vs["ai-ml"].Root {
		t.Fatalf("the knowledge base named ai-ml: %+v %v", e, err)
	}
}

func TestFindAmbiguousIDPrefix(t *testing.T) {
	ix := &Index{Entries: []Entry{
		{ID: "abcdefgh1111", Kind: Project, Name: "one", Path: "/vaults/one"},
		{ID: "abcdefgh2222", Kind: Project, Name: "two", Path: "/vaults/two"},
	}}
	if _, err := ix.Find("abcdefgh", ""); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("find ambiguous id prefix: %v", err)
	}
}

func TestStateFileRoundTrips(t *testing.T) {
	cfg, _, _ := fixture(t)
	ix, _ := Scan(cfg)
	dir := filepath.Join(t.TempDir(), "state")
	if _, _, err := Read(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read before write: %v", err)
	}
	four := 4
	ix.Entries[0].State = &State{GeneratedAt: "2026-09-15T12:00:00Z", OK: true, Pages: &four, Heat: "new", OpenThreads: []string{}}
	if err := Write(dir, ix.Entries, "2026-09-15T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	entries, generated, err := Read(dir)
	if err != nil || generated != "2026-09-15T12:00:00Z" || len(entries) != len(ix.Entries) || entries[0].State == nil || *entries[0].State.Pages != 4 {
		t.Fatalf("round trip %v %s %+v", err, generated, entries)
	}
	data, _ := os.ReadFile(File(dir))
	if !strings.Contains(string(data), `"schema": "claude-atlas.registry.v2"`) {
		t.Fatalf("file:\n%s", data)
	}
	os.WriteFile(File(dir), []byte(`{"schema":"claude-atlas.registry.v1","entries":[]}`), 0o644)
	if _, _, err := Read(dir); err == nil || !strings.Contains(err.Error(), "claude-atlas.registry.v1") {
		t.Fatalf("read another schema: %v", err)
	}
}

// A registered path whose folder is gone, and a registered work folder with no project
// in it, are entries like every other unreadable one, so list, doctor, and forget see
// them.
func TestScanMakesMissingEntries(t *testing.T) {
	cfg, _, _ := fixture(t)
	gone := filepath.Join(t.TempDir(), "gone")
	cfg.Knowledge = append(cfg.Knowledge, gone)
	goneWork := filepath.Join(t.TempDir(), "gone-work")
	plain := filepath.Join(t.TempDir(), "plain")
	os.MkdirAll(plain, 0o755)
	broken := filepath.Join(t.TempDir(), "broken")
	os.MkdirAll(filepath.Join(broken, project.Dir), 0o755)
	os.WriteFile(project.MarkerPath(broken), []byte("{not json"), 0o644)
	future := filepath.Join(t.TempDir(), "future")
	os.MkdirAll(filepath.Join(future, project.Dir), 0o755)
	os.WriteFile(project.MarkerPath(future), []byte(`{"schema":"claude-atlas.project.v9","id":"x"}`), 0o644)
	noID := filepath.Join(t.TempDir(), "noid")
	os.MkdirAll(filepath.Join(noID, project.Dir), 0o755)
	os.WriteFile(project.MarkerPath(noID), []byte(`{"schema":"`+project.Schema+`","name":"x"}`), 0o644)
	cfg.Projects = append(cfg.Projects, goneWork, plain, broken, future, noID)
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{gone: ReasonMissing, goneWork: ReasonMissing, plain: ReasonNotProject, broken: ReasonUnreadable, future: ReasonSchema, noID: ReasonUnreadable}
	for path, reason := range want {
		e := ix.ByPath(path)
		if e == nil || e.Reason != reason || e.Error == "" {
			t.Errorf("%s: %+v, want reason %s", path, e, reason)
			continue
		}
		matched := false
		for _, p := range ix.Problems {
			if p.Path == path && p.Reason == e.Error {
				matched = true
			}
		}
		if !matched {
			t.Errorf("%s keeps its problem with the same reason: %+v", path, ix.Problems)
		}
	}
	if e := ix.ByPath(gone); !strings.Contains(e.Error, "remove") {
		t.Fatalf("a gone knowledge base names remove: %s", e.Error)
	}
	if e := ix.ByPath(goneWork); !strings.Contains(e.Error, "forget") {
		t.Fatalf("a gone project names forget: %s", e.Error)
	}
	if e := ix.ByPath(plain); !strings.Contains(e.Error, "claude-atlas init") {
		t.Fatalf("a plain folder names init: %s", e.Error)
	}
	if _, err := ix.Find(gone, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find a missing vault: %v", err)
	}
}

func TestScanNamesAListedFolderThatIsNoKnowledgeBase(t *testing.T) {
	plain := t.TempDir()
	ix, err := Scan(&home.Config{Knowledge: []string{plain}})
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByPath(plain)
	if e == nil || e.Reason != ReasonNotVault || !strings.Contains(e.Error, "claude-atlas adopt") || !strings.Contains(e.Error, "claude-atlas remove") {
		t.Fatalf("a listed folder with no identity file is an entry that says what to do: %+v", e)
	}
}

// TestByPathFollowsASymlinkedAncestor proves a session that reached a folder through a
// symlinked parent still finds its entry. On macOS /tmp is such a link, and Claude Code
// hands the hooks and the server the resolved path.
func TestByPathFollowsASymlinkedAncestor(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "Code")
	if err := os.MkdirAll(filepath.Join(real, "webapp"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	ix := &Index{Entries: []Entry{
		{ID: "abcdefgh1111", Kind: Project, Name: "webapp", Path: filepath.Join(real, "webapp")},
	}}
	e := ix.ByPath(filepath.Join(link, "webapp"))
	if e == nil || e.Name != "webapp" {
		t.Fatalf("ByPath through a symlinked parent: %+v", e)
	}
	linked := &Index{Entries: []Entry{
		{ID: "abcdefgh1111", Kind: Project, Name: "webapp", Path: filepath.Join(link, "webapp")},
	}}
	if linked.ByPath(filepath.Join(real, "webapp")) == nil {
		t.Fatal("ByPath with a resolved path found nothing")
	}
	if ix.ByPath(filepath.Join(link, "other")) != nil {
		t.Fatal("ByPath matched a path that is no entry")
	}
}

// An entry the config lists twice, once under a spelling that reaches it through a
// symlink, is one entry.
func TestScanDedupesAnEntryRegisteredThroughASymlink(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{}
	real := filepath.Join(root, "Vaults", "ai-ml")
	if _, err := vault.Init(real, vault.Options{Name: "ai-ml"}, now); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "ai-ml-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	cfg.Knowledge = []string{real, link}
	work := filepath.Join(root, "work")
	os.MkdirAll(work, 0o755)
	if _, _, err := project.Init(work, project.Options{}, now); err != nil {
		t.Fatal(err)
	}
	workLink := filepath.Join(root, "work-link")
	if err := os.Symlink(work, workLink); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	cfg.Projects = []string{work, workLink}
	ix, err := Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Entries) != 2 {
		t.Fatalf("two entries, got %d: %+v", len(ix.Entries), ix.Entries)
	}
	if ix.Entries[0].Path != real || ix.Entries[1].Path != work {
		t.Fatalf("the entries keep the first spelling: %s %s", ix.Entries[0].Path, ix.Entries[1].Path)
	}
	for _, path := range []string{real, link} {
		if e := ix.ByPath(path); e == nil || e.Name != "ai-ml" {
			t.Fatalf("ByPath(%s): %+v", path, e)
		}
	}
}

func TestDescriptionSummaryAndUnfinished(t *testing.T) {
	cases := map[Description]string{
		{Page: "wiki/entities/x.md"}:                                   "described in wiki/entities/x.md",
		{Page: "wiki/entities/x.md", Commit: "abcdef0123", Behind: -1}: "described in wiki/entities/x.md at abcdef0, not in the repository's history",
		{Page: "wiki/entities/x.md", Commit: "abcdef0123", Behind: 0}:  "described in wiki/entities/x.md at abcdef0, current",
		{Page: "wiki/entities/x.md", Commit: "abcdef0123", Behind: 1}:  "described in wiki/entities/x.md at abcdef0, 1 commit behind",
		{Page: "wiki/entities/x.md", Commit: "abcdef0123", Behind: 12}: "described in wiki/entities/x.md at abcdef0, 12 commits behind",
	}
	for d, want := range cases {
		if got := d.Summary(); got != want {
			t.Errorf("%+v: %q", d, got)
		}
	}
	one, two := 1, 2
	u := Unfinished{Stubs: &one, DeadLinks: &two}
	if u.Text() != "1 stubs · 2 dead links" || *u.Total() != 3 || (Unfinished{}).Total() != nil {
		t.Fatalf("unfinished %q %v", u.Text(), u.Total())
	}
}
