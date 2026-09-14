// Package refresh derives state for every project and link page, rewrites the generated
// pages of the atlas vault, and renders Overview.md.
package refresh

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/pages"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/tree"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

const (
	HotDays  = 7
	WarmDays = 30
)

var (
	logHeading  = regexp.MustCompile(`(?m)^##\s+(\d{4}-\d{2}-\d{2})\b`)
	statusLine  = regexp.MustCompile(`(?m)^status:\s*(.+?)\s*$`)
	createdLine = regexp.MustCompile(`(?m)^created:\s*(\d{4}-\d{2}-\d{2})`)
	wikiLink    = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
)

func ptr[T any](v T) *T { return &v }

// Heat maps a vault's age and idleness to new, hot, warm, or cold; unknown idleness gives "".
// A vault created within newDays is "new" whatever its activity, so a fresh, possibly
// empty vault is not mistaken for one with a long active history.
func Heat(daysIdle, daysOld *int, newDays int) string {
	switch {
	case daysIdle == nil:
		return ""
	case daysOld != nil && *daysOld < newDays:
		return "new"
	case *daysIdle < HotDays:
		return "hot"
	case *daysIdle < WarmDays:
		return "warm"
	default:
		return "cold"
	}
}

func parseDate(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	return t, err == nil
}

func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// NewestLogDate is the latest `## YYYY-MM-DD` heading in wiki/log.md.
func NewestLogDate(vault string) (time.Time, bool) {
	data, err := os.ReadFile(filepath.Join(vault, "wiki", "log.md"))
	if err != nil {
		return time.Time{}, false
	}
	var newest time.Time
	found := false
	for _, m := range logHeading.FindAllStringSubmatch(string(data), -1) {
		if t, ok := parseDate(m[1]); ok && (!found || t.After(newest)) {
			newest, found = t, true
		}
	}
	return newest, found
}

// CreatedDate is the day the vault was created: the identity file's date, or for a
// claude-obsidian vault the `created:` field its template wrote into wiki/index.md.
func CreatedDate(root string) (time.Time, bool) {
	if v, err := vault.Open(root); err == nil && v.Config.Created != "" {
		if t, ok := parseDate(v.Config.Created); ok {
			return t, true
		}
	}
	for _, name := range []string{"index.md", "overview.md", "log.md"} {
		data, err := os.ReadFile(filepath.Join(root, "wiki", name))
		if err != nil {
			continue
		}
		front, _, ok := tree.SplitFrontmatter(string(data))
		if !ok {
			continue
		}
		if m := createdLine.FindStringSubmatch(front); m != nil {
			if t, ok := parseDate(m[1]); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// NewestWikiMtime is the latest modification date of any file under wiki/.
func NewestWikiMtime(vault string) (time.Time, bool) {
	var newest time.Time
	found := false
	filepath.WalkDir(filepath.Join(vault, "wiki"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !found || info.ModTime().After(newest) {
			newest, found = info.ModTime(), true
		}
		return nil
	})
	if !found {
		return time.Time{}, false
	}
	return dateOf(newest), true
}

// ActiveThreads lists the bullets under `## Active Threads` in wiki/hot.md. Prose, so best effort.
func ActiveThreads(vault string) []string {
	data, err := os.ReadFile(filepath.Join(vault, "wiki", "hot.md"))
	if err != nil {
		return nil
	}
	var threads []string
	inside := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			inside = strings.EqualFold(strings.TrimSpace(line[3:]), "active threads")
			continue
		}
		if !inside {
			continue
		}
		s := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* "):
			threads = append(threads, strings.TrimSpace(s[2:]))
		case s != "" && len(threads) > 0 && !strings.HasPrefix(s, "#"):
			threads[len(threads)-1] += " " + s
		}
	}
	return threads
}

// SeedPages counts wiki pages whose frontmatter says `status: seed`.
func SeedPages(vault string) int {
	count := 0
	filepath.WalkDir(filepath.Join(vault, "wiki"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		front, _, ok := tree.SplitFrontmatter(string(data))
		if !ok {
			return nil
		}
		if m := statusLine.FindStringSubmatch(front); m != nil && strings.Trim(m[1], `"'`) == "seed" {
			count++
		}
		return nil
	})
	return count
}

