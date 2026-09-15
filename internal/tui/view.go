package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/refresh"
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

// problems is the folder a vault the scan could not read sits in.
const problems = "problems"

// entryName is the name to show: a vault the scan could not read has only its folder.
func entryName(e registry.Entry) string {
	if e.Error != "" || e.Name == "" {
		return filepath.Base(e.Path)
	}
	return e.Name
}

// folder is where an item sits in the tree: its Rel without its own name.
func (i Item) folder() string {
	if i.Entry.Error != "" {
		return problems
	}
	rel := i.Entry.Rel()
	if j := strings.LastIndex(rel, "/"); j >= 0 {
		return rel[:j]
	}
	return ""
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

// maxLayers is how many folder layers the tree shows before folding deeper ones.
const maxLayers = 3

type folderNode struct {
	name     string
	path     string
	children []*folderNode
	items    []*Item
}

func (n *folderNode) count() int {
	total := len(n.items)
	for _, c := range n.children {
		total += c.count()
	}
	return total
}

// topRank puts projects before knowledge bases at the top of the tree, and the vaults
// the scan could not read last.
func topRank(path string) int {
	switch path {
	case "projects":
		return 0
	case "knowledge":
		return 1
	case problems:
		return 3
	}
	return 2
}

// buildTree arranges the items whose folder sits under root into nested folder nodes.
func buildTree(items []Item, root string) *folderNode {
	top := &folderNode{path: root}
	byPath := map[string]*folderNode{root: top}
	for i := range items {
		item := &items[i]
		folder := item.folder()
		if root != "" && folder != root && !strings.HasPrefix(folder, root+"/") {
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(folder, root), "/")
		node := top
		if rel != "" {
			path := root
			for _, seg := range strings.Split(rel, "/") {
				if path == "" {
					path = seg
				} else {
					path += "/" + seg
				}
				child, ok := byPath[path]
				if !ok {
					child = &folderNode{name: seg, path: path}
					byPath[path] = child
					node.children = append(node.children, child)
				}
				node = child
			}
		}
		node.items = append(node.items, item)
	}
	var sortNode func(n *folderNode)
	sortNode = func(n *folderNode) {
		sort.Slice(n.children, func(i, j int) bool {
			a, b := topRank(n.children[i].path), topRank(n.children[j].path)
			if a != b {
				return a < b
			}
			return n.children[i].name < n.children[j].name
		})
		sort.Slice(n.items, func(i, j int) bool { return entryName(n.items[i].Entry) < entryName(n.items[j].Entry) })
		for _, c := range n.children {
			sortNode(c)
		}
	}
	sortNode(top)
	return top
}

type rowKind int

const (
	rowVault rowKind = iota
	rowFolded
	rowFolder
)

// viewRow is a selectable element of the rendered tree with its line span.
type viewRow struct {
	kind  rowKind
	item  *Item
	path  string // folded folder path
	count int
	start int
	end   int
}

// frame remembers where the user was before zooming into a folded folder.
type frame struct {
	root   string
	cursor int
	offset int
}

type view struct {
	items     []Item
	opener    Opener
	hooks     Hooks
	edit      *editor
	add       *model // the add-vault or adopt screen while open
	ingest    *ingestScreen
	links     *linksScreen
	tasks     *tasksScreen
	changed   bool
	collapsed map[string]bool // folder paths folded by the user
	root      string
	stack     []frame
	ask       *Item  // vault awaiting a register-and-open confirmation
	askRun    bool   // Obsidian was running when we asked, so it will restart
	busy      string // message while an open runs in the background
	status    string
	errMsg    string
	rows      []viewRow
	lines     []string
	cursor    int
	offset    int
	width     int
	height    int
	detail    *Item
	refreshed string
	// focus is the vault the next refresh should land on, after a write.
	focus string
}

var (
	boxSt    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(0, 1)
	boxSelSt = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(0, 1)
	guideSt  = lipgloss.NewStyle().Foreground(muted)
	catSt    = lipgloss.NewStyle().Bold(true)
)

func newView(items []Item, opener Opener, hooks Hooks) view {
	v := view{items: items, opener: opener, hooks: hooks, collapsed: map[string]bool{}, width: 100, height: 40}
	v.stamp()
	v.layout()
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

// reloadKeeping is reload, with the details panel left as it was: open or closed.
func (v *view) reloadKeeping(path string) {
	keep := v.detail != nil
	v.reload(path)
	if !keep {
		v.detail = nil
	}
}

// reload re-reads the registry and keeps the cursor on the vault at path.
func (v *view) reload(path string) {
	entries, err := v.hooks.Load()
	if err != nil {
		v.errMsg = err.Error()
		return
	}
	v.items = Items(entries)
	v.stamp()
	v.layout()
	v.detail = nil
	for i := range v.rows {
		if v.rows[i].kind == rowVault && v.rows[i].item.Entry.Path == path {
			v.cursor = i
		}
	}
	for i := range v.items {
		if v.items[i].Entry.Path == path && path != "" {
			v.detail = &v.items[i]
		}
	}
	v.ensureVisible()
}

func (v view) Init() tea.Cmd { return nil }

func heatMark(state *registry.State) string {
	if state == nil {
		return "—"
	}
	switch state.Heat {
	case "new":
		return "✨"
	case "hot":
		return "🔥"
	case "warm":
		return "🌤️"
	case "cold":
		return "❄️"
	}
	return "⛔"
}

func unfinishedCount(state *registry.State) string {
	if state == nil {
		return "—"
	}
	if total := state.Unfinished.Total(); total != nil {
		return fmt.Sprint(*total)
	}
	return "—"
}

func idleText(state *registry.State) string {
	switch {
	case state == nil || state.DaysIdle == nil:
		return "—"
	case *state.DaysIdle == 0:
		return "today"
	default:
		return fmt.Sprintf("%dd", *state.DaysIdle)
	}
}

func pagesText(state *registry.State) string {
	if state == nil || state.Pages == nil {
		return "—"
	}
	return fmt.Sprint(*state.Pages)
}

// layout renders the tree into lines and records where each selectable row lands.
func (v *view) layout() {
	v.lines = nil
	v.rows = nil
	top := buildTree(v.items, v.root)
	v.renderNode(top, 0)
	if len(v.lines) > 0 {
		v.lines = append(v.lines, v.endLine())
	}
	if v.cursor > len(v.rows) {
		v.cursor = len(v.rows)
	}
}

// atEnd reports whether the cursor sits on the end marker below the last row.
func (v view) atEnd() bool { return len(v.rows) > 0 && v.cursor == len(v.rows) }

// endLine is the marker after the last row; it takes the cursor so the user knows the tree stops here.
func (v view) endLine() string {
	if v.atEnd() {
		return selSt.Render("(end)")
	}
	return dim.Render("(end)")
}

// current is the row under the cursor, or nil on the end marker or an empty tree.
func (v view) current() *viewRow {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return nil
	}
	return &v.rows[v.cursor]
}

func (v *view) guide(depth int) string {
	return guideSt.Render(strings.Repeat("│  ", depth))
}

func (v *view) renderNode(n *folderNode, depth int) {
	for _, item := range n.items {
		v.renderBox(item, depth)
	}
	for _, child := range n.children {
		if depth+1 > maxLayers {
			start := len(v.lines)
			v.lines = append(v.lines, v.guide(depth)+"▸ "+catSt.Render(child.name)+dim.Render(fmt.Sprintf("  %d vault%s · Enter to open", child.count(), plural(child.count()))))
			v.rows = append(v.rows, viewRow{kind: rowFolded, path: child.path, count: child.count(), start: start, end: start})
			continue
		}
		v.renderFolder(child, depth)
	}
}

// renderFolder writes a selectable folder header and, unless folded, its contents.
func (v *view) renderFolder(n *folderNode, depth int) {
	selected := len(v.rows) == v.cursor
	folded := v.collapsed[n.path]
	arrow, name := "▾ ", catSt.Render(n.name)
	if folded {
		arrow = "▸ "
	}
	if selected {
		arrow, name = selSt.Render(arrow), selSt.Render(n.name)
	}
	line := v.guide(depth) + arrow + name
	if folded {
		line += dim.Render(fmt.Sprintf("  %d vault%s", n.count(), plural(n.count())))
	}
	start := len(v.lines)
	v.lines = append(v.lines, line)
	v.rows = append(v.rows, viewRow{kind: rowFolder, path: n.path, count: n.count(), start: start, end: start})
	if !folded {
		v.renderNode(n, depth+1)
	}
}

// parentPath is the folder a row sits in, relative to the tree shown; "" at the root.
func (v view) parentPath(r viewRow) string {
	switch r.kind {
	case rowVault:
		return r.item.folder()
	default:
		if i := strings.LastIndex(r.path, "/"); i >= 0 {
			return r.path[:i]
		}
		return ""
	}
}

// moveTo puts the cursor on the row for a folder path, when it is visible.
func (v *view) moveTo(path string) {
	for i, r := range v.rows {
		if r.kind != rowVault && r.path == path {
			v.cursor = i
			return
		}
	}
}

// fold collapses or expands the branch at the cursor. On a vault it collapses the
// vault's folder and moves the cursor there.
func (v *view) fold() {
	r := v.current()
	if r == nil {
		return
	}
	switch r.kind {
	case rowFolder:
		v.collapsed[r.path] = !v.collapsed[r.path]
	case rowVault:
		parent := v.parentPath(*r)
		if parent == "" || parent == v.root {
			return
		}
		v.collapsed[parent] = true
		v.layout()
		v.moveTo(parent)
		return
	default:
		return
	}
	v.layout()
}

// foldAll collapses or expands every folder in the tree shown. The cursor stays on the
// nearest visible ancestor of where it was.
func (v *view) foldAll(collapse bool) {
	anchor := ""
	if r := v.current(); r != nil {
		anchor = r.path
		if r.kind == rowVault {
			anchor = r.item.folder()
		}
	}
	if !collapse {
		v.collapsed = map[string]bool{}
	} else {
		var mark func(n *folderNode)
		mark = func(n *folderNode) {
			for _, c := range n.children {
				v.collapsed[c.path] = true
				mark(c)
			}
		}
		mark(buildTree(v.items, v.root))
	}
	v.layout()
	best := -1
	for i, r := range v.rows {
		if r.kind == rowVault || r.path == "" {
			continue
		}
		if (anchor == r.path || strings.HasPrefix(anchor, r.path+"/")) && (best < 0 || len(r.path) > len(v.rows[best].path)) {
			best = i
		}
	}
	switch {
	case best >= 0:
		v.cursor = best
	case v.cursor > len(v.rows):
		v.cursor = len(v.rows)
	}
}

func (v *view) renderBox(item *Item, depth int) {
	width := min(46, max(24, v.width-depth*3-6))
	selected := len(v.rows) == v.cursor
	style := boxSt
	if selected {
		style = boxSelSt
	}
	name := entryName(item.Entry)
	if selected {
		name = selSt.Render(name)
	}
	state := item.Entry.State
	facts := dim.Render(fmt.Sprintf("%s pages · %s unfinished · idle %s", pagesText(state), unfinishedCount(state), idleText(state)))
	if item.Entry.Error != "" {
		facts = errSt.Render(item.Entry.Error)
	}
	box := style.Width(width).Render(fmt.Sprintf("%s %s\n%s", heatMark(state), name, facts))
	start := len(v.lines)
	for _, line := range strings.Split(box, "\n") {
		v.lines = append(v.lines, v.guide(depth)+line)
	}
	v.rows = append(v.rows, viewRow{kind: rowVault, item: item, start: start, end: len(v.lines) - 1})
}

func (v *view) ensureVisible() {
	if len(v.rows) == 0 {
		v.offset = 0
		return
	}
	avail := v.bodyHeight()
	start, end := len(v.lines)-1, len(v.lines)-1
	if r := v.current(); r != nil {
		start, end = r.start, r.end
	}
	if start < v.offset {
		v.offset = start
	}
	if end >= v.offset+avail {
		v.offset = end - avail + 1
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

func (v view) bodyHeight() int { return max(5, v.height-6) }

func (v view) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width, v.height = msg.Width, msg.Height
		if v.links != nil {
			v.links.width = msg.Width
		}
		if v.tasks != nil {
			v.tasks.width = msg.Width
		}
		v.layout()
		v.ensureVisible()
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
			if v.detail != nil {
				path = v.detail.Entry.Path
			} else if r := v.current(); r != nil && r.kind == rowVault {
				path = r.item.Entry.Path
			}
		}
		v.reloadKeeping(path)
		v.status = "refreshed"
		if v.links != nil {
			v.links.reload(v.items)
			v.links.status = "refreshed"
		}
		if v.tasks != nil {
			v.tasks.reload(v.items)
			v.tasks.status = "refreshed"
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
		if v.tasks != nil {
			return v.updateTasks(msg)
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
		switch msg.String() {
		case "n":
			return v.openAdd(newModel(v.hooks.VaultsDir, vault.Project))
		case "N":
			return v.openAdd(newModel(v.hooks.VaultsDir, vault.Knowledge))
		case "a":
			return v.openAdd(newAdoptModel())
		case "R":
			return v.refresh()
		case "T":
			return v.openTasks(nil)
		}
		if key := msg.String(); key == "o" || key == "c" || key == "e" || key == "i" || key == "l" || key == "t" {
			var item *Item
			if v.detail != nil {
				item = v.detail
			} else if r := v.current(); r != nil && r.kind == rowVault {
				item = r.item
			}
			if item == nil {
				return v, nil
			}
			switch key {
			case "c":
				return v.claude(item, "")
			case "e":
				return v.openEditor(item)
			case "l":
				return v.openLinks(item)
			case "t":
				return v.openTasks(item)
			case "i":
				return v.openIngest(item)
			}
			return v.open(item)
		}
		if v.detail != nil {
			if msg.Type == tea.KeyEsc || msg.Type == tea.KeyEnter || msg.Type == tea.KeyLeft {
				v.detail = nil
			}
			return v, nil
		}
		switch msg.Type {
		case tea.KeyUp:
			if v.cursor > 0 {
				v.cursor--
			}
		case tea.KeyDown:
			// One step past the last row lands on the end marker, so the bottom is unmistakable.
			if v.cursor < len(v.rows) {
				v.cursor++
			}
		case tea.KeyEnter, tea.KeyRight:
			r := v.current()
			if r == nil {
				return v, nil
			}
			switch r.kind {
			case rowFolded:
				v.stack = append(v.stack, frame{root: v.root, cursor: v.cursor, offset: v.offset})
				v.root = r.path
				v.cursor = 0
				v.offset = 0
			case rowFolder:
				v.collapsed[r.path] = !v.collapsed[r.path]
			default:
				v.detail = r.item
			}
		case tea.KeySpace, tea.KeyLeft:
			v.fold()
		case tea.KeyEsc:
			if len(v.stack) == 0 {
				return v, tea.Quit
			}
			last := v.stack[len(v.stack)-1]
			v.stack = v.stack[:len(v.stack)-1]
			v.root, v.cursor, v.offset = last.root, last.cursor, last.offset
		default:
			switch msg.String() {
			case "-":
				v.foldAll(true)
			case "+", "=":
				v.foldAll(false)
			}
		}
		v.layout()
		v.ensureVisible()
	}
	return v, nil
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
	v.detail = nil
	return v, cmd
}

// wrote records a write: the tree reloads now, and a background refresh brings the
// derived state up to date and lands the cursor back on this vault.
func (v *view) wrote(path, status string) tea.Cmd {
	v.changed = true
	v.status = status
	v.focus = path
	return v.refreshCmd()
}

// refresh rebuilds derived state in the background, then reloads the tree.
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
		cmd = v.wrote(path, "saved "+ed.draft.Name)
		v.reloadKeeping(path)
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
		v.reloadKeeping(s.entry.Path)
		return v, nil
	}
	v.links = &s
	if s.changed {
		v.links.changed = false
		v.changed = true
		v.focus = s.entry.Path
		v.reloadKeeping(s.entry.Path)
		v.links.reload(v.items)
		if refreshCmd := v.refreshCmd(); refreshCmd != nil {
			v.links.status += " · refreshing…"
			return v, tea.Batch(cmd, refreshCmd)
		}
	}
	return v, cmd
}

