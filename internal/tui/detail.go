package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// The connectors. The arrow always points at the knowledge base.
const (
	arrowOut = "╌╌╌╌▶ " // beside a project: the knowledge bases it mounts
	arrowIn  = "◀╌╌╌╌ " // beside a knowledge base: the projects that mount it
	noArrow  = "      "
)

// detailWidth is the label column of the expanded block.
const detailWidth = 15

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

// touchedText says when a vault was last touched.
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

// clip shortens text to width runes, the last one an ellipsis.
func clip(s string, width int) string {
	r := []rune(s)
	if len(r) <= width || width < 2 {
		return s
	}
	return string(r[:width-1]) + "…"
}

// boxLines is the two lines of a vault's box: the heat mark and the name, then one
// activity fact and one count. A problem shows its folder and its error.
func boxLines(e registry.Entry) []string {
	if e.Error != "" {
		return []string{filepath.Base(e.Path), errSt.Render(e.Error)}
	}
	name := kindStyle(e.Kind).Render(entryName(e))
	s := e.State
	if e.Kind == vault.Knowledge {
		return []string{
			heatMark(s) + " " + name + "   " + dim.Render(accessOf(e)),
			dim.Render(pagesText(s) + " pages · " + touchedText(s)),
		}
	}
	facts := touchedText(s)
	if t := taskCountText(s); t != "" {
		facts += " · " + t
	}
	return []string{heatMark(s) + " " + name, dim.Render(facts)}
}

// accessOf is a knowledge base's access; an identity file that names none is open.
func accessOf(e registry.Entry) string {
	if e.Access == "" {
		return vault.AccessOpen
	}
	return e.Access
}

// mountLine is one connector beside a project: the knowledge base in its color, the
// effective access, and what is wrong with the link, if anything. kbName is the
// knowledge base's name as the scan knows it; a mount under another name says so.
func mountLine(project registry.Entry, m registry.Mount, kbName string, width int) string {
	if m.Error != "" {
		return arrowOut + errSt.Render(clip(m.Error, width))
	}
	access := m.Effective
	if m.Effective != m.Access {
		access += fmt.Sprintf(" (%s not granted)", m.Access)
	}
	as := ""
	if m.Name != kbName {
		as = " as kb/" + m.Name
	}
	link := ""
	switch vaults.MountState(project, m) {
	case vaults.MountMissing:
		link = " · link missing"
	case vaults.MountWrong:
		link = " · link wrong"
	}
	// The name gives way to what follows it: the mount name, the three spaces, the access,
	// and the link text.
	budget := max(8, width-len([]rune(as))-3-len([]rune(access))-len([]rune(link)))
	text := knowledgeSt.Render(clip(kbName, budget))
	if as != "" {
		text += dim.Render(as)
	}
	text += "   " + dim.Render(access)
	if link != "" {
		text += errSt.Render(link)
	}
	return arrowOut + text
}

// mountedByLines is the connector column beside a knowledge base: how many projects
// mount it, or one line per project when expanded, then the grants whose project the
// scan did not find.
func mountedByLines(e registry.Entry, expanded bool) []string {
	var stale []registry.Grant
	for _, g := range e.Grants {
		if g.Error != "" {
			stale = append(stale, g)
		}
	}
	none := dim.Render("not mounted by any project")
	if !expanded {
		text := none
		if n := len(e.MountedBy); n > 0 {
			text = projectSt.Render(fmt.Sprintf("%d project%s", n, plural(n)))
		}
		if len(stale) > 0 {
			text += errSt.Render(fmt.Sprintf(" · %d grant%s stale", len(stale), plural(len(stale))))
		}
		return []string{arrowIn + text}
	}
	var out []string
	for _, r := range e.MountedBy {
		out = append(out, arrowIn+projectSt.Render(r.Name)+"   "+dim.Render(r.Access))
	}
	if len(out) == 0 {
		out = append(out, arrowIn+none)
	}
	for _, g := range stale {
		name := g.Name
		if name == "" {
			name = g.ID
		}
		out = append(out, noArrow+errSt.Render("grant  "+name+"   "+g.Access+" · "+g.Error))
	}
	return out
}

