package threads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/project"
)

var now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.Local)

func newProject(t *testing.T) *project.Project {
	t.Helper()
	work := filepath.Join(t.TempDir(), "webapp")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	p, _, err := project.Init(work, project.Options{}, now)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func write(t *testing.T, p *project.Project, rel, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p.Path(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Path(rel), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p *project.Project, rel string) string {
	t.Helper()
	data, err := os.ReadFile(p.Path(rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func start(t *testing.T, p *project.Project, title, text string) *Thread {
	t.Helper()
	th, err := Start(p, New{Title: title, Text: text}, now)
	if err != nil {
		t.Fatal(err)
	}
	return th
}

func ptr(s string) *string { return &s }

func TestPaths(t *testing.T) {
	if !IsCard("threads/A.md") || !IsCard("threads/archive/A.md") || IsCard(project.ThreadsIndex) || IsCard("threads/deep/er/A.md") || IsCard("stubs/A.md") {
		t.Fatal("IsCard")
	}
	if DocStage("stubs/A.md") != Stub || DocStage("receipts/A.md") != Receipt || DocStage("stubs/deep/A.md") != "" || DocStage("plans/a.txt") != "" || DocStage("threads/A.md") != "" {
		t.Fatal("DocStage")
	}
	if !Owned(project.ThreadsIndex) || !Owned("threads/archive/A.md") || Owned("specs/A.md") || Owned("phases/A.md") {
		t.Fatal("Owned")
	}
}

func TestStartWritesACardAndAStub(t *testing.T) {
	p := newProject(t)
	th := start(t, p, "", "# Login breaks on Safari\n\nThe cookie is dropped.")
	if th.Title != "Login breaks on Safari" || th.Stage != Stub || th.Path != "threads/Login breaks on Safari.md" || th.Priority != "normal" || len(th.Docs) != 1 {
		t.Fatalf("%+v", th)
	}
	stub := read(t, p, "stubs/Login breaks on Safari.md")
	for _, want := range []string{"type: stub", "thread: " + th.ID, "> [!stub] Login breaks on Safari", "**Stub** → Spec → Plan → Receipt", "[Thread](<../threads/Login breaks on Safari.md>)", "The cookie is dropped."} {
		if !strings.Contains(stub, want) {
			t.Fatalf("stub lacks %q:\n%s", want, stub)
		}
	}
	card := read(t, p, th.Path)
	for _, want := range []string{"stage: stub", "> [!thread] Login breaks on Safari", "[Stub](<../stubs/Login breaks on Safari.md>)", "> - Spec · none", "![[stubs/Login breaks on Safari]]"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}
	index := read(t, p, project.ThreadsIndex)
	if !strings.Contains(index, "> [!stub] Stub · 1") || !strings.Contains(index, "[Login breaks on Safari](<Login breaks on Safari.md>)") {
		t.Fatal(index)
	}
	if _, err := Start(p, New{}, now); err == nil {
		t.Fatal("a thread with no title and no text was opened")
	}
	if _, err := Start(p, New{Title: "x", Phase: "Nope"}, now); err == nil {
		t.Fatal("an unknown phase was accepted")
	}
	// The same title twice takes another file name for every page.
	again := start(t, p, "Login breaks on Safari", "again")
	if again.Path != "threads/Login breaks on Safari (2).md" || again.Docs[0].Path != "stubs/Login breaks on Safari (2).md" {
		t.Fatalf("%+v", again)
	}
}

func TestStartFromANote(t *testing.T) {
	p := newProject(t)
	write(t, p, "inbox/idea.md", "Cache the registry\n\nIt is read on every key.")
	th, err := Start(p, New{From: "inbox/idea.md"}, now)
	if err != nil || th.Title != "Cache the registry" {
		t.Fatalf("%+v %v", th, err)
	}
	if _, err := os.Stat(p.Path("inbox/idea.md")); !os.IsNotExist(err) {
		t.Fatal("the note stayed")
	}
	if !strings.Contains(read(t, p, th.Docs[0].Path), "It is read on every key.") {
		t.Fatal("the stub lacks the note's text")
	}
	if _, err := Start(p, New{From: "stubs/x.md"}, now); err == nil {
		t.Fatal("a note outside inbox/ was accepted")
	}
}

func TestTheStageIsTheFurthestDocument(t *testing.T) {
	p := newProject(t)
	th := start(t, p, "Fix it", "Do it.")
	// A stage may be skipped.
	th, err := File(p, th.ID, Filing{Stage: Plan, Text: "1. Do it."}, now)
	if err != nil || th.Stage != Plan || th.Doc(Spec) != nil {
		t.Fatalf("%+v %v", th, err)
	}
	// A skipped document may still be filed, and the stage stays.
	th, err = File(p, "fix", Filing{Stage: Spec, Text: "It works."}, now)
	if err != nil || th.Stage != Plan || len(th.Docs) != 3 {
		t.Fatalf("%+v %v", th, err)
	}
	if _, err := File(p, th.ID, Filing{Stage: Spec, Text: "again"}, now); err == nil || !strings.Contains(err.Error(), "revise it with Edit") {
		t.Fatalf("a second spec was filed: %v", err)
	}
	// Every document's callout names its siblings.
	if stub := read(t, p, "stubs/Fix it.md"); !strings.Contains(stub, "**Stub** → [Spec](<../specs/Fix it.md>) → [Plan](<../plans/Fix it.md>) → Receipt") {
		t.Fatal(stub)
	}
	for _, bad := range []Filing{{Stage: "done"}, {Stage: Receipt, Text: "x"}, {Stage: Receipt, Outcome: Completed}, {Stage: Spec, Outcome: Killed}} {
		if _, err := File(p, th.ID, bad, now); err == nil {
			t.Fatalf("%+v was accepted", bad)
		}
	}
	// Deleting a document by hand moves the thread back; nothing else holds the stage.
	os.Remove(p.Path("plans/Fix it.md"))
	board, err := Sync(p, now)
	if err != nil || board.Find(th.ID).Stage != Spec || !strings.Contains(read(t, p, th.Path), "stage: spec") {
		t.Fatalf("%+v %v", board.Find(th.ID), err)
	}
}

func TestAReceiptClosesAndReopenOpens(t *testing.T) {
	p := newProject(t)
	th := start(t, p, "Fix it", "Do it.")
	Set(p, th.ID, Changes{Blocked: ptr("the vendor")}, now)
	th, err := File(p, th.ID, Filing{Stage: Receipt, Outcome: Killed, Text: "Not worth it."}, now)
	if err != nil || !th.Closed() || th.Outcome != Killed || th.Path != "threads/archive/Fix it.md" || th.Blocked != "" {
		t.Fatalf("%+v %v", th, err)
	}
	receipt := read(t, p, "receipts/Fix it.md")
	if !strings.Contains(receipt, "> [!killed] Fix it · killed") || !strings.Contains(receipt, "outcome: killed") || !strings.Contains(receipt, "[Thread](<../threads/archive/Fix it.md>)") {
		t.Fatal(receipt)
	}
	// The stub's callout follows the card into the archive.
	if !strings.Contains(read(t, p, "stubs/Fix it.md"), "[Thread](<../threads/archive/Fix it.md>)") {
		t.Fatal("the stub still links the open card")
	}
	if card := read(t, p, th.Path); !strings.Contains(card, "**Killed**") || !strings.Contains(card, "[Receipt](<../../receipts/Fix it.md>)") {
		t.Fatal(card)
	}
	if index := read(t, p, project.ThreadsIndex); !strings.Contains(index, "> [!receipt] Closed · 1") || !strings.Contains(index, "No open threads") {
		t.Fatal(index)
	}
	if _, err := File(p, th.ID, Filing{Stage: Plan, Text: "x"}, now); err == nil {
		t.Fatal("a closed thread took a plan")
	}
	if _, err := Set(p, th.ID, Changes{Blocked: ptr("x")}, now); err == nil {
		t.Fatal("a closed thread was blocked")
	}
	th, err = Reopen(p, th.ID, now)
	if err != nil || th.Closed() || th.Stage != Stub || th.Path != "threads/Fix it.md" {
		t.Fatalf("%+v %v", th, err)
	}
	if _, err := Reopen(p, th.ID, now); err == nil {
		t.Fatal("an open thread was reopened")
	}
}

func TestSetKeepsHandEditsAndRenames(t *testing.T) {
	p := newProject(t)
	th := start(t, p, "Fix it", "Do it.")
	File(p, th.ID, Filing{Stage: Spec, Text: "It works."}, now)
	write(t, p, th.Path, strings.Replace(read(t, p, th.Path), "created:", "owner: nathan\ncreated:", 1))
	if _, err := CreatePhase(p, "Alpha", "", nil, now); err != nil {
		t.Fatal(err)
	}
	later := now.AddDate(0, 0, 3)
	th, err := Set(p, th.ID, Changes{Priority: ptr("high"), Phase: ptr("alpha"), Blocked: ptr("waits on\nthe vendor"), Title: ptr("Fix the login")}, later)
	if err != nil {
		t.Fatal(err)
	}
	if th.Priority != "high" || th.Phase != "Alpha" || th.Blocked != "waits on the vendor" || th.Title != "Fix the login" || th.Updated != "2026-09-16" || th.Path != "threads/Fix the login.md" {
		t.Fatalf("%+v", th)
	}
	if card := read(t, p, th.Path); !strings.Contains(card, "owner: nathan") || !strings.Contains(card, "> **Blocked:** waits on the vendor") {
		t.Fatal(card)
	}
	spec := read(t, p, "specs/Fix the login.md")
	if !strings.Contains(spec, `title: "Fix the login"`) || !strings.Contains(spec, "> [!spec] Fix the login") || !strings.Contains(spec, "It works.") {
		t.Fatal(spec)
	}
	if _, err := os.Stat(p.Path("stubs/Fix it.md")); !os.IsNotExist(err) {
		t.Fatal("the old stub stayed")
	}
	for _, bad := range []Changes{{Priority: ptr("urgent")}, {Phase: ptr("Nope")}, {Title: ptr(" ")}} {
		if _, err := Set(p, th.ID, bad, now); err == nil {
			t.Fatalf("%+v was accepted", bad)
		}
	}
	if th, _ := Set(p, th.ID, Changes{Blocked: ptr("")}, now); th.Blocked != "" {
		t.Fatal("unblock")
	}
}

func TestSyncKeepsProseAndTouchesNothingWhenCurrent(t *testing.T) {
	p := newProject(t)
	th := start(t, p, "Fix it", "Do it.")
	doc := th.Docs[0].Path
	// The model rewrote the page and dropped the callout; the user's own callout stays.
	write(t, p, doc, "---\ntype: stub\nthread: "+th.ID+"\ntitle: \"Fix it\"\ncreated: 2026-09-13\n---\n> [!note] Mine\n> kept\n\nNew words.\n")
	if _, err := Sync(p, now); err != nil {
		t.Fatal(err)
	}
	got := read(t, p, doc)
	if !strings.Contains(got, "> [!stub] Fix it") || !strings.Contains(got, "> [!note] Mine\n> kept\n\nNew words.") {
		t.Fatal(got)
	}
	old := now.Add(-time.Hour)
	for _, rel := range []string{doc, th.Path, project.ThreadsIndex} {
		os.Chtimes(p.Path(rel), old, old)
	}
	if _, err := Sync(p, now); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{doc, th.Path, project.ThreadsIndex} {
		if info, _ := os.Stat(p.Path(rel)); !info.ModTime().Equal(old) {
			t.Fatalf("%s was rewritten", rel)
		}
	}
}

func TestTouch(t *testing.T) {
	p := newProject(t)
	th := start(t, p, "Fix it", "Do it.")
	later := now.AddDate(0, 0, 2)
	if err := Touch(p, th.Docs[0].Path, later); err != nil {
		t.Fatal(err)
	}
	board, _ := Load(p)
	if board.Find(th.ID).Updated != "2026-09-15" {
		t.Fatalf("%+v", board.Find(th.ID))
	}
	if err := Touch(p, "phases/x.md", later); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReportsProblems(t *testing.T) {
	p := newProject(t)
	th := start(t, p, "Fix it", "Do it.")
	write(t, p, "specs/Orphan.md", "---\ntype: spec\nthread: thr-20200101-0000\ncreated: 2026-09-13\n---\n")
	write(t, p, "specs/Bare.md", "No frontmatter.\n")
	write(t, p, "stubs/Second.md", "---\ntype: stub\nthread: "+th.ID+"\ncreated: 2026-09-13\n---\n")
	write(t, p, "receipts/Bad.md", "---\ntype: receipt\nthread: "+th.ID+"\noutcome: finished\ncreated: 2026-09-13\n---\n")
	write(t, p, "threads/Empty.md", "---\ntype: thread\nthread_id: thr-20260913-aaaa\ntitle: \"Empty\"\ncreated: 2026-09-13\nupdated: 2026-09-13\n---\n")
	board, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, pr := range board.Problems {
		reasons[pr.Path] = pr.Reason
	}
	for rel, want := range map[string]string{
		"specs/Orphan.md": "no thread has the id", "specs/Bare.md": "no frontmatter", "stubs/Second.md": "already has a stub",
		"receipts/Bad.md": "outcome must be", "threads/Empty.md": "no documents",
	} {
		if !strings.Contains(reasons[rel], want) {
			t.Fatalf("%s: %q lacks %q", rel, reasons[rel], want)
		}
	}
	if board.Find(th.ID).Closed() {
		t.Fatal("a bad receipt closed the thread")
	}
}

func TestBoardOrderCountsAndResolve(t *testing.T) {
	p := newProject(t)
	a := start(t, p, "Alpha stub", "a")
	b := start(t, p, "Beta plan", "b")
	c := start(t, p, "Beta spec", "c")
	d := start(t, p, "Delta done", "d")
	File(p, b.ID, Filing{Stage: Plan, Text: "x"}, now.AddDate(0, 0, -20))
	File(p, c.ID, Filing{Stage: Spec, Text: "x"}, now)
	File(p, d.ID, Filing{Stage: Receipt, Outcome: Completed, Text: "x"}, now)
	Set(p, a.ID, Changes{Blocked: ptr("x")}, now)
	board, _ := Load(p)
	var order []string
	for _, th := range board.Open() {
		order = append(order, th.Title)
	}
	if strings.Join(order, ",") != "Beta plan,Beta spec,Alpha stub" {
		t.Fatal(order)
	}
	c2 := board.Counts(now)
	if c2.Open != 3 || c2.Stub != 1 || c2.Spec != 1 || c2.Plan != 1 || c2.Blocked != 1 || c2.Completed != 1 || c2.Killed != 0 || c2.Stale != 1 {
		t.Fatalf("%+v", c2)
	}
	if th, err := board.Resolve("alpha"); err != nil || th.ID != a.ID {
		t.Fatal(err)
	}
	if _, err := board.Resolve("beta"); err == nil || !strings.Contains(err.Error(), "matches 2") {
		t.Fatal(err)
	}
	if _, err := board.Resolve("nothing"); err == nil {
		t.Fatal("resolved nothing")
	}
	if index := RenderIndex(board, now); !strings.Contains(index, "stale") || strings.Index(index, "[!plan]") > strings.Index(index, "[!stub]") {
		t.Fatal(index)
	}
}

func TestPhases(t *testing.T) {
	p := newProject(t)
	if _, err := CreatePhase(p, "Alpha", "Ship it.", nil, now); err != nil {
		t.Fatal(err)
	}
	if page := read(t, p, "phases/Alpha.md"); !strings.Contains(page, "> [!phase] Alpha") || !strings.Contains(page, "Ship it.") {
		t.Fatal(page)
	}
	th, err := Start(p, New{Title: "Fix it", Phase: "alpha"}, now)
	if err != nil || th.Phase != "Alpha" {
		t.Fatalf("%+v %v", th, err)
	}
	if err := RemovePhase(p, "Alpha", now); err == nil || !strings.Contains(err.Error(), "1 thread still names") {
		t.Fatal(err)
	}
	if _, err := RenamePhase(p, "Alpha", "Beta", now); err != nil {
		t.Fatal(err)
	}
	board, _ := Load(p)
	if board.Find(th.ID).Phase != "Beta" || !strings.Contains(read(t, p, "phases/Beta.md"), "> [!phase] Beta") || len(board.Problems) != 0 {
		t.Fatalf("%+v", board)
	}
	if board.Finished("Beta") {
		t.Fatal("finished with an open thread")
	}
	File(p, th.ID, Filing{Stage: Receipt, Outcome: Completed, Text: "Done."}, now)
	board, _ = Load(p)
	if !board.Finished("Beta") || !strings.Contains(RenderIndex(board, now), "finished") {
		t.Fatal("not finished")
	}
	if ph, err := ReorderPhase(p, "Beta", 5, now); err != nil || ph.Order != 5 {
		t.Fatal(err)
	}
}

func TestMigrate(t *testing.T) {
	p := newProject(t)
	task := func(status, id, body string) string {
		return "---\ntype: task\ntitle: \"T " + status + "\"\nstatus: " + status + "\npriority: high\nphase: \"\"\ndue: \"\"\ncreated: 2026-09-01\nupdated: 2026-09-02\ntask_id: " + id + "\n---\n\n# T " + status + "\n\n" + body
	}
	write(t, p, "tasks/T planted.md", task("planted", "task-20260901-ab12", "## Idea\n\nDo it.\n"))
	write(t, p, "tasks/T active.md", task("active", "task-20260901-ab13", "## Idea\n\nDo it.\n\n## Plan\n\n1. Step.\n\n## Progress\n\n- 2026-09-02 · started\n"))
	write(t, p, "tasks/archive/T done.md", task("done", "task-20260901-ab14", "## Idea\n\nDo it.\n\n## Plan\n\n1. Step.\n\n## Outcome\n\nShipped.\n"))
	write(t, p, "tasks/archive/T cancelled.md", task("cancelled", "task-20260901-ab15", "## Idea\n\nDo it.\n"))
	write(t, p, "tasks/tasks.md", "# Tasks\n")
	if !Legacy(p) {
		t.Fatal("Legacy")
	}
	moved, left, err := Migrate(p, now)
	if err != nil || moved != 4 || len(left) != 0 {
		t.Fatalf("%d %+v %v", moved, left, err)
	}
	if _, err := os.Stat(p.Path("tasks")); !os.IsNotExist(err) || Legacy(p) {
		t.Fatal("tasks/ stayed")
	}
	board, _ := Load(p)
	if len(board.Problems) != 0 {
		t.Fatalf("%+v", board.Problems)
	}
	want := map[string][2]string{"thr-20260901-ab12": {Stub, ""}, "thr-20260901-ab13": {Plan, ""}, "thr-20260901-ab14": {Receipt, Completed}, "thr-20260901-ab15": {Receipt, Killed}}
	for id, w := range want {
		th := board.Find(id)
		if th == nil || th.Stage != w[0] || th.Outcome != w[1] || th.Priority != "high" || th.Created != "2026-09-01" {
			t.Fatalf("%s: %+v", id, th)
		}
	}
	if plan := read(t, p, "plans/T active.md"); !strings.Contains(plan, "1. Step.") || !strings.Contains(plan, "## Progress\n\n- 2026-09-02 · started") {
		t.Fatal(plan)
	}
	if !strings.Contains(read(t, p, "receipts/T done.md"), "Shipped.") {
		t.Fatal("the receipt lacks the outcome")
	}
}
