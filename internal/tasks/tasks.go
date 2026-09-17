// Package tasks is the task model of a project: task pages under atlas/tasks/, phase
// pages under atlas/phases/, and the index the core renders from them. There is no
// engine on the project side, so this package writes the pages itself: every write
// validates the page and regenerates the index. The body of a task page is prose the
// model and the user edit directly; only the frontmatter changes through here.
package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

const (
	// StaleDays is how long an active task may go without an update before it counts as
	// stale.
	StaleDays = 14
	// MaxTitle bounds a title derived from a note.
	MaxTitle = 80
)

// Statuses in lifecycle order; the last two are terminal.
var Statuses = []string{"planted", "planned", "active", "blocked", "done", "cancelled"}

// Priorities is the atlas vocabulary.
var Priorities = []string{"high", "normal", "low", "someday"}

var (
	statusOrder   = map[string]int{"active": 0, "blocked": 1, "planned": 2, "planted": 3, "done": 4, "cancelled": 5}
	priorityOrder = map[string]int{"high": 0, "normal": 1, "low": 2, "someday": 3}
	idPattern     = regexp.MustCompile(`^task-\d{8}-[0-9a-f]{4}$`)
	datePattern   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	planHeading   = regexp.MustCompile(`(?m)^##\s+Plan\b[^\n]*\n`)
	nextHeading   = regexp.MustCompile(`(?m)^##\s`)
)

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// Terminal reports whether a status ends a task.
func Terminal(status string) bool { return status == "done" || status == "cancelled" }

// IsPage reports whether an atlas-relative path is a task page: a markdown file directly
// under tasks/ or tasks/archive/, other than the index.
func IsPage(p string) bool {
	if !strings.HasSuffix(strings.ToLower(p), ".md") || p == project.TasksIndex {
		return false
	}
	dir := path.Dir(p)
	return dir == project.TasksDir || dir == project.ArchiveDir
}

// IsPhasePage reports whether an atlas-relative path is a phase page.
func IsPhasePage(p string) bool {
	return strings.HasSuffix(strings.ToLower(p), ".md") && path.Dir(p) == project.PhasesDir
}

// Task is what a task page's frontmatter says.
type Task struct {
	ID       string `json:"id"`
	Path     string `json:"path"` // atlas-relative
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	Phase    string `json:"phase,omitempty"`
	Due      string `json:"due,omitempty"`
	Created  string `json:"created"`
	Updated  string `json:"updated"`
	// HasPlan says whether the page has a Plan section with content.
	HasPlan bool `json:"has_plan"`
}

