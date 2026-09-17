package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/registry"
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
	dim         = lipgloss.NewStyle().Foreground(muted)
	errSt       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	selSt       = lipgloss.NewStyle().Foreground(projectColor).Bold(true)
	okSt        = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	boxSt       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(0, 1)
	projectSt   = lipgloss.NewStyle().Foreground(projectColor).Bold(true)
	knowledgeSt = lipgloss.NewStyle().Foreground(knowledgeColor).Bold(true)
	// captionSt is the sentence under the tab bar: a quiet gray aside, not a second row
	// of keys.
	captionSt = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
)

// kindColor is the color an entry's kind wears.
func kindColor(k registry.Kind) lipgloss.Color {
	if k == registry.Knowledge {
		return knowledgeColor
	}
	return projectColor
}

// kindStyle renders an entry's name in its kind's color.
func kindStyle(k registry.Kind) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(kindColor(k)).Bold(true)
}

// boxStyle is an entry's box; the border takes the kind's color when selected.
func boxStyle(k registry.Kind, selected bool) lipgloss.Style {
	if selected {
		return boxSt.BorderForeground(kindColor(k))
	}
	return boxSt
}

// onFill is the text color that reads on a filled tab: dark on the light fills, white on
// the rest.
func onFill(c lipgloss.Color) lipgloss.Color {
	if c == muted {
		return lipgloss.Color("0")
	}
	return lipgloss.Color("15")
}

// tabStyle is one tab in the bar: the active tab is filled with its color and its text
// takes the color that reads on that fill; the others are plain text. Both carry a space
// on each side, so every tab reads as a box.
func tabStyle(c lipgloss.Color, active bool) lipgloss.Style {
	s := lipgloss.NewStyle().Padding(0, 1)
	if active {
		return s.Background(c).Foreground(onFill(c)).Bold(true)
	}
	return s
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
