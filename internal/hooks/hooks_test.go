package hooks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/threads"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "v")
	if _, err := vault.Init(root, vault.Options{Name: "v", Mode: vault.Generic}, time.Now()); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.Open(root)
	return v
}

// env answers CLAUDE_ATLAS_HOME with a temp path that does not exist, so a test that
// names no home reads no atlas at all instead of the developer's ~/.claude-atlas.
func env(t *testing.T, values map[string]string) Env {
	t.Helper()
	noAtlas := filepath.Join(t.TempDir(), "no-atlas")
	return func(k string) string {
		if k == home.EnvHome && values[k] == "" {
			return noAtlas
		}
		return values[k]
	}
}

// atlas makes a home with the knowledge base ai-ml and a project on it in a git
// repository, and returns the home, the knowledge base's root, and the work folder.
func atlas(t *testing.T, now time.Time) (home.Home, string, string) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default()
	kb := filepath.Join(root, "Vaults", "ai-ml")
	if _, err := vault.Init(kb, vault.Options{Name: "ai-ml", Scope: "Machine learning."}, now); err != nil {
		t.Fatal(err)
	}
	cfg.AddKnowledge(kb)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "code")
	os.MkdirAll(filepath.Join(work, "src"), 0o755)
	os.WriteFile(filepath.Join(work, "src", "main.go"), []byte("package main\n"), 0o644)
	r := gitx.Repo{Dir: work}
	if err := r.Init(); err != nil {
		t.Fatal(err)
	}
	r.AddAll()
	if _, err := r.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := vaults.InitProject(h, cfg, work, project.Options{Description: "The web app."}, "ai-ml", true, now); err != nil {
		t.Fatal(err)
	}
	return h, kb, work
}

func run(t *testing.T, cwd string, e Env, context bool, now time.Time) string {
	t.Helper()
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+cwd+`"}`), &out, e, context, now); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestSessionStartInAKnowledgeBase(t *testing.T) {
	v := newVault(t)
	e := env(t, nil)
	if _, err := os.Stat(e(home.EnvHome)); !os.IsNotExist(err) {
		t.Fatalf("the default test home must not exist on disk: %v", err)
	}
	text := run(t, v.Path("wiki"), e, true, time.Now())
	for _, want := range []string{"claude-atlas: knowledge base v (generic mode) at ", "<vault-context>", "Active Threads", WriteSentence, KnowledgeSkills} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, absent := range []string{"type: meta", "Open threads", "Projects:", "Stubs:", "Wanted:"} {
		if strings.Contains(text, absent) {
			t.Errorf("%q should not be there:\n%s", absent, text)
		}
	}
	if text := run(t, t.TempDir(), env(t, nil), true, time.Now()); text != "" {
		t.Fatalf("silent outside a place:\n%s", text)
	}
	text = run(t, "/nowhere", env(t, map[string]string{vault.EnvVault: v.Root, "CLAUDE_ATLAS_SESSION_CONTEXT": "0"}), true, time.Now())
	if !strings.Contains(text, "claude-atlas: knowledge base") || strings.Contains(text, "<vault-context>") {
		t.Fatalf("env vault with context off:\n%s", text)
	}
	// The counts line comes before the skills line.
	os.MkdirAll(v.Path("wiki/concepts"), 0o755)
	training := vault.Skeleton("concept", "Training", time.Now())
	training = strings.Replace(training, "## Related\n\n", "## Related\n\n[[Optimizer]], [[Backpropagation]]\n\n", 1)
	os.WriteFile(v.Path("wiki/concepts/Training.md"), []byte(training), 0o644)
	os.WriteFile(v.Path("wiki/concepts/Backpropagation.md"), []byte(vault.Skeleton("concept", "Backpropagation", time.Now())), 0o644)
	os.WriteFile(v.Path("inbox/paper.md"), []byte("x"), 0o644)
	text = run(t, v.Root, env(t, nil), false, time.Now())
	want := "Stubs: 1 page to fill (Backpropagation). Wanted: 1 linked page does not exist yet (Optimizer). Fill or stub them with the wiki-lint skill."
	if !strings.Contains(text, want) || !strings.Contains(text, "Inbox: 1 source waiting; the wiki-ingest skill files them.") {
		t.Errorf("missing counts or inbox in:\n%s", text)
	}
	if i, j := strings.Index(text, want), strings.Index(text, "Skills:"); i < 0 || j < 0 || i > j {
		t.Errorf("counts line before the skills line:\n%s", text)
	}
	four := vault.Skeleton("concept", "Four Wants", time.Now())
	four = strings.Replace(four, "## Related\n\n", "## Related\n\n[[Alpha]], [[Bravo]], [[Charlie]], [[Delta]]\n\n", 1)
	os.WriteFile(v.Path("wiki/concepts/Four Wants.md"), []byte(four), 0o644)
	if text = run(t, v.Root, env(t, nil), false, time.Now()); !strings.Contains(text, "Wanted: 5 linked pages do not exist yet (Alpha, Bravo, Charlie, …).") {
		t.Errorf("missing capped wanted list in:\n%s", text)
	}
	// An interrupted operation warns at start and at stop.
	os.MkdirAll(v.Path(".vault-meta"), 0o755)
	os.WriteFile(v.Path(".vault-meta/inflight.json"), []byte(`{"operation_id":"save-x","paths":[]}`), 0o644)
	if text = run(t, v.Root, env(t, nil), false, time.Now()); !strings.Contains(text, "WARNING: operation save-x was interrupted") {
		t.Fatalf("recovery warning:\n%s", text)
	}
	var out bytes.Buffer
	Stop(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(t, nil))
	if !strings.Contains(out.String(), `"systemMessage"`) || !strings.Contains(out.String(), "save-x") {
		t.Fatalf("stop:\n%s", out.String())
	}
	out.Reset()
	Stop(strings.NewReader(`{"cwd":"`+t.TempDir()+`"}`), &out, env(t, nil))
	if out.Len() != 0 {
		t.Fatal("stop is silent outside a place")
	}
}

