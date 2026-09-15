package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// tasksScreen shows open tasks as boxes: one project's, or every project's as a board.
// `p` plants a task, `c` continues one in Claude Code, `o` opens its page in Obsidian.
// It is embedded in the view.

type tasksMode int

const (
	tasksList tasksMode = iota
	tasksPlant
)

type taskRow struct {
	project *Item
	rec     tasks.Record
}

type tasksScreen struct {
	hooks   Hooks
	opener  Opener
	item    *Item  // nil for the board
	items   []Item // every vault, for the board and for names
	rows    []taskRow
	notes   int
	cursor  int
	mode    tasksMode
	idea    textinput.Model
	width   int
	status  string
	err     string
	changed bool
	closed  bool
	// launch is set when a key asked for Claude Code; the view runs it.
	launch *taskLaunch
}

type taskLaunch struct {
	name, vault, dir, prompt string
}

func newTasks(hooks Hooks, opener Opener, item *Item, items []Item, width int) tasksScreen {
	idea := textinput.New()
	idea.Prompt = ""
	idea.Placeholder = "the idea, in your words"
	idea.CharLimit = 400
	idea.Width = 64
	s := tasksScreen{hooks: hooks, opener: opener, item: item, items: items, idea: idea, width: width}
	s.reload(items)
	return s
}

// reload rebuilds the boxes from the ledgers.
func (s *tasksScreen) reload(items []Item) {
	s.items = items
	if s.item != nil {
		for i := range items {
			if items[i].Entry.Path == s.item.Entry.Path {
				s.item = &items[i]
			}
		}
	}
	s.rows = nil
	s.notes = 0
	s.err = ""
	for i := range items {
		it := &items[i]
		if it.Entry.Kind != vault.Project || it.Entry.Error != "" {
			continue
		}
		if s.item != nil && it.Entry.Path != s.item.Entry.Path {
			continue
		}
		led, notes, err := s.hooks.Tasks(it.Entry)
		if err != nil {
			if s.item != nil {
				s.err = err.Error()
			}
			continue
		}
		s.notes += len(notes)
		for _, rec := range led.Open() {
			s.rows = append(s.rows, taskRow{project: it, rec: rec})
		}
	}
	sort.SliceStable(s.rows, func(i, j int) bool { return tasks.Less(s.rows[i].rec, s.rows[j].rec) })
	if s.cursor >= len(s.rows) {
		s.cursor = max(0, len(s.rows)-1)
	}
}

func (s tasksScreen) current() *taskRow {
	if len(s.rows) == 0 {
		return nil
	}
	return &s.rows[s.cursor]
}

// plantTarget is the project a plant goes into: the screen's, else the selected task's.
func (s tasksScreen) plantTarget() *Item {
	if s.item != nil {
		return s.item
	}
	if row := s.current(); row != nil {
		return row.project
	}
	return nil
}

func (s tasksScreen) update(msg tea.Msg) (tasksScreen, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	s.launch = nil
	switch s.mode {
	case tasksList:
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
			return s.open(), nil
		default:
			switch key.String() {
			case "q":
				s.closed = true
			case "p":
				if s.plantTarget() == nil {
					s.err = "select a task to plant into its project, or press t on a project"
					return s, nil
				}
				if s.hooks.Plant == nil {
					s.err = "planting is not available here"
					return s, nil
				}
				s.idea.SetValue("")
				s.mode = tasksPlant
				return s, s.idea.Focus()
			case "c":
				return s.claude(), nil
			case "o":
				return s.open(), nil
			}
		}
		return s, nil
	case tasksPlant:
		if isKey {
			switch key.Type {
			case tea.KeyEnter:
				return s.plant(), nil
			case tea.KeyEsc:
				s.mode = tasksList
				return s, nil
			}
		}
		var cmd tea.Cmd
		s.idea, cmd = s.idea.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s tasksScreen) plant() tasksScreen {
	text := strings.TrimSpace(s.idea.Value())
	if text == "" {
		s.mode = tasksList
		return s
	}
	target := s.plantTarget()
	if target == nil {
		s.mode = tasksList
		s.err = listChanged
		return s
	}
	planted, err := s.hooks.Plant(target.Entry, tasks.Plant{Text: text})
	if err != nil {
		s.err = err.Error()
		return s
	}
	s.mode = tasksList
	s.status = fmt.Sprintf("planted %s in %s", planted.ID, target.Entry.Name)
	s.changed = true
	s.reload(s.items)
	for i, row := range s.rows {
		if row.rec.ID == planted.ID {
			s.cursor = i
		}
	}
	return s
}

