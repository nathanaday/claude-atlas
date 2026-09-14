package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/tree"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// linksScreen shows a project's linked folders as boxes, one per link, and acts on
// them at once: add, edit the page, unlink. Every action is one backend call; the view
// refreshes afterwards so the facts catch up. It is embedded in the view.

type linksMode int

const (
	linksList linksMode = iota
	linksAdd
	linksEdit
	linksConfirmUnlink
)

// linkRow is one box: the link as the project page holds it, what the last refresh
// found in the folder, and the other projects that use it.
type linkRow struct {
	link   tree.Linked
	facts  string
	broken bool
	shared []string
}

type linksScreen struct {
	hooks   Hooks
	item    *Item
	rows    []linkRow
	cursor  int
	mode    linksMode
	source  pathField
	edit    linkEditor
	width   int
	status  string
	err     string
	changed bool // an action succeeded since the view last refreshed
	closed  bool
}

func newLinks(hooks Hooks, item *Item, items []Item, width int) linksScreen {
	s := linksScreen{hooks: hooks, item: item, width: width}
	s.source = newPathField("~/code/project or ~/Papers, or the name of a page another project links", 60)
	s.reload(items, item.Project.Rel)
	return s
}

// reload rebuilds the boxes from the view's items after a refresh or an action.
func (s *linksScreen) reload(items []Item, rel string) {
	for i := range items {
		if items[i].Project.Rel == rel {
			s.item = &items[i]
		}
	}
	p := s.item.Project
	s.rows = nil
	for _, l := range p.Linked {
		row := linkRow{link: l}
		switch {
		case l.Path == "":
			row.facts, row.broken = "names no page under "+links.Dir(l.Kind)+"/", true
		case l.Name == "":
			row.facts = "no page yet; refresh makes one"
		default:
			row.facts = "not refreshed yet"
		}
		if s.item.State != nil {
			for _, f := range s.item.State.Links {
				if f.Path == l.Path && f.Kind == l.Kind && l.Path != "" {
					row.facts, row.broken = refresh.LinkSummary(f), !f.OK
				}
			}
		}
		for i := range items {
			if q := items[i].Project; q.Rel != p.Rel && l.Path != "" && q.LinkedTo(l.Path) {
				row.shared = append(row.shared, q.Name)
			}
		}
		s.rows = append(s.rows, row)
	}
	if s.cursor >= len(s.rows) {
		s.cursor = max(0, len(s.rows)-1)
	}
}

func (s linksScreen) current() *linkRow {
	if len(s.rows) == 0 {
		return nil
	}
	return &s.rows[s.cursor]
}

// pageNames lists the pages the project does not link yet, for the add field.
func (s linksScreen) pageNames() []string {
	if s.hooks.Links == nil {
		return nil
	}
	var names []string
	for _, page := range s.hooks.Links() {
		if !s.item.Project.LinkedTo(page.Path) {
			names = append(names, page.Name)
		}
	}
	return names
}

func (s linksScreen) update(msg tea.Msg) (linksScreen, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	switch s.mode {
	case linksList:
		if !isKey {
			return s, nil
		}
		s.err = ""
		switch key.Type {
		case tea.KeyUp:
			if s.cursor > 0 {
				s.cursor--
			}
		case tea.KeyDown:
			if s.cursor < len(s.rows)-1 {
				s.cursor++
			}
		case tea.KeyEsc:
			s.closed = true
		case tea.KeyEnter:
			return s.startEdit()
		default:
			switch key.String() {
			case "a":
				s.source.names = s.pageNames()
				s.source.setValue("")
				s.mode = linksAdd
				return s, s.source.focus()
			case "e":
				return s.startEdit()
			case "u":
				if s.current() != nil {
					s.mode = linksConfirmUnlink
				}
			case "q":
				s.closed = true
			}
		}
		return s, nil
	case linksAdd:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				return s.add(), nil
			case tea.KeyEsc:
				s.err = ""
				s.mode = linksList
				return s, nil
			}
		}
		var cmd tea.Cmd
		s.source, cmd = s.source.update(msg)
		return s, cmd
	case linksEdit:
		var cmd tea.Cmd
		s.edit, cmd = s.edit.update(msg)
		switch {
		case s.edit.cancelled:
			s.mode = linksList
		case s.edit.saved:
			s.mode = linksList
			s.status = "edited " + s.edit.page.Name
			s.changed = true
		}
		return s, cmd
	case linksConfirmUnlink:
		if isKey {
			switch strings.ToLower(key.String()) {
			case "y":
				return s.unlink(), nil
			case "n", "esc":
				s.mode = linksList
			}
		}
		return s, nil
	}
	return s, nil
}

