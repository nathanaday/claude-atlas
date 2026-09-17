package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

// detailWidth is the label column of the expanded block.
const detailWidth = 15

// maxDetailTasks bounds the open tasks an expanded project lists.
const maxDetailTasks = 8

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

func pagesText(state *registry.State) string {
	if state == nil || state.Pages == nil {
		return "—"
	}
	return fmt.Sprint(*state.Pages)
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// taskSummaryText is one line of task counts for the expanded block.
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

// touchedText says when an entry was last touched.
func touchedText(s *registry.State) string {
	switch {
	case s == nil:
		return "not refreshed"
	case s.DaysIdle == nil:
		return "new"
	case *s.DaysIdle == 0:
		return "touched today"
	default:
		return fmt.Sprintf("idle %dd", *s.DaysIdle)
	}
}

// taskCountText is a project's open tasks for its box; "" before a refresh counted them.
func taskCountText(s *registry.State) string {
	if s == nil || s.Tasks == nil {
		return ""
	}
	n := s.Tasks.Counts.Open
	if n == 0 {
		return "no tasks"
	}
	return fmt.Sprintf("%d task%s open", n, plural(n))
}

// currentPhase is the first phase that still holds open work, or "".
func currentPhase(s *registry.State) string {
	if s == nil || s.Tasks == nil || len(s.Tasks.Phases) == 0 {
		return ""
	}
	return s.Tasks.Phases[0]
}

// inboxText is a knowledge base's waiting sources; "" before a refresh counted them.
func inboxText(s *registry.State) string {
	if s == nil || s.Inbox == nil {
		return ""
	}
	if *s.Inbox == 0 {
		return "inbox empty"
	}
	return fmt.Sprintf("%d in inbox", *s.Inbox)
}

// clip shortens text to width runes, the last one an ellipsis.
func clip(s string, width int) string {
	r := []rune(s)
	if len(r) <= width || width < 2 {
		return s
	}
	return string(r[:width-1]) + "…"
}

// boxLines is the lines of an entry's box: the heat mark and the name, then the facts.
// A problem shows its folder and its error.
func boxLines(e registry.Entry, width int) []string {
	if e.Error != "" {
		return []string{errSt.Render("✗ ") + filepath.Base(e.Path), errSt.Render(clip(e.Error, width))}
	}
	name := kindStyle(e.Kind).Render(entryName(e))
	s := e.State
	if e.Kind == registry.Knowledge {
		facts := []string{pagesText(s) + " pages"}
		if t := inboxText(s); t != "" {
			facts = append(facts, t)
		}
		facts = append(facts, touchedText(s))
		lines := []string{heatMark(s) + " " + name}
		if e.Scope != "" {
			lines = append(lines, dim.Render(clip(e.Scope, width)))
		}
		lines = append(lines, dim.Render(strings.Join(facts, " · ")), projectsText(e))
		return lines
	}
	lines := []string{heatMark(s) + " " + name + "  " + dim.Render(clip(home.Display(e.Path), max(8, width-lipgloss.Width(entryName(e))-4)))}
	if e.Description != "" {
		lines = append(lines, dim.Render(clip(e.Description, width)))
	}
	facts := []string{touchedText(s)}
	if t := taskCountText(s); t != "" {
		facts = append(facts, t)
	}
	if p := currentPhase(s); p != "" {
		facts = append(facts, "phase: "+p)
	}
	return append(lines, dim.Render(strings.Join(facts, " · ")))
}

// projectsText names the projects that use a knowledge base.
func projectsText(e registry.Entry) string {
	if len(e.Projects) == 0 {
		return dim.Render("used by no project yet")
	}
	names := make([]string, len(e.Projects))
	for i, r := range e.Projects {
		names[i] = r.Name
	}
	return projectSt.Render(fmt.Sprintf("%d project%s", len(names), plural(len(names)))) + dim.Render(": "+strings.Join(names, ", "))
}

// problemFix says what puts an entry the scan could not read right.
func problemFix(e registry.Entry) string {
	path := home.Display(e.Path)
	switch e.Reason {
	case registry.ReasonV1:
		return "run claude-atlas adopt " + path
	case registry.ReasonV2Project:
		return "run claude-atlas init in the work, then delete this folder"
	case registry.ReasonMissing:
		return "work in it again to heal the path, or run claude-atlas forget " + path
	case registry.ReasonNotProject:
		return "run claude-atlas init " + path + ", or claude-atlas forget " + path
	case registry.ReasonUnreadable:
		return "repair the identity file and press R"
	case registry.ReasonSchema:
		return "written by a newer claude-atlas; update the binary"
	}
	return e.Error
}

// detailLines is the block under an expanded box: what `show NAME` prints.
func detailLines(e registry.Entry) []string {
	var out []string
	row := func(k, val string) { out = append(out, label.Width(detailWidth).Render(k)+dash(val)) }
	if e.Error != "" {
		row("Path", home.Display(e.Path))
		row("Reason", e.Reason)
		row("Fix", problemFix(e))
		return out
	}
	row("Path", home.Display(e.Path))
	row("Created", e.Created)
	s := e.State
	if e.Kind == registry.Knowledge {
		row("Mode", string(e.Mode))
		row("Scope", e.Scope)
		if s == nil {
			out = append(out, dim.Render("never refreshed; press R"))
			return out
		}
		row("Last operation", s.LastOperation)
		row("Last touched", s.LastTouched)
		row("Unfinished", s.Unfinished.Text())
		for i, t := range s.OpenThreads {
			k := "Open threads"
			if i > 0 {
				k = ""
			}
			out = append(out, label.Width(detailWidth).Render(k)+"- "+refresh.PlainText(t))
		}
	} else {
		switch {
		case e.Knowledge == nil:
			row("Knowledge", "none")
		case e.Knowledge.Error != "":
			row("Knowledge", errSt.Render(e.Knowledge.Name+"  "+e.Knowledge.Error))
		default:
			row("Knowledge", knowledgeSt.Render(e.Knowledge.Name))
		}
		if s == nil {
			out = append(out, dim.Render("never refreshed; press R"))
			return out
		}
		if g := s.Git; g != nil {
			row("Git", refresh.LinkSummary(*g))
		}
		if d := s.Described; d != nil {
			row("Described", d.Summary())
		} else if e.Knowledge != nil && e.Knowledge.Error == "" {
			row("Described", dim.Render(registry.NotDescribed))
		}
		if s.Tasks != nil {
			row("Tasks", taskSummaryText(s.Tasks))
			if len(s.Tasks.Phases) > 0 {
				row("Phases", strings.Join(s.Tasks.Phases, " → "))
			}
			for i, t := range s.Tasks.Open {
				if i == maxDetailTasks {
					out = append(out, label.Width(detailWidth).Render("")+dim.Render(fmt.Sprintf("… and %d more", len(s.Tasks.Open)-i)))
					break
				}
				k := ""
				if i == 0 {
					k = "Open"
				}
				line := fmt.Sprintf("[%s] %s", t.Status, t.Title)
				if t.Phase != "" {
					line += dim.Render(" · " + t.Phase)
				}
				if t.Stale {
					line += errSt.Render(" · stale")
				}
				out = append(out, label.Width(detailWidth).Render(k)+line)
			}
		}
	}
	for _, note := range refresh.Signals(e, now()) {
		out = append(out, label.Width(detailWidth).Render("Signal")+errSt.Render(note))
	}
	return out
}
