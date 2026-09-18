package refresh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func fakeVault(t *testing.T, log, hot string, pages map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	os.MkdirAll(filepath.Join(root, "wiki"), 0o755)
	os.WriteFile(filepath.Join(root, ".claude-atlas.json"), []byte(`{"schema":"claude-atlas.vault.v3","id":"00000000-0000-4000-8000-000000000001","kind":"knowledge","name":"v","mode":"generic"}`), 0o644)
	os.WriteFile(filepath.Join(root, "wiki", "log.md"), []byte(log), 0o644)
	os.WriteFile(filepath.Join(root, "wiki", "hot.md"), []byte(hot), 0o644)
	for name, text := range pages {
		os.WriteFile(filepath.Join(root, "wiki", name), []byte(text), 0o644)
	}
	return root
}

func TestHeat(t *testing.T) {
	for days, want := range map[int]string{0: "hot", 6: "hot", 7: "warm", 29: "warm", 30: "cold"} {
		d := days
		if got := Heat(&d, nil, 7); got != want {
			t.Errorf("%d days: got %s want %s", days, got, want)
		}
	}
	if Heat(nil, p(0), 7) != "" {
		t.Error("nil idleness should be unknown even when new")
	}
	if Heat(p(0), p(3), 7) != "new" || Heat(p(40), p(6), 7) != "new" {
		t.Error("a vault under 7 days old is new whatever its idleness")
	}
	if Heat(p(0), p(7), 7) != "hot" {
		t.Error("7 days old is no longer new")
	}
	if Heat(p(0), p(3), 1) != "hot" || Heat(p(0), p(0), 1) != "new" || Heat(p(0), p(0), 0) != "hot" {
		t.Error("the threshold is configurable; 0 turns new off")
	}
}

func p(v int) *int { return &v }

func TestCreatedDateComesFromTheIndexPage(t *testing.T) {
	root := fakeVault(t, "", "", map[string]string{
		"index.md":    "---\ntitle: Wiki Index\ncreated: 2026-09-01\nupdated: 2026-09-10\n---\n",
		"overview.md": "---\ncreated: 2020-01-01\n---\n",
	})
	got, ok := CreatedDate(root)
	if !ok || got.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := CreatedDate(fakeVault(t, "", "", nil)); ok {
		t.Fatal("no created field should report none")
	}
}

