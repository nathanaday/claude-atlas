package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.Local)

func page(status, folder, id, extra string) (string, []byte) {
	p := folder + "/Fix it.md"
	text := "---\ntype: task\ntitle: \"Fix it\"\nstatus: " + status + "\npriority: high\ncreated: 2026-09-01\nupdated: 2026-09-02\ntags:\n  - task\ntask_id: " + id + "\n---\n\n# Fix it\n\n## Idea\n\nDo it.\n" + extra
	return p, []byte(text)
}

func TestParseEnforcesTheRules(t *testing.T) {
	p, text := page("planted", vault.TasksDir, "task-20260901-ab12", "")
	task, err := Parse(p, text)
	if err != nil || task.ID != "task-20260901-ab12" || task.Status != "planted" || task.Priority != "high" || task.HasPlan {
		t.Fatalf("%+v %v", task, err)
	}
	if !IsPage(p) || IsPage(vault.TasksIndex) || IsPage("wiki/tasks/index.md") || IsPage("wiki/tasks/deep/x.md") || IsPage("wiki/concepts/x.md") {
		t.Fatal("IsPage")
	}
	bad := map[string][2]string{
		"bad status":       {"soon", vault.TasksDir},
		"done in open":     {"done", vault.TasksDir},
		"active archived":  {"active", vault.TaskArchiveDir},
		"bad id":           {"planted", vault.TasksDir},
		"cancelled stays":  {"cancelled", vault.TaskArchiveDir},
		"planned has plan": {"planned", vault.TasksDir},
	}
	for name, spec := range bad {
		id := "task-20260901-ab12"
		if name == "bad id" {
			id = "nope"
		}
		p, text := page(spec[0], spec[1], id, "")
		_, err := Parse(p, text)
		switch name {
		case "cancelled stays", "planned has plan":
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
		default:
			if err == nil {
				t.Errorf("%s should fail", name)
			}
		}
	}
	p, text = page("planned", vault.TasksDir, "task-20260901-ab12", "\n## Plan\n\n1. Read.\n\n## Progress\n")
	if task, _ := Parse(p, text); !task.HasPlan {
		t.Fatal("plan detected")
	}
	p, text = page("planned", vault.TasksDir, "task-20260901-ab12", "\n## Plan\n\n\n## Progress\n\n- x\n")
	if task, _ := Parse(p, text); task.HasPlan {
		t.Fatal("an empty plan section is no plan")
	}
	if _, err := Parse("wiki/tasks/x.md", []byte("---\ntype: concept\ntitle: x\nstatus: seed\ncreated: 2026-01-01\nupdated: 2026-01-01\ntags: []\n---\n")); err == nil || !strings.Contains(err.Error(), "type: task") {
		t.Fatalf("type check: %v", err)
	}
}