// Phase is what a phase page's frontmatter says: a named slice of the timeline.
type Phase struct {
	Path    string `json:"path"` // atlas-relative
	Title   string `json:"title"`
	Order   int    `json:"order"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

// Parse reads a task page and checks the rules the core enforces: the type, the
// status, the priority, the id, the dates, and the folder matching the status.
func Parse(p string, content []byte) (*Task, error) {
	if !IsPage(p) {
		return nil, fmt.Errorf("%s: task pages sit directly in %s/ or %s/", p, project.TasksDir, project.ArchiveDir)
	}
	fields, body, err := vault.Frontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if fields == nil {
		return nil, fmt.Errorf("%s: no frontmatter", p)
	}
	t := &Task{
		Path: p, ID: vault.StringField(fields, "task_id"), Title: strings.TrimSpace(vault.StringField(fields, "title")),
		Status: vault.StringField(fields, "status"), Priority: vault.StringField(fields, "priority"),
		Phase: strings.TrimSpace(vault.StringField(fields, "phase")), Due: dateField(fields, "due"),
		Created: dateField(fields, "created"), Updated: dateField(fields, "updated"),
	}
	if vault.StringField(fields, "type") != "task" {
		return nil, fmt.Errorf("%s: a page under %s/ needs `type: task`", p, project.TasksDir)
	}
	if t.Title == "" {
		t.Title = vault.PageTitle(p)
	}
	if !contains(Statuses, t.Status) {
		return nil, fmt.Errorf("%s: status must be one of %s", p, strings.Join(Statuses, ", "))
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	if !contains(Priorities, t.Priority) {
		return nil, fmt.Errorf("%s: priority must be one of %s", p, strings.Join(Priorities, ", "))
	}
	if !idPattern.MatchString(t.ID) {
		return nil, fmt.Errorf("%s: task_id must look like task-20260913-3f2a", p)
	}
	for _, kv := range []struct{ k, v string }{{"created", t.Created}, {"updated", t.Updated}} {
		if !datePattern.MatchString(kv.v) {
			return nil, fmt.Errorf("%s: %s must be a date like 2026-09-13", p, kv.k)
		}
	}
	if t.Due != "" && !datePattern.MatchString(t.Due) {
		return nil, fmt.Errorf("%s: due must be empty or a date like 2026-09-13", p)
	}
	archived := path.Dir(p) == project.ArchiveDir
	switch {
	case Terminal(t.Status) && !archived:
		return nil, fmt.Errorf("%s: a %s task belongs in %s/; set the status again with the task tool to move it", p, t.Status, project.ArchiveDir)
	case !Terminal(t.Status) && archived:
		return nil, fmt.Errorf("%s: an archived task is done or cancelled; a %s task belongs in %s/", p, t.Status, project.TasksDir)
	}
	t.HasPlan = hasPlan(body)
	return t, nil
}

// ParsePhase reads a phase page.
func ParsePhase(p string, content []byte) (*Phase, error) {
	if !IsPhasePage(p) {
		return nil, fmt.Errorf("%s: phase pages sit directly in %s/", p, project.PhasesDir)
	}
	fields, _, err := vault.Frontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if fields == nil {
		return nil, fmt.Errorf("%s: no frontmatter", p)
	}
	if vault.StringField(fields, "type") != "phase" {
		return nil, fmt.Errorf("%s: a page under %s/ needs `type: phase`", p, project.PhasesDir)
	}
	ph := &Phase{Path: p, Title: strings.TrimSpace(vault.StringField(fields, "title")), Created: dateField(fields, "created"), Updated: dateField(fields, "updated")}
	if ph.Title == "" {
		ph.Title = vault.PageTitle(p)
	}
	switch v := fields["order"].(type) {
	case int:
		ph.Order = v
	case float64:
		ph.Order = int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("%s: order must be a whole number", p)
		}
		ph.Order = n
	case nil:
	default:
		return nil, fmt.Errorf("%s: order must be a whole number", p)
	}
	return ph, nil
}

// dateField reads a date property; YAML parses an unquoted date as a time.
func dateField(fields map[string]any, key string) string {
	if t, ok := fields[key].(time.Time); ok {
		return t.Format("2006-01-02")
	}
	return strings.TrimSpace(vault.StringField(fields, key))
}

// hasPlan reports whether a "## Plan" section holds anything but whitespace.
func hasPlan(body string) bool {
	loc := planHeading.FindStringIndex(body)
	if loc == nil {
		return false
	}
	rest := body[loc[1]:]
	if next := nextHeading.FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	return strings.TrimSpace(rest) != ""
}

// NewID makes a task id: the date and four hex digits.
func NewID(now time.Time) string {
	var b [2]byte
	rand.Read(b[:])
	return fmt.Sprintf("task-%s-%s", now.Format("20060102"), hex.EncodeToString(b[:]))
}

// Problem is a page under tasks/ or phases/ that is not valid.
type Problem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Board is every task and phase of a project, as the pages are now.
type Board struct {
	Tasks    []Task    `json:"tasks"`
	Phases   []Phase   `json:"phases"`
	Problems []Problem `json:"problems,omitempty"`
}

// Load reads every task and phase page of the project. A page it cannot parse is a
// Problem, not an error; only an unreadable folder fails.
func Load(p *project.Project) (*Board, error) {
	b := &Board{Tasks: []Task{}, Phases: []Phase{}}
	seen := map[string]string{}
	for _, dir := range []string{project.TasksDir, project.ArchiveDir} {
		files, err := pages(p, dir)
		if err != nil {
			return nil, err
		}
		for _, rel := range files {
			if !IsPage(rel) {
				continue
			}
			data, err := os.ReadFile(p.Path(rel))
			if err != nil {
				return nil, err
			}
			t, err := Parse(rel, data)
			if err != nil {
				b.Problems = append(b.Problems, Problem{Path: rel, Reason: strings.TrimPrefix(err.Error(), rel+": ")})
				continue
			}
			if other, dup := seen[t.ID]; dup {
				b.Problems = append(b.Problems, Problem{Path: rel, Reason: fmt.Sprintf("task_id %s is also used by %s", t.ID, other)})
				continue
			}
			seen[t.ID] = rel
			b.Tasks = append(b.Tasks, *t)
		}
	}
	files, err := pages(p, project.PhasesDir)
	if err != nil {
		return nil, err
	}
	titles := map[string]string{}
	for _, rel := range files {
		data, err := os.ReadFile(p.Path(rel))
		if err != nil {
			return nil, err
		}
		ph, err := ParsePhase(rel, data)
		if err != nil {
			b.Problems = append(b.Problems, Problem{Path: rel, Reason: strings.TrimPrefix(err.Error(), rel+": ")})
			continue
		}
		key := strings.ToLower(ph.Title)
		if other, dup := titles[key]; dup {
			b.Problems = append(b.Problems, Problem{Path: rel, Reason: fmt.Sprintf("phase %q is also the title of %s", ph.Title, other)})
			continue
		}
		titles[key] = rel
		b.Phases = append(b.Phases, *ph)
	}
	sort.SliceStable(b.Phases, func(i, j int) bool { return phaseLess(b.Phases[i], b.Phases[j]) })
	for _, t := range b.Tasks {
		if t.Phase != "" && b.Phase(t.Phase) == nil {
			b.Problems = append(b.Problems, Problem{Path: t.Path, Reason: fmt.Sprintf("phase %q has no page under %s/", t.Phase, project.PhasesDir)})
		}
	}
	return b, nil
}

// pages lists the markdown files directly under an atlas-relative folder, sorted.
func pages(p *project.Project, dir string) ([]string, error) {
	entries, err := os.ReadDir(p.Path(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		out = append(out, dir+"/"+name)
	}
	sort.Strings(out)
	return out, nil
}

func phaseLess(a, b Phase) bool {
	if a.Order != b.Order {
		return a.Order < b.Order
	}
	return strings.ToLower(a.Title) < strings.ToLower(b.Title)
}

// Less orders tasks for the index and the screens: status, then priority, then age.
func Less(a, b Task) bool {
	if statusOrder[a.Status] != statusOrder[b.Status] {
		return statusOrder[a.Status] < statusOrder[b.Status]
	}
	if priorityOrder[a.Priority] != priorityOrder[b.Priority] {
		return priorityOrder[a.Priority] < priorityOrder[b.Priority]
	}
	if a.Created != b.Created {
		return a.Created < b.Created
	}
	return a.Title < b.Title
}

// Open lists the tasks that are not finished, in index order.
func (b *Board) Open() []Task {
	var out []Task
	for _, t := range b.Tasks {
		if !Terminal(t.Status) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return Less(out[i], out[j]) })
	return out
}

// Archived lists the finished tasks, newest update first.
func (b *Board) Archived() []Task {
	var out []Task
	for _, t := range b.Tasks {
		if Terminal(t.Status) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Updated != out[j].Updated {
			return out[i].Updated > out[j].Updated
		}
		return out[i].Title < out[j].Title
	})
	return out
}

// Find returns the task with an id, or nil.
func (b *Board) Find(id string) *Task {
	for i := range b.Tasks {
		if b.Tasks[i].ID == id {
			return &b.Tasks[i]
		}
	}
	return nil
}

// FindByTitle returns the task whose title matches, without regard to case, or nil.
func (b *Board) FindByTitle(title string) *Task {
	for i := range b.Tasks {
		if strings.EqualFold(b.Tasks[i].Title, title) {
			return &b.Tasks[i]
		}
	}
	return nil
}

// Phase returns the phase whose title matches, without regard to case, or nil.
func (b *Board) Phase(title string) *Phase {
	for i := range b.Phases {
		if strings.EqualFold(b.Phases[i].Title, title) {
			return &b.Phases[i]
		}
	}
	return nil
}

// In lists the open tasks that name a phase, in index order.
func (b *Board) In(phase string) []Task {
	var out []Task
	for _, t := range b.Open() {
		if strings.EqualFold(t.Phase, phase) {
			out = append(out, t)
		}
	}
	return out
}

// Unphased lists the open tasks that name no phase, in index order.
func (b *Board) Unphased() []Task {
	var out []Task
	for _, t := range b.Open() {
		if t.Phase == "" || b.Phase(t.Phase) == nil {
			out = append(out, t)
		}
	}
	return out
}

// Finished reports whether a phase is done: it has at least one task and every task in
// it is finished. A phase has no status of its own.
func (b *Board) Finished(phase string) bool {
	any := false
	for _, t := range b.Tasks {
		if !strings.EqualFold(t.Phase, phase) {
			continue
		}
		any = true
		if !Terminal(t.Status) {
			return false
		}
	}
	return any
}

// lastFinished is the newest update among a phase's finished tasks.
func (b *Board) lastFinished(phase string) string {
	last := ""
	for _, t := range b.Tasks {
		if strings.EqualFold(t.Phase, phase) && Terminal(t.Status) && t.Updated > last {
			last = t.Updated
		}
	}
	return last
}

// Stale reports an active task with no update for StaleDays.
func Stale(t Task, today time.Time) bool {
	if t.Status != "active" {
		return false
	}
	u, err := time.ParseInLocation("2006-01-02", t.Updated, time.Local)
	if err != nil {
		return false
	}
	return today.Sub(u).Hours()/24 >= StaleDays
}

// Counts summarizes a board.
type Counts struct {
	Open      int `json:"open"`
	Planted   int `json:"planted"`
	Planned   int `json:"planned"`
	Active    int `json:"active"`
	Blocked   int `json:"blocked"`
	Done      int `json:"done"`
	Cancelled int `json:"cancelled"`
	Stale     int `json:"stale"`
	// Notes counts task notes waiting in inbox/.
	Notes int `json:"notes"`
	// Phases counts the phases that still hold an open task.
	Phases int `json:"phases"`
}

// Counts summarizes the board as of today. Notes is the caller's to fill.
func (b *Board) Counts(today time.Time) Counts {
	var c Counts
	for _, t := range b.Tasks {
		switch t.Status {
		case "planted":
			c.Planted++
		case "planned":
			c.Planned++
		case "active":
			c.Active++
		case "blocked":
			c.Blocked++
		case "done":
			c.Done++
		case "cancelled":
			c.Cancelled++
		}
		if !Terminal(t.Status) {
			c.Open++
		}
		if Stale(t, today) {
			c.Stale++
		}
	}
	for _, ph := range b.Phases {
		if len(b.In(ph.Title)) > 0 {
			c.Phases++
		}
	}
	return c
}

// Notes lists the task notes waiting in inbox/, atlas-relative.
func Notes(p *project.Project) []string {
	var out []string
	root := p.Path(project.InboxDir)
	filepath.WalkDir(root, func(q string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && q != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(p.Atlas(), q)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}

// Plant is what a planted task starts from.
type Plant struct {
	Title    string `json:"title"`
	Text     string `json:"text"`
	Priority string `json:"priority,omitempty"`
	Phase    string `json:"phase,omitempty"`
	Due      string `json:"due,omitempty"`
	// Plan is the Plan section's text; with it the task is planned, not planted.
	Plan string `json:"plan,omitempty"`
	// Start makes a planned task active, with a first Progress line.
	Start bool `json:"start,omitempty"`
	// From is the inbox note the task came from, atlas-relative; it is removed once the
	// page exists.
	From string `json:"from,omitempty"`
}

// Status is the status a plant gives the page.
func (p Plant) Status() string {
	switch {
	case p.Start:
		return "active"
	case strings.TrimSpace(p.Plan) != "":
		return "planned"
	}
	return "planted"
}

// TitleFromText picks a title from a note: its first heading, else its first line,
// trimmed of list and heading marks and cut at a word boundary.
func TitleFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "#-*> ")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > MaxTitle {
			cut := strings.LastIndex(line[:MaxTitle], " ")
			if cut < MaxTitle/2 {
				cut = MaxTitle
			}
			line = strings.TrimRight(line[:cut], " ,.;:")
		}
		return line
	}
	return ""
}

// Skeleton renders a planted task page.
func Skeleton(p Plant, id string, now time.Time) string {
	priority := p.Priority
	if priority == "" {
		priority = "normal"
	}
	date := now.Format("2006-01-02")
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntype: task\ntitle: %q\nstatus: %s\npriority: %s\nphase: %q\ndue: %q\ncreated: %s\nupdated: %s\ntask_id: %s\n---\n\n# %s\n\n## Idea\n\n",
		p.Title, p.Status(), priority, p.Phase, p.Due, date, date, id, p.Title)
	text := strings.TrimSpace(p.Text)
	if text == "" {
		text = p.Title
	}
	b.WriteString(text + "\n")
	if plan := strings.TrimSpace(p.Plan); plan != "" {
		b.WriteString("\n## Plan\n\n" + plan + "\n")
	}
	if p.Start {
		fmt.Fprintf(&b, "\n## Progress\n\n- %s · started\n", date)
	}
	return b.String()
}

// PhaseSkeleton renders a phase page.
func PhaseSkeleton(title, goal string, order int, now time.Time) string {
	date := now.Format("2006-01-02")
	goal = strings.TrimSpace(goal)
	if goal == "" {
		goal = "What this phase delivers, and how you will know it is done."
	}
	return fmt.Sprintf("---\ntype: phase\ntitle: %q\norder: %d\ncreated: %s\nupdated: %s\n---\n\n# %s\n\n## Goal\n\n%s\n", title, order, date, date, title, goal)
}

// PagePath is where a new open task titled title goes; taken says whether a path is used.
func PagePath(title string, taken func(string) bool) string {
	stem := vault.SanitizeTitle(title)
	candidate := project.TasksDir + "/" + stem + ".md"
	for n := 2; strings.EqualFold(candidate, project.TasksIndex) || taken(candidate) || taken(project.ArchiveDir+"/"+path.Base(candidate)); n++ {
		candidate = fmt.Sprintf("%s/%s (%d).md", project.TasksDir, stem, n)
	}
	return candidate
}

func exists(p *project.Project, rel string) bool {
	_, err := os.Stat(p.Path(rel))
	return err == nil
}

// PlantTask writes a new task page from a plant, removes the note it came from, and
// regenerates the index. The phase, when named, must have a page.
func PlantTask(p *project.Project, plant Plant, now time.Time) (*Task, error) {
	plant.Title = strings.TrimSpace(plant.Title)
	if plant.Title == "" {
		plant.Title = TitleFromText(plant.Text)
	}
	if plant.Title == "" {
		return nil, errors.New("a task needs a title or some text")
	}
	if plant.Priority != "" && !contains(Priorities, plant.Priority) {
		return nil, fmt.Errorf("priority must be one of %s", strings.Join(Priorities, ", "))
	}
	if plant.Due != "" && !datePattern.MatchString(plant.Due) {
		return nil, errors.New("due must be a date like 2026-09-13")
	}
	if plant.Start && strings.TrimSpace(plant.Plan) == "" {
		return nil, errors.New("start needs a plan; a task runs only once it is planned")
	}
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	plant.Phase = strings.TrimSpace(plant.Phase)
	if plant.Phase != "" {
		ph := board.Phase(plant.Phase)
		if ph == nil {
			return nil, fmt.Errorf("no phase named %q; create it with the phase tool first", plant.Phase)
		}
		plant.Phase = ph.Title
	}
	if plant.From != "" {
		rel := path.Clean(filepath.ToSlash(plant.From))
		if !strings.HasPrefix(rel, project.InboxDir+"/") || !exists(p, rel) {
			return nil, fmt.Errorf("%s is not a note under %s/", plant.From, project.InboxDir)
		}
		plant.From = rel
	}
	if err := p.EnsureFolders(); err != nil {
		return nil, err
	}
	rel := PagePath(plant.Title, func(r string) bool { return exists(p, r) })
	id := NewID(now)
	for board.Find(id) != nil {
		id = NewID(now)
	}
	if err := os.WriteFile(p.Path(rel), []byte(Skeleton(plant, id, now)), 0o644); err != nil {
		return nil, err
	}
	if plant.From != "" {
		if err := os.Remove(p.Path(plant.From)); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	if _, err := WriteIndex(p, now); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p.Path(rel))
	if err != nil {
		return nil, err
	}
	return Parse(rel, data)
}

// Changes is what Set may change on a task. A nil field is unchanged.
type Changes struct {
	Status   *string `json:"status,omitempty"`
	Priority *string `json:"priority,omitempty"`
	Phase    *string `json:"phase,omitempty"`
	Due      *string `json:"due,omitempty"`
}

// Set changes a task's frontmatter and regenerates the index. A status that crosses
// the line between open and finished moves the page between tasks/ and
// tasks/archive/. The page's body is untouched; `updated` becomes today.
func Set(p *project.Project, id string, ch Changes, now time.Time) (*Task, error) {
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	t := board.Find(id)
	if t == nil {
		if byTitle := board.FindByTitle(id); byTitle != nil {
			t = byTitle
		} else {
			return nil, fmt.Errorf("no task %s", id)
		}
	}
	data, err := os.ReadFile(p.Path(t.Path))
	if err != nil {
		return nil, err
	}
	content := string(data)
	if ch.Status != nil {
		if !contains(Statuses, *ch.Status) {
			return nil, fmt.Errorf("status must be one of %s", strings.Join(Statuses, ", "))
		}
		content = setField(content, "status", *ch.Status)
	}
	if ch.Priority != nil {
		if !contains(Priorities, *ch.Priority) {
			return nil, fmt.Errorf("priority must be one of %s", strings.Join(Priorities, ", "))
		}
		content = setField(content, "priority", *ch.Priority)
	}
	if ch.Phase != nil {
		phase := strings.TrimSpace(*ch.Phase)
		if phase != "" {
			ph := board.Phase(phase)
			if ph == nil {
				return nil, fmt.Errorf("no phase named %q; create it with the phase tool first", phase)
			}
			phase = ph.Title
		}
		content = setField(content, "phase", strconv.Quote(phase))
	}
	if ch.Due != nil {
		due := strings.TrimSpace(*ch.Due)
		if due != "" && !datePattern.MatchString(due) {
			return nil, errors.New("due must be empty or a date like 2026-09-13")
		}
		content = setField(content, "due", strconv.Quote(due))
	}
	content = setField(content, "updated", now.Format("2006-01-02"))
	status := t.Status
	if ch.Status != nil {
		status = *ch.Status
	}
	dest := t.Path
	base := path.Base(t.Path)
	switch {
	case Terminal(status) && path.Dir(t.Path) != project.ArchiveDir:
		dest = uniquePath(p, project.ArchiveDir, base)
	case !Terminal(status) && path.Dir(t.Path) != project.TasksDir:
		dest = uniquePath(p, project.TasksDir, base)
	}
	if _, err := Parse(dest, []byte(content)); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p.Path(dest)), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p.Path(dest), []byte(content), 0o644); err != nil {
		return nil, err
	}
	if dest != t.Path {
		if err := os.Remove(p.Path(t.Path)); err != nil {
			return nil, err
		}
	}
	if _, err := WriteIndex(p, now); err != nil {
		return nil, err
	}
	return Parse(dest, []byte(content))
}

// uniquePath is dir/base, or dir/base (n).md when that is taken.
func uniquePath(p *project.Project, dir, base string) string {
	stem := strings.TrimSuffix(base, path.Ext(base))
	candidate := dir + "/" + base
	for n := 2; exists(p, candidate); n++ {
		candidate = fmt.Sprintf("%s/%s (%d).md", dir, stem, n)
	}
	return candidate
}

var frontLine = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*):[^\n]*$`)

