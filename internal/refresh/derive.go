package refresh

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/describe"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// Derive observes one entry and returns what refresh records for it.
func Derive(e registry.Entry, today time.Time, generatedAt string, newDays int) *registry.State {
	if e.Error != "" {
		return &registry.State{GeneratedAt: generatedAt, Error: e.Error}
	}
	if e.Kind == registry.Project {
		return deriveProject(e, today, generatedAt, newDays)
	}
	return deriveKnowledge(e, today, generatedAt, newDays)
}

func deriveKnowledge(e registry.Entry, today time.Time, generatedAt string, newDays int) *registry.State {
	state := &registry.State{GeneratedAt: generatedAt, OpenThreads: []string{}}
	root := e.Path

	var touched time.Time
	touchedFound := false
	if op, ok := NewestLogDate(root); ok {
		state.LastOperation = op.Format("2006-01-02")
		touched, touchedFound = op, true
	}
	if mt, ok := NewestWikiMtime(root); ok && (!touchedFound || mt.After(touched)) {
		touched, touchedFound = mt, true
	}
	setHeat(state, e.Created, touched, touchedFound, today, newDays)
	state.OpenThreads = ActiveThreads(root)
	if state.OpenThreads == nil {
		state.OpenThreads = []string{}
	}

	report, err := lint.Run(root, lint.Options{AsOf: today})
	if err != nil {
		state.Error = err.Error()
		return state
	}
	state.Pages = ptr(report.Summary.PagesScanned)
	state.Unfinished.EmptySections = ptr(report.Summary.CategoryCounts["empty_sections"])
	state.Unfinished.Stubs = ptr(report.Summary.Stubs)
	state.Unfinished.WantedPages = ptr(report.Summary.WantedPages)
	state.Unfinished.DeadLinks = ptr(report.Summary.CategoryCounts["dead_links"])

	v, err := vault.Open(e.Path)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	if files, err := capture.ListInbox(v, today); err == nil {
		waiting := 0
		for _, f := range files {
			if !f.Captured {
				waiting++
			}
		}
		state.Inbox = ptr(waiting)
	}
	if pending, _ := txn.Pending(v); pending != nil {
		state.PendingRecovery = true
	}
	state.OK = true
	return state
}

func deriveProject(e registry.Entry, today time.Time, generatedAt string, newDays int) *registry.State {
	state := &registry.State{GeneratedAt: generatedAt}
	p, err := project.Open(e.Path)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	var touched time.Time
	touchedFound := false
	if fact := links.Inspect(links.Repo, e.Path); fact.OK {
		state.Git = &fact
		if t, ok := fact.Touched(); ok {
			touched, touchedFound = t, true
		}
	}
	state.Tasks = taskSummaryFor(p, today)
	if state.Tasks != nil {
		for _, t := range state.Tasks.Open {
			if u, ok := parseDate(t.Updated); ok && (!touchedFound || u.After(touched)) {
				touched, touchedFound = u, true
			}
		}
	}
	setHeat(state, e.Created, touched, touchedFound, today, newDays)
	state.Described = describe.Page(e)
	state.OK = true
	return state
}

// setHeat records when the entry was last touched and how warm that makes it. Creation
// counts as a touch, so an entry with no other sign of work still has a heat.
func setHeat(state *registry.State, created string, touched time.Time, touchedFound bool, today time.Time, newDays int) {
	var daysOld *int
	if c, ok := parseDate(created); ok {
		days := int(dateOf(today).Sub(dateOf(c)).Hours() / 24)
		daysOld = &days
	}
	if !touchedFound && daysOld != nil {
		touched, touchedFound = dateOf(today).AddDate(0, 0, -*daysOld), true
	}
	if touchedFound {
		state.LastTouched = touched.Format("2006-01-02")
		days := int(dateOf(today).Sub(dateOf(touched)).Hours() / 24)
		state.DaysIdle = &days
		state.Heat = Heat(state.DaysIdle, daysOld, newDays)
	}
}

