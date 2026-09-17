package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// clusterScreen shows a knowledge base's members as boxes and acts on them: a adds one
// from a picker of the knowledge bases that are not members, x drops the one under the
// cursor. Every action is one backend call on the cluster's own identity file; the view
// refreshes afterwards so the projects that mount the cluster catch up. It is embedded
// in the view.

type clusterMode int

const (
	clusterList clusterMode = iota
	clusterPick             // choosing a knowledge base to add
	clusterAsk              // y or n before a drop
)

type clusterScreen struct {
	hooks   Hooks
	entry   registry.Entry
	items   []Item
	rows    []registry.Ref
	cursor  int
	mode    clusterMode
	picks   []registry.Entry // clusterPick: what the user may choose from
	pick    int              // the candidate under the cursor
	confirm string           // clusterAsk: the member y would drop
	status  string
	err     string
	changed bool
	closed  bool
	width   int
}

func newCluster(hooks Hooks, e registry.Entry, items []Item, width int) clusterScreen {
	s := clusterScreen{hooks: hooks, entry: e, items: items, width: width}
	s.build()
	return s
}

// reload takes the entry and the other vaults again from the view's items.
func (s *clusterScreen) reload(items []Item) {
	s.items = items
	for i := range items {
		if items[i].Entry.Path == s.entry.Path {
			s.entry = items[i].Entry
		}
	}
	s.build()
}

// build turns the members into rows.
func (s *clusterScreen) build() {
	s.rows = append([]registry.Ref(nil), s.entry.Members...)
	if s.cursor >= len(s.rows) {
		s.cursor = max(0, len(s.rows)-1)
	}
}

func (s clusterScreen) current() *registry.Ref {
	if len(s.rows) == 0 {
		return nil
	}
	return &s.rows[s.cursor]
}

func (s clusterScreen) update(msg tea.Msg) (clusterScreen, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	switch s.mode {
	case clusterList:
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
				return s.openPicker(), nil
			case "x":
				return s.askDrop(), nil
			case "q":
				s.closed = true
			}
		}
		return s, nil
	case clusterPick:
		if !isKey {
			return s, nil
		}
		switch key.Type {
		case tea.KeyUp:
			if s.pick > 0 {
				s.pick--
			}
		case tea.KeyDown:
			if s.pick < len(s.picks)-1 {
				s.pick++
			}
		case tea.KeyEnter:
			return s.add(), nil
		case tea.KeyEsc:
			s.err = ""
			s.mode = clusterList
		default:
			if key.String() == "q" {
				s.err = ""
				s.mode = clusterList
			}
		}
		return s, nil
	case clusterAsk:
		if isKey {
			switch strings.ToLower(key.String()) {
			case "y":
				return s.drop(), nil
			case "n", "esc":
				s.mode = clusterList
			}
		}
		return s, nil
	}
	return s, nil
}

