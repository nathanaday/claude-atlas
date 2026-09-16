package txn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func writeFile(t *testing.T, v *vault.Vault, rel, text string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(v.Path(rel)), 0o755)
	if err := os.WriteFile(v.Path(rel), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStubPagesRefusesWhatIsNotWanted(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "wiki/concepts/Training.md", string(mkpage("Training", "# Training\n\nSee [[Gradient Clipping]] and [[Traning]].\n")))
	cases := []struct {
		title, pageType, want string
	}{
		{"Unlinked", "", `nothing in the wiki links to "Unlinked"`},
		{"Traning", "", `"Traning" nearly matches the page "Training"`},
		{"Training", "", `"Training" already has a page: wiki/concepts/Training.md`},
		{"Gradient Clipping", "source", "ingest"},
		{"Gradient Clipping", "task", `type "task" is not filed`},
	}
	for _, c := range cases {
		_, _, _, err := StubRequest(v, []StubTitle{{Title: c.title, Type: c.pageType}}, "", nil, now)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s (%s): got %v, want %q", c.title, c.pageType, err, c.want)
		}
	}
}

func TestStubPagesCreatesWantedPagesAndMovesClickedOnes(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "wiki/concepts/Training.md", string(mkpage("Training", "# Training\n\nSuffers from the [[vanishing gradient problem]] and needs [[Gradient Clipping]]; see [[Clicked]].\n")))
	writeFile(t, v, "wiki/Clicked.md", "")

	res, err := StubPages(v, []StubTitle{{Title: "gradient clipping", Type: "entity"}}, "", nil, now)
	if err != nil || len(res.Stubs) != 1 || res.Stubs[0] != (Stubbed{Title: "Gradient Clipping", Type: "entity", Path: "wiki/entities/Gradient Clipping.md"}) || res.OperationID == "" || res.Commit == "" {
		t.Fatalf("one stub: %+v %v", res, err)
	}
	if page := read(t, v, "wiki/entities/Gradient Clipping.md"); !strings.Contains(page, "status: seed") || !strings.Contains(page, "# Gradient Clipping") {
		t.Fatalf("stub page:\n%s", page)
	}
	ops, _ := History(v, 1, false)
	if ops[0].Kind != "stub" || ops[0].Summary != "stub Gradient Clipping" {
		t.Fatalf("history %+v", ops[0])
	}

	res, err = StubPages(v, nil, "", nil, now)
	want := []Stubbed{
		{Title: "vanishing gradient problem", Type: "concept", Path: "wiki/concepts/vanishing gradient problem.md"},
		{Title: "Clicked", Type: "concept", Path: "wiki/concepts/Clicked.md"},
	}
	if err != nil || len(res.Stubs) != 2 || res.Stubs[0] != want[0] || res.Stubs[1] != want[1] {
		t.Fatalf("every wanted page: %+v %v", res, err)
	}
	if _, err := os.Stat(v.Path("wiki/Clicked.md")); err == nil {
		t.Fatal("the empty file moves to its routed folder")
	}
	if !strings.Contains(read(t, v, "wiki/concepts/Clicked.md"), "status: seed") {
		t.Fatal("the clicked page gets frontmatter")
	}
	ops, _ = History(v, 1, false)
	if ops[0].Summary != "stub 2 pages: vanishing gradient problem, Clicked" {
		t.Fatalf("summary %q", ops[0].Summary)
	}
	if dirty, _ := v.Repo().Dirty(); dirty {
		t.Fatal("the stub operation commits everything it touched")
	}

	if _, err := UndoOperation(v, ops[0].ID, now); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(v.Path("wiki/Clicked.md")); err != nil || len(data) != 0 {
		t.Fatalf("undo restores the empty file: %q %v", data, err)
	}
}