// claude asks the view to start Claude Code on the selected task, in its workdir when it
// has one; with no task, the session starts in the project's vault on the task skill.
func (s tasksScreen) claude() tasksScreen {
	if s.opener.ClaudeIn == nil {
		s.err = "starting Claude Code is not available here"
		return s
	}
	row := s.current()
	switch {
	case row != nil:
		s.launch = &taskLaunch{name: row.rec.Title, vault: row.project.Entry.Path, dir: row.rec.Workdir, prompt: claudecode.TaskPrompt(row.rec.ID)}
	case s.item != nil:
		s.launch = &taskLaunch{name: s.item.Entry.Name, vault: s.item.Entry.Path, prompt: "/claude-atlas:task"}
	default:
		s.err = "select a task to continue"
	}
	return s
}

func (s tasksScreen) open() tasksScreen {
	row := s.current()
	if row == nil {
		return s
	}
	if s.opener.OpenPath == nil {
		s.err = "opening pages is not available here"
		return s
	}
	if err := s.opener.OpenPath(row.project.Entry.Path + "/" + row.rec.Path); err != nil {
		s.err = err.Error()
		return s
	}
	s.status = "opened " + row.rec.Title + " in Obsidian"
	return s
}

func (s tasksScreen) view() string {
	var b strings.Builder
	if s.item != nil {
		fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", title.Render(s.item.Entry.Name), catSt.Render("tasks"), dim.Render(home.Display(s.item.Entry.Path)))
	} else {
		projects := map[string]bool{}
		for _, row := range s.rows {
			projects[row.project.Entry.Path] = true
		}
		fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", title.Render("Atlas"), catSt.Render("tasks"), dim.Render(fmt.Sprintf("%d open across %d project%s", len(s.rows), len(projects), plural(len(projects)))))
	}
	if len(s.rows) == 0 {
		if s.item != nil {
			b.WriteString("  " + dim.Render("no open tasks; press p to plant one") + "\n")
		} else {
			b.WriteString("  " + dim.Render("no open tasks in any project; press t on a project to plant one") + "\n")
		}
	}
	width := min(76, max(32, s.width-6))
	for i, row := range s.rows {
		style := boxSt
		name := row.rec.Title
		if i == s.cursor && s.mode == tasksList {
			style = boxSelSt
			name = selSt.Render(name)
		}
		badge := row.rec.Status + " · " + row.rec.Priority
		if tasks.Stale(row.rec, now()) {
			badge = errSt.Render(badge + " · stale")
		} else {
			badge = dim.Render(badge)
		}
		pad := max(1, width-2-len(row.rec.Title)-len(stripANSI(badge)))
		lines := []string{name + strings.Repeat(" ", pad) + badge}
		second := row.rec.ID
		if row.rec.LastTouched != "" {
			second += " · touched " + row.rec.LastTouched
		}
		if row.rec.Due != "" {
			second += " · due " + row.rec.Due
		}
		if s.item == nil {
			second = row.project.Entry.Name + " · " + second
		}
		lines = append(lines, dim.Render(second))
		if row.rec.Workdir != "" {
			lines = append(lines, dim.Render("workdir "+home.Display(row.rec.Workdir)))
		}
		b.WriteString(indent(style.Width(width).Render(strings.Join(lines, "\n")), "  ") + "\n")
	}
	if s.notes > 0 {
		fmt.Fprintf(&b, "  %s\n", dim.Render(fmt.Sprintf("%d task note%s waiting in inbox/tasks/; c then /claude-atlas:task-plant turns them into tasks", s.notes, plural(s.notes))))
	}
	b.WriteString("\n")
	switch s.mode {
	case tasksPlant:
		into := listChanged
		if target := s.plantTarget(); target != nil {
			into = "plants into " + target.Entry.Name + " with status planted · Enter plant · Esc cancel"
		}
		b.WriteString("  " + activeL.Width(9).Render("Idea") + s.idea.View() + "\n")
		b.WriteString("  " + dim.Render(into) + "\n")
	default:
		hints := "p plant"
		if len(s.rows) > 0 {
			hints = "↑↓ move · p plant · c continue in Claude Code · o open in Obsidian"
		} else if s.item != nil {
			hints += " · c Claude Code"
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

// stripANSI measures styled text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
		case r == 0x1b:
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
