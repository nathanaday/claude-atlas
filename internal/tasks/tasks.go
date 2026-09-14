// Package tasks is the task model: task pages under wiki/tasks/, the ledger the core
// derives from them, and the index it renders. It validates pages; it never decides to
// write one. The transaction layer calls it before and after an operation.
package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/vault"
)

const (
	Schema = "claude-atlas.task-ledger.v1"
	// StaleDays is how long an active task may go without an operation before it counts
	// as stale.
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

// IsPage reports whether a vault-relative path is a task page: a markdown file directly
// under wiki/tasks/ or wiki/tasks/archive/, other than the index.
func IsPage(p string) bool {
	if !strings.HasSuffix(strings.ToLower(p), ".md") || p == vault.TasksIndex {
		return false
	}
	dir := path.Dir(p)
	return dir == vault.TasksDir || dir == vault.TaskArchiveDir
}

// Task is what a task page's frontmatter says.
type Task struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	Due      string `json:"due,omitempty"`
	Workdir  string `json:"workdir,omitempty"`
	Created  string `json:"created"`
	Updated  string `json:"updated"`
	// HasPlan says whether the page has a Plan section with content.
	HasPlan bool `json:"has_plan"`
}

// Parse reads a task page and checks the rules the core enforces: the type, the
// status, the priority, the id, the dates, and the folder matching the status.
func Parse(p string, content []byte) (*Task, error) {
	if !IsPage(p) {
		return nil, fmt.Errorf("%s: task pages sit directly in %s/ or %s/", p, vault.TasksDir, vault.TaskArchiveDir)
	}
	fields, body, err := vault.Frontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if fields == nil {
		return nil, fmt.Errorf("%s has no frontmatter", p)
	}
	if missing := vault.MissingFrontmatter(fields); len(missing) > 0 {
		return nil, fmt.Errorf("%s frontmatter lacks %s", p, strings.Join(missing, ", "))
	}
	t := &Task{
		Path: p, ID: vault.StringField(fields, "task_id"), Title: vault.StringField(fields, "title"),
		Status: vault.StringField(fields, "status"), Priority: vault.StringField(fields, "priority"),
		Due: dateField(fields, "due"), Workdir: vault.StringField(fields, "workdir"),
		Created: dateField(fields, "created"), Updated: dateField(fields, "updated"),
	}
	if vault.StringField(fields, "type") != "task" {
		return nil, fmt.Errorf("%s: a page under %s/ needs `type: task`", p, vault.TasksDir)
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
		return nil, fmt.Errorf("%s: task_id must look like task-20260913-3f2a; the route tool gives one", p)
	}
	for _, kv := range []struct{ k, v string }{{"created", t.Created}, {"updated", t.Updated}} {
		if !datePattern.MatchString(kv.v) {
			return nil, fmt.Errorf("%s: %s must be a date like 2026-09-13", p, kv.k)
		}
	}
	if t.Due != "" && !datePattern.MatchString(t.Due) {
		return nil, fmt.Errorf("%s: due must be empty or a date like 2026-09-13", p)
	}
	archived := path.Dir(p) == vault.TaskArchiveDir
	switch {
	case Terminal(t.Status) && !archived:
		return nil, fmt.Errorf("%s: a %s task moves to %s/ (delete this path and create it there in the same plan)", p, t.Status, vault.TaskArchiveDir)
	case !Terminal(t.Status) && archived:
		return nil, fmt.Errorf("%s: an archived task is done or cancelled; a %s task sits in %s/", p, t.Status, vault.TasksDir)
	}
	t.HasPlan = hasPlan(body)
	return t, nil
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

// Plant is what a planted task starts from.
type Plant struct {
	Title    string `json:"title"`
	Text     string `json:"text"`
	Priority string `json:"priority,omitempty"`
	Workdir  string `json:"workdir,omitempty"`
	Due      string `json:"due,omitempty"`
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
	fmt.Fprintf(&b, "---\ntype: task\ntitle: %q\nstatus: planted\npriority: %s\ncreated: %s\nupdated: %s\ntags:\n  - task\ntask_id: %s\ndue: %q\nworkdir: %q\n---\n\n# %s\n\n## Idea\n\n",
		p.Title, priority, date, date, id, p.Due, p.Workdir, p.Title)
	text := strings.TrimSpace(p.Text)
	if text == "" {
		text = p.Title
	}
	b.WriteString(text + "\n")
	return b.String()
}

// PagePath is where a new open task titled title goes; taken says whether a path is used.
func PagePath(title string, taken func(string) bool) string {
	stem := vault.SanitizeTitle(title)
	candidate := vault.TasksDir + "/" + stem + ".md"
	for n := 2; taken(candidate) || taken(vault.TaskArchiveDir+"/"+path.Base(candidate)); n++ {
		candidate = fmt.Sprintf("%s/%s (%d).md", vault.TasksDir, stem, n)
	}
	return candidate
}

// Touch is one operation that changed a task page.
type Touch struct {
	OperationID string `json:"operation_id"`
	Date        string `json:"date"`
	Summary     string `json:"summary"`
}

// Record is one task in the ledger: the page's facts and the operations behind it.
type Record struct {
	Task
	LastTouched string  `json:"last_touched"`
	History     []Touch `json:"history"`
}

// Problem is a page under wiki/tasks/ that is not a valid task.
type Problem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Ledger is the derived view of every task page.
type Ledger struct {
	Schema   string    `json:"schema"`
	Tasks    []Record  `json:"tasks"`
	Problems []Problem `json:"problems,omitempty"`
}

// Empty is the ledger of a vault with no tasks.
func Empty() Ledger { return Ledger{Schema: Schema, Tasks: []Record{}} }

// Encode renders the ledger as it is stored.
func (l Ledger) Encode() []byte {
	if l.Tasks == nil {
		l.Tasks = []Record{}
	}
	data, _ := json.MarshalIndent(l, "", "  ")
	return append(data, '\n')
}

// LoadLedger reads the stored ledger; a missing file is the empty ledger.
func LoadLedger(v *vault.Vault) (Ledger, error) {
	data, err := os.ReadFile(v.Path(vault.TaskLedgerPath))
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Ledger{}, err
	}
	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		return Ledger{}, fmt.Errorf("%s: %w", vault.TaskLedgerPath, err)
	}
	if l.Tasks == nil {
		l.Tasks = []Record{}
	}
	return l, nil
}