func TestStubPagesReplacesAnEmptyPageAtItsRoutedPath(t *testing.T) {
	v := newVault(t)
	if res, err := StubPages(v, nil, "", nil, now); err != nil || len(res.Stubs) != 0 || res.Stubs == nil || res.OperationID != "" {
		t.Fatalf("nothing wanted: %+v %v", res, err)
	}
	writeFile(t, v, "wiki/concepts/Linker.md", string(mkpage("Linker", "# Linker\n\n[[In Place]]\n")))
	writeFile(t, v, "wiki/concepts/In Place.md", "")
	res, err := StubPages(v, nil, "", nil, now)
	if err != nil || len(res.Stubs) != 1 || res.Stubs[0].Path != "wiki/concepts/In Place.md" {
		t.Fatalf("in place: %+v %v", res, err)
	}
	if !strings.Contains(read(t, v, "wiki/concepts/In Place.md"), "title: \"In Place\"") {
		t.Fatal("the empty page is replaced with the skeleton")
	}
	ops, err := History(v, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UndoOperation(v, ops[0].ID, now); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(v.Path("wiki/concepts/In Place.md")); err != nil || len(data) != 0 {
		t.Fatalf("undo restores the empty page: %q %v", data, err)
	}
}

func TestStubConflictsWhenTheUserTypesIntoTheEmptyPage(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "wiki/concepts/Later.md", string(mkpage("Later", "# Later\n\n[[Typed Into]]\n")))
	writeFile(t, v, "wiki/Typed Into.md", "")
	req, _, _, err := StubRequest(v, nil, "", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Prepare(v, req, now)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, v, "wiki/Typed Into.md", "Started writing.\n")
	if _, err := Apply(v, plan, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("apply after the user typed: %v", err)
	}
	if data, _ := os.ReadFile(v.Path("wiki/Typed Into.md")); string(data) != "Started writing.\n" {
		t.Fatalf("the user's text stays: %q", data)
	}
}

// The request carries the hash of the empty page as the stub read it, so text typed before
// the plan is a conflict too, not a base the plan overwrites.
func TestStubConflictsWhenTheUserTypesBeforeThePlan(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "wiki/concepts/Later.md", string(mkpage("Later", "# Later\n\n[[In Place]]\n")))
	writeFile(t, v, "wiki/concepts/In Place.md", "")
	req, _, _, err := StubRequest(v, nil, "", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, v, "wiki/concepts/In Place.md", "Started writing.\n")
	if _, err := Prepare(v, req, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("the replaced page: %v", err)
	}
	if data, _ := os.ReadFile(v.Path("wiki/concepts/In Place.md")); string(data) != "Started writing.\n" {
		t.Fatalf("the user's text stays: %q", data)
	}

	// The empty page a stub moves is deleted, and that write carries the hash too.
	writeFile(t, v, "wiki/concepts/Later.md", string(mkpage("Later", "# Later\n\n[[Moved]]\n")))
	writeFile(t, v, "wiki/Moved.md", "")
	req, _, _, err = StubRequest(v, nil, "", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, v, "wiki/Moved.md", "Started this one too.\n")
	if _, err := Prepare(v, req, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("the moved page: %v", err)
	}
}

// With no titles the stub takes what it can and reports the rest, so one candidate it
// cannot file does not cost the user the others.
func TestStubWithNoTitlesSkipsWhatItCannotStub(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "wiki/concepts/Linker.md", string(mkpage("Linker", "# Linker\n\n[one](Two.md), [two](notes/Two.md), and [[Ordinary]]\n")))
	writeFile(t, v, "wiki/concepts/Two.md", "")
	writeFile(t, v, "wiki/concepts/notes/Two.md", "")

	res, err := StubPages(v, nil, "", nil, now)
	if err != nil || len(res.Stubs) != 1 || res.Stubs[0].Title != "Ordinary" {
		t.Fatalf("the rest still stubs: %+v %v", res, err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Title != "Two" || !strings.Contains(res.Skipped[0].Reason, "two empty pages are named Two") {
		t.Fatalf("skipped %+v", res.Skipped)
	}
	for _, rel := range []string{"wiki/concepts/Two.md", "wiki/concepts/notes/Two.md"} {
		if data, err := os.ReadFile(v.Path(rel)); err != nil || len(data) != 0 {
			t.Fatalf("%s stays as it was: %q %v", rel, data, err)
		}
	}
	// A title the user names is still refused.
	if _, _, _, err := StubRequest(v, []StubTitle{{Title: "Two"}}, "", nil, now); err == nil || !strings.Contains(err.Error(), "two empty pages are named Two") {
		t.Fatalf("a named title: %v", err)
	}
}

func TestStubSummaryNamesAtMostThreeTitles(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "wiki/concepts/Linker.md", string(mkpage("Linker", "# Linker\n\n[[Alpha]] [[Bravo]] [[Charlie]] [[Delta]] [[Echo]]\n")))
	res, err := StubPages(v, nil, "", nil, now)
	if err != nil || len(res.Stubs) != 5 {
		t.Fatalf("five stubs: %+v %v", res, err)
	}
	ops, err := History(v, 1, false)
	if err != nil || ops[0].Summary != "stub 5 pages: Alpha, Bravo, Charlie, and 2 more" {
		t.Fatalf("summary %+v %v", ops, err)
	}
}

