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
	Stage     func(*tree.Project, *capture.StagePlan) (*capture.StageResult, []string, error)
	VaultsDir string
}

type editMode int

const (
	editFields editMode = iota
	editText
	editCategory
	editList
	editListText
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
	fieldRepos
	fieldMaterials
	fieldCount
)

var fieldNames = [fieldCount]string{"Name", "Purpose", "Category", "Vault", "Priority", "State", "Blocked on", "Review after", "Done when", "Repos", "Materials"}

type draft struct {
	Name, Purpose, Category, Vault, Priority, State string
	BlockedOn, ReviewAfter, Done                    string
	Repos, Materials                                []string
}

func draftOf(p *tree.Project) draft {
	return draft{
		Name: p.Name, Purpose: p.Purpose, Category: p.Category(), Vault: p.VaultPath(), Priority: p.Priority, State: p.State,
		BlockedOn: p.BlockedOn, ReviewAfter: p.ReviewAfter, Done: p.DefinitionOfDone,
		Repos: append([]string{}, p.Repos...), Materials: append([]string{}, p.Materials...),
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

func (d draft) list(field int) []string {
	if field == fieldRepos {
		return d.Repos
	}
	return d.Materials
}

func (d *draft) setList(field int, list []string) {
	if field == fieldRepos {
		d.Repos = list
	} else {
		d.Materials = list
	}
}

func (d draft) get(field int) string {
	switch field {
	case fieldRepos, fieldMaterials:
		return strings.Join(d.list(field), ", ")
	}
	return [fieldCount]string{d.Name, d.Purpose, d.Category, d.Vault, d.Priority, d.State, d.BlockedOn, d.ReviewAfter, d.Done, "", ""}[field]
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
// and links. It also removes the project from the atlas. It is embedded in the view.
type editor struct {
	hooks     Hooks
	current   *tree.Project
	original  draft
	draft     draft
	field     int
	text      textinput.Model
	picker    picker
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
	return editor{hooks: hooks, current: p, original: draftOf(p), draft: draftOf(p), text: text, picker: newPicker(known), rel: p.Rel}
}

func (e editor) dirty() bool { return !e.draft.equal(e.original) }

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
		list := e.draft.list(e.field)
		switch key.Type {
		case tea.KeyUp:
			if len(list) > 0 {
				e.listPos = (e.listPos + len(list) - 1) % len(list)
			}
		case tea.KeyDown:
			if len(list) > 0 {
				e.listPos = (e.listPos + 1) % len(list)
			}
		case tea.KeyEsc, tea.KeyEnter:
			e.mode = editFields
		default:
			switch key.String() {
			case "a":
				e.text.SetValue("")
				e.mode = editListText
				return e, e.text.Focus()
			case "d":
				if len(list) > 0 {
					e.draft.setList(e.field, append(append([]string{}, list[:e.listPos]...), list[e.listPos+1:]...))
					if e.listPos >= len(list)-1 && e.listPos > 0 {
						e.listPos--
					}
				}
			}
		}
		return e, nil
	case editListText:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				path := strings.TrimSpace(e.text.Value())
				if path != "" {
					abs, _ := filepath.Abs(home.Expand(path))
					if info, err := os.Stat(abs); err != nil || !info.IsDir() {
						e.err = home.Display(abs) + " is not a directory"
						return e, nil
					}
					e.draft.setList(e.field, append(e.draft.list(e.field), abs))
					e.listPos = len(e.draft.list(e.field)) - 1
				}
				e.err = ""
				e.mode = editList
				return e, nil
			case tea.KeyEsc:
				e.err = ""
				e.mode = editList
				return e, nil
			}
		}
		var cmd tea.Cmd
		e.text, cmd = e.text.Update(msg)
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
		case fieldRepos, fieldMaterials:
			e.listPos = 0
			e.mode = editList
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
	if strings.Join(e.draft.Repos, "\x00") != strings.Join(e.original.Repos, "\x00") {
		repos := e.draft.Repos
		edit.Repos = &repos
	}
	if strings.Join(e.draft.Materials, "\x00") != strings.Join(e.original.Materials, "\x00") {
		materials := e.draft.Materials
		edit.Materials = &materials
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
		name := label.Render(fieldNames[f])
		if f == e.field {
			marker = cursorSt.Render("▸ ")
			name = activeL.Render(fieldNames[f])
		}
		var content string
		switch {
		case e.mode == editText && f == e.field:
			content = e.text.View()
		case e.mode == editCategory && f == e.field:
			content = e.picker.view("               ")
		case f == fieldPriority || f == fieldState:
			content = "◂ " + e.draft.get(f) + " ▸"
		case f == fieldCategory:
			content = e.draft.Category
			if content == "" {
				content = topLevel
			}
		case (e.mode == editList || e.mode == editListText) && f == e.field:
			content = e.viewList()
		case f == fieldRepos || f == fieldMaterials:
			list := e.draft.list(f)
			if len(list) == 0 {
				content = dim.Render("none")
			} else {
				content = fmt.Sprintf("%d linked", len(list))
			}
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
		b.WriteString("  " + dim.Render("↑↓ choose · a add a folder · d remove · Esc done") + "\n")
	case editListText:
		b.WriteString("  " + dim.Render("type a folder path · Enter add · Esc cancel") + "\n")
	case editText:
		b.WriteString("  " + dim.Render("Enter keep · Esc cancel") + "\n")
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

// viewList renders the list editor for repos or materials.
func (e editor) viewList() string {
	list := e.draft.list(e.field)
	var b strings.Builder
	if len(list) == 0 && e.mode != editListText {
		b.WriteString(dim.Render("none yet; press a to add a folder") + "\n")
	}
	for i, item := range list {
		marker := "  "
		text := home.Display(item)
		if i == e.listPos && e.mode == editList {
			marker = cursorSt.Render("▸ ")
			text = cursorSt.Render(text)
		}
		b.WriteString(marker + text + "\n")
	}
	if e.mode == editListText {
		b.WriteString("  " + e.text.View() + "\n")
	}
	return "\n               " + strings.ReplaceAll(strings.TrimRight(b.String(), "\n"), "\n", "\n               ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
