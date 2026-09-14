package tui

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/tree"
)

// Item is one project with the state from its last refresh (nil when never refreshed).
type Item struct {
	Project *tree.Project
	State   *tree.State
}

// Opener connects the view to Obsidian and Claude Code without the screen doing the work itself.
type Opener struct {
	Status          func(vault string) (registered, running bool, err error)
	Open            func(vault string) error
	RegisterAndOpen func(vault string) error
	// Claude builds the Claude Code process for a vault, with prompt as the first
	// message when not empty; the view hands it the terminal.
	Claude func(vault, prompt string) (*exec.Cmd, error)
}

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

// maxLayers is how many category layers the tree shows before folding deeper ones.
const maxLayers = 3

type catNode struct {
	name     string
	path     string
	children []*catNode
	projects []*Item
}

func (n *catNode) count() int {
	total := len(n.projects)
	for _, c := range n.children {
		total += c.count()
	}
	return total
}

// buildTree arranges the items whose category sits under root into nested category nodes.
func buildTree(items []Item, root string) *catNode {
	top := &catNode{path: root}
	byPath := map[string]*catNode{root: top}
	for i := range items {
		item := &items[i]
		cat := item.Project.Category()
		if root != "" && cat != root && !strings.HasPrefix(cat, root+"/") {
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(cat, root), "/")
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
					child = &catNode{name: seg, path: path}
					byPath[path] = child
					node.children = append(node.children, child)
				}
				node = child
			}
		}
		node.projects = append(node.projects, item)
	}
	var sortNode func(n *catNode)
	sortNode = func(n *catNode) {
		sort.Slice(n.children, func(i, j int) bool { return n.children[i].name < n.children[j].name })
		sort.Slice(n.projects, func(i, j int) bool { return n.projects[i].Project.Name < n.projects[j].Project.Name })
		for _, c := range n.children {
			sortNode(c)
		}
	}
	sortNode(top)
	return top
}

type rowKind int

const (
	rowProject rowKind = iota
	rowFolded
	rowCategory
)

// viewRow is a selectable element of the rendered tree with its line span.
type viewRow struct {
	kind  rowKind
	item  *Item
	path  string // folded category path
	count int
	start int
	end   int
}

// frame remembers where the user was before zooming into a folded category.
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
	changed   bool
	collapsed map[string]bool // category paths folded by the user
	root      string
	stack     []frame
	ask       *Item  // project awaiting a register-and-open confirmation
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
		if it.State != nil && it.State.GeneratedAt > v.refreshed {
			v.refreshed = it.State.GeneratedAt
		}
	}
}

// reloadKeeping is reload, with the details panel left as it was: open or closed.
func (v *view) reloadKeeping(rel string) {
	keep := v.detail != nil
	v.reload(rel)
	if !keep {
		v.detail = nil
	}
}

// reload re-reads the tree after an edit and keeps the cursor on the project at rel.
func (v *view) reload(rel string) {
	projects, err := v.hooks.Load()
	if err != nil {
		v.errMsg = err.Error()
		return
	}
	items := make([]Item, 0, len(projects))
	for _, p := range projects {
		var state *tree.State
		if v.hooks.State != nil {
			state = v.hooks.State(p.Rel)
		}
		items = append(items, Item{Project: p, State: state})
	}
	v.items = items
	v.stamp()
	v.layout()
	v.detail = nil
	for i := range v.rows {
		if v.rows[i].kind == rowProject && v.rows[i].item.Project.Rel == rel {
			v.cursor = i
		}
	}
	for i := range v.items {
		if v.items[i].Project.Rel == rel && rel != "" {
			v.detail = &v.items[i]
		}
	}
	v.ensureVisible()
}

func (v view) Init() tea.Cmd { return nil }