// openTasks shows a project's open tasks, or every project's when item is nil.
func (v view) openTasks(item *Item) (tea.Model, tea.Cmd) {
	if v.hooks.Tasks == nil {
		v.errMsg = "tasks are not available here"
		return v, nil
	}
	if item != nil && item.Entry.Kind != vault.Project {
		v.errMsg = "a knowledge base has no tasks"
		return v, nil
	}
	s := newTasks(v.hooks, v.opener, item, v.items, v.width)
	v.tasks = &s
	return v, nil
}

// updateTasks forwards keys to the tasks screen, starts Claude Code when asked, and
// refreshes in the background after a plant.
func (v view) updateTasks(msg tea.Msg) (tea.Model, tea.Cmd) {
	s, cmd := v.tasks.update(msg)
	if s.closed {
		v.tasks = nil
		path := ""
		if s.item != nil {
			path = s.item.Entry.Path
		} else if r := v.current(); r != nil && r.kind == rowVault {
			path = r.item.Entry.Path
		}
		v.reloadKeeping(path)
		return v, nil
	}
	v.tasks = &s
	if l := s.launch; l != nil {
		v.tasks.launch = nil
		cmd, err := v.opener.ClaudeIn(l.vault, l.dir, l.prompt)
		if err != nil {
			v.tasks.err = err.Error()
			return v, nil
		}
		name := l.name
		return v, tea.ExecProcess(cmd, func(err error) tea.Msg { return claudeDoneMsg{name: name, err: err} })
	}
	if s.changed {
		v.tasks.changed = false
		v.changed = true
		if refreshCmd := v.refreshCmd(); refreshCmd != nil {
			v.tasks.status += " · refreshing…"
			return v, tea.Batch(cmd, refreshCmd)
		}
	}
	return v, cmd
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
		out += "  " + dim.Render(line) + "\n"
	}
	if v.status != "" {
		out += "  " + okSt.Render(v.status) + "\n"
	}
	if v.errMsg != "" {
		out += "  " + errSt.Render(v.errMsg) + "\n"
	}
	return out
}

