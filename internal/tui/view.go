// Package tui is the atlas view: every knowledge base and project on one screen, as
// two tabs, with the keys that open one in Obsidian or start Claude Code in it. It
// lists and launches; creating and changing things is the CLI's and the session's job.
package tui

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
)

// Item is one entry in the view: what the scan found, with the state the last refresh
// derived (nil when none has run).
type Item struct {
	Entry registry.Entry
}

// Items wraps the entries the CLI loaded.
func Items(entries []registry.Entry) []Item {
	items := make([]Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, Item{Entry: e})
	}
	return items
}

// entryName is the name to show: an entry the scan could not read has only its folder.
func entryName(e registry.Entry) string {
	if e.Error != "" || e.Name == "" {
		return filepath.Base(e.Path)
	}
	return e.Name
}

// Opener connects the view to Obsidian and Claude Code without the screen doing the
// work itself. Obsidian opens a knowledge base at path. Claude runs a Claude Code
// session in a knowledge base or a project's work folder at path, holding the terminal
// until the session ends; the CLI decides how to launch in each.
type Opener struct {
	Obsidian func(path string) error
	Claude   func(path string) error
}

// now is the clock the screens use; tests may replace it.
var now = time.Now

// claudeDoneMsg reports that a Claude Code session ended and the view has the terminal back.
type claudeDoneMsg struct {
	name string
	err  error
}

// openedMsg reports the outcome of an Obsidian open that ran in the background.
type openedMsg struct {
	name string
	err  error
}

// refreshedMsg reports that a background refresh finished.
type refreshedMsg struct {
	err error
}

// launch adapts an Opener.Claude call to what Bubble Tea hands the terminal to.
type launch struct {
	run func() error
}

func (l launch) Run() error        { return l.run() }
func (launch) SetStdin(io.Reader)  {}
func (launch) SetStdout(io.Writer) {}
func (launch) SetStderr(io.Writer) {}

// tab is one screen of the tab bar.
type tab int

const (
	tabKnowledge tab = iota
	tabProjects
	tabProblems
)

var tabNames = map[tab]string{tabKnowledge: "Knowledge", tabProjects: "Projects", tabProblems: "Problems"}

// captions say what each tab holds, for a user who is new to the two kinds.
var captions = map[tab]string{
	tabKnowledge: "A knowledge base is the wiki you open and work from. Projects use it; sources enter through its inbox.",
	tabProjects:  "A project is an atlas/ folder inside your work: its tasks and phases, and the one knowledge base it uses.",
	tabProblems:  "Entries the atlas found but could not read.",
}

// empties is what a tab says when it lists nothing.
var empties = map[tab]string{
	tabKnowledge: "no knowledge bases yet; run `claude-atlas new-knowledge NAME`",
	tabProjects:  "no projects yet; run `claude-atlas init` in a work folder",
}

// boardOf is the index of the board behind a tab.
func boardOf(t tab) int { return int(t) }

// boardTab is the tab a board sits on.
func boardTab(i int) tab { return tab(i) }

type view struct {
	items   []Item
	opener  Opener
	acts    actions.Atlas
	tab     tab
	boards  [3]board // knowledge, projects, problems
	changed bool
	// plant is the one-line prompt for a new task, open while not nil, and the project
	// it plants into.
	plant     *textinput.Model
	plantInto *Item
	busy      string // message while an open or a refresh runs in the background
	status    string
	errMsg    string
	width     int
	height    int
	refreshed string
	// help shows every key in the footer; off, the footer names only the tab's keys.
	help bool
}

func newView(items []Item, opener Opener, acts actions.Atlas) view {
	v := view{items: items, opener: opener, acts: acts, width: 100, height: 40}
	v.boards = [3]board{
		newBoard(boardKnowledge, items, v.width),
		newBoard(boardProjects, items, v.width),
		newBoard(boardProblems, items, v.width),
	}
	v.stamp()
	return v
}

func (v *view) stamp() {
	v.refreshed = ""
	for _, it := range v.items {
		if s := it.Entry.State; s != nil && s.GeneratedAt > v.refreshed {
			v.refreshed = s.GeneratedAt
		}
	}
}

// board is the active tab's board.
func (v *view) board() *board { return &v.boards[boardOf(v.tab)] }

// current is the entry under the cursor; nil on the end marker or an empty board.
func (v *view) current() *Item { return v.board().current() }

// tabs lists the tabs the bar shows: Problems only while there is one.
func (v view) tabs() []tab {
	out := []tab{tabKnowledge, tabProjects}
	if len(v.boards[boardOf(tabProblems)].items) > 0 {
		out = append(out, tabProblems)
	}
	return out
}

