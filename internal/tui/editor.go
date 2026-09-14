package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/tree"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// Hooks connect the screens to the atlas without the screens touching disk themselves.
// Every hook is one backend call that a CLI command also exposes.
type Hooks struct {
	Load       func() ([]*tree.Project, error)
	Categories func() []string
	State      func(rel string) *tree.State
	Update     func(*tree.Project, vaults.Edit) error
	Unlink     func(*tree.Project) error
	// Create makes or adopts a vault and registers it; it returns the project's rel.
	Create func(AddVault) (string, error)
	// Refresh rebuilds derived state for every project.
	Refresh func() error
	// StagePlan says which files under a source are new to the project's vault; an empty
	// source means its linked material folders.
	StagePlan func(*tree.Project, string) (*capture.StagePlan, error)
	// Stage copies a plan's files into the inbox and links new folders; it returns the
	// folders it linked.
	Stage func(*tree.Project, *capture.StagePlan) (*capture.StageResult, []string, error)
	// Links lists the link pages in the atlas, so a folder another project uses can be
	// linked by name. AddLink, RemoveLink, and EditLink act on one project's links and
	// one link page; the view refreshes after each.
	Links      func() []links.Page
	AddLink    func(*tree.Project, string) (links.Page, error)
	RemoveLink func(*tree.Project, string) error
	EditLink   func(links.Page, vaults.LinkEdit) (links.Page, error)
	VaultsDir  string
}

type editMode int

const (
	editFields editMode = iota
	editText
	editPath
	editCategory
	editList
	editListPick
	confirmRemove
	confirmMove
)

// editOutcome says how an editor session ended.
type editOutcome int

const (
	editOpen editOutcome = iota
	editCancelled
	editSaved
	editRemoved
)

const (
	fieldName = iota
	fieldPurpose
	fieldCategory
	fieldVault
	fieldPriority
	fieldState
	fieldBlockedOn
	fieldReviewAfter
	fieldDone
	fieldRelated
	fieldCount
)

var fieldNames = [fieldCount]string{"Name", "Purpose", "Category", "Vault", "Priority", "State", "Blocked on", "Review after", "Done when", "Related"}

// labelWidth fits the longest field name, "Review after".
const labelWidth = 13

type draft struct {
	Name, Purpose, Category, Vault, Priority, State string
	BlockedOn, ReviewAfter, Done                    string
	Related                                         []string // project rels
}

func draftOf(p *tree.Project) draft {
	return draft{
		Name: p.Name, Purpose: p.Purpose, Category: p.Category(), Vault: p.VaultPath(), Priority: p.Priority, State: p.State,
		BlockedOn: p.BlockedOn, ReviewAfter: p.ReviewAfter, Done: p.DefinitionOfDone,
		Related: append([]string{}, p.RelatedTo...),
	}
}

func (d draft) equal(o draft) bool {
	for f := 0; f < fieldCount; f++ {
		if d.get(f) != o.get(f) {
			return false
		}
	}
	return true
}

func (d draft) get(field int) string {
	if field == fieldRelated {
		return strings.Join(d.Related, "\x00")
	}
	return [fieldCount]string{d.Name, d.Purpose, d.Category, d.Vault, d.Priority, d.State, d.BlockedOn, d.ReviewAfter, d.Done, ""}[field]
}

func (d *draft) set(field int, v string) {
	switch field {
	case fieldName:
		d.Name = v
	case fieldPurpose:
		d.Purpose = v
	case fieldCategory:
		d.Category = v
	case fieldVault:
		d.Vault = v
	case fieldPriority:
		d.Priority = v
	case fieldState:
		d.State = v
	case fieldBlockedOn:
		d.BlockedOn = v
	case fieldReviewAfter:
		d.ReviewAfter = v
	case fieldDone:
		d.Done = v
	}
}

// editor edits one project's page: its name, purpose, category, vault, priority, state,
// and related projects. It also removes the project from the atlas. It is embedded in
// the view; links have their own screen.
type editor struct {
	hooks     Hooks
	current   *tree.Project
	projects  []*tree.Project
	original  draft
	draft     draft
	field     int
	text      textinput.Model
	path      pathField
	picker    picker
	pick      picker
	listPos   int
	mode      editMode
	err       string
	discard   bool
	pendingMv vaults.Edit
	outcome   editOutcome
	// rel is where the project's page sits after a save; a category change moves it.
	rel string
}

var (
	selSt = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	okSt  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	modSt = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F00"))
)

