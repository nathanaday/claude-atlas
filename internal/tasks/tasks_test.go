package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/project"
)

var now = time.Date(2026, 9, 13, 12, 0, 0, 0, time.Local)

func page(status, folder, id, extra string) (string, []byte) {
	p := folder + "/Fix it.md"
	text := "---\ntype: task\ntitle: \"Fix it\"\nstatus: " + status + "\npriority: high\nphase: \"\"\ndue: \"\"\ncreated: 2026-09-01\nupdated: 2026-09-02\ntask_id: " + id + "\n---\n\n# Fix it\n\n## Idea\n\nDo it.\n" + extra
	return p, []byte(text)
}

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

func write(t *testing.T, p *project.Project, rel string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p.Path(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Path(rel), data, 0o644); err != nil {
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

func TestParseEnforcesTheRules(t *testing.T) {
	p, text := page("planted", project.TasksDir, "task-20260901-ab12", "")
	task, err := Parse(p, text)
	if err != nil || task.ID != "task-20260901-ab12" || task.Status != "planted" || task.Priority != "high" || task.HasPlan || task.Phase != "" {
		t.Fatalf("%+v %v", task, err)
	}
	if !IsPage(p) || IsPage(project.TasksIndex) || IsPage("tasks/deep/x.md") || IsPage("phases/x.md") || IsPage("tasks/notes.txt") {
		t.Fatal("IsPage")
	}
	if !IsPhasePage("phases/Alarm quality.md") || IsPhasePage("tasks/x.md") || IsPhasePage("phases/x.txt") {
		t.Fatal("IsPhasePage")
	}
	bad := map[string][2]string{
		"bad status":       {"soon", project.TasksDir},
		"done in open":     {"done", project.TasksDir},
		"active archived":  {"active", project.ArchiveDir},
		"bad id":           {"planted", project.TasksDir},
		"cancelled stays":  {"cancelled", project.ArchiveDir},
		"planned has plan": {"planned", project.TasksDir},
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
	p, text = page("planned", project.TasksDir, "task-20260901-ab12", "\n## Plan\n\n1. Read.\n\n## Progress\n")
	if task, _ := Parse(p, text); !task.HasPlan {
		t.Fatal("plan detected")
	}
	p, text = page("planned", project.TasksDir, "task-20260901-ab12", "\n## Plan\n\n\n## Progress\n\n- x\n")
	if task, _ := Parse(p, text); task.HasPlan {
		t.Fatal("an empty plan section is no plan")
	}
	if _, err := Parse("tasks/x.md", []byte("---\ntype: concept\ntitle: x\nstatus: seed\ncreated: 2026-01-01\nupdated: 2026-01-01\n---\n")); err == nil || !strings.Contains(err.Error(), "type: task") {
		t.Fatalf("type check: %v", err)
	}
	if _, err := Parse("tasks/x.md", []byte("no frontmatter")); err == nil || !strings.Contains(err.Error(), "no frontmatter") {
		t.Fatalf("frontmatter: %v", err)
	}
	withPhase := func(yaml string) (*Task, error) {
		return Parse(project.TasksDir+"/Fix it.md", []byte("---\ntype: task\ntitle: \"Fix it\"\nstatus: planted\ncreated: 2026-09-01\nupdated: 2026-09-02\ntask_id: task-20260901-ab12\n"+yaml+"---\n\n# Fix it\n"))
	}
	if task, err := withPhase("phase: \"Alarm quality\"\ndue: 2026-10-01\n"); err != nil || task.Phase != "Alarm quality" || task.Due != "2026-10-01" || task.Priority != "normal" {
		t.Fatalf("phase and due: %+v %v", task, err)
	}
	if _, err := withPhase("due: soon\n"); err == nil || !strings.Contains(err.Error(), "due must be") {
		t.Fatalf("bad due: %v", err)
	}
	if _, err := withPhase("priority: urgent\n"); err == nil || !strings.Contains(err.Error(), "priority must be") {
		t.Fatalf("bad priority: %v", err)
	}
	if task, err := Parse("tasks/Untitled.md", []byte("---\ntype: task\nstatus: planted\ncreated: 2026-09-01\nupdated: 2026-09-02\ntask_id: task-20260901-ab12\n---\n")); err != nil || task.Title != "Untitled" {
		t.Fatalf("a page without a title takes its stem: %+v %v", task, err)
	}
}

func TestParsePhase(t *testing.T) {
	ph, err := ParsePhase("phases/Alarm quality.md", []byte(PhaseSkeleton("Alarm quality", "Fewer false alarms.", 2, now)))
	if err != nil || ph.Title != "Alarm quality" || ph.Order != 2 || ph.Created != "2026-09-13" || ph.Updated != "2026-09-13" {
		t.Fatalf("%+v %v", ph, err)
	}
	if _, err := ParsePhase("tasks/x.md", []byte("---\ntype: phase\n---\n")); err == nil {
		t.Fatal("a phase page sits under phases/")
	}
	if _, err := ParsePhase("phases/x.md", []byte("---\ntype: task\ntitle: x\n---\n")); err == nil || !strings.Contains(err.Error(), "type: phase") {
		t.Fatalf("type: %v", err)
	}
	if _, err := ParsePhase("phases/x.md", []byte("---\ntype: phase\ntitle: x\norder: soon\n---\n")); err == nil || !strings.Contains(err.Error(), "whole number") {
		t.Fatalf("order: %v", err)
	}
	if ph, err := ParsePhase("phases/x.md", []byte("---\ntype: phase\norder: \"3\"\n---\n")); err != nil || ph.Order != 3 || ph.Title != "x" {
		t.Fatalf("a quoted order and a missing title: %+v %v", ph, err)
	}
	if ph, err := ParsePhase("phases/x.md", []byte("---\ntype: phase\ntitle: x\n---\n")); err != nil || ph.Order != 0 {
		t.Fatalf("no order is 0: %+v %v", ph, err)
	}
	if _, err := ParsePhase("phases/x.md", []byte("no frontmatter")); err == nil {
		t.Fatal("frontmatter required")
	}
}

func TestTitleFromTextSkeletonAndPagePath(t *testing.T) {
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
	text := Skeleton(Plant{Title: "Fix \"it\"", Text: "Why.\n\nHow.", Phase: "Alarm quality", Due: "2026-10-01"}, "task-20260913-3f2a", now)
	task, err := Parse(project.TasksDir+"/Fix it.md", []byte(text))
	if err != nil || task.Title != "Fix \"it\"" || task.Phase != "Alarm quality" || task.Due != "2026-10-01" || task.Priority != "normal" || task.Status != "planted" {
		t.Fatalf("skeleton parses: %+v %v\n%s", task, err, text)
	}
	if !strings.Contains(text, "## Idea\n\nWhy.\n\nHow.\n") || strings.Contains(text, "## Plan") || strings.Contains(text, "repos") || strings.Contains(text, "workdir") {
		t.Fatalf("idea verbatim, no plan, no v2 fields:\n%s", text)
	}
	planned := Plant{Title: "Ship", Text: "Now.", Plan: "1. Build.\n2. Test."}
	if planned.Status() != "planned" || (Plant{}).Status() != "planted" || (Plant{Plan: "x", Start: true}).Status() != "active" {
		t.Fatal("Status")
	}
	text = Skeleton(planned, "task-20260913-3f2a", now)
	task, err = Parse(project.TasksDir+"/Ship.md", []byte(text))
	if err != nil || task.Status != "planned" || !task.HasPlan {
		t.Fatalf("planned skeleton: %+v %v\n%s", task, err, text)
	}
	if !strings.Contains(text, "## Idea\n\nNow.\n\n## Plan\n\n1. Build.\n2. Test.\n") || strings.Contains(text, "## Progress") {
		t.Fatalf("planned page:\n%s", text)
	}
	planned.Start = true
	text = Skeleton(planned, "task-20260913-3f2a", now)
	if task, err = Parse(project.TasksDir+"/Ship.md", []byte(text)); err != nil || task.Status != "active" || !strings.Contains(text, "## Progress\n\n- "+now.Format("2006-01-02")+" · started\n") {
		t.Fatalf("started page: %+v %v\n%s", task, err, text)
	}
	if !idPattern.MatchString(NewID(now)) {
		t.Fatal("NewID")
	}
	taken := map[string]bool{project.TasksDir + "/Fix it.md": true, project.ArchiveDir + "/Fix it (2).md": true}
	if got := PagePath("Fix it", func(p string) bool { return taken[p] }); got != project.TasksDir+"/Fix it (3).md" {
		t.Fatalf("path %q", got)
	}
	if got := PagePath("tasks", func(string) bool { return false }); got != project.TasksDir+"/tasks (2).md" {
		t.Errorf("a task titled after the index takes another name: %q", got)
	}
	if !strings.Contains(PhaseSkeleton("x", "", 1, now), "What this phase delivers") {
		t.Fatal("a phase without a goal gets a prompt for one")
	}
}

func TestLoadReportsProblemsAndOrdersPhases(t *testing.T) {
	p := newProject(t)
	board, err := Load(p)
	if err != nil || len(board.Tasks) != 0 || len(board.Phases) != 0 || len(board.Problems) != 0 {
		t.Fatalf("empty board: %+v %v", board, err)
	}
	rel, text := page("active", project.TasksDir, "task-20260901-ab12", "\n## Plan\n\n1. Go.\n")
	write(t, p, rel, text)
	write(t, p, project.ArchiveDir+"/Old.md", []byte(strings.Replace(strings.Replace(string(text), "status: active", "status: done", 1), "task-20260901-ab12", "task-20260801-cd34", 1)))
	write(t, p, project.TasksDir+"/broken.md", []byte("no frontmatter"))
	write(t, p, project.TasksDir+"/dup.md", []byte(strings.Replace(string(text), "Fix it", "Dup", -1)))
	write(t, p, project.TasksDir+"/.hidden.md", text)
	write(t, p, project.TasksDir+"/notes.txt", []byte("x"))
	write(t, p, project.TasksDir+"/Lost.md", []byte(strings.Replace(strings.Replace(string(text), "phase: \"\"", "phase: \"Nowhere\"", 1), "task-20260901-ab12", "task-20260901-ef56", 1)))
	write(t, p, project.PhasesDir+"/Beta.md", []byte(PhaseSkeleton("Beta", "", 2, now)))
	write(t, p, project.PhasesDir+"/Alpha.md", []byte(PhaseSkeleton("Alpha", "", 1, now)))
	write(t, p, project.PhasesDir+"/alpha again.md", []byte(PhaseSkeleton("alpha", "", 3, now)))
	write(t, p, project.PhasesDir+"/bad.md", []byte("---\ntype: task\n---\n"))
	board, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Tasks) != 3 || board.Find("task-20260901-ab12") == nil || board.Find("task-20260801-cd34") == nil || board.FindByTitle("fix IT") == nil {
		t.Fatalf("tasks %+v", board.Tasks)
	}
	if len(board.Phases) != 2 || board.Phases[0].Title != "Alpha" || board.Phases[1].Title != "Beta" || board.Phase("beta") == nil || board.Phase("Gamma") != nil {
		t.Fatalf("phases %+v", board.Phases)
	}
	reasons := map[string]string{}
	for _, pr := range board.Problems {
		reasons[pr.Path] = pr.Reason
	}
	if !strings.Contains(reasons["tasks/broken.md"], "no frontmatter") || !strings.Contains(reasons["tasks/dup.md"], "also used by") || !strings.Contains(reasons["phases/alpha again.md"], "also the title") || !strings.Contains(reasons["phases/bad.md"], "type: phase") || !strings.Contains(reasons["tasks/Lost.md"], "has no page") {
		t.Fatalf("problems %+v", board.Problems)
	}
	if len(board.Open()) != 2 || board.Open()[0].Title != "Fix it" || len(board.Archived()) != 1 {
		t.Fatalf("open %+v archived %+v", board.Open(), board.Archived())
	}
	if len(board.Unphased()) != 2 || len(board.In("Alpha")) != 0 {
		t.Fatal("a task naming a missing phase is unphased")
	}
	counts := board.Counts(now)
	if counts.Open != 2 || counts.Active != 2 || counts.Done != 1 || counts.Stale != 0 || counts.Phases != 0 {
		t.Fatalf("counts %+v", counts)
	}
	later := now.AddDate(0, 0, 20)
	if !Stale(*board.Find("task-20260901-ab12"), later) || board.Counts(later).Stale != 2 {
		t.Fatal("active for 20 days is stale")
	}
	os.WriteFile(p.Path(project.InboxDir+"/a.md"), []byte("x"), 0o644)
	os.WriteFile(p.Path(project.InboxDir+"/.hidden"), []byte("x"), 0o644)
	os.MkdirAll(p.Path(project.InboxDir+"/sub"), 0o755)
	os.WriteFile(p.Path(project.InboxDir+"/sub/b.txt"), []byte("x"), 0o644)
	if notes := Notes(p); strings.Join(notes, ",") != "inbox/a.md,inbox/sub/b.txt" {
		t.Fatalf("notes %v", notes)
	}
}

func TestPlantTaskWritesThePageAndRemovesTheNote(t *testing.T) {
	p := newProject(t)
	if _, err := PlantTask(p, Plant{}, now); err == nil {
		t.Fatal("a plant needs a title or text")
	}
	if _, err := PlantTask(p, Plant{Title: "x", Priority: "urgent"}, now); err == nil {
		t.Fatal("bad priority")
	}
	if _, err := PlantTask(p, Plant{Title: "x", Due: "soon"}, now); err == nil {
		t.Fatal("bad due")
	}
	if _, err := PlantTask(p, Plant{Title: "x", Start: true}, now); err == nil {
		t.Fatal("start needs a plan")
	}
	if _, err := PlantTask(p, Plant{Title: "x", Phase: "Nowhere"}, now); err == nil || !strings.Contains(err.Error(), "no phase named") {
		t.Fatalf("unknown phase: %v", err)
	}
	if _, err := PlantTask(p, Plant{Title: "x", From: "tasks/nope.md"}, now); err == nil || !strings.Contains(err.Error(), "not a note under") {
		t.Fatalf("from outside the inbox: %v", err)
	}
	if _, err := CreatePhase(p, "Alarm quality", "Fewer false alarms.", nil, now); err != nil {
		t.Fatal(err)
	}
	write(t, p, project.InboxDir+"/note.md", []byte("# Filter vehicle alarms\n\nCars on the road trip the detector.\n"))
	task, err := PlantTask(p, Plant{Text: "# Filter vehicle alarms\n\nCars on the road trip the detector.\n", Phase: "alarm QUALITY", Priority: "high", Due: "2026-10-01", From: "inbox/note.md"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "Filter vehicle alarms" || task.Path != "tasks/Filter vehicle alarms.md" || task.Phase != "Alarm quality" || task.Priority != "high" || task.Due != "2026-10-01" || task.Status != "planted" || !idPattern.MatchString(task.ID) {
		t.Fatalf("planted %+v", task)
	}
	if _, err := os.Stat(p.Path("inbox/note.md")); !os.IsNotExist(err) {
		t.Fatal("the note is removed")
	}
	if !strings.Contains(read(t, p, task.Path), "Cars on the road trip the detector.") {
		t.Fatal("the text is on the page verbatim")
	}
	index := read(t, p, project.TasksIndex)
	if !strings.Contains(index, "## Alarm quality (1 open)") || !strings.Contains(index, "[Filter vehicle alarms](<Filter vehicle alarms.md>)") {
		t.Fatalf("index:\n%s", index)
	}
	// A second task with the same title takes another path; a planned one has a plan.
	second, err := PlantTask(p, Plant{Title: "Filter vehicle alarms", Text: "Again.", Plan: "1. Look."}, now)
	if err != nil || second.Path != "tasks/Filter vehicle alarms (2).md" || second.Status != "planned" || !second.HasPlan || second.ID == task.ID {
		t.Fatalf("second %+v %v", second, err)
	}
	// A clone without the folders still plants.
	os.RemoveAll(p.Path(project.InboxDir))
	if _, err := PlantTask(p, Plant{Title: "Later"}, now); err != nil {
		t.Fatal(err)
	}
}

func TestSetChangesFrontmatterMovesThePageAndKeepsTheBody(t *testing.T) {
	// BUG REPORT: tasks.setField joins "\n---" + body, but vault.SplitFrontmatter's body
	// starts after the fence line's newline, so the blank line after the fence is lost and
	// a second setField on the same page yields "---# Title", which no longer parses. The
	// fix is "\n---\n" + body. Remove this skip once it lands.
	p := newProject(t)
	task, err := PlantTask(p, Plant{Title: "Fix it", Text: "Do it."}, now)
	if err != nil {
		t.Fatal(err)
	}
	body := "\n## Plan\n\n1. Go.\n\n## Progress\n\n- note with: colon\n"
	write(t, p, task.Path, []byte(strings.Replace(read(t, p, task.Path), "\n---\n", "\ncustom: kept\n---\n", 1)+body))
	str := func(s string) *string { return &s }
	if _, err := Set(p, "task-00000000-0000", Changes{}, now); err == nil || !strings.Contains(err.Error(), "no task") {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := Set(p, task.ID, Changes{Status: str("soon")}, now); err == nil {
		t.Fatal("bad status")
	}
	if _, err := Set(p, task.ID, Changes{Priority: str("urgent")}, now); err == nil {
		t.Fatal("bad priority")
	}
	if _, err := Set(p, task.ID, Changes{Due: str("soon")}, now); err == nil {
		t.Fatal("bad due")
	}
	if _, err := Set(p, task.ID, Changes{Phase: str("Nowhere")}, now); err == nil || !strings.Contains(err.Error(), "no phase named") {
		t.Fatalf("unknown phase: %v", err)
	}
	if _, err := CreatePhase(p, "Alpha", "", nil, now); err != nil {
		t.Fatal(err)
	}
	later := now.AddDate(0, 0, 3)
	got, err := Set(p, task.ID, Changes{Status: str("active"), Priority: str("high"), Phase: str("alpha"), Due: str("2026-10-01")}, later)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "active" || got.Priority != "high" || got.Phase != "Alpha" || got.Due != "2026-10-01" || got.Updated != "2026-09-16" || got.Path != task.Path || !got.HasPlan {
		t.Fatalf("set %+v", got)
	}
	text := read(t, p, task.Path)
	if !strings.Contains(text, "custom: kept\n") || !strings.HasSuffix(text, body) || strings.Count(text, "status:") != 1 || !strings.Contains(text, "phase: \"Alpha\"\n") {
		t.Fatalf("frontmatter rewritten in place, body kept:\n%s", text)
	}
	// The title finds a task too, and done moves it to the archive.
	done, err := Set(p, "FIX IT", Changes{Status: str("done")}, later)
	if err != nil || done.Path != project.ArchiveDir+"/Fix it.md" || done.Status != "done" {
		t.Fatalf("done %+v %v", done, err)
	}
	if _, err := os.Stat(p.Path(task.Path)); !os.IsNotExist(err) {
		t.Fatal("the open page is gone")
	}
	index := read(t, p, project.TasksIndex)
	if !strings.Contains(index, "## Archive") || !strings.Contains(index, "[Fix it](<archive/Fix it.md>) | done | Alpha | 2026-09-16") || !strings.Contains(index, "## Finished phases") || !strings.Contains(index, "| Alpha | 1 | 2026-09-16 |") {
		t.Fatalf("index:\n%s", index)
	}
	// Back to open moves it back, and a taken name counts up.
	write(t, p, project.TasksDir+"/Fix it.md", []byte(strings.Replace(read(t, p, done.Path), done.ID, "task-20260901-ab12", 1)))
	write(t, p, project.TasksDir+"/Fix it.md", []byte(strings.Replace(read(t, p, project.TasksDir+"/Fix it.md"), "status: done", "status: planted", 1)))
	back, err := Set(p, done.ID, Changes{Status: str("blocked")}, later)
	if err != nil || back.Path != project.TasksDir+"/Fix it (2).md" || back.Status != "blocked" {
		t.Fatalf("back %+v %v", back, err)
	}
	if _, err := os.Stat(p.Path(done.Path)); !os.IsNotExist(err) {
		t.Fatal("the archived page is gone")
	}
	// Clearing the phase and the due date.
	cleared, err := Set(p, back.ID, Changes{Phase: str(""), Due: str("")}, later)
	if err != nil || cleared.Phase != "" || cleared.Due != "" {
		t.Fatalf("cleared %+v %v", cleared, err)
	}
}

func TestSetFieldRewritesOneLine(t *testing.T) {
	// BUG REPORT: tasks.setField joins "\n---" + body, but vault.SplitFrontmatter's body
	// starts after the fence line's newline, so the blank line after the fence is lost and
	// a second setField on the same page yields "---# Title", which no longer parses. The
	// fix is "\n---\n" + body. Remove this skip once it lands.
	content := "---\ntype: task\nstatus: planted\ncustom: kept\n---\n\n# T\n\nstatus: in the body stays\n"
	out := setField(content, "status", "active")
	if !strings.Contains(out, "---\ntype: task\nstatus: active\ncustom: kept\n---\n\n# T\n\nstatus: in the body stays\n") {
		t.Fatalf("replace:\n%s", out)
	}
	out = setField(content, "phase", "\"Alpha\"")
	if !strings.Contains(out, "custom: kept\nphase: \"Alpha\"\n---\n") {
		t.Fatalf("append:\n%s", out)
	}
	if setField("no frontmatter", "x", "y") != "no frontmatter" {
		t.Fatal("a page without frontmatter is untouched")
	}
}

func TestPhasesCreateAndRemove(t *testing.T) {
	p := newProject(t)
	if _, err := CreatePhase(p, "  ", "", nil, now); err == nil {
		t.Fatal("a phase needs a title")
	}
	first, err := CreatePhase(p, "Alarm quality", "Fewer false alarms.", nil, now)
	if err != nil || first.Order != 1 || first.Path != "phases/Alarm quality.md" {
		t.Fatalf("first %+v %v", first, err)
	}
	if !strings.Contains(read(t, p, first.Path), "## Goal\n\nFewer false alarms.\n") {
		t.Fatal("the goal is on the page")
	}
	five := 5
	second, err := CreatePhase(p, "Release", "", &five, now)
	if err != nil || second.Order != 5 {
		t.Fatalf("second %+v %v", second, err)
	}
	third, err := CreatePhase(p, "Polish", "", nil, now)
	if err != nil || third.Order != 6 {
		t.Fatalf("the next order follows the highest: %+v %v", third, err)
	}
	if _, err := CreatePhase(p, "alarm quality", "", nil, now); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("a title is unique without regard to case: %v", err)
	}
	if _, err := PlantTask(p, Plant{Title: "Filter vehicles", Phase: "Alarm quality"}, now); err != nil {
		t.Fatal(err)
	}
	if err := RemovePhase(p, "Alarm quality", now); err == nil || !strings.Contains(err.Error(), "still name") || !strings.Contains(err.Error(), "Filter vehicles") {
		t.Fatalf("remove while named: %v", err)
	}
	if err := RemovePhase(p, "Nowhere", now); err == nil {
		t.Fatal("remove needs a phase")
	}
	if err := RemovePhase(p, "Polish", now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Path(third.Path)); !os.IsNotExist(err) {
		t.Fatal("the page is gone")
	}
	index := read(t, p, project.TasksIndex)
	if strings.Contains(index, "Polish") || !strings.Contains(index, "## Release (0 open)") || !strings.Contains(index, "## Alarm quality (1 open)") {
		t.Fatalf("index:\n%s", index)
	}
}

func TestPhasesRenameAndReorder(t *testing.T) {
	// BUG REPORT: tasks.setField joins "\n---" + body, but vault.SplitFrontmatter's body
	// starts after the fence line's newline, so the blank line after the fence is lost and
	// a second setField on the same page yields "---# Title", which no longer parses. The
	// fix is "\n---\n" + body. Remove this skip once it lands.
	p := newProject(t)
	first, err := CreatePhase(p, "Alarm quality", "Fewer false alarms.", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePhase(p, "Release", "", nil, now); err != nil {
		t.Fatal(err)
	}
	if _, err := CreatePhase(p, "Polish", "", nil, now); err != nil {
		t.Fatal(err)
	}
	task, err := PlantTask(p, Plant{Title: "Filter vehicles", Phase: "Alarm quality"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RenamePhase(p, "Nowhere", "x", now); err == nil {
		t.Fatal("rename needs a phase")
	}
	if _, err := RenamePhase(p, "Alarm quality", "Release", now); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("rename onto a taken title: %v", err)
	}
	renamed, err := RenamePhase(p, "alarm quality", "Alarm filtering", now.AddDate(0, 0, 1))
	if err != nil || renamed.Title != "Alarm filtering" || renamed.Path != "phases/Alarm filtering.md" || renamed.Order != 1 || renamed.Updated != "2026-09-14" {
		t.Fatalf("renamed %+v %v", renamed, err)
	}
	if _, err := os.Stat(p.Path(first.Path)); !os.IsNotExist(err) {
		t.Fatal("the old page is gone")
	}
	if !strings.Contains(read(t, p, renamed.Path), "title: \"Alarm filtering\"") || !strings.Contains(read(t, p, renamed.Path), "Fewer false alarms.") {
		t.Fatal("the page keeps its goal under the new title")
	}
	board, _ := Load(p)
	if got := board.Find(task.ID); got.Phase != "Alarm filtering" {
		t.Fatalf("the task follows the rename: %+v", got)
	}
	if _, err := ReorderPhase(p, "Nowhere", 1, now); err == nil {
		t.Fatal("reorder needs a phase")
	}
	if ph, err := ReorderPhase(p, "Alarm filtering", 9, now); err != nil || ph.Order != 9 {
		t.Fatalf("reordered %+v %v", ph, err)
	}
	board, _ = Load(p)
	if board.Phases[0].Title != "Release" || board.Phases[2].Title != "Alarm filtering" {
		t.Fatalf("order %+v", board.Phases)
	}
}

func TestRenderIndexGroups(t *testing.T) {
	p := newProject(t)
	empty, _ := Load(p)
	if out := RenderIndex(empty, now); !strings.Contains(out, "## Open\n\nNo open tasks.") || !strings.Contains(out, "## Archive\n\nNothing finished yet.") {
		t.Fatalf("empty:\n%s", out)
	}
	write(t, p, project.PhasesDir+"/Beta.md", []byte(PhaseSkeleton("Beta", "", 2, now)))
	write(t, p, project.PhasesDir+"/Alpha.md", []byte(PhaseSkeleton("Alpha", "", 1, now)))
	task := func(title, status, phase, priority, due, updated, id string) []byte {
		return []byte("---\ntype: task\ntitle: " + title + "\nstatus: " + status + "\npriority: " + priority + "\nphase: \"" + phase + "\"\ndue: \"" + due + "\"\ncreated: 2026-09-01\nupdated: " + updated + "\ntask_id: " + id + "\n---\n\n# " + title + "\n")
	}
	write(t, p, project.TasksDir+"/A task.md", task("A task", "active", "Alpha", "low", "", "2026-08-24", "task-20260901-0001"))
	write(t, p, project.ArchiveDir+"/B task.md", task("B task", "cancelled", "Beta", "normal", "", "2026-09-13", "task-20260901-0002"))
	write(t, p, project.TasksDir+"/Loose.md", task("Loose", "planted", "", "normal", "2026-10-01", "2026-09-13", "task-20260901-0003"))
	write(t, p, project.TasksDir+"/broken.md", []byte("x"))
	board, _ := Load(p)
	out := RenderIndex(board, now)
	sections := []string{"# Tasks", "## Alpha (1 open)", "| [A task](<A task.md>) | active · stale | low | — | 2026-08-24 |", "## No phase", "| [Loose](<Loose.md>) | planted | normal | 2026-10-01 |", "## Finished phases", "| Beta | 1 | 2026-09-13 |", "## Archive", "| [B task](<archive/B task.md>) | cancelled | Beta | 2026-09-13 |", "## Not readable", "`tasks/broken.md`"}
	last := -1
	for _, want := range sections {
		i := strings.Index(out, want)
		if i < 0 {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
		if i < last {
			t.Fatalf("%q out of order in:\n%s", want, out)
		}
		last = i
	}
	if strings.Contains(out, "## Beta") || strings.Contains(out, "## Open\n") {
		t.Fatalf("a finished phase has no open section and a board with phases says No phase:\n%s", out)
	}
	if !strings.Contains(out, "do not edit") {
		t.Fatal("the index says it is generated")
	}
	// WriteIndex writes the same text.
	if _, err := WriteIndex(p, now); err != nil {
		t.Fatal(err)
	}
	if read(t, p, project.TasksIndex) != out {
		t.Fatal("WriteIndex writes RenderIndex")
	}
}
