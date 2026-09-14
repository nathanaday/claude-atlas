package tui

import (
	"errors"
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

// linksScreen shows a project's mounted repositories as boxes, one per link, and acts
// on them at once: mount an existing one, create a new one, edit the page, unlink.
// Every action is one backend call; the view refreshes afterwards so the facts catch
// up. It is embedded in the view.

type linksMode int

const (
	linksList linksMode = iota
	linksAdd
	linksConfirmInit
	linksNewName
	linksNewPath
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
	pending string // the folder awaiting a yes to git init
	name    textinput.Model
	where   pathField
	edit    linkEditor
	width   int
	status  string
	err     string
	changed bool // an action succeeded since the view last refreshed
	closed  bool
}

func newLinks(hooks Hooks, item *Item, items []Item, width int) linksScreen {
	s := linksScreen{hooks: hooks, item: item, width: width}
	s.source = newPathField("~/code/project, or the name of a repository another project links", 60)
	s.name = textinput.New()
	s.name.Prompt = ""
	s.name.Placeholder = "paper, slides, app"
	s.name.CharLimit = 80
	s.name.Width = 40
	s.where = newPathField("", 60)
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
		case l.Kind == links.Materials:
			row.facts, row.broken = "a folder, not a git repository; initialize git there and refresh, or unlink", true
		default:
			row.facts = "not refreshed yet"
		}
		if s.item.State != nil && l.Kind == links.Repo {
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
			case "n":
				if s.hooks.NewRepo == nil {
					s.err = "creating repositories is not available here"
					return s, nil
				}
				s.name.SetValue("")
				s.mode = linksNewName
				return s, s.name.Focus()
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
	case linksConfirmInit:
		if isKey {
			switch strings.ToLower(key.String()) {
			case "y":
				return s.link(s.pending, true), nil
			case "n", "esc":
				s.err = ""
				s.mode = linksList
			}
		}
		return s, nil
	case linksNewName:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				name := strings.TrimSpace(s.name.Value())
				if name == "" {
					s.err = "the repository needs a name"
					return s, nil
				}
				s.err = ""
				s.where.setValue(home.Display(s.item.Project.VaultPath() + "/" + name))
				s.mode = linksNewPath
				return s, s.where.focus()
			case tea.KeyEsc:
				s.err = ""
				s.mode = linksList
				return s, nil
			}
		}
		var cmd tea.Cmd
		s.name, cmd = s.name.Update(msg)
		return s, cmd
	case linksNewPath:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				return s.create(), nil
			case tea.KeyEsc:
				s.mode = linksNewName
				return s, s.name.Focus()
			}
		}
		var cmd tea.Cmd
		s.where, cmd = s.where.update(msg)
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
	return s.link(target, false)
}

// link mounts a repository; a plain folder is offered a git init first.
func (s linksScreen) link(target string, initGit bool) linksScreen {
	if s.hooks.AddLink == nil {
		s.err = "linking is not available here"
		return s
	}
	page, err := s.hooks.AddLink(s.item.Project, target, initGit)
	var notRepo *vaults.NotRepoError
	if errors.As(err, &notRepo) {
		s.pending = target
		s.err = ""
		s.mode = linksConfirmInit
		return s
	}
	if err != nil {
		s.err = err.Error()
		return s
	}
	s.err = ""
	s.status = "linked " + page.Name
	if initGit {
		s.status += "; it is a git repository now"
	}
	s.changed = true
	s.mode = linksList
	return s
}

