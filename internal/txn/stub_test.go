package txn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		_, _, err := StubRequest(v, []StubTitle{{Title: c.title, Type: c.pageType}}, "", now)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s (%s): got %v, want %q", c.title, c.pageType, err, c.want)
		}
	}
}

func TestStubPagesCreatesWantedPagesAndMovesClickedOnes(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "wiki/concepts/Training.md", string(mkpage("Training", "# Training\n\nSuffers from the [[vanishing gradient problem]] and needs [[Gradient Clipping]]; see [[Clicked]].\n")))
	writeFile(t, v, "wiki/Clicked.md", "")

	res, err := StubPages(v, []StubTitle{{Title: "gradient clipping", Type: "entity"}}, "", now)
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

	res, err = StubPages(v, nil, "", now)
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
	if res, err := StubPages(v, nil, "", now); err != nil || len(res.Stubs) != 0 || res.Stubs == nil || res.OperationID != "" {
		t.Fatalf("nothing wanted: %+v %v", res, err)
	}
	writeFile(t, v, "wiki/concepts/Linker.md", string(mkpage("Linker", "# Linker\n\n[[In Place]]\n")))
	writeFile(t, v, "wiki/concepts/In Place.md", "")
	res, err := StubPages(v, nil, "", now)
	if err != nil || len(res.Stubs) != 1 || res.Stubs[0].Path != "wiki/concepts/In Place.md" {
		t.Fatalf("in place: %+v %v", res, err)
	}
	if !strings.Contains(read(t, v, "wiki/concepts/In Place.md"), "title: \"In Place\"") {
		t.Fatal("the empty page is replaced with the skeleton")
	}
}

func TestStubConflictsWhenTheUserTypesIntoTheEmptyPage(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, "wiki/concepts/Later.md", string(mkpage("Later", "# Later\n\n[[Typed Into]]\n")))
	writeFile(t, v, "wiki/Typed Into.md", "")
	req, _, err := StubRequest(v, nil, "", now)
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
}