// PlainText strips wikilinks; a link copied from another vault resolves to nothing in the atlas.
func PlainText(s string) string {
	return wikiLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := wikiLink.FindStringSubmatch(m)
		if sub[2] != "" {
			return sub[2]
		}
		return sub[1]
	})
}

func baseState(generatedAt string) *tree.State {
	return &tree.State{Schema: tree.StateSchema, GeneratedAt: generatedAt, OpenThreads: []string{}}
}

// Derive observes one project's vault: a claude-atlas vault, or a claude-obsidian vault
// that has not been adopted yet, which reads the same way but is marked legacy.
func Derive(project *tree.Project, today time.Time, generatedAt string) *tree.State {
	return derive(project, today, generatedAt, nil, home.DefaultNewDays)
}

// derive is Derive with the link facts already known, keyed by folder path, and the
// configured age under which a vault is new.
func derive(project *tree.Project, today time.Time, generatedAt string, facts map[string]links.Link, newDays int) *tree.State {
	state := baseState(generatedAt)
	state.Project = project.Rel
	root := project.VaultPath()
	state.Vault = root
	if !vault.IsVault(root) && !vault.IsLegacy(root) {
		if _, err := os.Stat(root); err != nil {
			state.VaultError = "not found"
		} else {
			state.VaultError = "not a claude-atlas vault"
		}
		return state
	}
	state.Legacy = vault.IsLegacy(root)
	var touched time.Time
	touchedFound := false
	if op, ok := NewestLogDate(root); ok {
		state.LastOperation = op.Format("2006-01-02")
		touched, touchedFound = op, true
	}
	if mt, ok := NewestWikiMtime(root); ok && (!touchedFound || mt.After(touched)) {
		touched, touchedFound = mt, true
	}
	// Work in a linked repo or on linked material counts as work on the project.
	state.Links = inspectLinks(project, facts)
	for _, link := range state.Links {
		if t, ok := link.Touched(); ok && (!touchedFound || t.After(touched)) {
			touched, touchedFound = t, true
		}
	}
	var daysOld *int
	if created, ok := CreatedDate(root); ok {
		state.Created = created.Format("2006-01-02")
		daysOld = ptr(int(dateOf(today).Sub(dateOf(created)).Hours() / 24))
	}
	if touchedFound {
		state.LastTouched = touched.Format("2006-01-02")
		days := int(dateOf(today).Sub(dateOf(touched)).Hours() / 24)
		state.DaysIdle = ptr(days)
		state.Heat = Heat(state.DaysIdle, daysOld, newDays)
	}
	state.OpenThreads = ActiveThreads(root)
	if state.OpenThreads == nil {
		state.OpenThreads = []string{}
	}
	state.Unfinished.SeedPages = ptr(SeedPages(root))
	if info, err := os.Stat(filepath.Join(root, "wiki")); err != nil || !info.IsDir() {
		state.VaultError = "no wiki/ directory"
		return state
	}
	report, err := lint.Run(root, lint.Options{AsOf: today})
	if err != nil {
		state.VaultError = err.Error()
		return state
	}
	if !state.Legacy {
		if v, err := vault.Open(root); err == nil {
			if pending, _ := txn.Pending(v); pending != nil {
				state.PendingRecovery = true
			}
			state.Tasks = taskSummary(v, today)
		}
	}
	state.VaultOK = true
	state.Pages = ptr(report.Summary.PagesScanned)
	state.Unfinished.EmptySections = ptr(report.Summary.CategoryCounts["empty_sections"])
	state.Unfinished.DeadLinks = ptr(report.Summary.CategoryCounts["dead_links"])
	return state
}

// taskSummary reads the vault's task ledger; nil when the vault has no tasks at all.
func taskSummary(v *vault.Vault, today time.Time) *tree.TaskSummary {
	led, err := tasks.Current(v, today)
	if err != nil {
		return nil
	}
	sum := &tree.TaskSummary{Counts: led.Counts(today), Open: []tree.TaskLine{}}
	sum.Counts.Notes = len(tasks.Notes(v))
	for _, r := range led.Open() {
		sum.Open = append(sum.Open, tree.TaskLine{
			ID: r.ID, Title: r.Title, Status: r.Status, Priority: r.Priority, Due: r.Due, Workdir: r.Workdir,
			LastTouched: r.LastTouched, Path: v.Path(r.Path), Stale: tasks.Stale(r, today),
		})
	}
	if len(sum.Open) == 0 && sum.Counts.Done+sum.Counts.Cancelled+sum.Counts.Notes == 0 {
		return nil
	}
	return sum
}