// add links what the field names: a page, or a folder that gets one.
func (s linksScreen) add() linksScreen {
	target := s.source.value()
	if target == "" {
		s.mode = linksList
		return s
	}
	if s.hooks.AddLink == nil {
		s.err = "linking is not available here"
		return s
	}
	page, err := s.hooks.AddLink(s.item.Project, target)
	if err != nil {
		s.err = err.Error()
		return s
	}
	s.err = ""
	s.status = fmt.Sprintf("linked %s (%s)", page.Name, page.Kind)
	s.changed = true
	s.mode = linksList
	return s
}

func (s linksScreen) unlink() linksScreen {
	row := s.current()
	s.mode = linksList
	if row == nil {
		return s
	}
	target := row.link.Name
	if target == "" {
		target = row.link.Path
	}
	if s.hooks.RemoveLink == nil {
		s.err = "unlinking is not available here"
		return s
	}
	if err := s.hooks.RemoveLink(s.item.Project, target); err != nil {
		s.err = err.Error()
		return s
	}
	s.status = "unlinked " + target + "; the page and the folder stay"
	s.changed = true
	return s
}

func (s linksScreen) startEdit() (linksScreen, tea.Cmd) {
	row := s.current()
	if row == nil {
		return s, nil
	}
	if row.link.Name == "" || row.link.Path == "" {
		s.err = "this link has no page to edit; refresh, or unlink it"
		return s, nil
	}
	if s.hooks.EditLink == nil {
		s.err = "editing links is not available here"
		return s, nil
	}
	s.edit = newLinkEditor(s.hooks, links.Page{Kind: row.link.Kind, Name: row.link.Name, Path: row.link.Path})
	s.mode = linksEdit
	return s, nil
}

// label is the box's first line: the page name, or the path when it has none.
func (r linkRow) label() string {
	if r.link.Name != "" {
		return r.link.Name
	}
	if r.link.Path != "" {
		return home.Display(r.link.Path)
	}
	return r.link.Entry
}

func (s linksScreen) view() string {
	p := s.item.Project
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", title.Render(p.Name), catSt.Render("links"), dim.Render(home.Display(p.VaultPath())))
	if s.mode == linksEdit {
		b.WriteString(s.edit.view())
		return b.String()
	}
	if len(s.rows) == 0 {
		b.WriteString("  " + dim.Render("no links yet; press a to link a git repo or a folder of material") + "\n")
	}
	width := min(72, max(32, s.width-6))
	for i, row := range s.rows {
		style := boxSt
		name := row.label()
		if i == s.cursor && s.mode != linksAdd {
			style = boxSelSt
			name = selSt.Render(name)
		}
		kind := dim.Render(row.link.Kind)
		pad := max(1, width-2-lipgloss.Width(row.label())-len(row.link.Kind))
		lines := []string{name + strings.Repeat(" ", pad) + kind}
		if row.link.Path != "" && row.link.Name != "" {
			lines = append(lines, home.Display(row.link.Path))
		}
		facts := row.facts
		if row.broken {
			facts = errSt.Render(facts)
		} else {
			facts = dim.Render(facts)
		}
		if len(row.shared) > 0 {
			facts += dim.Render(" · also " + strings.Join(row.shared, ", "))
		}
		lines = append(lines, facts)
		b.WriteString(indent(style.Width(width).Render(strings.Join(lines, "\n")), "  ") + "\n")
	}
	b.WriteString("\n")
	switch s.mode {
	case linksAdd:
		b.WriteString("  " + activeL.Width(9).Render("Add") + s.source.view("           ") + "\n")
		b.WriteString("  " + dim.Render("a folder path, or the name of a page another project links · "+pathHint()+" · Enter link · Esc cancel") + "\n")
	case linksConfirmUnlink:
		fmt.Fprintf(&b, "  %s Unlink %s from %s? The page and the folder stay.  %s\n",
			errSt.Render("▲"), s.current().label(), p.Name, title.Render("y")+" / "+title.Render("n"))
	default:
		hints := "a add"
		if len(s.rows) > 0 {
			hints = "↑↓ move · a add · e edit · u unlink"
		}
		b.WriteString("  " + dim.Render(hints+" · Esc back") + "\n")
	}
	if s.status != "" {
		b.WriteString("  " + okSt.Render(s.status) + "\n")
	}
	if s.err != "" {
		b.WriteString("  " + errSt.Render(s.err) + "\n")
	}
	return b.String()
}

func indent(block, pad string) string {
	return pad + strings.ReplaceAll(block, "\n", "\n"+pad)
}

// linkEditor edits one link page: its name, kind, and folder.
type linkEditor struct {
	hooks     Hooks
	page      links.Page
	name      string
	kind      string
	path      string
	field     int
	text      textinput.Model
	folder    pathField
	typing    bool
	err       string
	discard   bool
	saved     bool
	cancelled bool
}

const (
	linkFieldName = iota
	linkFieldKind
	linkFieldPath
	linkFieldCount
)

