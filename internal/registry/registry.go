// Package registry scans the vaults directory for identity files and resolves each
// vault's mounts and repositories. It replaces the atlas vault: nothing here is stored
// beyond what Write derives, and Scan rebuilds the whole picture from the vaults
// themselves every time.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// The reasons a vault the atlas knows cannot be read. An Entry with an Error carries one,
// so a command decides on the code and not on the sentence.
const (
	ReasonV1         = "v1"         // a v1 identity file; adopt rewrites it
	ReasonUnreadable = "unreadable" // the identity file is not JSON
	ReasonSchema     = "schema"     // an identity file from a later version
	ReasonMissing    = "missing"    // a registered path whose folder is gone
)

// maxDepth is the deepest directory level Scan searches below the vaults directory: a
// vault root at this level is found, one past it is not.
const maxDepth = 5

// Ref names a vault another entry refers to.
type Ref struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Access string `json:"access,omitempty"` // the effective access, on a mount or a mounted-by
}

// Grant is what a knowledge base grants one project, resolved against the scan: Name is
// the project's current name when the scan holds it, and Error says when it does not.
type Grant struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Access string `json:"access"`
	Error  string `json:"error,omitempty"`
}

// Mount is a project's mount resolved against the scan.
type Mount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Access    string `json:"access"`              // what the project asked for
	Effective string `json:"effective,omitempty"` // the lesser of the request and the grant; "" when unresolved
	Path      string `json:"path,omitempty"`      // the knowledge base's wiki/, when found
	Error     string `json:"error,omitempty"`     // "no knowledge base with id …"
}