// Find returns the record with an id.
func (l Ledger) Find(id string) *Record {
	for i := range l.Tasks {
		if l.Tasks[i].ID == id {
			return &l.Tasks[i]
		}
	}
	return nil
}

// FindByPath returns the record at a page path.
func (l Ledger) FindByPath(p string) *Record {
	for i := range l.Tasks {
		if l.Tasks[i].Path == p {
			return &l.Tasks[i]
		}
	}
	return nil
}

// Less orders tasks for the index and the screens: status, then priority, then age.
func Less(a, b Record) bool {
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
func (l Ledger) Open() []Record {
	var out []Record
	for _, r := range l.Tasks {
		if !Terminal(r.Status) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return Less(out[i], out[j]) })
	return out
}

// Archived lists the finished tasks, newest touch first.
func (l Ledger) Archived() []Record {
	var out []Record
	for _, r := range l.Tasks {
		if Terminal(r.Status) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastTouched > out[j].LastTouched })
	return out
}

// Stale reports an active task with no touch for StaleDays.
func Stale(r Record, today time.Time) bool {
	if r.Status != "active" {
		return false
	}
	t, err := time.ParseInLocation("2006-01-02", r.LastTouched, time.Local)
	if err != nil {
		return false
	}
	return today.Sub(t).Hours()/24 >= StaleDays
}

// Counts summarizes a ledger.
type Counts struct {
	Open      int `json:"open"`
	Planted   int `json:"planted"`
	Planned   int `json:"planned"`
	Active    int `json:"active"`
	Blocked   int `json:"blocked"`
	Done      int `json:"done"`
	Cancelled int `json:"cancelled"`
	Stale     int `json:"stale"`
	// Notes counts task notes waiting in inbox/tasks/.
	Notes int `json:"notes"`
}