// switchTab moves along the bar and stops at its ends.
func (v *view) switchTab(delta int) {
	tabs := v.tabs()
	at := 0
	for i, t := range tabs {
		if t == v.tab {
			at = i
		}
	}
	v.goTo(tabs[max(0, min(len(tabs)-1, at+delta))])
}

// goTo shows a tab.
func (v *view) goTo(t tab) {
	v.tab = t
	b := v.board()
	b.layout()
	b.ensureVisible(v.bodyHeight())
}

// reload re-reads the registry when the actions can, then rebuilds every board with
// the cursor on the entry at path, on its tab.
func (v *view) reload(path string) {
	if v.acts.Load != nil {
		entries, err := v.acts.Load()
		if err != nil {
			v.errMsg = err.Error()
			return
		}
		v.items = Items(entries)
		v.stamp()
	}
	v.rebuild(path)
}

// rebuild lays the boards out again over v.items.
func (v *view) rebuild(path string) {
	for i := range v.boards {
		v.boards[i].width = v.width
		v.boards[i].reload(v.items)
	}
	for i := range v.boards {
		if path != "" && v.boards[i].moveTo(path) {
			v.tab = boardTab(i)
		}
	}
	if v.tab == tabProblems && len(v.boards[boardOf(tabProblems)].items) == 0 {
		v.tab = tabKnowledge
	}
	for i := range v.boards {
		v.boards[i].layout()
	}
	v.board().ensureVisible(v.bodyHeight())
}

func (v view) Init() tea.Cmd { return nil }

// head is everything above the body: a blank line, the tab bar, the caption, a blank
// line. The caption wraps to the screen, so its height is known here and nothing below
// it shifts when the tab changes.
func (v view) head() string {
	var caption []string
	for _, line := range strings.Split(captionSt.Width(max(20, v.width-6)).Render(captions[v.tab]), "\n") {
		caption = append(caption, "    "+strings.TrimRight(line, " "))
	}
	return fmt.Sprintf("\n  %s\n%s\n\n", v.tabBar(), strings.Join(caption, "\n"))
}

// bodyHeight is how many lines the body may take: the screen without the head, the
// footer, and the line that counts what the body leaves out.
func (v view) bodyHeight() int {
	chrome := countLines(v.head()) + countLines("\n"+v.footer(v.hints()...)) + 1
	return max(5, v.height-chrome)
}

// countLines counts the screen lines a rendered block takes.
func countLines(block string) int { return strings.Count(strings.TrimSuffix(block, "\n"), "\n") + 1 }

func (v view) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width, v.height = msg.Width, msg.Height
		for i := range v.boards {
			v.boards[i].width = msg.Width
			v.boards[i].layout()
		}
		v.board().ensureVisible(v.bodyHeight())
		return v, nil
	case claudeDoneMsg:
		if msg.err != nil {
			v.errMsg = msg.err.Error()
		} else {
			v.status = "back from Claude Code in " + msg.name
		}
		// A session may have planted or finished tasks; read everything again.
		return v.refresh()
	case openedMsg:
		v.busy = ""
		if msg.err != nil {
			v.errMsg = msg.err.Error()
		} else {
			v.status = "opened " + msg.name + " in Obsidian"
		}
		return v, nil
	case refreshedMsg:
		v.busy = ""
		if msg.err != nil {
			v.errMsg = msg.err.Error()
			return v, nil
		}
		path := ""
		if it := v.current(); it != nil {
			path = it.Entry.Path
		}
		v.reload(path)
		if v.status == "" {
			v.status = "refreshed"
		}
		return v, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return v, tea.Quit
		}
		if v.plant != nil {
			return v.updatePlant(msg)
		}
		if msg.String() == "q" {
			return v, tea.Quit
		}
		if v.busy != "" {
			return v, nil
		}
		v.status, v.errMsg = "", ""
		switch msg.Type {
		case tea.KeyLeft:
			v.switchTab(-1)
			return v, nil
		case tea.KeyRight:
			v.switchTab(1)
			return v, nil
		}
		switch msg.String() {
		case "R":
			return v.refresh()
		case "h":
			v.help = !v.help
			v.board().ensureVisible(v.bodyHeight())
			return v, nil
		case "o", "c", "p":
			item := v.current()
			if item == nil {
				return v, nil
			}
			if item.Entry.Error != "" {
				v.errMsg = item.Entry.Error
				return v, nil
			}
			switch msg.String() {
			case "c":
				return v.claude(item)
			case "p":
				return v.openPlant(item)
			}
			return v.open(item)
		}
		b := v.board()
		switch msg.Type {
		case tea.KeyUp:
			b.move(-1)
		case tea.KeyDown:
			b.move(1)
		case tea.KeyEnter:
			b.toggle()
		case tea.KeyEsc:
			if !b.collapseAll() {
				return v, tea.Quit
			}
		default:
			return v, nil
		}
		b.layout()
		b.ensureVisible(v.bodyHeight())
	}
	return v, nil
}

