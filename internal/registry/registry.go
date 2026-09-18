// Package registry knows every knowledge base and project the atlas config lists: it
// reads each one's identity file and resolves each project's knowledge base. Nothing
// here is stored beyond what Write derives, and Scan rebuilds the whole picture from the
// folders themselves every time.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// Kind says what an entry is.
type Kind string

const (
	Knowledge Kind = "knowledge"
	Project   Kind = "project"
)

// Noun is the kind as a person says it.
func (k Kind) Noun() string {
	if k == Knowledge {
		return "knowledge base"
	}
	return string(k)
}

// The reasons an entry the atlas knows cannot be read. An Entry with an Error carries
// one, so a command decides on the code and not on the sentence.
const (
	ReasonV1         = "v1"          // a v1 identity file; adopt rewrites it
	ReasonV2Project  = "v2-project"  // a v2 project vault; init recreates the project in the work
	ReasonUnreadable = "unreadable"  // the identity file is not JSON
	ReasonSchema     = "schema"      // an identity file from a later version
	ReasonMissing    = "missing"     // a registered path whose folder is gone
	ReasonNotProject = "not-project" // a registered work folder with no atlas/project.json
	ReasonNotVault   = "not-vault"   // a registered knowledge base folder with no identity file
)

// Ref names an entry another entry refers to: a project's knowledge base, or one of a
// knowledge base's projects. Path is set when the scan found it.
type Ref struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"` // "no knowledge base with id …"
}

// Entry is one knowledge base or project the atlas knows. Refresh adds the derived State.
type Entry struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	Name string `json:"name"`
	// Path is a knowledge base's root, or a project's work folder, the parent of atlas/.
	Path    string `json:"path"`
	Created string `json:"created,omitempty"`
	// A knowledge base's fields.
	Mode  vault.Mode `json:"mode,omitempty"`
	Scope string     `json:"scope,omitempty"`
	// Projects lists the projects that use a knowledge base.
	Projects []Ref `json:"projects,omitempty"`
	// A project's fields.
	Description string `json:"description,omitempty"`
	// Knowledge is the one knowledge base a project uses, resolved against the scan; nil
	// when the project uses none.
	Knowledge *Ref `json:"knowledge,omitempty"`
	// Error is set for an entry the atlas knows but could not read. Such an entry has
	// Path, Error, and Reason, and nothing else.
	Error string `json:"error,omitempty"`
	// Reason is the code behind Error.
	Reason string `json:"reason,omitempty"`
	State  *State `json:"state,omitempty"`
}

// State is what refresh derived for one entry; the refresh package fills it.
type State struct {
	GeneratedAt string `json:"generated_at"`
	// OK is set when the entry could be read in full.
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// A knowledge base's state.
	PendingRecovery bool       `json:"pending_recovery,omitempty"`
	LastOperation   string     `json:"last_operation,omitempty"`
	Pages           *int       `json:"pages,omitempty"`
	Inbox           *int       `json:"inbox,omitempty"`
	OpenThreads     []string   `json:"open_threads,omitempty"`
	Unfinished      Unfinished `json:"unfinished"`
	// Both kinds.
	LastTouched string `json:"last_touched,omitempty"`
	DaysIdle    *int   `json:"days_idle"`
	Heat        string `json:"heat"`
	// A project's state.
	Tasks *TaskSummary `json:"tasks,omitempty"`
	// Described is the page in the project's knowledge base that describes it, when one
	// does.
	Described *Description `json:"described,omitempty"`
	// Git is what git says about the work folder, when it is a repository.
	Git *links.Link `json:"git,omitempty"`
}

// Description is the page that describes a project: an entity page in its knowledge
// base whose `project` property names the project's id or name, and the commit it was
// written from when the work is a repository. The atlas derives it and never writes it.
type Description struct {
	Page   string `json:"page"` // vault-relative path of the page
	Commit string `json:"commit,omitempty"`
	// Behind counts the commits on the work's current branch since Commit; -1 when
	// Commit is empty, the work is not a repository, or Commit is not in its history.
	Behind int `json:"behind"`
}