func newEditor(hooks Hooks, p *tree.Project) editor {
	text := textinput.New()
	text.Prompt = ""
	text.CharLimit = 300
	text.Width = 60
	var known []string
	if hooks.Categories != nil {
		known = hooks.Categories()
	}
	var projects []*tree.Project
	if hooks.Load != nil {
		projects, _ = hooks.Load()
	}
	// The project as the tree loaded it, so related projects resolve.
	for _, q := range projects {
		if q.Rel == p.Rel {
			p = q
		}
	}
	e := editor{hooks: hooks, current: p, projects: projects, original: draftOf(p), draft: draftOf(p), text: text, picker: newPicker(known), rel: p.Rel}
	e.path = newPathField("~/Documents/Vaults/project", 60)
	return e
}

func (e editor) dirty() bool { return !e.draft.equal(e.original) }

func (e editor) listLen() int { return len(e.draft.Related) }

func (e *editor) removeAt(pos int) {
	e.draft.Related = append(append([]string{}, e.draft.Related[:pos]...), e.draft.Related[pos+1:]...)
}

func (e editor) update(msg tea.Msg) (editor, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	switch e.mode {
	case editFields:
		return e.updateFields(msg)
	case editText:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				e.draft.set(e.field, strings.TrimSpace(e.text.Value()))
				e.mode = editFields
				return e, nil
			case tea.KeyEsc:
				e.mode = editFields
				return e, nil
			}
		}
		var cmd tea.Cmd
		e.text, cmd = e.text.Update(msg)
		return e, cmd
	case editPath:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				if v := e.path.value(); v != "" {
					abs, _ := filepath.Abs(home.Expand(v))
					e.draft.Vault = abs
				}
				e.mode = editFields
				return e, nil
			case tea.KeyEsc:
				e.mode = editFields
				return e, nil
			}
		}
		var cmd tea.Cmd
		e.path, cmd = e.path.update(msg)
		return e, cmd
	case editCategory:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				e.draft.Category = e.picker.selected().value
				e.mode = editFields
				return e, nil
			case tea.KeyEsc:
				e.mode = editFields
				return e, nil
			}
		}
		var cmd tea.Cmd
		e.picker, cmd = e.picker.update(msg)
		return e, cmd
	case editList:
		if !isKey {
			return e, nil
		}
		n := e.listLen()
		switch key.Type {
		case tea.KeyUp:
			if n > 0 {
				e.listPos = (e.listPos + n - 1) % n
			}
		case tea.KeyDown:
			if n > 0 {
				e.listPos = (e.listPos + 1) % n
			}
		case tea.KeyEsc, tea.KeyEnter:
			e.mode = editFields
		default:
			switch key.String() {
			case "a":
				return e.startAdd()
			case "d":
				if n > 0 {
					e.removeAt(e.listPos)
					if e.listPos >= n-1 && e.listPos > 0 {
						e.listPos--
					}
				}
			}
		}
		return e, nil
	case editListPick:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				if opt := e.pick.selected(); opt.value != "" {
					e.draft.Related = append(e.draft.Related, opt.value)
					e.listPos = len(e.draft.Related) - 1
				}
				e.mode = editList
				return e, nil
			case tea.KeyEsc:
				e.mode = editList
				return e, nil
			}
		}
		var cmd tea.Cmd
		e.pick, cmd = e.pick.update(msg)
		return e, cmd
	case confirmRemove:
		if isKey {
			switch strings.ToLower(key.String()) {
			case "y":
				if err := e.hooks.Unlink(e.current); err != nil {
					e.err = err.Error()
					e.mode = editFields
					return e, nil
				}
				e.outcome = editRemoved
			case "n", "esc":
				e.mode = editFields
			}
		}
		return e, nil
	case confirmMove:
		if isKey {
			switch strings.ToLower(key.String()) {
			case "y":
				e.pendingMv.MoveVault = true
				return e.apply(e.pendingMv), nil
			case "n", "esc":
				e.mode = editFields
			}
		}
		return e, nil
	}
	return e, nil
}

// startAdd opens the project picker for a relation.
func (e editor) startAdd() (editor, tea.Cmd) {
	e.err = ""
	opts := e.relatedOptions()
	if len(opts) == 0 {
		e.err = "every other project is related already"
		return e, nil
	}
	e.pick = newOptionPicker(opts)
	e.mode = editListPick
	return e, e.pick.focus()
}

// relatedOptions lists the projects the draft could relate to.
func (e editor) relatedOptions() []option {
	var opts []option
	for _, q := range e.projects {
		if q.Rel == e.current.Rel || contains(e.draft.Related, q.Rel) {
			continue
		}
		opts = append(opts, option{label: q.Name + "  " + dim.Render(q.Rel), value: q.Rel, match: q.Name + " " + q.Rel})
	}
	return opts
}

