package tui

import (
	"sort"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// boardKind says which vaults a board lists.
type boardKind int

const (
	boardProjects boardKind = iota
	boardKnowledge
	boardProblems
)

// boardRow is one vault on a board and the lines it spans, the expanded block included.
type boardRow struct {
	item       *Item
	start, end int
}

// board is one tab's list: the vaults of one kind as boxes, each with its connectors to
// the other kind; the cursor; the scroll offset; and which vaults are expanded.
type board struct {
	kind     boardKind
	all      []Item          // every vault, for the connectors
	items    []*Item         // this board's vaults, in display order
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

// belongs says whether a vault sits on this board.
func (b board) belongs(e registry.Entry) bool {
	switch b.kind {
	case boardProblems:
		return e.Error != ""
	case boardKnowledge:
		return e.Error == "" && e.Kind == vault.Knowledge
	}
	return e.Error == "" && e.Kind == vault.Project
}

// group is the header a project sits under: its first tag. Other vaults have none.
func group(e registry.Entry) string {
	if e.Error == "" && e.Kind == vault.Project && len(e.Tags) > 0 {
		return e.Tags[0]
	}
	return ""
}

// less orders a board: untagged projects first, then the tag groups by name, and names
// within a group.
func less(a, b registry.Entry) bool {
	ga, gb := group(a), group(b)
	if ga != gb {
		if ga == "" || gb == "" {
			return ga == ""
		}
		return ga < gb
	}
	return entryName(a) < entryName(b)
}

// reload takes the vaults again, drops the expansions of vaults that are gone, keeps the
// cursor on the same vault, and lays out. The board keeps pointers into the slice it is
// given, so the caller keeps that slice.
func (b *board) reload(items []Item) {
	keep := ""
	if it := b.current(); it != nil {
		keep = it.Entry.Path
	}
	b.all = items
	b.items = nil
	for i := range items {
		if b.belongs(items[i].Entry) {
			b.items = append(b.items, &items[i])
		}
	}
	sort.SliceStable(b.items, func(i, j int) bool { return less(b.items[i].Entry, b.items[j].Entry) })
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

// find is the index of the vault at path, or -1.
func (b board) find(path string) int {
	for i, it := range b.items {
		if path != "" && it.Entry.Path == path {
			return i
		}
	}
	return -1
}

// current is the vault under the cursor; nil on the end marker or an empty board.
func (b board) current() *Item {
	if b.cursor < 0 || b.cursor >= len(b.items) {
		return nil
	}
	return b.items[b.cursor]
}

// atEnd reports whether the cursor sits on the end marker below the last vault.
func (b board) atEnd() bool { return len(b.items) > 0 && b.cursor == len(b.items) }

// move steps the cursor; one step past the last vault lands on the end marker.
func (b *board) move(delta int) {
	b.cursor = max(0, min(len(b.items), b.cursor+delta))
}

// moveTo puts the cursor on the vault at path and reports whether it is here.
func (b *board) moveTo(path string) bool {
	i := b.find(path)
	if i < 0 {
		return false
	}
	b.cursor = i
	return true
}

// toggle expands the vault under the cursor, or collapses it.
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

// collapseAll collapses every vault and reports whether any was expanded.
func (b *board) collapseAll() bool {
	had := len(b.expanded) > 0
	b.expanded = map[string]bool{}
	b.layout()
	return had
}

// boxWidth is the box column: about half the screen, between 26 and 40 columns, the
// borders not counted.
func boxWidth(width int) int { return min(40, max(26, width/2-4)) }

// labelWidth is what is left for a connector's label after the indent, the box with
// its borders, and the arrow.
func (b board) labelWidth() int {
	return max(12, b.width-2-(boxWidth(b.width)+2)-len([]rune(arrowOut)))
}

// layout renders the boxes into lines and records the span of each vault.
func (b *board) layout() {
	b.lines, b.rows = nil, nil
	last := ""
	for i, it := range b.items {
		if g := group(it.Entry); g != "" && g != last {
			b.lines = append(b.lines, dim.Render(g))
		}
		last = group(it.Entry)
		b.render(it, i == b.cursor)
	}
	if len(b.lines) > 0 {
		end := dim.Render("(end)")
		if b.atEnd() {
			end = selSt.Render("(end)")
		}
		b.lines = append(b.lines, end)
	}
}

// side is the connector column beside a box: a project's mounts, or the projects that
// mount a knowledge base. A problem has none.
func (b board) side(e registry.Entry) []string {
	switch b.kind {
	case boardProjects:
		if len(e.Mounts) == 0 {
			return []string{noArrow + dim.Render("no knowledge base mounted")}
		}
		var out []string
		for _, m := range e.Mounts {
			out = append(out, mountLine(e, m, b.kbName(m), b.labelWidth()))
		}
		return out
	case boardKnowledge:
		return mountedByLines(e, b.expanded[e.Path])
	}
	return nil
}

// kbName is the knowledge base a mount names, as the atlas knows it now; the mount's
// own name when the scan does not hold it.
func (b board) kbName(m registry.Mount) string {
	for i := range b.all {
		if b.all[i].Entry.Error == "" && b.all[i].Entry.ID == m.ID {
			return b.all[i].Entry.Name
		}
	}
	return m.Name
}

// render writes one vault: its box with the connectors beside it, and the detail block
// under it when expanded. The box grows to hold as many lines as the connectors need.
func (b *board) render(it *Item, selected bool) {
	e := it.Entry
	content := boxLines(e)
	side := b.side(e)
	for len(content) < len(side) {
		content = append(content, "")
	}
	box := boxStyle(e.Kind, selected).Width(boxWidth(b.width)).Render(strings.Join(content, "\n"))
	start := len(b.lines)
	for i, line := range strings.Split(box, "\n") {
		if i >= 1 && i-1 < len(side) {
			line += side[i-1]
		}
		b.lines = append(b.lines, line)
	}
	if b.expanded[e.Path] {
		for _, line := range detailLines(e, now()) {
			b.lines = append(b.lines, "   "+line)
		}
	}
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