// Summary says where the page is and how current it is: "described in
// wiki/entities/webapp.md at fc70d93, 12 commits behind".
func (d Description) Summary() string {
	commit := d.Commit
	if len(commit) > 7 {
		commit = commit[:7]
	}
	where := "described in " + d.Page
	switch {
	case commit == "":
		return where
	case d.Behind < 0:
		return fmt.Sprintf("%s at %s, not in the repository's history", where, commit)
	case d.Behind == 0:
		return fmt.Sprintf("%s at %s, current", where, commit)
	case d.Behind == 1:
		return fmt.Sprintf("%s at %s, 1 commit behind", where, commit)
	}
	return fmt.Sprintf("%s at %s, %d commits behind", where, commit, d.Behind)
}

// NotDescribed is what every surface says for a project no page describes.
const NotDescribed = "not described in the knowledge base"

// Unfinished counts work a knowledge base still owes. nil means unknown.
type Unfinished struct {
	EmptySections *int `json:"empty_sections"`
	Stubs         *int `json:"stubs"`
	WantedPages   *int `json:"wanted_pages"`
	DeadLinks     *int `json:"dead_links"`
}

func (u Unfinished) counts() []struct {
	label string
	n     *int
} {
	return []struct {
		label string
		n     *int
	}{{"empty sections", u.EmptySections}, {"stubs", u.Stubs}, {"wanted pages", u.WantedPages}, {"dead links", u.DeadLinks}}
}

// Text lists the known counts with their labels, such as "1 empty sections · 2 stubs".
func (u Unfinished) Text() string {
	var bits []string
	for _, c := range u.counts() {
		if c.n != nil {
			bits = append(bits, fmt.Sprintf("%d %s", *c.n, c.label))
		}
	}
	return strings.Join(bits, " · ")
}

// Total adds the known counts; nil when none is known.
func (u Unfinished) Total() *int {
	var total *int
	for _, c := range u.counts() {
		if c.n != nil {
			if total == nil {
				total = new(int)
			}
			*total += *c.n
		}
	}
	return total
}

// TaskLine is one open task as the atlas shows it.
type TaskLine struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	Phase    string `json:"phase,omitempty"`
	Due      string `json:"due,omitempty"`
	Updated  string `json:"updated"`
	Path     string `json:"path"` // absolute path of the task page
	Stale    bool   `json:"stale,omitempty"`
}

// TaskSummary is what refresh read from a project's task pages.
type TaskSummary struct {
	Counts tasks.Counts `json:"counts"`
	Open   []TaskLine   `json:"open"`
	// Phases lists the phases in order, finished ones last.
	Phases []string `json:"phases,omitempty"`
}

// Problem is a path the scan could not use.
type Problem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Index is one scan.
type Index struct {
	Entries  []Entry
	Problems []Problem
}

var ErrAmbiguous = errors.New("ambiguous")
var ErrNotFound = errors.New("no such vault or project")

// ErrStale says the registry state file is one this version cannot read: another
// schema, or not JSON. The file is derived, so a refresh replaces it.
var ErrStale = errors.New("stale registry")

// Scan reads every knowledge base cfg.Knowledge lists and every project cfg.Projects
// lists, and resolves each project's knowledge base against the whole set. It searches
// no folder: a knowledge base or a project the config does not list is not in the atlas.
func Scan(cfg *home.Config) (*Index, error) {
	ix := &Index{}
	found := map[string]bool{}

	for _, v := range cfg.Knowledge {
		abs, err := filepath.Abs(v)
		if err != nil {
			abs = v
		}
		key := realPath(abs)
		if found[key] {
			continue
		}
		found[key] = true
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			fail(ix, abs, "not found; work in it again to heal the path, or run claude-atlas remove", ReasonMissing)
			continue
		}
		scanRoot(ix, abs)
	}

	for _, work := range cfg.Projects {
		abs, err := filepath.Abs(work)
		if err != nil {
			abs = work
		}
		key := realPath(abs)
		if found[key] {
			continue
		}
		found[key] = true
		scanProject(ix, abs)
	}

	sortEntries(ix.Entries)
	resolve(ix)
	return ix, nil
}