func TestSessionStartInAProject(t *testing.T) {
	now := time.Now()
	h, kb, work := atlas(t, now)
	e := env(t, map[string]string{home.EnvHome: h.Root})
	text := run(t, filepath.Join(work, "src"), e, true, now)
	for _, want := range []string{
		"claude-atlas: project code at " + home.Display(work) + " (git, ",
		"Description: The web app.",
		"Knowledge: ai-ml · Machine learning. · ",
		" pages · " + home.Display(kb),
		"This project has no page in ai-ml; the describe skill writes it.",
		SearchSentence + " " + WriteSentence,
		"Skills: " + ProjectSkills,
		"Open threads: none. Open one with the thread-stub skill.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, absent := range []string{"<vault-context>", "The atlas config", "Projects:"} {
		if strings.Contains(text, absent) {
			t.Errorf("%q should not be there:\n%s", absent, text)
		}
	}
	// Threads, phases, notes, and a page in the knowledge base.
	p, _ := project.Open(work)
	if _, err := threads.CreatePhase(p, "Alpha", "", nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := threads.Start(p, threads.New{Title: "Fix the dialog", Text: "It quits on Enter.", Phase: "Alpha", Priority: "high"}, now); err != nil {
		t.Fatal(err)
	}
	old := now.AddDate(0, 0, -20)
	if _, err := threads.Start(p, threads.New{Title: "Stale one"}, old); err != nil {
		t.Fatal(err)
	}
	if _, err := threads.File(p, "Stale one", threads.Filing{Stage: threads.Plan, Text: "1. Go."}, old); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p.Path("inbox/idea.md"), []byte("An idea."), 0o644)
	os.WriteFile(p.Path("specs/broken.md"), []byte("x"), 0o644)
	os.MkdirAll(p.Path("tasks"), 0o755)
	os.WriteFile(p.Path("tasks/Old.md"), []byte("---\ntype: task\n---\n"), 0o644)
	head, _ := (gitx.Repo{Dir: work}).Head()
	os.MkdirAll(filepath.Join(kb, "wiki", "entities"), 0o755)
	os.WriteFile(filepath.Join(kb, "wiki", "entities", "code.md"), []byte("---\ntitle: code\ntype: entity\nentity_type: project\nproject: "+p.Config.ID+"\ncommit: "+head+"\nstatus: developing\ncreated: 2026-09-17\nupdated: 2026-09-17\ntags:\n  - entity\n---\n\n# code\n"), 0o644)
	text = run(t, work, e, false, now)
	for _, want := range []string{
		"This project is described in wiki/entities/code.md at " + head[:7] + ", current.",
		"Open threads: 2 (plan 1, spec 0, stub 1; 1 stale) in 1 phase. A thread moves stub, spec, plan, receipt",
		"- [plan] Stale one (thr-",
		" · stale\n",
		"- [stub] Fix the dialog (thr-",
		" · Alpha · high · updated " + now.Format("2006-01-02") + "\n",
		"1 note waits in atlas/code/inbox/; the thread-stub skill opens a thread from each.",
		"Not readable: specs/broken.md (",
		"task pages from before threads; `claude-atlas upgrade",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "no page in") {
		t.Errorf("a described project:\n%s", text)
	}
	// A knowledge base session lists its projects and the ones without a page.
	text = run(t, kb, e, false, now)
	for _, want := range []string{"claude-atlas: knowledge base ai-ml (generic mode)", "Scope: Machine learning.", "Projects: code (" + home.Display(work) + ", 2 open threads). Open a thread in one with the thread tool"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in knowledge base session:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Not yet described here") {
		t.Errorf("code is described:\n%s", text)
	}
	os.Remove(filepath.Join(kb, "wiki", "entities", "code.md"))
	if text = run(t, kb, e, false, now); !strings.Contains(text, "Not yet described here: code; the describe skill writes the page.") {
		t.Errorf("missing the undescribed list:\n%s", text)
	}
	// Outside every place the hook is silent.
	if text := run(t, filepath.Dir(work), e, true, now); text != "" {
		t.Fatalf("silent outside:\n%s", text)
	}
}

