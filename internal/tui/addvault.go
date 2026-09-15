// Package tui holds the interactive screens behind bare CLI commands.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/tree"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// AddVault is what the user chose on the add-vault or adopt screen.
type AddVault struct {
	Name     string // display name as typed, or the directory name when adopting
	Slug     string // file and directory name
	Path     string // where the vault will be created, or the vault being adopted
	Category string // "" for the top level
	Mode     string // generic or lyt
	Purpose  string
	Adopt    bool // Path exists already and is adopted rather than created
}

type step int

const (
	stepName step = iota
	stepCategory
	stepLocation
	stepMode
	stepPurpose
	stepConfirm
	stepCount
)

// muted replaces gray for secondary text; gray is unreadable on dark terminals.
const muted = lipgloss.Color("#FFC600")

var (
	title    = lipgloss.NewStyle().Bold(true)
	label    = lipgloss.NewStyle().Foreground(muted).Width(11)
	activeL  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Width(11)
	dim      = lipgloss.NewStyle().Foreground(muted)
	value    = lipgloss.NewStyle()
	cursorSt = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	newSt    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	errSt    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	rule     = lipgloss.NewStyle().Foreground(muted)
)

// model is the add-vault screen; with adopting set it takes an existing vault's path instead of a name.
type model struct {
	vaultsDir  string
	adopting   bool
	categories []string
	step       step
	name       textinput.Model
	where      pathField // the vault's path when adopting
	category   picker
	location   pathField // where a new vault goes; it follows the category until edited
	edited     bool      // the user typed a location of their own
	mode       string
	purpose    textinput.Model
	chosen     option
	err        string
	done       bool
	cancelled  bool
}

func newModel(vaultsDir string, categories []string) model {
	name := textinput.New()
	name.Placeholder = "sensor-triage"
	name.Prompt = ""
	name.CharLimit = 80
	name.Focus()
	category := newPicker(categories)
	purpose := textinput.New()
	purpose.Placeholder = "one line on why this exists (optional)"
	purpose.Prompt = ""
	purpose.CharLimit = 200
	purpose.Width = 60
	return model{vaultsDir: vaultsDir, categories: categories, name: name, category: category, location: newPathField("", 60), mode: string(vault.Generic), purpose: purpose}
}

// newAdoptModel is the same screen for a vault that already exists.
func newAdoptModel(categories []string) model {
	m := newModel("", categories)
	m.adopting = true
	m.name.Blur()
	m.where = newPathField("~/Documents/OldVault", 60)
	m.where.focus()
	return m
}

func (m model) Init() tea.Cmd { return textinput.Blink }

// typed is the text of the first step: a name, or a path when adopting.
func (m model) typed() string {
	if m.adopting {
		return m.where.value()
	}
	return strings.TrimSpace(m.name.Value())
}

func (m model) slug() string {
	source := m.typed()
	if m.adopting {
		source = filepath.Base(m.path())
	}
	slug, err := tree.Slugify(source)
	if err != nil {
		return ""
	}
	return slug
}

// defaultPath is where a new vault goes in a category unless the user types a location.
func (m model) defaultPath(category string) string {
	path, err := vaults.DefaultPath(m.vaultsDir, category, m.slug())
	if err != nil {
		return ""
	}
	return path
}

func (m model) path() string {
	typed := m.typed()
	if !m.adopting {
		if !m.edited {
			return m.defaultPath(m.chosen.value)
		}
		typed = m.location.value()
	}
	abs, err := filepath.Abs(home.Expand(typed))
	if err != nil {
		return ""
	}
	return abs
}

func (m model) nameError() string {
	if m.adopting {
		return m.pathError()
	}
	if m.typed() == "" {
		return "type a name"
	}
	if m.slug() == "" {
		return "the name needs at least one letter or digit"
	}
	return ""
}

func (m model) locationError() string {
	if m.edited && m.location.value() == "" {
		return "type where the vault goes"
	}
	if err := vaults.CheckNewPath(m.path()); err != nil {
		return err.Error()
	}
	return ""
}

func (m model) pathError() string {
	if m.typed() == "" {
		return "type the vault's path"
	}
	info, err := os.Stat(m.path())
	if err != nil || !info.IsDir() {
		return home.Display(m.path()) + " is not a directory"
	}
	if !vault.IsAdoptable(m.path()) {
		return home.Display(m.path()) + " is not a vault: no .obsidian/, wiki/, or identity file"
	}
	if m.slug() == "" {
		return "the directory name needs at least one letter or digit"
	}
	return ""
}