func (v view) View() string {
	if v.edit != nil {
		return v.edit.view()
	}
	if v.add != nil {
		return v.add.View()
	}
	if v.ingest != nil {
		return v.ingest.view()
	}
	if v.links != nil {
		return v.links.view()
	}
	if v.tasks != nil {
		return v.tasks.view()
	}
	if v.detail != nil {
		return v.viewDetail()
	}
	var b strings.Builder
	crumb := "all vaults"
	if v.root != "" {
		crumb = strings.ReplaceAll(v.root, "/", " › ")
	}
	stamp := "not refreshed yet"
	if t, err := time.Parse("2006-01-02T15:04:05Z", v.refreshed); err == nil {
		stamp = "refreshed " + t.Local().Format("2006-01-02 15:04")
	}
	fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", title.Render("Atlas"), catSt.Render(crumb), dim.Render(fmt.Sprintf("%d vault%s · %s", len(v.items), plural(len(v.items)), stamp)))
	if len(v.lines) == 0 {
		b.WriteString("  " + dim.Render("no vaults yet; press n to make one, or run `claude-atlas new-project NAME`") + "\n")
	}
	end := min(len(v.lines), v.offset+v.bodyHeight())
	for _, line := range v.lines[v.offset:end] {
		b.WriteString("  " + line + "\n")
	}
	if end < len(v.lines) {
		b.WriteString("  " + dim.Render(fmt.Sprintf("… %d more lines", len(v.lines)-end)) + "\n")
	}
	b.WriteString("\n" + v.footer(v.treeHints(), v.globalHints()))
	return b.String()
}

