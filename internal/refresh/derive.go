package refresh

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// Derive observes one vault and returns what refresh records for it.
func Derive(e registry.Entry, today time.Time, generatedAt string, newDays int) *registry.State {
	if e.Error != "" {
		return &registry.State{GeneratedAt: generatedAt, VaultError: e.Error, OpenThreads: []string{}}
	}
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
	var repoFacts map[string]links.Link
	for _, r := range e.Repos {
		if r.Path == "" {
			continue
		}
		fact := links.Inspect(links.Repo, r.Path)
		if repoFacts == nil {
			repoFacts = map[string]links.Link{}
		}
		repoFacts[r.Name] = fact
		if t, ok := fact.Touched(); ok && (!touchedFound || t.After(touched)) {
			touched, touchedFound = t, true
		}
	}
	state.RepoFacts = repoFacts

	var daysOld *int
	if created, ok := parseDate(e.Created); ok {
		days := int(dateOf(today).Sub(dateOf(created)).Hours() / 24)
		daysOld = &days
	}
	if touchedFound {
		state.LastTouched = touched.Format("2006-01-02")
		days := int(dateOf(today).Sub(dateOf(touched)).Hours() / 24)
		state.DaysIdle = &days
		state.Heat = Heat(state.DaysIdle, daysOld, newDays)
	}
	state.OpenThreads = ActiveThreads(root)
	if state.OpenThreads == nil {
		state.OpenThreads = []string{}
	}

	report, err := lint.Run(root, lint.Options{AsOf: today})
	if err != nil {
		state.VaultError = err.Error()
		return state
	}
	state.Pages = ptr(report.Summary.PagesScanned)
	state.Unfinished.EmptySections = ptr(report.Summary.CategoryCounts["empty_sections"])
	state.Unfinished.Stubs = ptr(report.Summary.Stubs)
	state.Unfinished.WantedPages = ptr(report.Summary.WantedPages)
	state.Unfinished.DeadLinks = ptr(report.Summary.CategoryCounts["dead_links"])

	v, err := vault.Open(e.Path)
	if err != nil {
		state.VaultError = err.Error()
		return state
	}
	if e.Kind == vault.Project {
		state.Tasks = taskSummaryFor(v, today)
	}
	if pending, _ := txn.Pending(v); pending != nil {
		state.PendingRecovery = true
	}
	state.VaultOK = true
	return state
}

// taskSummaryFor reads the vault's task ledger; nil only when the ledger cannot be read.
func taskSummaryFor(v *vault.Vault, today time.Time) *registry.TaskSummary {
	led, err := tasks.Current(v, today)
	if err != nil {
		return nil
	}
	sum := &registry.TaskSummary{Counts: led.Counts(today), Open: []registry.TaskLine{}}
	sum.Counts.Notes = len(tasks.Notes(v))
	for _, r := range led.Open() {
		sum.Open = append(sum.Open, registry.TaskLine{
			ID: r.ID, Title: r.Title, Status: r.Status, Priority: r.Priority, Due: r.Due, Workdir: r.Workdir,
			LastTouched: r.LastTouched, Path: v.Path(r.Path), Stale: tasks.Stale(r, today),
		})
	}
	return sum
}

// ProjectChange is what one project needed to bring its local state up to date: the
// mount symlinks the repair touched, the repositories adoption linked, and Error for
// what stopped one of them.
type ProjectChange struct {
	Project string
	vaults.MountRepair
	// Adopted names the repositories found under repos/ and written to the identity file.
	Adopted []string
	Error   string
}

// any reports whether the change is worth a line.
func (c ProjectChange) any() bool {
	return len(c.Created)+len(c.Repaired)+len(c.Removed)+len(c.Missing)+len(c.Adopted) > 0 || c.Error != ""
}

