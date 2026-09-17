// Package tui holds the interactive screens behind bare CLI commands.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// AddVault is what the user chose on the add-vault or adopt screen.
type AddVault struct {
	Kind  vault.Kind
	Name  string // display name as typed, or the folder's name when adopting
	Path  string // where the vault will be created, or the vault being adopted
	Mode  string // generic or lyt
	Tags  []string
	Scope string
	Adopt bool // Path exists already and is adopted rather than created
	// MountID is the knowledge base a new project mounts once it exists; "" mounts none.
	// The mount asks for write, and the mounts screen changes that.
	MountID string
}

type step int

const (
	stepKind step = iota
	stepName
	stepMode
	stepFacts // tags for a project, scope for a knowledge base
	stepMount // the knowledge base a new project mounts, or none
	stepPath
	stepConfirm
)

// fieldPad indents what sits under a field: two spaces and the label.
const fieldPad = "             "

// model is the add-vault screen; with adopting set it takes the path of a vault that
// exists instead of making one.
type model struct {
	vaultsDir string
	adopting  bool
	steps     []step
	at        int
	kind      vault.Kind
	name      textinput.Model
	mode      string
	tags      textinput.Model
	scope     textinput.Model
	path      pathField
	edited    bool // the user typed a path of their own
	kbs       []registry.Entry
	kb        int // an index into kbs, or len(kbs) for no mount
	err       string
	done      bool
	cancelled bool
}

// withKnowledge lists the knowledge bases a new project may mount, so the screen offers
// them instead of asking for a name.
func (m model) withKnowledge(items []Item) model {
	for _, it := range items {
		if it.Entry.Error == "" && it.Entry.Kind == vault.Knowledge {
			m.kbs = append(m.kbs, it.Entry)
		}
	}
	m.kb = len(m.kbs)
	return m
}

// mountChoice is the knowledge base the mount step stands on, or nil for none.
func (m model) mountChoice() *registry.Entry {
	if m.kb < 0 || m.kb >= len(m.kbs) {
		return nil
	}
	return &m.kbs[m.kb]
}

// applies says whether a step belongs in this run: only a new project with a knowledge
// base to reach is asked what to mount.
func (m model) applies(s step) bool {
	if s == stepMount {
		return !m.adopting && m.kind == vault.Project && len(m.kbs) > 0
	}
	return true
}

func newInput(placeholder string) textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.CharLimit = 200
	input.Width = 60
	return input
}

func newModel(vaultsDir string, kind vault.Kind) model {
	m := model{
		vaultsDir: vaultsDir,
		steps:     []step{stepKind, stepName, stepMode, stepFacts, stepMount, stepPath, stepConfirm},
		kind:      kind,
		mode:      string(vault.Generic),
		name:      newInput("sensor-triage"),
		tags:      newInput("usc, fall (optional)"),
		scope:     newInput("one or two sentences on what it covers (optional)"),
		path:      newPathField("", 60),
	}
	return m
}

// newAdoptModel is the same screen for a vault that already exists: it asks the path
// first and never computes one.
func newAdoptModel() model {
	m := newModel("", vault.Project)
	m.adopting = true
	m.steps = []step{stepPath, stepKind, stepName, stepMode, stepConfirm}
	m.path = newPathField("~/Documents/OldVault", 60)
	m.path.focus()
	return m
}

func (m model) Init() tea.Cmd { return textinput.Blink }

// step is where the user stands.
func (m model) step() step {
	if m.at >= len(m.steps) {
		return stepConfirm
	}
	return m.steps[m.at]
}

func (m model) vaultName() string { return strings.TrimSpace(m.name.Value()) }

// defaultPath is where a new vault goes unless the user types a location.
func (m model) defaultPath() string {
	if m.adopting || m.vaultName() == "" {
		return ""
	}
	return vaults.PathFor(m.vaultsDir, m.kind, m.vaultName())
}

// target is the vault's path: the one typed, else the default.
func (m model) target() string {
	typed := m.path.value()
	if typed == "" {
		typed = home.Display(m.defaultPath())
	}
	if typed == "" {
		return ""
	}
	abs, err := filepath.Abs(home.Expand(typed))
	if err != nil {
		return ""
	}
	return abs
}

