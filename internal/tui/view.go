package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// Item is one vault in the view: what the scan found, with the state the last refresh
// derived (nil when none has run).
type Item struct {
	Entry registry.Entry
}

// Items wraps the entries the hooks loaded.
func Items(entries []registry.Entry) []Item {
	items := make([]Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, Item{Entry: e})
	}
	return items
}

// entryName is the name to show: a vault the scan could not read has only its folder.
func entryName(e registry.Entry) string {
	if e.Error != "" || e.Name == "" {
		return filepath.Base(e.Path)
	}
	return e.Name
}

// Opener connects the view to Obsidian and Claude Code without the screen doing the work itself.
type Opener struct {
	Status          func(vault string) (registered, running bool, err error)
	Open            func(vault string) error
	RegisterAndOpen func(vault string) error
	// Claude builds the Claude Code process for a vault, with prompt as the first
	// message when not empty; the view hands it the terminal. ClaudeIn starts it in
	// another folder, such as a task's workdir, with the vault still selected.
	Claude   func(vault, prompt string) (*exec.Cmd, error)
	ClaudeIn func(vault, dir, prompt string) (*exec.Cmd, error)
	// OpenPath opens one page of a vault in Obsidian.
	OpenPath func(path string) error
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

// tab is one screen of the tab bar.
type tab int

const (
	tabProjects tab = iota
	tabKnowledge
	tabTasks
	tabProblems
)

var tabNames = map[tab]string{tabProjects: "Projects", tabKnowledge: "Knowledge", tabTasks: "Tasks", tabProblems: "Problems"}

// captions say what each tab holds, for a user who is new to the two kinds.
var captions = map[tab]string{
	tabProjects:  "A project holds tasks, questions, and an inbox. It mounts the knowledge bases it reads and writes.",
	tabKnowledge: "A knowledge base is a wiki. Projects mount it, and sources reach it through a project's inbox. A guarded one grants write access per project.",
	tabTasks:     "Every project's open tasks.",
	tabProblems:  "Vaults the atlas found but could not read.",
}

// empties is what a tab says when it lists nothing.
var empties = map[tab]string{
	tabProjects:  "no projects yet; press n to make one, or run `claude-atlas new-project NAME`",
	tabKnowledge: "no knowledge bases yet; press N to make one, or run `claude-atlas new-knowledge NAME`",
}

// boardOf is the index of the board behind a tab; the Tasks tab has none and maps to 0.
func boardOf(t tab) int {
	switch t {
	case tabKnowledge:
		return 1
	case tabProblems:
		return 2
	}
	return 0
}

// boardTab is the tab a board sits on.
func boardTab(i int) tab {
	switch i {
	case 1:
		return tabKnowledge
	case 2:
		return tabProblems
	}
	return tabProjects
}

type view struct {
	items     []Item
	opener    Opener
	hooks     Hooks
	tab       tab
	boards    [3]board     // projects, knowledge, problems
	tasksTab  *tasksScreen // the hosted board, built on the first visit to the Tasks tab
	edit      *editor
	add       *model // the add-vault or adopt screen while open
	ingest    *ingestScreen
	links     *linksScreen
	mounts    *mountsScreen
	cluster   *clusterScreen
	tasks     *tasksScreen
	changed   bool
	ask       *Item  // vault awaiting a register-and-open confirmation
	askRun    bool   // Obsidian was running when we asked, so it will restart
	busy      string // message while an open runs in the background
	status    string
	errMsg    string
	width     int
	height    int
	refreshed string
	// focus is the vault the next refresh should land on, after a write.
	focus string
	// help shows every key in the footer; off, the footer names only the tab's keys.
	help bool
}

func newView(items []Item, opener Opener, hooks Hooks) view {
	v := view{items: items, opener: opener, hooks: hooks, width: 100, height: 40}
	v.boards = [3]board{
		newBoard(boardProjects, items, v.width),
		newBoard(boardKnowledge, items, v.width),
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

// board is the active tab's board; nil on the Tasks tab.
func (v *view) board() *board {
	if v.tab == tabTasks {
		return nil
	}
	return &v.boards[boardOf(v.tab)]
}

// current is the vault under the cursor; nil on the end marker, an empty board, or the
// Tasks tab.
func (v *view) current() *Item {
	if b := v.board(); b != nil {
		return b.current()
	}
	return nil
}

// tabs lists the tabs the bar shows: Problems only while there is one.
func (v view) tabs() []tab {
	out := []tab{tabProjects, tabKnowledge, tabTasks}
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

// goTo shows a tab. The first visit to Tasks builds the board, when tasks are available.
func (v *view) goTo(t tab) {
	v.tab = t
	if t == tabTasks && v.tasksTab == nil && v.hooks.Tasks != nil {
		s := newTasks(v.hooks, v.opener, nil, v.items, v.width)
		s.hosted = true
		s.quiet = !v.help
		s.avail = v.bodyHeight()
		s.ensureVisible()
		v.tasksTab = &s
	}
	if b := v.board(); b != nil {
		b.layout()
		b.ensureVisible(v.bodyHeight())
	}
}

// reload re-reads the registry when the hooks can, then rebuilds every board with the
// cursor on the vault at path, on its tab.
func (v *view) reload(path string) {
	if v.hooks.Load != nil {
		entries, err := v.hooks.Load()
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
	if v.tasksTab != nil {
		v.tasksTab.reload(v.items)
	}
	for i := range v.boards {
		if path != "" && v.boards[i].moveTo(path) {
			v.tab = boardTab(i)
		}
	}
	if v.tab == tabProblems && len(v.boards[boardOf(tabProblems)].items) == 0 {
		v.tab = tabProjects
	}
	for i := range v.boards {
		v.boards[i].layout()
	}
	if b := v.board(); b != nil {
		b.ensureVisible(v.bodyHeight())
	}
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

// lines counts the screen lines a rendered block takes.
func countLines(block string) int { return strings.Count(strings.TrimSuffix(block, "\n"), "\n") + 1 }

func (v view) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width, v.height = msg.Width, msg.Height
		for i := range v.boards {
			v.boards[i].width = msg.Width
			v.boards[i].layout()
		}
		if v.tasksTab != nil {
			v.tasksTab.width = msg.Width
			v.tasksTab.avail = v.bodyHeight()
			v.tasksTab.ensureVisible()
		}
		if v.links != nil {
			v.links.width = msg.Width
		}
		if v.mounts != nil {
			v.mounts.width = msg.Width
		}
		if v.cluster != nil {
			v.cluster.width = msg.Width
		}
		if v.tasks != nil {
			v.tasks.width = msg.Width
			v.tasks.avail = v.bodyHeight()
			v.tasks.ensureVisible()
		}
		if b := v.board(); b != nil {
			b.ensureVisible(v.bodyHeight())
		}
		return v, nil
	case claudeDoneMsg:
		if msg.err != nil {
			v.errMsg = msg.err.Error()
		} else {
			v.status = "back from Claude Code in " + msg.name
		}
		if v.tasks != nil {
			v.tasks.reload(v.items)
			v.tasks.status = "back from Claude Code"
			return v, v.refreshCmd()
		}
		if v.tab == tabTasks && v.tasksTab != nil {
			v.tasksTab.reload(v.items)
			v.tasksTab.status = "back from Claude Code"
			return v, v.refreshCmd()
		}
		return v, nil
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
		path := v.focus
		v.focus = ""
		if path == "" {
			if it := v.current(); it != nil {
				path = it.Entry.Path
			}
		}
		v.reload(path)
		v.status = "refreshed"
		if v.links != nil {
			v.links.reload(v.items)
			v.links.status = "refreshed"
		}
		if v.mounts != nil {
			v.mounts.reload(v.items)
			v.mounts.status = "refreshed"
		}
		if v.cluster != nil {
			v.cluster.reload(v.items)
			v.cluster.status = "refreshed"
		}
		if v.tasks != nil {
			v.tasks.reload(v.items)
			v.tasks.status = "refreshed"
		}
		if v.tasksTab != nil {
			v.tasksTab.status = "refreshed"
		}
		return v, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return v, tea.Quit
		}
		if v.edit != nil {
			return v.updateEdit(msg)
		}
		if v.add != nil {
			return v.updateAdd(msg)
		}
		if v.ingest != nil {
			return v.updateIngest(msg)
		}
		if v.links != nil {
			return v.updateLinks(msg)
		}
		if v.mounts != nil {
			return v.updateMounts(msg)
		}
		if v.cluster != nil {
			return v.updateCluster(msg)
		}
		if v.tasks != nil {
			return v.updateTasks(msg)
		}
		// The board's idea field takes every key while it is open, q included.
		if v.tab == tabTasks && v.tasksTab != nil && v.tasksTab.mode != tasksList {
			return v.updateTasksTab(msg)
		}
		if msg.String() == "q" {
			return v, tea.Quit
		}
		if v.busy != "" {
			return v, nil
		}
		if v.ask != nil {
			switch strings.ToLower(msg.String()) {
			case "y":
				item := v.ask
				v.ask = nil
				return v.openAsync(item, true)
			case "n", "esc":
				v.ask = nil
			}
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
		case "n":
			return v.openAdd(newModel(v.hooks.VaultsDir, vault.Project).withKnowledge(v.items))
		case "N":
			return v.openAdd(newModel(v.hooks.VaultsDir, vault.Knowledge).withKnowledge(v.items))
		case "C":
			return v.openAdd(newClusterModel(v.hooks.VaultsDir).withKnowledge(v.items))
		case "a":
			return v.openAdd(v.adoptModel())
		case "R":
			return v.refresh()
		case "T":
			v.goTo(tabTasks)
			return v, nil
		case "h":
			v.help = !v.help
			if v.tasksTab != nil {
				v.tasksTab.quiet = !v.help
			}
			if b := v.board(); b != nil {
				b.ensureVisible(v.bodyHeight())
			}
			return v, nil
		}
		if v.tab == tabTasks {
			return v.updateTasksTab(msg)
		}
		if key := msg.String(); key == "o" || key == "c" || key == "e" || key == "i" || key == "l" || key == "m" || key == "M" || key == "t" {
			item := v.current()
			if item == nil {
				return v, nil
			}
			if item.Entry.Error != "" && key != "o" && key != "e" {
				v.errMsg = item.Entry.Error
				return v, nil
			}
			switch key {
			case "c":
				return v.claude(item, "")
			case "e":
				return v.openEditor(item)
			case "l":
				return v.openLinks(item)
			case "m":
				return v.openMounts(item)
			case "M":
				return v.openCluster(item)
			case "t":
				return v.openTasks(item)
			case "i":
				return v.openIngest(item)
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

// adoptModel is the adopt screen. On the Problems tab it starts on the folder under the
// cursor, unless that folder is gone.
func (v *view) adoptModel() model {
	m := newAdoptModel()
	if it := v.current(); v.tab == tabProblems && it != nil && it.Entry.Reason != registry.ReasonMissing {
		m.path.setValue(it.Entry.Path)
	}
	return m
}

// refreshCmd rebuilds the registry in the background; nil when no hook does it.
func (v view) refreshCmd() tea.Cmd {
	if v.hooks.Refresh == nil {
		return nil
	}
	fn := v.hooks.Refresh
	return func() tea.Msg { return refreshedMsg{err: fn()} }
}

// openAdd starts the add-vault screen, or the adopt screen, in place.
func (v view) openAdd(m model) (tea.Model, tea.Cmd) {
	if v.hooks.Create == nil || v.hooks.Load == nil {
		v.errMsg = "creating vaults is not available here"
		return v, nil
	}
	v.add = &m
	return v, m.Init()
}

// updateAdd forwards keys to the add screen and creates the vault once confirmed.
func (v view) updateAdd(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := v.add.Update(msg)
	m := next.(model)
	if m.cancelled {
		v.add = nil
		return v, nil
	}
	choice := m.result()
	if choice == nil {
		v.add = &m
		return v, cmd
	}
	v.add = nil
	path, err := v.hooks.Create(*choice)
	if err != nil {
		v.errMsg = err.Error()
		return v, nil
	}
	verb := "created "
	if choice.Adopt {
		verb = "adopted "
	}
	cmd = v.wrote(path, verb+choice.Name)
	v.reload(path)
	// A cluster is a knowledge base until it has a member, so the members screen opens on
	// the new vault: that is where a member is added, and it says what a cluster is.
	if choice.Cluster {
		if it := v.current(); it != nil && it.Entry.Path == path {
			next, open := v.openCluster(it)
			return next, tea.Batch(cmd, open)
		}
	}
	return v, cmd
}

// wrote records a write: the boards reload now, and a background refresh brings the
// derived state up to date and lands the cursor back on this vault.
func (v *view) wrote(path, status string) tea.Cmd {
	v.changed = true
	v.status = status
	v.focus = path
	return v.refreshCmd()
}

// refresh rebuilds derived state in the background, then reloads the boards.
func (v view) refresh() (tea.Model, tea.Cmd) {
	if v.hooks.Refresh == nil || v.hooks.Load == nil {
		v.errMsg = "refresh is not available here"
		return v, nil
	}
	v.busy = "reading every vault…"
	return v, v.refreshCmd()
}

// openEditor starts editing a vault's identity file in place.
func (v view) openEditor(item *Item) (tea.Model, tea.Cmd) {
	if v.hooks.Load == nil || v.hooks.Edit == nil {
		v.errMsg = "editing is not available here"
		return v, nil
	}
	ed := newEditor(v.hooks, item.Entry)
	v.edit = &ed
	return v, nil
}

// updateEdit forwards keys to the editor and folds its outcome back into the view.
func (v view) updateEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	ed, cmd := v.edit.update(msg)
	path := ed.entry.Path
	switch ed.outcome {
	case editOpen:
		v.edit = &ed
		return v, cmd
	case editSaved:
		// A rename moves the folder, so follow the vault to its new path; the cursor
		// and the expanded block stay on it.
		if ed.path != "" && ed.path != path {
			for i := range v.boards {
				v.boards[i].rekey(path, ed.path)
			}
			path = ed.path
		}
		cmd = v.wrote(path, "saved "+ed.draft.Name)
		v.reload(path)
	case editRemoved:
		cmd = v.wrote("", fmt.Sprintf("forgot %s; the vault is still on disk", ed.entry.Name))
		v.reload("")
	}
	v.edit = nil
	return v, cmd
}

// openLinks shows a project's mounted repositories.
func (v view) openLinks(item *Item) (tea.Model, tea.Cmd) {
	if v.hooks.Load == nil || v.hooks.AddRepo == nil {
		v.errMsg = "repositories are not available here"
		return v, nil
	}
	if item.Entry.Kind != vault.Project {
		v.errMsg = "a knowledge base has no repositories"
		return v, nil
	}
	s := newLinks(v.hooks, item.Entry, v.width)
	v.links = &s
	return v, nil
}

// updateLinks forwards keys to the repositories screen; after an action it refreshes
// every vault in the background so the facts catch up, and keeps the screen open.
func (v view) updateLinks(msg tea.Msg) (tea.Model, tea.Cmd) {
	s, cmd := v.links.update(msg)
	if s.closed {
		v.links = nil
		v.reload(s.entry.Path)
		return v, nil
	}
	v.links = &s
	if s.changed {
		v.links.changed = false
		v.changed = true
		v.focus = s.entry.Path
		v.reload(s.entry.Path)
		v.links.reload(v.items)
		if refreshCmd := v.refreshCmd(); refreshCmd != nil {
			v.links.status += " · refreshing…"
			return v, tea.Batch(cmd, refreshCmd)
		}
	}
	return v, cmd
}

// openMounts shows what a project mounts, or which projects mount a knowledge base.
func (v view) openMounts(item *Item) (tea.Model, tea.Cmd) {
	if item.Entry.Error != "" {
		v.errMsg = item.Entry.Error
		return v, nil
	}
	ready := v.hooks.Mount != nil
	if item.Entry.Kind == vault.Knowledge {
		ready = v.hooks.Grant != nil
	}
	if v.hooks.Load == nil || !ready {
		v.errMsg = "mounts are not available here"
		return v, nil
	}
	s := newMounts(v.hooks, item.Entry, v.items, v.width)
	v.mounts = &s
	return v, nil
}

// updateMounts forwards keys to the mounts screen; after an action it refreshes every
// vault in the background so the facts catch up, and keeps the screen open.
func (v view) updateMounts(msg tea.Msg) (tea.Model, tea.Cmd) {
	s, cmd := v.mounts.update(msg)
	if s.closed {
		v.mounts = nil
		v.reload(s.entry.Path)
		return v, nil
	}
	v.mounts = &s
	if s.changed {
		v.mounts.changed = false
		v.changed = true
		v.focus = s.entry.Path
		v.reload(s.entry.Path)
		v.mounts.reload(v.items)
		if refreshCmd := v.refreshCmd(); refreshCmd != nil {
			v.mounts.status += " · refreshing…"
			return v, tea.Batch(cmd, refreshCmd)
		}
	}
	return v, cmd
}

// openCluster shows the knowledge bases a knowledge base gathers.
func (v view) openCluster(item *Item) (tea.Model, tea.Cmd) {
	if item.Entry.Error != "" {
		v.errMsg = item.Entry.Error
		return v, nil
	}
	if item.Entry.Kind != vault.Knowledge {
		v.errMsg = "a project holds no members"
		return v, nil
	}
	if v.hooks.Load == nil || v.hooks.AddMember == nil || v.hooks.RemoveMember == nil {
		v.errMsg = "members are not available here"
		return v, nil
	}
	s := newCluster(v.hooks, item.Entry, v.items, v.width)
	v.cluster = &s
	return v, nil
}

// updateCluster forwards keys to the members screen; after an action it refreshes every
// vault in the background so the projects that mount the cluster catch up, and keeps the
// screen open.
func (v view) updateCluster(msg tea.Msg) (tea.Model, tea.Cmd) {
	s, cmd := v.cluster.update(msg)
	if s.closed {
		v.cluster = nil
		v.reload(s.entry.Path)
		return v, nil
	}
	v.cluster = &s
	if s.changed {
		v.cluster.changed = false
		v.changed = true
		v.focus = s.entry.Path
		v.reload(s.entry.Path)
		v.cluster.reload(v.items)
		if refreshCmd := v.refreshCmd(); refreshCmd != nil {
			v.cluster.status += " · refreshing…"
			return v, tea.Batch(cmd, refreshCmd)
		}
	}
	return v, cmd
}

// openTasks shows a project's open tasks.
func (v view) openTasks(item *Item) (tea.Model, tea.Cmd) {
	if v.hooks.Tasks == nil {
		v.errMsg = "tasks are not available here"
		return v, nil
	}
	if item.Entry.Kind != vault.Project {
		v.errMsg = "a knowledge base has no tasks"
		return v, nil
	}
	s := newTasks(v.hooks, v.opener, item, v.items, v.width)
	s.avail = v.bodyHeight()
	s.ensureVisible()
	v.tasks = &s
	return v, nil
}

// updateTasks forwards keys to a project's tasks screen.
func (v view) updateTasks(msg tea.Msg) (tea.Model, tea.Cmd) {
	s, cmd := v.tasks.update(msg)
	if s.closed {
		v.tasks = nil
		v.reload(s.item.Entry.Path)
		return v, nil
	}
	v.tasks = &s
	return v, v.afterTasks(v.tasks, cmd)
}

// updateTasksTab forwards keys to the hosted board. Esc there returns to the Projects tab.
func (v view) updateTasksTab(msg tea.Msg) (tea.Model, tea.Cmd) {
	if v.tasksTab == nil {
		return v, nil
	}
	s, cmd := v.tasksTab.update(msg)
	if s.closed {
		s.closed = false
		v.tasksTab = &s
		v.goTo(tabProjects)
		return v, nil
	}
	v.tasksTab = &s
	return v, v.afterTasks(v.tasksTab, cmd)
}

// afterTasks does what a tasks board asked for after a key: starts Claude Code, or
// refreshes after a plant.
func (v *view) afterTasks(s *tasksScreen, cmd tea.Cmd) tea.Cmd {
	if l := s.launch; l != nil {
		s.launch = nil
		c, err := v.opener.ClaudeIn(l.vault, l.dir, l.prompt)
		if err != nil {
			s.err = err.Error()
			return nil
		}
		name := l.name
		return tea.ExecProcess(c, func(err error) tea.Msg { return claudeDoneMsg{name: name, err: err} })
	}
	if s.changed {
		s.changed = false
		v.changed = true
		if refreshCmd := v.refreshCmd(); refreshCmd != nil {
			s.status += " · refreshing…"
			return tea.Batch(cmd, refreshCmd)
		}
	}
	return cmd
}

// openIngest starts the ingest screen for a project.
func (v view) openIngest(item *Item) (tea.Model, tea.Cmd) {
	if v.hooks.StagePlan == nil || v.hooks.Stage == nil {
		v.errMsg = "ingesting is not available here"
		return v, nil
	}
	if item.Entry.Kind != vault.Project {
		v.errMsg = "a knowledge base has no inbox; knowledge enters through a project that mounts it"
		return v, nil
	}
	s := newIngest(v.hooks, item.Entry)
	v.ingest = &s
	return v, tea.Batch(textinput.Blink, s.source.focus())
}

// updateIngest forwards keys to the ingest screen and acts on how it ended.
func (v view) updateIngest(msg tea.Msg) (tea.Model, tea.Cmd) {
	s, cmd := v.ingest.update(msg)
	switch s.outcome {
	case ingestOpen:
		v.ingest = &s
		return v, cmd
	case ingestCancelled:
		v.ingest = nil
		return v, nil
	case ingestNothing:
		v.ingest = nil
		v.status = fmt.Sprintf("nothing to ingest: %d file%s already ingested, nothing waiting in inbox/", len(s.plan.Unchanged), plural(len(s.plan.Unchanged)))
		return v, nil
	}
	v.ingest = nil
	waiting := s.plan.Waiting
	if s.result != nil {
		waiting += len(s.result.Staged)
	}
	if s.outcome == ingestStartNow {
		return v.claude(&Item{Entry: s.entry}, claudecode.IngestPrompt)
	}
	v.status = fmt.Sprintf("%d file%s waiting in inbox/; press c and run /claude-atlas:wiki-ingest when ready", waiting, plural(waiting))
	return v, nil
}

// claude hands the terminal to a Claude Code session in the vault and resumes after.
func (v view) claude(item *Item, prompt string) (tea.Model, tea.Cmd) {
	if v.opener.Claude == nil {
		v.errMsg = "starting Claude Code is not available here"
		return v, nil
	}
	cmd, err := v.opener.Claude(item.Entry.Path, prompt)
	if err != nil {
		v.errMsg = err.Error()
		return v, nil
	}
	name := entryName(item.Entry)
	return v, tea.ExecProcess(cmd, func(err error) tea.Msg { return claudeDoneMsg{name: name, err: err} })
}

// open starts opening a vault in Obsidian, asking first when Obsidian does not know it.
func (v view) open(item *Item) (tea.Model, tea.Cmd) {
	if v.opener.Status == nil {
		v.errMsg = "opening vaults is not available here"
		return v, nil
	}
	registered, running, err := v.opener.Status(item.Entry.Path)
	if err != nil {
		v.errMsg = err.Error()
		return v, nil
	}
	if registered {
		return v.openAsync(item, false)
	}
	v.ask = item
	v.askRun = running
	return v, nil
}

func (v view) openAsync(item *Item, register bool) (tea.Model, tea.Cmd) {
	name, path := entryName(item.Entry), item.Entry.Path
	fn := v.opener.Open
	v.busy = "opening " + name + " in Obsidian…"
	if register {
		fn = v.opener.RegisterAndOpen
		v.busy = "registering " + name + " with Obsidian…"
		if v.askRun {
			v.busy = "registering " + name + " and restarting Obsidian…"
		}
	}
	return v, func() tea.Msg { return openedMsg{name: name, err: fn(path)} }
}

// footer renders the prompt, progress, or status lines under a screen.
func (v view) footer(hints ...string) string {
	switch {
	case v.ask != nil:
		question := "Register it as a vault?"
		if v.askRun {
			question += " Obsidian will quit and relaunch."
		}
		return fmt.Sprintf("  %s Obsidian does not know %s.\n  %s\n  %s / %s\n",
			errSt.Render("▲"), entryName(v.ask.Entry), question, title.Render("y"), title.Render("n"))
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
	case tabTasks:
		return muted
	case tabProblems:
		return lipgloss.Color("9")
	}
	return projectColor
}

// tabs renders the tab boxes, with their counts when there is room for them.
func (v view) tabRow(counts bool) string {
	var parts []string
	for _, t := range v.tabs() {
		text := tabNames[t]
		if n, ok := v.count(t); ok && counts {
			text += fmt.Sprintf(" (%d)", n)
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

// count is the number after a tab's name: its vaults, or the open tasks the last refresh
// counted. It is false for Tasks until a refresh has counted them.
func (v view) count(t tab) (int, bool) {
	if t != tabTasks {
		return len(v.boards[boardOf(t)].items), true
	}
	n, known := 0, false
	for _, it := range v.items {
		if s := it.Entry.State; it.Entry.Kind == vault.Project && s != nil && s.Tasks != nil {
			n += s.Tasks.Counts.Open
			known = true
		}
	}
	return n, known
}

// fit makes a frame exactly as tall as the screen, so the tab bar sits on the same row
// whatever the tab holds. A frame the terminal has to scroll moves everything above the
// fold out of sight; a short one leaves the top where it is. trim cuts a frame that
// somehow grew, which the board's own line budget already prevents.
func (v view) fit(frame string, trim bool) string {
	out := strings.Split(strings.TrimSuffix(frame, "\n"), "\n")
	if v.height <= 0 {
		return strings.Join(out, "\n")
	}
	for len(out) < v.height {
		out = append(out, "")
	}
	if trim && len(out) > v.height {
		out = out[:v.height]
	}
	return strings.Join(out, "\n")
}

func (v view) View() string {
	switch {
	case v.edit != nil:
		return v.fit(v.edit.view(), false)
	case v.add != nil:
		return v.fit(v.add.View(), false)
	case v.ingest != nil:
		return v.fit(v.ingest.view(), false)
	case v.links != nil:
		return v.fit(v.links.view(), false)
	case v.mounts != nil:
		return v.fit(v.mounts.view(), false)
	case v.cluster != nil:
		return v.fit(v.cluster.view(), false)
	case v.tasks != nil:
		return v.fit(v.tasks.view(), false)
	}
	var b strings.Builder
	b.WriteString(v.head())
	if v.tab == tabTasks {
		if v.tasksTab == nil {
			b.WriteString("  " + dim.Render("tasks are not available here") + "\n\n")
		} else {
			b.WriteString(v.tasksTab.view())
		}
		b.WriteString(v.footer(v.hints()...))
		return v.fit(b.String(), true)
	}
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
	return v.fit(b.String(), true)
}

// hints is the footer. With help off it names the tab's own keys: Enter for the vault
// under the cursor, the key that adds a vault of the tab's kind, h, and q. With help on
// it names every key on two lines.
func (v view) hints() []string {
	quit := "h help · q quit"
	if v.help {
		quit = "h hide help · q quit"
	}
	if v.tab == tabTasks {
		if v.help {
			return []string{"←→ tabs · R refresh · " + quit}
		}
		return []string{quit}
	}
	if v.help {
		return []string{v.boardHints(), "←→ tabs · n new project · N new knowledge base · C new cluster · a adopt · R refresh · " + quit}
	}
	bd := v.board()
	it := bd.current()
	var parts []string
	if it != nil {
		parts = append(parts, enterHint(bd, it))
	}
	switch v.tab {
	case tabProjects:
		parts = append(parts, "n new project")
	case tabKnowledge:
		parts = append(parts, "N new knowledge base", "C new cluster")
	case tabProblems:
		if it != nil && it.Entry.Reason != registry.ReasonMissing {
			parts = append(parts, "a adopt")
		}
	}
	return []string{strings.Join(append(parts, quit), " · ")}
}

// enterHint says what Enter does to the vault under the cursor.
func enterHint(bd *board, it *Item) string {
	if bd.expanded[it.Entry.Path] {
		return "Enter collapse"
	}
	return "Enter details"
}

// vaultKeys lists the keys that act on one vault.
func vaultKeys(e registry.Entry) string {
	if e.Error != "" {
		keys := "o Obsidian · e edit"
		if e.Reason != registry.ReasonMissing {
			keys += " · a adopt"
		}
		return keys
	}
	keys := "o Obsidian · c Claude"
	if e.Kind == vault.Project {
		keys += " · i ingest · t tasks · l repos"
	} else {
		keys += " · M members"
	}
	return keys + " · m mounts · e edit"
}

// boardHints lists the keys for the vault under the cursor.
func (v view) boardHints() string {
	hints := "↑↓ move"
	bd := v.board()
	if bd == nil {
		return hints
	}
	it := bd.current()
	if it == nil {
		return hints
	}
	return hints + " · " + enterHint(bd, it) + " · " + vaultKeys(it.Entry)
}

// RunView shows the atlas until the user quits. It reports whether any vault changed.
func RunView(items []Item, opener Opener, hooks Hooks) (bool, error) {
	final, err := tea.NewProgram(newView(items, opener, hooks), tea.WithAltScreen()).Run()
	if err != nil {
		return false, fmt.Errorf("interactive screen failed: %w", err)
	}
	return final.(view).changed, nil
}