// setField replaces a scalar frontmatter line, or adds one before the closing fence, and
// leaves every other line as it is. value is written as given.
func setField(content, key, value string) string {
	front, body, ok, err := vault.SplitFrontmatter(content)
	if !ok || err != nil {
		return content
	}
	lines := strings.Split(strings.TrimRight(front, "\n"), "\n")
	done := false
	for i, line := range lines {
		if m := frontLine.FindStringSubmatch(line); m != nil && m[1] == key {
			lines[i] = key + ": " + value
			done = true
			break
		}
	}
	if !done {
		lines = append(lines, key+": "+value)
	}
	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body
}

// CreatePhase writes a phase page and regenerates the index. Without an order it takes
// the next one after the highest.
func CreatePhase(p *project.Project, title, goal string, order *int, now time.Time) (*Phase, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, errors.New("a phase needs a title")
	}
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	if board.Phase(title) != nil {
		return nil, fmt.Errorf("a phase named %q exists", title)
	}
	n := 1
	for _, ph := range board.Phases {
		if ph.Order >= n {
			n = ph.Order + 1
		}
	}
	if order != nil {
		n = *order
	}
	if err := p.EnsureFolders(); err != nil {
		return nil, err
	}
	rel := project.PhasesDir + "/" + vault.SanitizeTitle(title) + ".md"
	if exists(p, rel) {
		return nil, fmt.Errorf("%s exists; choose another title", rel)
	}
	if err := os.WriteFile(p.Path(rel), []byte(PhaseSkeleton(title, goal, n, now)), 0o644); err != nil {
		return nil, err
	}
	if _, err := WriteIndex(p, now); err != nil {
		return nil, err
	}
	return &Phase{Path: rel, Title: title, Order: n, Created: now.Format("2006-01-02"), Updated: now.Format("2006-01-02")}, nil
}