// create makes a new repository where the fields say and mounts it.
func (s linksScreen) create() linksScreen {
	name := strings.TrimSpace(s.name.Value())
	at := s.where.value()
	if at == home.Display(s.item.Project.VaultPath()+"/"+name) {
		at = ""
	}
	page, err := s.hooks.NewRepo(s.item.Project, name, at)
	if err != nil {
		s.err = err.Error()
		return s
	}
	s.err = ""
	s.status = "created " + home.Display(page.Path) + " with its own git history, and linked it"
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
	fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", title.Render(p.Name), catSt.Render("repositories"), dim.Render(home.Display(p.VaultPath())))
	if s.mode == linksEdit {
		b.WriteString(s.edit.view())
		return b.String()
	}
	if len(s.rows) == 0 {
		b.WriteString("  " + dim.Render("no repositories yet: a mounted git repository holds this project's deliverables. Press n to create one, or a to link one that exists.") + "\n")
	}
	width := min(72, max(32, s.width-6))
	for i, row := range s.rows {
		style := boxSt
		name := row.label()
		if i == s.cursor && s.mode != linksAdd {
			style = boxSelSt
			name = selSt.Render(name)
		}
		kind := "repository"
		if row.link.Kind == links.Materials {
			kind = "folder"
		}
		badge := dim.Render(kind)
		pad := max(1, width-2-lipgloss.Width(row.label())-len(kind))
		lines := []string{name + strings.Repeat(" ", pad) + badge}
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
		b.WriteString("  " + activeL.Width(9).Render("Link") + s.source.view("           ") + "\n")
		b.WriteString("  " + dim.Render("a repository's folder, or the name of one another project links · "+pathHint()+" · Enter link · Esc cancel") + "\n")
	case linksConfirmInit:
		fmt.Fprintf(&b, "  %s %s is not a git repository. Initialize one there, with one commit of what it holds?  %s\n",
			errSt.Render("▲"), home.Display(s.pending), title.Render("y")+" / "+title.Render("n"))
	case linksNewName:
		b.WriteString("  " + activeL.Width(9).Render("Name") + s.name.View() + "\n")
		b.WriteString("  " + dim.Render("a new repository for this project's deliverables, with its own git history · Enter next · Esc cancel") + "\n")
	case linksNewPath:
		b.WriteString("  " + label.Width(9).Render("Name") + strings.TrimSpace(s.name.Value()) + "\n")
		b.WriteString("  " + activeL.Width(9).Render("Where") + s.where.view("           ") + "\n")
		b.WriteString("  " + dim.Render("the default sits beside the wiki in the vault, ignored by the vault's git; any folder on this machine works · "+pathHint()+" · Enter create · Esc back") + "\n")
	case linksConfirmUnlink:
		fmt.Fprintf(&b, "  %s Unlink %s from %s? The repository and its page stay.  %s\n",
			errSt.Render("▲"), s.current().label(), p.Name, title.Render("y")+" / "+title.Render("n"))
	default:
		hints := "n new repository · a link existing"
		if len(s.rows) > 0 {
			hints = "↑↓ move · n new · a link existing · e edit · u unlink"
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

// linkEditor edits one link page: its name and its folder.
type linkEditor struct {
	hooks     Hooks
	page      links.Page
	name      string
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
	linkFieldPath
	linkFieldCount
)

func newLinkEditor(hooks Hooks, page links.Page) linkEditor {
	text := textinput.New()
	text.Prompt = ""
	text.CharLimit = 120
	text.Width = 40
	return linkEditor{hooks: hooks, page: page, name: page.Name, path: page.Path, text: text, folder: newPathField("", 60)}
}

func (e linkEditor) dirty() bool {
	return e.name != e.page.Name || e.path != e.page.Path
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
	case tea.KeyEnter:
		switch e.field {
		case linkFieldName:
			e.text.SetValue(e.name)
			e.text.CursorEnd()
			e.typing = true
			return e, e.text.Focus()
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

func (e linkEditor) save() linkEditor {
	if !e.dirty() {
		e.cancelled = true
		return e
	}
	edit := vaults.LinkEdit{}
	if e.name != e.page.Name {
		edit.Name = e.name
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
		{"Name", e.name}, {"Path", home.Display(e.path)},
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
		changed := (f == linkFieldName && e.name != e.page.Name) || (f == linkFieldPath && e.path != e.page.Path)
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
		hints := "↑↓ field · Enter edit"
		if e.dirty() {
			hints += " · " + title.Render("s") + " save"
		}
		b.WriteString("  " + dim.Render(hints+" · Esc back") + "\n")
		b.WriteString("  " + dim.Render("renaming rewrites every project that links this page; the path must be a git repository") + "\n")
	}
	if e.err != "" {
		b.WriteString("  " + errSt.Render(e.err) + "\n")
	}
	return b.String()
}