// refreshCmd rebuilds the registry in the background; nil when no action does it.
func (v view) refreshCmd() tea.Cmd {
	if v.acts.Refresh == nil {
		return nil
	}
	fn := v.acts.Refresh
	return func() tea.Msg {
		_, err := fn()
		return refreshedMsg{err: err}
	}
}

// refresh rebuilds derived state in the background, then reloads the boards.
func (v view) refresh() (tea.Model, tea.Cmd) {
	if v.acts.Refresh == nil || v.acts.Load == nil {
		v.errMsg = "refresh is not available here"
		return v, nil
	}
	v.changed = true
	v.busy = "reading every knowledge base and project…"
	return v, v.refreshCmd()
}

// openPlant opens the one-line prompt that plants a task in a project.
func (v view) openPlant(item *Item) (tea.Model, tea.Cmd) {
	if item.Entry.Kind != registry.Project {
		v.errMsg = "a knowledge base has no tasks; plant into a project"
		return v, nil
	}
	if v.acts.Plant == nil {
		v.errMsg = "planting is not available here"
		return v, nil
	}
	in := textinput.New()
	in.Prompt = "  task for " + entryName(item.Entry) + ": "
	in.Width = max(20, v.width-lipgloss.Width(in.Prompt)-4)
	in.Focus()
	v.plant, v.plantInto = &in, item
	return v, textinput.Blink
}

// updatePlant forwards keys to the prompt; Enter plants, Esc cancels.
func (v view) updatePlant(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.plant, v.plantInto = nil, nil
		return v, nil
	case tea.KeyEnter:
		text := strings.TrimSpace(v.plant.Value())
		item := v.plantInto
		v.plant, v.plantInto = nil, nil
		if text == "" {
			return v, nil
		}
		t, err := v.acts.Plant(item.Entry, tasks.Plant{Text: text})
		if err != nil {
			v.errMsg = err.Error()
			return v, nil
		}
		v.changed = true
		v.status = fmt.Sprintf("planted %s in %s (%s)", t.Title, entryName(item.Entry), t.ID)
		if cmd := v.refreshCmd(); cmd != nil {
			v.busy = "reading every knowledge base and project…"
			return v, cmd
		}
		return v, nil
	}
	in, cmd := v.plant.Update(msg)
	v.plant = &in
	return v, cmd
}

// claude hands the terminal to a Claude Code session in the entry's folder and
// resumes after.
func (v view) claude(item *Item) (tea.Model, tea.Cmd) {
	if v.opener.Claude == nil {
		v.errMsg = "starting Claude Code is not available here"
		return v, nil
	}
	name, path, run := entryName(item.Entry), item.Entry.Path, v.opener.Claude
	return v, tea.Exec(launch{run: func() error { return run(path) }}, func(err error) tea.Msg { return claudeDoneMsg{name: name, err: err} })
}

// open starts opening a knowledge base in Obsidian in the background.
func (v view) open(item *Item) (tea.Model, tea.Cmd) {
	if item.Entry.Kind != registry.Knowledge {
		v.errMsg = "a project is not an Obsidian vault; open its knowledge base"
		return v, nil
	}
	if v.opener.Obsidian == nil {
		v.errMsg = "opening in Obsidian is not available here"
		return v, nil
	}
	name, path, fn := entryName(item.Entry), item.Entry.Path, v.opener.Obsidian
	v.busy = "opening " + name + " in Obsidian…"
	return v, func() tea.Msg { return openedMsg{name: name, err: fn(path)} }
}

// footer renders the prompt, progress, or status lines under a screen.
func (v view) footer(hints ...string) string {
	switch {
	case v.plant != nil:
		return v.plant.View() + "\n" + v.wrapped(dim, "Enter plant · Esc cancel")
	case v.busy != "":
		return "  " + okSt.Render(v.busy) + "\n"
	}
	out := ""
	for _, line := range hints {
		out += v.wrapped(dim, line)
	}
	if v.status != "" {
		out += v.wrapped(okSt, v.status)
	}
	if v.errMsg != "" {
		out += v.wrapped(errSt, v.errMsg)
	}
	return out
}

// wrapped is one footer line, broken to the screen so it never runs past the last row.
// Wrapping here keeps the whole message and keeps the frame's height countable.
func (v view) wrapped(style lipgloss.Style, text string) string {
	out := ""
	for _, line := range strings.Split(style.Width(max(20, v.width-4)).Render(text), "\n") {
		out += "  " + strings.TrimRight(line, " ") + "\n"
	}
	return out
}

