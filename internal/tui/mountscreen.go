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

// mountsScreen shows one side of the same relation as boxes, and acts on it. On a
// project: what it mounts, with a to mount and u to unmount. On a knowledge base: the
// projects that mount it and the grants it holds, with w and r to grant, x to revoke,
// and a to grant a project that does not mount it yet. Every action is one backend
// call; the view refreshes afterwards so the facts catch up. It is embedded in the view.

type mountsMode int

const (
	mountsList       mountsMode = iota
	mountsPickName              // typing a knowledge base's name (project) or a project's name (knowledge base)
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
	confirm string          // mountsConfirm: what y would run, "unmount" or "revoke"
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
	if e.Kind == vault.Knowledge {
		s.name.Placeholder = "cs566"
	}
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

// build turns the entry's mounts, or the projects on the other side of them, into boxes.
func (s *mountsScreen) build() {
	s.rows = nil
	if s.entry.Kind == vault.Knowledge {
		s.buildProjects()
	} else {
		for _, m := range s.entry.Mounts {
			mm := m
			s.rows = append(s.rows, mountRow{mount: &mm, name: mm.Name, state: mountState(s.entry, mm)})
		}
	}
	if s.cursor >= len(s.rows) {
		s.cursor = max(0, len(s.rows)-1)
	}
}

// buildProjects makes one row per project on the other side of the knowledge base: the
// projects that mount it, in the scan's order, then the grants the rest hold.
func (s *mountsScreen) buildProjects() {
	mounts := map[string]bool{}
	for _, ref := range s.entry.MountedBy {
		r := ref
		mounts[r.ID] = true
		s.rows = append(s.rows, mountRow{ref: &r, grant: s.grantFor(r.ID), name: r.Name})
	}
	for _, grant := range s.entry.Grants {
		if mounts[grant.ID] {
			continue
		}
		g := grant
		name := g.Name
		if g.Error != "" || name == "" {
			name = g.ID
		}
		s.rows = append(s.rows, mountRow{grant: &g, name: name})
	}
}

// grantFor is the knowledge base's grant for one project, or nil when it holds none.
func (s mountsScreen) grantFor(id string) *registry.Grant {
	for i := range s.entry.Grants {
		if s.entry.Grants[i].ID == id {
			g := s.entry.Grants[i]
			return &g
		}
	}
	return nil
}

// rowID is the project a knowledge base's row is about.
func rowID(row mountRow) string {
	switch {
	case row.ref != nil:
		return row.ref.ID
	case row.grant != nil:
		return row.grant.ID
	}
	return ""
}

// grantText says what one project has: the mount, the grant, and what the scan could
// not resolve.
func grantText(row mountRow) string {
	parts := []string{"does not mount it"}
	if row.ref != nil {
		parts[0] = "mounts it"
		if row.ref.Access != "" {
			parts[0] += " (effective " + row.ref.Access + ")"
		}
	}
	if row.grant == nil {
		return strings.Join(append(parts, "no grant"), " · ")
	}
	parts = append(parts, "grant: "+row.grant.Access)
	if row.grant.Error != "" {
		parts = append(parts, row.grant.Error)
	}
	return strings.Join(parts, " · ")
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
				s.mode = mountsPickName
				return s, s.name.Focus()
			case "u":
				if s.entry.Kind == vault.Project && s.current() != nil {
					s.confirm = "unmount"
					s.mode = mountsConfirm
				}
			case "w":
				if s.entry.Kind == vault.Knowledge {
					return s.grantRow(vault.AccessWrite), nil
				}
			case "r":
				if s.entry.Kind == vault.Knowledge {
					return s.grantRow(vault.AccessRead), nil
				}
			case "x":
				if s.entry.Kind == vault.Knowledge {
					return s.askRevoke(), nil
				}
			case "q":
				s.closed = true
			}
		}
		return s, nil
	case mountsPickName:
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
				return s.addPicked(vault.AccessWrite), nil
			case "r":
				return s.addPicked(vault.AccessRead), nil
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
				if s.confirm == "revoke" {
					return s.revoke(), nil
				}
				return s.unmount(), nil
			case "n", "esc":
				s.mode = mountsList
			}
		}
		return s, nil
	}
	return s, nil
}