// realPath is the dedupe key of a root: two spellings of one folder, one of them through
// a symlink, resolve to the same key and yield one entry. A path that cannot be resolved,
// such as a registered folder that is gone, keeps its own spelling.
func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// markerFileExists reports whether root carries the identity file, readable or not.
func markerFileExists(root string) bool {
	info, err := os.Stat(filepath.Join(root, vault.Marker))
	return err == nil && info.Mode().IsRegular()
}

// scanRoot reads the identity file at a listed knowledge base's root and records an
// Entry: a full one when it is a knowledge base, an Entry{Path, Error} alongside a
// matching Problem when the atlas could not use it.
func scanRoot(ix *Index, root string) {
	cfg, ok := vault.ReadConfig(root)
	if !ok {
		if !markerFileExists(root) {
			fail(ix, root, fmt.Sprintf("no %s; run claude-atlas adopt %s, or claude-atlas remove", vault.Marker, root), ReasonNotVault)
			return
		}
		fail(ix, root, "identity file is not JSON; run claude-atlas adopt", ReasonUnreadable)
		return
	}
	switch cfg.Schema {
	case vault.Schema, vault.SchemaV2:
		if cfg.Kind != vault.Kind {
			if cfg.Kind == "project" {
				fail(ix, root, "a v2 project vault; run claude-atlas init in the work, then delete this folder", ReasonV2Project)
			} else {
				fail(ix, root, fmt.Sprintf("unsupported kind %q", cfg.Kind), ReasonSchema)
			}
			return
		}
		ix.Entries = append(ix.Entries, knowledgeEntry(root, cfg))
	case vault.SchemaV1:
		fail(ix, root, fmt.Sprintf("v1 vault; run claude-atlas adopt %s", root), ReasonV1)
	default:
		fail(ix, root, fmt.Sprintf("unsupported schema %q", cfg.Schema), ReasonSchema)
	}
}

// scanProject reads atlas/project.json under a registered work folder.
func scanProject(ix *Index, work string) {
	info, err := os.Stat(work)
	if err != nil || !info.IsDir() {
		fail(ix, work, "not found; work in it again to heal the path, or run claude-atlas forget", ReasonMissing)
		return
	}
	cfg, ok := project.ReadConfig(work)
	if !ok {
		if !project.IsProject(work) {
			fail(ix, work, "no "+project.Dir+"/"+project.Marker+"; run claude-atlas init there, or claude-atlas forget", ReasonNotProject)
			return
		}
		fail(ix, work, "identity file is not JSON", ReasonUnreadable)
		return
	}
	if cfg.Schema != project.Schema {
		fail(ix, work, fmt.Sprintf("unsupported schema %q", cfg.Schema), ReasonSchema)
		return
	}
	if cfg.ID == "" {
		fail(ix, work, "identity file has no id", ReasonUnreadable)
		return
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = filepath.Base(work)
	}
	e := Entry{ID: cfg.ID, Kind: Project, Name: name, Path: work, Created: cfg.Created, Description: cfg.Description}
	if cfg.Knowledge != nil && cfg.Knowledge.ID != "" {
		e.Knowledge = &Ref{ID: cfg.Knowledge.ID, Name: cfg.Knowledge.Name}
	}
	ix.Entries = append(ix.Entries, e)
}

// fail records an entry the atlas knows but could not read: the same reason in both a
// Problem and an Entry{Path, Error, Reason}.
func fail(ix *Index, root, reason, code string) {
	ix.Problems = append(ix.Problems, Problem{Path: root, Reason: reason})
	ix.Entries = append(ix.Entries, Entry{Path: root, Error: reason, Reason: code})
}

