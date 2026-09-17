package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

type editMode int

const (
	editFields editMode = iota
	editText
	confirmRemove
)

// editOutcome says how an editor session ended.
type editOutcome int

const (
	editOpen editOutcome = iota
	editCancelled
	editSaved
	editRemoved
	// editMembers asks the view to open this knowledge base's members screen.
	editMembers
)

// The fields an editor shows: a project has a name and tags, a knowledge base a name, a
// scope, and an access level.
const (
	fieldName = iota
	fieldTags
	fieldScope
	fieldAccess
	fieldMembers
)

var fieldNames = []string{fieldName: "Name", fieldTags: "Tags", fieldScope: "Scope", fieldAccess: "Access", fieldMembers: "Members"}

// labelWidth fits the longest field name.
const labelWidth = 8

type draft struct{ Name, Tags, Scope, Access string }

func (d draft) get(field int) string {
	// Members are the knowledge base's own list, not a draft field: Enter on that row
	// opens the members screen, so there is nothing here to compare or edit.
	return [...]string{fieldName: d.Name, fieldTags: d.Tags, fieldScope: d.Scope, fieldAccess: d.Access, fieldMembers: ""}[field]
}

func (d *draft) set(field int, v string) {
	switch field {
	case fieldName:
		d.Name = v
	case fieldTags:
		d.Tags = v
	case fieldScope:
		d.Scope = v
	case fieldAccess:
		d.Access = v
	}
}

// editor edits one vault's identity file: its name, and its tags or its scope and
// access. It also forgets a vault the config names. It is embedded in the view.
type editor struct {
	hooks    actions.Atlas
	entry    registry.Entry
	fields   []int
	at       int
	original draft
	draft    draft
	text     textinput.Model
	mode     editMode
	err      string
	discard  bool
	outcome  editOutcome
	// path is where the vault sits after a saved edit: its own folder, or the one a
	// rename moved it to.
	path string
}

func draftOf(e registry.Entry) draft {
	d := draft{Name: e.Name, Tags: strings.Join(e.Tags, ", "), Scope: e.Scope, Access: e.Access}
	if e.Kind == vault.Knowledge && d.Access == "" {
		d.Access = vault.AccessOpen
	}
	return d
}

func newEditor(hooks actions.Atlas, e registry.Entry) editor {
	text := textinput.New()
	text.Prompt = ""
	text.CharLimit = 300
	text.Width = 60
	fields := []int{fieldName, fieldTags}
	if e.Kind == vault.Knowledge {
		fields = []int{fieldName, fieldScope, fieldAccess, fieldMembers}
	}
	return editor{hooks: hooks, entry: e, fields: fields, original: draftOf(e), draft: draftOf(e), text: text}
}

// field is the field under the cursor.
func (e editor) field() int { return e.fields[e.at] }

func (e editor) dirty() bool {
	for _, f := range e.fields {
		if e.draft.get(f) != e.original.get(f) {
			return true
		}
	}
	return false
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
				e.draft.set(e.field(), strings.TrimSpace(e.text.Value()))
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
	case confirmRemove:
		if isKey {
			switch strings.ToLower(key.String()) {
			case "y":
				if err := e.hooks.Unregister(e.entry); err != nil {
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
		e.at = (e.at + len(e.fields) - 1) % len(e.fields)
	case tea.KeyDown:
		e.at = (e.at + 1) % len(e.fields)
	case tea.KeyLeft, tea.KeyRight:
		if e.field() == fieldAccess {
			e.cycleAccess()
		}
	case tea.KeyEnter:
		if e.field() == fieldMembers {
			if e.dirty() {
				e.err = "save or discard first: members are the knowledge base's own list"
				return e, nil
			}
			e.outcome = editMembers
			return e, nil
		}
		if e.field() == fieldAccess {
			e.cycleAccess()
			return e, nil
		}
		e.text.SetValue(e.draft.get(e.field()))
		e.text.CursorEnd()
		e.mode = editText
		return e, e.text.Focus()
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
			if e.hooks.Unregister == nil {
				e.err = "forgetting vaults is not available here"
				return e, nil
			}
			e.mode = confirmRemove
		}
	}
	return e, nil
}

func (e *editor) cycleAccess() {
	if e.draft.Access == vault.AccessOpen {
		e.draft.Access = vault.AccessGuarded
		return
	}
	e.draft.Access = vault.AccessOpen
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
	edit := vaults.Edit{}
	if e.draft.Name != e.original.Name {
		edit.Name = strings.TrimSpace(e.draft.Name)
	}
	if e.draft.Tags != e.original.Tags {
		tags := splitTags(e.draft.Tags)
		edit.Tags = &tags
	}
	if e.draft.Scope != e.original.Scope {
		scope := strings.TrimSpace(e.draft.Scope)
		edit.Scope = &scope
	}
	if e.draft.Access != e.original.Access {
		access := e.draft.Access
		edit.Access = &access
	}
	path, err := e.hooks.Edit(e.entry, edit)
	if err != nil {
		e.err = err.Error()
		e.mode = editFields
		return e
	}
	e.path = path
	e.outcome = editSaved
	return e
}

func (e editor) view() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s\n\n", title.Render(e.entry.Name), dim.Render(e.entry.Rel()))
	for i, f := range e.fields {
		marker := "  "
		name := label.Width(labelWidth).Render(fieldNames[f])
		if i == e.at {
			marker = cursorSt.Render("▸ ")
			name = activeL.Width(labelWidth).Render(fieldNames[f])
		}
		var content string
		switch {
		case e.mode == editText && i == e.at:
			content = e.text.View()
		case f == fieldAccess:
			content = "◂ " + e.draft.Access + " ▸" + dim.Render("  "+accessHint(e.draft.Access))
		case f == fieldMembers:
			content = memberSummary(e.entry) + dim.Render("  Enter to add or drop")
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
		fmt.Fprintf(&b, "  %s Forget %s? The vault at %s stays on disk.  %s\n",
			errSt.Render("▲"), e.entry.Name, home.Display(e.entry.Path), title.Render("y")+" / "+title.Render("n"))
	case editText:
		b.WriteString("  " + dim.Render("Enter keep · Esc cancel") + "\n")
	default:
		hints := "↑↓ field · Enter edit"
		if e.entry.Kind == vault.Knowledge {
			hints += " · ←→ access · Enter on Members opens them"
		}
		if e.dirty() {
			hints += " · " + title.Render("s") + " save"
		}
		hints += " · r forget · Esc back"
		b.WriteString("  " + dim.Render(hints) + "\n")
		if e.entry.Kind == vault.Project {
			b.WriteString("  " + dim.Render("tags are comma-separated; the first one is the folder in the view") + "\n")
		}
	}
	if e.err != "" {
		b.WriteString("  " + errSt.Render(e.err) + "\n")
	}
	return b.String()
}

// accessHint says what an access level means for a knowledge base.
// memberSummary is what a knowledge base gathers, for the editor's Members row.
func memberSummary(e registry.Entry) string {
	if len(e.Members) == 0 {
		return dim.Render("none; this is an ordinary knowledge base")
	}
	names := make([]string, len(e.Members))
	for i, m := range e.Members {
		names[i] = m.Name
		if m.Error != "" {
			names[i] = m.Name + " (" + m.Error + ")"
		}
	}
	return strings.Join(names, ", ")
}

func accessHint(access string) string {
	if access == vault.AccessGuarded {
		return "only the projects it grants may write"
	}
	return "every project that mounts it may write"
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
