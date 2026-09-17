package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
)

func taskHooks(ledgers map[string]*tasks.Ledger, planted *[]string) actions.Atlas {
	return actions.Atlas{
		Load: func() ([]registry.Entry, error) { return entriesOf(sample()), nil },
		Tasks: func(e registry.Entry) (tasks.Ledger, []string, error) {
			if led, ok := ledgers[e.Path]; ok {
				return *led, nil, nil
			}
			return tasks.Empty(), nil, nil
		},
		Plant: func(e registry.Entry, plant tasks.Plant) (txn.Planted, error) {
			*planted = append(*planted, e.Name+": "+plant.Text)
			led := ledgers[e.Path]
			if led == nil {
				l := tasks.Empty()
				led = &l
				ledgers[e.Path] = led
			}
			id := "task-20260913-00" + string(rune('a'+len(led.Tasks)))
			led.Tasks = append(led.Tasks, tasks.Record{Task: tasks.Task{ID: id, Path: "wiki/tasks/x.md", Title: plant.Text, Status: "planted", Priority: "normal"}})
			return txn.Planted{Path: "wiki/tasks/x.md", ID: id}, nil
		},
		Refresh: func() (*registry.Index, []refresh.ProjectChange, error) { return nil, nil, nil },
	}
}

func TestTasksScreenPlantsListsAndContinues(t *testing.T) {
	ledgers := map[string]*tasks.Ledger{
		"/v/p3": {Tasks: []tasks.Record{
			{Task: tasks.Task{ID: "task-20260901-aaaa", Path: "wiki/tasks/Fix it.md", Title: "Fix it", Status: "active", Priority: "high", Workdir: "/code/p3"}, LastTouched: "2026-09-10"},
			{Task: tasks.Task{ID: "task-20260901-bbbb", Path: "wiki/tasks/Later.md", Title: "Later", Status: "planted", Priority: "low"}, LastTouched: "2026-09-01"},
			{Task: tasks.Task{ID: "task-20260801-cccc", Path: "wiki/tasks/archive/Old.md", Title: "Old", Status: "done", Priority: "normal"}},
		}},
		"/v/welcome": {Tasks: []tasks.Record{{Task: tasks.Task{ID: "task-20260902-dddd", Path: "wiki/tasks/W.md", Title: "Welcome task", Status: "blocked", Priority: "normal"}}}},
	}
	var planted []string
	var launched []string
	op := Opener{
		ClaudeIn: func(vault, dir, prompt string) (*exec.Cmd, error) {
			launched = append(launched, vault+"|"+dir+"|"+prompt)
			return exec.Command("true"), nil
		},
		OpenPath: func(path string) error { launched = append(launched, "open "+path); return nil },
	}
	v := newView(sample(), op, taskHooks(ledgers, &planted))
	v = findVault(t, v, "p3")
	v = keyV(v, "t")
	out := v.View()
	if v.tasks == nil || !strings.Contains(out, "p3   tasks") || !strings.Contains(out, "Fix it") || !strings.Contains(out, "active · high") || !strings.Contains(out, "workdir /code/p3") || strings.Contains(out, "Old") {
		t.Fatalf("project tasks:\n%s", out)
	}
	if v.tasks.rows[0].rec.ID != "task-20260901-aaaa" || v.tasks.rows[1].rec.ID != "task-20260901-bbbb" {
		t.Fatalf("board order: %+v", v.tasks.rows)
	}
	// c continues the selected task in its workdir with the task-run prompt.
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	v = next.(view)
	if cmd == nil || len(launched) != 1 || launched[0] != "/v/p3|/code/p3|/claude-atlas:task-run task-20260901-aaaa" {
		t.Fatalf("launch: %v cmd=%v", launched, cmd)
	}
	// o opens the page.
	v = keyV(v, "o")
	if launched[1] != "open /v/p3/wiki/tasks/Fix it.md" || !strings.Contains(v.tasks.status, "opened Fix it") {
		t.Fatalf("open: %v status=%q", launched, v.tasks.status)
	}
	// p plants into the project and refreshes in the background.
	v = keyV(v, "p")
	if v.tasks.mode != tasksPlant || !strings.Contains(v.View(), "plants into p3") {
		t.Fatalf("plant mode:\n%s", v.View())
	}
	v = typeV(v, "Write the docs")
	next, cmd = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	v = next.(view)
	if len(planted) != 1 || planted[0] != "p3: Write the docs" || cmd == nil || !strings.Contains(v.tasks.status, "planted task-") || len(v.tasks.rows) != 3 || !v.changed {
		t.Fatalf("plant: %v status=%q rows=%d", planted, v.tasks.status, len(v.tasks.rows))
	}
	if v.tasks.rows[v.tasks.cursor].rec.Title != "Write the docs" {
		t.Fatal("cursor lands on the new task")
	}
	v = pressV(v, tea.KeyEsc)
	if v.tasks != nil {
		t.Fatal("esc closes")
	}
	// T shows every project's open tasks, blocked ones before planted ones, with vault names.
	v = keyV(v, "T")
	out = v.View()
	if v.tasksTab == nil || v.tasksTab.item != nil || len(v.tasksTab.rows) != 4 || !strings.Contains(out, "welcome · task-20260902-dddd") {
		t.Fatalf("board:\n%s", out)
	}
	if v.tasksTab.rows[0].rec.Status != "active" || v.tasksTab.rows[1].rec.Status != "blocked" {
		t.Fatalf("board order %+v", v.tasksTab.rows)
	}
	v = pressV(v, tea.KeyDown)
	v = keyV(v, "p")
	if !strings.Contains(v.View(), "plants into welcome") {
		t.Fatalf("board plant target:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEsc, tea.KeyEsc)
	if v.tab != tabProjects {
		t.Fatal("esc returns to Projects")
	}
	// An empty project offers to plant; c there opens the task skill in the vault.
	v = findVault(t, v, "course")
	if it := v.current(); it == nil || it.Entry.Name != "course" {
		t.Fatalf("cursor %+v", v.current())
	}
	v = keyV(v, "t")
	if !strings.Contains(v.View(), "no open tasks; press p to plant one") {
		t.Fatalf("empty:\n%s", v.View())
	}
	next, cmd = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	v = next.(view)
	if cmd == nil || launched[len(launched)-1] != "/v/course||/claude-atlas:task" {
		t.Fatalf("c on an empty project: %v", launched)
	}
	none := keyV(newView(sample(), Opener{}, actions.Atlas{}), "t")
	if none.tasks != nil || !strings.Contains(none.errMsg, "not available") {
		t.Fatal("t without hooks reports why")
	}
	// A knowledge base has no tasks of its own.
	kb := findVault(t, newView(sample(), op, taskHooks(ledgers, &planted)), "ai-ml")
	kb = keyV(kb, "t")
	if kb.tasks != nil || !strings.Contains(kb.errMsg, "knowledge base") {
		t.Fatalf("t on a knowledge base: tasks=%v err=%q", kb.tasks, kb.errMsg)
	}
}

// Every card has the same shape, and a long title wraps instead of pushing the status
// off its own line.
func TestATaskCardWrapsItsTitle(t *testing.T) {
	long := "Work out why the planner drops frames when the thermal camera and the visible camera disagree about the scene for more than a second at a time on the edge box"
	ledgers := map[string]*tasks.Ledger{
		"/v/p3": {Tasks: []tasks.Record{
			{Task: tasks.Task{ID: "task-20260901-aaaa", Path: "wiki/tasks/x.md", Title: long, Status: "planted", Priority: "normal"}},
			{Task: tasks.Task{ID: "task-20260902-bbbb", Path: "wiki/tasks/y.md", Title: "Short one", Status: "active", Priority: "high", Due: "2026-09-20"}},
		}},
	}
	var planted []string
	v := keyV(newView(sample(), Opener{}, taskHooks(ledgers, &planted)), "T")
	next, _ := v.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	v = next.(view)
	out := v.View()
	t.Logf("\n%s", out)

	width := min(76, max(32, v.tasksTab.width-6))
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > v.width {
			t.Fatalf("a card overflows the screen: %q is %d wide", line, lipgloss.Width(line))
		}
	}
	// The project and the id come first, the title wraps under them, the status last.
	var card []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "p3 · task-20260901-aaaa") {
			card = []string{line}
			continue
		}
		if card != nil && strings.Contains(line, "│") {
			card = append(card, line)
		} else if card != nil {
			break
		}
	}
	if len(card) < 5 {
		t.Fatalf("the long card should have a header, wrapped title lines, and a status line:\n%s", out)
	}
	titled := 0
	for _, line := range card[1:] {
		if strings.Contains(line, "planted · normal") {
			break
		}
		titled++
	}
	if titled != titleLines {
		t.Fatalf("a long title takes %d lines, want %d:\n%s", titled, titleLines, out)
	}
	if !strings.Contains(strings.Join(card, "\n"), "…") {
		t.Fatalf("a title that does not fit ends with an ellipsis:\n%s", out)
	}
	// A short title keeps its own line, and the status still has one of its own.
	if !strings.Contains(out, "Short one") || !strings.Contains(out, "active · high · due 2026-09-20") {
		t.Fatalf("the second card:\n%s", out)
	}
	if got := wrapTo("one two three", width-2, 3); len(got) != 1 || got[0] != "one two three" {
		t.Fatalf("a title that fits is one line: %q", got)
	}
}

