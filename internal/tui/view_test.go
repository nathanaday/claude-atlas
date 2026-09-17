package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func knowledge(name, heat string) Item {
	four, one := 4, 1
	return Item{Entry: registry.Entry{
		ID: "id-" + name, Kind: registry.Knowledge, Name: name, Path: "/v/" + name, Mode: vault.Generic,
		Created: "2026-09-01", Scope: name + " sources",
		State: &registry.State{OK: true, Heat: heat, Pages: &four, Inbox: &one,
			GeneratedAt: "2026-09-12T18:00:00Z", OpenThreads: []string{"thread"}, LastOperation: "2026-09-10"},
	}}
}

func proj(name, kb, heat string) Item {
	e := registry.Entry{
		ID: "id-" + name, Kind: registry.Project, Name: name, Path: "/code/" + name,
		Created: "2026-09-01", Description: "the " + name + " work",
		State: &registry.State{OK: true, Heat: heat, GeneratedAt: "2026-09-12T18:00:00Z"},
	}
	if kb != "" {
		e.Knowledge = &registry.Ref{ID: "id-" + kb, Name: kb, Path: "/v/" + kb}
	}
	return Item{Entry: e}
}

// sample is two knowledge bases, three projects (two on papers, one with no knowledge
// base), and one entry the scan could not read.
func sample() []Item {
	items := []Item{
		knowledge("papers", "warm"),
		knowledge("ai-ml", "cold"),
		proj("webapp", "papers", "hot"),
		proj("firmware", "papers", "cold"),
		proj("thesis", "", "new"),
	}
	zero := 0
	items[0].Entry.Projects = []registry.Ref{{ID: "id-webapp", Name: "webapp"}, {ID: "id-firmware", Name: "firmware"}}
	items[2].Entry.State.DaysIdle = &zero
	items[2].Entry.State.Tasks = &registry.TaskSummary{
		Counts: tasks.Counts{Open: 3, Active: 1, Planned: 2, Phases: 1},
		Open:   []registry.TaskLine{{ID: "task-20260917-0001", Title: "Filter vehicle false alarms", Status: "active", Priority: "high", Phase: "Alarm quality"}},
		Phases: []string{"Alarm quality", "Launch"},
	}
	items[2].Entry.State.Described = &registry.Description{Page: "wiki/entities/webapp.md", Commit: "abc1234", Behind: 2}
	items = append(items, Item{Entry: registry.Entry{Path: "/old/gateway", Error: missingError, Reason: registry.ReasonMissing}})
	return items
}

const missingError = "not found; work in it again to heal the path, or run claude-atlas forget"

func entriesOf(items []Item) []registry.Entry {
	out := make([]registry.Entry, 0, len(items))
	for _, it := range items {
		out = append(out, it.Entry)
	}
	return out
}

// findEntry puts the cursor on the entry with that name, on its tab.
func findEntry(t *testing.T, v view, name string) view {
	t.Helper()
	for i := range v.boards {
		for j, it := range v.boards[i].items {
			if entryName(it.Entry) == name {
				v.tab = boardTab(i)
				v.boards[i].cursor = j
				v.boards[i].layout()
				return v
			}
		}
	}
	t.Fatalf("no box for %s", name)
	return v
}

func pressV(v view, keys ...tea.KeyType) view {
	for _, k := range keys {
		next, _ := v.Update(tea.KeyMsg{Type: k})
		v = next.(view)
	}
	return v
}

func keyV(v view, s string) view {
	next, _ := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(view)
}

func runCmd(v view, cmd tea.Cmd) view {
	if cmd == nil {
		return v
	}
	next, _ := v.Update(cmd())
	return next.(view)
}