// TaskLink is a markdown link that opens a task page in its own vault.
func TaskLink(t tree.TaskLine) string {
	title := strings.NewReplacer("[", "(", "]", ")").Replace(t.Title)
	return "[" + title + "](obsidian://open?path=" + url.QueryEscape(t.Path) + ")"
}

func inspectLinks(project *tree.Project, facts map[string]links.Link) []links.Link {
	out := []links.Link{}
	for _, l := range project.Linked {
		if l.Path == "" {
			continue
		}
		link, ok := facts[l.Path]
		if !ok || link.Kind != l.Kind {
			link = links.Inspect(l.Kind, l.Path)
		}
		link.Name = l.Name
		out = append(out, link)
	}
	return out
}

// linkLabel names a link by its page, or by its path when it has none.
func linkLabel(l links.Link) string {
	if l.Name != "" {
		return l.Name + " (" + home.Display(home.Expand(l.Path)) + ")"
	}
	return home.Display(home.Expand(l.Path))
}

// linkCell renders a link for a table: the page, or the path when it has none.
func linkCell(l links.Link) string {
	if l.Name != "" {
		return "[[" + links.Dir(l.Kind) + "/" + l.Name + "\\|" + l.Name + "]]"
	}
	return "`" + home.Display(home.Expand(l.Path)) + "`"
}

// LinkSummary renders one link's derived facts on a line.
func LinkSummary(l links.Link) string {
	if !l.OK {
		return l.Error
	}
	var bits []string
	if l.Kind == links.Repo {
		if l.Branch != "" {
			bits = append(bits, l.Branch)
		}
		if l.Dirty != nil {
			if *l.Dirty == 0 {
				bits = append(bits, "clean")
			} else {
				bits = append(bits, fmt.Sprintf("%d uncommitted", *l.Dirty))
			}
		}
		if l.LastCommit != "" {
			bits = append(bits, "last commit "+l.LastCommit)
		}
	} else {
		if l.Files != nil {
			bits = append(bits, fmt.Sprintf("%d file%s", *l.Files, plural(*l.Files)))
		}
		if l.Bytes != nil {
			bits = append(bits, links.HumanBytes(*l.Bytes))
		}
		if l.Newest != "" {
			bits = append(bits, "newest "+l.Newest)
		}
	}
	if len(bits) == 0 {
		return "ok"
	}
	return strings.Join(bits, " · ")
}

func sumPtr(values []*int) *int {
	total, any := 0, false
	for _, v := range values {
		if v != nil {
			total, any = total+*v, true
		}
	}
	if !any {
		return nil
	}
	return ptr(total)
}

// Row pairs a project with its derived state.
type Row struct {
	Project *tree.Project
	State   *tree.State
}

// Result is one refresh: rows in tree order, files that could not be read as projects,
// every link page with its facts, and the project pages whose links were upgraded.
type Result struct {
	Rows         []Row
	Problems     []tree.Problem
	Links        []LinkRow
	LinkProblems []links.Problem
	Upgraded     []string
}

