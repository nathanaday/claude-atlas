package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const topLevel = "(top level)"

// option is one row of a picker.
type option struct {
	label  string // what the row shows
	value  string // what choosing it means; "" for the top level
	match  string // what the filter matches against; the label when empty
	create bool   // the row creates a new category
}

// categoryOptions filters known categories by the typed text and offers to create a new one.
func categoryOptions(known []string, typed string) []option {
	typed = strings.Trim(strings.TrimSpace(typed), "/")
	var opts []option
	if typed == "" {
		opts = append(opts, option{label: topLevel, value: ""})
	}
	exact := false
	for _, cat := range known {
		if typed == "" || strings.Contains(strings.ToLower(cat), strings.ToLower(typed)) {
			opts = append(opts, option{label: cat, value: cat})
		}
		if strings.EqualFold(cat, typed) {
			exact = true
		}
	}
	if typed != "" && !exact {
		opts = append(opts, option{label: typed, value: typed, create: true})
	}
	return opts
}

// filterOptions keeps the options whose match text contains the typed text.
func filterOptions(all []option, typed string) []option {
	typed = strings.ToLower(strings.TrimSpace(typed))
	var opts []option
	for _, opt := range all {
		text := opt.match
		if text == "" {
			text = opt.label
		}
		if typed == "" || strings.Contains(strings.ToLower(text), typed) {
			opts = append(opts, opt)
		}
	}
	return opts
}

// picker is a filterable list. The category picker also accepts a new name.
type picker struct {
	options func(typed string) []option
	input   textinput.Model
	cursor  int
}

func newInput(placeholder string) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.Prompt = ""
	input.CharLimit = 120
	return input
}

func newPicker(known []string) picker {
	return picker{options: func(typed string) []option { return categoryOptions(known, typed) }, input: newInput("type to filter, or a new name")}
}

// newOptionPicker chooses among fixed options.
func newOptionPicker(all []option) picker {
	return picker{options: func(typed string) []option { return filterOptions(all, typed) }, input: newInput("type to filter")}
}

func (p picker) selected() option {
	opts := p.options(p.input.Value())
	if len(opts) == 0 {
		return option{}
	}
	if p.cursor >= len(opts) {
		return opts[0]
	}
	return opts[p.cursor]
}

func (p *picker) reset() {
	p.input.SetValue("")
	p.cursor = 0
}

func (p *picker) focus() tea.Cmd { return p.input.Focus() }
func (p *picker) blur()          { p.input.Blur() }

// update handles movement keys itself and passes everything else to the input.
func (p picker) update(msg tea.Msg) (picker, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		n := len(p.options(p.input.Value()))
		switch key.Type {
		case tea.KeyUp:
			if n > 0 {
				p.cursor = (p.cursor + n - 1) % n
			}
			return p, nil
		case tea.KeyDown:
			if n > 0 {
				p.cursor = (p.cursor + 1) % n
			}
			return p, nil
		}
	}
	before := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		p.cursor = 0
	}
	return p, cmd
}

// view renders the input line and the option rows, indented by pad.
func (p picker) view(pad string) string {
	var b strings.Builder
	b.WriteString(p.input.View() + "\n")
	opts := p.options(p.input.Value())
	if len(opts) == 0 {
		b.WriteString(pad + dim.Render("no match") + "\n")
	}
	for i, opt := range opts {
		marker := "  "
		text := opt.label
		if opt.create {
			text = newSt.Render("+ new category: " + opt.label)
		}
		if i == p.cursor {
			marker = cursorSt.Render("▸ ")
			if !opt.create {
				text = cursorSt.Render(text)
			}
		}
		b.WriteString(pad + marker + text + "\n")
	}
	return b.String()
}
