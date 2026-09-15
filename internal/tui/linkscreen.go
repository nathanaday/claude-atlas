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
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// linksScreen shows a project's mounted repositories as boxes, one per repository, and
// acts on them at once: mount one that exists, create one, clone one, edit it, drop it.
// Every action is one backend call; the view refreshes afterwards so the facts catch up.
// It is embedded in the view.

type linksMode int

const (
	linksList linksMode = iota
	linksAdd
	linksConfirmInit
	linksClonePath
	linksChanges
	linksNewName
	linksNewPath
	linksEdit
	linksConfirmUnlink
)

// linkRow is one box: the repository as the identity file holds it, and what the last
// refresh found in its folder.
type linkRow struct {
	repo   registry.Repo
	facts  string
	broken bool
}

type linksScreen struct {
	hooks   Hooks
	entry   registry.Entry
	rows    []linkRow
	cursor  int
	mode    linksMode
	source  pathField
	pending string     // the folder awaiting a yes to git init, or the URL awaiting a location
	asked   vault.Repo // the repository awaiting a change policy
	name    textinput.Model
	where   pathField
	edit    linkEditor
	width   int
	status  string
	err     string
	changed bool // an action succeeded since the view last refreshed
	closed  bool
}

func newLinks(hooks Hooks, e registry.Entry, width int) linksScreen {
	s := linksScreen{hooks: hooks, entry: e, width: width}
	s.source = newPathField("~/code/project, or https://github.com/you/repo to clone", 60)
	s.name = textinput.New()
	s.name.Prompt = ""
	s.name.Placeholder = "paper, slides, app"
	s.name.CharLimit = 80
	s.name.Width = 40
	s.where = newPathField("", 60)
	s.build()
	return s
}

// reload takes the entry again from the view's items after a refresh or an action.
func (s *linksScreen) reload(items []Item) {
	for i := range items {
		if items[i].Entry.Path == s.entry.Path {
			s.entry = items[i].Entry
		}
	}
	s.build()
}

// build turns the entry's repositories into boxes.
func (s *linksScreen) build() {
	s.rows = nil
	for _, r := range s.entry.Repos {
		row := linkRow{repo: r, facts: "not refreshed yet"}
		if r.Error != "" {
			row.facts, row.broken = r.Error, true
		}
		if s.entry.State != nil {
			if fact, ok := s.entry.State.RepoFacts[r.Name]; ok {
				row.facts, row.broken = refresh.LinkSummary(fact), !fact.OK
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
				return s.mount(s.pending, true), nil
			case "n", "esc":
				s.err = ""
				s.mode = linksList
			}
		}
		return s, nil
	case linksClonePath:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				return s.clone(), nil
			case tea.KeyEsc:
				s.err = ""
				s.mode = linksList
				return s, nil
			}
		}
		var cmd tea.Cmd
		s.where, cmd = s.where.update(msg)
		return s, cmd
	case linksChanges:
		if isKey {
			switch strings.ToLower(key.String()) {
			case "p":
				return s.setChanges(links.ChangesPR), nil
			case "c":
				return s.setChanges(links.ChangesCommit), nil
			case "esc":
				s.mode = linksList
			}
		}
		return s, nil
	case linksNewName:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				name := links.CleanName(strings.TrimSpace(s.name.Value()))
				if name == "" {
					s.err = "the repository needs a name"
					return s, nil
				}
				s.err = ""
				s.where.setValue(home.Display(s.entry.RepoDir(name)))
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
			s.status = "edited " + s.edit.repo.Name
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

// record adds a repository the backend just wrote to the screen's own entry, so the
// boxes and the next call see it before the refresh lands.
func (s *linksScreen) record(repo vault.Repo, path string) {
	s.entry.Repos = append(s.entry.Repos, registry.Repo{Name: repo.Name, Path: path, Remote: repo.Remote, Changes: repo.Changes})
	s.changed = true
	s.build()
}