// Repo is a project's repository resolved to a path.
type Repo struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"` // "" when the config names no path and <project>/repos/<name> is absent
	Remote  string `json:"remote,omitempty"`
	Changes string `json:"changes,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Entry is one vault the atlas knows: its identity file, where it is, and how its
// mounts and repositories resolve. Refresh adds the derived State.
type Entry struct {
	ID      string     `json:"id"`
	Kind    vault.Kind `json:"kind"`
	Name    string     `json:"name"`
	Path    string     `json:"path"`
	Mode    vault.Mode `json:"mode"`
	Created string     `json:"created"`
	Tags    []string   `json:"tags,omitempty"`
	Scope   string     `json:"scope,omitempty"`
	Access  string     `json:"access,omitempty"`
	Grants  []Grant    `json:"grants,omitempty"`
	Mounts  []Mount    `json:"mounts,omitempty"`
	Repos   []Repo     `json:"repos,omitempty"`
	// MountedBy lists the projects that mount a knowledge base, with their effective access.
	MountedBy []Ref `json:"mounted_by,omitempty"`
	// Error is set for a vault the atlas knows but could not read: a v1 identity file, one
	// that is not JSON, or a registered folder that is gone. Such an entry has Path,
	// Error, and Reason, and nothing else.
	Error string `json:"error,omitempty"`
	// Reason is the code behind Error: ReasonV1, ReasonUnreadable, ReasonSchema, ReasonMissing.
	Reason string `json:"reason,omitempty"`
	State  *State `json:"state,omitempty"`
}

// State is what refresh derived for one vault; the refresh package fills it.
type State struct {
	GeneratedAt     string       `json:"generated_at"`
	VaultOK         bool         `json:"vault_ok"`
	VaultError      string       `json:"vault_error,omitempty"`
	PendingRecovery bool         `json:"pending_recovery,omitempty"`
	LastOperation   string       `json:"last_operation,omitempty"`
	LastTouched     string       `json:"last_touched,omitempty"`
	DaysIdle        *int         `json:"days_idle"`
	Heat            string       `json:"heat"`
	Pages           *int         `json:"pages"`
	OpenThreads     []string     `json:"open_threads"`
	Unfinished      Unfinished   `json:"unfinished"`
	Tasks           *TaskSummary `json:"tasks,omitempty"`
	// RepoFacts pairs each repository name with what git says about it.
	RepoFacts map[string]links.Link `json:"repo_facts,omitempty"`
}

// Unfinished counts work the vault still owes. nil means unknown.
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
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Due         string `json:"due,omitempty"`
	Workdir     string `json:"workdir,omitempty"`
	LastTouched string `json:"last_touched"`
	Path        string `json:"path"` // absolute path of the task page
	Stale       bool   `json:"stale,omitempty"`
}

// TaskSummary is what refresh read from a vault's task ledger.
type TaskSummary struct {
	Counts tasks.Counts `json:"counts"`
	Open   []TaskLine   `json:"open"`
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
var ErrNotFound = errors.New("no such vault")

// Scan walks cfg.VaultsDir for identity files, reads the vaults it finds, adds the
// vaults cfg.Vaults names outside that directory, and resolves every project's mounts
// and repositories against the whole set.
func Scan(cfg *home.Config) (*Index, error) {
	ix := &Index{}
	found := map[string]bool{}

	if cfg.VaultsDir != "" {
		err := filepath.WalkDir(cfg.VaultsDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if path == cfg.VaultsDir {
					return filepath.SkipAll
				}
				return err
			}
			if !d.IsDir() {
				return nil
			}
			if path != cfg.VaultsDir {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" {
					return fs.SkipDir
				}
			}
			if markerFileExists(path) {
				abs, aerr := filepath.Abs(path)
				if aerr != nil {
					abs = path
				}
				found[abs] = true
				scanRoot(ix, abs)
				return fs.SkipDir
			}
			if path == cfg.VaultsDir {
				return nil
			}
			rel, rerr := filepath.Rel(cfg.VaultsDir, path)
			if rerr == nil {
				depth := strings.Count(filepath.ToSlash(rel), "/") + 1
				if depth >= maxDepth {
					return fs.SkipDir
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	for _, v := range cfg.Vaults {
		abs, err := filepath.Abs(v)
		if err != nil {
			abs = v
		}
		if found[abs] {
			continue
		}
		found[abs] = true
		info, err := os.Stat(abs)
		if err != nil || !info.IsDir() {
			fail(ix, abs, "not found; run claude-atlas remove PATH to forget it", ReasonMissing)
			continue
		}
		scanRoot(ix, abs)
	}

	// Sort before resolving, so a knowledge base's MountedBy is built by walking the
	// entries in their final order and comes out in that order too.
	sortEntries(ix.Entries)
	resolve(ix, cfg)
	return ix, nil
}

// markerFileExists reports whether root carries the identity file, readable or not.
func markerFileExists(root string) bool {
	info, err := os.Stat(filepath.Join(root, vault.Marker))
	return err == nil && info.Mode().IsRegular()
}

// scanRoot reads the identity file at root and records an Entry: a full one when it is
// a current vault, an Entry{Path, Error} alongside a matching Problem when the scan
// found the vault but could not read it (a bad marker, a v1 vault, or an unsupported
// schema). A path with no identity file at all is a Problem only.
func scanRoot(ix *Index, root string) {
	cfg, ok := vault.ReadConfig(root)
	if !ok {
		if !markerFileExists(root) {
			ix.Problems = append(ix.Problems, Problem{Path: root, Reason: "not found"})
			return
		}
		fail(ix, root, "identity file is not JSON; run claude-atlas adopt", ReasonUnreadable)
		return
	}
	switch cfg.Schema {
	case vault.Schema:
		ix.Entries = append(ix.Entries, buildEntry(root, cfg))
	case vault.SchemaV1:
		fail(ix, root, fmt.Sprintf("v1 vault; run claude-atlas adopt %s --as knowledge|project", root), ReasonV1)
	default:
		fail(ix, root, fmt.Sprintf("unsupported schema %q", cfg.Schema), ReasonSchema)
	}
}

// fail records a vault the atlas knows but could not read: the same reason in both a
// Problem and an Entry{Path, Error, Reason}.
func fail(ix *Index, root, reason, code string) {
	ix.Problems = append(ix.Problems, Problem{Path: root, Reason: reason})
	ix.Entries = append(ix.Entries, Entry{Path: root, Error: reason, Reason: code})
}

// buildEntry turns a valid identity file into an Entry, unresolved: a project's mounts
// and repos carry only what the identity file recorded.
func buildEntry(root string, cfg vault.Config) Entry {
	name := cfg.Name
	if name == "" {
		name = filepath.Base(root)
	}
	mode := cfg.Mode
	if mode == "" {
		mode = vault.Generic
	}
	e := Entry{
		ID:      cfg.ID,
		Kind:    cfg.Kind,
		Name:    name,
		Path:    root,
		Mode:    mode,
		Created: cfg.Created,
		Tags:    cfg.Tags,
		Scope:   cfg.Scope,
		Access:  cfg.Access,
	}
	for _, g := range cfg.Grants {
		e.Grants = append(e.Grants, Grant{ID: g.ID, Name: g.Name, Access: g.Access})
	}
	if cfg.Kind == vault.Project {
		for _, m := range cfg.Mounts {
			e.Mounts = append(e.Mounts, Mount{ID: m.ID, Name: m.Name, Access: m.Access})
		}
		for _, r := range cfg.Repos {
			e.Repos = append(e.Repos, Repo{Name: r.Name, Remote: r.Remote, Changes: r.Changes})
		}
	}
	return e
}

// resolve fills in every project's mounts and repos, and every knowledge base's
// MountedBy, now that the whole set of entries is known.
func resolve(ix *Index, cfg *home.Config) {
	byID := map[string]*Entry{}
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error == "" && e.ID != "" {
			byID[e.ID] = e
		}
	}
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error != "" || e.Kind != vault.Project {
			continue
		}
		for j := range e.Mounts {
			m := &e.Mounts[j]
			kb, ok := byID[m.ID]
			if !ok || kb.Kind != vault.Knowledge {
				m.Error = "no knowledge base with id " + m.ID
				continue
			}
			m.Path = kb.Wiki()
			m.Effective = Effective(m.Access, GrantedAccess(*kb, e.ID))
			kb.MountedBy = append(kb.MountedBy, Ref{ID: e.ID, Name: e.Name, Access: m.Effective})
		}
		for j := range e.Repos {
			r := &e.Repos[j]
			dir := e.RepoDir(r.Name)
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				r.Path = dir
				continue
			}
			if p := cfg.RepoPath(e.ID, r.Name); p != "" {
				r.Path = p
				continue
			}
			r.Error = "no folder; link it with claude-atlas link"
		}
	}
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error != "" || e.Kind != vault.Knowledge {
			continue
		}
		for j := range e.Grants {
			g := &e.Grants[j]
			if proj, ok := byID[g.ID]; ok && proj.Kind == vault.Project {
				g.Name = proj.Name
				continue
			}
			g.Error = "no project with id " + g.ID
		}
	}
}

// GrantedAccess is what a knowledge base grants a project: open grants write to everyone;
// guarded grants what the project's own grant says, or read when there is none.
func GrantedAccess(kb Entry, projectID string) string {
	switch kb.Access {
	case vault.AccessOpen:
		return vault.AccessWrite
	case vault.AccessGuarded:
		for _, g := range kb.Grants {
			if g.ID == projectID {
				return g.Access
			}
		}
		return vault.AccessRead
	default:
		return vault.AccessRead
	}
}

// Effective is the lesser of what a project asked for and what it was granted. Anything
// it does not recognize reads only, so a hand-edited identity file cannot widen access.
func Effective(request, grant string) string {
	if request == vault.AccessWrite && grant == vault.AccessWrite {
		return vault.AccessWrite
	}
	return vault.AccessRead
}

// sortEntries orders valid entries before entries with an Error, projects before
// knowledge bases, then by lowercased name, then by path.
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
			return a.Kind == vault.Project
		}
		an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if an != bn {
			return an < bn
		}
		return a.Path < b.Path
	})
}

// ByID finds an entry by its vault id.
func (ix *Index) ByID(id string) *Entry {
	for i := range ix.Entries {
		if ix.Entries[i].ID == id {
			return &ix.Entries[i]
		}
	}
	return nil
}

// ByPath finds an entry by its root path.
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
	return nil
}

// Find matches a name without regard to case, an id or an id prefix of at least 8
// characters, or a path. Two vaults with one name make it return ErrAmbiguous with both.
func (ix *Index) Find(arg string) (*Entry, error) {
	if strings.ContainsAny(arg, "/\\") || strings.HasPrefix(arg, "~") {
		abs, err := filepath.Abs(home.Expand(arg))
		if err == nil {
			if e := ix.ByPath(abs); e != nil && e.Error == "" {
				return e, nil
			}
		}
		return nil, fmt.Errorf("%w: %s", ErrNotFound, arg)
	}
	var byName []*Entry
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error != "" {
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
		return nil, fmt.Errorf("%w: %s is the name of %d vaults (%s); use the path or the id", ErrAmbiguous, arg, len(byName), strings.Join(paths, ", "))
	}
	var byID []*Entry
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error != "" {
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
		return nil, fmt.Errorf("%w: %s is the id of %d vaults (%s); use the full id", ErrAmbiguous, arg, len(byID), strings.Join(paths, ", "))
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, arg)
}

// Projects lists the valid entries whose kind is project.
func (ix *Index) Projects() []Entry {
	var out []Entry
	for _, e := range ix.Entries {
		if e.Error == "" && e.Kind == vault.Project {
			out = append(out, e)
		}
	}
	return out
}

// Knowledge lists the valid entries whose kind is knowledge.
func (ix *Index) Knowledge() []Entry {
	var out []Entry
	for _, e := range ix.Entries {
		if e.Error == "" && e.Kind == vault.Knowledge {
			out = append(out, e)
		}
	}
	return out
}

// Rel is the entry's place in the view: projects/<first tag>/<name> or projects/<name>
// for a project, knowledge/<name> for a knowledge base, problems/<folder> for a vault the
// atlas could not read.
func (e Entry) Rel() string {
	if e.Error != "" {
		return "problems/" + filepath.Base(e.Path)
	}
	if e.Kind == vault.Knowledge {
		return "knowledge/" + e.Name
	}
	if len(e.Tags) > 0 {
		return "projects/" + e.Tags[0] + "/" + e.Name
	}
	return "projects/" + e.Name
}

// Wiki is the entry's wiki folder.
func (e Entry) Wiki() string { return filepath.Join(e.Path, vault.WikiDir) }

// RepoDir is where a repository of that name sits by default.
func (e Entry) RepoDir(name string) string { return filepath.Join(e.Path, vault.ReposDir, name) }

// KbDir is where a project holds a knowledge base it mounts under that name.
func (e Entry) KbDir(name string) string { return filepath.Join(e.Path, vault.KbDir, name) }

// StateSchema is the schema the registry state file declares.
const StateSchema = "claude-atlas.registry.v1"

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
// run.
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
		return nil, "", fmt.Errorf("%s: %w", File(stateDir), err)
	}
	if doc.Schema != StateSchema {
		return nil, "", fmt.Errorf("%s: unsupported schema %q; run claude-atlas refresh", File(stateDir), doc.Schema)
	}
	return doc.Entries, doc.GeneratedAt, nil
}