// move gives the step before or after this one; adopting skips the location, which the
// first step gave.
func (m model) move(by int) step {
	s := m.step + step(by)
	if m.adopting && s == stepLocation {
		s += step(by)
	}
	return s
}

func (m *model) focus() tea.Cmd {
	m.name.Blur()
	m.where.blur()
	m.category.blur()
	m.location.blur()
	m.purpose.Blur()
	switch m.step {
	case stepName:
		if m.adopting {
			return m.where.focus()
		}
		return m.name.Focus()
	case stepCategory:
		return m.category.focus()
	case stepLocation:
		if !m.edited {
			m.location.setValue(home.Display(m.defaultPath(m.chosen.value)))
		}
		return m.location.focus()
	case stepPurpose:
		return m.purpose.Focus()
	}
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch key.Type {
		case tea.KeyCtrlC:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEsc:
			if m.step == stepName {
				m.cancelled = true
				return m, tea.Quit
			}
			m.step = m.move(-1)
			m.err = ""
			return m, m.focus()
		case tea.KeyEnter:
			return m.advance()
		}
	}
	var cmd tea.Cmd
	switch m.step {
	case stepName:
		if m.adopting {
			m.where, cmd = m.where.update(msg)
		} else {
			m.name, cmd = m.name.Update(msg)
		}
		m.err = ""
	case stepCategory:
		m.category, cmd = m.category.update(msg)
		m.err = ""
	case stepLocation:
		m.location, cmd = m.location.update(msg)
		m.edited = m.location.value() != home.Display(m.defaultPath(m.chosen.value))
		m.err = ""
	case stepMode:
		if isKey && (key.Type == tea.KeyLeft || key.Type == tea.KeyRight || key.Type == tea.KeySpace) {
			if m.mode == string(vault.Generic) {
				m.mode = string(vault.LYT)
			} else {
				m.mode = string(vault.Generic)
			}
		}
	case stepPurpose:
		m.purpose, cmd = m.purpose.Update(msg)
	}
	return m, cmd
}

func (m model) advance() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepName:
		if e := m.nameError(); e != "" {
			m.err = e
			return m, nil
		}
	case stepCategory:
		chosen := m.category.selected()
		if _, err := tree.CleanCategory(chosen.value); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.chosen = chosen
	case stepLocation:
		if e := m.locationError(); e != "" {
			m.err = e
			return m, nil
		}
	case stepConfirm:
		m.done = true
		return m, tea.Quit
	}
	m.step = m.move(1)
	m.err = ""
	return m, m.focus()
}

func (m model) pagePath() string {
	if m.chosen.value == "" {
		return "tree/" + m.slug() + ".md"
	}
	return "tree/" + m.chosen.value + "/" + m.slug() + ".md"
}

// result is the choice once the user confirmed, or nil.
func (m model) result() *AddVault {
	if m.cancelled || !m.done {
		return nil
	}
	name := m.typed()
	if m.adopting {
		name = filepath.Base(m.path())
	}
	return &AddVault{
		Name:     name,
		Slug:     m.slug(),
		Path:     m.path(),
		Category: m.chosen.value,
		Mode:     m.mode,
		Purpose:  strings.TrimSpace(m.purpose.Value()),
		Adopt:    m.adopting,
	}
}