// pick resolves the typed name to a vault the atlas knows: a knowledge base to mount on
// a project, a project to grant access to a knowledge base.
func (s mountsScreen) pick() mountsScreen {
	name := strings.TrimSpace(s.name.Value())
	if name == "" {
		s.mode = mountsList
		return s
	}
	want, missing := vault.Knowledge, "no knowledge base named "
	if s.entry.Kind == vault.Knowledge {
		want, missing = vault.Project, "no project named "
	}
	for i := range s.items {
		e := s.items[i].Entry
		if e.Kind == want && strings.EqualFold(e.Name, name) {
			s.picked = &e
			s.err = ""
			s.mode = mountsPickAccess
			return s
		}
	}
	s.err = missing + name
	return s
}

// addPicked mounts or grants with that access, whichever side the screen shows.
func (s mountsScreen) addPicked(access string) mountsScreen {
	if s.entry.Kind == vault.Knowledge {
		s.mode = mountsList
		if s.picked == nil {
			return s
		}
		return s.grant(*s.picked, access)
	}
	return s.mount(access)
}

// grantRow grants the project under the cursor. A row the scan could not resolve names
// no project, so only revoking is left.
func (s mountsScreen) grantRow(access string) mountsScreen {
	row := s.current()
	if row == nil {
		s.err = "no project here yet; press a to grant one by name"
		return s
	}
	id := rowID(*row)
	for i := range s.items {
		e := s.items[i].Entry
		if e.ID == id && e.Kind == vault.Project {
			return s.grant(e, access)
		}
	}
	s.err = "no project with id " + id + "; revoke it with x"
	return s
}

// grant records what one project may do in this knowledge base. An open knowledge base
// keeps the grant for the day it is guarded.
func (s mountsScreen) grant(project registry.Entry, access string) mountsScreen {
	if s.hooks.Grant == nil {
		s.err = "granting is not available here"
		return s
	}
	if err := s.hooks.Grant(s.entry, project, access); err != nil {
		s.err = err.Error()
		return s
	}
	s.err = ""
	s.status = fmt.Sprintf("granted %s %s", project.Name, access)
	if s.entry.Access == vault.AccessOpen {
		s.status += fmt.Sprintf("; %s is open, so the grant applies when it is guarded", s.entry.Name)
	}
	s.entry.Grants = withGrant(s.entry.Grants, registry.Grant{ID: project.ID, Name: project.Name, Access: access})
	s.changed = true
	s.build()
	return s
}

// withGrant is the grants with g in place of the project's old grant, or added.
func withGrant(grants []registry.Grant, g registry.Grant) []registry.Grant {
	out := make([]registry.Grant, 0, len(grants)+1)
	replaced := false
	for _, have := range grants {
		if have.ID == g.ID {
			out, replaced = append(out, g), true
			continue
		}
		out = append(out, have)
	}
	if !replaced {
		out = append(out, g)
	}
	return out
}

// askRevoke asks before a revoke; a row with nothing to revoke says so.
func (s mountsScreen) askRevoke() mountsScreen {
	row := s.current()
	if row == nil {
		return s
	}
	if row.grant == nil {
		s.err = row.name + " has no grant"
		return s
	}
	s.confirm = "revoke"
	s.mode = mountsConfirm
	return s
}

// revoke drops the grant under the cursor. A project that mounts the knowledge base
// keeps its mount and falls back to what a guarded knowledge base grants everyone.
func (s mountsScreen) revoke() mountsScreen {
	row := s.current()
	s.mode = mountsList
	if row == nil {
		s.status = listChanged
		return s
	}
	if s.hooks.Revoke == nil {
		s.err = "revoking is not available here"
		return s
	}
	id, name := rowID(*row), row.name
	if err := s.hooks.Revoke(s.entry, id); err != nil {
		s.err = err.Error()
		return s
	}
	var keep []registry.Grant
	for _, g := range s.entry.Grants {
		if g.ID != id {
			keep = append(keep, g)
		}
	}
	s.entry.Grants = keep
	s.err = ""
	s.status = "revoked " + name + "'s grant"
	s.changed = true
	s.build()
	return s
}