func heatMark(state *tree.State) string {
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

func unfinishedCount(state *tree.State) string {
	if state == nil {
		return "—"
	}
	total, known := 0, false
	for _, v := range []*int{state.Unfinished.EmptySections, state.Unfinished.SeedPages, state.Unfinished.DeadLinks} {
		if v != nil {
			total, known = total+*v, true
		}
	}
	if !known {
		return "—"
	}
	return fmt.Sprint(total)
}

func idleText(state *tree.State) string {
	switch {
	case state == nil || state.DaysIdle == nil:
		return "—"
	case *state.DaysIdle == 0:
		return "today"
	default:
		return fmt.Sprintf("%dd", *state.DaysIdle)
	}
}

func pagesText(state *tree.State) string {
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

func (v *view) renderNode(n *catNode, depth int) {
	for _, item := range n.projects {
		v.renderBox(item, depth)
	}
	for _, child := range n.children {
		if depth+1 > maxLayers {
			start := len(v.lines)
			v.lines = append(v.lines, v.guide(depth)+"▸ "+catSt.Render(child.name)+dim.Render(fmt.Sprintf("  %d project%s · Enter to open", child.count(), plural(child.count()))))
			v.rows = append(v.rows, viewRow{kind: rowFolded, path: child.path, count: child.count(), start: start, end: start})
			continue
		}
		v.renderCategory(child, depth)
	}
}

// renderCategory writes a selectable category header and, unless folded, its contents.
func (v *view) renderCategory(n *catNode, depth int) {
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
		line += dim.Render(fmt.Sprintf("  %d project%s", n.count(), plural(n.count())))
	}
	start := len(v.lines)
	v.lines = append(v.lines, line)
	v.rows = append(v.rows, viewRow{kind: rowCategory, path: n.path, count: n.count(), start: start, end: start})
	if !folded {
		v.renderNode(n, depth+1)
	}
}

// parentPath is the category a row sits in, relative to the tree shown; "" at the root.
func (v view) parentPath(r viewRow) string {
	switch r.kind {
	case rowProject:
		return r.item.Project.Category()
	default:
		if i := strings.LastIndex(r.path, "/"); i >= 0 {
			return r.path[:i]
		}
		return ""
	}
}

// moveTo puts the cursor on the row for a category path, when it is visible.
func (v *view) moveTo(path string) {
	for i, r := range v.rows {
		if r.kind != rowProject && r.path == path {
			v.cursor = i
			return
		}
	}
}

// fold collapses or expands the branch at the cursor. On a project it collapses the
// project's category and moves the cursor there.
func (v *view) fold() {
	r := v.current()
	if r == nil {
		return
	}
	switch r.kind {
	case rowCategory:
		v.collapsed[r.path] = !v.collapsed[r.path]
	case rowProject:
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

// foldAll collapses or expands every category in the tree shown. The cursor stays on
// the nearest visible ancestor of where it was.
func (v *view) foldAll(collapse bool) {
	anchor := ""
	if r := v.current(); r != nil {
		anchor = r.path
		if r.kind == rowProject {
			anchor = r.item.Project.Category()
		}
	}
	if !collapse {
		v.collapsed = map[string]bool{}
	} else {
		var mark func(n *catNode)
		mark = func(n *catNode) {
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
		if r.kind == rowProject || r.path == "" {
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
	name := item.Project.Name
	if selected {
		name = selSt.Render(name)
	}
	body := fmt.Sprintf("%s %s\n%s", heatMark(item.State), name,
		dim.Render(fmt.Sprintf("%s pages · %s unfinished · idle %s", pagesText(item.State), unfinishedCount(item.State), idleText(item.State))))
	box := style.Width(width).Render(body)
	start := len(v.lines)
	for _, line := range strings.Split(box, "\n") {
		v.lines = append(v.lines, v.guide(depth)+line)
	}
	v.rows = append(v.rows, viewRow{kind: rowProject, item: item, start: start, end: len(v.lines) - 1})
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
		v.layout()
		v.ensureVisible()
		return v, nil
	case claudeDoneMsg:
		if msg.err != nil {
			v.errMsg = msg.err.Error()
		} else {
			v.status = "back from Claude Code in " + msg.name
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
		rel := ""
		if v.detail != nil {
			rel = v.detail.Project.Rel
		} else if r := v.current(); r != nil && r.kind == rowProject {
			rel = r.item.Project.Rel
		}
		v.reloadKeeping(rel)
		v.status = "refreshed"
		if v.links != nil {
			v.links.reload(v.items, v.links.item.Project.Rel)
			v.links.status = "refreshed"
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
			return v.openAdd(false)
		case "a":
			return v.openAdd(true)
		case "R":
			return v.refresh()
		}
		if key := msg.String(); key == "o" || key == "c" || key == "e" || key == "i" || key == "l" {
			var item *Item
			if v.detail != nil {
				item = v.detail
			} else if r := v.current(); r != nil && r.kind == rowProject {
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
			case rowCategory:
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

// openAdd starts the add-vault screen, or the adopt screen, in place.
func (v view) openAdd(adopt bool) (tea.Model, tea.Cmd) {
	if v.hooks.Create == nil || v.hooks.Load == nil {
		v.errMsg = "creating vaults is not available here"
		return v, nil
	}
	var known []string
	if v.hooks.Categories != nil {
		known = v.hooks.Categories()
	}
	var m model
	if adopt {
		m = newAdoptModel(known)
	} else {
		m = newModel(v.hooks.VaultsDir, known)
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
	rel, err := v.hooks.Create(*choice)
	if err != nil {
		v.errMsg = err.Error()
		return v, nil
	}
	v.changed = true
	if choice.Adopt {
		v.status = "adopted " + choice.Name
	} else {
		v.status = "created " + choice.Name
	}
	v.reload(rel)
	v.detail = nil
	return v, nil
}

// refresh rebuilds derived state in the background, then reloads the tree.
func (v view) refresh() (tea.Model, tea.Cmd) {
	if v.hooks.Refresh == nil || v.hooks.Load == nil {
		v.errMsg = "refresh is not available here"
		return v, nil
	}
	v.busy = "refreshing every vault…"
	fn := v.hooks.Refresh
	return v, func() tea.Msg { return refreshedMsg{err: fn()} }
}

// openEditor starts editing a project's page in place.
func (v view) openEditor(item *Item) (tea.Model, tea.Cmd) {
	if v.hooks.Load == nil || v.hooks.Update == nil {
		v.errMsg = "editing is not available here"
		return v, nil
	}
	ed := newEditor(v.hooks, item.Project)
	v.edit = &ed
	return v, nil
}

// openLinks shows the project's linked folders as boxes.
func (v view) openLinks(item *Item) (tea.Model, tea.Cmd) {
	if v.hooks.Load == nil || v.hooks.AddLink == nil {
		v.errMsg = "linking is not available here"
		return v, nil
	}
	s := newLinks(v.hooks, item, v.items, v.width)
	v.links = &s
	return v, nil
}

// updateLinks forwards keys to the links screen; after an action it refreshes every
// vault in the background so the facts catch up, and keeps the screen open.
func (v view) updateLinks(msg tea.Msg) (tea.Model, tea.Cmd) {
	s, cmd := v.links.update(msg)
	if s.closed {
		v.links = nil
		v.reloadKeeping(s.item.Project.Rel)
		return v, nil
	}
	v.links = &s
	if s.changed {
		v.links.changed = false
		v.changed = true
		v.reloadKeeping(s.item.Project.Rel)
		v.links.reload(v.items, s.item.Project.Rel)
		if v.hooks.Refresh != nil {
			v.links.status += " · refreshing…"
			fn := v.hooks.Refresh
			return v, tea.Batch(cmd, func() tea.Msg { return refreshedMsg{err: fn()} })
		}
	}
	return v, cmd
}

// nameOf is a project's display name by rel, or the rel when it is unknown.
func (v view) nameOf(rel string) string {
	for _, it := range v.items {
		if it.Project.Rel == rel {
			return it.Project.Name
		}
	}
	return rel
}

// updateEdit forwards keys to the editor and folds its outcome back into the view.
func (v view) updateEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	ed, cmd := v.edit.update(msg)
	switch ed.outcome {
	case editOpen:
		v.edit = &ed
		return v, cmd
	case editSaved:
		v.changed = true
		v.status = "saved " + ed.draft.Name
		v.reloadKeeping(ed.rel)
	case editRemoved:
		v.changed = true
		v.status = fmt.Sprintf("removed %s from the atlas; the vault is still on disk", ed.current.Name)
		v.reload("")
	}
	v.edit = nil
	return v, cmd
}

// openIngest starts the ingest screen for a project.
func (v view) openIngest(item *Item) (tea.Model, tea.Cmd) {
	if v.hooks.StagePlan == nil || v.hooks.Stage == nil {
		v.errMsg = "ingesting is not available here"
		return v, nil
	}
	s := newIngest(v.hooks, item)
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
	item := s.item
	if len(s.linked) > 0 {
		v.changed = true
		v.reloadKeeping(item.Project.Rel)
	}
	waiting := s.plan.Waiting
	if s.result != nil {
		waiting += len(s.result.Staged)
	}
	if s.outcome == ingestStartNow {
		return v.claude(item, claudecode.IngestPrompt)
	}
	v.status = fmt.Sprintf("%d file%s waiting in inbox/; press c and run /claude-atlas:wiki-ingest when ready", waiting, plural(waiting))
	return v, nil
}

// claude hands the terminal to a Claude Code session in the project's vault and resumes after.
func (v view) claude(item *Item, prompt string) (tea.Model, tea.Cmd) {
	if v.opener.Claude == nil {
		v.errMsg = "starting Claude Code is not available here"
		return v, nil
	}
	cmd, err := v.opener.Claude(item.Project.VaultPath(), prompt)
	if err != nil {
		v.errMsg = err.Error()
		return v, nil
	}
	name := item.Project.Name
	return v, tea.ExecProcess(cmd, func(err error) tea.Msg { return claudeDoneMsg{name: name, err: err} })
}

// open starts opening a project's vault, asking first when Obsidian does not know it.
func (v view) open(item *Item) (tea.Model, tea.Cmd) {
	if v.opener.Status == nil {
		v.errMsg = "opening vaults is not available here"
		return v, nil
	}
	registered, running, err := v.opener.Status(item.Project.VaultPath())
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
	name, vault := item.Project.Name, item.Project.VaultPath()
	fn := v.opener.Open
	v.busy = "opening " + name + " in Obsidian…"
	if register {
		fn = v.opener.RegisterAndOpen
		v.busy = "registering " + name + " with Obsidian…"
		if v.askRun {
			v.busy = "registering " + name + " and restarting Obsidian…"
		}
	}
	return v, func() tea.Msg { return openedMsg{name: name, err: fn(vault)} }
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
			errSt.Render("▲"), v.ask.Project.Name, question, title.Render("y"), title.Render("n"))
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
	if v.detail != nil {
		return v.viewDetail()
	}
	var b strings.Builder
	crumb := "all projects"
	if v.root != "" {
		crumb = strings.ReplaceAll(v.root, "/", " › ")
	}
	stamp := ""
	if t, err := time.Parse("2006-01-02T15:04:05Z", v.refreshed); err == nil {
		stamp = "refreshed " + t.Local().Format("2006-01-02 15:04")
	} else {
		stamp = "not refreshed yet"
	}
	fmt.Fprintf(&b, "\n  %s   %s   %s\n\n", title.Render("Atlas"), catSt.Render(crumb), dim.Render(fmt.Sprintf("%d project%s · %s", len(v.items), plural(len(v.items)), stamp)))
	if len(v.lines) == 0 {
		b.WriteString("  " + dim.Render("no projects yet; run `claude-atlas new-vault`") + "\n")
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
	return "n new vault · a adopt · R refresh · - + fold all · q quit"
}

// treeHints lists the keys that do something for the row under the cursor.
func (v view) treeHints() string {
	hints := "↑↓ move"
	if r := v.current(); r != nil {
		switch r.kind {
		case rowProject:
			hints += " · Enter details · o Obsidian · c Claude · i ingest · e edit · l links · Space fold"
		case rowCategory:
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
	p, s := v.detail.Project, v.detail.State
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s\n\n", title.Render(p.Name), dim.Render("tree/"+p.Rel+".md"))
	if s == nil {
		b.WriteString("  " + dim.Render("never refreshed; run `claude-atlas refresh`") + "\n\n")
	} else {
		summary := heatMark(s) + " " + dash(s.Heat)
		if s.Created != "" {
			summary += " · created " + s.Created
		}
		if s.LastTouched != "" {
			summary += " · last touched " + s.LastTouched + " (" + idleText(s) + ")"
		}
		b.WriteString("  " + summary + "\n\n")
	}
	row := func(k, val string) { fmt.Fprintf(&b, "  %s%s\n", label.Width(16).Render(k), val) }
	row("Vault", home.Display(p.VaultPath()))
	row("Category", dash(p.Category()))
	row("Priority", p.Priority)
	row("State", p.State)
	row("Blocked on", dash(p.BlockedOn))
	row("Review after", dash(p.ReviewAfter))
	if s != nil {
		row("Pages", pagesText(s))
		u := s.Unfinished
		var bits []string
		for _, kv := range []struct {
			k string
			v *int
		}{{"empty sections", u.EmptySections}, {"seed pages", u.SeedPages}, {"dead links", u.DeadLinks}} {
			if kv.v != nil {
				bits = append(bits, fmt.Sprintf("%d %s", *kv.v, kv.k))
			}
		}
		row("Unfinished", dash(strings.Join(bits, " · ")))
		row("Last operation", dash(s.LastOperation))
		check := okSt.Render("ok")
		if !s.VaultOK {
			check = errSt.Render(dash(s.VaultError))
		}
		row("Vault check", check)
		if t, err := time.Parse("2006-01-02T15:04:05Z", s.GeneratedAt); err == nil {
			row("Refreshed", t.Local().Format("2006-01-02 15:04"))
		}
	}
	section := func(name, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		b.WriteString("\n  " + catSt.Render(name) + "\n")
		for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
			b.WriteString("    " + line + "\n")
		}
	}
	if len(p.Linked) > 0 {
		b.WriteString("\n  " + catSt.Render("Links") + "\n")
		for _, l := range p.Linked {
			name := l.Name
			if name == "" {
				name = home.Display(l.Path)
			}
			where := dim.Render(l.Kind + " · " + home.Display(l.Path))
			if l.Path == "" {
				name, where = l.Entry, errSt.Render("names no page")
			}
			fmt.Fprintf(&b, "    %-24s %s\n", name, where)
			if s != nil {
				for _, f := range s.Links {
					if f.Path == l.Path && f.Kind == l.Kind {
						facts := refresh.LinkSummary(f)
						if !f.OK {
							facts = errSt.Render(facts)
						}
						b.WriteString("               " + dim.Render(facts) + "\n")
					}
				}
			}
		}
	}
	if from := tree.RelatedFrom(v.projects(), p); len(p.RelatedTo)+len(from) > 0 {
		b.WriteString("\n  " + catSt.Render("Related") + "\n")
		for _, rel := range p.RelatedTo {
			fmt.Fprintf(&b, "    %-24s %s\n", v.nameOf(rel), dim.Render(rel))
		}
		for _, q := range from {
			fmt.Fprintf(&b, "    %-24s %s\n", q.Name, dim.Render(q.Rel+"  · related from its page"))
		}
	}

	section("Purpose", p.Purpose)
	section("Done when", p.DefinitionOfDone)
	if s != nil && len(s.OpenThreads) > 0 {
		b.WriteString("\n  " + catSt.Render("Open threads") + "\n")
		for _, t := range s.OpenThreads {
			b.WriteString("    - " + t + "\n")
		}
	}
	b.WriteString("\n" + v.footer("o Obsidian · c Claude Code · i ingest · e edit · l links · R refresh · Esc back · q quit"))
	return b.String()
}

// projects lists every project the view holds.
func (v view) projects() []*tree.Project {
	out := make([]*tree.Project, 0, len(v.items))
	for i := range v.items {
		out = append(out, v.items[i].Project)
	}
	return out
}

// RunView shows the tree until the user quits. It reports whether any project was edited.
func RunView(items []Item, opener Opener, hooks Hooks) (bool, error) {
	final, err := tea.NewProgram(newView(items, opener, hooks), tea.WithAltScreen()).Run()
	if err != nil {
		return false, fmt.Errorf("interactive screen failed: %w", err)
	}
	return final.(view).changed, nil
}