// Tree derives state for every project and link page, rewrites the state directory from
// scratch, and regenerates the category pages.
func Tree(cfg *home.Config, stateDir string, today time.Time, generatedAt string) (*Result, error) {
	upgraded, err := vaults.UpgradeLinks(cfg)
	if err != nil {
		return nil, err
	}
	projects, problems, err := tree.Walk(cfg.TreeRoot())
	if err != nil {
		return nil, err
	}
	pages, linkProblems, err := links.Walk(cfg.AtlasVault)
	if err != nil {
		return nil, err
	}
	linkRows := inspectPages(pages, projects)
	facts := map[string]links.Link{}
	for _, r := range linkRows {
		facts[r.Page.Path] = r.Link
	}
	if err := os.RemoveAll(stateDir); err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(projects))
	for _, project := range projects {
		state := derive(project, today, generatedAt, facts, cfg.NewDays())
		if err := tree.WriteState(stateDir, project.Rel, state); err != nil {
			return nil, err
		}
		rows = append(rows, Row{project, state})
	}
	data, err := json.MarshalIndent(LinksState{Schema: LinksStateSchema, GeneratedAt: generatedAt, Links: linkRows}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(linksStatePath(stateDir), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	if err := WriteCategories(cfg, projects); err != nil {
		return nil, err
	}
	return &Result{Rows: rows, Problems: problems, Links: linkRows, LinkProblems: linkProblems, Upgraded: upgraded}, nil
}

var (
	heatOrder     = map[string]int{"hot": 0, "new": 1, "warm": 2, "cold": 3, "": 4}
	priorityOrder = map[string]int{"high": 0, "normal": 1, "low": 2, "someday": 3}
)

func idle(state *tree.State) string {
	switch {
	case state.DaysIdle == nil:
		return "—"
	case *state.DaysIdle == 0:
		return "today"
	default:
		return fmt.Sprintf("%dd", *state.DaysIdle)
	}
}

func intOr(v *int, def string) string {
	if v == nil {
		return def
	}
	return fmt.Sprint(*v)
}

func unfinishedTotal(u tree.Unfinished) string {
	return intOr(sumPtr([]*int{u.EmptySections, u.SeedPages, u.DeadLinks}), "—")
}

// Signals crosses authored intent with derived state; these lines are the point of the page.
func Signals(node *tree.Project, state *tree.State, today time.Time) []string {
	var notes []string
	if !state.VaultOK {
		notes = append(notes, "vault unreachable: "+state.VaultError)
	}
	if state.Legacy {
		notes = append(notes, "claude-obsidian vault; adopt it with `claude-atlas adopt "+home.Display(state.Vault)+"`")
	}
	if state.PendingRecovery {
		notes = append(notes, "an operation was interrupted; run `claude-atlas recover "+node.Rel+"`")
	}
	if state.Heat == "cold" && node.State == "active" && (node.Priority == "high" || node.Priority == "normal") {
		notes = append(notes, fmt.Sprintf("declared priority %s, active, but cold for %d days", node.Priority, *state.DaysIdle))
	}
	for _, link := range state.Links {
		if !link.OK {
			notes = append(notes, fmt.Sprintf("%s %s: %s", link.Kind, linkLabel(link), link.Error))
		}
	}
	notes = append(notes, node.Warnings...)
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
	if node.State == "blocked" {
		on := node.BlockedOn
		if on == "" {
			on = "(nothing recorded)"
		}
		notes = append(notes, "blocked on: "+on)
	}
	if node.ReviewAfter != "" {
		if t, ok := parseDate(node.ReviewAfter); !ok {
			notes = append(notes, fmt.Sprintf("review_after %q is not a date", node.ReviewAfter))
		} else if t.Before(dateOf(today)) {
			notes = append(notes, fmt.Sprintf("review date %s has passed; intent may be stale", node.ReviewAfter))
		}
	}
	return notes
}

// LinkSignals lists what is wrong across the link pages: files that are not link pages,
// and pages no project links.
func LinkSignals(res *Result) []string {
	var notes []string
	for _, problem := range res.LinkProblems {
		notes = append(notes, fmt.Sprintf("%s is not a link page: %s", problem.File, problem.Reason))
	}
	for _, row := range res.Links {
		if len(row.Projects) == 0 {
			notes = append(notes, fmt.Sprintf("%s.md is linked by no project; link it with `claude-atlas link NAME %s` or delete the page", row.Page.Rel(), row.Page.Name))
		}
	}
	return notes
}

// related lists the projects related to p, in either direction, as table cells.
func related(rows []Row, p *tree.Project) []string {
	var out []string
	seen := map[string]bool{}
	for _, rel := range p.RelatedTo {
		for _, r := range rows {
			if r.Project.Rel == rel && !seen[rel] {
				seen[rel] = true
				out = append(out, "[[tree/"+rel+"\\|"+r.Project.Name+"]]")
			}
		}
	}
	for _, r := range rows {
		if r.Project != p && !seen[r.Project.Rel] {
			for _, rel := range r.Project.RelatedTo {
				if rel == p.Rel {
					seen[r.Project.Rel] = true
					out = append(out, "[[tree/"+r.Project.Rel+"\\|"+r.Project.Name+"]]")
				}
			}
		}
	}
	return out
}

func categoryCell(category string) string {
	if category == "" {
		return "—"
	}
	return "[[" + CategoriesDir + "/" + category + "\\|" + category + "]]"
}

// Render writes the overview page: callouts and tables, nothing else.
func Render(res *Result, generatedAt string, today time.Time) string {
	rows := res.Rows
	stamp, _ := time.Parse("2006-01-02T15:04:05Z", generatedAt)
	heats := map[string]int{}
	for _, r := range rows {
		heats[r.State.Heat]++
	}
	parts := []string{fmt.Sprintf("%d project%s", len(rows), plural(len(rows)))}
	for _, h := range []string{"hot", "new", "warm", "cold"} {
		if heats[h] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", heats[h], h))
		}
	}
	if heats[""] > 0 {
		parts = append(parts, fmt.Sprintf("%d unreachable", heats[""]))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: Overview\ngenerated_at: %s\n---\n\n", generatedAt)
	fmt.Fprintf(&b, "> [!info] Generated page\n> `claude-atlas refresh` rewrites this page from every project under `tree/`. Edit a project's own page and refresh again; edits made here are lost.\n> **%s** · refreshed %s\n\n",
		strings.Join(parts, " · "), stamp.Local().Format("2006-01-02 15:04"))

	b.WriteString("## All projects\n\n")
	b.WriteString("| Heat | Project | Category | Priority | State | Idle | Pages | Tasks | Threads | Unfinished |\n")
	b.WriteString("|:--|:--|:--|:--|:--|--:|--:|--:|--:|--:|\n")
	sorted := append([]Row(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, c := sorted[i], sorted[j]
		if heatOrder[a.State.Heat] != heatOrder[c.State.Heat] {
			return heatOrder[a.State.Heat] < heatOrder[c.State.Heat]
		}
		if priorityOrder[a.Project.Priority] != priorityOrder[c.Project.Priority] {
			return priorityOrder[a.Project.Priority] < priorityOrder[c.Project.Priority]
		}
		return a.Project.Rel < c.Project.Rel
	})
	for _, r := range sorted {
		fmt.Fprintf(&b, "| %s | [[tree/%s\\|%s]] | %s | %s | %s | %s | %s | %s | %d | %s |\n",
			heatLabel(r.State.Heat), r.Project.Rel, r.Project.Name, categoryCell(r.Project.Category()),
			r.Project.Priority, r.Project.State, idle(r.State),
			intOr(r.State.Pages, "—"), taskCell(r.State), len(r.State.OpenThreads), unfinishedTotal(r.State.Unfinished))
	}

	b.WriteString("\n## Signals\n\n")
	flagged := 0
	for _, problem := range res.Problems {
		flagged++
		fmt.Fprintf(&b, "> [!failure] tree/%s.md\n> Not a project: %s.\n\n", problem.Rel, problem.Reason)
	}
	for _, r := range rows {
		for _, note := range Signals(r.Project, r.State, today) {
			flagged++
			fmt.Fprintf(&b, "> [!%s] %s\n> %s\n\n", calloutFor(note), r.Project.Rel, capitalize(note))
		}
	}
	for _, note := range LinkSignals(res) {
		flagged++
		fmt.Fprintf(&b, "> [!%s] %s\n\n", calloutFor(note), capitalize(note))
	}
	if flagged == 0 {
		b.WriteString("> [!success] Nothing needs attention\n> No project is cold against its declared priority, blocked, unreachable, or past its review date.\n\n")
	}

	if len(res.Links) > 0 {
		b.WriteString("## Repos and materials\n\n")
		b.WriteString("| Kind | Page | Folder | Projects | Found |\n|:--|:--|:--|:--|:--|\n")
		for _, row := range res.Links {
			var projects []string
			for _, rel := range row.Projects {
				name := rel
				for _, r := range rows {
					if r.Project.Rel == rel {
						name = r.Project.Name
					}
				}
				projects = append(projects, "[[tree/"+rel+"\\|"+name+"]]")
			}
			fmt.Fprintf(&b, "| %s | %s | `%s` | %s | %s |\n", row.Page.Kind, linkCell(row.Link), home.Display(row.Page.Path), dash(strings.Join(projects, ", ")), LinkSummary(row.Link))
		}
		b.WriteString("\n")
	}

	if open := openTasks(rows); len(open) > 0 {
		b.WriteString("## Tasks\n\n")
		b.WriteString("| Project | Task | Status | Priority | Due | Last touched |\n|:--|:--|:--|:--|:--|:--|\n")
		for _, t := range open {
			status := t.line.Status
			if t.line.Stale {
				status += " · stale"
			}
			fmt.Fprintf(&b, "| [[tree/%s\\|%s]] | %s | %s | %s | %s | %s |\n", t.row.Project.Rel, t.row.Project.Name, TaskLink(t.line), status, t.line.Priority, dash(t.line.Due), dash(t.line.LastTouched))
		}
		b.WriteString("\n")
	}

	category := "\x00"
	for _, r := range rows {
		if cat := r.Project.Category(); cat != category {
			category = cat
			if cat == "" {
				b.WriteString("\n## Projects\n")
			} else {
				fmt.Fprintf(&b, "\n## %s\n", cat)
			}
		}
		fmt.Fprintf(&b, "\n### %s\n\n", r.Project.Name)
		if r.Project.Purpose != "" {
			fmt.Fprintf(&b, "> [!abstract] Purpose\n> %s\n\n", strings.ReplaceAll(strings.TrimSpace(r.Project.Purpose), "\n", "\n> "))
		}
		b.WriteString("| | |\n|:--|:--|\n")
		fmt.Fprintf(&b, "| Page | [[tree/%s\\|%s]] |\n", r.Project.Rel, r.Project.Rel)
		fmt.Fprintf(&b, "| Vault | `%s` |\n", home.Display(r.Project.VaultPath()))
		fmt.Fprintf(&b, "| Priority | %s |\n| State | %s |\n", r.Project.Priority, r.Project.State)
		if r.Project.BlockedOn != "" {
			fmt.Fprintf(&b, "| Blocked on | %s |\n", r.Project.BlockedOn)
		}
		switch {
		case !r.State.VaultOK:
			fmt.Fprintf(&b, "| Heat | %s · %s |\n", heatLabel(""), r.State.VaultError)
		case r.State.LastTouched != "":
			fmt.Fprintf(&b, "| Heat | %s · last touched %s (%s) |\n", heatLabel(r.State.Heat), r.State.LastTouched, idle(r.State))
		}
		if r.State.Created != "" {
			fmt.Fprintf(&b, "| Created | %s |\n", r.State.Created)
		}
		if r.State.LastOperation != "" {
			fmt.Fprintf(&b, "| Last operation | %s |\n", r.State.LastOperation)
		}
		if r.State.Pages != nil {
			fmt.Fprintf(&b, "| Pages | %d |\n", *r.State.Pages)
		}
		u := r.State.Unfinished
		if u.EmptySections != nil || u.SeedPages != nil || u.DeadLinks != nil {
			var bits []string
			for _, kv := range []struct {
				k string
				v *int
			}{{"empty sections", u.EmptySections}, {"seed pages", u.SeedPages}, {"dead links", u.DeadLinks}} {
				if kv.v != nil {
					bits = append(bits, fmt.Sprintf("%d %s", *kv.v, kv.k))
				}
			}
			fmt.Fprintf(&b, "| Unfinished | %s |\n", strings.Join(bits, " · "))
		}
		if r.Project.ReviewAfter != "" {
			fmt.Fprintf(&b, "| Review after | %s |\n", r.Project.ReviewAfter)
		}
		for _, link := range r.State.Links {
			label := "Repo"
			if link.Kind == links.Materials {
				label = "Materials"
			}
			fmt.Fprintf(&b, "| %s | %s · %s |\n", label, linkCell(link), LinkSummary(link))
		}
		if rel := related(rows, r.Project); len(rel) > 0 {
			fmt.Fprintf(&b, "| Related | %s |\n", strings.Join(rel, ", "))
		}
		if t := r.State.Tasks; t != nil {
			fmt.Fprintf(&b, "| Tasks | %s |\n", taskCell(r.State))
		}
		if r.Project.DefinitionOfDone != "" {
			fmt.Fprintf(&b, "\n> [!success] Done when\n> %s\n", strings.TrimSpace(r.Project.DefinitionOfDone))
		}
		if t := r.State.Tasks; t != nil && len(t.Open) > 0 {
			b.WriteString("\n> [!todo] Open tasks\n")
			for i, line := range t.Open {
				if i == 5 {
					fmt.Fprintf(&b, "> - … and %d more\n", len(t.Open)-i)
					break
				}
				fmt.Fprintf(&b, "> - %s · %s · %s\n", TaskLink(line), line.Status, line.Priority)
			}
		}
		if len(r.State.OpenThreads) > 0 {
			b.WriteString("\n> [!todo] Open threads\n")
			for _, t := range r.State.OpenThreads {
				b.WriteString("> - " + PlainText(t) + "\n")
			}
		}
	}
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// taskCell summarizes a project's tasks for a table.
func taskCell(state *tree.State) string {
	t := state.Tasks
	if t == nil {
		return "—"
	}
	c := t.Counts
	if c.Open == 0 {
		if c.Notes > 0 {
			return fmt.Sprintf("0 · %d note%s", c.Notes, plural(c.Notes))
		}
		return "0"
	}
	var bits []string
	for _, kv := range []struct {
		n    int
		name string
	}{{c.Active, "active"}, {c.Blocked, "blocked"}, {c.Planned, "planned"}, {c.Planted, "planted"}} {
		if kv.n > 0 {
			bits = append(bits, fmt.Sprintf("%d %s", kv.n, kv.name))
		}
	}
	out := fmt.Sprintf("%d open (%s)", c.Open, strings.Join(bits, ", "))
	if c.Stale > 0 {
		out += fmt.Sprintf(" · %d stale", c.Stale)
	}
	if c.Notes > 0 {
		out += fmt.Sprintf(" · %d note%s", c.Notes, plural(c.Notes))
	}
	return out
}

// openTask pairs a task with its project for the cross-vault list.
type openTask struct {
	row  Row
	line tree.TaskLine
}

// openTasks lists every project's open tasks in board order: status, priority, age.
func openTasks(rows []Row) []openTask {
	var out []openTask
	for _, r := range rows {
		if r.State.Tasks == nil {
			continue
		}
		for _, line := range r.State.Tasks.Open {
			out = append(out, openTask{row: r, line: line})
		}
	}
	order := map[string]int{"active": 0, "blocked": 1, "planned": 2, "planted": 3}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].line, out[j].line
		if order[a.Status] != order[b.Status] {
			return order[a.Status] < order[b.Status]
		}
		if priorityOrder[a.Priority] != priorityOrder[b.Priority] {
			return priorityOrder[a.Priority] < priorityOrder[b.Priority]
		}
		return a.LastTouched > b.LastTouched
	})
	return out
}