func (e editor) updateFields(msg tea.Msg) (editor, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}
	e.err = ""
	switch key.Type {
	case tea.KeyUp:
		e.field = (e.field + fieldCount - 1) % fieldCount
	case tea.KeyDown:
		e.field = (e.field + 1) % fieldCount
	case tea.KeyLeft, tea.KeyRight:
		if e.field == fieldPriority || e.field == fieldState {
			e.cycle(key.Type == tea.KeyRight)
		}
	case tea.KeyEnter:
		switch e.field {
		case fieldCategory:
			e.picker.reset()
			e.mode = editCategory
			return e, e.picker.focus()
		case fieldPriority, fieldState:
			e.cycle(true)
		case fieldRelated:
			e.listPos = 0
			e.mode = editList
		case fieldVault:
			e.path.names = nil
			e.path.setValue(home.Display(e.draft.Vault))
			e.mode = editPath
			return e, e.path.focus()
		default:
			e.text.SetValue(e.draft.get(e.field))
			e.text.CursorEnd()
			e.mode = editText
			return e, e.text.Focus()
		}
	case tea.KeyEsc:
		if e.dirty() && !e.discard {
			e.discard = true
			e.err = "unsaved changes: press s to save, or Esc again to discard"
			return e, nil
		}
		e.outcome = editCancelled
	default:
		switch key.String() {
		case "s":
			return e.save(), nil
		case "r":
			e.mode = confirmRemove
		}
	}
	return e, nil
}

func (e *editor) cycle(forward bool) {
	values := tree.Priorities
	if e.field == fieldState {
		values = tree.States
	}
	current := e.draft.get(e.field)
	idx := 0
	for i, v := range values {
		if v == current {
			idx = i
		}
	}
	if forward {
		idx = (idx + 1) % len(values)
	} else {
		idx = (idx + len(values) - 1) % len(values)
	}
	e.draft.set(e.field, values[idx])
}

func (e editor) save() editor {
	if !e.dirty() {
		e.outcome = editCancelled
		return e
	}
	if strings.TrimSpace(e.draft.Name) == "" {
		e.err = "the name cannot be empty"
		return e
	}
	if !vaults.ValidReviewDate(e.draft.ReviewAfter) {
		e.err = "review after must be a date like 2026-10-01"
		e.field = fieldReviewAfter
		return e
	}
	edit := vaults.Edit{Name: e.draft.Name, Priority: e.draft.Priority, State: e.draft.State}
	if e.draft.Purpose != e.original.Purpose {
		edit.Purpose = e.draft.Purpose
		edit.ClearPurpose = e.draft.Purpose == ""
	}
	if e.draft.BlockedOn != e.original.BlockedOn {
		v := e.draft.BlockedOn
		edit.BlockedOn = &v
	}
	if e.draft.ReviewAfter != e.original.ReviewAfter {
		v := e.draft.ReviewAfter
		edit.ReviewAfter = &v
	}
	if e.draft.Done != e.original.Done {
		v := e.draft.Done
		edit.DefinitionOfDone = &v
	}
	if e.draft.Category != e.original.Category {
		cat := e.draft.Category
		edit.Category = &cat
	}
	if e.draft.get(fieldRelated) != e.original.get(fieldRelated) {
		related := append([]string{}, e.draft.Related...)
		edit.Related = &related
	}
	if e.draft.Vault != e.original.Vault {
		target, _ := filepath.Abs(home.Expand(e.draft.Vault))
		edit.Vault = target
		if _, err := os.Stat(target); err != nil {
			e.pendingMv = edit
			e.mode = confirmMove
			return e
		}
	}
	return e.apply(edit)
}

func (e editor) apply(edit vaults.Edit) editor {
	if err := e.hooks.Update(e.current, edit); err != nil {
		e.err = err.Error()
		e.mode = editFields
		return e
	}
	rel := e.current.ID()
	if edit.Category != nil {
		if *edit.Category != "" {
			rel = *edit.Category + "/" + rel
		}
	} else if e.current.Category() != "" {
		rel = e.current.Category() + "/" + rel
	}
	e.rel = rel
	e.outcome = editSaved
	return e
}