func TestSessionStartHealsTheConfigAndNamesAMissingKnowledgeBase(t *testing.T) {
	now := time.Now()
	h, _, work := atlas(t, now)
	e := env(t, map[string]string{home.EnvHome: h.Root})
	clone := filepath.Join(t.TempDir(), "clone")
	os.MkdirAll(clone, 0o755)
	if _, _, err := project.Init(clone, project.Options{Knowledge: &project.Knowledge{ID: "gone-0000", Name: "papers"}}, now); err != nil {
		t.Fatal(err)
	}
	text := run(t, clone, e, false, now)
	for _, want := range []string{"The atlas config did not list this project; it does now.", "Knowledge: the knowledge base papers (gone-0000) is not on this machine"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if cfg, _ := h.Load(); !cfg.HasProject(clone) {
		t.Fatal("the config lists the clone now")
	}
	copied := filepath.Join(t.TempDir(), "copied")
	os.MkdirAll(filepath.Join(copied, project.Dir, "p"), 0o755)
	orig, _ := project.Open(work)
	data, _ := os.ReadFile(orig.Path(project.Marker))
	os.WriteFile(filepath.Join(copied, project.Dir, "p", project.Marker), data, 0o644)
	if text = run(t, copied, e, false, now); !strings.Contains(text, "The atlas config listed this project at another path; it now points here.") {
		t.Errorf("missing the moved line:\n%s", text)
	}
	solo := filepath.Join(t.TempDir(), "solo")
	os.MkdirAll(solo, 0o755)
	project.Init(solo, project.Options{}, now)
	if text = run(t, solo, e, false, now); !strings.Contains(text, "Knowledge: none; link one with `claude-atlas link KB`.") {
		t.Errorf("missing the no-knowledge line:\n%s", text)
	}
}

func TestSessionStartHealsAKnowledgeBase(t *testing.T) {
	now := time.Now()
	h, kb, work := atlas(t, now)
	e := env(t, map[string]string{home.EnvHome: h.Root})
	if text := run(t, kb, e, false, now); strings.Contains(text, "atlas config") {
		t.Fatalf("a listed knowledge base needs no heal:\n%s", text)
	}
	moved := filepath.Join(t.TempDir(), "ai-ml")
	if err := os.Rename(kb, moved); err != nil {
		t.Fatal(err)
	}
	if text := run(t, filepath.Join(moved, "wiki"), e, false, now); !strings.Contains(text, "The atlas config listed this knowledge base at another path; it now points here.") {
		t.Fatalf("missing the moved line:\n%s", text)
	}
	if cfg, _ := h.Load(); !cfg.HasKnowledge(moved) || cfg.HasKnowledge(kb) {
		t.Fatalf("the config follows the folder: %+v", cfg.Knowledge)
	}
	if text := run(t, work, e, false, now); !strings.Contains(text, "Knowledge: ai-ml") {
		t.Fatalf("the project reaches its moved knowledge base:\n%s", text)
	}
	other := filepath.Join(t.TempDir(), "other")
	if _, err := vault.Init(other, vault.Options{Name: "other"}, now); err != nil {
		t.Fatal(err)
	}
	if text := run(t, other, e, false, now); !strings.Contains(text, "The atlas config did not list this knowledge base; it does now.") {
		t.Fatalf("missing the added line:\n%s", text)
	}
}

func TestSessionStartNamesOldVaults(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "wiki"), 0o755)
	os.WriteFile(filepath.Join(root, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	text := run(t, filepath.Join(root, "wiki"), env(t, nil), true, time.Now())
	for _, want := range []string{"v1 vault", "adopt", root} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	old := t.TempDir()
	os.WriteFile(filepath.Join(old, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v2","id":"p-1","kind":"project","name":"old","mode":"generic"}`), 0o644)
	if text := run(t, old, env(t, nil), true, time.Now()); !strings.Contains(text, "v2 project vault") || !strings.Contains(text, "claude-atlas init") {
		t.Errorf("a v2 project vault:\n%s", text)
	}
	flat := t.TempDir()
	os.MkdirAll(filepath.Join(flat, project.Dir), 0o755)
	os.WriteFile(filepath.Join(flat, project.Dir, project.Marker), []byte(`{"schema":"`+project.Schema+`","id":"f","name":"flat"}`), 0o644)
	if text := run(t, flat, env(t, nil), true, time.Now()); !strings.Contains(text, "sits directly in atlas/; run claude-atlas upgrade") {
		t.Errorf("a flat project:\n%s", text)
	}
}

func TestGuard(t *testing.T) {
	v := newVault(t)
	work := filepath.Join(t.TempDir(), "work")
	os.MkdirAll(work, 0o755)
	if _, _, err := project.Init(work, project.Options{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	wp, _ := project.Open(work)
	if _, err := threads.Start(wp, threads.New{Title: "Fix it"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		v.Path("wiki/concepts/A.md"):                                            true,
		v.Path(".raw/captured/x.pdf"):                                           true,
		v.Path(".claude-atlas.json"):                                            true,
		v.Path(".vault-meta/lock"):                                              true,
		v.Path("inbox/paper.md"):                                                false,
		v.Path("ideas/note.md"):                                                 false,
		v.Path("notes.md"):                                                      false,
		filepath.Join(t.TempDir(), "wiki/x.md"):                                 false,
		filepath.Join(work, "atlas", "work", "threads", "threads.md"):           true,
		filepath.Join(work, "atlas", "work", "threads", "Fix it.md"):            true,
		filepath.Join(work, "atlas", "work", "threads", "archive", "Fix it.md"): true,
		filepath.Join(work, "atlas", "work", "project.json"):                    true,
		filepath.Join(work, "atlas", "work", "stubs", "Fix it.md"):              false,
		filepath.Join(work, "atlas", "work", "specs", "New.md"):                 true,
		filepath.Join(work, "atlas", "work", "phases", "Alpha.md"):              false,
		filepath.Join(work, "atlas", "work", "inbox", "note.md"):                false,
		filepath.Join(work, "src", "main.go"):                                   false,
		filepath.Join(work, "threads", "threads.md"):                            false,
		filepath.Join(work, "atlas", "threads", "threads.md"):                   false,
		filepath.Join(work, "atlas", "work", "specs", "notes.txt"):              false,
	}
	for path, deny := range cases {
		var out bytes.Buffer
		if err := Guard(strings.NewReader(`{"tool_name":"Write","tool_input":{"file_path":"`+path+`"}}`), &out); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(out.String(), `"deny"`); got != deny {
			t.Errorf("%s: deny=%v, got %q", path, deny, out.String())
		}
	}
	var out bytes.Buffer
	Guard(strings.NewReader(`{"tool_name":"Edit","cwd":"`+v.Root+`","tool_input":{"file_path":"wiki/hot.md"}}`), &out)
	if !strings.Contains(out.String(), "deny") {
		t.Fatal("relative paths resolve against cwd")
	}
	out.Reset()
	Guard(strings.NewReader(`{"tool_name":"Write","tool_input":{"file_path":"`+filepath.Join(work, "atlas", "work", "threads", "threads.md")+`"}}`), &out)
	if !strings.Contains(out.String(), "are generated") {
		t.Errorf("the board's reason: %q", out.String())
	}
	out.Reset()
	Guard(strings.NewReader(`{"tool_name":"Write","tool_input":{"file_path":"`+filepath.Join(work, "atlas", "work", "plans", "New.md")+`"}}`), &out)
	if !strings.Contains(out.String(), "a new plan comes from the thread tool (id, stage: plan, text)") {
		t.Errorf("a new document's reason: %q", out.String())
	}
	out.Reset()
	Guard(strings.NewReader(`{"tool_name":"NotebookEdit","tool_input":{"notebook_path":"`+v.Path("wiki/x.ipynb")+`"}}`), &out)
	if !strings.Contains(out.String(), "deny") {
		t.Fatal("a notebook path is guarded too")
	}
	out.Reset()
	Guard(strings.NewReader(`{"tool_name":"Write","tool_input":{}}`), &out)
	if out.Len() != 0 {
		t.Fatal("no path, no decision")
	}
}

func TestTouchedMarksTheThread(t *testing.T) {
	work := filepath.Join(t.TempDir(), "work")
	os.MkdirAll(work, 0o755)
	day := time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local)
	p, _, err := project.Init(work, project.Options{}, day)
	if err != nil {
		t.Fatal(err)
	}
	th, err := threads.Start(p, threads.New{Title: "Fix it"}, day)
	if err != nil {
		t.Fatal(err)
	}
	in := `{"tool_name":"Edit","cwd":"` + work + `","tool_input":{"file_path":"` + p.Path(th.Docs[0].Path) + `"}}`
	if err := Touched(strings.NewReader(in), day.AddDate(0, 0, 3)); err != nil {
		t.Fatal(err)
	}
	board, _ := threads.Load(p)
	if got := board.Find(th.ID).Updated; got != "2026-09-20" {
		t.Fatalf("updated %s", got)
	}
	// Any other file is none of its business.
	if err := Touched(strings.NewReader(`{"tool_input":{"file_path":"`+filepath.Join(work, "main.go")+`"}}`), day); err != nil {
		t.Fatal(err)
	}
}