func (m model) View() string {
	var b strings.Builder
	heading, first := "Add a vault", "Name"
	if m.adopting {
		heading, first = "Adopt a vault", "Path"
	}
	b.WriteString("\n  " + title.Render(heading) + "\n\n")

	if m.adopting && m.step == stepName {
		b.WriteString(m.row(stepName, first, m.where.view("             ")))
	} else if m.adopting {
		b.WriteString(m.row(stepName, first, m.typed()))
	} else {
		b.WriteString(m.row(stepName, first, m.name.View()))
	}
	if m.step == stepName && m.err != "" {
		b.WriteString("             " + errSt.Render(m.err) + "\n")
	}
	b.WriteString("\n")

	switch {
	case m.step < stepCategory:
		b.WriteString(m.row(stepCategory, "Category", dim.Render("choose after the "+strings.ToLower(first))))
	case m.step == stepCategory:
		b.WriteString(m.row(stepCategory, "Category", m.category.view("             ")))
		if m.err != "" {
			b.WriteString("             " + errSt.Render(m.err) + "\n")
		}
	default:
		shown := m.chosen.label
		if m.chosen.create {
			shown += dim.Render("  (new)")
		}
		b.WriteString(m.row(stepCategory, "Category", shown))
	}
	b.WriteString("\n")

	if !m.adopting {
		switch {
		case m.step < stepLocation:
			hint := "follows the category"
			if path := m.defaultPath(m.category.selected().value); m.step == stepCategory && path != "" {
				hint = "→ " + home.Display(path)
			}
			b.WriteString(m.row(stepLocation, "Location", dim.Render(hint)))
		case m.step == stepLocation:
			b.WriteString(m.row(stepLocation, "Location", m.location.view("             ")))
			if m.err != "" {
				b.WriteString("             " + errSt.Render(m.err) + "\n")
			}
		default:
			b.WriteString(m.row(stepLocation, "Location", home.Display(m.path())))
		}
		b.WriteString("\n")
	}

	switch {
	case m.step < stepMode:
		b.WriteString(m.row(stepMode, "Mode", dim.Render("generic")))
	case m.step == stepMode:
		b.WriteString(m.row(stepMode, "Mode", "◂ "+m.mode+" ▸"+dim.Render("  "+modeHint(m.mode))))
	default:
		b.WriteString(m.row(stepMode, "Mode", m.mode))
	}
	b.WriteString("\n")

	switch {
	case m.step < stepPurpose:
		b.WriteString(m.row(stepPurpose, "Purpose", dim.Render("optional")))
	case m.step == stepPurpose:
		b.WriteString(m.row(stepPurpose, "Purpose", m.purpose.View()))
	default:
		p := m.purpose.Value()
		if p == "" {
			p = dim.Render("none")
		}
		b.WriteString(m.row(stepPurpose, "Purpose", p))
	}
	b.WriteString("\n")

	if m.step == stepConfirm {
		verb := "create this vault"
		if m.adopting {
			verb = "adopt this vault"
		}
		b.WriteString("  " + rule.Render(strings.Repeat("─", 56)) + "\n")
		b.WriteString("  " + label.Render("Vault") + value.Render(home.Display(m.path())) + "\n")
		b.WriteString("  " + label.Render("Page") + value.Render(m.pagePath()) + "\n")
		b.WriteString("\n  " + title.Render("Enter") + " " + verb + "   " + dim.Render("Esc back") + "\n")
	} else {
		hints := "Enter next"
		switch m.step {
		case stepName:
			if m.adopting {
				hints += " · " + pathHint()
			}
		case stepCategory:
			hints += " · ↑↓ choose · type to filter or name a new category"
		case stepLocation:
			hints += " · type another path to put the vault elsewhere · " + pathHint()
		case stepMode:
			hints += " · ←→ generic or lyt"
		}
		if m.step > stepName {
			hints += " · Esc back"
		} else {
			hints += " · Esc cancel"
		}
		b.WriteString("  " + dim.Render(hints) + "\n")
	}
	return b.String()
}

func modeHint(mode string) string {
	if mode == string(vault.LYT) {
		return "atomic notes under Maps of Content"
	}
	return "pages filed by type"
}

func (m model) row(s step, name, content string) string {
	l := label
	if m.step == s {
		l = activeL
	}
	return "  " + l.Render(name) + content + "\n"
}

// Categories lists every directory under the tree root, nested paths included.
func Categories(treeRoot string) []string {
	var cats []string
	filepath.WalkDir(treeRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == treeRoot {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(treeRoot, path)
		cats = append(cats, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(cats)
	return cats
}

func runChoice(m model) (*AddVault, error) {
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, fmt.Errorf("interactive screen failed: %w", err)
	}
	return final.(model).result(), nil
}

// RunAddVault shows the screen and returns nil when the user cancels.
func RunAddVault(vaultsDir string, categories []string) (*AddVault, error) {
	return runChoice(newModel(vaultsDir, categories))
}

// RunAdopt shows the adopt screen and returns nil when the user cancels.
func RunAdopt(categories []string) (*AddVault, error) {
	return runChoice(newAdoptModel(categories))
}