func (l Ledger) Counts(today time.Time) Counts {
	var c Counts
	for _, r := range l.Tasks {
		switch r.Status {
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
		if !Terminal(r.Status) {
			c.Open++
		}
		if Stale(r, today) {
			c.Stale++
		}
	}
	return c
}

// Notes lists the task notes waiting in inbox/tasks/, vault-relative.
func Notes(v *vault.Vault) []string {
	var out []string
	filepath.WalkDir(v.Path(vault.InboxTasksDir), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != v.Path(vault.InboxTasksDir) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(v.Root, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	return out
}

// pages reads every task page in the vault.
func pages(v *vault.Vault) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, dir := range []string{vault.TasksDir, vault.TaskArchiveDir} {
		entries, err := os.ReadDir(v.Path(dir))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			rel := dir + "/" + entry.Name()
			if entry.IsDir() || !IsPage(rel) || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			data, err := os.ReadFile(v.Path(rel))
			if err != nil {
				return nil, err
			}
			out[rel] = data
		}
	}
	return out, nil
}

// Build derives the ledger from the task pages. History carries over from the previous
// ledger by task id; a page the previous ledger did not know gets its history from git.
// pending, when given, is the operation about to commit, recorded on the tasks whose
// paths are in touched.
func Build(v *vault.Vault, prev Ledger, pending *Touch, touched []string, now time.Time) (Ledger, error) {
	files, err := pages(v)
	if err != nil {
		return Ledger{}, err
	}
	l := Empty()
	isTouched := map[string]bool{}
	for _, p := range touched {
		isTouched[p] = true
	}
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	seen := map[string]string{}
	for _, p := range paths {
		t, err := Parse(p, files[p])
		if err != nil {
			l.Problems = append(l.Problems, Problem{Path: p, Reason: err.Error()})
			continue
		}
		if other, dup := seen[t.ID]; dup {
			l.Problems = append(l.Problems, Problem{Path: p, Reason: fmt.Sprintf("task_id %s is also used by %s", t.ID, other)})
			continue
		}
		seen[t.ID] = p
		rec := Record{Task: *t, History: []Touch{}}
		if old := prev.Find(t.ID); old != nil {
			rec.History = append(rec.History, old.History...)
		} else {
			rec.History = gitHistory(v, p)
		}
		if pending != nil && isTouched[p] && (len(rec.History) == 0 || rec.History[len(rec.History)-1].OperationID != pending.OperationID) {
			rec.History = append(rec.History, *pending)
		}
		rec.LastTouched = t.Updated
		for _, h := range rec.History {
			if h.Date > rec.LastTouched {
				rec.LastTouched = h.Date
			}
		}
		l.Tasks = append(l.Tasks, rec)
	}
	return l, nil
}

// gitHistory reads the operations that touched a page, oldest first.
func gitHistory(v *vault.Vault, p string) []Touch {
	commits, err := v.Repo().LogFollow(p)
	if err != nil {
		return []Touch{}
	}
	out := []Touch{}
	for i := len(commits) - 1; i >= 0; i-- {
		c := commits[i]
		id := c.Trailers["atlas-operation"]
		if id == "" {
			id = c.SHA[:min(7, len(c.SHA))]
		}
		out = append(out, Touch{OperationID: id, Date: c.Date.Local().Format("2006-01-02"), Summary: c.Subject})
	}
	return out
}

// Current is the ledger as the pages are now: the stored one when it still matches
// them, else a fresh build that is not written. Reads are always right; the file
// catches up at the next task operation.
func Current(v *vault.Vault, now time.Time) (Ledger, error) {
	stored, err := LoadLedger(v)
	if err != nil {
		return Ledger{}, err
	}
	files, err := pages(v)
	if err != nil {
		return Ledger{}, err
	}
	fresh := len(files) == len(stored.Tasks)+len(stored.Problems)
	if fresh {
		for _, r := range stored.Tasks {
			data, ok := files[r.Path]
			if !ok {
				fresh = false
				break
			}
			t, err := Parse(r.Path, data)
			if err != nil || t.Status != r.Status || t.Updated != r.Updated || t.Title != r.Title || t.Priority != r.Priority {
				fresh = false
				break
			}
		}
	}
	if fresh {
		return stored, nil
	}
	return Build(v, stored, nil, nil, now)
}

// RenderIndex writes the tasks index from a ledger. created keeps the page's original
// date when the page exists.
func RenderIndex(l Ledger, existing []byte, now time.Time) string {
	today := now.Format("2006-01-02")
	created := today
	if fields, _, err := vault.Frontmatter(string(existing)); err == nil && fields != nil {
		if c := dateField(fields, "created"); datePattern.MatchString(c) {
			created = c
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntype: meta\ntitle: Tasks\nstatus: evergreen\ncreated: %s\nupdated: %s\ntags:\n  - meta\n  - tasks\n---\n\n# Tasks\n\n", created, today)
	b.WriteString("> [!info] Generated page\n> The core rewrites this page from the task pages after every task operation. Change a task on its own page, or with the task skills.\n\n")
	open := l.Open()
	b.WriteString("## Open\n\n")
	if len(open) == 0 {
		b.WriteString("No open tasks. Plant one with `claude-atlas plant`, a note in `inbox/tasks/`, or `/claude-atlas:task-plant`.\n")
	} else {
		b.WriteString("| Task | Status | Priority | Due | Last touched |\n|:--|:--|:--|:--|:--|\n")
		for _, r := range open {
			status := r.Status
			if Stale(r, now) {
				status += " · stale"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", ref(r), status, r.Priority, dash(r.Due), dash(r.LastTouched))
		}
	}
	b.WriteString("\n## Archive\n\n")
	archived := l.Archived()
	if len(archived) == 0 {
		b.WriteString("Nothing finished yet.\n")
	} else {
		b.WriteString("| Task | Status | Priority | Finished |\n|:--|:--|:--|:--|\n")
		for _, r := range archived {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", ref(r), r.Status, r.Priority, dash(r.LastTouched))
		}
	}
	if len(l.Problems) > 0 {
		b.WriteString("\n## Not readable as tasks\n\n")
		for _, p := range l.Problems {
			fmt.Fprintf(&b, "- `%s`: %s\n", p.Path, p.Reason)
		}
	}
	return b.String()
}

func ref(r Record) string {
	stem := vault.PageTitle(r.Path)
	if r.Title != "" && r.Title != stem && !strings.ContainsAny(r.Title, "[]|#") {
		return "[[" + stem + "\\|" + r.Title + "]]"
	}
	return "[[" + stem + "]]"
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
