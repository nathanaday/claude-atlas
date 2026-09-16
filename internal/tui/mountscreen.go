package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// mountsScreen shows what a project mounts as boxes, one per mount, and acts on them:
// mount a knowledge base, unmount one. Every action is one backend call; the view
// refreshes afterwards so the facts catch up. It is embedded in the view.

type mountsMode int

const (
	mountsList       mountsMode = iota
	mountsPickKB                // typing a knowledge base's name
	mountsPickAccess            // w or r for the new mount (project) or the new grant (knowledge base)
	mountsConfirm               // y or n before an unmount or a revoke
)

// mountRow is one line of the screen: on a project, a mount; on a knowledge base, a
// project that mounts it or holds a grant (or both).
type mountRow struct {
	mount *registry.Mount // project screen
	ref   *registry.Ref   // knowledge base screen: a project that mounts it
	grant *registry.Grant // knowledge base screen: its grant, when it has one
	name  string          // what the row is called: the mount name, or the project's name or id
	state string          // project screen: the symlink state or the mount's error
}

type mountsScreen struct {
	hooks   Hooks
	entry   registry.Entry
	items   []Item
	rows    []mountRow
	cursor  int
	mode    mountsMode
	name    textinput.Model // the knowledge base (project screen) or project (knowledge base screen) to add
	picked  *registry.Entry // the vault the typed name resolved to
	status  string
	err     string
	changed bool
	closed  bool
	width   int
}

func newMounts(hooks Hooks, e registry.Entry, items []Item, width int) mountsScreen {
	s := mountsScreen{hooks: hooks, entry: e, items: items, width: width}
	s.name = textinput.New()
	s.name.Prompt = ""
	s.name.Placeholder = "ai-ml"
	s.name.CharLimit = 80
	s.name.Width = 40
	s.build()
	return s
}

// reload takes the entry and the other vaults again from the view's items.
func (s *mountsScreen) reload(items []Item) {
	s.items = items
	for i := range items {
		if items[i].Entry.Path == s.entry.Path {
			s.entry = items[i].Entry
		}
	}
	s.build()
}

// build turns the entry's mounts into boxes.
func (s *mountsScreen) build() {
	s.rows = nil
	for _, m := range s.entry.Mounts {
		s.rows = append(s.rows, mountRow{mount: &m, name: m.Name, state: mountState(s.entry, m)})
	}
	if s.cursor >= len(s.rows) {
		s.cursor = max(0, len(s.rows)-1)
	}
}

// mountState says what a mount is: what the scan could not resolve, or what the symlink is.
func mountState(project registry.Entry, m registry.Mount) string {
	if m.Error != "" {
		return m.Error
	}
	switch vaults.MountState(project, m) {
	case vaults.MountOK:
		return "ok"
	case vaults.MountWrong:
		return "symlink points elsewhere"
	default:
		return "symlink missing"
	}
}

// accessText is what the project asked for and what it gets; empty while unresolved.
func accessText(m registry.Mount) string {
	if m.Effective == "" {
		return ""
	}
	return m.Access + " → " + m.Effective
}

// kbName is the knowledge base a mount names, as the atlas knows it now.
func (s mountsScreen) kbName(m registry.Mount) string {
	for i := range s.items {
		if s.items[i].Entry.ID == m.ID {
			return s.items[i].Entry.Name
		}
	}
	return m.Name
}

func (s mountsScreen) current() *mountRow {
	if len(s.rows) == 0 {
		return nil
	}
	return &s.rows[s.cursor]
}

func (s mountsScreen) update(msg tea.Msg) (mountsScreen, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	switch s.mode {
	case mountsList:
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
		default:
			switch key.String() {
			case "a":
				s.name.SetValue("")
				s.picked = nil
				s.mode = mountsPickKB
				return s, s.name.Focus()
			case "u":
				if s.current() != nil {
					s.mode = mountsConfirm
				}
			case "q":
				s.closed = true
			}
		}
		return s, nil
	case mountsPickKB:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				return s.pick(), nil
			case tea.KeyEsc:
				s.err = ""
				s.mode = mountsList
				return s, nil
			}
		}
		var cmd tea.Cmd
		s.name, cmd = s.name.Update(msg)
		return s, cmd
	case mountsPickAccess:
		if isKey {
			switch strings.ToLower(key.String()) {
			case "w", "enter":
				return s.mount(vault.AccessWrite), nil
			case "r":
				return s.mount(vault.AccessRead), nil
			case "esc":
				s.err = ""
				s.mode = mountsList
			}
		}
		return s, nil
	case mountsConfirm:
		if isKey {
			switch strings.ToLower(key.String()) {
			case "y":
				return s.unmount(), nil
			case "n", "esc":
				s.mode = mountsList
			}
		}
		return s, nil
	}
	return s, nil
}

