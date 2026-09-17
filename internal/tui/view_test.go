package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func item(kind vault.Kind, name string, tags []string, heat string) Item {
	four := 4
	e := registry.Entry{
		ID: "id-" + name, Kind: kind, Name: name, Path: "/v/" + name, Mode: vault.Generic,
		Created: "2026-09-01", Tags: tags,
		State: &registry.State{VaultOK: true, Heat: heat, Pages: &four,
			GeneratedAt: "2026-09-12T18:00:00Z", OpenThreads: []string{"thread"}},
	}
	if kind == vault.Knowledge {
		e.Scope, e.Access = name+" sources", vault.AccessOpen
	}
	return Item{Entry: e}
}

// sample is three projects (welcome untagged, p3 under itl, course under usc), two
// knowledge bases, and one vault the scan could not read.
func sample() []Item {
	items := []Item{
		item(vault.Project, "welcome", nil, "new"),
		item(vault.Project, "p3", []string{"itl"}, "hot"),
		item(vault.Project, "course", []string{"usc"}, "cold"),
		item(vault.Knowledge, "papers", nil, "warm"),
		item(vault.Knowledge, "ai-ml", nil, "cold"),
	}
	zero := 0
	// p3 was touched today, has three open tasks, mounts ai-ml, and works in one repository.
	items[1].Entry.State.DaysIdle = &zero
	items[1].Entry.State.Tasks = &registry.TaskSummary{Counts: tasks.Counts{Open: 3, Active: 1, Planned: 2}}
	items[1].Entry.Mounts = []registry.Mount{{ID: "id-ai-ml", Name: "ai-ml", Access: vault.AccessWrite, Effective: vault.AccessWrite, Path: "/v/ai-ml/wiki"}}
	items[1].Entry.Repos = []registry.Repo{{Name: "atlas", Path: "/code/atlas", Remote: "git@example.com:atlas.git", Changes: "pr"}}
	// course asked papers for write and got read.
	items[2].Entry.Mounts = []registry.Mount{{ID: "id-papers", Name: "papers", Access: vault.AccessWrite, Effective: vault.AccessRead, Path: "/v/papers/wiki"}}
	// The knowledge bases know who mounts them; papers has a stale grant too.
	items[3].Entry.MountedBy = []registry.Ref{{ID: "id-course", Name: "course", Access: vault.AccessRead}}
	items[3].Entry.Grants = []registry.Grant{{ID: "gone-0000", Name: "gone-0000", Access: vault.AccessRead, Error: "no project with id gone-0000"}}
	items[4].Entry.MountedBy = []registry.Ref{{ID: "id-p3", Name: "p3", Access: vault.AccessWrite}}
	items = append(items, Item{Entry: registry.Entry{Path: "/v/old-notes", Error: v1Error, Reason: registry.ReasonV1}})
	return items
}

// v1Error is what the scan says about a vault from version 1; a box wraps it, so a
// screen is checked for its first words only.
const (
	v1Error = "v1 vault; run claude-atlas adopt /v/old-notes --as knowledge|project"
	v1Start = "v1 vault; run claude-atlas adopt"
)