// A long board scrolls: the window follows the cursor and the footer stays on screen.
func TestTheTasksBoardScrolls(t *testing.T) {
	var recs []tasks.Record
	for i := 1; i <= 6; i++ {
		recs = append(recs, tasks.Record{Task: tasks.Task{
			ID: fmt.Sprintf("task-2026090%d-aaaa", i), Path: "wiki/tasks/x.md",
			Title: fmt.Sprintf("Task %d", i), Status: "active", Priority: "normal"}})
	}
	var planted []string
	v := keyV(newView(sample(), Opener{}, taskHooks(map[string]*tasks.Ledger{"/v/p3": {Tasks: recs}}, &planted)), "T")
	v = keyV(v, "h")                                              // help on: the board's hints show, and the footer takes two lines
	next, _ := v.Update(tea.WindowSizeMsg{Width: 80, Height: 15}) // eight lines for the boxes
	v = next.(view)
	if v.tasksTab == nil || len(v.tasksTab.rows) != 6 || v.tasksTab.avail != v.bodyHeight() || v.tasksTab.avail != 8 {
		t.Fatalf("six tasks in an eight-line window: %+v", v.tasksTab)
	}
	out := v.View()
	t.Logf("\n%s", out)
	if !strings.Contains(out, "Task 1") || !strings.Contains(out, "Task 2") || strings.Contains(out, "Task 3") {
		t.Fatalf("the first two boxes:\n%s", out)
	}
	if !strings.Contains(out, "more lines") || !strings.Contains(out, "p plant") {
		t.Fatalf("the rest is counted and the hints stay:\n%s", out)
	}
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown)
	out = v.View()
	t.Logf("\n%s", out)
	if v.tasksTab.cursor != 5 || !strings.Contains(out, "Task 6") || strings.Contains(out, "Task 1") {
		t.Fatalf("the window follows the cursor: cursor=%d\n%s", v.tasksTab.cursor, out)
	}
	if !strings.Contains(out, "p plant") {
		t.Fatalf("the hints stay on screen:\n%s", out)
	}
}