// add mounts what the field names: a folder, or a URL to clone.
func (s linksScreen) add() linksScreen {
	target := s.source.value()
	if target == "" {
		s.mode = linksList
		return s
	}
	if links.IsRemoteURL(target) {
		if s.hooks.CloneRepo == nil {
			s.err = "cloning is not available here"
			return s
		}
		s.pending = target
		s.err = ""
		s.where.setValue(home.Display(s.entry.RepoDir(links.NameFromURL(target))))
		s.mode = linksClonePath
		return s
	}
	return s.mount(target, false)
}

// clone clones the pending URL where the field says and mounts it, then asks how changes
// should land there.
func (s linksScreen) clone() linksScreen {
	at := s.where.value()
	if at == home.Display(s.entry.RepoDir(links.NameFromURL(s.pending))) {
		at = ""
	}
	repo, path, err := s.hooks.CloneRepo(s.entry, s.pending, at)
	if err != nil {
		s.err = err.Error()
		return s
	}
	s.err = ""
	s.status = "cloned " + repo.Name + " into " + home.Display(path) + ", and mounted it"
	s.record(repo, path)
	return s.askChanges(repo)
}

// askChanges asks how changes land in a repository with a remote and no policy yet.
func (s linksScreen) askChanges(repo vault.Repo) linksScreen {
	if repo.Remote == "" || repo.Changes != "" || s.hooks.EditRepo == nil {
		s.mode = linksList
		return s
	}
	s.asked = repo
	s.mode = linksChanges
	return s
}

func (s linksScreen) setChanges(policy string) linksScreen {
	repo, err := s.hooks.EditRepo(s.entry, s.asked.Name, vaults.RepoEdit{Changes: &policy})
	if err != nil {
		s.err = err.Error()
		s.mode = linksList
		return s
	}
	for i := range s.entry.Repos {
		if s.entry.Repos[i].Name == repo.Name {
			s.entry.Repos[i].Changes = repo.Changes
		}
	}
	s.status = repo.Name + ": " + links.PolicyText(policy)
	s.changed = true
	s.mode = linksList
	s.build()
	return s
}

// mount mounts a folder; a plain folder is offered a git init first.
func (s linksScreen) mount(target string, initGit bool) linksScreen {
	if s.hooks.AddRepo == nil {
		s.err = "mounting repositories is not available here"
		return s
	}
	repo, path, err := s.hooks.AddRepo(s.entry, target, initGit)
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
	s.status = "mounted " + repo.Name
	if initGit {
		s.status += "; it is a git repository now"
	}
	s.record(repo, path)
	return s.askChanges(repo)
}

// create makes a new repository where the fields say and mounts it.
func (s linksScreen) create() linksScreen {
	name := links.CleanName(strings.TrimSpace(s.name.Value()))
	at := s.where.value()
	if at == home.Display(s.entry.RepoDir(name)) {
		at = ""
	}
	repo, path, err := s.hooks.NewRepo(s.entry, name, at)
	if err != nil {
		s.err = err.Error()
		return s
	}
	s.err = ""
	s.status = "created " + home.Display(path) + " with its own git history, and mounted it"
	s.record(repo, path)
	s.mode = linksList
	return s
}

func (s linksScreen) unlink() linksScreen {
	row := s.current()
	s.mode = linksList
	if row == nil {
		return s
	}
	if s.hooks.RemoveRepo == nil {
		s.err = "unlinking is not available here"
		return s
	}
	name := row.repo.Name
	if err := s.hooks.RemoveRepo(s.entry, name); err != nil {
		s.err = err.Error()
		return s
	}
	var keep []registry.Repo
	for _, r := range s.entry.Repos {
		if r.Name != name {
			keep = append(keep, r)
		}
	}
	s.entry.Repos = keep
	s.status = "unlinked " + name + "; the folder stays"
	s.changed = true
	s.build()
	return s
}

func (s linksScreen) startEdit() (linksScreen, tea.Cmd) {
	row := s.current()
	if row == nil {
		return s, nil
	}
	if s.hooks.EditRepo == nil {
		s.err = "editing repositories is not available here"
		return s, nil
	}
	s.edit = newLinkEditor(s.hooks, s.entry, row.repo)
	s.mode = linksEdit
	return s, nil
}