// mount records the picked knowledge base with that access and makes the symlink.
func (s mountsScreen) mount(access string) mountsScreen {
	s.mode = mountsList
	if s.picked == nil {
		return s
	}
	if s.hooks.Mount == nil {
		s.err = "mounting is not available here"
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
	kb := s.entry.Kind == vault.Knowledge
	var b strings.Builder
	if kb {
		fmt.Fprintf(&b, "\n  %s   %s   %s   %s\n\n", title.Render(s.entry.Name), catSt.Render("mounts"),
			dim.Render(s.access()), dim.Render(home.Display(s.entry.Path)))
	} else {
		fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", title.Render(s.entry.Name), catSt.Render("mounts"), dim.Render(home.Display(s.entry.Path)))
	}
	if len(s.rows) == 0 {
		empty := "no mounts yet: a mount is a knowledge base this project reads or writes through kb/. Press a to mount one."
		if kb {
			empty = "nothing mounts this knowledge base yet, and it grants nothing. Press a to grant a project access."
		}
		b.WriteString("  " + dim.Render(empty) + "\n")
	}
	width := min(72, max(32, s.width-6))
	other := vault.Knowledge // the kind on the other side of every row
	if kb {
		other = vault.Project
	}
	for i, row := range s.rows {
		style := boxStyle(other, i == s.cursor && s.mode != mountsPickName)
		name := kindStyle(other).Render(row.name)
		var lines []string
		if kb {
			lines = projectLines(row, name)
		} else {
			lines = s.mountLines(row, name, width)
		}
		b.WriteString(indent(style.Width(width).Render(strings.Join(lines, "\n")), "  ") + "\n")
	}
	b.WriteString("\n")
	switch s.mode {
	case mountsPickName:
		if kb {
			b.WriteString("  " + activeL.Width(21).Render("Project name") + s.name.View() + "\n")
			b.WriteString("  " + dim.Render("the project that may reach this knowledge base · Enter next · Esc cancel") + "\n")
			break
		}
		b.WriteString("  " + activeL.Width(21).Render("Knowledge base name") + s.name.View() + "\n")
		b.WriteString("  " + dim.Render("the knowledge base this project should reach through kb/ · Enter next · Esc cancel") + "\n")
	case mountsPickAccess:
		name := ""
		if s.picked != nil {
			name = s.picked.Name
		}
		if kb {
			fmt.Fprintf(&b, "  %s Grant %s access to %s.\n", title.Render("?"), name, s.entry.Name)
		} else {
			fmt.Fprintf(&b, "  %s Mount %s on %s.\n", title.Render("?"), name, s.entry.Name)
		}
		b.WriteString("  " + dim.Render("Access: "+title.Render("w")+" write (default), "+title.Render("r")+" read · Esc cancel") + "\n")
	case mountsConfirm:
		row := s.current()
		if row == nil {
			b.WriteString("  " + dim.Render(listChanged) + "\n")
			break
		}
		yn := title.Render("y") + " / " + title.Render("n")
		if kb {
			fmt.Fprintf(&b, "  %s Revoke %s's grant?  %s\n", errSt.Render("▲"), row.name, yn)
			break
		}
		fmt.Fprintf(&b, "  %s Unmount %s? The knowledge base stays.  %s\n", errSt.Render("▲"), row.name, yn)
	default:
		hint := "a mount · u unmount"
		if kb {
			hint = "w grant write · r grant read · x revoke · a grant by name"
		}
		if len(s.rows) > 0 {
			hint = "↑↓ move · " + hint
		}
		b.WriteString("  " + dim.Render(hint+" · esc back") + "\n")
	}
	if s.status != "" {
		b.WriteString("  " + okSt.Render(s.status) + "\n")
	}
	if s.err != "" {
		b.WriteString("  " + errSt.Render(s.err) + "\n")
	}
	return b.String()
}

// access is what the knowledge base grants by default; an identity file that names none
// is open, as the editor shows it.
func (s mountsScreen) access() string {
	if s.entry.Access == "" {
		return vault.AccessOpen
	}
	return s.entry.Access
}

// mountLines is one box on a project: the mount, where it points, and its symlink.
func (s mountsScreen) mountLines(row mountRow, name string, width int) []string {
	badge := accessText(*row.mount)
	pad := max(1, width-2-lipgloss.Width(row.name)-lipgloss.Width(badge))
	where := s.kbName(*row.mount)
	if row.mount.Path != "" {
		where += "  " + home.Display(filepath.Dir(row.mount.Path))
	}
	state := dim.Render(row.state)
	if row.state != "ok" {
		state = errSt.Render(row.state)
	}
	return []string{name + strings.Repeat(" ", pad) + dim.Render(badge), dim.Render(where), state}
}

// projectLines is one box on a knowledge base: the project, and what it has.
func projectLines(row mountRow, name string) []string {
	text := grantText(row)
	if row.grant != nil && row.grant.Error != "" {
		return []string{name, errSt.Render(text)}
	}
	return []string{name, dim.Render(text)}
}