// quits reports whether a command is tea.Quit.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestTabBarAndArrows(t *testing.T) {
	v := newView(sample(), Opener{}, actions.Atlas{})
	out := v.View()
	t.Logf("\n%s", out)
	for _, want := range []string{"Atlas   Knowledge (2)  Projects (3)  Problems (1)", "A knowledge base is the wiki", "refreshed 2026-09-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if v.tab != tabKnowledge || len(v.boards[0].rows) != 2 {
		t.Fatalf("the Knowledge tab lists the knowledge bases: tab=%d rows=%d", v.tab, len(v.boards[0].rows))
	}
	v = pressV(v, tea.KeyRight)
	if v.tab != tabProjects || !strings.Contains(v.View(), "A project is an atlas/ folder") || len(v.boards[1].rows) != 3 {
		t.Fatalf("right: tab=%d\n%s", v.tab, v.View())
	}
	v = pressV(v, tea.KeyRight)
	if v.tab != tabProblems || !strings.Contains(v.View(), "could not read") {
		t.Fatalf("problems: tab=%d", v.tab)
	}
	v = pressV(v, tea.KeyRight)
	if v.tab != tabProblems {
		t.Fatal("the bar does not wrap")
	}
	v = pressV(v, tea.KeyLeft, tea.KeyLeft, tea.KeyLeft)
	if v.tab != tabKnowledge {
		t.Fatalf("left stops at Knowledge: tab=%d", v.tab)
	}
	clean := newView(sample()[:5], Opener{}, actions.Atlas{})
	if strings.Contains(clean.View(), "Problems") || len(clean.tabs()) != 2 {
		t.Fatal("no problems, no Problems tab")
	}
}

