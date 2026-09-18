// Package threads is the state model of a project. A thread is one line of work: an
// issue, a feature, a chore. It moves through four stages, and each stage is a document
// in its own folder under atlas/<name>/: stubs/, specs/, plans/, receipts/. The thread's
// card under threads/ holds its identity. The stage is never set: it is the furthest
// document that exists, so a thread cannot move on without its document.
//
// There is no engine on the project side, so this package writes the pages itself. Code
// owns the cards, the board, and the first callout of every document; the rest of a
// document is prose the model and the user edit directly.
package threads

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
	// StaleDays is how long a thread with a plan may go without an update before it
	// counts as stale.
	StaleDays = 14
	// MaxTitle bounds a title derived from a note.
	MaxTitle = 80

	Stub    = "stub"
	Spec    = "spec"
	Plan    = "plan"
	Receipt = "receipt"

	Completed = "completed"
	Killed    = "killed"
)

// Stages in order; a receipt closes the thread.
var Stages = []string{Stub, Spec, Plan, Receipt}

// Outcomes are what a receipt may say.
var Outcomes = []string{Completed, Killed}

// Priorities is the atlas vocabulary.
var Priorities = []string{"high", "normal", "low", "someday"}

var (
	stageDir      = map[string]string{Stub: project.StubsDir, Spec: project.SpecsDir, Plan: project.PlansDir, Receipt: project.ReceiptsDir}
	stageRank     = map[string]int{Stub: 0, Spec: 1, Plan: 2, Receipt: 3}
	priorityOrder = map[string]int{"high": 0, "normal": 1, "low": 2, "someday": 3}
	idPattern     = regexp.MustCompile(`^thr-\d{8}-[0-9a-f]{4}$`)
	datePattern   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// Dir is the folder that holds the documents of a stage, or "".
func Dir(stage string) string { return stageDir[stage] }

// Label is a stage's name as a page shows it.
func Label(stage string) string {
	if stage == "" {
		return ""
	}
	return strings.ToUpper(stage[:1]) + stage[1:]
}

// IsCard reports whether a path relative to the project folder is a thread's card.
func IsCard(p string) bool {
	if !isMarkdown(p) || p == project.ThreadsIndex {
		return false
	}
	dir := path.Dir(p)
	return dir == project.ThreadsDir || dir == project.ArchiveDir
}

// DocStage is the stage a path relative to the project folder is a document of, or "".
func DocStage(p string) string {
	if !isMarkdown(p) {
		return ""
	}
	for _, stage := range Stages {
		if path.Dir(p) == stageDir[stage] {
			return stage
		}
	}
	return ""
}

// IsPhasePage reports whether a path relative to the project folder is a phase page.
func IsPhasePage(p string) bool { return isMarkdown(p) && path.Dir(p) == project.PhasesDir }

// Owned reports whether code owns a path relative to the project folder in full: the
// cards and the board.
func Owned(p string) bool {
	return p == project.ThreadsIndex || strings.HasPrefix(p, project.ThreadsDir+"/")
}

func isMarkdown(p string) bool { return strings.HasSuffix(strings.ToLower(p), ".md") }

// Doc is one stage document of a thread.
type Doc struct {
	Stage   string `json:"stage"`
	Path    string `json:"path"` // relative to the project folder
	Created string `json:"created"`
	// Outcome is a receipt's: completed or killed.
	Outcome string `json:"outcome,omitempty"`
}

// Thread is a card with the documents that name it.
type Thread struct {
	ID    string `json:"id"`
	Path  string `json:"path"` // the card, relative to the project folder
	Title string `json:"title"`
	// Stage is the furthest document that exists.
	Stage    string `json:"stage"`
	Outcome  string `json:"outcome,omitempty"`
	Priority string `json:"priority"`
	Phase    string `json:"phase,omitempty"`
	// Blocked says what an open thread waits on; empty when it waits on nothing.
	Blocked string `json:"blocked,omitempty"`
	Created string `json:"created"`
	Updated string `json:"updated"`
	Docs    []Doc  `json:"docs"`
}

// Closed reports whether the thread has a receipt.
func (t Thread) Closed() bool { return t.Stage == Receipt }

// Doc returns the thread's document of a stage, or nil.
func (t Thread) Doc(stage string) *Doc {
	for i := range t.Docs {
		if t.Docs[i].Stage == stage {
			return &t.Docs[i]
		}
	}
	return nil
}

// Current is the document of the thread's stage, or nil when the thread has none.
func (t Thread) Current() *Doc { return t.Doc(t.Stage) }

// Phase is what a phase page's frontmatter says: a named slice of the timeline.
type Phase struct {
	Path    string `json:"path"` // relative to the project folder
	Title   string `json:"title"`
	Order   int    `json:"order"`
	Created string `json:"created"`
	Updated string `json:"updated"`
}

// parseCard reads a thread's card. The stage on it is not read: the documents say it.
func parseCard(p string, content []byte) (*Thread, error) {
	fields, _, err := vault.Frontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if fields == nil {
		return nil, fmt.Errorf("%s: no frontmatter", p)
	}
	if vault.StringField(fields, "type") != "thread" {
		return nil, fmt.Errorf("%s: a page under %s/ needs `type: thread`", p, project.ThreadsDir)
	}
	t := &Thread{
		Path: p, ID: vault.StringField(fields, "thread_id"), Title: strings.TrimSpace(vault.StringField(fields, "title")),
		Priority: vault.StringField(fields, "priority"), Phase: strings.TrimSpace(vault.StringField(fields, "phase")),
		Blocked: strings.TrimSpace(vault.StringField(fields, "blocked")),
		Created: dateField(fields, "created"), Updated: dateField(fields, "updated"), Docs: []Doc{},
	}
	if t.Title == "" {
		t.Title = vault.PageTitle(p)
	}
	if t.Priority == "" {
		t.Priority = "normal"
	}
	if !contains(Priorities, t.Priority) {
		return nil, fmt.Errorf("%s: priority must be one of %s", p, strings.Join(Priorities, ", "))
	}
	if !idPattern.MatchString(t.ID) {
		return nil, fmt.Errorf("%s: thread_id must look like thr-20260913-3f2a", p)
	}
	for _, kv := range []struct{ k, v string }{{"created", t.Created}, {"updated", t.Updated}} {
		if !datePattern.MatchString(kv.v) {
			return nil, fmt.Errorf("%s: %s must be a date like 2026-09-13", p, kv.k)
		}
	}
	return t, nil
}

// parseDoc reads a stage document: its type is its stage, and `thread` names its thread.
func parseDoc(p string, content []byte) (*Doc, string, error) {
	stage := DocStage(p)
	fields, _, err := vault.Frontmatter(string(content))
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", p, err)
	}
	if fields == nil {
		return nil, "", fmt.Errorf("%s: no frontmatter; file a %s with the thread tool so it names its thread", p, stage)
	}
	if got := vault.StringField(fields, "type"); got != stage {
		return nil, "", fmt.Errorf("%s: a page under %s/ needs `type: %s`", p, stageDir[stage], stage)
	}
	id := vault.StringField(fields, "thread")
	if !idPattern.MatchString(id) {
		return nil, "", fmt.Errorf("%s: `thread` must hold the thread's id, like thr-20260913-3f2a", p)
	}
	d := &Doc{Stage: stage, Path: p, Created: dateField(fields, "created")}
	if stage == Receipt {
		d.Outcome = vault.StringField(fields, "outcome")
		if !contains(Outcomes, d.Outcome) {
			return nil, "", fmt.Errorf("%s: outcome must be %s", p, strings.Join(Outcomes, " or "))
		}
	}
	return d, id, nil
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

// NewID makes a thread id: the date and four hex digits.
func NewID(now time.Time) string {
	var b [2]byte
	rand.Read(b[:])
	return fmt.Sprintf("thr-%s-%s", now.Format("20060102"), hex.EncodeToString(b[:]))
}

// Problem is a page that is not valid, or a document no thread claims.
type Problem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Board is every thread and phase of a project, as the pages are now.
type Board struct {
	Threads  []Thread  `json:"threads"`
	Phases   []Phase   `json:"phases"`
	Problems []Problem `json:"problems,omitempty"`
}

func (b *Board) problem(rel string, err error) {
	b.Problems = append(b.Problems, Problem{Path: rel, Reason: strings.TrimPrefix(err.Error(), rel+": ")})
}

// Load reads every card, document, and phase page of the project. A page it cannot
// parse is a Problem, not an error; only an unreadable folder fails.
func Load(p *project.Project) (*Board, error) {
	b := &Board{Threads: []Thread{}, Phases: []Phase{}}
	byID := map[string]int{}
	for _, dir := range []string{project.ThreadsDir, project.ArchiveDir} {
		files, err := pages(p, dir)
		if err != nil {
			return nil, err
		}
		for _, rel := range files {
			if !IsCard(rel) {
				continue
			}
			data, err := os.ReadFile(p.Path(rel))
			if err != nil {
				return nil, err
			}
			t, err := parseCard(rel, data)
			if err != nil {
				b.problem(rel, err)
				continue
			}
			if i, dup := byID[t.ID]; dup {
				b.Problems = append(b.Problems, Problem{Path: rel, Reason: fmt.Sprintf("thread_id %s is also used by %s", t.ID, b.Threads[i].Path)})
				continue
			}
			byID[t.ID] = len(b.Threads)
			b.Threads = append(b.Threads, *t)
		}
	}
	for _, stage := range Stages {
		files, err := pages(p, stageDir[stage])
		if err != nil {
			return nil, err
		}
		for _, rel := range files {
			data, err := os.ReadFile(p.Path(rel))
			if err != nil {
				return nil, err
			}
			d, id, err := parseDoc(rel, data)
			if err != nil {
				b.problem(rel, err)
				continue
			}
			i, ok := byID[id]
			if !ok {
				b.Problems = append(b.Problems, Problem{Path: rel, Reason: fmt.Sprintf("no thread has the id %s", id)})
				continue
			}
			t := &b.Threads[i]
			if other := t.Doc(stage); other != nil {
				b.Problems = append(b.Problems, Problem{Path: rel, Reason: fmt.Sprintf("%s already has a %s: %s", t.Title, stage, other.Path)})
				continue
			}
			t.Docs = append(t.Docs, *d)
		}
	}
	for i := range b.Threads {
		t := &b.Threads[i]
		if len(t.Docs) == 0 {
			t.Stage = Stub
			b.Problems = append(b.Problems, Problem{Path: t.Path, Reason: "the thread has no documents; file a stub, or delete the card"})
			continue
		}
		last := t.Docs[len(t.Docs)-1]
		t.Stage, t.Outcome = last.Stage, last.Outcome
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
			b.problem(rel, err)
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
	for _, t := range b.Threads {
		if t.Phase != "" && b.Phase(t.Phase) == nil {
			b.Problems = append(b.Problems, Problem{Path: t.Path, Reason: fmt.Sprintf("phase %q has no page under %s/", t.Phase, project.PhasesDir)})
		}
	}
	return b, nil
}

// pages lists the markdown files directly under dir, relative to the project folder,
// sorted.
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
		if entry.IsDir() || strings.HasPrefix(name, ".") || !isMarkdown(name) {
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

// Less orders open threads for the board and the screens: the furthest stage first,
// blocked ones last within a stage, then priority, then age.
func Less(a, b Thread) bool {
	if stageRank[a.Stage] != stageRank[b.Stage] {
		return stageRank[a.Stage] > stageRank[b.Stage]
	}
	if (a.Blocked != "") != (b.Blocked != "") {
		return a.Blocked == ""
	}
	if priorityOrder[a.Priority] != priorityOrder[b.Priority] {
		return priorityOrder[a.Priority] < priorityOrder[b.Priority]
	}
	if a.Created != b.Created {
		return a.Created < b.Created
	}
	return a.Title < b.Title
}

// Open lists the threads with no receipt, in board order.
func (b *Board) Open() []Thread {
	var out []Thread
	for _, t := range b.Threads {
		if !t.Closed() {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return Less(out[i], out[j]) })
	return out
}

// At lists the open threads at a stage, in board order.
func (b *Board) At(stage string) []Thread {
	var out []Thread
	for _, t := range b.Open() {
		if t.Stage == stage {
			out = append(out, t)
		}
	}
	return out
}

// Closed lists the threads with a receipt, newest update first.
func (b *Board) Closed() []Thread {
	var out []Thread
	for _, t := range b.Threads {
		if t.Closed() {
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

// Find returns the thread with an id, or nil.
func (b *Board) Find(id string) *Thread {
	for i := range b.Threads {
		if b.Threads[i].ID == id {
			return &b.Threads[i]
		}
	}
	return nil
}

// Resolve finds a thread by its id, its title, or the start of its title, without regard
// to case. Open threads win over closed ones when a title matches both.
func (b *Board) Resolve(key string) (*Thread, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, errors.New("name the thread by its id or its title")
	}
	if t := b.Find(key); t != nil {
		return t, nil
	}
	for _, exact := range []bool{true, false} {
		var open, closed []*Thread
		for i := range b.Threads {
			t := &b.Threads[i]
			title := strings.ToLower(t.Title)
			if (exact && title != strings.ToLower(key)) || (!exact && !strings.HasPrefix(title, strings.ToLower(key))) {
				continue
			}
			if t.Closed() {
				closed = append(closed, t)
			} else {
				open = append(open, t)
			}
		}
		matches := open
		if len(matches) == 0 {
			matches = closed
		}
		switch len(matches) {
		case 0:
		case 1:
			return matches[0], nil
		default:
			return nil, fmt.Errorf("%q matches %d threads; use the id", key, len(matches))
		}
	}
	return nil, fmt.Errorf("no thread %q", key)
}

// ByDoc returns the thread that owns a document path, or nil.
func (b *Board) ByDoc(rel string) *Thread {
	for i := range b.Threads {
		for _, d := range b.Threads[i].Docs {
			if d.Path == rel {
				return &b.Threads[i]
			}
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

// In lists the open threads that name a phase, in board order.
func (b *Board) In(phase string) []Thread {
	var out []Thread
	for _, t := range b.Open() {
		if strings.EqualFold(t.Phase, phase) {
			out = append(out, t)
		}
	}
	return out
}

// Finished reports whether a phase is done: it has at least one thread and every thread
// in it is closed. A phase has no status of its own.
func (b *Board) Finished(phase string) bool {
	any := false
	for _, t := range b.Threads {
		if !strings.EqualFold(t.Phase, phase) {
			continue
		}
		any = true
		if !t.Closed() {
			return false
		}
	}
	return any
}

// Stale reports an open thread with a plan, not blocked, with no update for StaleDays.
func Stale(t Thread, today time.Time) bool {
	if t.Stage != Plan || t.Blocked != "" {
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
	Stub      int `json:"stub"`
	Spec      int `json:"spec"`
	Plan      int `json:"plan"`
	Blocked   int `json:"blocked"`
	Completed int `json:"completed"`
	Killed    int `json:"killed"`
	Stale     int `json:"stale"`
	// Notes counts the notes waiting in inbox/.
	Notes int `json:"notes"`
	// Phases counts the phases that still hold an open thread.
	Phases int `json:"phases"`
}

// Counts summarizes the board as of today. Notes is the caller's to fill.
func (b *Board) Counts(today time.Time) Counts {
	var c Counts
	for _, t := range b.Threads {
		switch {
		case t.Outcome == Killed:
			c.Killed++
		case t.Closed():
			c.Completed++
		case t.Stage == Spec:
			c.Spec++
		case t.Stage == Plan:
			c.Plan++
		default:
			c.Stub++
		}
		if !t.Closed() {
			c.Open++
			if t.Blocked != "" {
				c.Blocked++
			}
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

// Notes lists the notes waiting in inbox/, relative to the project folder.
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

// TitleFromText picks a title from a note: its first heading, else its first line,
// trimmed of list and heading marks and cut at a word boundary.
func TitleFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#-*> "))
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

func exists(p *project.Project, rel string) bool {
	_, err := os.Stat(p.Path(rel))
	return err == nil
}

// stemTaken reports whether any page of a thread could not take the file name stem.md.
func stemTaken(p *project.Project, stem string) bool {
	name := stem + ".md"
	if strings.EqualFold(project.ThreadsDir+"/"+name, project.ThreadsIndex) {
		return true
	}
	for _, dir := range []string{project.ThreadsDir, project.ArchiveDir, project.StubsDir, project.SpecsDir, project.PlansDir, project.ReceiptsDir} {
		if exists(p, dir+"/"+name) {
			return true
		}
	}
	return false
}

// freeStem is the file name, without its extension, that every page of a new thread
// titled title takes.
func freeStem(p *project.Project, title string) string {
	base := vault.SanitizeTitle(title)
	stem := base
	for n := 2; stemTaken(p, stem); n++ {
		stem = fmt.Sprintf("%s (%d)", base, n)
	}
	return stem
}

func stemOf(rel string) string { return strings.TrimSuffix(path.Base(rel), path.Ext(rel)) }

// New is what a thread starts from.
type New struct {
	Title string `json:"title,omitempty"`
	// Text is the stub: the user's words, kept as given. With From and no Text, the
	// note's content is the stub.
	Text     string `json:"text,omitempty"`
	Priority string `json:"priority,omitempty"`
	Phase    string `json:"phase,omitempty"`
	// From is the inbox note the thread came from, relative to the project folder; it is
	// removed once the stub exists.
	From string `json:"from,omitempty"`
}

// Start opens a thread: a card and a stub. The phase, when named, must have a page.
func Start(p *project.Project, n New, now time.Time) (*Thread, error) {
	if n.From != "" {
		rel := path.Clean(filepath.ToSlash(n.From))
		if !strings.HasPrefix(rel, project.InboxDir+"/") || !exists(p, rel) {
			return nil, fmt.Errorf("%s is not a note under %s/", n.From, project.InboxDir)
		}
		n.From = rel
		if strings.TrimSpace(n.Text) == "" {
			data, err := os.ReadFile(p.Path(rel))
			if err != nil {
				return nil, err
			}
			n.Text = string(data)
		}
	}
	n.Title = strings.TrimSpace(n.Title)
	if n.Title == "" {
		n.Title = TitleFromText(n.Text)
	}
	if n.Title == "" {
		return nil, errors.New("a thread needs a title or some text")
	}
	if n.Priority == "" {
		n.Priority = "normal"
	}
	if !contains(Priorities, n.Priority) {
		return nil, fmt.Errorf("priority must be one of %s", strings.Join(Priorities, ", "))
	}
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	n.Phase = strings.TrimSpace(n.Phase)
	if n.Phase != "" {
		ph := board.Phase(n.Phase)
		if ph == nil {
			return nil, fmt.Errorf("no phase named %q; create it with the phase tool first", n.Phase)
		}
		n.Phase = ph.Title
	}
	if err := p.EnsureFolders(); err != nil {
		return nil, err
	}
	id := NewID(now)
	for board.Find(id) != nil {
		id = NewID(now)
	}
	text := strings.TrimSpace(n.Text)
	if text == "" {
		text = n.Title
	}
	date := now.Format("2006-01-02")
	stem := freeStem(p, n.Title)
	t := Thread{ID: id, Path: project.ThreadsDir + "/" + stem + ".md", Title: n.Title, Priority: n.Priority, Phase: n.Phase, Created: date, Updated: date}
	if err := os.WriteFile(p.Path(t.Path), []byte(cardFront(t)), 0o644); err != nil {
		return nil, err
	}
	stub := Doc{Stage: Stub, Path: project.StubsDir + "/" + stem + ".md", Created: date}
	if err := os.WriteFile(p.Path(stub.Path), []byte(docPage(t, stub, text)), 0o644); err != nil {
		return nil, err
	}
	if n.From != "" {
		if err := os.Remove(p.Path(n.From)); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return synced(p, id, now)
}

// Filing is a document to file on a thread.
type Filing struct {
	Stage string `json:"stage"`
	// Text is the document's body. A receipt needs it.
	Text string `json:"text,omitempty"`
	// Outcome is a receipt's: completed or killed.
	Outcome string `json:"outcome,omitempty"`
}

// File writes a thread's document for a stage, which moves the thread to that stage when
// it is the furthest. A stage may be skipped; a document that exists is revised with
// Edit, never filed again; a receipt closes the thread.
func File(p *project.Project, key string, f Filing, now time.Time) (*Thread, error) {
	if !contains(Stages, f.Stage) {
		return nil, fmt.Errorf("stage must be one of %s", strings.Join(Stages, ", "))
	}
	if f.Stage == Receipt && !contains(Outcomes, f.Outcome) {
		return nil, fmt.Errorf("a receipt needs an outcome: %s", strings.Join(Outcomes, " or "))
	}
	if f.Stage != Receipt && f.Outcome != "" {
		return nil, errors.New("only a receipt has an outcome")
	}
	text := strings.TrimSpace(f.Text)
	if f.Stage == Receipt && text == "" {
		return nil, errors.New("a receipt needs text: what was delivered, or why the thread was killed")
	}
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	t, err := board.Resolve(key)
	if err != nil {
		return nil, err
	}
	if t.Closed() {
		return nil, fmt.Errorf("%s is closed (%s); reopen it first", t.Title, t.Outcome)
	}
	if d := t.Doc(f.Stage); d != nil {
		return nil, fmt.Errorf("%s already has a %s at %s; revise it with Edit", t.Title, f.Stage, d.Path)
	}
	if err := p.EnsureFolders(); err != nil {
		return nil, err
	}
	rel := stageDir[f.Stage] + "/" + stemOf(t.Path) + ".md"
	for n := 2; exists(p, rel); n++ {
		rel = fmt.Sprintf("%s/%s (%d).md", stageDir[f.Stage], stemOf(t.Path), n)
	}
	d := Doc{Stage: f.Stage, Path: rel, Created: now.Format("2006-01-02"), Outcome: f.Outcome}
	if err := os.WriteFile(p.Path(rel), []byte(docPage(*t, d, text)), 0o644); err != nil {
		return nil, err
	}
	if err := touch(p, t, now, f.Stage == Receipt); err != nil {
		return nil, err
	}
	return synced(p, t.ID, now)
}

// touch sets a card's `updated` to today, and clears `blocked` when the thread closes.
func touch(p *project.Project, t *Thread, now time.Time, unblock bool) error {
	data, err := os.ReadFile(p.Path(t.Path))
	if err != nil {
		return err
	}
	content := setField(string(data), "updated", now.Format("2006-01-02"))
	if unblock {
		content = setField(content, "blocked", `""`)
	}
	return os.WriteFile(p.Path(t.Path), []byte(content), 0o644)
}

// Touch marks the thread that owns a document as updated today. rel is the document's
// path relative to the project folder; any other path does nothing.
func Touch(p *project.Project, rel string, now time.Time) error {
	if DocStage(rel) == "" {
		return nil
	}
	board, err := Load(p)
	if err != nil {
		return err
	}
	t := board.ByDoc(rel)
	if t == nil || t.Updated == now.Format("2006-01-02") {
		return nil
	}
	if err := touch(p, t, now, false); err != nil {
		return err
	}
	_, err = Sync(p, now)
	return err
}

// Reopen removes a closed thread's receipt, which puts the thread back at the stage
// before it.
func Reopen(p *project.Project, key string, now time.Time) (*Thread, error) {
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	t, err := board.Resolve(key)
	if err != nil {
		return nil, err
	}
	if !t.Closed() {
		return nil, fmt.Errorf("%s is open, at its %s", t.Title, t.Stage)
	}
	if err := os.Remove(p.Path(t.Doc(Receipt).Path)); err != nil {
		return nil, err
	}
	if err := touch(p, t, now, false); err != nil {
		return nil, err
	}
	return synced(p, t.ID, now)
}

// Changes is what Set may change on a thread. A nil field is unchanged.
type Changes struct {
	Title    *string `json:"title,omitempty"`
	Priority *string `json:"priority,omitempty"`
	Phase    *string `json:"phase,omitempty"`
	Blocked  *string `json:"blocked,omitempty"`
}

// Empty reports whether nothing changes.
func (c Changes) Empty() bool {
	return c.Title == nil && c.Priority == nil && c.Phase == nil && c.Blocked == nil
}

// Set changes a thread's card. A new title renames the card and every document. With no
// change it only sets `updated` to today.
func Set(p *project.Project, key string, ch Changes, now time.Time) (*Thread, error) {
	board, err := Load(p)
	if err != nil {
		return nil, err
	}
	t, err := board.Resolve(key)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p.Path(t.Path))
	if err != nil {
		return nil, err
	}
	content := string(data)
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
	if ch.Blocked != nil {
		blocked := oneLine(*ch.Blocked)
		if blocked != "" && t.Closed() {
			return nil, fmt.Errorf("%s is closed; only an open thread can be blocked", t.Title)
		}
		content = setField(content, "blocked", strconv.Quote(blocked))
	}
	title := ""
	if ch.Title != nil {
		title = strings.TrimSpace(*ch.Title)
		if title == "" {
			return nil, errors.New("a thread needs a title")
		}
		if title == t.Title {
			title = ""
		}
	}
	if title != "" {
		content = setField(content, "title", strconv.Quote(title))
	}
	content = setField(content, "updated", now.Format("2006-01-02"))
	if err := os.WriteFile(p.Path(t.Path), []byte(content), 0o644); err != nil {
		return nil, err
	}
	if title != "" {
		if err := rename(p, t, title); err != nil {
			return nil, err
		}
	}
	return synced(p, t.ID, now)
}

// rename moves a thread's card and documents to the file name a new title gives, and
// rewrites the title each document carries.
func rename(p *project.Project, t *Thread, title string) error {
	stem := stemOf(t.Path)
	if !strings.EqualFold(vault.SanitizeTitle(title), stem) {
		stem = freeStem(p, title)
	}
	move := func(rel string) error {
		dest := path.Dir(rel) + "/" + stem + ".md"
		if dest == rel {
			return nil
		}
		return os.Rename(p.Path(rel), p.Path(dest))
	}
	for _, d := range t.Docs {
		data, err := os.ReadFile(p.Path(d.Path))
		if err != nil {
			return err
		}
		if err := os.WriteFile(p.Path(d.Path), []byte(setField(string(data), "title", strconv.Quote(title))), 0o644); err != nil {
			return err
		}
		if err := move(d.Path); err != nil {
			return err
		}
	}
	return move(t.Path)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// synced brings the generated pages up to date and returns one thread as it now is.
func synced(p *project.Project, id string, now time.Time) (*Thread, error) {
	board, err := Sync(p, now)
	if err != nil {
		return nil, err
	}
	t := board.Find(id)
	if t == nil {
		return nil, fmt.Errorf("thread %s is not readable after the change; see the threads tool", id)
	}
	return t, nil
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

// CreatePhase writes a phase page and regenerates the board. Without an order it takes
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
	if _, err := Sync(p, now); err != nil {
		return nil, err
	}
	return &Phase{Path: rel, Title: title, Order: n, Created: now.Format("2006-01-02"), Updated: now.Format("2006-01-02")}, nil
}

// RenamePhase retitles a phase, renames its page, and rewrites every thread that names it.
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
	content = replaceLead(content, phaseLead(title))
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
	for _, t := range board.Threads {
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
	if _, err := Sync(p, now); err != nil {
		return nil, err
	}
	return &Phase{Path: dest, Title: title, Order: ph.Order, Created: ph.Created, Updated: now.Format("2006-01-02")}, nil
}

// ReorderPhase sets a phase's order and regenerates the board.
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
	if _, err := Sync(p, now); err != nil {
		return nil, err
	}
	ph.Order, ph.Updated = order, now.Format("2006-01-02")
	return ph, nil
}

// RemovePhase deletes a phase page. It refuses while a thread still names the phase.
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
	for _, t := range board.Threads {
		if strings.EqualFold(t.Phase, ph.Title) {
			names = append(names, t.Title)
		}
	}
	if n := len(names); n > 0 {
		verb := "name"
		if n == 1 {
			verb = "names"
		}
		return fmt.Errorf("%d thread%s still %s the phase %q (%s); move them to another phase first", n, plural(n), verb, ph.Title, strings.Join(names, "; "))
	}
	if err := os.Remove(p.Path(ph.Path)); err != nil {
		return err
	}
	_, err = Sync(p, now)
	return err
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