func (e editor) view() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s\n\n", title.Render(e.current.Name), dim.Render("tree/"+e.current.Rel+".md"))
	for f := 0; f < fieldCount; f++ {
		marker := "  "
		name := label.Width(labelWidth).Render(fieldNames[f])
		if f == e.field {
			marker = cursorSt.Render("▸ ")
			name = activeL.Width(labelWidth).Render(fieldNames[f])
		}
		var content string
		switch {
		case e.mode == editText && f == e.field:
			content = e.text.View()
		case e.mode == editPath && f == e.field:
			content = e.path.view(listPad)
		case e.mode == editCategory && f == e.field:
			content = e.picker.view(listPad)
		case f == fieldPriority || f == fieldState:
			content = "◂ " + e.draft.get(f) + " ▸"
		case f == fieldCategory:
			content = e.draft.Category
			if content == "" {
				content = topLevel
			}
		case (e.mode == editList || e.mode == editListPick) && f == e.field:
			content = e.viewList()
		case f == fieldRelated:
			content = e.relatedSummary()
		case f == fieldVault:
			content = home.Display(e.draft.Vault)
		default:
			content = e.draft.get(f)
			if content == "" {
				content = dim.Render("none")
			}
		}
		if e.draft.get(f) != e.original.get(f) {
			content = strings.TrimRight(content, "\n") + modSt.Render("  •")
		}
		b.WriteString("  " + marker + name + strings.TrimRight(content, "\n") + "\n")
	}
	b.WriteString("\n")
	switch e.mode {
	case confirmRemove:
		fmt.Fprintf(&b, "  %s Remove %s from the atlas? The vault at %s stays on disk.  %s\n",
			errSt.Render("▲"), e.current.Name, home.Display(e.current.VaultPath()), title.Render("y")+" / "+title.Render("n"))
	case confirmMove:
		fmt.Fprintf(&b, "  %s %s does not exist. Move the vault directory there?  %s\n",
			errSt.Render("▲"), home.Display(e.pendingMv.Vault), title.Render("y")+" / "+title.Render("n"))
	case editList:
		b.WriteString("  " + dim.Render("↑↓ choose · a add a project · d remove · Esc done") + "\n")
	case editListPick:
		b.WriteString("  " + dim.Render("↑↓ choose · type to filter · Enter add · Esc cancel") + "\n")
	case editText:
		b.WriteString("  " + dim.Render("Enter keep · Esc cancel") + "\n")
	case editPath:
		b.WriteString("  " + dim.Render(pathHint()+" · Enter keep · Esc cancel") + "\n")
	case editCategory:
		b.WriteString("  " + dim.Render("↑↓ choose · type to filter or name a new category · Enter keep · Esc cancel") + "\n")
	default:
		hints := "↑↓ field · Enter edit · ←→ change"
		if e.dirty() {
			hints += " · " + title.Render("s") + " save"
		}
		hints += " · r remove · Esc back"
		b.WriteString("  " + dim.Render(hints) + "\n")
	}
	if e.err != "" {
		b.WriteString("  " + errSt.Render(e.err) + "\n")
	}
	return b.String()
}

func (e editor) relatedSummary() string {
	names := e.relatedNames(e.draft.Related)
	from := e.relatedFrom()
	switch {
	case len(names) == 0 && len(from) == 0:
		return dim.Render("none")
	case len(names) == 0:
		return dim.Render("related from " + strings.Join(from, ", "))
	}
	out := strings.Join(names, ", ")
	if len(from) > 0 {
		out += dim.Render("  · related from " + strings.Join(from, ", "))
	}
	return out
}

func (e editor) relatedNames(rels []string) []string {
	var names []string
	for _, rel := range rels {
		name := rel
		for _, q := range e.projects {
			if q.Rel == rel {
				name = q.Name
			}
		}
		names = append(names, name)
	}
	return names
}

// relatedFrom names the projects whose own page relates to this one.
func (e editor) relatedFrom() []string {
	var names []string
	for _, q := range tree.RelatedFrom(e.projects, e.current) {
		names = append(names, q.Name)
	}
	return names
}

// listPad indents what sits under a field: two spaces, the marker, and the label.
const listPad = "                 "

// viewList renders the list editor for related projects.
func (e editor) viewList() string {
	var b strings.Builder
	n := e.listLen()
	if n == 0 && e.mode == editList {
		b.WriteString(dim.Render("none yet; press a to relate a project") + "\n")
	}
	for i := 0; i < n; i++ {
		marker := "  "
		rel := e.draft.Related[i]
		text := fmt.Sprintf("%-24s %s", e.relatedNames([]string{rel})[0], dim.Render(rel))
		if i == e.listPos && e.mode == editList {
			marker = cursorSt.Render("▸ ")
			text = cursorSt.Render(text)
		}
		b.WriteString(marker + text + "\n")
	}
	if e.mode == editList {
		if from := e.relatedFrom(); len(from) > 0 {
			b.WriteString("  " + dim.Render("related from "+strings.Join(from, ", ")+"; edit those on their own pages") + "\n")
		}
	}
	if e.mode == editListPick {
		b.WriteString("  " + e.pick.view("  ") + "\n")
	}
	if e.err != "" && e.mode != editList {
		b.WriteString("  " + errSt.Render(e.err) + "\n")
	}
	return "\n" + listPad + strings.ReplaceAll(strings.TrimRight(b.String(), "\n"), "\n", "\n"+listPad)
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