// candidates are the knowledge bases this cluster may gather: not itself, not a member
// already, and not a cluster, because a cluster does not nest yet.
func (s clusterScreen) candidates() []registry.Entry {
	member := map[string]bool{}
	for _, m := range s.entry.Members {
		member[m.ID] = true
	}
	var out []registry.Entry
	for i := range s.items {
		e := s.items[i].Entry
		if e.Error != "" || e.Kind != vault.Knowledge || e.ID == s.entry.ID || member[e.ID] || vaults.IsCluster(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// openPicker lists what the user may add, so a name is chosen and never typed.
func (s clusterScreen) openPicker() clusterScreen {
	s.picks, s.pick = s.candidates(), 0
	if len(s.picks) == 0 {
		s.err = "no knowledge base left to add; press N in the view to make one"
		return s
	}
	s.err = ""
	s.mode = clusterPick
	return s
}

// add records the knowledge base under the cursor as a member.
func (s clusterScreen) add() clusterScreen {
	s.mode = clusterList
	if s.pick < 0 || s.pick >= len(s.picks) {
		return s
	}
	if s.hooks.AddMember == nil {
		s.err = "adding a member is not available here"
		return s
	}
	kb := s.picks[s.pick]
	if err := s.hooks.AddMember(s.entry, kb); err != nil {
		s.err = err.Error()
		return s
	}
	s.entry.Members = append(s.entry.Members, registry.Ref{ID: kb.ID, Name: kb.Name})
	s.err = ""
	s.status = fmt.Sprintf("added %s to %s", kb.Name, s.entry.Name)
	s.changed = true
	s.build()
	return s
}

// askDrop asks before a member leaves the cluster.
func (s clusterScreen) askDrop() clusterScreen {
	row := s.current()
	if row == nil {
		return s
	}
	s.confirm = memberName(*row)
	s.mode = clusterAsk
	return s
}

// drop takes the member under the cursor out of the cluster. The knowledge base stays,
// and every project that mounts the cluster loses the mount at its next refresh.
func (s clusterScreen) drop() clusterScreen {
	row := s.current()
	s.mode = clusterList
	if row == nil {
		s.status = listChanged
		return s
	}
	if s.hooks.RemoveMember == nil {
		s.err = "dropping a member is not available here"
		return s
	}
	id, name := row.ID, memberName(*row)
	if err := s.hooks.RemoveMember(s.entry, id); err != nil {
		s.err = err.Error()
		return s
	}
	var keep []registry.Ref
	for _, m := range s.entry.Members {
		if m.ID != id {
			keep = append(keep, m)
		}
	}
	s.entry.Members = keep
	s.err = ""
	s.status = fmt.Sprintf("dropped %s from %s; the knowledge base stays", name, s.entry.Name)
	s.changed = true
	s.build()
	return s
}

// memberName is what a member is called: its name, or its id when the scan lost it and
// the identity file recorded no name.
func memberName(m registry.Ref) string {
	if m.Name == "" {
		return m.ID
	}
	return m.Name
}

// memberScope is what a member covers, as the scan knows it now.
func (s clusterScreen) memberScope(m registry.Ref) string {
	for i := range s.items {
		if e := s.items[i].Entry; e.Error == "" && e.ID == m.ID {
			if e.Scope == "" {
				return "no scope yet; press e on it to write one"
			}
			return e.Scope
		}
	}
	return ""
}

func (s clusterScreen) view() string {
	var b strings.Builder
	head := fmt.Sprintf("\n  %s   %s", title.Render(s.entry.Name), catSt.Render("members"))
	if s.entry.Scope != "" {
		head += "   " + dim.Render(clip(s.entry.Scope, max(20, s.width-len([]rune(s.entry.Name))-16)))
	}
	b.WriteString(head + "\n\n")
	if len(s.rows) == 0 {
		b.WriteString("  " + dim.Render("no members yet: a cluster is a knowledge base that gathers others, and a project that mounts it reaches them all. Press a to add one.") + "\n")
	}
	width := min(72, max(32, s.width-6))
	for i, row := range s.rows {
		selected := i == s.cursor && s.mode != clusterPick
		style := boxStyle(vault.Knowledge, selected).Width(width)
		lines := []string{knowledgeSt.Render(memberName(row)), dim.Render(s.memberScope(row))}
		if row.Error != "" {
			lines[1] = errSt.Render(row.Error)
		}
		rendered := strings.Split(indent(style.Render(strings.Join(lines, "\n")), "  "), "\n")
		b.WriteString(strings.Join(focus(rendered, selected), "\n") + "\n")
	}
	b.WriteString("\n")
	switch s.mode {
	case clusterPick:
		b.WriteString("  " + title.Render("Choose one") + "  " + dim.Render("the knowledge base this cluster gathers") + "\n")
		for i, e := range s.picks {
			line := "    " + e.Name
			if i == s.pick {
				line = "  ▸ " + knowledgeSt.Render(e.Name)
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("  " + dim.Render("↑↓ move · Enter add · Esc cancel") + "\n")
	case clusterAsk:
		fmt.Fprintf(&b, "  %s Drop %s from %s? The knowledge base stays.  %s\n",
			errSt.Render("▲"), s.confirm, s.entry.Name, title.Render("y")+" / "+title.Render("n"))
	default:
		hint := "a add · x drop"
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