func heatLabel(heat string) string {
	switch heat {
	case "new":
		return "✨ new"
	case "hot":
		return "🔥 hot"
	case "warm":
		return "🌤️ warm"
	case "cold":
		return "❄️ cold"
	default:
		return "⛔ unreachable"
	}
}

func calloutFor(note string) string {
	switch {
	case strings.HasPrefix(note, "vault unreachable"), strings.HasPrefix(note, "repo "), strings.HasPrefix(note, "materials "), strings.Contains(note, "is not a link page"):
		return "failure"
	case strings.Contains(note, "is linked by no project"):
		return "info"
	case strings.HasPrefix(note, "blocked on"), strings.HasPrefix(note, "an operation was interrupted"):
		return "danger"
	case strings.HasPrefix(note, "claude-obsidian vault"):
		return "info"
	case strings.HasPrefix(note, "review"):
		return "question"
	default:
		return "warning"
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// NowUTC is the timestamp format derived state records.
func NowUTC() string {
	return time.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
}

// Run refreshes every project and writes Overview.md; it returns the page path and the result.
func Run(cfg *home.Config, stateDir string, today time.Time) (string, *Result, error) {
	generatedAt := NowUTC()
	res, err := Tree(cfg, stateDir, today, generatedAt)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(cfg.AtlasVault, 0o755); err != nil {
		return "", nil, err
	}
	page := filepath.Join(cfg.AtlasVault, "Overview.md")
	if err := os.WriteFile(page, []byte(Render(res, generatedAt, today)), 0o644); err != nil {
		return "", nil, err
	}
	if _, err := pages.WriteGraph(cfg.AtlasVault); err != nil {
		return "", nil, err
	}
	return page, res, nil
}