// RenamePhase retitles a phase, renames its page, and rewrites every task that names it.
func RenamePhase(p *project.Project, old, title string, now time.Time) (*Phase, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, errors.New("a phase needs a title")
	}
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	ph := board.Phase(old)
	if ph == nil {
		return nil, fmt.Errorf("no phase named %q", old)
	}
	if other := board.Phase(title); other != nil && other.Path != ph.Path {
		return nil, fmt.Errorf("a phase named %q exists", title)
	}
	data, err := os.ReadFile(p.Path(ph.Path))
	if err != nil {
		return nil, err
	}
	content := setField(setField(string(data), "title", strconv.Quote(title)), "updated", now.Format("2006-01-02"))
	dest := project.PhasesDir + "/" + vault.SanitizeTitle(title) + ".md"
	if !strings.EqualFold(dest, ph.Path) && exists(p, dest) {
		return nil, fmt.Errorf("%s exists; choose another title", dest)
	}
	if err := os.WriteFile(p.Path(dest), []byte(content), 0o644); err != nil {
		return nil, err
	}
	if !strings.EqualFold(dest, ph.Path) {
		os.Remove(p.Path(ph.Path))
	}
	for _, t := range board.Tasks {
		if !strings.EqualFold(t.Phase, ph.Title) {
			continue
		}
		data, err := os.ReadFile(p.Path(t.Path))
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(p.Path(t.Path), []byte(setField(string(data), "phase", strconv.Quote(title))), 0o644); err != nil {
			return nil, err
		}
	}
	if _, err := WriteIndex(p, now); err != nil {
		return nil, err
	}
	return &Phase{Path: dest, Title: title, Order: ph.Order, Created: ph.Created, Updated: now.Format("2006-01-02")}, nil
}