// globalHints lists the keys that work anywhere in the tree.
func (v view) globalHints() string {
	return "n new project · N new knowledge base · a adopt · T all tasks · R refresh · - + fold all · q quit"
}

// vaultKeys lists the keys that act on one vault.
func vaultKeys(e registry.Entry) string {
	keys := "o Obsidian · c Claude"
	if e.Kind == vault.Project {
		keys += " · i ingest · t tasks · l repos"
	}
	return keys + " · e edit"
}

// treeHints lists the keys that do something for the row under the cursor.
func (v view) treeHints() string {
	hints := "↑↓ move"
	if r := v.current(); r != nil {
		switch r.kind {
		case rowVault:
			hints += " · Enter details · " + vaultKeys(r.item.Entry) + " · Space fold"
		case rowFolder:
			if v.collapsed[r.path] {
				hints += " · Enter unfold"
			} else {
				hints += " · Enter fold"
			}
		case rowFolded:
			hints += " · Enter open"
		}
	}
	if len(v.stack) > 0 {
		hints += " · Esc back"
	}
	return hints
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func (v view) viewDetail() string {
	e := v.detail.Entry
	s := e.State
	where := e.Rel()
	if e.Error != "" {
		where = home.Display(e.Path)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s\n\n", title.Render(entryName(e)), dim.Render(where))
	if s == nil {
		b.WriteString("  " + dim.Render("never refreshed; press R") + "\n\n")
	} else {
		summary := heatMark(s) + " " + dash(s.Heat)
		if e.Created != "" {
			summary += " · created " + e.Created
		}
		if s.LastTouched != "" {
			summary += " · last touched " + s.LastTouched + " (" + idleText(s) + ")"
		}
		b.WriteString("  " + summary + "\n\n")
	}
	row := func(k, val string) { fmt.Fprintf(&b, "  %s%s\n", label.Width(16).Render(k), dash(val)) }
	row("Name", entryName(e))
	row("Kind", string(e.Kind))
	row("Id", e.ID)
	row("Path", home.Display(e.Path))
	row("Mode", string(e.Mode))
	if e.Kind == vault.Knowledge {
		row("Scope", e.Scope)
		row("Access", e.Access)
		for _, g := range e.Grants {
			row("Grant", g.Name+"  "+g.Access)
		}
		for _, m := range e.MountedBy {
			row("Mounted by", m.Name+"  "+m.Access)
		}
	} else {
		row("Tags", strings.Join(e.Tags, ", "))
	}
	if len(e.Mounts) > 0 {
		b.WriteString("\n  " + catSt.Render("Mounts") + "\n")
		for _, m := range e.Mounts {
			detail := dim.Render(m.Effective + " · " + home.Display(m.Path))
			if m.Error != "" {
				detail = errSt.Render(m.Error)
			}
			fmt.Fprintf(&b, "    %-24s %s\n", m.Name, detail)
		}
	}
	if len(e.Repos) > 0 {
		b.WriteString("\n  " + catSt.Render("Repositories") + "\n")
		for _, r := range e.Repos {
			where := home.Display(r.Path) + " · changes: " + links.Policy(r.Changes, r.Remote)
			if r.Remote != "" {
				where += " · " + r.Remote
			}
			if r.Path == "" {
				where = r.Error
			}
			fmt.Fprintf(&b, "    %-24s %s\n", r.Name, dim.Render(where))
			if s != nil {
				if fact, ok := s.RepoFacts[r.Name]; ok {
					facts := refresh.LinkSummary(fact)
					if !fact.OK {
						facts = errSt.Render(facts)
					}
					b.WriteString("                             " + dim.Render(facts) + "\n")
				}
			}
		}
	}
	if s != nil {
		b.WriteString("\n")
		check := okSt.Render("ok")
		if !s.VaultOK {
			check = errSt.Render(dash(s.VaultError))
		}
		row("Vault check", check)
		row("Heat", s.Heat)
		row("Last touched", s.LastTouched)
		if s.DaysIdle != nil {
			row("Idle", fmt.Sprintf("%d day%s", *s.DaysIdle, plural(*s.DaysIdle)))
		}
		row("Last operation", s.LastOperation)
		row("Pages", pagesText(s))
		row("Unfinished", s.Unfinished.Text())
		for i, t := range s.OpenThreads {
			k := "Open threads"
			if i > 0 {
				k = ""
			}
			fmt.Fprintf(&b, "  %s- %s\n", label.Width(16).Render(k), refresh.PlainText(t))
		}
		if s.Tasks != nil {
			row("Tasks", taskSummaryText(s.Tasks))
		}
		if t, err := time.Parse("2006-01-02T15:04:05Z", s.GeneratedAt); err == nil {
			row("Refreshed", t.Local().Format("2006-01-02 15:04"))
		}
	}
	if signals := refresh.Signals(e, now()); len(signals) > 0 {
		b.WriteString("\n  " + catSt.Render("Signals") + "\n")
		for _, signal := range signals {
			b.WriteString("    - " + signal + "\n")
		}
	}
	b.WriteString("\n" + v.footer(vaultKeys(e)+" · R refresh · Esc back · q quit"))
	return b.String()
}

// taskSummaryText is one line of task counts for the details screen.
func taskSummaryText(t *registry.TaskSummary) string {
	c := t.Counts
	if c.Open == 0 {
		if c.Notes > 0 {
			return fmt.Sprintf("none open · %d note%s waiting", c.Notes, plural(c.Notes))
		}
		return "none open"
	}
	out := fmt.Sprintf("%d open: %d active · %d blocked · %d planned · %d planted", c.Open, c.Active, c.Blocked, c.Planned, c.Planted)
	if c.Stale > 0 {
		out += errSt.Render(fmt.Sprintf(" · %d stale", c.Stale))
	}
	if c.Notes > 0 {
		out += fmt.Sprintf(" · %d note%s waiting", c.Notes, plural(c.Notes))
	}
	return out
}

// RunView shows the tree until the user quits. It reports whether any vault changed.
func RunView(items []Item, opener Opener, hooks Hooks) (bool, error) {
	final, err := tea.NewProgram(newView(items, opener, hooks), tea.WithAltScreen()).Run()
	if err != nil {
		return false, fmt.Errorf("interactive screen failed: %w", err)
	}
	return final.(view).changed, nil
}