func (s linksScreen) view() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", title.Render(s.entry.Name), catSt.Render("repositories"), dim.Render(home.Display(s.entry.Path)))
	if s.mode == linksEdit {
		b.WriteString(s.edit.view())
		return b.String()
	}
	if len(s.rows) == 0 {
		b.WriteString("  " + dim.Render("no repositories yet: a mounted git repository holds this project's deliverables. Press n to create one, or a to mount one that exists.") + "\n")
	}
	width := min(72, max(32, s.width-6))
	for i, row := range s.rows {
		style := boxSt
		name := row.repo.Name
		if i == s.cursor && s.mode != linksAdd {
			style = boxSelSt
			name = selSt.Render(row.repo.Name)
		}
		badge := "changes: " + links.Policy(row.repo.Changes, row.repo.Remote)
		pad := max(1, width-2-lipgloss.Width(row.repo.Name)-len(badge))
		lines := []string{name + strings.Repeat(" ", pad) + dim.Render(badge)}
		if row.repo.Path != "" {
			lines = append(lines, home.Display(row.repo.Path))
		}
		if row.repo.Remote != "" {
			lines = append(lines, dim.Render(row.repo.Remote))
		}
		facts := dim.Render(row.facts)
		if row.broken {
			facts = errSt.Render(row.facts)
		}
		lines = append(lines, facts)
		b.WriteString(indent(style.Width(width).Render(strings.Join(lines, "\n")), "  ") + "\n")
	}
	b.WriteString("\n")
	switch s.mode {
	case linksAdd:
		b.WriteString("  " + activeL.Width(9).Render("Mount") + s.source.view("           ") + "\n")
		b.WriteString("  " + dim.Render("a git repository's folder, or a URL to clone · "+pathHint()+" · Enter mount · Esc cancel") + "\n")
	case linksClonePath:
		b.WriteString("  " + label.Width(9).Render("Clone") + s.pending + "\n")
		b.WriteString("  " + activeL.Width(9).Render("Into") + s.where.view("           ") + "\n")
		b.WriteString("  " + dim.Render("the default sits under repos/ in the vault, ignored by the vault's git · "+pathHint()+" · Enter clone · Esc cancel") + "\n")
	case linksChanges:
		fmt.Fprintf(&b, "  %s %s has a remote, %s. How should claude-atlas land its changes there?\n", title.Render("?"), s.asked.Name, s.asked.Remote)
		b.WriteString("  " + title.Render("p") + " pull requests: work on a branch and open a PR; never push to the default branch\n")
		b.WriteString("  " + title.Render("c") + " commits on the current branch\n")
		b.WriteString("  " + dim.Render("Esc decide later (pull requests until then)") + "\n")
	case linksConfirmInit:
		fmt.Fprintf(&b, "  %s %s is not a git repository. Initialize one there, with one commit of what it holds?  %s\n",
			errSt.Render("▲"), home.Display(s.pending), title.Render("y")+" / "+title.Render("n"))
	case linksNewName:
		b.WriteString("  " + activeL.Width(9).Render("Name") + s.name.View() + "\n")
		b.WriteString("  " + dim.Render("a new repository for this project's deliverables, with its own git history · Enter next · Esc cancel") + "\n")
	case linksNewPath:
		b.WriteString("  " + label.Width(9).Render("Name") + strings.TrimSpace(s.name.Value()) + "\n")
		b.WriteString("  " + activeL.Width(9).Render("Where") + s.where.view("           ") + "\n")
		b.WriteString("  " + dim.Render("the default sits under repos/ in the vault, ignored by the vault's git; any folder outside the vault works too · "+pathHint()+" · Enter create · Esc back") + "\n")
	case linksConfirmUnlink:
		fmt.Fprintf(&b, "  %s Unlink %s from %s? The folder and its history stay.  %s\n",
			errSt.Render("▲"), s.current().repo.Name, s.entry.Name, title.Render("y")+" / "+title.Render("n"))
	default:
		hints := "n new repository · a mount one that exists"
		if len(s.rows) > 0 {
			hints = "↑↓ move · n new · a mount · e edit · u unlink"
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

// linkEditor edits one mounted repository: the folder it mounts, its remote, and how
// changes land there. A repository's name comes from its folder and does not change.
type linkEditor struct {
	hooks     Hooks
	entry     registry.Entry
	repo      registry.Repo
	path      string
	remote    string
	changes   string
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
	linkFieldPath = iota
	linkFieldRemote
	linkFieldChanges
	linkFieldCount
)

func newLinkEditor(hooks Hooks, e registry.Entry, repo registry.Repo) linkEditor {
	text := textinput.New()
	text.Prompt = ""
	text.CharLimit = 200
	text.Width = 40
	return linkEditor{hooks: hooks, entry: e, repo: repo, path: repo.Path, remote: repo.Remote, changes: repo.Changes,
		text: text, folder: newPathField("", 60)}
}

func (e linkEditor) dirty() bool {
	return e.path != e.repo.Path || e.remote != e.repo.Remote || e.changes != e.repo.Changes
}

// cycleChanges moves through default, pr, commit.
func (e *linkEditor) cycleChanges() {
	switch e.changes {
	case "":
		e.changes = links.ChangesPR
	case links.ChangesPR:
		e.changes = links.ChangesCommit
	default:
		e.changes = ""
	}
}

func (e linkEditor) update(msg tea.Msg) (linkEditor, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if e.typing {
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				if e.field == linkFieldRemote {
					e.remote = strings.TrimSpace(e.text.Value())
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
		if e.field == linkFieldPath {
			e.folder, cmd = e.folder.update(msg)
		} else {
			e.text, cmd = e.text.Update(msg)
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
		if e.field == linkFieldChanges {
			e.cycleChanges()
		}
	case tea.KeyEnter:
		switch e.field {
		case linkFieldRemote:
			e.text.SetValue(e.remote)
			e.text.CursorEnd()
			e.typing = true
			return e, e.text.Focus()
		case linkFieldChanges:
			e.cycleChanges()
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
	edit := vaults.RepoEdit{}
	if e.path != e.repo.Path {
		edit.Path = e.path
	}
	if e.remote != e.repo.Remote {
		remote := e.remote
		edit.Remote = &remote
	}
	if e.changes != e.repo.Changes {
		changes := e.changes
		edit.Changes = &changes
	}
	updated, err := e.hooks.EditRepo(e.entry, e.repo.Name, edit)
	if err != nil {
		e.err = err.Error()
		return e
	}
	e.repo.Remote, e.repo.Changes, e.repo.Path = updated.Remote, updated.Changes, e.path
	e.saved = true
	return e
}

func (e linkEditor) view() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  %s\n\n", title.Render("Edit "+e.repo.Name))
	changesShown := "◂ default: " + links.Policy("", e.remote) + " ▸"
	if e.changes != "" {
		changesShown = "◂ " + e.changes + " ▸"
	}
	remoteShown := e.remote
	if remoteShown == "" {
		remoteShown = dim.Render("none")
	}
	rows := [linkFieldCount]struct{ name, content string }{
		{"Path", home.Display(e.path)}, {"Remote", remoteShown}, {"Changes", changesShown},
	}
	for f, r := range rows {
		marker, name := "  ", label.Width(8).Render(r.name)
		if f == e.field {
			marker, name = cursorSt.Render("▸ "), activeL.Width(8).Render(r.name)
		}
		content := r.content
		if e.typing && f == e.field {
			if f == linkFieldPath {
				content = e.folder.view("            ")
			} else {
				content = e.text.View()
			}
		}
		changed := (f == linkFieldPath && e.path != e.repo.Path) ||
			(f == linkFieldRemote && e.remote != e.repo.Remote) || (f == linkFieldChanges && e.changes != e.repo.Changes)
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
		hints := "↑↓ field · Enter edit · ←→ changes"
		if e.dirty() {
			hints += " · " + title.Render("s") + " save"
		}
		b.WriteString("  " + dim.Render(hints+" · Esc back") + "\n")
		b.WriteString("  " + dim.Render("changes: pr means a branch and a pull request, commit means the current branch") + "\n")
	}
	if e.err != "" {
		b.WriteString("  " + errSt.Render(e.err) + "\n")
	}
	return b.String()
}