func TestKnowledgeBoxesShowScopePagesInboxAndProjects(t *testing.T) {
	v := newView(sample(), Opener{}, actions.Atlas{})
	out := v.View()
	for _, want := range []string{"papers", "papers sources", "4 pages", "1 in inbox", "2 projects: webapp, firmware", "used by no project yet", "🌤️", "❄️"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestProjectsAreGroupedByKnowledgeBase(t *testing.T) {
	v := newView(sample(), Opener{}, actions.Atlas{})
	v.goTo(tabProjects)
	out := v.View()
	t.Logf("\n%s", out)
	for _, want := range []string{"papers", "no knowledge base", "webapp", "firmware", "thesis", "3 tasks open", "phase: Alarm quality", "touched today", "the webapp work", "/code/webapp"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	names := []string{}
	for _, it := range v.boards[1].items {
		names = append(names, it.Entry.Name)
	}
	if got := strings.Join(names, ","); got != "firmware,webapp,thesis" {
		t.Fatalf("grouped by knowledge base, the ungrouped last: %s", got)
	}
	if strings.Index(out, "papers") > strings.Index(out, "no knowledge base") {
		t.Fatal("the knowledge base group comes before the ungrouped projects")
	}
}

func TestEnterExpandsInPlace(t *testing.T) {
	v := findEntry(t, newView(sample(), Opener{}, actions.Atlas{}), "webapp")
	v = pressV(v, tea.KeyEnter)
	out := v.View()
	t.Logf("\n%s", out)
	for _, want := range []string{"Path", "/code/webapp", "Knowledge", "papers", "Described", "described in wiki/entities/webapp.md at abc1234, 2 commits behind", "Tasks", "3 open: 1 active", "Phases", "Alarm quality → Launch", "[active] Filter vehicle false alarms", "Enter collapse"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	v = pressV(v, tea.KeyEnter)
	if out := v.View(); strings.Contains(out, "Described") || !strings.Contains(out, "Enter details") {
		t.Fatalf("Enter again collapses:\n%s", out)
	}
	v = findEntry(t, v, "papers")
	v = pressV(v, tea.KeyEnter)
	out = v.View()
	for _, want := range []string{"Mode", "generic", "Scope", "papers sources", "Last operation", "2026-09-10", "Open threads", "- thread", "Unfinished"} {
		if !strings.Contains(out, want) {
			t.Errorf("knowledge base details miss %q:\n%s", want, out)
		}
	}
	v = findEntry(t, v, "thesis")
	v = pressV(v, tea.KeyEnter)
	if out := v.View(); !strings.Contains(out, "Knowledge      none") {
		t.Fatalf("a project without a knowledge base says so:\n%s", out)
	}
	v = findEntry(t, v, "gateway")
	v = pressV(v, tea.KeyEnter)
	if out := v.View(); !strings.Contains(out, "Reason") || !strings.Contains(out, "missing") || !strings.Contains(out, "heal the path") {
		t.Fatalf("a problem explains itself:\n%s", out)
	}
}

func TestEscCollapsesThenQuits(t *testing.T) {
	v := findEntry(t, newView(sample(), Opener{}, actions.Atlas{}), "webapp")
	v = pressV(v, tea.KeyEnter)
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyEsc})
	v = next.(view)
	if quits(cmd) || len(v.board().expanded) != 0 {
		t.Fatal("the first Esc collapses")
	}
	_, cmd = v.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !quits(cmd) {
		t.Fatal("the second Esc quits")
	}
	_, cmd = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !quits(cmd) {
		t.Fatal("q quits")
	}
}

func TestCursorMovesAndStopsAtTheEndMarker(t *testing.T) {
	v := newView(sample(), Opener{}, actions.Atlas{})
	v.goTo(tabProjects)
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown)
	if !v.board().atEnd() || v.current() != nil {
		t.Fatalf("four downs land on the end marker: cursor=%d", v.board().cursor)
	}
	if !strings.Contains(v.View(), "(end)") {
		t.Fatal("the end marker shows")
	}
	v = pressV(v, tea.KeyUp)
	if it := v.current(); it == nil || it.Entry.Name != "thesis" {
		t.Fatal("up from the end lands on the last project")
	}
}

func TestOpensOnlyKnowledgeBasesInObsidian(t *testing.T) {
	opened := ""
	opener := Opener{Obsidian: func(path string) error { opened = path; return nil }}
	v := findEntry(t, newView(sample(), opener, actions.Atlas{}), "papers")
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	v = next.(view)
	if v.busy == "" || cmd == nil {
		t.Fatal("o opens in the background")
	}
	v = runCmd(v, cmd)
	if opened != "/v/papers" || v.busy != "" || !strings.Contains(v.status, "opened papers in Obsidian") {
		t.Fatalf("opened=%q busy=%q status=%q", opened, v.busy, v.status)
	}
	v = findEntry(t, v, "webapp")
	v = keyV(v, "o")
	if !strings.Contains(v.errMsg, "not an Obsidian vault") {
		t.Fatalf("a project does not open in Obsidian: %q", v.errMsg)
	}
	failing := Opener{Obsidian: func(string) error { return errors.New("no Obsidian") }}
	v = findEntry(t, newView(sample(), failing, actions.Atlas{}), "papers")
	next, cmd = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	v = runCmd(next.(view), cmd)
	if v.errMsg != "no Obsidian" {
		t.Fatalf("the error reaches the footer: %q", v.errMsg)
	}
}

func TestClaudeStartsInEitherKind(t *testing.T) {
	launched := ""
	opener := Opener{Claude: func(path string) error { launched = path; return nil }}
	v := findEntry(t, newView(sample(), opener, actions.Atlas{}), "webapp")
	_, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil {
		t.Fatal("c hands the terminal to Claude Code")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); ok {
		t.Fatal("c does not quit")
	}
	// The command wraps an exec; running its callback path is Bubble Tea's. Drive the
	// done message by hand.
	next, refreshCmd := v.Update(claudeDoneMsg{name: "webapp"})
	v = next.(view)
	if !strings.Contains(v.errMsg+v.status, "back from Claude Code in webapp") && refreshCmd != nil {
		t.Fatalf("status=%q err=%q", v.status, v.errMsg)
	}
	if launched != "" {
		t.Fatalf("the launch runs only when Bubble Tea executes it: %q", launched)
	}
	if l := (launch{run: func() error { launched = "ran"; return nil }}); l.Run() != nil || launched != "ran" {
		t.Fatal("the launch adapter runs the opener")
	}
	none := findEntry(t, newView(sample(), Opener{}, actions.Atlas{}), "papers")
	none = keyV(none, "c")
	if !strings.Contains(none.errMsg, "not available") {
		t.Fatalf("no opener: %q", none.errMsg)
	}
}

func TestKeysOnAProblemRefuse(t *testing.T) {
	v := findEntry(t, newView(sample(), Opener{}, actions.Atlas{}), "gateway")
	for _, key := range []string{"o", "c", "p"} {
		v = keyV(v, key)
		if !strings.Contains(v.errMsg, "not found") {
			t.Fatalf("%s on a problem names the error: %q", key, v.errMsg)
		}
	}
}

func TestRefreshReloadsAndKeepsTheCursor(t *testing.T) {
	refreshed := 0
	acts := actions.Atlas{
		Load:    func() ([]registry.Entry, error) { return entriesOf(sample()), nil },
		Refresh: func() (*registry.Index, error) { refreshed++; return &registry.Index{}, nil },
	}
	v := findEntry(t, newView(sample(), Opener{}, acts), "firmware")
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	v = next.(view)
	if v.busy == "" || cmd == nil {
		t.Fatal("R refreshes in the background")
	}
	v = runCmd(v, cmd)
	if refreshed != 1 || v.busy != "" || v.status != "refreshed" || !v.changed {
		t.Fatalf("refreshed=%d busy=%q status=%q changed=%v", refreshed, v.busy, v.status, v.changed)
	}
	if it := v.current(); it == nil || it.Entry.Name != "firmware" {
		t.Fatal("the cursor stays on the same project")
	}
	none := keyV(newView(sample(), Opener{}, actions.Atlas{}), "R")
	if !strings.Contains(none.errMsg, "not available") {
		t.Fatalf("no refresh action: %q", none.errMsg)
	}
}

func TestPlantPromptsForOneLineAndPlants(t *testing.T) {
	var got tasks.Plant
	var into string
	acts := actions.Atlas{
		Plant: func(e registry.Entry, p tasks.Plant) (*tasks.Task, error) {
			got, into = p, e.Name
			return &tasks.Task{ID: "task-20260917-abcd", Title: tasks.TitleFromText(p.Text)}, nil
		},
	}
	v := findEntry(t, newView(sample(), Opener{}, acts), "webapp")
	v = keyV(v, "p")
	if v.plant == nil || !strings.Contains(v.View(), "task for webapp:") || !strings.Contains(v.View(), "Enter plant") {
		t.Fatalf("p opens the prompt:\n%s", v.View())
	}
	for _, r := range "fix the login page" {
		v = keyV(v, string(r))
	}
	v = keyV(v, "q") // q is text while the prompt is open
	v = pressV(v, tea.KeyBackspace, tea.KeyEnter)
	if v.plant != nil || into != "webapp" || got.Text != "fix the login page" {
		t.Fatalf("Enter plants: into=%q text=%q", into, got.Text)
	}
	if !v.changed || !strings.Contains(v.status, "planted fix the login page in webapp (task-20260917-abcd)") {
		t.Fatalf("status=%q changed=%v", v.status, v.changed)
	}
	v = keyV(v, "p")
	v = pressV(v, tea.KeyEsc)
	if v.plant != nil {
		t.Fatal("Esc cancels the prompt")
	}
	v = keyV(v, "p")
	v = pressV(v, tea.KeyEnter)
	if v.plant != nil || into != "webapp" || v.errMsg != "" {
		t.Fatal("an empty line plants nothing and says nothing")
	}
	v = findEntry(t, v, "papers")
	v = keyV(v, "p")
	if v.plant != nil || !strings.Contains(v.errMsg, "plant into a project") {
		t.Fatalf("a knowledge base has no tasks: %q", v.errMsg)
	}
	failing := actions.Atlas{Plant: func(registry.Entry, tasks.Plant) (*tasks.Task, error) { return nil, errors.New("no phase named x") }}
	v = findEntry(t, newView(sample(), Opener{}, failing), "webapp")
	v = keyV(v, "p")
	v = keyV(v, "x")
	v = pressV(v, tea.KeyEnter)
	if v.errMsg != "no phase named x" {
		t.Fatalf("a plant error reaches the footer: %q", v.errMsg)
	}
}

func TestPlantWritesARealTaskPage(t *testing.T) {
	work := t.TempDir()
	p, _, err := project.Init(work, project.Options{Name: "webapp"}, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	acts := actions.Atlas{Plant: func(e registry.Entry, plant tasks.Plant) (*tasks.Task, error) {
		return tasks.PlantTask(p, plant, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	}}
	items := []Item{{Entry: registry.Entry{ID: "id", Kind: registry.Project, Name: "webapp", Path: work}}}
	v := findEntry(t, newView(items, Opener{}, acts), "webapp")
	v = keyV(v, "p")
	for _, r := range "Write the README" {
		v = keyV(v, string(r))
	}
	v = pressV(v, tea.KeyEnter)
	if v.errMsg != "" {
		t.Fatal(v.errMsg)
	}
	page := filepath.Join(work, project.Dir, project.TasksDir, "Write the README.md")
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("the page is written: %v", err)
	}
	if !strings.Contains(string(data), "status: planted") {
		t.Fatalf("planted:\n%s", data)
	}
	if _, err := os.Stat(filepath.Join(work, project.Dir, project.TasksIndex)); err != nil {
		t.Fatal("the index is generated")
	}
}

func TestHelpTogglesTheFooter(t *testing.T) {
	v := findEntry(t, newView(sample(), Opener{}, actions.Atlas{}), "papers")
	out := v.View()
	if !strings.Contains(out, "Enter details · o Obsidian · c Claude · h help · q quit") || strings.Contains(out, "←→ tabs") {
		t.Fatalf("help off names the tab's keys:\n%s", out)
	}
	v = keyV(v, "h")
	out = v.View()
	if !strings.Contains(out, "↑↓ move") || !strings.Contains(out, "←→ tabs · R refresh · h hide help · q quit") {
		t.Fatalf("help on names every key:\n%s", out)
	}
	v = findEntry(t, keyV(v, "h"), "webapp")
	if out := v.View(); !strings.Contains(out, "c Claude · p plant") || strings.Contains(out, "o Obsidian") {
		t.Fatalf("a project offers Claude and plant, not Obsidian:\n%s", out)
	}
}

// Every frame is exactly as tall as the screen and no wider, so the terminal never
// scrolls the tab bar out of sight on one tab and leaves it in place on another.
func TestEveryTabFillsTheScreenExactly(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 120, Height: 40}, {Width: 80, Height: 24}, {Width: 60, Height: 12}} {
		v := newView(sample(), Opener{}, actions.Atlas{})
		next, _ := v.Update(size)
		v = next.(view)
		for _, name := range []string{"Knowledge", "Projects", "Problems"} {
			v = pressV(v, tea.KeyEnter)
			out := v.View()
			if got := strings.Count(out, "\n") + 1; got != size.Height {
				t.Errorf("%s at %dx%d is %d lines, want %d:\n%s", name, size.Width, size.Height, got, size.Height, out)
			}
			for _, line := range strings.Split(out, "\n") {
				if w := lipgloss.Width(line); w > size.Width {
					t.Errorf("%s at %dx%d: %q is %d columns, want at most %d", name, size.Width, size.Height, line, w, size.Width)
				}
			}
			v = pressV(v, tea.KeyRight)
		}
	}
}

func TestEmptyTabsSayWhatToRun(t *testing.T) {
	v := newView(nil, Opener{}, actions.Atlas{})
	if out := v.View(); !strings.Contains(out, "claude-atlas new-knowledge NAME") {
		t.Fatalf("empty knowledge tab:\n%s", out)
	}
	v.goTo(tabProjects)
	if out := v.View(); !strings.Contains(out, "claude-atlas init") {
		t.Fatalf("empty projects tab:\n%s", out)
	}
}

func TestNarrowBarDropsTheStampThenTheCounts(t *testing.T) {
	v := newView(sample(), Opener{}, actions.Atlas{})
	v.width = 120
	if !strings.Contains(v.tabBar(), "refreshed 2026") {
		t.Fatal("a wide screen shows the stamp")
	}
	v.width = 50
	if bar := v.tabBar(); strings.Contains(bar, "refreshed") || !strings.Contains(bar, "(2)") {
		t.Fatalf("a narrower screen keeps the counts: %q", bar)
	}
	v.width = 30
	if bar := v.tabBar(); strings.Contains(bar, "(2)") {
		t.Fatalf("a narrow screen drops the counts: %q", bar)
	}
}