func TestStubInLYTMode(t *testing.T) {
	needGit(t)
	root := filepath.Join(t.TempDir(), "lyt")
	if _, err := vault.Init(root, vault.Options{Kind: vault.Project, Mode: vault.LYT}, now); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, v, "wiki/notes/Linker.md", string(mkpage("Linker", "# Linker\n\nSee [[Atomic]] and [[Everything]].\n")))

	res, err := StubPages(v, []StubTitle{{Title: "Everything", Type: "moc"}}, "", nil, now)
	if err != nil || len(res.Stubs) != 1 || res.Stubs[0] != (Stubbed{Title: "Everything", Type: "moc", Path: "wiki/mocs/Everything.md"}) {
		t.Fatalf("the moc: %+v %v", res, err)
	}
	res, err = StubPages(v, nil, "", nil, now)
	if err != nil || len(res.Stubs) != 1 || res.Stubs[0] != (Stubbed{Title: "Atomic", Type: "note", Path: "wiki/notes/Atomic.md"}) {
		t.Fatalf("the mode's default type: %+v %v", res, err)
	}
	page := read(t, v, "wiki/notes/Atomic.md")
	if !strings.Contains(page, "type: note") || !strings.Contains(page, "status: seed") || !strings.Contains(page, "# Atomic") {
		t.Fatalf("stub page:\n%s", page)
	}
	report, err := lint.Run(v.Root, lint.Options{AsOf: now})
	if err != nil {
		t.Fatal(err)
	}
	var stubs []string
	for _, s := range report.Stubs {
		stubs = append(stubs, s.Path)
	}
	if len(report.WantedPages) != 0 || strings.Join(stubs, ",") != "wiki/mocs/Everything.md,wiki/notes/Atomic.md" {
		t.Fatalf("both links resolve and both stubs wait to be filled: %v %+v", stubs, report.WantedPages)
	}
}