func TestNewestLogDate(t *testing.T) {
	root := fakeVault(t, "# Log\n\n## 2026-09-04 — a\n\n## 2026-09-10 — b\n\n## not-a-date\n", "", nil)
	got, ok := NewestLogDate(root)
	if !ok || got.Format("2006-01-02") != "2026-09-10" {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := NewestLogDate(filepath.Join(t.TempDir(), "missing")); ok {
		t.Fatal("missing vault should report no date")
	}
}

func TestActiveThreadsJoinsContinuationLines(t *testing.T) {
	root := fakeVault(t, "", "# R\n\n## Recent Changes\n\n- ignored\n\n## Active Threads\n\n- First thread\n  continues here.\n- Second\n\n## Later\n\n- no\n", nil)
	got := ActiveThreads(root)
	if strings.Join(got, "|") != "First thread continues here.|Second" {
		t.Fatalf("got %v", got)
	}
}

func TestPlainTextStripsWikilinks(t *testing.T) {
	if got := PlainText("See [[Spec]] and [[Long Name|alias]]."); got != "See Spec and alias." {
		t.Fatalf("got %q", got)
	}
}

func TestDeriveTakesLaterOfLogAndMtime(t *testing.T) {
	root := fakeVault(t, "## 2026-08-01 — old\n", "", nil)
	os.MkdirAll(filepath.Join(root, "inbox"), 0o755)
	os.WriteFile(filepath.Join(root, "inbox", "paper.md"), []byte("x"), 0o644)
	e := registry.Entry{Kind: registry.Knowledge, Path: root}
	state := Derive(e, time.Now(), "t", 7)
	if state.LastOperation != "2026-08-01" || state.LastTouched != time.Now().Format("2006-01-02") {
		t.Fatalf("got %+v", state)
	}
	if state.DaysIdle == nil || *state.DaysIdle != 0 || state.Heat != "hot" {
		t.Fatalf("idle %v heat %s", state.DaysIdle, state.Heat)
	}
	if !state.OK || state.Pages == nil || *state.Pages != 2 || state.Inbox == nil || *state.Inbox != 1 || state.Tasks != nil {
		t.Fatalf("got %+v", state)
	}
	if got := Derive(registry.Entry{Path: root, Error: "v1 vault"}, time.Now(), "t", 7); got.OK || got.Error != "v1 vault" {
		t.Fatalf("an error entry: %+v", got)
	}
}

// projectFixture makes a knowledge base and a project on it, the work a git repository,
// and returns the config and the project's scanned entry.
func projectFixture(t *testing.T, now time.Time) (*home.Config, registry.Entry, *project.Project) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{Schema: home.ConfigSchema, Heat: &home.HeatConfig{NewDays: 7}}
	kb := filepath.Join(root, "Vaults", "ai-ml")
	if _, err := vault.Init(kb, vault.Options{Name: "ai-ml"}, now); err != nil {
		t.Fatal(err)
	}
	cfg.AddKnowledge(kb)
	kbv, _ := vault.Open(kb)
	code := filepath.Join(root, "code")
	os.MkdirAll(code, 0o755)
	r := gitx.Repo{Dir: code}
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(code, "a.txt"), []byte("a"), 0o644)
	r.AddAll()
	if _, err := r.Commit("one"); err != nil {
		t.Fatal(err)
	}
	p, _, err := project.Init(code, project.Options{Knowledge: &project.Knowledge{ID: kbv.Config.ID, Name: "ai-ml"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject(code)
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByPath(code)
	if e == nil {
		t.Fatal("the project was not scanned")
	}
	return cfg, *e, p
}

func TestDeriveAProject(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.Local)
	cfg, e, p := projectFixture(t, now)
	state := Derive(e, now, "t", 7)
	if !state.OK || state.Git == nil || !state.Git.OK || state.Tasks == nil || state.Tasks.Counts.Open != 0 || state.Described != nil || state.Pages != nil {
		t.Fatalf("fresh project: %+v", state)
	}
	if state.Heat != "new" || state.LastTouched == "" {
		t.Fatalf("the last commit touches the project: %+v", state)
	}
	if _, err := tasks.CreatePhase(p, "Alpha", "", nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.PlantTask(p, tasks.Plant{Title: "Blocked one", Phase: "Alpha"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.PlantTask(p, tasks.Plant{Title: "Stale one", Plan: "1. Go.", Start: true}, now.AddDate(0, 0, -20)); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p.Path("inbox/note.md"), []byte("later"), 0o644)
	head, _ := (gitx.Repo{Dir: e.Path}).Head()
	os.MkdirAll(filepath.Join(e.KnowledgePath(), "wiki", "entities"), 0o755)
	os.WriteFile(filepath.Join(e.KnowledgePath(), "wiki", "entities", "code.md"), []byte("---\ntitle: code\ntype: entity\nentity_type: project\nproject: "+e.ID+"\ncommit: "+head+"\n---\n"), 0o644)
	os.WriteFile(filepath.Join(e.Path, "a.txt"), []byte("b"), 0o644)
	r := gitx.Repo{Dir: e.Path}
	r.AddAll()
	r.Commit("two")
	ix, _ := registry.Scan(cfg)
	e = *ix.ByPath(e.Path)
	state = Derive(e, now, "t", 7)
	if state.Tasks == nil || state.Tasks.Counts.Open != 2 || state.Tasks.Counts.Stale != 1 || state.Tasks.Counts.Notes != 1 || len(state.Tasks.Open) != 2 {
		t.Fatalf("tasks %+v", state.Tasks)
	}
	if strings.Join(state.Tasks.Phases, ",") != "Alpha" || state.Tasks.Open[0].Title != "Stale one" || !state.Tasks.Open[0].Stale || state.Tasks.Open[1].Phase != "Alpha" || !filepath.IsAbs(state.Tasks.Open[1].Path) {
		t.Fatalf("open %+v phases %v", state.Tasks.Open, state.Tasks.Phases)
	}
	if state.Described == nil || state.Described.Page != "wiki/entities/code.md" || state.Described.Behind != 1 {
		t.Fatalf("described %+v", state.Described)
	}
	e.State = state
	got := strings.Join(Signals(e, now), "\n")
	for _, want := range []string{"1 stale task"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, registry.NotDescribed) {
		t.Errorf("a described project has no describe signal:\n%s", got)
	}
}

func TestCreationTouchesAProject(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.Local)
	work := filepath.Join(t.TempDir(), "notes")
	os.MkdirAll(work, 0o755)
	if _, _, err := project.Init(work, project.Options{}, now); err != nil {
		t.Fatal(err)
	}
	cfg := &home.Config{}
	cfg.AddProject(work)
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := *ix.ByPath(work)
	if state := Derive(e, now, "t", 7); state.Heat != "new" || state.LastTouched != "2026-09-16" || state.DaysIdle == nil || *state.DaysIdle != 0 {
		t.Fatalf("a new folder with no git and no tasks is new: %+v", state)
	}
	if state := Derive(e, now.AddDate(0, 3, 0), "t", 7); state.Heat != "cold" || state.LastTouched != "2026-09-16" {
		t.Fatalf("and cold once nothing touched it for months: %+v", state)
	}
}

func TestRegistryDerivesEveryEntry(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.Local)
	cfg, _, _ := projectFixture(t, now)
	old := filepath.Join(t.TempDir(), "old")
	os.MkdirAll(filepath.Join(old, "wiki"), 0o755)
	os.WriteFile(filepath.Join(old, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1"}`), 0o644)
	cfg.AddKnowledge(old)
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	entries, ix, err := Registry(home.Home{Root: filepath.Join(root, "home")}, cfg, stateDir, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || len(ix.Problems) != 1 {
		t.Fatalf("entries %d problems %+v", len(entries), ix.Problems)
	}
	for _, e := range entries {
		switch {
		case e.Kind == registry.Knowledge && e.Error == "":
			if e.State == nil || !e.State.OK || e.State.Heat != "new" || e.State.Pages == nil || *e.State.Pages < 4 || e.State.Tasks != nil {
				t.Errorf("knowledge base state %+v", e.State)
			}
		case e.Kind == registry.Project:
			if e.State == nil || !e.State.OK || e.State.Tasks == nil || e.State.Git == nil {
				t.Errorf("project state %+v", e.State)
			}
		default:
			if e.State == nil || e.State.OK || !strings.Contains(e.State.Error, "v1") {
				t.Errorf("v1 entry %+v", e)
			}
		}
	}
	read, _, err := registry.Read(stateDir)
	if err != nil || len(read) != 3 {
		t.Fatalf("registry file %v %d", err, len(read))
	}
}

func TestSignalsOverAnEntry(t *testing.T) {
	e := registry.Entry{Name: "p", Kind: registry.Project, Path: "/v/p",
		Knowledge: &registry.Ref{Name: "gone", Error: "no knowledge base with id x on this machine"},
		State:     &registry.State{OK: true, PendingRecovery: true, Tasks: &registry.TaskSummary{Open: []registry.TaskLine{{Title: "A", Status: "blocked"}, {Title: "B", Status: "active", Stale: true}}}},
	}
	got := strings.Join(Signals(e, time.Now()), "\n")
	for _, want := range []string{"interrupted", "knowledge base gone: no knowledge base", "1 blocked task: A", "1 stale task"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, registry.NotDescribed) {
		t.Errorf("an unresolved knowledge base asks for no page:\n%s", got)
	}
	undescribed := registry.Entry{Name: "p", Kind: registry.Project, Knowledge: &registry.Ref{Name: "kb", Path: "/v/kb"}, State: &registry.State{OK: true}}
	if got := strings.Join(Signals(undescribed, time.Now()), "\n"); !strings.Contains(got, registry.NotDescribed) || !strings.Contains(got, "describe skill") {
		t.Fatalf("a project without a page:\n%s", got)
	}
	behind := registry.Entry{Name: "p", Kind: registry.Project, Knowledge: &registry.Ref{Name: "kb", Path: "/v/kb"}, State: &registry.State{OK: true, Described: &registry.Description{Page: "wiki/entities/p.md", Commit: "abc", Behind: 40}}}
	if got := strings.Join(Signals(behind, time.Now()), "\n"); !strings.Contains(got, "40 commits behind") {
		t.Fatalf("a page far behind:\n%s", got)
	}
	if got := Signals(registry.Entry{Name: "k", Error: "v1 vault"}, time.Now()); len(got) != 1 || !strings.Contains(got[0], "v1 vault") {
		t.Fatalf("error entry %v", got)
	}
	if got := Signals(registry.Entry{Name: "k", Kind: registry.Knowledge}, time.Now()); len(got) != 1 || got[0] != "not refreshed" {
		t.Fatalf("no state %v", got)
	}
	if got := Signals(registry.Entry{Name: "k", Kind: registry.Knowledge, State: &registry.State{Error: "boom"}}, time.Now()); len(got) != 1 || !strings.Contains(got[0], "unreachable: boom") {
		t.Fatalf("a state that failed %v", got)
	}
}