// pick resolves the typed name to a knowledge base the atlas knows.
func (s mountsScreen) pick() mountsScreen {
	name := strings.TrimSpace(s.name.Value())
	if name == "" {
		s.mode = mountsList
		return s
	}
	for i := range s.items {
		e := s.items[i].Entry
		if e.Kind == vault.Knowledge && strings.EqualFold(e.Name, name) {
			s.picked = &e
			s.err = ""
			s.mode = mountsPickAccess
			return s
		}
	}
	s.err = "no knowledge base named " + name
	return s
}

// mount records the picked knowledge base with that access and makes the symlink.
func (s mountsScreen) mount(access string) mountsScreen {
	s.mode = mountsList
	if s.picked == nil {
		return s
	}
	kb := *s.picked
	m, err := s.hooks.Mount(s.entry, kb, access, "")
	if err != nil {
		s.err = err.Error()
		// A named mount came back with the error: the identity file holds it and only the
		// symlink failed, so the view should show the row.
		s.changed = m.Name != ""
		return s
	}
	s.err = ""
	s.status = fmt.Sprintf("mounted %s as kb/%s", kb.Name, m.Name)
	s.entry.Mounts = append(s.entry.Mounts, registry.Mount{ID: m.ID, Name: m.Name, Access: m.Access, Path: kb.Wiki()})
	s.changed = true
	s.build()
	return s
}

// unmount drops the mount under the cursor. The knowledge base is untouched.
func (s mountsScreen) unmount() mountsScreen {
	row := s.current()
	s.mode = mountsList
	if row == nil {
		s.status = listChanged
		return s
	}
	if s.hooks.Unmount == nil {
		s.err = "unmounting is not available here"
		return s
	}
	id, name := row.mount.ID, row.name
	if err := s.hooks.Unmount(s.entry, id); err != nil {
		s.err = err.Error()
		return s
	}
	var keep []registry.Mount
	for _, m := range s.entry.Mounts {
		if m.ID != id {
			keep = append(keep, m)
		}
	}
	s.entry.Mounts = keep
	s.err = ""
	s.status = "unmounted " + name + "; the knowledge base stays"
	s.changed = true
	s.build()
	return s
}

func (s mountsScreen) view() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", title.Render(s.entry.Name), catSt.Render("mounts"), dim.Render(home.Display(s.entry.Path)))
	if len(s.rows) == 0 {
		b.WriteString("  " + dim.Render("no mounts yet: a mount is a knowledge base this project reads or writes through kb/. Press a to mount one.") + "\n")
	}
	width := min(72, max(32, s.width-6))
	for i, row := range s.rows {
		style := boxSt
		name := row.name
		if i == s.cursor && s.mode != mountsPickKB {
			style = boxSelSt
			name = selSt.Render(row.name)
		}
		badge := accessText(*row.mount)
		pad := max(1, width-2-lipgloss.Width(row.name)-lipgloss.Width(badge))
		lines := []string{name + strings.Repeat(" ", pad) + dim.Render(badge)}
		where := s.kbName(*row.mount)
		if row.mount.Path != "" {
			where += "  " + home.Display(filepath.Dir(row.mount.Path))
		}
		state := dim.Render(row.state)
		if row.state != "ok" {
			state = errSt.Render(row.state)
		}
		lines = append(lines, dim.Render(where), state)
		b.WriteString(indent(style.Width(width).Render(strings.Join(lines, "\n")), "  ") + "\n")
	}
	b.WriteString("\n")
	switch s.mode {
	case mountsPickKB:
		b.WriteString("  " + activeL.Width(21).Render("Knowledge base name") + s.name.View() + "\n")
		b.WriteString("  " + dim.Render("the knowledge base this project should reach through kb/ · Enter next · Esc cancel") + "\n")
	case mountsPickAccess:
		name := ""
		if s.picked != nil {
			name = s.picked.Name
		}
		fmt.Fprintf(&b, "  %s Mount %s on %s.\n", title.Render("?"), name, s.entry.Name)
		b.WriteString("  " + dim.Render("Access: "+title.Render("w")+" write (default), "+title.Render("r")+" read · Esc cancel") + "\n")
	case mountsConfirm:
		row := s.current()
		if row == nil {
			b.WriteString("  " + dim.Render(listChanged) + "\n")
			break
		}
		fmt.Fprintf(&b, "  %s Unmount %s? The knowledge base stays.  %s\n",
			errSt.Render("▲"), row.name, title.Render("y")+" / "+title.Render("n"))
	default:
		b.WriteString("  " + dim.Render("a mount · u unmount · esc back") + "\n")
	}
	if s.status != "" {
		b.WriteString("  " + okSt.Render(s.status) + "\n")
	}
	if s.err != "" {
		b.WriteString("  " + errSt.Render(s.err) + "\n")
	}
	return b.String()
}