func (m model) nameError() string {
	switch name := m.vaultName(); {
	case name == "":
		return "type a name"
	case strings.ContainsAny(name, `/\`):
		return `a name carries no "/"; the path step says where the vault goes`
	}
	return ""
}

func (m model) pathError() string {
	if !m.adopting {
		if m.target() == "" {
			return "type where the vault goes"
		}
		if err := vaults.CheckNewPath(m.target()); err != nil {
			return err.Error()
		}
		return ""
	}
	if m.path.value() == "" {
		return "type the vault's path"
	}
	info, err := os.Stat(m.target())
	if err != nil || !info.IsDir() {
		return home.Display(m.target()) + " is not a directory"
	}
	if !vault.IsAdoptable(m.target()) {
		return home.Display(m.target()) + " is not a vault: no .obsidian/, wiki/, or identity file"
	}
	return ""
}

// focus puts the cursor in the field of the current step and fills in what follows from
// the answers so far.
func (m *model) focus() tea.Cmd {
	m.name.Blur()
	m.tags.Blur()
	m.scope.Blur()
	m.path.blur()
	switch m.step() {
	case stepName:
		if m.adopting && m.vaultName() == "" {
			m.name.SetValue(filepath.Base(m.target()))
			m.name.CursorEnd()
		}
		return m.name.Focus()
	case stepFacts:
		if m.kind == vault.Knowledge {
			return m.scope.Focus()
		}
		return m.tags.Focus()
	case stepPath:
		if !m.adopting && !m.edited {
			m.path.setValue(home.Display(m.defaultPath()))
		}
		return m.path.focus()
	}
	return nil
}

// toggle swaps a two-value field: the kind or the mode.
func (m *model) toggle() {
	switch m.step() {
	case stepKind:
		if m.kind == vault.Project {
			m.kind = vault.Knowledge
		} else {
			m.kind = vault.Project
		}
	case stepMode:
		if m.mode == string(vault.Generic) {
			m.mode = string(vault.LYT)
		} else {
			m.mode = string(vault.Generic)
		}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch key.Type {
		case tea.KeyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEsc:
			if m.at == 0 {
				m.cancelled = true
				return m, tea.Quit
			}
			m.at--
			for m.at > 0 && !m.applies(m.step()) {
				m.at--
			}
			m.err = ""
			return m, m.focus()
		case tea.KeyEnter:
			return m.advance()
		}
	}
	var cmd tea.Cmd
	switch m.step() {
	case stepKind, stepMode:
		if isKey && (key.Type == tea.KeyLeft || key.Type == tea.KeyRight || key.Type == tea.KeySpace) {
			m.toggle()
		}
	case stepMount:
		if isKey {
			switch key.Type {
			case tea.KeyLeft, tea.KeyUp:
				if m.kb > 0 {
					m.kb--
				}
			case tea.KeyRight, tea.KeyDown, tea.KeySpace:
				if m.kb < len(m.kbs) {
					m.kb++
				}
			}
		}
	case stepName:
		m.name, cmd = m.name.Update(msg)
		m.err = ""
	case stepFacts:
		if m.kind == vault.Knowledge {
			m.scope, cmd = m.scope.Update(msg)
		} else {
			m.tags, cmd = m.tags.Update(msg)
		}
	case stepPath:
		m.path, cmd = m.path.update(msg)
		m.edited = m.path.value() != home.Display(m.defaultPath())
		m.err = ""
	}
	return m, cmd
}

func (m model) advance() (tea.Model, tea.Cmd) {
	switch m.step() {
	case stepName:
		if e := m.nameError(); e != "" {
			m.err = e
			return m, nil
		}
	case stepPath:
		if e := m.pathError(); e != "" {
			m.err = e
			return m, nil
		}
	case stepConfirm:
		m.done = true
		return m, tea.Quit
	}
	m.at++
	for m.at < len(m.steps) && !m.applies(m.step()) {
		m.at++
	}
	m.err = ""
	return m, m.focus()
}

// splitTags reads a comma-separated tag list.
func splitTags(s string) []string {
	var out []string
	for _, tag := range strings.Split(s, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

// result is the choice once the user confirmed, or nil.
func (m model) result() *AddVault {
	if m.cancelled || !m.done {
		return nil
	}
	out := &AddVault{Kind: m.kind, Name: m.vaultName(), Path: m.target(), Mode: m.mode, Adopt: m.adopting}
	if m.kind == vault.Knowledge {
		out.Scope = strings.TrimSpace(m.scope.Value())
	} else {
		out.Tags = splitTags(m.tags.Value())
		if m.applies(stepMount) {
			if kb := m.mountChoice(); kb != nil {
				out.MountID = kb.ID
			}
		}
	}
	return out
}

// kindHint says what a kind is for.
func kindHint(kind vault.Kind) string {
	if kind == vault.Knowledge {
		return "sources, entities, concepts; projects mount it"
	}
	return "tasks, questions, notes, repositories"
}

func modeHint(mode string) string {
	if mode == string(vault.LYT) {
		return "atomic notes under Maps of Content"
	}
	return "pages filed by type"
}

func (m model) row(s step, name, content string) string {
	l := label
	if m.step() == s {
		l = activeL
	}
	out := "  " + l.Render(name) + content + "\n"
	if m.step() == s && m.err != "" {
		out += fieldPad + errSt.Render(m.err) + "\n"
	}
	return out
}

// content is what one step shows: its field when the user stands on it, the answer once
// it is behind them, and a hint before that.
func (m model) content(s step) string {
	at, done := m.step() == s, m.stepDone(s)
	switch s {
	case stepKind:
		if at {
			return "◂ " + string(m.kind) + " ▸" + dim.Render("  "+kindHint(m.kind))
		}
		if !done {
			return dim.Render(string(m.kind))
		}
		return string(m.kind)
	case stepName:
		if at {
			return m.name.View()
		}
		if !done {
			return dim.Render("what to call it")
		}
		return m.vaultName()
	case stepMode:
		if at {
			return "◂ " + m.mode + " ▸" + dim.Render("  "+modeHint(m.mode))
		}
		if !done {
			return dim.Render(m.mode)
		}
		return m.mode
	case stepMount:
		name, hint := "none", "this project reaches no knowledge base yet"
		if kb := m.mountChoice(); kb != nil {
			name = kb.Name
			hint = "mounted at kb/" + kb.Name + " with write access; m on the project changes it"
			// Mounting a cluster reaches every member, so the count belongs on the choice.
			if n := len(kb.Members); vaults.IsCluster(*kb) {
				name += fmt.Sprintf("   %d member%s", n, plural(n))
				hint = fmt.Sprintf("a cluster: the project reaches its %d member%s too", n, plural(n))
			}
		}
		if at {
			return "◂ " + name + " ▸" + dim.Render("  "+hint)
		}
		if !done {
			return dim.Render("a knowledge base to mount, or none")
		}
		if name == "none" {
			return dim.Render(name)
		}
		return name
	case stepFacts:
		if m.kind == vault.Knowledge {
			if at {
				return m.scope.View()
			}
			if !done {
				return dim.Render("optional; one or two sentences on what it covers")
			}
			if text := strings.TrimSpace(m.scope.Value()); text != "" {
				return text
			}
			return dim.Render("none")
		}
		if at {
			return m.tags.View()
		}
		if !done {
			return dim.Render("optional; the first tag is the folder in the view")
		}
		if tags := splitTags(m.tags.Value()); len(tags) > 0 {
			return strings.Join(tags, ", ")
		}
		return dim.Render("none")
	default:
		if at {
			return m.path.view(fieldPad)
		}
		if !done && !m.adopting {
			if path := m.defaultPath(); path != "" {
				return dim.Render("→ " + home.Display(path))
			}
			return dim.Render("follows the kind and the name")
		}
		return home.Display(m.target())
	}
}

// stepDone reports whether the user has answered a step already.
func (m model) stepDone(s step) bool {
	for _, done := range m.steps[:m.at] {
		if done == s {
			return true
		}
	}
	return false
}

// stepLabel is the label of one step.
func stepLabel(s step, kind vault.Kind) string {
	switch s {
	case stepKind:
		return "Kind"
	case stepName:
		return "Name"
	case stepMode:
		return "Mode"
	case stepMount:
		return "Mounts"
	case stepFacts:
		if kind == vault.Knowledge {
			return "Scope"
		}
		return "Tags"
	default:
		return "Path"
	}
}

func (m model) View() string {
	var b strings.Builder
	heading := "Add a vault"
	if m.adopting {
		heading = "Adopt a vault"
	}
	b.WriteString("\n  " + title.Render(heading) + "\n\n")
	for _, s := range m.steps {
		if s == stepConfirm || !m.applies(s) {
			continue
		}
		b.WriteString(m.row(s, stepLabel(s, m.kind), m.content(s)))
		b.WriteString("\n")
	}
	if m.step() == stepConfirm {
		verb := "create this vault"
		if m.adopting {
			verb = "adopt this vault"
		}
		b.WriteString("  " + rule.Render(strings.Repeat("─", 56)) + "\n")
		b.WriteString("  " + label.Render("Vault") + value.Render(home.Display(m.target())) + "\n")
		b.WriteString("\n  " + title.Render("Enter") + " " + verb + "   " + dim.Render("Esc back") + "\n")
		return b.String()
	}
	hints := "Enter next"
	switch m.step() {
	case stepKind:
		hints += " · ←→ project or knowledge base"
	case stepMode:
		hints += " · ←→ generic or lyt"
	case stepMount:
		hints += " · ←→ which knowledge base"
	case stepPath:
		hints += " · " + pathHint()
		if !m.adopting {
			hints += " · type another path to put the vault elsewhere"
		}
	}
	if m.at > 0 {
		hints += " · Esc back"
	} else {
		hints += " · Esc cancel"
	}
	b.WriteString("  " + dim.Render(hints) + "\n")
	return b.String()
}

func runChoice(m model) (*AddVault, error) {
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, fmt.Errorf("interactive screen failed: %w", err)
	}
	return final.(model).result(), nil
}

// RunAddVault shows the add screen and returns nil when the user cancels.
func RunAddVault(vaultsDir string, kind vault.Kind) (*AddVault, error) {
	return runChoice(newModel(vaultsDir, kind))
}

// RunAdopt shows the adopt screen and returns nil when the user cancels.
func RunAdopt() (*AddVault, error) { return runChoice(newAdoptModel()) }