func TestTitleFromTextAndSkeleton(t *testing.T) {
	for in, want := range map[string]string{
		"# Fix the dialog\n\nmore":      "Fix the dialog",
		"\n- ship it, then celebrate\n": "ship it, then celebrate",
		strings.Repeat("word ", 30):     strings.TrimSpace(strings.Repeat("word ", 16)),
		"":                              "",
	} {
		if got := TitleFromText(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	text := Skeleton(Plant{Title: "Fix \"it\"", Text: "Why.\n\nHow.", Workdir: "/tmp/x", Due: "2026-10-01"}, "task-20260913-3f2a", now)
	task, err := Parse(vault.TasksDir+"/Fix it.md", []byte(text))
	if err != nil || task.Title != "Fix \"it\"" || task.Workdir != "/tmp/x" || task.Due != "2026-10-01" || task.Priority != "normal" || task.Status != "planted" {
		t.Fatalf("skeleton parses: %+v %v\n%s", task, err, text)
	}
	if !strings.Contains(text, "## Idea\n\nWhy.\n\nHow.\n") {
		t.Fatalf("idea verbatim:\n%s", text)
	}
	if !idPattern.MatchString(NewID(now)) {
		t.Fatal("NewID")
	}
	taken := map[string]bool{vault.TasksDir + "/Fix it.md": true, vault.TasksDir + "/Fix it (2).md": true}
	if got := PagePath("Fix it", func(p string) bool { return taken[p] }); got != vault.TasksDir+"/Fix it (3).md" {
		t.Fatalf("path %q", got)
	}
	for _, title := range []string{"tasks", "Tasks", "index"} {
		if got := PagePath(title, func(string) bool { return false }); got != vault.TasksDir+"/"+title+" (2).md" {
			t.Errorf("a task titled %q takes the index's name: %q", title, got)
		}
	}
}

func TestLedgerBuildIndexAndFreshness(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "v")
	if _, err := vault.Init(root, vault.Generic, now); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.Open(root)
	led, err := LoadLedger(v)
	if err != nil || len(led.Tasks) != 0 || led.Schema != Schema {
		t.Fatalf("fresh ledger: %+v %v", led, err)
	}
	p, text := page("active", vault.TasksDir, "task-20260901-ab12", "\n## Plan\n\n1. Go.\n")
	os.MkdirAll(v.Path(vault.TasksDir), 0o755)
	os.WriteFile(v.Path(p), text, 0o644)
	q, qtext := page("done", vault.TaskArchiveDir, "task-20260801-cd34", "")
	os.MkdirAll(v.Path(vault.TaskArchiveDir), 0o755)
	os.WriteFile(v.Path(q), qtext, 0o644)
	os.WriteFile(v.Path(vault.TasksDir+"/broken.md"), []byte("no frontmatter"), 0o644)
	t.Setenv("GIT_AUTHOR_DATE", "2026-09-13T12:00:00")
	t.Setenv("GIT_COMMITTER_DATE", "2026-09-13T12:00:00")
	repo := v.Repo()
	repo.AddAll()
	repo.Commit("task: hand made\n\natlas-operation: task-20260902-0000\n")
	touch := Touch{OperationID: "task-20260913-1111", Date: "2026-09-13", Summary: "plant"}
	built, err := Build(v, led, &touch, []string{p}, now)
	if err != nil || len(built.Tasks) != 2 || len(built.Problems) != 1 {
		t.Fatalf("build: %+v %v", built, err)
	}
	active := built.Find("task-20260901-ab12")
	if active == nil || len(active.History) != 2 || active.History[0].OperationID != "task-20260902-0000" || active.History[1].OperationID != touch.OperationID || active.LastTouched != "2026-09-13" {
		t.Fatalf("history from git then the pending touch: %+v", active)
	}
	if done := built.Find("task-20260801-cd34"); done == nil || len(done.History) != 1 || done.LastTouched < "2026-09-02" {
		t.Fatalf("archived history: %+v", done)
	}
	counts := built.Counts(now)
	if counts.Open != 1 || counts.Active != 1 || counts.Done != 1 || counts.Stale != 0 {
		t.Fatalf("counts %+v", counts)
	}
	later := now.AddDate(0, 0, 20)
	if !Stale(*active, later) || built.Counts(later).Stale != 1 {
		t.Fatal("active for 20 days is stale")
	}
	index := RenderIndex(built, nil, now)
	for _, want := range []string{"title: Tasks", "## Open", "| [[Fix it]] | active | high | — | 2026-09-13 |", "## Archive", "| [[Fix it]] | done |", "## Not readable as tasks", "`wiki/tasks/broken.md`"} {
		if !strings.Contains(index, want) {
			t.Errorf("missing %q in:\n%s", want, index)
		}
	}
	if again := RenderIndex(built, []byte("---\ncreated: 2020-01-01\n---\n"), now); !strings.Contains(again, "created: 2020-01-01") {
		t.Fatal("created carries over")
	}
	// Current rebuilds when the pages moved on since the stored ledger.
	os.WriteFile(v.Path(vault.TaskLedgerPath), built.Encode(), 0o644)
	current, err := Current(v, now)
	if err != nil || len(current.Tasks) != 2 || current.Find("task-20260901-ab12").LastTouched != "2026-09-13" {
		t.Fatalf("stored ledger is current: %+v %v", current, err)
	}
	os.WriteFile(v.Path(p), []byte(strings.Replace(string(text), "status: active", "status: blocked", 1)), 0o644)
	current, _ = Current(v, now)
	if current.Find("task-20260901-ab12").Status != "blocked" {
		t.Fatal("a hand edit shows without a rebuild being stored")
	}
	if stored, _ := LoadLedger(v); stored.Find("task-20260901-ab12").Status != "active" {
		t.Fatal("reads do not write the ledger")
	}
	os.MkdirAll(v.Path(vault.InboxTasksDir), 0o755)
	os.WriteFile(v.Path(vault.InboxTasksDir+"/a.md"), []byte("x"), 0o644)
	os.WriteFile(v.Path(vault.InboxTasksDir+"/.hidden"), []byte("x"), 0o644)
	if notes := Notes(v); len(notes) != 1 || notes[0] != "inbox/tasks/a.md" {
		t.Fatalf("notes %v", notes)
	}
}
