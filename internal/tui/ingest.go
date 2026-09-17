package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

type ingestStep int

const (
	ingestPath ingestStep = iota
	ingestConfirm
	ingestLaunch
)

type ingestOutcome int

const (
	ingestOpen ingestOutcome = iota
	ingestCancelled
	ingestNothing  // nothing new and nothing waiting; the screen closes with a note
	ingestDone     // staged; the user will start Claude Code later
	ingestStartNow // staged; start Claude Code with the ingest skill
)

// trustNote explains Claude Code's own first-run dialog, whose default answer quits.
const trustNote = "The first time in a vault, Claude Code asks whether you trust the folder; choose Yes."

// ingestScreen stages sources from outside the vault into its inbox, then offers to start
// Claude Code on them. It is embedded in the view.
type ingestScreen struct {
	hooks   actions.Atlas
	entry   registry.Entry
	step    ingestStep
	source  pathField
	plan    *capture.StagePlan
	result  *capture.StageResult
	linked  []string
	err     string
	outcome ingestOutcome
}

func newIngest(hooks actions.Atlas, e registry.Entry) ingestScreen {
	source := newPathField("~/Papers or ~/Papers/paper.pdf; blank: the folders ingested before", 64)
	if hooks.Sources != nil {
		for _, m := range hooks.Sources(e) {
			source.names = append(source.names, home.Display(m))
		}
	}
	return ingestScreen{hooks: hooks, entry: e, source: source}
}

func (s ingestScreen) update(msg tea.Msg) (ingestScreen, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	switch s.step {
	case ingestPath:
		if isKey {
			switch key.Type {
			case tea.KeyEsc:
				s.outcome = ingestCancelled
				return s, nil
			case tea.KeyEnter:
				var given []string
				if src := strings.TrimSpace(s.source.value()); src != "" {
					given = []string{src}
				}
				plan, err := s.hooks.StagePlan(s.entry, given)
				if err != nil {
					s.err = err.Error()
					return s, nil
				}
				s.err = ""
				s.plan = plan
				s.step = ingestConfirm
				return s, nil
			}
		}
		var cmd tea.Cmd
		s.source, cmd = s.source.update(msg)
		s.err = ""
		return s, cmd
	case ingestConfirm:
		if !isKey {
			return s, nil
		}
		switch key.Type {
		case tea.KeyEsc:
			s.step = ingestPath
			s.plan = nil
			return s, s.source.focus()
		case tea.KeyEnter:
			if len(s.plan.New) == 0 {
				if s.plan.Waiting == 0 {
					s.outcome = ingestNothing
					return s, nil
				}
				// Nothing to copy, but files staged earlier still wait: go straight to the launch.
				s.result = &capture.StageResult{Staged: []capture.Staged{}}
				s.step = ingestLaunch
				return s, nil
			}
			res, linked, err := s.hooks.Stage(s.entry, s.plan)
			if err != nil {
				s.err = err.Error()
				return s, nil
			}
			s.result, s.linked = res, linked
			s.step = ingestLaunch
		}
		return s, nil
	case ingestLaunch:
		if !isKey {
			return s, nil
		}
		switch key.Type {
		case tea.KeyEnter:
			s.outcome = ingestStartNow
		case tea.KeyEsc:
			s.outcome = ingestDone
		}
		return s, nil
	}
	return s, nil
}

func (s ingestScreen) view() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s   %s\n\n", title.Render("Ingest into "+s.entry.Name), dim.Render(home.Display(s.entry.Path)))
	row := func(k, v string) { fmt.Fprintf(&b, "  %s%s\n", label.Width(12).Render(k), v) }
	switch s.step {
	case ingestPath:
		fmt.Fprintf(&b, "  %s%s\n", activeL.Width(12).Render("Source"), s.source.view("              "))
		hint := "a file or folder outside the vault; the originals stay where they are"
		if n := len(s.source.names); n > 0 {
			hint = fmt.Sprintf("a file or folder; blank stages what is new in the %d folder%s ingested before", n, plural(n))
		}
		b.WriteString("              " + dim.Render(hint) + "\n")
		if s.err != "" {
			b.WriteString("              " + errSt.Render(s.err) + "\n")
		}
		b.WriteString("\n  " + dim.Render(pathHint()+" · Enter next · Esc cancel") + "\n")
	case ingestConfirm:
		for i, src := range s.plan.Sources {
			k := "Source"
			if i > 0 {
				k = ""
			}
			row(k, home.Display(src))
		}
		if len(s.plan.New) == 0 {
			row("New", dim.Render("nothing; every file is already ingested or already waiting"))
		} else {
			row("New", fmt.Sprintf("%d file%s → inbox/", len(s.plan.New), plural(len(s.plan.New))))
			for i, f := range s.plan.New {
				if i == 8 {
					row("", dim.Render(fmt.Sprintf("… and %d more", len(s.plan.New)-i)))
					break
				}
				row("", dim.Render(strings.TrimPrefix(f.To, "inbox/")))
			}
		}
		if n := len(s.plan.Unchanged); n > 0 {
			row("Unchanged", fmt.Sprintf("%d already ingested or waiting", n))
		}
		if s.plan.Waiting > 0 {
			row("Inbox", fmt.Sprintf("%d file%s waiting to be ingested", s.plan.Waiting, plural(s.plan.Waiting)))
		}
		for _, sk := range s.plan.Skipped {
			row("Skipped", home.Display(sk.From)+dim.Render("  "+sk.Reason))
		}
		for _, dir := range s.plan.Dirs {
			known := false
			for _, name := range s.source.names {
				if name == home.Display(dir) {
					known = true
				}
			}
			if !known {
				row("Remember", home.Display(dir)+dim.Render("  so a later ingest with no path stages what is new there"))
			}
		}
		if s.err != "" {
			b.WriteString("  " + errSt.Render(s.err) + "\n")
		}
		switch {
		case len(s.plan.New) == 0 && s.plan.Waiting == 0:
			b.WriteString("\n  " + dim.Render("Nothing to ingest. Enter close · Esc back") + "\n")
		case len(s.plan.New) == 0:
			b.WriteString("\n  " + title.Render("Enter") + " continue with what is waiting   " + dim.Render("Esc back") + "\n")
		default:
			b.WriteString("\n  " + title.Render("Enter") + " stage these files   " + dim.Render("Esc back") + "\n")
		}
	case ingestLaunch:
		if len(s.result.Staged) > 0 {
			row("Staged", fmt.Sprintf("%d file%s in inbox/", len(s.result.Staged), plural(len(s.result.Staged))))
		}
		waiting := s.plan.Waiting + len(s.result.Staged)
		row("Inbox", fmt.Sprintf("%d file%s waiting to be ingested", waiting, plural(waiting)))
		for _, dir := range s.linked {
			row("Remembered", home.Display(dir))
		}
		b.WriteString("\n  " + title.Render("Enter") + " start Claude Code with /claude-atlas:wiki-ingest   " + dim.Render("Esc later") + "\n")
		b.WriteString("  " + dim.Render(trustNote) + "\n")
	}
	return b.String()
}
