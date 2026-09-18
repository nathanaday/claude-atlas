package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

type harness struct {
	t    *testing.T
	home string
	out  bytes.Buffer
	err  bytes.Buffer
}

func (h *harness) run(args ...string) int {
	h.out.Reset()
	h.err.Reset()
	c := console.NewWith(true, strings.NewReader(""), &h.out, false)
	return run(append([]string{"--home", h.home}, args...), strings.NewReader(""), &h.out, &h.err, c)
}

// setup makes an atlas with one knowledge base, welcome, at <root>/Vaults/welcome, and
// runs from a folder that is inside nothing. It returns <root>/Vaults.
func setup(t *testing.T) (*harness, string) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	t.Chdir(root)
	h := &harness{t: t, home: filepath.Join(root, "home")}
	vaults := filepath.Join(root, "Vaults")
	code := h.run("setup", "--no-plugin", "--first-vault", filepath.Join(vaults, "welcome"))
	if code != 0 {
		t.Fatalf("setup exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	return h, vaults
}

func (h *harness) config(t *testing.T) *home.Config {
	t.Helper()
	cfg, err := home.Home{Root: h.home}.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// work makes a folder to become a project, as a git repository when asked.
func work(t *testing.T, name string, git bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if git {
		repo := gitx.Repo{Dir: dir}
		if err := repo.Init(); err != nil {
			t.Fatal(err)
		}
		if err := repo.AddAll(); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Commit("initial"); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSetupCreatesHomeAndFirstKnowledgeBase(t *testing.T) {
	h, vaults := setup(t)
	if !strings.Contains(h.out.String(), "Setup complete.") {
		t.Fatalf("output:\n%s", h.out.String())
	}
	welcome := filepath.Join(vaults, "welcome")
	for _, path := range []string{
		filepath.Join(welcome, vault.Marker),
		filepath.Join(welcome, ".git", "HEAD"),
		filepath.Join(welcome, "inbox"),
		filepath.Join(welcome, "ideas"),
		filepath.Join(h.home, "config.json"),
		filepath.Join(h.home, "state", "registry.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s", path)
		}
	}
	identity, _ := os.ReadFile(filepath.Join(welcome, vault.Marker))
	if !strings.Contains(string(identity), `"kind": "knowledge"`) || !strings.Contains(string(identity), vault.Schema) {
		t.Fatalf("identity file:\n%s", identity)
	}
	cfg, _ := os.ReadFile(filepath.Join(h.home, "config.json"))
	if !strings.Contains(string(cfg), `"schema": "claude-atlas.config.v3"`) {
		t.Fatalf("config.json:\n%s", cfg)
	}
	if code := h.run("setup", "--no-plugin"); code != 0 || !strings.Contains(h.out.String(), "keep       1 listed") {
		t.Fatalf("rerun exit %d:\n%s", code, h.out.String())
	}
}

func TestInitLinkUnlinkForget(t *testing.T) {
	h, _ := setup(t)
	dir := work(t, "webapp", true)
	if code := h.run("init", dir, "--description", "The web app.", "--knowledge", "welcome"); code != 0 {
		t.Fatalf("init exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	p, err := project.Open(dir)
	if err != nil || p.Name() != "webapp" || p.Config.Description != "The web app." || p.Config.Knowledge == nil || p.Config.Knowledge.Name != "welcome" {
		t.Fatalf("project: %+v %v", p, err)
	}
	for _, rel := range []string{project.Marker, project.ThreadsDir, project.ArchiveDir, project.StubsDir, project.SpecsDir, project.PlansDir, project.ReceiptsDir, project.PhasesDir, project.InboxDir, project.Snippet} {
		if _, err := os.Stat(p.Path(rel)); err != nil {
			t.Errorf("missing atlas/%s", rel)
		}
	}
	if cfg := h.config(t); len(cfg.Projects) != 1 || cfg.Projects[0] != dir {
		t.Fatalf("config should list the project: %+v", cfg.Projects)
	}
	if code := h.run("list"); code != 0 || !strings.Contains(h.out.String(), "project") || !strings.Contains(h.out.String(), "webapp") {
		t.Fatalf("list exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("show", "webapp"); code != 0 {
		t.Fatalf("show exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	for _, want := range []string{"Kind             project", "Description      The web app.", "Knowledge        welcome", registry.NotDescribed, "Git"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("show missing %q:\n%s", want, h.out.String())
		}
	}
	if code := h.run("show", "welcome"); code != 0 || !strings.Contains(h.out.String(), "Project          webapp") {
		t.Fatalf("the knowledge base lists its projects:\n%s", h.out.String())
	}
	if code := h.run("edit", "webapp", "--description", "Changed."); code != 0 {
		t.Fatalf("edit exit %d %s", code, h.err.String())
	}
	if p, _ := project.Open(dir); p.Config.Description != "Changed." {
		t.Fatalf("description not saved: %+v", p.Config)
	}
	if code := h.run("edit", "webapp", "--scope", "x"); code != 2 {
		t.Fatalf("scope on a project is a usage error, got %d", code)
	}
	if code := h.run("unlink", "--project", "webapp"); code != 0 || !strings.Contains(h.out.String(), "no longer uses welcome") {
		t.Fatalf("unlink exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if p, _ := project.Open(dir); p.Config.Knowledge != nil {
		t.Fatal("unlink should clear the knowledge base")
	}
	if code := h.run("unlink", "--project", "webapp"); code != 1 {
		t.Fatalf("unlink twice exit %d", code)
	}
	// From inside the work, the project is implied.
	t.Chdir(filepath.Join(dir))
	if code := h.run("link", "welcome"); code != 0 || !strings.Contains(h.out.String(), "webapp uses welcome") {
		t.Fatalf("link exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("link", "nope"); code != 1 || !strings.Contains(h.err.String(), "no knowledge base named") {
		t.Fatalf("link to nothing exit %d %s", code, h.err.String())
	}
	if code := h.run("remove", "webapp"); code != 1 || !strings.Contains(h.err.String(), "forget") {
		t.Fatalf("remove on a project exit %d %s", code, h.err.String())
	}
	if code := h.run("forget", "webapp"); code != 0 || !strings.Contains(h.out.String(), "forgot") {
		t.Fatalf("forget exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if cfg := h.config(t); len(cfg.Projects) != 0 {
		t.Fatalf("forget should drop the project: %+v", cfg.Projects)
	}
	if _, err := os.Stat(p.Path(project.Marker)); err != nil {
		t.Fatal("forget must leave atlas/ on disk")
	}
	if code := h.run("forget", "welcome"); code != 1 || !strings.Contains(h.err.String(), "knowledge base") {
		t.Fatalf("forget on a knowledge base exit %d %s", code, h.err.String())
	}
}

func TestInitRefusals(t *testing.T) {
	h, vaults := setup(t)
	dir := work(t, "plain", false)
	if code := h.run("init", dir, "--no-knowledge"); code != 0 {
		t.Fatalf("init exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if !strings.Contains(h.out.String(), "none; `claude-atlas link KB` sets one") {
		t.Fatalf("init should say there is no knowledge base:\n%s", h.out.String())
	}
	if code := h.run("init", dir, "--no-knowledge"); code != 1 || !strings.Contains(h.err.String(), "is a project already") {
		t.Fatalf("init twice exit %d %s", code, h.err.String())
	}
	nested := filepath.Join(dir, "sub")
	os.MkdirAll(nested, 0o755)
	if code := h.run("init", nested, "--no-knowledge"); code != 1 || !strings.Contains(h.err.String(), "inside the project") {
		t.Fatalf("init inside a project exit %d %s", code, h.err.String())
	}
	inVault := filepath.Join(vaults, "welcome", "wiki")
	if code := h.run("init", inVault, "--no-knowledge"); code != 1 || !strings.Contains(h.err.String(), "inside the knowledge base") {
		t.Fatalf("init inside a knowledge base exit %d %s", code, h.err.String())
	}
	if code := h.run("init", dir, "--knowledge", "x", "--no-knowledge"); code != 2 {
		t.Fatalf("both knowledge flags exit %d", code)
	}
	if code := h.run("init", filepath.Join(t.TempDir(), "missing"), "--no-knowledge"); code != 1 {
		t.Fatalf("a missing folder exit %d", code)
	}
}

func TestThreadAndPhaseCommands(t *testing.T) {
	h, _ := setup(t)
	if code := h.run("threads"); code != 0 || !strings.Contains(h.out.String(), "no open threads in any project") {
		t.Fatalf("threads exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--knowledge", "welcome"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	atlas := filepath.Join(dir, project.Dir, "webapp")
	if code := h.run("thread", "webapp", "new", "Fix", "the", "dialog", "--priority", "high"); code != 0 || !strings.Contains(h.out.String(), "opened") || !strings.Contains(h.out.String(), "stubs/Fix the dialog.md") {
		t.Fatalf("new exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if data, _ := os.ReadFile(filepath.Join(atlas, "threads", "threads.md")); !strings.Contains(string(data), "Fix the dialog") {
		t.Fatalf("threads.md should list the thread:\n%s", data)
	}
	if code := h.run("threads", "webapp"); code != 0 || !strings.Contains(h.out.String(), "  stub\n    high     Fix the dialog") {
		t.Fatalf("threads webapp exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("threads"); code != 0 || !strings.Contains(h.out.String(), "webapp\n") || !strings.Contains(h.out.String(), "Fix the dialog") {
		t.Fatalf("all threads:\n%s", h.out.String())
	}
	if code := h.run("thread", "webapp"); code != 2 {
		t.Fatalf("thread with no action exit %d", code)
	}
	if code := h.run("thread", "webapp", "new"); code != 1 || !strings.Contains(h.err.String(), "needs a title or some text") {
		t.Fatalf("new without text exit %d %s", code, h.err.String())
	}
	if code := h.run("thread", "webapp", "new", "x", "--priority", "urgent"); code != 1 {
		t.Fatalf("a bad priority exit %d", code)
	}
	if code := h.run("thread", "webapp", "new", "x", "--phase", "Nope"); code != 1 || !strings.Contains(h.err.String(), "no phase named") {
		t.Fatalf("an unknown phase exit %d %s", code, h.err.String())
	}
	// Phases: create, open a thread in one, rename follows, reorder, remove refused while
	// a thread names it.
	if code := h.run("phase", "webapp", "create", "Alarm quality", "--goal", "Fewer false alarms."); code != 0 {
		t.Fatalf("phase create exit %d %s", code, h.err.String())
	}
	if code := h.run("thread", "webapp", "new", "Filter vehicles", "--phase", "alarm quality"); code != 0 {
		t.Fatalf("new in a phase exit %d %s", code, h.err.String())
	}
	if code := h.run("phase", "webapp", "rename", "Alarm quality", "--to", "Alarms"); code != 0 {
		t.Fatalf("phase rename exit %d %s", code, h.err.String())
	}
	if data, _ := os.ReadFile(filepath.Join(atlas, "threads", "Filter vehicles.md")); !strings.Contains(string(data), `phase: "Alarms"`) {
		t.Fatalf("rename should follow into the thread:\n%s", data)
	}
	if code := h.run("phase", "webapp", "reorder", "Alarms", "--order", "5"); code != 0 || !strings.Contains(h.out.String(), "order 5") {
		t.Fatalf("phase reorder exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("phase", "webapp", "remove", "Alarms"); code != 1 || !strings.Contains(h.err.String(), "still names") {
		t.Fatalf("remove a phase in use exit %d %s", code, h.err.String())
	}
	if code := h.run("phase", "webapp", "rename", "Alarms"); code != 2 {
		t.Fatalf("rename without --to exit %d", code)
	}
	// file: a document moves the thread; the text comes from a flag or a file.
	if code := h.run("thread", "webapp", "file", "fix the", "spec", "--text", "Enter confirms."); code != 0 || !strings.Contains(h.out.String(), "spec · high") {
		t.Fatalf("file spec exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	planFile := filepath.Join(dir, "plan.md")
	os.WriteFile(planFile, []byte("1. Bind the key.\n"), 0o644)
	if code := h.run("thread", "webapp", "file", "Fix the dialog", "plan", "--file", planFile); code != 0 {
		t.Fatalf("file plan exit %d %s", code, h.err.String())
	}
	if data, _ := os.ReadFile(filepath.Join(atlas, "plans", "Fix the dialog.md")); !strings.Contains(string(data), "1. Bind the key.") {
		t.Fatalf("the plan holds the file's text:\n%s", data)
	}
	if code := h.run("thread", "webapp", "file", "Fix the dialog", "plan", "--text", "again"); code != 1 || !strings.Contains(h.err.String(), "revise it with Edit") {
		t.Fatalf("a second plan exit %d %s", code, h.err.String())
	}
	if code := h.run("thread", "webapp", "show", "Fix the dialog", "--json"); code != 0 || !strings.Contains(h.out.String(), `"stage": "plan"`) {
		t.Fatalf("show --json exit %d\n%s", code, h.out.String())
	}
	if code := h.run("thread", "webapp", "set", "Filter vehicles", "--phase", "", "--blocked", "the vendor"); code != 0 || !strings.Contains(h.out.String(), "blocked: the vendor") {
		t.Fatalf("set exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("phase", "webapp", "remove", "Alarms"); code != 0 {
		t.Fatalf("remove a free phase exit %d %s", code, h.err.String())
	}
	if code := h.run("thread", "webapp", "set", "Filter vehicles"); code != 2 {
		t.Fatalf("set with nothing to change exit %d", code)
	}
	if code := h.run("thread", "webapp", "set", "nope", "--priority", "low"); code != 1 {
		t.Fatalf("an unknown thread exit %d", code)
	}
	// close files the receipt and moves the card; reopen deletes it.
	if code := h.run("thread", "webapp", "close", "Fix the dialog", "Enter", "confirms", "now."); code != 0 || !strings.Contains(h.out.String(), "completed") {
		t.Fatalf("close exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(atlas, "threads", "archive", "Fix the dialog.md")); err != nil {
		t.Fatal("a closed thread's card moves to the archive")
	}
	if code := h.run("thread", "webapp", "close", "Filter vehicles", "--killed"); code != 1 || !strings.Contains(h.err.String(), "needs text") {
		t.Fatalf("a receipt with no text exit %d %s", code, h.err.String())
	}
	if code := h.run("threads", "webapp", "--all"); code != 0 || !strings.Contains(h.out.String(), "receipt") || !strings.Contains(h.out.String(), "completed") {
		t.Fatalf("threads --all:\n%s", h.out.String())
	}
	if code := h.run("threads", "webapp", "--json"); code != 0 || !strings.Contains(h.out.String(), `"outcome": "completed"`) {
		t.Fatalf("threads --json:\n%s", h.out.String())
	}
	if code := h.run("thread", "webapp", "reopen", "Fix the dialog"); code != 0 || !strings.Contains(h.out.String(), "reopened") {
		t.Fatalf("reopen exit %d %s", code, h.err.String())
	}
	// From inside the work, "." and nothing both mean this project.
	t.Chdir(filepath.Join(dir))
	if code := h.run("thread", ".", "new", "From inside"); code != 0 {
		t.Fatalf("new with . exit %d %s", code, h.err.String())
	}
	if code := h.run("threads"); code != 0 || !strings.Contains(h.out.String(), "From inside") || strings.Contains(h.out.String(), "webapp\n") {
		t.Fatalf("threads inside the work lists this project alone:\n%s", h.out.String())
	}
}

func TestUpgradeTurnsTasksIntoThreads(t *testing.T) {
	h, _ := setup(t)
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--no-knowledge"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	atlas := filepath.Join(dir, project.Dir, "webapp")
	os.MkdirAll(filepath.Join(atlas, "tasks"), 0o755)
	os.WriteFile(filepath.Join(atlas, "tasks", "Fix it.md"), []byte("---\ntype: task\ntitle: \"Fix it\"\nstatus: planned\npriority: normal\ncreated: 2026-09-01\nupdated: 2026-09-02\ntask_id: task-20260901-ab12\n---\n\n## Idea\n\nDo it.\n\n## Plan\n\n1. Go.\n"), 0o644)
	if code := h.run("upgrade", "webapp"); code != 0 || !strings.Contains(h.out.String(), "turned 1 task into thread") {
		t.Fatalf("upgrade exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("threads", "webapp"); code != 0 || !strings.Contains(h.out.String(), "  plan\n") || !strings.Contains(h.out.String(), "thr-20260901-ab12") {
		t.Fatalf("the task is a thread with a plan:\n%s", h.out.String())
	}
	if code := h.run("upgrade", "webapp"); code != 0 || !strings.Contains(h.out.String(), "current") {
		t.Fatalf("a second upgrade exit %d\n%s", code, h.out.String())
	}
}

func TestDescribeStagesASnapshot(t *testing.T) {
	h, vaults := setup(t)
	dir := work(t, "webapp", true)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# guide\n"), 0o644)
	if code := h.run("init", dir, "--knowledge", "welcome"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	if code := h.run("describe", "webapp", "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "staged") || !strings.Contains(h.out.String(), "/claude-atlas:describe") {
		t.Fatalf("describe exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	inbox := filepath.Join(vaults, "welcome", "inbox")
	entries, _ := os.ReadDir(inbox)
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "webapp-") {
			found = e.Name()
		}
	}
	if found == "" {
		t.Fatalf("no snapshot in %s", inbox)
	}
	data, _ := os.ReadFile(filepath.Join(inbox, found))
	if !strings.Contains(string(data), "type: project-snapshot") || !strings.Contains(string(data), "## CLAUDE.md") || strings.Contains(string(data), "atlas/project.json") {
		t.Fatalf("snapshot:\n%s", data)
	}
	if code := h.run("describe", "webapp", "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "unchanged") {
		t.Fatalf("describe twice exit %d\n%s", code, h.out.String())
	}
	plain := work(t, "docs", false)
	if code := h.run("init", plain, "--no-knowledge"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	if code := h.run("describe", "docs", "--no-claude"); code != 1 || !strings.Contains(h.err.String(), "uses no knowledge base") {
		t.Fatalf("describe without a knowledge base exit %d %s", code, h.err.String())
	}
}

func TestKnowledgeListShowEditRemove(t *testing.T) {
	h, vaults := setup(t)
	if code := h.run("list"); code != 0 || !strings.Contains(h.out.String(), "knowledge new   welcome  ") {
		t.Fatalf("list exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("show", "welcome"); code != 0 {
		t.Fatalf("show exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	for _, want := range []string{"Kind             knowledge base", "Id", "Path", "Heat", "Mode", filepath.Join(vaults, "welcome")} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("show missing %q:\n%s", want, h.out.String())
		}
	}
	if code := h.run("edit", "welcome", "--scope", "Everything."); code != 0 {
		t.Fatalf("edit exit %d %s", code, h.err.String())
	}
	if code := h.run("show", "welcome"); code != 0 || !strings.Contains(h.out.String(), "Scope            Everything.") {
		t.Fatalf("scope: exit %d\n%s", code, h.out.String())
	}
	if code := h.run("edit", "welcome", "--description", "x"); code != 2 {
		t.Fatalf("description on a knowledge base is a usage error, got %d", code)
	}
	outside := filepath.Join(t.TempDir(), "scratch")
	if code := h.run("new-knowledge", outside, "--scope", "Scratch."); code != 0 {
		t.Fatalf("new-knowledge exit %d %s", code, h.err.String())
	}
	if cfg := h.config(t); len(cfg.Knowledge) != 2 || !cfg.HasKnowledge(outside) {
		t.Fatalf("every knowledge base is listed: %+v", cfg.Knowledge)
	}
	if code := h.run("remove", "scratch"); code != 0 || !strings.Contains(h.out.String(), "removed") {
		t.Fatalf("remove exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("remove", "welcome"); code != 0 {
		t.Fatalf("remove forgets any knowledge base: exit %d %s", code, h.err.String())
	}
	if cfg := h.config(t); len(cfg.Knowledge) != 0 {
		t.Fatalf("remove should forget the vault: %+v", cfg.Knowledge)
	}
	if _, err := os.Stat(filepath.Join(outside, vault.Marker)); err != nil {
		t.Fatal("remove must leave the vault on disk")
	}
	if code := h.run("new-knowledge"); code != 2 {
		t.Fatalf("new-knowledge with no name and no terminal is a usage error, got %d", code)
	}
}

func TestRefreshAndDoctorReportProblems(t *testing.T) {
	h, vaults := setup(t)
	legacy := filepath.Join(vaults, "legacy")
	os.MkdirAll(legacy, 0o755)
	os.WriteFile(filepath.Join(legacy, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	old := filepath.Join(vaults, "oldproject")
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v2","id":"11111111-1111-4111-8111-111111111111","kind":"project","name":"old"}`), 0o644)
	cfg := h.config(t)
	cfg.AddKnowledge(legacy)
	cfg.AddKnowledge(old)
	if err := (home.Home{Root: h.home}).Save(cfg); err != nil {
		t.Fatal(err)
	}
	if code := h.run("refresh"); code != 0 || !strings.Contains(h.out.String(), "✗") || !strings.Contains(h.out.String(), "legacy") || !strings.Contains(h.out.String(), "v2 project vault") {
		t.Fatalf("refresh exit %d:\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("list"); code != 0 || !strings.Contains(h.out.String(), "✗ legacy") || !strings.Contains(h.out.String(), "v1 vault; run claude-atlas adopt") ||
		strings.Count(h.out.String(), "oldproject") != 2 || strings.Contains("\n"+h.out.String(), "\n  ?") || !strings.Contains(h.out.String(), "a v2 project vault") {
		t.Fatalf("list names each problem once, with its reason; exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "v1      ?          legacy") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	for _, path := range []string{legacy, old} {
		if code := h.run("forget", path); code != 1 || !strings.Contains(h.err.String(), "claude-atlas remove") {
			t.Fatalf("forget points a listed knowledge base path at remove: exit %d %s", code, h.err.String())
		}
		if code := h.run("remove", path); code != 0 {
			t.Fatalf("remove an unreadable entry: exit %d %s", code, h.err.String())
		}
	}
	// A project whose knowledge base is not on this machine.
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--knowledge", "welcome"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	p, _ := project.Open(dir)
	p.Config.Knowledge.ID = "00000000-0000-4000-8000-000000000000"
	p.Save()
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "webapp · knowledge") || !strings.Contains(h.out.String(), "no knowledge base with id") {
		t.Fatalf("an unresolved knowledge base: exit %d\n%s", code, h.out.String())
	}
	if code := h.run("link", "welcome", "--project", "webapp"); code != 0 {
		t.Fatalf("link repairs it: exit %d %s", code, h.err.String())
	}
	if code := h.run("doctor"); code != 0 {
		t.Fatalf("doctor after the repair: exit %d\n%s", code, h.out.String())
	}
}

// A registered project whose folder is gone is still something the atlas knows:
// refresh, list, and doctor all name it, and forget drops it.
func TestDoctorAndForgetSeeAMissingProject(t *testing.T) {
	h, _ := setup(t)
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--no-knowledge"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if code := h.run("refresh"); code != 0 || !strings.Contains(h.out.String(), "✗") || !strings.Contains(h.out.String(), "webapp") {
		t.Fatalf("refresh exit %d:\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("list"); code != 0 || !strings.Contains(h.out.String(), "✗ webapp") || !strings.Contains(h.out.String(), "not found") || strings.Contains("\n"+h.out.String(), "\n  ?") {
		t.Fatalf("list exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "missing ?          webapp") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("forget", dir); code != 0 || !strings.Contains(h.out.String(), "forgot") {
		t.Fatalf("forget exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if cfg := h.config(t); len(cfg.Projects) != 0 {
		t.Fatalf("forget should drop a project whose folder is gone: %+v", cfg.Projects)
	}
	if code := h.run("doctor"); code != 0 {
		t.Fatalf("doctor after the removal: exit %d\n%s", code, h.out.String())
	}
}

// A registered work folder that exists but lost its atlas/ is reported as not a project.
func TestDoctorReportsAWorkFolderThatIsNoLongerAProject(t *testing.T) {
	h, _ := setup(t)
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--no-knowledge"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	os.RemoveAll(filepath.Join(dir, project.Dir))
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "webapp") || !strings.Contains(h.out.String(), "bad") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("info"); code != 0 || !strings.Contains(h.out.String(), "webapp") {
		t.Fatalf("info exit %d:\n%s", code, h.out.String())
	}
}

// The in-vault commands resolve the knowledge base from a name, a path, the folder you
// are in, or the project you are in.
func TestOpenVaultArgResolvesNamesPathsAndProjects(t *testing.T) {
	h, vaults := setup(t)
	welcome := filepath.Join(vaults, "welcome")
	e := &env{home: home.Home{Root: h.home}}
	if v, err := e.openVaultArg("welcome"); err != nil || v.Root != welcome {
		t.Fatalf("name: %v %v", v, err)
	}
	if v, err := e.openVaultArg(welcome); err != nil || v.Root != welcome {
		t.Fatalf("path: %v %v", v, err)
	}
	if _, err := e.openVaultArg("nope"); err == nil {
		t.Fatal("unknown name should fail")
	}
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--knowledge", "welcome"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	if v, err := e.openVaultArg("webapp"); err != nil || v.Root != welcome {
		t.Fatalf("a project name resolves to its knowledge base: %v %v", v, err)
	}
	t.Chdir(dir)
	if v, err := e.openVaultArg(""); err != nil || v.Root != welcome {
		t.Fatalf("inside the work: %v %v", v, err)
	}
	if code := h.run("lint"); code != 0 {
		t.Fatalf("lint from inside a project lints its knowledge base: exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	t.Chdir(filepath.Join(welcome, "wiki"))
	if v, err := e.openVaultArg(""); err != nil || v.Root != welcome {
		t.Fatalf("inside the vault: %v %v", v, err)
	}
}

func TestCommandsNeedSetupFirst(t *testing.T) {
	h := &harness{t: t, home: filepath.Join(t.TempDir(), "none")}
	if code := h.run("refresh"); code != 1 || !strings.Contains(h.err.String(), "claude-atlas setup") {
		t.Fatalf("exit %d err %s", code, h.err.String())
	}
	if code := h.run("info"); code != 0 || !strings.Contains(h.out.String(), "not set up") {
		t.Fatalf("info before setup: %d %s", code, h.out.String())
	}
	if code := h.run("new-knowledge"); code != 2 {
		t.Fatalf("new-knowledge needs a name, got %d", code)
	}
	if code := h.run("bogus"); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
	if code := h.run("version"); code != 0 || strings.TrimSpace(h.out.String()) != Version {
		t.Fatalf("version: %q", h.out.String())
	}
}

func TestIngestStagesNewFilesAndRemembersTheFolder(t *testing.T) {
	h, vaults := setup(t)
	welcome := filepath.Join(vaults, "welcome")
	src := filepath.Join(filepath.Dir(vaults), "Papers")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.md"), []byte("aaa"), 0o644)
	if code := h.run("ingest", "welcome"); code != 1 || !strings.Contains(h.err.String(), "has not ingested from a folder yet") {
		t.Fatalf("no sources: exit %d err %s", code, h.err.String())
	}
	if code := h.run("ingest", "welcome", src, "--dry-run"); code != 0 || !strings.Contains(h.out.String(), "new        Papers/a.md") {
		t.Fatalf("dry run exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(welcome, "inbox", "Papers", "a.md")); err == nil {
		t.Fatal("dry run must not stage")
	}
	if code := h.run("ingest", "welcome", src, "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "staged") || !strings.Contains(h.out.String(), "remembered") || !strings.Contains(h.out.String(), "wiki-ingest") {
		t.Fatalf("ingest exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if data, _ := os.ReadFile(filepath.Join(welcome, "inbox", "Papers", "a.md")); string(data) != "aaa" {
		t.Fatal("file not staged")
	}
	if code := h.run("ingest", "welcome", "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "nothing new to stage; 1 file already waiting") {
		t.Fatalf("second ingest exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	os.WriteFile(filepath.Join(src, "b.md"), []byte("bbb"), 0o644)
	if code := h.run("ingest", "welcome", "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "new        Papers/b.md") || strings.Contains(h.out.String(), "new        Papers/a.md") {
		t.Fatalf("third ingest exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
}

func TestUpgradeRaisesAV2KnowledgeBase(t *testing.T) {
	h, vaults := setup(t)
	welcome := filepath.Join(vaults, "welcome")
	v, err := vault.Open(welcome)
	if err != nil {
		t.Fatal(err)
	}
	v2 := `{"schema":"claude-atlas.vault.v2","id":"` + v.Config.ID + `","kind":"knowledge","name":"welcome","mode":"generic","created":"2026-09-01","scope":"Old.","access":"guarded","grants":[{"id":"x","name":"y","access":"write"}]}` + "\n"
	os.WriteFile(filepath.Join(welcome, vault.Marker), []byte(v2), 0o644)
	os.RemoveAll(filepath.Join(welcome, "ideas"))
	if code := h.run("upgrade", "welcome"); code != 0 || !strings.Contains(h.out.String(), "raised to "+vault.Schema) || !strings.Contains(h.out.String(), "ideas/.gitkeep") {
		t.Fatalf("upgrade exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	data, _ := os.ReadFile(filepath.Join(welcome, vault.Marker))
	if strings.Contains(string(data), "grants") || strings.Contains(string(data), "guarded") || !strings.Contains(string(data), vault.Schema) {
		t.Fatalf("upgrade should drop access and grants:\n%s", data)
	}
	if code := h.run("upgrade", "--all"); code != 0 || !strings.Contains(h.out.String(), "current") {
		t.Fatalf("upgrade --all exit %d\n%s", code, h.out.String())
	}
}

// A project in the flat layout is named by doctor, and upgrade moves it into
// atlas/<name>/, by path, by the folder's name, or with --all.
func TestUpgradeMovesAFlatProject(t *testing.T) {
	h, _ := setup(t)
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--no-knowledge", "--name", "Web App"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	folder := filepath.Join(dir, project.Dir, "Web App")
	flatten := func() {
		aside := dir + "-aside"
		os.Rename(folder, aside)
		os.Remove(filepath.Join(dir, project.Dir))
		os.Rename(aside, filepath.Join(dir, project.Dir))
	}
	flatten()
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "flat") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	for _, arg := range []string{dir, "webapp", "--all"} {
		if code := h.run("upgrade", arg); code != 0 || !strings.Contains(h.out.String(), "into atlas/Web App/") {
			t.Fatalf("upgrade %s exit %d\n%s%s", arg, code, h.out.String(), h.err.String())
		}
		if _, err := os.Stat(filepath.Join(folder, project.Marker)); err != nil {
			t.Fatalf("upgrade %s: the identity file moved", arg)
		}
		flatten()
	}
}

func TestEditRenamesTheProjectFolder(t *testing.T) {
	h, _ := setup(t)
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--no-knowledge"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	if code := h.run("edit", "webapp", "--name", "Web App"); code != 0 {
		t.Fatalf("edit exit %d %s", code, h.err.String())
	}
	if _, err := os.Stat(filepath.Join(dir, project.Dir, "Web App", project.Marker)); err != nil {
		t.Fatal("the folder follows the name")
	}
	if code := h.run("threads", "Web App"); code != 0 {
		t.Fatalf("threads after rename exit %d %s", code, h.err.String())
	}
}

func TestAV1VaultIsNamedByDoctorAndUpgrade(t *testing.T) {
	h, vaults := setup(t)
	welcome := filepath.Join(vaults, "welcome")
	os.WriteFile(filepath.Join(welcome, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "v1      ?          welcome") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("upgrade", "--all"); code != 0 || !strings.Contains(h.out.String(), "v1") {
		t.Fatalf("upgrade --all exit %d:\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("upgrade", "welcome"); code != 1 || !strings.Contains(h.err.String(), "adopt") {
		t.Fatalf("one v1 vault still fails: exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("adopt", welcome, "--scope", "Adopted."); code != 0 || !strings.Contains(h.out.String(), "v1 vault as a knowledge base") {
		t.Fatalf("adopt exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if v, err := vault.Open(welcome); err != nil || v.Config.Scope != "Adopted." {
		t.Fatalf("adopted: %+v %v", v, err)
	}
}

func TestBareCommandOpensTheViewOrExplains(t *testing.T) {
	var out, errOut bytes.Buffer
	c := console.NewWith(false, strings.NewReader(""), &out, false)
	if code := run([]string{"--home", filepath.Join(t.TempDir(), "none")}, strings.NewReader(""), &out, &errOut, c); code != 0 || !strings.Contains(out.String(), "Usage:") || strings.Contains(out.String(), "No atlas yet") {
		t.Fatalf("piped: %d\n%s", code, out.String())
	}
	out.Reset()
	c = console.NewWith(false, strings.NewReader(""), &out, true)
	if code := run([]string{"--home", filepath.Join(t.TempDir(), "none")}, strings.NewReader(""), &out, &errOut, c); code != 0 || !strings.Contains(out.String(), "open the view") || !strings.Contains(out.String(), "No atlas yet; run `claude-atlas setup`") {
		t.Fatalf("no atlas: %d\n%s", code, out.String())
	}
	out.Reset()
	if code := run([]string{"help"}, strings.NewReader(""), &out, &errOut, c); code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("help: %d", code)
	}
}

func TestConfigNewDays(t *testing.T) {
	h, _ := setup(t)
	if code := h.run("config"); code != 0 || !strings.Contains(h.out.String(), "new-days           7") || strings.Contains(h.out.String(), "repo-changes") {
		t.Fatalf("config exit %d\n%s", code, h.out.String())
	}
	if code := h.run("config", "new-days", "1"); code != 0 || !strings.Contains(h.out.String(), "refreshed") {
		t.Fatalf("set exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if cfg := h.config(t); cfg.NewDays() != 1 {
		t.Fatalf("saved %+v", cfg.Heat)
	}
	for _, bad := range [][]string{{"config", "new-days", "-1"}, {"config", "new-days", "soon"}, {"config", "repo-changes", "pr"}, {"config", "new-days"}} {
		if code := h.run(bad...); code != 2 {
			t.Fatalf("%v exit %d", bad, code)
		}
	}
	if code := h.run("config", "new-days", "0"); code != 0 {
		t.Fatalf("zero exit %d %s", code, h.err.String())
	}
	if code := h.run("list"); code != 0 || strings.Contains(h.out.String(), "new  ") {
		t.Fatalf("with 0, a fresh vault is not new:\n%s", h.out.String())
	}
}

func TestStubCommand(t *testing.T) {
	h, vaults := setup(t)
	welcome := filepath.Join(vaults, "welcome")
	os.MkdirAll(filepath.Join(welcome, "wiki", "concepts"), 0o755)
	os.WriteFile(filepath.Join(welcome, "wiki", "concepts", "Training.md"), []byte("---\ntitle: Training\ntype: concept\nstatus: developing\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# Training\n\nSee [[vanishing gradient problem]] and [[Adam]].\n"), 0o644)
	index, _ := os.ReadFile(filepath.Join(welcome, "wiki", "index.md"))
	os.WriteFile(filepath.Join(welcome, "wiki", "index.md"), append(index, []byte("\n- [[Training]]\n")...), 0o644)
	if code := h.run("lint", "welcome", "--strict"); code != 0 || !strings.Contains(h.out.String(), "## Wanted pages (2)") {
		t.Fatalf("wanted pages are not findings: exit %d\n%s", code, h.out.String())
	}
	if code := h.run("stub", "welcome", "Adam", "--type", "entity"); code != 0 || !strings.Contains(h.out.String(), "wiki/entities/Adam.md (entity)") {
		t.Fatalf("stub Adam exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("stub", "welcome"); code != 0 || !strings.Contains(h.out.String(), "wiki/concepts/vanishing gradient problem.md (concept)") || !strings.Contains(h.out.String(), "committed") {
		t.Fatalf("stub all exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("stub", "welcome"); code != 0 || !strings.Contains(h.out.String(), "nothing to stub") {
		t.Fatalf("nothing left exit %d\n%s", code, h.out.String())
	}
	if code := h.run("stub"); code != 2 {
		t.Fatalf("usage exit %d", code)
	}
}

func TestNewKnowledgeTakesABareNameAsAFolderHere(t *testing.T) {
	h, _ := setup(t)
	here := t.TempDir()
	t.Chdir(here)
	if code := h.run("new-knowledge", "papers"); code != 0 {
		t.Fatalf("new-knowledge exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	cwd, _ := os.Getwd()
	papers := filepath.Join(cwd, "papers")
	if _, err := os.Stat(filepath.Join(papers, vault.Marker)); err != nil {
		t.Fatalf("a bare name makes the folder in the current directory: %v", err)
	}
	if !h.config(t).HasKnowledge(papers) {
		t.Fatalf("and the config lists it: %+v", h.config(t).Knowledge)
	}
	if !strings.Contains(h.out.String(), "claude-atlas init --knowledge papers") {
		t.Fatalf("the hint names it:\n%s", h.out.String())
	}
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--no-knowledge"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	t.Chdir(dir)
	if code := h.run("new-knowledge", "notes"); code != 0 {
		t.Fatalf("a knowledge base inside a project: exit %d %s", code, h.err.String())
	}
	if out := h.out.String(); !strings.Contains(out, "commits into the repository") || !strings.Contains(out, "claude-atlas link notes --project webapp") {
		t.Fatalf("it commits into the project's repository, and the hint links it:\n%s", out)
	}
	if code := h.run("link", "notes"); code != 0 {
		t.Fatalf("link exit %d %s", code, h.err.String())
	}
	if code := h.run("relocate", "x"); code != 2 {
		t.Fatalf("relocate is gone: exit %d", code)
	}
}

func TestInitNextHintNamesTheProject(t *testing.T) {
	h, _ := setup(t)
	dir := work(t, "webapp", false)
	if code := h.run("init", dir, "--no-knowledge"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	out := h.out.String()
	if strings.Contains(out, "thread . new") || !strings.Contains(out, `claude-atlas thread webapp new "..."`) || !strings.Contains(out, "claude-atlas open-claude webapp") {
		t.Fatalf("init from elsewhere should name the project:\n%s", out)
	}

	here := work(t, "api", false)
	t.Chdir(here)
	if code := h.run("init", "--no-knowledge"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	out = h.out.String()
	if !strings.Contains(out, `claude-atlas thread . new "..."`) || strings.Contains(out, "open-claude") {
		t.Fatalf("init in the folder should use . and claude:\n%s", out)
	}

	same := work(t, "welcome", false)
	if code := h.run("init", same, "--knowledge", "welcome"); code != 0 {
		t.Fatalf("init exit %d %s", code, h.err.String())
	}
	out = h.out.String()
	if !strings.Contains(out, "claude-atlas describe welcome ") || !strings.Contains(out, "claude-atlas open-claude "+home.Display(same)) {
		t.Fatalf("a name the knowledge base shares should give the path to open-claude:\n%s", out)
	}
}

func TestShellArg(t *testing.T) {
	for in, want := range map[string]string{
		"webapp":    "webapp",
		"my app":    "'my app'",
		"it's":      `'it'\''s'`,
		"":          "''",
		"a/b-c_d.e": "a/b-c_d.e",
		"$HOME":     "'$HOME'",
	} {
		if got := shellArg(in); got != want {
			t.Errorf("shellArg(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestInitMakesAGitRepository(t *testing.T) {
	h, _ := setup(t)
	plain := work(t, "plain", false)
	if code := h.run("init", plain, "--no-knowledge"); code != 0 || !strings.Contains(h.out.String(), "initialized a repository on main") {
		t.Fatalf("init exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if !(gitx.Repo{Dir: plain}).IsRepo() {
		t.Fatal("the folder is a repository")
	}
	repo := work(t, "repo", true)
	if code := h.run("init", repo, "--no-knowledge"); code != 0 || !strings.Contains(h.out.String(), "a repository already") {
		t.Fatalf("init exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	none := work(t, "none", false)
	if code := h.run("init", none, "--no-knowledge", "--no-git"); code != 0 || !strings.Contains(h.out.String(), "none; --no-git") {
		t.Fatalf("init exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(none, ".git")); err == nil {
		t.Fatal("--no-git makes no repository")
	}
}

func TestListRebuildsAStaleRegistry(t *testing.T) {
	h, _ := setup(t)
	path := registry.File(filepath.Join(h.home, "state"))
	if err := os.WriteFile(path, []byte(`{"schema":"claude-atlas.registry.v1","entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := h.run("list"); code != 0 || !strings.Contains(h.out.String(), "welcome") {
		t.Fatalf("list exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
}