// ReorderPhase sets a phase's order and regenerates the index.
func ReorderPhase(p *project.Project, title string, order int, now time.Time) (*Phase, error) {
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	ph := board.Phase(title)
	if ph == nil {
		return nil, fmt.Errorf("no phase named %q", title)
	}
	data, err := os.ReadFile(p.Path(ph.Path))
	if err != nil {
		return nil, err
	}
	content := setField(setField(string(data), "order", strconv.Itoa(order)), "updated", now.Format("2006-01-02"))
	if err := os.WriteFile(p.Path(ph.Path), []byte(content), 0o644); err != nil {
		return nil, err
	}
	if _, err := WriteIndex(p, now); err != nil {
		return nil, err
	}
	ph.Order, ph.Updated = order, now.Format("2006-01-02")
	return ph, nil
}

// RemovePhase deletes a phase page. It refuses while a task still names the phase.
func RemovePhase(p *project.Project, title string, now time.Time) error {
	board, err := Load(p)
	if err != nil {
		return err
	}
	ph := board.Phase(title)
	if ph == nil {
		return fmt.Errorf("no phase named %q", title)
	}
	var names []string
	for _, t := range board.Tasks {
		if strings.EqualFold(t.Phase, ph.Title) {
			names = append(names, t.Title)
		}
	}
	if len(names) > 0 {
		return fmt.Errorf("%d task%s still name%s the phase %q (%s); move them to another phase first", len(names), plural(len(names)), map[bool]string{true: "s", false: ""}[len(names) == 1], ph.Title, strings.Join(names, "; "))
	}
	if err := os.Remove(p.Path(ph.Path)); err != nil {
		return err
	}
	_, err = WriteIndex(p, now)
	return err
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// WriteIndex loads the board and rewrites tasks/tasks.md from it.
func WriteIndex(p *project.Project, now time.Time) (*Board, error) {
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.Path(project.TasksDir), 0o755); err != nil {
		return nil, err
	}
	return board, os.WriteFile(p.Path(project.TasksIndex), []byte(RenderIndex(board, now)), 0o644)
}