// tabColor is the color a tab's box is filled with while it is the active one.
func tabColor(t tab) lipgloss.Color {
	switch t {
	case tabKnowledge:
		return knowledgeColor
	case tabProblems:
		return lipgloss.Color("9")
	}
	return projectColor
}

// tabRow renders the tab boxes, with their counts when there is room for them.
func (v view) tabRow(counts bool) string {
	var parts []string
	for _, t := range v.tabs() {
		text := tabNames[t]
		if counts {
			text += fmt.Sprintf(" (%d)", len(v.boards[boardOf(t)].items))
		}
		parts = append(parts, tabStyle(tabColor(t), t == v.tab).Render(text))
	}
	return strings.Join(parts, "")
}

// tabBar is the first line: every tab with its count in parentheses, the active one
// filled with its color, and the refresh stamp at the right. It never wraps: a screen
// too narrow for all of that loses the stamp, then the name, then the counts.
func (v view) tabBar() string {
	room := v.width - 4
	fits := func(s string) bool { return lipgloss.Width(s) <= room }

	full := title.Render("Atlas") + "  " + v.tabRow(true)
	stamp := "not refreshed yet"
	if t, err := time.Parse("2006-01-02T15:04:05Z", v.refreshed); err == nil {
		stamp = "refreshed " + t.Local().Format("2006-01-02 15:04")
	}
	if pad := room - lipgloss.Width(full) - lipgloss.Width(stamp); pad >= 3 {
		return full + strings.Repeat(" ", pad) + dim.Render(stamp)
	}
	switch {
	case fits(full):
		return full
	case fits(v.tabRow(true)):
		return v.tabRow(true)
	}
	return v.tabRow(false)
}

// fit makes a frame exactly as tall as the screen, so the tab bar sits on the same row
// whatever the tab holds. A frame the terminal has to scroll moves everything above the
// fold out of sight; a short one leaves the top where it is.
func (v view) fit(frame string) string {
	out := strings.Split(strings.TrimSuffix(frame, "\n"), "\n")
	if v.height <= 0 {
		return strings.Join(out, "\n")
	}
	for len(out) < v.height {
		out = append(out, "")
	}
	if len(out) > v.height {
		out = out[:v.height]
	}
	return strings.Join(out, "\n")
}

func (v view) View() string {
	var b strings.Builder
	b.WriteString(v.head())
	bd := v.board()
	body := v.bodyHeight()
	if len(bd.items) == 0 {
		b.WriteString("  " + dim.Render(empties[v.tab]) + "\n")
		body--
	}
	lines, more := bd.window(body)
	for _, line := range lines {
		b.WriteString("  " + line + "\n")
	}
	if more > 0 {
		b.WriteString("  " + dim.Render(fmt.Sprintf("… %d more lines", more)) + "\n")
	}
	b.WriteString("\n" + v.footer(v.hints()...))
	return v.fit(b.String())
}

// hints is the footer. With help off it names the tab's own keys: Enter and the launch
// keys for the entry under the cursor, h, and q. With help on it names every key on
// two lines.
func (v view) hints() []string {
	quit := "h help · q quit"
	if v.help {
		quit = "h hide help · q quit"
	}
	if v.help {
		return []string{v.boardHints(), "←→ tabs · R refresh · " + quit}
	}
	bd := v.board()
	var parts []string
	if it := bd.current(); it != nil {
		parts = append(parts, enterHint(bd, it), entryKeys(it.Entry))
	}
	return []string{strings.Join(append(parts, quit), " · ")}
}

// enterHint says what Enter does to the entry under the cursor.
func enterHint(bd *board, it *Item) string {
	if bd.expanded[it.Entry.Path] {
		return "Enter collapse"
	}
	return "Enter details"
}

// entryKeys lists the launch keys for one entry.
func entryKeys(e registry.Entry) string {
	switch {
	case e.Error != "":
		return "R refresh"
	case e.Kind == registry.Knowledge:
		return "o Obsidian · c Claude"
	}
	return "c Claude · p plant"
}

// boardHints lists the keys for the entry under the cursor.
func (v view) boardHints() string {
	hints := "↑↓ move"
	bd := v.board()
	it := bd.current()
	if it == nil {
		return hints
	}
	return hints + " · " + enterHint(bd, it) + " · " + entryKeys(it.Entry)
}

// RunView shows the atlas until the user quits. It reports whether anything changed: a
// plant or a refresh happened, so the caller reads the registry again.
func RunView(items []Item, opener Opener, acts actions.Atlas) (bool, error) {
	final, err := tea.NewProgram(newView(items, opener, acts), tea.WithAltScreen()).Run()
	if err != nil {
		return false, fmt.Errorf("interactive screen failed: %w", err)
	}
	return final.(view).changed, nil
}