func TestStubLeavesAnUnsanitizableEmptyPageAlone(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "wiki/concepts/Linker.md", string(mkpage("Linker", "# Linker\n\nSee [[What?]], [[a  b]], and [[Ordinary]].\n")))
	writeFile(t, v, "wiki/What?.md", "")
	writeFile(t, v, "wiki/a  b.md", "")

	res, err := StubPages(v, nil, "", nil, now)
	if err != nil || len(res.Stubs) != 1 || res.Stubs[0].Title != "Ordinary" {
		t.Fatalf("only the ordinary wanted page stubs: %+v %v", res, err)
	}
	if data, err := os.ReadFile(v.Path("wiki/What?.md")); err != nil || len(data) != 0 {
		t.Fatalf("What?.md stays untouched: %q %v", data, err)
	}
	if data, err := os.ReadFile(v.Path("wiki/a  b.md")); err != nil || len(data) != 0 {
		t.Fatalf("a  b.md stays untouched: %q %v", data, err)
	}

	report, err := lint.Run(v.Root, lint.Options{AsOf: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range report.DeadLinks {
		if d.Target == "What?" || d.Target == "a  b" {
			t.Fatalf("%q should still resolve: %+v", d.Target, d)
		}
	}

	if _, _, _, err := StubRequest(v, []StubTitle{{Title: "What?"}}, "", nil, now); err == nil ||
		!strings.Contains(err.Error(), `the link text "What?" cannot be a file name`) {
		t.Fatalf("refusal for What?: %v", err)
	}
}

func TestStubKind(t *testing.T) {
	v := newVault(t)
	if defaultStubType(vault.LYT) != "note" || defaultStubType(vault.Generic) != "concept" {
		t.Fatal("default stub types")
	}
	if _, err := Prepare(v, Request{Kind: Stub, Summary: "x", Writes: []Write{{Path: "wiki/concepts/x.canvas", Mode: Create, Content: []byte("{}")}}}, now); err == nil {
		t.Fatal("a stub writes pages only")
	}
	if _, err := Prepare(v, Request{Kind: Stub, Summary: "x", Writes: []Write{{Path: "ideas/x.md", Mode: Create, Content: mkpage("x", "")}}}, now); err == nil {
		t.Fatal("a stub writes under wiki/ only")
	}
	p, text := taskPage("T", "planted", "task-20260912-aaaa", vault.TasksDir, "")
	if _, err := Prepare(v, Request{Kind: Stub, Summary: "x", Writes: []Write{{Path: p, Mode: Create, Content: text}}}, now); err == nil {
		t.Fatal("a stub never writes a task page")
	}

	// One title, one page: a name two empty pages share says which two, and a title given
	// twice with two types says so rather than taking the first.
	writeFile(t, v, "wiki/concepts/Linker.md", string(mkpage("Linker", "# Linker\n\n[one](Two.md), [two](notes/Two.md), and [[Once]]\n")))
	writeFile(t, v, "wiki/concepts/Two.md", "")
	writeFile(t, v, "wiki/concepts/notes/Two.md", "")
	_, _, _, err := StubRequest(v, []StubTitle{{Title: "Two"}}, "", nil, now)
	if err == nil || err.Error() != "two empty pages are named Two: wiki/concepts/notes/Two.md, wiki/concepts/Two.md; keep one" {
		t.Fatalf("two empty pages: %v", err)
	}
	_, _, _, err = StubRequest(v, []StubTitle{{Title: "Once"}, {Title: "once", Type: "entity"}}, "", nil, now)
	if err == nil || err.Error() != "Once is given twice with different types: concept and entity" {
		t.Fatalf("one title, two types: %v", err)
	}
	if _, _, _, err = StubRequest(v, []StubTitle{{Title: "Once"}, {Title: "once", Type: "concept"}}, "", nil, now); err != nil {
		t.Fatalf("the same type twice is one stub: %v", err)
	}
}

func TestStubIntoCreatesInTheKnowledgeBase(t *testing.T) {
	p, kb := newVault(t), newKnowledge(t)
	writeFile(t, p, "wiki/concepts/Training.md", string(mkpage("Training", "# Training\n\nSee [[Vanishing Gradient]].\n")))

	res, err := StubInto(p, kb, []StubTitle{{Title: "Vanishing Gradient"}}, "", "cs566", nil, now)
	if err != nil || len(res.Stubs) != 1 || res.Stubs[0].Path != "wiki/concepts/Vanishing Gradient.md" || res.Commit == "" {
		t.Fatalf("stub into the knowledge base: %+v %v", res, err)
	}
	if page := read(t, kb, "wiki/concepts/Vanishing Gradient.md"); !strings.Contains(page, "status: seed") || !strings.Contains(page, "# Vanishing Gradient") {
		t.Fatalf("stub page:\n%s", page)
	}
	ops, err := History(kb, 1, false)
	if err != nil || ops[0].Kind != "stub" || ops[0].Summary != "stub Vanishing Gradient (via cs566)" {
		t.Fatalf("the knowledge base's operation: %+v %v", ops[0], err)
	}
	if _, err := os.Stat(p.Path("wiki/concepts/Vanishing Gradient.md")); err == nil {
		t.Fatal("the stub lands in the knowledge base, not the project")
	}
	if ops, err := History(p, 1, false); err != nil || ops[0].Kind != "setup" {
		t.Fatalf("the project gets no operation: %+v %v", ops, err)
	}
	if r, err := lint.Run(p.Root, lint.Options{AsOf: now}); err != nil || len(r.WantedPages) != 1 || r.WantedPages[0].Title != "Vanishing Gradient" {
		t.Fatalf("the project cannot see the page until it mounts the knowledge base: %+v %v", r.WantedPages, err)
	}
	if err := os.MkdirAll(p.Path(vault.KbDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(kb.Path(vault.WikiDir), p.Path(vault.KbDir+"/cs566")); err != nil {
		t.Fatal(err)
	}
	if r, err := lint.Run(p.Root, lint.Options{AsOf: now}); err != nil || len(r.WantedPages) != 0 {
		t.Fatalf("the project reads the stub through its mount: %+v %v", r.WantedPages, err)
	}
	if _, err := StubInto(p, kb, []StubTitle{{Title: "Nowhere"}}, "", "cs566", nil, now); err == nil || !strings.Contains(err.Error(), "nothing in the wiki links to") {
		t.Fatalf("a title nothing links to: %v", err)
	}

	// One knowledge base as source and destination stubs its own wanted pages, and the
	// summary still names the project the session came through.
	writeFile(t, kb, "wiki/concepts/Seed.md", string(mkpage("Seed", "# Seed\n\nSee [[Attention]].\n")))
	res, err = StubInto(kb, kb, nil, "", "cs566", nil, now)
	if err != nil || len(res.Stubs) != 1 || res.Stubs[0].Path != "wiki/concepts/Attention.md" {
		t.Fatalf("the knowledge base's own wanted pages: %+v %v", res, err)
	}
	if ops, err := History(kb, 1, false); err != nil || ops[0].Summary != "stub Attention (via cs566)" {
		t.Fatalf("summary: %+v %v", ops, err)
	}
}
