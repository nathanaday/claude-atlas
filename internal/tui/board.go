package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/registry"
)

// boardKind says which entries a board lists.
type boardKind int

const (
	boardKnowledge boardKind = iota
	boardProjects
	boardProblems
)

// noKnowledge is the group projects without a knowledge base sit under.
const noKnowledge = "no knowledge base"

// boardRow is one entry on a board and the lines it spans, the expanded block included.
type boardRow struct {
	item       *Item
	start, end int
}

// board is one tab's list: the entries of one kind as boxes, grouped; the cursor; the
// scroll offset; and which entries are expanded.
type board struct {
	kind     boardKind
	items    []*Item         // this board's entries, in display order
	expanded map[string]bool // by path
	cursor   int             // an index into items; len(items) is the end marker
	offset   int
	width    int
	rows     []boardRow
	lines    []string
}

func newBoard(kind boardKind, items []Item, width int) board {
	b := board{kind: kind, expanded: map[string]bool{}, width: width}
	b.reload(items)
	return b
}

// belongs says whether an entry sits on this board.
func (b board) belongs(e registry.Entry) bool {
	switch b.kind {
	case boardProblems:
		return e.Error != ""
	case boardKnowledge:
		return e.Error == "" && e.Kind == registry.Knowledge
	}
	return e.Error == "" && e.Kind == registry.Project
}

// group is the header an entry sits under: on the Projects board, its knowledge base's
// name, or noKnowledge. The other boards have no groups.
func (b board) group(e registry.Entry) string {
	if b.kind != boardProjects || e.Error != "" {
		return ""
	}
	if e.Knowledge == nil || e.Knowledge.Error != "" {
		return noKnowledge
	}
	return e.Knowledge.Name
}

// less orders a board: the groups by name, projects without a knowledge base last, and
// names within a group.
func (b board) less(x, y registry.Entry) bool {
	gx, gy := b.group(x), b.group(y)
	if gx != gy {
		if gx == noKnowledge || gy == noKnowledge {
			return gy == noKnowledge
		}
		return strings.ToLower(gx) < strings.ToLower(gy)
	}
	return strings.ToLower(entryName(x)) < strings.ToLower(entryName(y))
}

// reload takes the entries again, drops the expansions of entries that are gone, keeps
// the cursor on the same entry, and lays out. The board keeps pointers into the slice
// it is given, so the caller keeps that slice.
func (b *board) reload(items []Item) {
	keep := ""
	if it := b.current(); it != nil {
		keep = it.Entry.Path
	}
	b.items = nil
	for i := range items {
		if b.belongs(items[i].Entry) {
			b.items = append(b.items, &items[i])
		}
	}
	sort.SliceStable(b.items, func(i, j int) bool { return b.less(b.items[i].Entry, b.items[j].Entry) })
	for path := range b.expanded {
		if b.find(path) < 0 {
			delete(b.expanded, path)
		}
	}
	b.cursor = min(b.cursor, len(b.items))
	if i := b.find(keep); i >= 0 {
		b.cursor = i
	}
	b.layout()
}

// find is the index of the entry at path, or -1.
func (b board) find(path string) int {
	for i, it := range b.items {
		if path != "" && it.Entry.Path == path {
			return i
		}
	}
	return -1
}

// current is the entry under the cursor; nil on the end marker or an empty board.
func (b board) current() *Item {
	if b.cursor < 0 || b.cursor >= len(b.items) {
		return nil
	}
	return b.items[b.cursor]
}

// atEnd reports whether the cursor sits on the end marker below the last entry.
func (b board) atEnd() bool { return len(b.items) > 0 && b.cursor == len(b.items) }

// move steps the cursor; one step past the last entry lands on the end marker.
func (b *board) move(delta int) {
	b.cursor = max(0, min(len(b.items), b.cursor+delta))
}

// moveTo puts the cursor on the entry at path and reports whether it is here.
func (b *board) moveTo(path string) bool {
	i := b.find(path)
	if i < 0 {
		return false
	}
	b.cursor = i
	return true
}

// toggle expands the entry under the cursor, or collapses it.
func (b *board) toggle() {
	it := b.current()
	if it == nil {
		return
	}
	if b.expanded[it.Entry.Path] {
		delete(b.expanded, it.Entry.Path)
	} else {
		b.expanded[it.Entry.Path] = true
	}
	b.layout()
}

// collapseAll collapses every entry and reports whether any was expanded.
func (b *board) collapseAll() bool {
	had := len(b.expanded) > 0
	b.expanded = map[string]bool{}
	b.layout()
	return had
}

// boxWidth is the box column: most of the screen, between 26 and 72 columns, the
// borders not counted.
func boxWidth(width int) int { return min(72, max(26, width-8)) }

// layout renders the boxes into lines and records the span of each entry. A group's
// header belongs to the first entry under it, so scrolling back to that entry brings
// the header with it.
func (b *board) layout() {
	b.lines, b.rows = nil, nil
	last := ""
	for i, it := range b.items {
		start := len(b.lines)
		if g := b.group(it.Entry); g != "" && g != last {
			header := knowledgeSt.Render(g)
			if g == noKnowledge {
				header = dim.Render(g)
			}
			b.lines = append(b.lines, header)
		}
		last = b.group(it.Entry)
		b.render(it, i == b.cursor, start)
	}
	if len(b.lines) > 0 {
		end := dim.Render("(end)")
		if b.atEnd() {
			end = selSt.Render("(end)")
		}
		b.lines = append(b.lines, end)
	}
}

// render writes one entry: its box, and the detail block under it when expanded. Only
// the entry under the cursor keeps its colors. start is where the entry's row begins, at
// its group header when it has one.
func (b *board) render(it *Item, selected bool, start int) {
	e := it.Entry
	width := boxWidth(b.width)
	box := boxStyle(e.Kind, selected).Width(width).Render(strings.Join(boxLines(e, width-2), "\n"))
	// The view indents every line by two, so a line stops two short of the screen. One
	// that reached the edge would wrap and push the frame past the last row.
	narrow := lipgloss.NewStyle().MaxWidth(max(10, b.width-2))
	var lines []string
	for _, line := range strings.Split(box, "\n") {
		lines = append(lines, narrow.Render(line))
	}
	if b.expanded[e.Path] {
		for _, line := range detailLines(e) {
			lines = append(lines, narrow.Render("   "+line))
		}
	}
	b.lines = append(b.lines, focus(lines, selected)...)
	b.rows = append(b.rows, boardRow{item: it, start: start, end: len(b.lines) - 1})
}

// ensureVisible scrolls so the cursor's lines fit in avail lines. A row taller than the
// window keeps its top on screen.
func (b *board) ensureVisible(avail int) {
	if len(b.rows) == 0 {
		b.offset = 0
		return
	}
	start, end := len(b.lines)-1, len(b.lines)-1
	if b.cursor < len(b.rows) {
		start, end = b.rows[b.cursor].start, b.rows[b.cursor].end
	}
	if start < b.offset {
		b.offset = start
	}
	if end >= b.offset+avail {
		b.offset = min(start, end-avail+1)
	}
	if b.offset < 0 {
		b.offset = 0
	}
}

// window is the lines on screen and how many more follow them.
func (b board) window(avail int) ([]string, int) {
	end := min(len(b.lines), b.offset+avail)
	if b.offset >= end {
		return nil, 0
	}
	return b.lines[b.offset:end], len(b.lines) - end
}
