package tui

import (
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// boardItems is four projects (one untagged, one under itl, two under usc), two knowledge
// bases, and one vault the scan could not read. p3 mounts three knowledge bases: one
// fully, one with reduced access, one the scan did not find.
func boardItems() []Item {
	four := 4
	mk := func(kind vault.Kind, name string, tags ...string) Item {
		return Item{Entry: registry.Entry{ID: "id-" + name, Kind: kind, Name: name, Path: "/v/" + name, Mode: vault.Generic, Tags: tags,
			State: &registry.State{VaultOK: true, Heat: "warm", Pages: &four}}}
	}
	items := []Item{
		mk(vault.Project, "welcome"),
		mk(vault.Project, "p3", "itl"),
		mk(vault.Project, "course", "usc"),
		mk(vault.Project, "aleph", "usc"),
		mk(vault.Knowledge, "papers"),
		mk(vault.Knowledge, "ai-ml"),
	}
	items[1].Entry.Mounts = []registry.Mount{
		{ID: "id-ai-ml", Name: "ai-ml", Access: "write", Effective: "write", Path: "/v/ai-ml/wiki"},
		{ID: "id-papers", Name: "papers", Access: "write", Effective: "read", Path: "/v/papers/wiki"},
		{ID: "id-x", Name: "x", Access: "write", Error: "no knowledge base with id id-x"},
	}
	items[4].Entry.MountedBy = []registry.Ref{{ID: "id-p3", Name: "p3", Access: "read"}}
	items[5].Entry.MountedBy = []registry.Ref{{ID: "id-p3", Name: "p3", Access: "write"}}
	items = append(items, Item{Entry: registry.Entry{Path: "/v/old-notes", Error: "v1 vault", Reason: registry.ReasonV1}})
	return items
}

func names(b board) string {
	var out []string
	for _, it := range b.items {
		out = append(out, entryName(it.Entry))
	}
	return strings.Join(out, ",")
}

func TestBoardsKeepTheirKind(t *testing.T) {
	items := boardItems()
	if got := names(newBoard(boardProjects, items, 100)); got != "welcome,p3,aleph,course" {
		t.Fatalf("projects: %s", got)
	}
	if got := names(newBoard(boardKnowledge, items, 100)); got != "ai-ml,papers" {
		t.Fatalf("knowledge: %s", got)
	}
	if got := names(newBoard(boardProblems, items, 100)); got != "old-notes" {
		t.Fatalf("problems: %s", got)
	}
}

func TestProjectsGroupUnderTheirFirstTag(t *testing.T) {
	b := newBoard(boardProjects, boardItems(), 100)
	text := strings.Join(b.lines, "\n")
	t.Logf("\n%s", text)
	if len(b.rows) != 4 {
		t.Fatalf("a header is not a row: %d rows", len(b.rows))
	}
	welcome, itl, p3, usc := strings.Index(text, "welcome"), strings.Index(text, "\nitl\n"), strings.Index(text, "p3"), strings.Index(text, "\nusc\n")
	if !(welcome >= 0 && welcome < itl && itl < p3 && p3 < usc) {
		t.Fatalf("order: welcome=%d itl=%d p3=%d usc=%d", welcome, itl, p3, usc)
	}
	if strings.HasPrefix(text, "itl") || strings.Count(text, "\nusc\n") != 1 {
		t.Fatalf("untagged first with no header, one header per group:\n%s", text)
	}
	if strings.Contains(strings.Join(newBoard(boardKnowledge, boardItems(), 100).lines, "\n"), "\nusc\n") {
		t.Fatal("knowledge bases have no groups")
	}
}

func TestTheBoxGrowsToFitTheConnectors(t *testing.T) {
	b := newBoard(boardProjects, boardItems(), 100)
	b.moveTo("/v/p3")
	b.layout()
	r := b.rows[b.cursor]
	if r.end-r.start+1 != 5 {
		t.Fatalf("three mounts need three content lines and two borders, got %d lines", r.end-r.start+1)
	}
	lines := b.lines[r.start : r.end+1]
	for i, want := range []string{"╌╌╌╌▶ ai-ml   write · link missing", "╌╌╌╌▶ papers   read (write not granted) · link missing", "╌╌╌╌▶ no knowledge base with id id-x"} {
		if !strings.Contains(lines[i+1], want) {
			t.Errorf("line %d = %q, want %q", i+1, lines[i+1], want)
		}
	}
	b.moveTo("/v/welcome")
	b.layout()
	r = b.rows[b.cursor]
	if r.end-r.start+1 != 4 || !strings.Contains(b.lines[r.start+1], "no knowledge base mounted") {
		t.Fatalf("no mounts: %q", b.lines[r.start:r.end+1])
	}
	kb := newBoard(boardKnowledge, boardItems(), 100)
	if text := strings.Join(kb.lines, "\n"); strings.Count(text, "◀╌╌╌╌ 1 project") != 2 {
		t.Fatalf("knowledge connectors:\n%s", text)
	}
}

func TestToggleExpandsUnderTheBox(t *testing.T) {
	b := newBoard(boardKnowledge, boardItems(), 100)
	b.moveTo("/v/papers")
	b.layout()
	before := b.rows[b.cursor].end - b.rows[b.cursor].start
	b.toggle()
	r := b.rows[b.cursor]
	text := strings.Join(b.lines[r.start:r.end+1], "\n")
	if r.end-r.start <= before || !b.expanded["/v/papers"] {
		t.Fatalf("expanded row did not grow: %d -> %d", before, r.end-r.start)
	}
	for _, want := range []string{"◀╌╌╌╌ p3   read", "Path", "/v/papers", "Access", "open", "Vault check"} {
		if !strings.Contains(text, want) {
			t.Errorf("expanded block missing %q:\n%s", want, text)
		}
	}
	b.toggle()
	if b.expanded["/v/papers"] || strings.Contains(strings.Join(b.lines, "\n"), "Path") {
		t.Fatal("toggle again collapses")
	}
	b.toggle()
	b.move(-1)
	b.toggle()
	if len(b.expanded) != 2 {
		t.Fatalf("two expanded: %v", b.expanded)
	}
	if !b.collapseAll() || len(b.expanded) != 0 || b.collapseAll() {
		t.Fatal("collapseAll reports what it did")
	}
	b.move(1)
	b.move(1)
	b.toggle() // on the end marker: nothing to expand, no panic
	if len(b.expanded) != 0 {
		t.Fatal("the end marker expands nothing")
	}
}

func TestReloadKeepsTheCursorAndTheExpansion(t *testing.T) {
	items := boardItems()
	b := newBoard(boardProjects, items, 100)
	b.moveTo("/v/course")
	b.toggle()
	items = append(items, Item{Entry: registry.Entry{ID: "id-b", Kind: vault.Project, Name: "brand-new", Path: "/v/brand-new"}})
	b.reload(items)
	if it := b.current(); it == nil || it.Entry.Name != "course" || !b.expanded["/v/course"] {
		t.Fatalf("after reload: current=%v expanded=%v", it, b.expanded)
	}
	b.reload(items[:1])
	if len(b.expanded) != 0 || b.cursor > len(b.items) {
		t.Fatalf("a vault that is gone loses its expansion; the cursor clamps: %v %d", b.expanded, b.cursor)
	}
}

func TestMoveStopsAtTheEndMarker(t *testing.T) {
	b := newBoard(boardKnowledge, boardItems(), 100)
	b.move(-1)
	if b.cursor != 0 {
		t.Fatal("up at the top stays")
	}
	b.move(1)
	b.move(1)
	b.layout()
	if !b.atEnd() || b.current() != nil || !strings.HasSuffix(b.lines[len(b.lines)-1], "(end)") {
		t.Fatalf("two downs reach the end marker: cursor=%d", b.cursor)
	}
	b.move(1)
	if !b.atEnd() {
		t.Fatal("down at the end stays")
	}
	b.move(-1)
	if it := b.current(); it == nil || it.Entry.Name != "papers" {
		t.Fatalf("up from the end returns to the last vault: %v", it)
	}
	if !b.moveTo("/v/ai-ml") || b.current().Entry.Name != "ai-ml" || b.moveTo("/v/nowhere") {
		t.Fatal("moveTo")
	}
}

func TestWindowKeepsTheCursorVisible(t *testing.T) {
	b := newBoard(boardProjects, boardItems(), 80)
	b.move(1)
	b.move(1)
	b.layout()
	b.ensureVisible(5)
	r := b.rows[b.cursor]
	if r.start < b.offset || r.end >= b.offset+5 {
		t.Fatalf("row %d-%d not within offset %d + 5", r.start, r.end, b.offset)
	}
	lines, more := b.window(5)
	if len(lines) != 5 || more != len(b.lines)-b.offset-5 {
		t.Fatalf("window: %d lines, %d more", len(lines), more)
	}
	b.move(-1)
	b.move(-1)
	b.ensureVisible(5)
	if b.offset != 0 {
		t.Fatalf("back at the top the offset is 0, got %d", b.offset)
	}
	empty := newBoard(boardProblems, nil, 80)
	empty.ensureVisible(5)
	if lines, more := empty.window(5); len(lines) != 0 || more != 0 || len(empty.lines) != 0 {
		t.Fatal("an empty board has no lines and no end marker")
	}
}

func TestATallRowKeepsItsTopOnScreen(t *testing.T) {
	b := newBoard(boardProjects, boardItems(), 80)
	b.moveTo("/v/p3")
	b.toggle() // the expanded block makes the row taller than the window
	r := b.rows[b.cursor]
	if r.end-r.start+1 <= 5 {
		t.Fatalf("the row must not fit: %d lines", r.end-r.start+1)
	}
	b.ensureVisible(5)
	if b.offset != r.start {
		t.Fatalf("offset %d, want the row's top %d", b.offset, r.start)
	}
	lines, _ := b.window(5)
	if len(lines) != 5 || lines[0] != b.lines[r.start] {
		t.Fatalf("the window starts at the box's top border: %q", lines)
	}
}

func TestBoxWidthFollowsTheScreen(t *testing.T) {
	for w, want := range map[int]int{40: 26, 60: 26, 80: 36, 100: 40, 200: 40} {
		if got := boxWidth(w); got != want {
			t.Errorf("boxWidth(%d) = %d, want %d", w, got, want)
		}
	}
}