// problemFix says what puts a vault the scan could not read right.
func problemFix(e registry.Entry) string {
	path := home.Display(e.Path)
	switch e.Reason {
	case registry.ReasonV1:
		return "press a to adopt it, or run claude-atlas adopt " + path + " --as project|knowledge"
	case registry.ReasonMissing:
		return "press e then r to forget it, or run claude-atlas remove " + path
	case registry.ReasonUnreadable:
		return "the identity file is not JSON; press a to adopt it again, or repair the file and press R"
	case registry.ReasonSchema:
		return "written by a newer claude-atlas; update the binary"
	}
	return e.Error
}

// detailLines is everything the atlas knows about one vault, for the block under its
// box: the identity file, the repositories, the state the last refresh derived, and the
// signals. The box carries the name and the connectors carry the mounts, so neither
// repeats here.
func detailLines(e registry.Entry, today time.Time) []string {
	var out []string
	row := func(k, val string) { out = append(out, label.Width(detailWidth).Render(k)+dash(val)) }
	if e.Error != "" {
		row("Path", home.Display(e.Path))
		row("Reason", e.Reason)
		row("Fix", problemFix(e))
		return out
	}
	s := e.State
	row("Path", home.Display(e.Path))
	row("Id", e.ID)
	row("Mode", string(e.Mode))
	row("Created", e.Created)
	if e.Kind == vault.Knowledge {
		row("Scope", e.Scope)
		row("Access", accessOf(e))
		for _, g := range e.Grants {
			text := g.Name + "  " + g.Access
			if g.Error != "" {
				text = errSt.Render(text + "  " + g.Error)
			}
			row("Grant", text)
		}
	} else {
		row("Tags", strings.Join(e.Tags, ", "))
	}
	if len(e.Repos) > 0 {
		out = append(out, catSt.Render("Repositories"))
		for _, r := range e.Repos {
			where := home.Display(r.Path) + " · changes: " + links.Policy(r.Changes, r.Remote)
			if r.Remote != "" {
				where += " · " + r.Remote
			}
			if r.Path == "" {
				where = r.Error
			}
			out = append(out, fmt.Sprintf("  %-24s %s", r.Name, dim.Render(where)))
			if s != nil {
				if fact, ok := s.RepoFacts[r.Name]; ok {
					facts := refresh.LinkSummary(fact)
					if !fact.OK {
						facts = errSt.Render(facts)
					}
					out = append(out, strings.Repeat(" ", 27)+dim.Render(facts))
				}
			}
		}
	}
	if s == nil {
		out = append(out, dim.Render("never refreshed; press R"))
		return out
	}
	out = append(out, "")
	check := okSt.Render("ok")
	if !s.VaultOK {
		check = errSt.Render(dash(s.VaultError))
	}
	row("Vault check", check)
	row("Heat", heatMark(s)+" "+dash(s.Heat))
	row("Last touched", s.LastTouched)
	row("Last operation", s.LastOperation)
	row("Pages", pagesText(s))
	row("Unfinished", s.Unfinished.Text())
	for i, t := range s.OpenThreads {
		k := "Open threads"
		if i > 0 {
			k = ""
		}
		out = append(out, label.Width(detailWidth).Render(k)+"- "+refresh.PlainText(t))
	}
	if s.Tasks != nil {
		row("Tasks", taskSummaryText(s.Tasks))
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", s.GeneratedAt); err == nil {
		row("Refreshed", t.Local().Format("2006-01-02 15:04"))
	}
	if signals := refresh.Signals(e, today); len(signals) > 0 {
		out = append(out, catSt.Render("Signals"))
		for _, signal := range signals {
			out = append(out, "  - "+signal)
		}
	}
	return out
}