// findVault puts the cursor on the vault with that name, on its tab.
func findVault(t *testing.T, v view, name string) view {
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

func entriesOf(items []Item) []registry.Entry {
	out := make([]registry.Entry, 0, len(items))
	for _, it := range items {
		out = append(out, it.Entry)
	}
	return out
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

func typeV(v view, text string) view {
	for _, r := range text {
		v = keyV(v, string(r))
	}
	return v
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
	v := newView(sample(), Opener{}, Hooks{})
	out := v.View()
	t.Logf("\n%s", out)
	for _, want := range []string{"Atlas   Projects (3)  Knowledge (2)  Tasks (3)  Problems (1)", "A project holds tasks", "refreshed 2026-09-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if v.tab != tabProjects || len(v.boards[0].rows) != 3 {
		t.Fatalf("the Projects tab lists the projects: tab=%d rows=%d", v.tab, len(v.boards[0].rows))
	}
	v = pressV(v, tea.KeyRight)
	if v.tab != tabKnowledge || !strings.Contains(v.View(), "A knowledge base is a wiki") {
		t.Fatalf("right: tab=%d\n%s", v.tab, v.View())
	}
	v = pressV(v, tea.KeyRight)
	if v.tab != tabTasks || !strings.Contains(v.View(), "tasks are not available here") {
		t.Fatalf("tasks without hooks: tab=%d\n%s", v.tab, v.View())
	}
	v = pressV(v, tea.KeyRight)
	if v.tab != tabProblems || !strings.Contains(v.View(), "could not read") {
		t.Fatalf("problems: tab=%d", v.tab)
	}
	v = pressV(v, tea.KeyRight)
	if v.tab != tabProblems {
		t.Fatal("the bar does not wrap")
	}
	v = pressV(v, tea.KeyLeft, tea.KeyLeft, tea.KeyLeft, tea.KeyLeft)
	if v.tab != tabProjects {
		t.Fatalf("left stops at Projects: tab=%d", v.tab)
	}
	v = keyV(v, "T")
	if v.tab != tabTasks {
		t.Fatal("T is the Tasks tab")
	}
	clean := newView(sample()[:5], Opener{}, Hooks{})
	if strings.Contains(clean.View(), "Problems") || len(clean.tabs()) != 3 {
		t.Fatal("no problems, no Problems tab")
	}
	clean = pressV(clean, tea.KeyRight, tea.KeyRight, tea.KeyRight)
	if clean.tab != tabTasks {
		t.Fatal("the bar ends at Tasks then")
	}
}

// Every frame is exactly as tall as the screen and no wider, so the terminal never
// scrolls the tab bar out of sight on one tab and leaves it in place on another.
func TestEveryTabFillsTheScreenExactly(t *testing.T) {
	hooks := Hooks{
		Load:  func() ([]registry.Entry, error) { return entriesOf(sample()), nil },
		Tasks: func(registry.Entry) (tasks.Ledger, []string, error) { return tasks.Empty(), nil, nil },
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 120, Height: 40}, {Width: 80, Height: 24}, {Width: 60, Height: 12}} {
		v := newView(sample(), Opener{}, hooks)
		next, _ := v.Update(size)
		v = next.(view)
		for _, name := range []string{"Projects", "Knowledge", "Tasks", "Problems"} {
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
		// Expanding a vault and turning help on do not change the frame's height either.
		v = keyV(findVault(t, v, "p3"), "h")
		v = pressV(v, tea.KeyEnter)
		if got := strings.Count(v.View(), "\n") + 1; got != size.Height {
			t.Errorf("expanded with help at %dx%d is %d lines, want %d", size.Width, size.Height, got, size.Height)
		}
	}
}

func TestProjectsTabDrawsTheConnectors(t *testing.T) {
	v := newView(sample(), Opener{}, Hooks{})
	out := v.View()
	t.Logf("\n%s", out)
	for _, want := range []string{"╌╌╌╌▶ ai-ml   write · link missing", "╌╌╌╌▶ papers   read (write not granted)", "no knowledge base mounted", "touched today · 3 tasks open", "│ new ", "  itl\n", "  usc\n", "(end)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	welcome, itl, p3, usc, course := strings.Index(out, "welcome"), strings.Index(out, "  itl\n"), strings.Index(out, "p3"), strings.Index(out, "  usc\n"), strings.Index(out, "course")
	if !(welcome < itl && itl < p3 && p3 < usc && usc < course) {
		t.Fatalf("order: untagged first, then the tag groups: %d %d %d %d %d", welcome, itl, p3, usc, course)
	}
	if strings.Contains(out, "Path") {
		t.Fatal("nothing is expanded yet")
	}
}

func TestKnowledgeTabCountsThenExpands(t *testing.T) {
	v := pressV(newView(sample(), Opener{}, Hooks{}), tea.KeyRight)
	out := v.View()
	t.Logf("\n%s", out)
	for _, want := range []string{"ai-ml   open", "papers   open", "4 pages · new", "◀╌╌╌╌ 1 project"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Count(out, "◀╌╌╌╌ 1 project") != 2 || !strings.Contains(out, "1 grant stale") {
		t.Fatalf("counts:\n%s", out)
	}
	v = findVault(t, v, "papers")
	v = pressV(v, tea.KeyEnter)
	out = v.View()
	t.Logf("\n%s", out)
	for _, want := range []string{"◀╌╌╌╌ course   read", "grant  gone-0000   read", "Scope", "papers sources", "Access", "open", "Grant", "Enter collapse"} {
		if !strings.Contains(out, want) {
			t.Errorf("expanded knowledge base missing %q", want)
		}
	}
	if strings.Contains(out, "Tags") || strings.Contains(out, "i ingest") {
		t.Error("a knowledge base has no tags and no ingest")
	}
	v = pressV(v, tea.KeyEnter)
	if strings.Contains(v.View(), "Scope") || !strings.Contains(v.View(), "◀╌╌╌╌ 1 project · 1 grant stale") {
		t.Fatal("enter again collapses back to the count")
	}
}

func TestEnterExpandsEscCollapsesThenQuits(t *testing.T) {
	v := findVault(t, newView(sample(), Opener{}, Hooks{}), "p3")
	v = pressV(v, tea.KeyEnter)
	out := v.View()
	t.Logf("\n%s", out)
	for _, want := range []string{"Path", "/v/p3", "Created", "2026-09-01", "Vault check", "ok", "Last touched",
		"Open threads", "- thread", "Tasks", "3 open: 1 active", "Repositories", "atlas", "git@example.com:atlas.git", "Enter collapse"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, gone := range []string{"Mounts", "Id", "Mode", "Tags", "Pages", "Refreshed", "/code/atlas"} {
		if strings.Contains(out, gone) {
			t.Errorf("the block is short; %q belongs to show", gone)
		}
	}
	v = findVault(t, v, "welcome")
	v = pressV(v, tea.KeyEnter)
	if n := len(v.boards[0].expanded); n != 2 {
		t.Fatalf("two expanded, got %d", n)
	}
	v = pressV(v, tea.KeyEsc)
	if len(v.boards[0].expanded) != 0 || strings.Contains(v.View(), "Path") {
		t.Fatal("esc collapses everything")
	}
	_, cmd := v.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !quits(cmd) {
		t.Fatal("esc with nothing open quits")
	}
	_, cmd = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !quits(cmd) {
		t.Fatal("q quits")
	}
}

func TestFooterNamesTheTabsKeysUntilHelp(t *testing.T) {
	v := newView(sample(), Opener{}, Hooks{})
	out := v.View()
	if !strings.Contains(out, "  Enter details · n new project · h help · q quit\n") {
		t.Fatalf("projects footer:\n%s", out)
	}
	for _, hidden := range []string{"i ingest", "←→ tabs", "N new knowledge base", "a adopt", "R refresh"} {
		if strings.Contains(out, hidden) {
			t.Errorf("%q shows before h:\n%s", hidden, out)
		}
	}
	v = keyV(v, "h")
	// The hint lines wrap to the screen, so the keys are checked in pieces.
	out = stripANSI(v.View())
	for _, want := range []string{
		"↑↓ move · Enter details · o Obsidian · c Claude · i ingest", "m mounts",
		"←→ tabs · n new project · N new knowledge base · a adopt", "h hide help · q quit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	v = keyV(v, "h")
	if strings.Contains(v.View(), "i ingest") || !strings.Contains(v.View(), "h help") {
		t.Fatal("h again hides the keys")
	}
	v = findVault(t, v, "ai-ml")
	if out := v.View(); !strings.Contains(out, "Enter details · N new knowledge base · h help · q quit") || strings.Contains(out, "n new project") {
		t.Fatalf("knowledge footer:\n%s", out)
	}
	if hints := v.boardHints(); strings.Contains(hints, "ingest") || strings.Contains(hints, "repos") || !strings.Contains(hints, "m mounts · e edit") {
		t.Fatalf("a knowledge base has no ingest, tasks, or repositories: %q", hints)
	}
	v = findVault(t, v, "old-notes")
	if out := v.View(); !strings.Contains(out, "Enter details · a adopt · h help · q quit") {
		t.Fatalf("problems footer:\n%s", out)
	}
	if hints := v.boardHints(); strings.Contains(hints, "c Claude") || strings.Contains(hints, "ingest") || !strings.Contains(hints, "a adopt") || !strings.Contains(hints, "e edit") {
		t.Fatalf("a problem opens, edits, and adopts: %q", hints)
	}
	v = findVault(t, v, "welcome")
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyDown)
	if !v.boards[0].atEnd() || strings.Contains(v.View(), "Enter") || !strings.Contains(v.View(), "  n new project · h help · q quit\n") {
		t.Fatalf("end marker footer:\n%s", v.View())
	}
	v = pressV(findVault(t, v, "p3"), tea.KeyEnter)
	if !strings.Contains(v.View(), "Enter collapse · n new project") {
		t.Fatalf("expanded footer:\n%s", v.View())
	}
	// Help stays on across tabs, and the body gives up the lines the hints take.
	v = keyV(v, "h")
	withHelp := v.bodyHeight()
	v = pressV(v, tea.KeyRight)
	if !v.help || !strings.Contains(v.View(), "h hide help") {
		t.Fatalf("help across tabs: help=%v\n%s", v.help, v.View())
	}
	v = keyV(v, "h")
	if v.bodyHeight() <= withHelp {
		t.Fatalf("help off gives the body its lines back: %d vs %d", v.bodyHeight(), withHelp)
	}
}

func TestDownRevealsTheEndAndNeverWraps(t *testing.T) {
	v := newView(sample(), Opener{}, Hooks{})
	next, _ := v.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	v = next.(view)
	v = pressV(v, tea.KeyUp)
	if v.boards[0].cursor != 0 {
		t.Fatal("up at the top must stay at the top")
	}
	v = pressV(v, tea.KeyDown, tea.KeyDown)
	if v.boards[0].cursor != 2 || !strings.Contains(v.View(), "more lines") {
		t.Fatalf("last vault selected with the end still hidden: cursor=%d\n%s", v.boards[0].cursor, v.View())
	}
	v = pressV(v, tea.KeyDown)
	out := v.View()
	if !v.boards[0].atEnd() || strings.Contains(out, "more lines") || !strings.Contains(out, "(end)") {
		t.Fatalf("one more down reveals the end marker: cursor=%d\n%s", v.boards[0].cursor, out)
	}
	v = pressV(v, tea.KeyDown)
	if !v.boards[0].atEnd() {
		t.Fatal("down at the end must stay at the end")
	}
	v = pressV(v, tea.KeyEnter) // nothing to expand here
	if len(v.boards[0].expanded) != 0 {
		t.Fatal("enter on the end marker expands nothing")
	}
	v = pressV(v, tea.KeyUp)
	if v.boards[0].atEnd() || v.current() == nil || v.current().Entry.Name != "course" {
		t.Fatal("up from the end returns to the last vault")
	}
}

func TestScrollKeepsCursorVisible(t *testing.T) {
	v := newView(sample(), Opener{}, Hooks{})
	next, _ := v.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	v = next.(view)
	v = pressV(v, tea.KeyDown, tea.KeyDown)
	bd := v.boards[0]
	r := bd.rows[bd.cursor]
	if r.start < bd.offset || r.end >= bd.offset+v.bodyHeight() {
		t.Fatalf("cursor row %d-%d not within offset %d + %d", r.start, r.end, bd.offset, v.bodyHeight())
	}
	if bd.offset == 0 {
		t.Fatal("expected scrolling")
	}
}

func TestEmptyTabs(t *testing.T) {
	v := newView(nil, Opener{}, Hooks{})
	if !strings.Contains(v.View(), "no projects yet") {
		t.Fatal("empty projects message missing")
	}
	v = pressV(v, tea.KeyDown, tea.KeyEnter) // must not panic
	v = pressV(v, tea.KeyRight)
	if !strings.Contains(v.View(), "no knowledge bases yet") {
		t.Fatal("empty knowledge message missing")
	}
	v = pressV(v, tea.KeyEnter, tea.KeyUp)
	if _, cmd := v.Update(tea.KeyMsg{Type: tea.KeyEsc}); !quits(cmd) {
		t.Fatal("esc on an empty tab quits")
	}
}

func TestAWriteLandsOnTheVaultsTab(t *testing.T) {
	entries := entriesOf(sample())
	fresh := registry.Entry{ID: "id-fresh", Kind: vault.Knowledge, Name: "fresh", Path: "/v/fresh", Mode: vault.Generic}
	hooks := Hooks{
		Load:    func() ([]registry.Entry, error) { return append(entries, fresh), nil },
		Refresh: func() error { return nil },
	}
	v := newView(sample(), Opener{}, hooks)
	cmd := v.wrote("/v/fresh", "created fresh")
	if !v.changed || v.status != "created fresh" || cmd == nil {
		t.Fatalf("wrote: changed=%v status=%q", v.changed, v.status)
	}
	v = runCmd(v, cmd)
	if v.tab != tabKnowledge || v.current() == nil || v.current().Entry.Name != "fresh" || v.status != "refreshed" {
		t.Fatalf("after the refresh: tab=%d current=%v status=%q", v.tab, v.current(), v.status)
	}
	if !strings.Contains(v.View(), "Knowledge (3)") {
		t.Fatalf("the count follows:\n%s", v.View())
	}
}

type fakeOpener struct {
	registered      map[string]bool
	running         bool
	opened          []string
	registeredCalls []string
}

func (f *fakeOpener) opener() Opener {
	return Opener{
		Status: func(vault string) (bool, bool, error) { return f.registered[vault], f.running, nil },
		Open:   func(vault string) error { f.opened = append(f.opened, vault); return nil },
		RegisterAndOpen: func(vault string) error {
			f.registeredCalls = append(f.registeredCalls, vault)
			f.opened = append(f.opened, vault)
			return nil
		},
	}
}

func TestOpenRegisteredVaultDirectly(t *testing.T) {
	f := &fakeOpener{registered: map[string]bool{"/v/p3": true}}
	v := findVault(t, newView(sample(), f.opener(), Hooks{}), "p3")
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	v = next.(view)
	if v.busy == "" || cmd == nil {
		t.Fatalf("expected a background open, busy=%q", v.busy)
	}
	v = runCmd(v, cmd)
	if len(f.opened) != 1 || f.opened[0] != "/v/p3" || len(f.registeredCalls) != 0 || !strings.Contains(v.View(), "opened p3 in Obsidian") {
		t.Fatalf("opened=%v registered=%v\n%s", f.opened, f.registeredCalls, v.View())
	}
}

func TestOpenUnknownVaultAsksThenRegisters(t *testing.T) {
	f := &fakeOpener{registered: map[string]bool{}, running: true}
	v := newView(sample(), f.opener(), Hooks{}) // the cursor starts on welcome
	next, _ := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	v = next.(view)
	if v.ask == nil || !strings.Contains(v.View(), "quit and relaunch") {
		t.Fatalf("expected a confirmation\n%s", v.View())
	}
	next, _ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	v = next.(view)
	if v.ask != nil || len(f.registeredCalls) != 0 {
		t.Fatal("n should cancel without registering")
	}
	next, _ = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	v = next.(view)
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	v = next.(view)
	if !strings.Contains(v.busy, "restarting Obsidian") {
		t.Fatalf("busy=%q", v.busy)
	}
	v = runCmd(v, cmd)
	if len(f.registeredCalls) != 1 || f.registeredCalls[0] != "/v/welcome" || v.busy != "" {
		t.Fatalf("registered=%v busy=%q", f.registeredCalls, v.busy)
	}
}

func TestOpenFromAnExpandedVault(t *testing.T) {
	f := &fakeOpener{registered: map[string]bool{"/v/p3": true}}
	v := pressV(findVault(t, newView(sample(), f.opener(), Hooks{}), "p3"), tea.KeyEnter)
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	v = runCmd(next.(view), cmd)
	if len(f.opened) != 1 || !v.boards[0].expanded["/v/p3"] {
		t.Fatalf("opened=%v expanded=%v", f.opened, v.boards[0].expanded)
	}
}

func TestClaudeKeyHandsOffTheTerminal(t *testing.T) {
	var got string
	op := Opener{Claude: func(vault, prompt string) (*exec.Cmd, error) { got = vault; return exec.Command("true"), nil }}
	v := findVault(t, newView(sample(), op, Hooks{}), "p3")
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	v = next.(view)
	if got != "/v/p3" || cmd == nil || v.errMsg != "" {
		t.Fatalf("vault=%q cmd=%v err=%q", got, cmd, v.errMsg)
	}
	next, _ = v.Update(claudeDoneMsg{name: "p3"})
	if !strings.Contains(next.(view).View(), "back from Claude Code in p3") {
		t.Fatal("status after return missing")
	}
	none := keyV(newView(sample(), Opener{}, Hooks{}), "c")
	if none.errMsg == "" {
		t.Fatal("missing launcher should report an error")
	}
	bad := keyV(findVault(t, newView(sample(), op, Hooks{}), "old-notes"), "c")
	if bad.errMsg != v1Error {
		t.Fatalf("c on a problem names the problem: %q", bad.errMsg)
	}
}

func TestNewAndAdoptFromTheTabs(t *testing.T) {
	dir := t.TempDir()
	var got []AddVault
	hooks := Hooks{
		Load:      func() ([]registry.Entry, error) { return entriesOf(sample()), nil },
		Create:    func(c AddVault) (string, error) { got = append(got, c); return c.Path, nil },
		VaultsDir: dir,
	}
	v := newView(sample(), Opener{}, hooks)
	v = keyV(v, "n")
	if v.add == nil || v.add.adopting || v.add.kind != vault.Project || !strings.Contains(v.View(), "Add a vault") {
		t.Fatalf("n should open the add screen:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEnter) // kind: project
	v = typeV(v, "Sensor Triage")
	v = pressV(v, tea.KeyEnter)               // name
	v = pressV(v, tea.KeyRight, tea.KeyEnter) // mode: lyt
	v = typeV(v, "usc, fall")
	v = pressV(v, tea.KeyEnter) // tags
	// The mount step offers the knowledge bases the atlas holds; none is the default.
	if v.add.step() != stepMount || len(v.add.kbs) != 2 || v.add.mountChoice() != nil {
		t.Fatalf("mount step: step=%d kbs=%d", v.add.step(), len(v.add.kbs))
	}
	if out := v.View(); !strings.Contains(out, "Mounts") || !strings.Contains(out, "◂ none ▸") {
		t.Fatalf("mount step:\n%s", out)
	}
	v = pressV(v, tea.KeyLeft) // the last knowledge base listed
	if kb := v.add.mountChoice(); kb == nil || kb.Name != "ai-ml" {
		t.Fatalf("left should choose a knowledge base: %+v", kb)
	}
	if out := v.View(); !strings.Contains(out, "mounted at kb/ai-ml with write access") {
		t.Fatalf("mount hint:\n%s", out)
	}
	v = pressV(v, tea.KeyEnter) // mounts: ai-ml
	v = pressV(v, tea.KeyEnter) // path: the default
	v = pressV(v, tea.KeyEnter) // confirm
	want := filepath.Join(dir, "projects", "Sensor Triage")
	if v.add != nil || len(got) != 1 {
		t.Fatalf("create: add=%v got=%+v", v.add, got)
	}
	if c := got[0]; c.Kind != vault.Project || c.Name != "Sensor Triage" || c.Mode != "lyt" || c.Adopt ||
		c.Path != want || strings.Join(c.Tags, ",") != "usc,fall" || c.MountID != "id-ai-ml" {
		t.Fatalf("create: %+v, want path %s", c, want)
	}
	if !v.changed || v.status != "created Sensor Triage" {
		t.Fatalf("status %q changed %v", v.status, v.changed)
	}
	// Adopt an existing Obsidian folder.
	old := filepath.Join(t.TempDir(), "Old Notes")
	os.MkdirAll(filepath.Join(old, ".obsidian"), 0o755)
	v = keyV(v, "a")
	if v.add == nil || !v.add.adopting || !strings.Contains(v.View(), "Adopt a vault") || v.add.path.value() != "" {
		t.Fatalf("a should open the adopt screen with no path:\n%s", v.View())
	}
	v = typeV(v, t.TempDir())
	v = pressV(v, tea.KeyEnter)
	if v.add.step() != stepPath || v.add.err == "" {
		t.Fatalf("a plain directory is not adoptable: step=%d err=%q", v.add.step(), v.add.err)
	}
	v.add.path.setValue(old)
	v = pressV(v, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	if v.add != nil || len(got) != 2 || !got[1].Adopt || got[1].Path != old || got[1].Name != "Old Notes" || got[1].Kind != vault.Project {
		t.Fatalf("adopt: add=%v got=%+v", v.add, got)
	}
	if v.status != "adopted Old Notes" {
		t.Fatalf("status %q", v.status)
	}
	// Esc on the first step cancels without creating anything.
	v = keyV(v, "n")
	v = pressV(v, tea.KeyEsc)
	if v.add != nil || len(got) != 2 {
		t.Fatal("esc should cancel the add screen")
	}
	none := keyV(newView(sample(), Opener{}, Hooks{}), "n")
	if none.add != nil || !strings.Contains(none.errMsg, "not available") {
		t.Fatal("n without hooks reports why")
	}
}

func TestNewKnowledgeKeyOpensTheAddScreenWithKindSet(t *testing.T) {
	dir := t.TempDir()
	var got []AddVault
	hooks := Hooks{
		Load:      func() ([]registry.Entry, error) { return entriesOf(sample()), nil },
		Create:    func(c AddVault) (string, error) { got = append(got, c); return c.Path, nil },
		VaultsDir: dir,
	}
	v := newView(sample(), Opener{}, hooks)
	v = keyV(v, "N")
	if v.add == nil || v.add.kind != vault.Knowledge {
		t.Fatalf("N should open the add screen on knowledge: %+v", v.add)
	}
	v = pressV(v, tea.KeyEnter) // kind: knowledge
	v = typeV(v, "ai-ml")
	v = pressV(v, tea.KeyEnter, tea.KeyEnter) // name, mode: generic
	v = typeV(v, "Papers on retrieval.")
	v = pressV(v, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter) // scope, path, confirm
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if c := got[0]; c.Kind != vault.Knowledge || c.Scope != "Papers on retrieval." || len(c.Tags) != 0 ||
		c.Path != filepath.Join(dir, "knowledge", "ai-ml") {
		t.Fatalf("knowledge: %+v", c)
	}
}

func TestRefreshKey(t *testing.T) {
	calls := 0
	hooks := Hooks{
		Load:    func() ([]registry.Entry, error) { return entriesOf(sample()), nil },
		Refresh: func() error { calls++; return nil },
	}
	v := findVault(t, newView(sample(), Opener{}, hooks), "p3")
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	v = next.(view)
	if cmd == nil || v.busy == "" {
		t.Fatalf("R should start a background refresh: busy=%q", v.busy)
	}
	next, _ = v.Update(cmd())
	v = next.(view)
	if calls != 1 || v.busy != "" || v.status != "refreshed" || v.errMsg != "" {
		t.Fatalf("after refresh: calls=%d busy=%q status=%q err=%q", calls, v.busy, v.status, v.errMsg)
	}
	if v.tab != tabProjects || v.current() == nil || v.current().Entry.Name != "p3" {
		t.Fatalf("a refresh keeps the tab and the cursor: tab=%d current=%v", v.tab, v.current())
	}
	v = pressV(v, tea.KeyEnter) // expand, then refresh from there
	next, cmd = v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	next, _ = next.(view).Update(cmd())
	if v = next.(view); !v.boards[0].expanded["/v/p3"] || !strings.Contains(v.View(), "Path") {
		t.Fatal("a refresh keeps the expansion")
	}
	none := keyV(newView(sample(), Opener{}, Hooks{}), "R")
	if !strings.Contains(none.errMsg, "not available") {
		t.Fatal("R without hooks reports why")
	}
}

func TestIngestFromTheTabs(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Papers")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.pdf"), []byte("a"), 0o644)
	var planned, staged []string
	var launched string
	hooks := Hooks{
		Load: func() ([]registry.Entry, error) { return entriesOf(sample()), nil },
		StagePlan: func(en registry.Entry, source string) (*capture.StagePlan, error) {
			if source == "" {
				return nil, errors.New("name a file or folder to ingest")
			}
			planned = append(planned, source)
			return &capture.StagePlan{Vault: en.Path, Sources: []string{source}, Dirs: []string{source},
				New: []capture.Staged{{From: filepath.Join(source, "a.pdf"), To: "inbox/Papers/a.pdf"}}, Unchanged: []string{"x"}}, nil
		},
		Stage: func(en registry.Entry, plan *capture.StagePlan) (*capture.StageResult, []string, error) {
			staged = append(staged, plan.New[0].To)
			return &capture.StageResult{Staged: plan.New}, plan.Dirs, nil
		},
	}
	op := Opener{Claude: func(vault, prompt string) (*exec.Cmd, error) { launched = prompt; return exec.Command("true"), nil }}
	v := findVault(t, newView(sample(), op, hooks), "p3")
	v = keyV(v, "i")
	if v.ingest == nil || !strings.Contains(v.View(), "Ingest into p3") {
		t.Fatalf("i should open the ingest screen:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEnter) // blank source is refused by the hook
	if v.ingest == nil || v.ingest.err == "" || v.ingest.step != ingestPath {
		t.Fatalf("blank source: %+v", v.ingest)
	}
	v = typeV(v, src)
	v = pressV(v, tea.KeyEnter)
	out := v.View()
	if v.ingest.step != ingestConfirm || !strings.Contains(out, "1 file → inbox/") || !strings.Contains(out, "Papers/a.pdf") || !strings.Contains(out, "1 already ingested") || !strings.Contains(out, "so a later ingest") {
		t.Fatalf("confirm step:\n%s", out)
	}
	v = pressV(v, tea.KeyEsc) // back to the path
	if v.ingest.step != ingestPath {
		t.Fatal("esc should go back to the path")
	}
	v = pressV(v, tea.KeyEnter, tea.KeyEnter) // plan again, then stage
	if v.ingest == nil || v.ingest.step != ingestLaunch || len(staged) != 1 || !strings.Contains(v.View(), "Staged") {
		t.Fatalf("launch step: ingest=%+v staged=%v", v.ingest, staged)
	}
	v = pressV(v, tea.KeyEsc) // later
	if v.ingest != nil || !strings.Contains(v.status, "1 file waiting in inbox/") || launched != "" {
		t.Fatalf("later: status=%q launched=%q changed=%v", v.status, launched, v.changed)
	}
	// Nothing new but files waiting: Enter continues to the launch step instead of closing.
	nothingNew := hooks
	nothingNew.StagePlan = func(en registry.Entry, source string) (*capture.StagePlan, error) {
		return &capture.StagePlan{Vault: en.Path, Sources: []string{source}, Unchanged: []string{"a"}, Waiting: 2}, nil
	}
	w := findVault(t, newView(sample(), op, nothingNew), "p3")
	w = keyV(w, "i")
	w = typeV(w, src)
	w = pressV(w, tea.KeyEnter)
	if !strings.Contains(w.View(), "2 files waiting") || !strings.Contains(w.View(), "continue with what is waiting") {
		t.Fatalf("waiting files should be offered:\n%s", w.View())
	}
	w = pressV(w, tea.KeyEnter)
	if w.ingest == nil || w.ingest.step != ingestLaunch || !strings.Contains(w.View(), "trust the folder") {
		t.Fatalf("should reach the launch step: %+v\n%s", w.ingest, w.View())
	}
	// Nothing new and nothing waiting: Enter closes with a note.
	nothingAtAll := hooks
	nothingAtAll.StagePlan = func(en registry.Entry, source string) (*capture.StagePlan, error) {
		return &capture.StagePlan{Vault: en.Path, Sources: []string{source}, Unchanged: []string{"a", "b"}}, nil
	}
	w = findVault(t, newView(sample(), op, nothingAtAll), "p3")
	w = keyV(w, "i")
	w = typeV(w, src)
	w = pressV(w, tea.KeyEnter, tea.KeyEnter)
	if w.ingest != nil || !strings.Contains(w.status, "nothing to ingest: 2 files already ingested") {
		t.Fatalf("nothing at all: ingest=%v status=%q", w.ingest, w.status)
	}
	// Once more, starting Claude Code this time. The cursor never left p3.
	v = keyV(v, "i")
	v = typeV(v, src)
	next, cmd := pressV(v, tea.KeyEnter, tea.KeyEnter).Update(tea.KeyMsg{Type: tea.KeyEnter})
	v = next.(view)
	if v.ingest != nil || cmd == nil || launched != claudecode.IngestPrompt {
		t.Fatalf("start now: ingest=%v cmd=%v launched=%q", v.ingest, cmd, launched)
	}
	if len(planned) != 3 {
		t.Fatalf("planned %v", planned)
	}
	none := keyV(findVault(t, newView(sample(), Opener{}, Hooks{}), "p3"), "i")
	if none.ingest != nil || !strings.Contains(none.errMsg, "not available") {
		t.Fatal("i without hooks reports why")
	}
	// A knowledge base has no inbox of its own.
	kb := keyV(findVault(t, newView(sample(), op, hooks), "ai-ml"), "i")
	if kb.ingest != nil || !strings.Contains(kb.errMsg, "knowledge base") {
		t.Fatalf("i on a knowledge base: ingest=%v err=%q", kb.ingest, kb.errMsg)
	}
}

func TestTasksTabHostsTheBoard(t *testing.T) {
	var asked []string
	hooks := Hooks{
		Load: func() ([]registry.Entry, error) { return entriesOf(sample()), nil },
		Tasks: func(e registry.Entry) (tasks.Ledger, []string, error) {
			asked = append(asked, e.Name)
			return tasks.Empty(), nil, nil
		},
	}
	v := newView(sample(), Opener{}, hooks)
	if v.tasksTab != nil {
		t.Fatal("the board waits for the first visit")
	}
	v = keyV(v, "T")
	out := v.View()
	t.Logf("\n%s", out)
	if v.tab != tabTasks || v.tasksTab == nil || !v.tasksTab.hosted || v.tasksTab.item != nil {
		t.Fatalf("T hosts the board: tab=%d board=%+v", v.tab, v.tasksTab)
	}
	for _, want := range []string{"Every project's open tasks", "no open tasks in any project", "  h help · q quit\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "open across") || strings.Contains(out, "p plant") || strings.Contains(out, "Esc back") {
		t.Errorf("the hosted board has no header and no hints of its own until h:\n%s", out)
	}
	v = keyV(v, "h")
	if out := v.View(); !strings.Contains(out, "p plant") || !strings.Contains(out, "←→ tabs · R refresh · h hide help · q quit") || strings.Contains(out, "Esc back") {
		t.Fatalf("help on the Tasks tab:\n%s", out)
	}
	v = keyV(v, "h")
	if strings.Join(asked, ",") != "welcome,p3,course" {
		t.Fatalf("the board asks the projects only: %v", asked)
	}
	v = keyV(v, "p")
	if v.tasksTab.err == "" {
		t.Fatal("p with no task says what to do")
	}
	v = pressV(v, tea.KeyEsc)
	if v.tab != tabProjects {
		t.Fatal("esc on the Tasks tab returns to Projects")
	}
	v = keyV(v, "T")
	if _, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); !quits(cmd) {
		t.Fatal("q on the Tasks tab quits")
	}
	v = pressV(v, tea.KeyLeft)
	if v.tab != tabKnowledge {
		t.Fatal("left from Tasks is Knowledge")
	}
	// A board with a task plants through the tab; a q typed into the idea is a letter.
	ledgers := map[string]*tasks.Ledger{
		"/v/p3": {Tasks: []tasks.Record{{Task: tasks.Task{ID: "task-20260901-aaaa", Path: "wiki/tasks/x.md", Title: "Fix it", Status: "active", Priority: "high"}}}},
	}
	var planted []string
	w := keyV(newView(sample(), Opener{}, taskHooks(ledgers, &planted)), "T")
	if len(w.tasksTab.rows) != 1 {
		t.Fatalf("one task: %+v", w.tasksTab.rows)
	}
	w = keyV(w, "p")
	if w.tasksTab.mode != tasksPlant {
		t.Fatal("p opens the idea field")
	}
	w = keyV(w, "q")
	if w.tasksTab.idea.Value() != "q" || w.tasksTab.mode != tasksPlant {
		t.Fatalf("q typed into the idea field: %q", w.tasksTab.idea.Value())
	}
	w = typeV(w, "uick idea")
	w = pressV(w, tea.KeyEnter)
	if len(planted) != 1 || w.tasksTab.mode != tasksList || !w.changed {
		t.Fatalf("plant through the tab: planted=%v mode=%d changed=%v", planted, w.tasksTab.mode, w.changed)
	}
}

func TestContinueATaskFromTheTasksTab(t *testing.T) {
	ledgers := map[string]*tasks.Ledger{
		"/v/p3": {Tasks: []tasks.Record{{Task: tasks.Task{ID: "task-20260901-aaaa", Path: "wiki/tasks/x.md",
			Title: "Fix it", Status: "active", Priority: "high", Workdir: "/code/p3"}}}},
	}
	var launched []string
	op := Opener{ClaudeIn: func(vault, dir, prompt string) (*exec.Cmd, error) {
		launched = append(launched, vault+"|"+dir+"|"+prompt)
		return exec.Command("true"), nil
	}}
	var planted []string
	v := keyV(newView(sample(), op, taskHooks(ledgers, &planted)), "T")
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	v = next.(view)
	want := "/v/p3|/code/p3|" + claudecode.TaskPrompt("task-20260901-aaaa")
	if cmd == nil || len(launched) != 1 || launched[0] != want {
		t.Fatalf("c on the Tasks tab: %v cmd=%v", launched, cmd)
	}
	next, _ = v.Update(claudeDoneMsg{name: "Fix it"})
	v = next.(view)
	if v.tasksTab == nil || v.tasksTab.status != "back from Claude Code" {
		t.Fatalf("the board says the session ended: %+v", v.tasksTab)
	}
}

func TestTheProblemsTabGoesWithItsLastVault(t *testing.T) {
	entries := entriesOf(sample())
	hooks := Hooks{
		Load:    func() ([]registry.Entry, error) { return entries, nil },
		Refresh: func() error { return nil },
	}
	v := findVault(t, newView(sample(), Opener{}, hooks), "old-notes")
	if v.tab != tabProblems || !strings.Contains(v.View(), "Problems") {
		t.Fatalf("the cursor starts on the Problems tab: tab=%d", v.tab)
	}
	entries = entriesOf(sample()[:5]) // the problem is adopted, so the scan no longer reports it
	next, _ := v.Update(refreshedMsg{})
	v = next.(view)
	out := v.View()
	if v.tab != tabProjects || strings.Contains(out, "Problems") {
		t.Fatalf("the tab goes away with its last vault: tab=%d\n%s", v.tab, out)
	}
}

func TestProblemsTabAdoptsAndExplains(t *testing.T) {
	var got []AddVault
	hooks := Hooks{
		Load:      func() ([]registry.Entry, error) { return entriesOf(sample()), nil },
		Create:    func(c AddVault) (string, error) { got = append(got, c); return c.Path, nil },
		VaultsDir: t.TempDir(),
	}
	v := findVault(t, newView(sample(), Opener{}, hooks), "old-notes")
	out := v.View()
	t.Logf("\n%s", out)
	if v.tab != tabProblems || !strings.Contains(out, "could not read") || !strings.Contains(out, "old-notes") || !strings.Contains(out, v1Start) {
		t.Fatalf("the Problems tab names the folder and the reason:\n%s", out)
	}
	v = pressV(v, tea.KeyEnter)
	out = v.View()
	for _, want := range []string{"Reason", "v1", "Fix", "press a to adopt it"} {
		if !strings.Contains(out, want) {
			t.Errorf("expanded problem missing %q:\n%s", want, out)
		}
	}
	v = keyV(v, "m")
	if v.mounts != nil || !strings.Contains(v.errMsg, v1Start) {
		t.Fatalf("m on a problem names the problem: err=%q", v.errMsg)
	}
	v = keyV(v, "a")
	if v.add == nil || !v.add.adopting || v.add.path.value() != "/v/old-notes" {
		t.Fatalf("a starts the adopt screen on the folder: add=%+v", v.add)
	}
	v = pressV(v, tea.KeyEsc)
	if v.add != nil || len(got) != 0 {
		t.Fatal("esc cancels")
	}
	gone := sample()
	gone[5].Entry.Reason, gone[5].Entry.Error = registry.ReasonMissing, "not found; run claude-atlas remove /v/old-notes to forget it"
	w := findVault(t, newView(gone, Opener{}, hooks), "old-notes")
	if strings.Contains(w.boardHints(), "a adopt") || !strings.Contains(pressV(w, tea.KeyEnter).View(), "press e then r to forget it") {
		t.Fatalf("a folder that is gone is forgotten, not adopted: %q", w.boardHints())
	}
	w = keyV(w, "a")
	if w.add == nil || w.add.path.value() != "" {
		t.Fatal("a on a missing folder starts the adopt screen empty")
	}
}
