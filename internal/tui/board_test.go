package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/registry"
)

// ansiProfile is termenv.ANSI. Lip Gloss renders plain text under go test, where there
// is no TTY; this profile makes it emit escape codes for one test. The termenv module
// is not imported directly, so the value is spelled out.
const ansiProfile = 2

func TestOnlyTheRowUnderTheCursorIsColored(t *testing.T) {
	was := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(ansiProfile)
	t.Cleanup(func() { lipgloss.SetColorProfile(was) })
	b := newBoard(boardProjects, sample(), 100)
	b.moveTo("/code/webapp")
	b.toggle() // the expanded block belongs to the row too
	b.moveTo("/code/firmware")
	b.layout()
	for _, r := range b.rows {
		text := strings.Join(b.boxOf(r), "\n")
		colored := strings.Contains(text, "\x1b[")
		if want := r.item == b.current(); colored != want {
			t.Errorf("%s colored=%v, want %v:\n%s", r.item.Entry.Name, colored, want, text)
		}
	}
}

// boxOf is a row's lines from its box's top border, leaving out the group header a row
// carries when it opens a group.
func (b board) boxOf(r boardRow) []string {
	lines := b.lines[r.start : r.end+1]
	for i, line := range lines {
		if strings.Contains(line, "╭") {
			return lines[i:]
		}
	}
	return lines
}

func TestBoardsKeepTheirOwnKind(t *testing.T) {
	items := sample()
	kb := newBoard(boardKnowledge, items, 100)
	pr := newBoard(boardProjects, items, 100)
	pb := newBoard(boardProblems, items, 100)
	if len(kb.items) != 2 || len(pr.items) != 3 || len(pb.items) != 1 {
		t.Fatalf("knowledge=%d projects=%d problems=%d", len(kb.items), len(pr.items), len(pb.items))
	}
	for _, it := range kb.items {
		if it.Entry.Kind != registry.Knowledge {
			t.Fatal("a project on the knowledge board")
		}
	}
	if kb.group(kb.items[0].Entry) != "" {
		t.Fatal("the knowledge board has no groups")
	}
}

func TestReloadKeepsCursorAndExpansion(t *testing.T) {
	items := sample()
	b := newBoard(boardProjects, items, 100)
	b.moveTo("/code/webapp")
	b.toggle()
	fewer := append([]Item{}, items[:3]...)
	fewer = append(fewer, items[4:]...) // firmware is gone
	b.reload(fewer)
	if it := b.current(); it == nil || it.Entry.Name != "webapp" {
		t.Fatal("the cursor follows the entry")
	}
	if !b.expanded["/code/webapp"] || len(b.items) != 2 {
		t.Fatalf("expansion stays; the gone entry leaves: items=%d", len(b.items))
	}
	b.reload(items[:2])
	if len(b.expanded) != 0 || b.cursor != 0 {
		t.Fatalf("expansions of gone entries are dropped and the cursor clamps: expanded=%v cursor=%d", b.expanded, b.cursor)
	}
}

func TestEnsureVisibleScrollsToTheCursor(t *testing.T) {
	b := newBoard(boardProjects, sample(), 100)
	b.move(3)
	b.ensureVisible(6)
	lines, more := b.window(6)
	if len(lines) != 6 || !strings.Contains(strings.Join(lines, "\n"), "(end)") {
		t.Fatalf("the window holds the end marker: %d lines, %d more", len(lines), more)
	}
	b.move(-3)
	b.ensureVisible(6)
	if b.offset != 0 {
		t.Fatalf("back to the top: offset=%d", b.offset)
	}
}

func TestGroupHeadersOpenEachKnowledgeBase(t *testing.T) {
	b := newBoard(boardProjects, sample(), 100)
	text := stripANSI(strings.Join(b.lines, "\n"))
	if strings.Count(text, "papers\n") < 1 || !strings.Contains(text, noKnowledge) {
		t.Fatalf("headers:\n%s", text)
	}
	if b.rows[0].start != 0 || !strings.Contains(stripANSI(b.lines[0]), "papers") {
		t.Fatal("the first row carries its group header")
	}
}