// knowledgeEntry turns a valid identity file into an Entry.
func knowledgeEntry(root string, cfg vault.Config) Entry {
	name := cfg.Name
	if name == "" {
		name = filepath.Base(root)
	}
	mode := cfg.Mode
	if mode == "" {
		mode = vault.Generic
	}
	return Entry{ID: cfg.ID, Kind: Knowledge, Name: name, Path: root, Mode: mode, Created: cfg.Created, Scope: cfg.Scope}
}

// resolve fills in every project's knowledge base and every knowledge base's projects,
// now that the whole set of entries is known.
func resolve(ix *Index) {
	byID := map[string]*Entry{}
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error == "" && e.ID != "" {
			byID[e.ID] = e
		}
	}
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error != "" || e.Kind != Project || e.Knowledge == nil {
			continue
		}
		kb, ok := byID[e.Knowledge.ID]
		if !ok || kb.Kind != Knowledge {
			e.Knowledge.Error = "no knowledge base with id " + e.Knowledge.ID + " on this machine"
			continue
		}
		e.Knowledge.Name = kb.Name
		e.Knowledge.Path = kb.Path
		kb.Projects = append(kb.Projects, Ref{ID: e.ID, Name: e.Name, Path: e.Path})
	}
}

// sortEntries orders valid entries before entries with an Error, knowledge bases before
// projects, then by lowercased name, then by path.
func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if (a.Error == "") != (b.Error == "") {
			return a.Error == ""
		}
		if a.Error != "" {
			return a.Path < b.Path
		}
		if a.Kind != b.Kind {
			return a.Kind == Knowledge
		}
		an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if an != bn {
			return an < bn
		}
		return a.Path < b.Path
	})
}

// ByID finds an entry by its id.
func (ix *Index) ByID(id string) *Entry {
	for i := range ix.Entries {
		if ix.Entries[i].ID == id {
			return &ix.Entries[i]
		}
	}
	return nil
}

// ByPath finds an entry by its root path. A path that reaches the folder through a
// symlinked parent, which is what a session hands the hooks and the server on macOS,
// where /tmp links to /private/tmp, matches the entry it resolves to.
func (ix *Index) ByPath(path string) *Entry {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	for i := range ix.Entries {
		if ix.Entries[i].Path == abs {
			return &ix.Entries[i]
		}
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil
	}
	for i := range ix.Entries {
		if entry, err := filepath.EvalSymlinks(ix.Entries[i].Path); err == nil && entry == resolved {
			return &ix.Entries[i]
		}
	}
	return nil
}

// Find matches a name without regard to case, an id or an id prefix of at least 8
// characters, or a path. Two entries with one name make it return ErrAmbiguous with
// both. kind narrows the search; "" searches both kinds.
func (ix *Index) Find(arg string, kind Kind) (*Entry, error) {
	if strings.ContainsAny(arg, "/\\") || strings.HasPrefix(arg, "~") {
		abs, err := filepath.Abs(home.Expand(arg))
		if err == nil {
			if e := ix.ByPath(abs); e != nil && e.Error == "" && (kind == "" || e.Kind == kind) {
				return e, nil
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrNotFound, arg)
	}
	var byName []*Entry
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error != "" || (kind != "" && e.Kind != kind) {
			continue
		}
		if strings.EqualFold(e.Name, arg) {
			byName = append(byName, e)
		}
	}
	if len(byName) == 1 {
		return byName[0], nil
	}
	if len(byName) > 1 {
		var paths []string
		for _, e := range byName {
			paths = append(paths, e.Path)
		}
		return nil, fmt.Errorf("%w: %s is the name of %d entries (%s); use the path or the id", ErrAmbiguous, arg, len(byName), strings.Join(paths, ", "))
	}
	var byID []*Entry
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error != "" || (kind != "" && e.Kind != kind) {
			continue
		}
		if e.ID == arg || (len(arg) >= 8 && strings.HasPrefix(e.ID, arg)) {
			byID = append(byID, e)
		}
	}
	if len(byID) == 1 {
		return byID[0], nil
	}
	if len(byID) > 1 {
		var paths []string
		for _, e := range byID {
			paths = append(paths, e.Path)
		}
		return nil, fmt.Errorf("%w: %s is the id of %d entries (%s); use the full id", ErrAmbiguous, arg, len(byID), strings.Join(paths, ", "))
	}
	what := "vault or project"
	if kind != "" {
		what = kind.Noun()
	}
	return nil, fmt.Errorf("%w: no %s named %s", ErrNotFound, what, arg)
}