func newLinkEditor(hooks Hooks, page links.Page) linkEditor {
	text := textinput.New()
	text.Prompt = ""
	text.CharLimit = 120
	text.Width = 40
	return linkEditor{hooks: hooks, page: page, name: page.Name, kind: page.Kind, path: page.Path, text: text, folder: newPathField("", 60)}
}

func (e linkEditor) dirty() bool {
	return e.name != e.page.Name || e.kind != e.page.Kind || e.path != e.page.Path
}

func (e linkEditor) update(msg tea.Msg) (linkEditor, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if e.typing {
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				if e.field == linkFieldName {
					e.name = strings.TrimSpace(e.text.Value())
				} else if v := e.folder.value(); v != "" {
					e.path = home.Expand(v)
				}
				e.typing = false
				return e, nil
			case tea.KeyEsc:
				e.typing = false
				return e, nil
			}
		}
		var cmd tea.Cmd
		if e.field == linkFieldName {
			e.text, cmd = e.text.Update(msg)
		} else {
			e.folder, cmd = e.folder.update(msg)
		}
		return e, cmd
	}
	if !isKey {
		return e, nil
	}
	e.err = ""
	switch key.Type {
	case tea.KeyUp:
		e.field = (e.field + linkFieldCount - 1) % linkFieldCount
	case tea.KeyDown:
		e.field = (e.field + 1) % linkFieldCount
	case tea.KeyLeft, tea.KeyRight:
		if e.field == linkFieldKind {
			e.toggleKind()
		}
	case tea.KeyEnter:
		switch e.field {
		case linkFieldName:
			e.text.SetValue(e.name)
			e.text.CursorEnd()
			e.typing = true
			return e, e.text.Focus()
		case linkFieldKind:
			e.toggleKind()
		case linkFieldPath:
			e.folder.names = nil
			e.folder.setValue(home.Display(e.path))
			e.typing = true
			return e, e.folder.focus()
		}
	case tea.KeyEsc:
		if e.dirty() && !e.discard {
			e.discard = true
			e.err = "unsaved changes: press s to save, or Esc again to discard"
			return e, nil
		}
		e.cancelled = true
	default:
		if key.String() == "s" {
			return e.save(), nil
		}
	}
	return e, nil
}

func (e *linkEditor) toggleKind() {
	if e.kind == links.Repo {
		e.kind = links.Materials
	} else {
		e.kind = links.Repo
	}
}

func (e linkEditor) save() linkEditor {
	if !e.dirty() {
		e.cancelled = true
		return e
	}
	edit := vaults.LinkEdit{}
	if e.name != e.page.Name {
		edit.Name = e.name
	}
	if e.kind != e.page.Kind {
		edit.Kind = e.kind
	}
	if e.path != e.page.Path {
		edit.Path = e.path
	}
	updated, err := e.hooks.EditLink(e.page, edit)
	if err != nil {
		e.err = err.Error()
		return e
	}
	e.page = updated
	e.saved = true
	return e
}

func (e linkEditor) view() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s   %s\n\n", title.Render("Edit "+e.page.Name), dim.Render(e.page.Rel()+".md"))
	rows := [linkFieldCount]struct{ name, content string }{
		{"Name", e.name}, {"Kind", "◂ " + e.kind + " ▸"}, {"Path", home.Display(e.path)},
	}
	for f, r := range rows {
		marker, name := "  ", label.Width(8).Render(r.name)
		if f == e.field {
			marker, name = cursorSt.Render("▸ "), activeL.Width(8).Render(r.name)
		}
		content := r.content
		if e.typing && f == e.field {
			if f == linkFieldName {
				content = e.text.View()
			} else {
				content = e.folder.view("            ")
			}
		}
		changed := (f == linkFieldName && e.name != e.page.Name) || (f == linkFieldKind && e.kind != e.page.Kind) || (f == linkFieldPath && e.path != e.page.Path)
		if changed {
			content = strings.TrimRight(content, "\n") + modSt.Render("  •")
		}
		b.WriteString("  " + marker + name + content + "\n")
	}
	b.WriteString("\n")
	switch {
	case e.typing && e.field == linkFieldPath:
		b.WriteString("  " + dim.Render(pathHint()+" · Enter keep · Esc cancel") + "\n")
	case e.typing:
		b.WriteString("  " + dim.Render("Enter keep · Esc cancel") + "\n")
	default:
		hints := "↑↓ field · Enter edit · ←→ kind"
		if e.dirty() {
			hints += " · " + title.Render("s") + " save"
		}
		b.WriteString("  " + dim.Render(hints+" · Esc back") + "\n")
		b.WriteString("  " + dim.Render("renaming or changing the kind rewrites every project that links this page") + "\n")
	}
	if e.err != "" {
		b.WriteString("  " + errSt.Render(e.err) + "\n")
	}
	return b.String()
}