// Registry scans, derives every readable entry, writes the registry file, and returns the
// entries. With ensure, it first links every git repository waiting under a project's
// repos/ and recreates its mount symlinks, then reports what each project needed; one
// project's failure does not stop the others. Adoption changes identity files, so the
// scan runs again afterwards and the caller sees the repositories it just linked.
func Registry(h home.Home, cfg *home.Config, stateDir string, today time.Time, ensure bool) ([]registry.Entry, *registry.Index, []ProjectChange, error) {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	var changes []ProjectChange
	if ensure {
		adopted := false
		for _, e := range ix.Entries {
			if e.Error != "" || e.Kind != vault.Project {
				continue
			}
			change := ProjectChange{Project: e.Name}
			repos, adoptErr := vaults.AdoptRepos(h, cfg, e, today)
			for _, r := range repos {
				change.Adopted = append(change.Adopted, r.Name)
				adopted = true
			}
			if adoptErr != nil {
				change.Error = adoptErr.Error()
			}
			rep, err := vaults.EnsureMounts(e, ix)
			change.MountRepair = rep
			if err != nil && change.Error == "" {
				change.Error = err.Error()
			}
			if change.any() {
				changes = append(changes, change)
			}
		}
		if adopted {
			if ix, err = registry.Scan(cfg); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	generatedAt := NowUTC()
	newDays := cfg.NewDays()
	for i := range ix.Entries {
		ix.Entries[i].State = Derive(ix.Entries[i], today, generatedAt, newDays)
	}
	if err := os.RemoveAll(stateDir); err != nil {
		return nil, nil, nil, err
	}
	if err := registry.Write(stateDir, ix.Entries, generatedAt); err != nil {
		return nil, nil, nil, err
	}
	return ix.Entries, ix, changes, nil
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
	if !state.VaultOK {
		notes = append(notes, "vault unreachable: "+state.VaultError)
	}
	if state.PendingRecovery {
		notes = append(notes, "an operation was interrupted; run `claude-atlas recover "+e.Path+"`")
	}
	for _, m := range e.Mounts {
		if m.Error != "" {
			notes = append(notes, fmt.Sprintf("mount %s: %s", m.Name, m.Error))
			continue
		}
		switch vaults.MountState(e, m) {
		case vaults.MountMissing:
			notes = append(notes, fmt.Sprintf("mount %s: symlink missing; run `claude-atlas refresh`", m.Name))
		case vaults.MountWrong:
			notes = append(notes, fmt.Sprintf("mount %s: symlink points elsewhere; run `claude-atlas refresh`", m.Name))
		}
	}
	for _, r := range e.Repos {
		if r.Error != "" {
			notes = append(notes, fmt.Sprintf("repo %s: %s", r.Name, r.Error))
			continue
		}
		if fact, ok := state.RepoFacts[r.Name]; ok && !fact.OK {
			notes = append(notes, fmt.Sprintf("repo %s: %s", r.Name, fact.Error))
		}
	}
	if e.Kind == vault.Knowledge {
		for _, g := range e.Grants {
			if g.Error == "" {
				continue
			}
			notes = append(notes, fmt.Sprintf("grant %s (%s): %s; run `claude-atlas revoke %s %s`", g.Name, g.ID, g.Error, e.Name, g.ID))
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

// All rebuilds the registry from a scan and brings every project's local state up to
// date: the CLI runs it after every change.
func All(h home.Home, cfg *home.Config, today time.Time) ([]registry.Entry, *registry.Index, []ProjectChange, error) {
	return Registry(h, cfg, h.StateDir(), today, true)
}

// Entries reads the registry the last refresh wrote, writing one first when none exists.
func Entries(h home.Home, cfg *home.Config, today time.Time) ([]registry.Entry, error) {
	entries, _, err := registry.Read(h.StateDir())
	if errors.Is(err, os.ErrNotExist) {
		entries, _, _, err = All(h, cfg, today)
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
	generatedAt := NowUTC()
	newDays := cfg.NewDays()
	for i := range ix.Entries {
		ix.Entries[i].State = Derive(ix.Entries[i], today, generatedAt, newDays)
	}
	return ix, nil
}