// Projects lists the valid project entries.
func (ix *Index) Projects() []Entry {
	var out []Entry
	for _, e := range ix.Entries {
		if e.Error == "" && e.Kind == Project {
			out = append(out, e)
		}
	}
	return out
}

// Knowledge lists the valid knowledge base entries.
func (ix *Index) Knowledge() []Entry {
	var out []Entry
	for _, e := range ix.Entries {
		if e.Error == "" && e.Kind == Knowledge {
			out = append(out, e)
		}
	}
	return out
}

// ProjectsOf lists the valid projects that use the knowledge base with id.
func (ix *Index) ProjectsOf(id string) []Entry {
	var out []Entry
	for _, e := range ix.Projects() {
		if e.Knowledge != nil && e.Knowledge.ID == id && e.Knowledge.Error == "" {
			out = append(out, e)
		}
	}
	return out
}

// Rel is the entry's place in the view: knowledge/<name> for a knowledge base,
// projects/<knowledge base>/<name> or projects/<name> for a project, problems/<folder>
// for an entry the atlas could not read.
func (e Entry) Rel() string {
	if e.Error != "" {
		return "problems/" + filepath.Base(e.Path)
	}
	if e.Kind == Knowledge {
		return "knowledge/" + e.Name
	}
	if e.Knowledge != nil && e.Knowledge.Error == "" {
		return "projects/" + e.Knowledge.Name + "/" + e.Name
	}
	return "projects/" + e.Name
}

// Wiki is a knowledge base's wiki folder.
func (e Entry) Wiki() string { return filepath.Join(e.Path, vault.WikiDir) }

// Atlas is a project's atlas/ folder.
func (e Entry) Atlas() string { return filepath.Join(e.Path, project.Dir) }

// KnowledgePath is the root of the knowledge base a project uses, or "".
func (e Entry) KnowledgePath() string {
	if e.Knowledge == nil || e.Knowledge.Error != "" {
		return ""
	}
	return e.Knowledge.Path
}

// StateSchema is the schema the registry state file declares.
const StateSchema = "claude-atlas.registry.v2"

// registryFile is the on-disk shape of the state file.
type registryFile struct {
	Schema      string  `json:"schema"`
	GeneratedAt string  `json:"generated_at"`
	Entries     []Entry `json:"entries"`
}

// File is the registry state file under stateDir.
func File(stateDir string) string { return filepath.Join(stateDir, "registry.json") }

// Write records entries atomically: a temp file in stateDir, then a rename.
func Write(stateDir string, entries []Entry, generatedAt string) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registryFile{Schema: StateSchema, GeneratedAt: generatedAt, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(stateDir, "registry-*.json.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, File(stateDir)); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// Read loads the registry state file. It reports os.ErrNotExist when refresh has never
// run, and ErrStale when the file is not one this version reads.
func Read(stateDir string) ([]Entry, string, error) {
	data, err := os.ReadFile(File(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("%s: %w", File(stateDir), os.ErrNotExist)
	}
	if err != nil {
		return nil, "", err
	}
	var doc registryFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, "", fmt.Errorf("%w: %s: %v", ErrStale, File(stateDir), err)
	}
	if doc.Schema != StateSchema {
		return nil, "", fmt.Errorf("%w: %s: unsupported schema %q; run claude-atlas refresh", ErrStale, File(stateDir), doc.Schema)
	}
	return doc.Entries, doc.GeneratedAt, nil
}