// RenderIndex renders tasks/tasks.md: the open tasks grouped by phase in phase order,
// then the ones with no phase, the finished phases, and the archive.
func RenderIndex(b *Board, now time.Time) string {
	var w strings.Builder
	w.WriteString("# Tasks\n\n")
	w.WriteString("Generated by claude-atlas from the task and phase pages; do not edit. Change a task on its own page, or with the task tools.\n")
	header := "\n| Task | Status | Priority | Due | Updated |\n|:--|:--|:--|:--|:--|\n"
	row := func(t Task) {
		status := t.Status
		if Stale(t, now) {
			status += " · stale"
		}
		fmt.Fprintf(&w, "| %s | %s | %s | %s | %s |\n", link(t), status, t.Priority, dash(t.Due), dash(t.Updated))
	}
	open := b.Open()
	if len(open) == 0 {
		w.WriteString("\n## Open\n\nNo open tasks. Plant one with `claude-atlas plant`, a note in `inbox/`, or the task-plant skill.\n")
	}
	var finished []Phase
	for _, ph := range b.Phases {
		if b.Finished(ph.Title) {
			finished = append(finished, ph)
			continue
		}
		in := b.In(ph.Title)
		if len(in) == 0 && len(open) == 0 {
			continue
		}
		fmt.Fprintf(&w, "\n## %s (%d open)\n", ph.Title, len(in))
		if len(in) == 0 {
			w.WriteString("\nNo open tasks in this phase yet.\n")
			continue
		}
		w.WriteString(header)
		for _, t := range in {
			row(t)
		}
	}
	if unphased := b.Unphased(); len(unphased) > 0 {
		if len(b.Phases) > 0 {
			w.WriteString("\n## No phase\n")
		} else {
			w.WriteString("\n## Open\n")
		}
		w.WriteString(header)
		for _, t := range unphased {
			row(t)
		}
	}
	if len(finished) > 0 {
		w.WriteString("\n## Finished phases\n\n| Phase | Tasks | Last finished |\n|:--|:--|:--|\n")
		for _, ph := range finished {
			n := 0
			for _, t := range b.Tasks {
				if strings.EqualFold(t.Phase, ph.Title) {
					n++
				}
			}
			fmt.Fprintf(&w, "| %s | %d | %s |\n", ph.Title, n, dash(b.lastFinished(ph.Title)))
		}
	}
	w.WriteString("\n## Archive\n")
	archived := b.Archived()
	if len(archived) == 0 {
		w.WriteString("\nNothing finished yet.\n")
	} else {
		w.WriteString("\n| Task | Status | Phase | Finished |\n|:--|:--|:--|:--|\n")
		for _, t := range archived {
			fmt.Fprintf(&w, "| %s | %s | %s | %s |\n", link(t), t.Status, dash(t.Phase), dash(t.Updated))
		}
	}
	if len(b.Problems) > 0 {
		w.WriteString("\n## Not readable\n\n")
		for _, pr := range b.Problems {
			fmt.Fprintf(&w, "- `%s`: %s\n", pr.Path, pr.Reason)
		}
	}
	return w.String()
}

// link is a markdown link to a task page, relative to tasks/, so it opens in any viewer.
func link(t Task) string {
	rel := strings.TrimPrefix(t.Path, project.TasksDir+"/")
	title := strings.ReplaceAll(t.Title, "|", "\\|")
	return fmt.Sprintf("[%s](<%s>)", title, rel)
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
