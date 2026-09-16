package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/vault"
)

// muted replaces gray for secondary text; gray is unreadable on dark terminals.
const muted = lipgloss.Color("#FFC600")

// The kind colors. A project's name is blue and a knowledge base's name is green,
// everywhere a name appears.
const (
	projectColor   = lipgloss.Color("12")
	knowledgeColor = lipgloss.Color("2")
)

var (
	title       = lipgloss.NewStyle().Bold(true)
	label       = lipgloss.NewStyle().Foreground(muted).Width(11)
	activeL     = lipgloss.NewStyle().Foreground(projectColor).Bold(true).Width(11)
	dim         = lipgloss.NewStyle().Foreground(muted)
	value       = lipgloss.NewStyle()
	cursorSt    = lipgloss.NewStyle().Foreground(projectColor).Bold(true)
	errSt       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	rule        = lipgloss.NewStyle().Foreground(muted)
	selSt       = lipgloss.NewStyle().Foreground(projectColor).Bold(true)
	okSt        = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	modSt       = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F00"))
	catSt       = lipgloss.NewStyle().Bold(true)
	boxSt       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(0, 1)
	boxSelSt    = boxSt.BorderForeground(projectColor)
	projectSt   = lipgloss.NewStyle().Foreground(projectColor).Bold(true)
	knowledgeSt = lipgloss.NewStyle().Foreground(knowledgeColor).Bold(true)
)

// kindColor is the color a vault's kind wears.
func kindColor(k vault.Kind) lipgloss.Color {
	if k == vault.Knowledge {
		return knowledgeColor
	}
	return projectColor
}

// kindStyle renders a vault's name in its kind's color.
func kindStyle(k vault.Kind) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(kindColor(k)).Bold(true)
}

// boxStyle is a vault's box; the border takes the kind's color when selected.
func boxStyle(k vault.Kind, selected bool) lipgloss.Style {
	if selected {
		return boxSt.BorderForeground(kindColor(k))
	}
	return boxSt
}

// focus keeps a row's colors when it is under the cursor and strips them otherwise, so
// the selected row is the colored one and every other row reads plain.
func focus(lines []string, selected bool) []string {
	if selected {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = stripANSI(line)
	}
	return out
}

// stripANSI drops escape sequences: it measures styled text and renders a row plain.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
		case r == 0x1b:
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