// The same for the plant prompt on the board: the project it would plant into can go
// away while the user is typing.
func TestPlantSurvivesAReloadThatDroppedTheProject(t *testing.T) {
	ledgers := map[string]*tasks.Ledger{
		"/v/p3": {Tasks: []tasks.Record{{Task: tasks.Task{ID: "task-20260901-aaaa", Path: "wiki/tasks/x.md", Title: "Fix it", Status: "active", Priority: "high"}}}},
	}
	var planted []string
	entries := entriesOf(sample())
	hooks := taskHooks(ledgers, &planted)
	hooks.Load = func() ([]registry.Entry, error) { return entries, nil }
	v := keyV(newView(sample(), Opener{}, hooks), "T")
	if v.tasksTab == nil || len(v.tasksTab.rows) != 1 {
		t.Fatalf("one task to start: %+v", v.tasksTab)
	}
	v = keyV(v, "p")
	v = typeV(v, "Later")
	entries = nil
	next, _ := v.Update(refreshedMsg{})
	v = next.(view)
	if v.tasksTab == nil || v.tasksTab.plantTarget() != nil {
		t.Fatalf("the refresh takes the project away: %+v", v.tasksTab)
	}
	_ = v.View() // the prompt has no project to name and must still render
	next, _ = v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	v = next.(view)
	if v.tasksTab == nil || v.tasksTab.mode != tasksList || len(planted) != 0 || !strings.Contains(v.tasksTab.err, "the list changed") {
		t.Fatalf("enter after the project is gone: mode=%d planted=%v err=%q", v.tasksTab.mode, planted, v.tasksTab.err)
	}
}