// taskSummaryFor reads the project's task pages; nil only when the folder cannot be
// read.
func taskSummaryFor(p *project.Project, today time.Time) *registry.TaskSummary {
	board, err := tasks.Load(p)
	if err != nil {
		return nil
	}
	sum := &registry.TaskSummary{Counts: board.Counts(today), Open: []registry.TaskLine{}}
	sum.Counts.Notes = len(tasks.Notes(p))
	for _, t := range board.Open() {
		sum.Open = append(sum.Open, registry.TaskLine{
			ID: t.ID, Title: t.Title, Status: t.Status, Priority: t.Priority, Phase: t.Phase, Due: t.Due,
			Updated: t.Updated, Path: p.Path(t.Path), Stale: tasks.Stale(t, today),
		})
	}
	var open, finished []string
	for _, ph := range board.Phases {
		if board.Finished(ph.Title) {
			finished = append(finished, ph.Title)
		} else {
			open = append(open, ph.Title)
		}
	}
	sum.Phases = append(open, finished...)
	return sum
}

// deriveStates fills in every entry's derived state, as one pass over the scan.
func deriveStates(ix *registry.Index, cfg *home.Config, today time.Time) string {
	generatedAt := NowUTC()
	newDays := cfg.NewDays()
	for i := range ix.Entries {
		ix.Entries[i].State = Derive(ix.Entries[i], today, generatedAt, newDays)
	}
	return generatedAt
}

// Registry scans, derives every readable entry, writes the registry file, and returns the
// entries.
func Registry(h home.Home, cfg *home.Config, stateDir string, today time.Time) ([]registry.Entry, *registry.Index, error) {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return nil, nil, err
	}
	generatedAt := deriveStates(ix, cfg, today)
	if err := os.RemoveAll(stateDir); err != nil {
		return nil, nil, err
	}
	if err := registry.Write(stateDir, ix.Entries, generatedAt); err != nil {
		return nil, nil, err
	}
	return ix.Entries, ix, nil
}

// Signals lists what needs attention on one entry.
func Signals(e registry.Entry, today time.Time) []string {
	if e.Error != "" {
		return []string{e.Error}
	}
	state := e.State
	if state == nil {
		return []string{"not refreshed"}
	}
	var notes []string
	if !state.OK {
		notes = append(notes, "unreachable: "+state.Error)
	}
	if state.PendingRecovery {
		notes = append(notes, "an operation was interrupted; run `claude-atlas recover "+e.Path+"`")
	}
	if e.Kind == registry.Project {
		if e.Knowledge != nil && e.Knowledge.Error != "" {
			notes = append(notes, fmt.Sprintf("knowledge base %s: %s", e.Knowledge.Name, e.Knowledge.Error))
		}
		if e.Knowledge != nil && e.Knowledge.Error == "" && state.Described == nil {
			notes = append(notes, registry.NotDescribed+"; the describe skill writes the page")
		}
		if d := state.Described; d != nil && d.Behind > describe.BehindThreshold {
			notes = append(notes, fmt.Sprintf("its page in the knowledge base is %d commits behind; the describe skill brings it up to date", d.Behind))
		}
	}
	if state.Tasks != nil {
		var blocked, stale []string
		for _, t := range state.Tasks.Open {
			if t.Status == "blocked" {
				blocked = append(blocked, t.Title)
			}
			if t.Stale {
				stale = append(stale, t.Title)
			}
		}
		if len(blocked) > 0 {
			notes = append(notes, fmt.Sprintf("%d blocked task%s: %s", len(blocked), plural(len(blocked)), strings.Join(blocked, "; ")))
		}
		if len(stale) > 0 {
			notes = append(notes, fmt.Sprintf("%d stale task%s, active but untouched for %d days: %s", len(stale), plural(len(stale)), tasks.StaleDays, strings.Join(stale, "; ")))
		}
	}
	return notes
}

// All rebuilds the registry from a scan: the CLI runs it after every change.
func All(h home.Home, cfg *home.Config, today time.Time) ([]registry.Entry, *registry.Index, error) {
	return Registry(h, cfg, h.StateDir(), today)
}

// Entries reads the registry the last refresh wrote, writing one first when none exists
// or the one there is stale.
func Entries(h home.Home, cfg *home.Config, today time.Time) ([]registry.Entry, error) {
	entries, _, err := registry.Read(h.StateDir())
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, registry.ErrStale) {
		entries, _, err = All(h, cfg, today)
	}
	return entries, err
}

// Derived scans and derives every entry's state, and writes nothing: what a tool reads
// when it wants the atlas as it is now.
func Derived(cfg *home.Config, today time.Time) (*registry.Index, error) {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return nil, err
	}
	deriveStates(ix, cfg, today)
	return ix, nil
}
