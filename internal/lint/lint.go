// Package lint checks a vault's wiki without changing it: link resolution, orphans,
// frontmatter, empty sections, index freshness, and ledger consistency. The report is
// deterministic for a given tree and audit date. Ported from claude-obsidian's engine.
package lint

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/nathanaday/claude-atlas/internal/ledger"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

const ReportVersion = 3

// Options tune one run.
type Options struct {
	// Overlay replaces files before analysis: a vault-relative path maps to its new
	// content, or to nil to treat the file as deleted. Plans use it to check their result.
	Overlay map[string][]byte
	// Exclude lists path globs (relative to the vault) to leave out of every check.
	Exclude []string
	AsOf    time.Time
	// Mounts names the knowledge bases a project mounts: a mount name maps to the path
	// of that knowledge base's wiki directory. A nil map means Run reads the symlinks
	// under kb/ itself; an empty map means the project mounts nothing.
	Mounts map[string]string
}

// LinkFinding is a link that does not resolve as written.
type LinkFinding struct {
	Source       string `json:"source"`
	Line         int    `json:"line"`
	Target       string `json:"target"`
	Syntax       string `json:"syntax"`
	Reason       string `json:"reason"`
	ResolvedPath string `json:"resolved_path,omitempty"`
	Suggestion   string `json:"suggestion,omitempty"`
}

// Ambiguous is a link with more than one candidate.
type Ambiguous struct {
	Source     string   `json:"source"`
	Line       int      `json:"line"`
	Target     string   `json:"target"`
	Syntax     string   `json:"syntax"`
	Candidates []string `json:"candidates"`
}

// LinkRef is one place a link appears.
type LinkRef struct {
	Source string `json:"source"`
	Line   int    `json:"line"`
}

// WantedPage is a page the wiki links to that nobody has written yet.
type WantedPage struct {
	Title string    `json:"title"`
	Links []LinkRef `json:"links"`
}

// Stub is a page that exists and holds nothing yet: a seed page with only headings, or an
// empty file a page links to.
type Stub struct {
	Path       string   `json:"path"`
	Empty      bool     `json:"empty"`
	LinkedFrom []string `json:"linked_from"`
}

type Duplicate struct {
	Basename string   `json:"basename"`
	Paths    []string `json:"paths"`
}

type PathFinding struct {
	Path    string `json:"path"`
	Message string `json:"message,omitempty"`
}

type FrontmatterFinding struct {
	Path           string   `json:"path"`
	HasFrontmatter bool     `json:"has_frontmatter"`
	MissingFields  []string `json:"missing_fields"`
}

type SectionFinding struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Heading string `json:"heading"`
}

type Summary struct {
	PagesScanned   int            `json:"pages_scanned"`
	LinksScanned   int            `json:"links_scanned"`
	IssuesFound    int            `json:"issues_found"`
	WantedPages    int            `json:"wanted_pages"`
	Stubs          int            `json:"stubs"`
	CategoryCounts map[string]int `json:"category_counts"`
}

// Report is the result of one run.
type Report struct {
	Version            int                  `json:"version"`
	AsOf               string               `json:"as_of"`
	Summary            Summary              `json:"summary"`
	DeadLinks          []LinkFinding        `json:"dead_links"`
	AmbiguousTargets   []Ambiguous          `json:"ambiguous_targets"`
	DuplicateBasenames []Duplicate          `json:"duplicate_basenames"`
	Orphans            []PathFinding        `json:"orphans"`
	UnindexedPages     []PathFinding        `json:"unindexed_pages"`
	MissingFrontmatter []FrontmatterFinding `json:"missing_frontmatter"`
	EmptySections      []SectionFinding     `json:"empty_sections"`
	StaleIndexEntries  []LinkFinding        `json:"stale_index_entries"`
	ReadErrors         []PathFinding        `json:"read_errors"`
	LedgerErrors       []PathFinding        `json:"ledger_errors"`
	TaskErrors         []PathFinding        `json:"task_errors"`
	KindErrors         []PathFinding        `json:"kind_errors"`
	MountErrors        []PathFinding        `json:"mount_errors"`
	WantedPages        []WantedPage         `json:"wanted_pages"`
	Stubs              []Stub               `json:"stubs"`
}

type page struct {
	path     string
	text     string
	body     string // text below the frontmatter
	masked   string
	fields   map[string]any
	hasFront bool
	frontErr error
	headings map[string]bool
	blocks   map[string]bool
	aliases  []string
	isIndex  bool // index.md, _index.md, or a folder index
	isMOC    bool // type: moc
	links    []link
}

type target struct {
	path string
	page *page
}

func (t target) withoutSuffix() (string, bool) {
	ext := strings.ToLower(path.Ext(t.path))
	if ext == ".md" || ext == ".canvas" || ext == ".base" {
		return strings.TrimSuffix(t.path, path.Ext(t.path)), true
	}
	return "", false
}

type link struct {
	source       string
	line         int
	target       string
	filePart     string
	fragment     string
	fragmentKind string // heading or block
	syntax       string
	mdRelative   bool
}

var (
	fenceOpen   = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	inlineCode  = regexp.MustCompile("`+[^`\n]*`+")
	atxHeading  = regexp.MustCompile(`(?m)^[ \t]{0,3}(#{1,6})[ \t]+(.+?)[ \t]*$`)
	blockID     = regexp.MustCompile(`(?m)(?:^|[ \t])\^([A-Za-z0-9][A-Za-z0-9_-]*)[ \t]*$`)
	wikiLink    = regexp.MustCompile(`(!)?\[\[([^\]\r\n]+?)\]\]`)
	mdLink      = regexp.MustCompile(`(!)?\[([^\]\r\n]*)\]\(([^\r\n)]*)\)`)
	htmlComment = regexp.MustCompile(`(?s)<!--.*?(?:-->|$)`)
	uriScheme   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	blockIDLine = regexp.MustCompile(`(?m)^[ \t]*\^[A-Za-z0-9][A-Za-z0-9_-]*[ \t]*$`)
)

var orphanExcluded = map[string]bool{
	"_index.md": true, "index.md": true, "log.md": true, "hot.md": true, "overview.md": true, "dashboard.md": true,
}

// folderIndexes are the index pages the layout names after their folder, so that no two
// pages share the basename index.
var folderIndexes = map[string]bool{vault.TasksIndex: true, vault.CanvasIndex: true}

// rootPages are the pages every wiki root holds, so a project and each knowledge base it
// mounts have one of each.
var rootPages = map[string]bool{"index": true, "log": true, "hot": true, "overview": true}

// mountRoot reports whether dir is a mount's top folder, kb/<name>.
func mountRoot(dir string) bool {
	name, ok := strings.CutPrefix(dir, vault.KbDir+"/")
	return ok && name != "" && !strings.Contains(name, "/")
}

// duplicateExempt reports whether a basename repeats by design. A mount brings a wiki
// root page and a folder index page of its own, and neither is the project's doing. The
// project's own pages keep every name, so the layout's rule that a folder index takes its
// folder's name still shows up as a duplicate when a page breaks it.
func duplicateExempt(rel string) bool {
	stem := strings.ToLower(strings.TrimSuffix(path.Base(rel), path.Ext(rel)))
	if stem == "_index" {
		return true
	}
	if !strings.HasPrefix(rel, vault.KbDir+"/") {
		return false
	}
	dir := path.Dir(rel)
	if strings.EqualFold(path.Base(dir), stem) {
		return true
	}
	return rootPages[stem] && mountRoot(dir)
}

// taskErrors checks a task page: the rules the core enforces, a plan where the status
// promises one, and an active task nobody has touched for tasks.StaleDays.
func taskErrors(pg *page, asOf time.Time) []PathFinding {
	t, err := tasks.Parse(pg.path, []byte(pg.text))
	if err != nil {
		return []PathFinding{{Path: pg.path, Message: strings.TrimPrefix(err.Error(), pg.path+": ")}}
	}
	var out []PathFinding
	if (t.Status == "planned" || t.Status == "active") && !t.HasPlan {
		out = append(out, PathFinding{Path: pg.path, Message: t.Status + " without a Plan section; run task-plan or set the status back to planted"})
	}
	if t.Status == "active" {
		if updated, err := time.ParseInLocation("2006-01-02", t.Updated, time.Local); err == nil && asOf.Sub(updated).Hours()/24 >= tasks.StaleDays {
			out = append(out, PathFinding{Path: pg.path, Message: fmt.Sprintf("active but untouched since %s; continue it, block it, or finish it", t.Updated)})
		}
	}
	return out
}

// knowledgeHasNo says why each project-only folder is out of place in a knowledge base.
var knowledgeHasNo = map[string]string{
	vault.InboxDir:     "sources enter through a project that mounts it",
	vault.IdeasDir:     "ideas live in a project",
	vault.TasksDir:     "tasks live in a project",
	vault.QuestionsDir: "move its pages to a project or delete them",
	vault.SessionsDir:  "move its pages to a project or delete them",
}

// kindErrors checks the vault against its kind. A knowledge base has none of the
// project-only paths and carries no project fields; a project carries no knowledge base
// fields. A tree without a current identity file is not checked.
func kindErrors(root string, present map[string]bool) []PathFinding {
	// Reads the file on disk, not the overlay; a plan that rewrites the identity file
	// (mount, grant) will need the overlay here.
	cfg, ok := vault.ReadConfig(root)
	if !ok || cfg.Schema != vault.Schema {
		return nil
	}
	var out []PathFinding
	switch cfg.Kind {
	case vault.Knowledge:
		for _, rel := range vault.ProjectOnly {
			if rel == vault.TaskLedgerPath {
				if present[rel] {
					out = append(out, PathFinding{Path: rel, Message: "a knowledge base has no task ledger"})
				}
				continue
			}
			if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil && info.IsDir() {
				out = append(out, PathFinding{Path: rel, Message: "a knowledge base has no " + rel + "/; " + knowledgeHasNo[rel]})
			}
		}
		if len(cfg.Tags)+len(cfg.Mounts)+len(cfg.Repos) > 0 {
			out = append(out, PathFinding{Path: vault.Marker, Message: "a knowledge base carries no tags, mounts, or repos; those are a project's fields"})
		}
	case vault.Project:
		if cfg.Scope != "" || cfg.Access != "" || len(cfg.Grants) > 0 {
			out = append(out, PathFinding{Path: vault.Marker, Message: "a project carries no scope, access, or grants; those are a knowledge base's fields"})
		}
	}
	return out
}

// readMounts lists the knowledge bases a project mounts, in name order: the mount name
// and the wiki directory it leads to. given replaces the scan of kb/; a nil map means
// read the symlinks. A mount that leads nowhere is left out and named in the findings.
func readMounts(root string, given map[string]string) ([]string, map[string]string, []PathFinding) {
	paths := map[string]string{}
	var names []string
	var errs []PathFinding
	rel := func(name string) string { return vault.KbDir + "/" + name }
	// keep takes a mount whose target is a directory named wiki, and names the mistake
	// otherwise. shown is the target as the user wrote it.
	keep := func(name, dir, shown string) {
		switch info, err := os.Stat(dir); {
		case err != nil || !info.IsDir():
			errs = append(errs, PathFinding{Path: rel(name), Message: "the mount is not a directory: " + shown})
		case filepath.Base(dir) != vault.WikiDir:
			errs = append(errs, PathFinding{Path: rel(name), Message: "target is not a wiki directory: " + shown})
		default:
			names = append(names, name)
			paths[name] = dir
		}
	}
	if given != nil {
		var wanted []string
		for name := range given {
			wanted = append(wanted, name)
		}
		sort.Strings(wanted)
		for _, name := range wanted {
			keep(name, given[name], given[name])
		}
		return names, paths, errs
	}
	entries, err := os.ReadDir(filepath.Join(root, vault.KbDir))
	if err != nil {
		return nil, paths, nil
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		link := filepath.Join(root, vault.KbDir, name)
		target, err := os.Readlink(link)
		if err != nil {
			errs = append(errs, PathFinding{Path: rel(name), Message: "not a mount: a mount is a symlink to a knowledge base's wiki"})
			continue
		}
		dir, err := filepath.EvalSymlinks(link)
		if err != nil {
			errs = append(errs, PathFinding{Path: rel(name), Message: "the mount points at nothing: " + target})
			continue
		}
		keep(name, dir, target)
	}
	return names, paths, errs
}

// mountTargets walks every mounted knowledge base into targets a link can reach. A target
// keeps the path the project reads it at, kb/<name>/<path under the wiki>, and its pages
// are parsed for headings, blocks, and aliases only: they are not the project's pages.
func mountTargets(root string, opts Options) ([]target, []PathFinding) {
	names, paths, errs := readMounts(root, opts.Mounts)
	var out []target
	for _, name := range names {
		dir := paths[name]
		files, err := walk(dir)
		if err != nil {
			errs = append(errs, PathFinding{Path: vault.KbDir + "/" + name, Message: "unable to read the mount: " + err.Error()})
			continue
		}
		for _, rel := range files {
			p := vault.KbDir + "/" + name + "/" + rel
			if excluded(p, opts.Exclude) {
				continue
			}
			t := target{path: p}
			if strings.EqualFold(path.Ext(rel), ".md") {
				if data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))); err == nil {
					t.page = parsePage(p, string(data))
				}
			}
			out = append(out, t)
		}
	}
	return out, errs
}

// Run lints the vault at root.
func Run(root string, opts Options) (*Report, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("vault root is not a directory: %s", root)
	}
	asOf := opts.AsOf
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	files, err := walk(root, vault.KbDir, vault.ReposDir)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, f := range files {
		present[f] = true
	}
	for rel, content := range opts.Overlay {
		if content == nil {
			delete(present, rel)
		} else {
			present[rel] = true
		}
	}
	report := &Report{Version: ReportVersion, AsOf: asOf.Format("2006-01-02")}
	var paths []string
	for rel := range present {
		if excluded(rel, opts.Exclude) {
			continue
		}
		paths = append(paths, rel)
	}
	sort.Slice(paths, func(i, j int) bool { return pathLess(paths[i], paths[j]) })

	var pages []*page
	var targets []target
	for _, rel := range paths {
		var pg *page
		if strings.HasPrefix(rel, "wiki/") && strings.EqualFold(path.Ext(rel), ".md") {
			data, ok := opts.Overlay[rel]
			if !ok {
				data, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
				if err != nil {
					report.ReadErrors = append(report.ReadErrors, PathFinding{Path: rel, Message: "unable to read page"})
					targets = append(targets, target{path: rel})
					continue
				}
			}
			pg = parsePage(rel, string(data))
			if pg.frontErr != nil {
				report.ReadErrors = append(report.ReadErrors, PathFinding{Path: rel, Message: pg.frontErr.Error()})
			}
			pages = append(pages, pg)
		}
		targets = append(targets, target{path: rel, page: pg})
	}

	mounted, mountErrs := mountTargets(root, opts)
	report.MountErrors = mountErrs

	resolver := newResolver(targets, mounted)
	near := newNearIndex(targets, mounted)
	wanted := map[string]*WantedPage{}
	incoming := map[string]map[string]bool{}
	for _, pg := range pages {
		incoming[pg.path] = map[string]bool{}
	}
	links := 0
	for _, pg := range pages {
		for _, l := range pg.links {
			links++
			candidates := resolver.resolve(l)
			if len(candidates) > 1 {
				var names []string
				for _, c := range candidates {
					names = append(names, c.path)
				}
				entry := Ambiguous{Source: l.source, Line: l.line, Target: l.target, Syntax: l.syntax, Candidates: names}
				report.AmbiguousTargets = append(report.AmbiguousTargets, entry)
				if pg.isIndex {
					report.StaleIndexEntries = append(report.StaleIndexEntries, LinkFinding{Source: l.source, Line: l.line, Target: l.target, Syntax: l.syntax, Reason: "ambiguous-target"})
				}
				continue
			}
			if len(candidates) == 0 {
				entry := LinkFinding{Source: l.source, Line: l.line, Target: l.target, Syntax: l.syntax, Reason: "target-not-found"}
				if title, ok := wantedTitle(l, pg); ok {
					if entry.Suggestion = near.match(title); entry.Suggestion == "" {
						key := strings.ToLower(title)
						if wanted[key] == nil {
							wanted[key] = &WantedPage{Title: title}
						}
						wanted[key].Links = append(wanted[key].Links, LinkRef{Source: l.source, Line: l.line})
						continue
					}
				}
				report.DeadLinks = append(report.DeadLinks, entry)
				if pg.isIndex {
					report.StaleIndexEntries = append(report.StaleIndexEntries, entry)
				}
				continue
			}
			c := candidates[0]
			if _, ok := incoming[c.path]; ok && c.path != l.source {
				incoming[c.path][l.source] = true
			}
			if reason := fragmentError(l, c); reason != "" {
				entry := LinkFinding{Source: l.source, Line: l.line, Target: l.target, Syntax: l.syntax, Reason: reason, ResolvedPath: c.path}
				report.DeadLinks = append(report.DeadLinks, entry)
				if pg.isIndex {
					report.StaleIndexEntries = append(report.StaleIndexEntries, entry)
				}
			}
		}
	}

	for _, w := range wanted {
		report.WantedPages = append(report.WantedPages, *w)
	}

	// A basename is a duplicate wherever the project reads it: in its own wiki, or
	// through a mount, where Obsidian shows it beside the project's own pages.
	byStem := map[string][]string{}
	addStem := func(rel string) {
		if duplicateExempt(rel) {
			return
		}
		stem := strings.ToLower(strings.TrimSuffix(path.Base(rel), path.Ext(rel)))
		byStem[stem] = append(byStem[stem], rel)
	}
	for _, pg := range pages {
		addStem(pg.path)
	}
	for _, t := range mounted {
		if t.page != nil {
			addStem(t.path)
		}
	}
	for _, names := range byStem {
		if len(names) < 2 {
			continue
		}
		sort.Slice(names, func(i, j int) bool { return pathLess(names[i], names[j]) })
		report.DuplicateBasenames = append(report.DuplicateBasenames, Duplicate{Basename: strings.TrimSuffix(path.Base(names[0]), path.Ext(names[0])), Paths: names})
	}

	indexPages := map[string]bool{}
	for _, pg := range pages {
		if pg.isIndex || pg.isMOC {
			indexPages[pg.path] = true
		}
	}
	stubs := map[string]bool{}
	for _, pg := range pages {
		if s, ok := stubOf(pg, incoming[pg.path]); ok {
			stubs[pg.path] = true
			report.Stubs = append(report.Stubs, s)
		}
	}
	for _, pg := range pages {
		if !orphanCandidate(pg.path) {
			continue
		}
		navigational, catalogued := false, false
		for src := range incoming[pg.path] {
			if path.Base(src) != "log.md" {
				navigational = true
			}
			if indexPages[src] {
				catalogued = true
			}
		}
		if !navigational {
			report.Orphans = append(report.Orphans, PathFinding{Path: pg.path})
		}
		if !catalogued && !stubs[pg.path] {
			report.UnindexedPages = append(report.UnindexedPages, PathFinding{Path: pg.path})
		}
	}

	for _, pg := range pages {
		emptyStub := stubs[pg.path] && strings.TrimSpace(pg.text) == ""
		if pg.frontErr == nil && !emptyStub {
			if missing := vault.MissingFrontmatter(pg.fields); len(missing) > 0 {
				report.MissingFrontmatter = append(report.MissingFrontmatter, FrontmatterFinding{Path: pg.path, HasFrontmatter: pg.hasFront, MissingFields: missing})
			}
		}
		if !stubs[pg.path] {
			report.EmptySections = append(report.EmptySections, emptySections(pg)...)
		}
	}

	report.LedgerErrors = ledgerErrors(root, opts.Overlay, present, asOf)
	report.KindErrors = kindErrors(root, present)
	for _, pg := range pages {
		if tasks.IsPage(pg.path) {
			report.TaskErrors = append(report.TaskErrors, taskErrors(pg, asOf)...)
		}
	}

	sortFindings(report)
	report.Summary = Summary{PagesScanned: len(pages), LinksScanned: links, WantedPages: len(report.WantedPages), Stubs: len(report.Stubs), CategoryCounts: map[string]int{
		"dead_links":          len(report.DeadLinks),
		"ambiguous_targets":   len(report.AmbiguousTargets),
		"duplicate_basenames": len(report.DuplicateBasenames),
		"orphans":             len(report.Orphans),
		"unindexed_pages":     len(report.UnindexedPages),
		"missing_frontmatter": len(report.MissingFrontmatter),
		"empty_sections":      len(report.EmptySections),
		"stale_index_entries": len(report.StaleIndexEntries),
		"read_errors":         len(report.ReadErrors),
		"ledger_errors":       len(report.LedgerErrors),
		"task_errors":         len(report.TaskErrors),
		"kind_errors":         len(report.KindErrors),
		"mount_errors":        len(report.MountErrors),
	}}
	for _, n := range report.Summary.CategoryCounts {
		report.Summary.IssuesFound += n
	}
	report.fillEmpty()
	return report, nil
}

// walk lists the files under root. skipTop names the folders at root it leaves out: the
// vault's own walk leaves out kb/ and repos/, which hold other vaults.
func walk(root string, skipTop ...string) ([]string, error) {
	reserved := map[string]bool{}
	for _, name := range skipTop {
		reserved[name] = true
	}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if p == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" {
				return fs.SkipDir
			}
			if reserved[name] && filepath.Dir(p) == root {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

func excluded(rel string, patterns []string) bool {
	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
		if strings.HasSuffix(pattern, "/*") && strings.HasPrefix(rel, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func pathLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

func parsePage(rel, text string) *page {
	text = strings.TrimPrefix(text, "\xef\xbb\xbf")
	pg := &page{path: rel, text: text, headings: map[string]bool{}, blocks: map[string]bool{}}
	fields, body, err := vault.Frontmatter(text)
	pg.body, pg.frontErr = body, err
	pg.hasFront = strings.HasPrefix(text, "---")
	if fields != nil {
		pg.fields = fields
		pg.aliases = vault.StringList(fields, "aliases")
		pg.isMOC = vault.StringField(fields, "type") == "moc"
	} else {
		pg.fields = map[string]any{}
	}
	base := strings.ToLower(path.Base(rel))
	pg.isIndex = base == "index.md" || base == "_index.md" || folderIndexes[rel]
	pg.masked = maskCode(text)
	for _, m := range atxHeading.FindAllStringSubmatch(pg.masked, -1) {
		if h := normalizeHeading(m[2]); h != "" {
			pg.headings[h] = true
		}
	}
	for _, m := range blockID.FindAllStringSubmatch(pg.masked, -1) {
		pg.blocks[strings.ToLower(m[1])] = true
	}
	pg.links = parseLinks(pg)
	return pg
}

// maskCode blanks fenced blocks, inline code, and the frontmatter so links inside them are not graph edges.
func maskCode(text string) string {
	lines := strings.SplitAfter(text, "\n")
	out := make([]string, len(lines))
	inFence, fence := false, ""
	inFront := strings.HasPrefix(text, "---")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		if inFront {
			out[i] = blank(line)
			if i > 0 && trimmed == "---" {
				inFront = false
			}
			continue
		}
		if inFence {
			out[i] = blank(line)
			if m := fenceOpen.FindStringSubmatch(line); m != nil && strings.HasPrefix(m[1], fence[:1]) && len(m[1]) >= len(fence) && strings.TrimSpace(trimmed) == m[1] {
				inFence = false
			}
			continue
		}
		if m := fenceOpen.FindStringSubmatch(line); m != nil {
			inFence, fence = true, m[1]
			out[i] = blank(line)
			continue
		}
		out[i] = inlineCode.ReplaceAllStringFunc(line, blank)
	}
	return htmlComment.ReplaceAllStringFunc(strings.Join(out, ""), blank)
}

func blank(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c != '\n' && c != '\r' {
			b[i] = ' '
		}
	}
	return string(b)
}

func normalizeHeading(h string) string {
	h = strings.TrimSpace(regexp.MustCompile(`[ \t]+#+[ \t]*$`).ReplaceAllString(h, ""))
	return strings.ToLower(strings.Join(strings.Fields(h), " "))
}

func splitFragment(target string) (file, fragment, kind string) {
	escaped := false
	for i, ch := range target {
		if ch == '\\' && !escaped {
			escaped = true
			continue
		}
		if ch == '#' && !escaped {
			frag := strings.TrimSpace(target[i+1:])
			if strings.HasPrefix(frag, "^") {
				return target[:i], frag[1:], "block"
			}
			return target[:i], frag, "heading"
		}
		escaped = false
	}
	return target, "", ""
}

var unescapeRE = regexp.MustCompile(`\\([\\|#\[\]])`)

func wikiTarget(body string) string {
	t := body
	if pipe := strings.Index(body, "|"); pipe >= 0 {
		t = body[:pipe]
		t = strings.TrimSuffix(t, "\\")
	}
	return unescapeRE.ReplaceAllString(strings.TrimSpace(t), "$1")
}

func mdDestination(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "<") && strings.Contains(v, ">") {
		return strings.TrimSpace(v[1:strings.Index(v, ">")])
	}
	if m := regexp.MustCompile(`^(.*?)[ \t]+(?:"[^"]*"|'[^']*'|\([^)]*\))[ \t]*$`).FindStringSubmatch(v); m != nil {
		v = m[1]
	}
	return strings.TrimSpace(v)
}

func lineOf(text string, offset int) int {
	return strings.Count(text[:offset], "\n") + 1
}

func parseLinks(pg *page) []link {
	var links []link
	var occupied [][2]int
	for _, m := range wikiLink.FindAllStringSubmatchIndex(pg.masked, -1) {
		body := pg.masked[m[4]:m[5]]
		raw := wikiTarget(body)
		if raw == "" {
			continue
		}
		file, frag, kind := splitFragment(raw)
		syntax := "wikilink"
		if m[2] >= 0 {
			syntax = "embed"
		}
		links = append(links, link{source: pg.path, line: lineOf(pg.masked, m[0]), target: raw, filePart: file, fragment: frag, fragmentKind: kind, syntax: syntax})
		occupied = append(occupied, [2]int{m[0], m[1]})
	}
	for _, m := range mdLink.FindAllStringSubmatchIndex(pg.masked, -1) {
		inside := false
		for _, o := range occupied {
			if m[0] >= o[0] && m[0] < o[1] {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		dest := mdDestination(pg.masked[m[6]:m[7]])
		if dest == "" || strings.HasPrefix(dest, "//") || uriScheme.MatchString(dest) || strings.HasPrefix(dest, "#") {
			continue
		}
		decoded, err := url.PathUnescape(dest)
		if err != nil {
			decoded = dest
		}
		file, frag, kind := splitFragment(decoded)
		syntax := "markdown-link"
		if m[2] >= 0 {
			syntax = "markdown-embed"
		}
		links = append(links, link{source: pg.path, line: lineOf(pg.masked, m[0]), target: decoded, filePart: file, fragment: frag, fragmentKind: kind, syntax: syntax, mdRelative: true})
	}
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].line != links[j].line {
			return links[i].line < links[j].line
		}
		return pathLess(links[i].target, links[j].target)
	})
	return links
}

// tier holds one set of link targets: the vault's own files, or the files its mounts bring.
type tier struct {
	targets    []target
	byFull     map[string][]target
	byNoSuffix map[string][]target
	byBasename map[string][]target
	byAlias    map[string][]target
}

// resolver resolves a link against the vault's own files first, and the mounts second: a
// page of the project's own wins over a page of the same name in a knowledge base.
type resolver struct {
	own    *tier
	mounts *tier
}

func newResolver(own, mounts []target) *resolver {
	return &resolver{own: newTier(own), mounts: newTier(mounts)}
}

func newTier(targets []target) *tier {
	r := &tier{targets: targets, byFull: map[string][]target{}, byNoSuffix: map[string][]target{}, byBasename: map[string][]target{}, byAlias: map[string][]target{}}
	for _, t := range targets {
		full := strings.ToLower(t.path)
		r.byFull[full] = append(r.byFull[full], t)
		name := strings.ToLower(path.Base(t.path))
		r.byBasename[name] = append(r.byBasename[name], t)
		if ns, ok := t.withoutSuffix(); ok {
			r.byNoSuffix[strings.ToLower(ns)] = append(r.byNoSuffix[strings.ToLower(ns)], t)
			stem := strings.ToLower(path.Base(ns))
			r.byBasename[stem] = append(r.byBasename[stem], t)
		}
		if t.page != nil {
			for _, alias := range t.page.aliases {
				key := strings.ToLower(alias)
				r.byAlias[key] = append(r.byAlias[key], t)
			}
		}
	}
	return r
}

func dedupe(cands []target) []target {
	seen := map[string]target{}
	for _, c := range cands {
		seen[c.path] = c
	}
	out := make([]target, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return pathLess(out[i].path, out[j].path) })
	return out
}

func (r *tier) exact(query string) []target {
	n := strings.TrimLeft(path.Clean(strings.ReplaceAll(query, "\\", "/")), "/")
	if n == "" || n == "." {
		return nil
	}
	key := strings.ToLower(n)
	var cands []target
	cands = append(cands, r.byFull[key]...)
	cands = append(cands, r.byNoSuffix[key]...)
	return dedupe(cands)
}

func (r *resolver) resolve(l link) []target {
	raw, err := url.PathUnescape(strings.TrimSpace(l.filePart))
	if err != nil {
		raw = strings.TrimSpace(l.filePart)
	}
	raw = strings.ReplaceAll(raw, "\\", "/")
	if raw == "" {
		return r.own.exact(l.source)
	}
	raw = strings.TrimLeft(raw, "/")
	sourceDir := path.Dir(l.source)
	var queries []string
	if l.mdRelative || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		queries = append(queries, path.Clean(path.Join(sourceDir, raw)))
	}
	queries = append(queries, path.Clean(raw))
	if !strings.HasPrefix(strings.ToLower(raw), "wiki/") {
		queries = append(queries, path.Clean(path.Join("wiki", raw)))
	}
	if found := r.own.find(queries, raw); len(found) > 0 {
		return found
	}
	return r.mounts.find(queries, raw)
}

// find returns the targets a link names: an exact path, then a bare name against
// basenames and aliases, then a path the target's own ends with.
func (r *tier) find(queries []string, raw string) []target {
	for _, q := range queries {
		if found := r.exact(q); len(found) > 0 {
			return found
		}
	}
	if !strings.Contains(raw, "/") {
		key := strings.ToLower(raw)
		var cands []target
		cands = append(cands, r.byBasename[key]...)
		cands = append(cands, r.byAlias[key]...)
		return dedupe(cands)
	}
	suffix := strings.ToLower(path.Clean(raw))
	var cands []target
	for _, t := range r.targets {
		if strings.HasSuffix(strings.ToLower(t.path), "/"+suffix) {
			cands = append(cands, t)
			continue
		}
		if ns, ok := t.withoutSuffix(); ok && strings.HasSuffix(strings.ToLower(ns), "/"+suffix) {
			cands = append(cands, t)
		}
	}
	return dedupe(cands)
}

func fragmentError(l link, t target) string {
	if l.fragment == "" || t.page == nil {
		return ""
	}
	switch l.fragmentKind {
	case "heading":
		if !t.page.headings[normalizeHeading(l.fragment)] {
			return "heading-not-found"
		}
	case "block":
		if !t.page.blocks[strings.ToLower(l.fragment)] {
			return "block-not-found"
		}
	}
	return ""
}

// wantedTitle returns the page a link names when the link is a placeholder: a bare
// wikilink from an ordinary page whose name is already a valid file name.
func wantedTitle(l link, pg *page) (string, bool) {
	title := strings.TrimSpace(l.filePart)
	if l.syntax != "wikilink" || pg.isIndex || title == "" || strings.Contains(title, "/") || vault.SanitizeTitle(title) != title {
		return "", false
	}
	switch strings.ToLower(path.Ext(title)) {
	case ".md", ".canvas", ".base":
		return "", false
	}
	if pg.path == vault.LogPage || strings.HasPrefix(strings.ToLower(pg.path), "wiki/folds/") {
		return "", false
	}
	return title, true
}

// nearIndex holds every page name and alias, to tell a typo from a new page.
type nearIndex struct {
	names []nearName
}

type nearName struct {
	name   string
	key    []rune
	digits string
}

func newNearIndex(tiers ...[]target) *nearIndex {
	n := &nearIndex{}
	add := func(name string) {
		if key := nameKey(name); len(key) > 0 {
			n.names = append(n.names, nearName{name: name, key: key, digits: digits(key)})
		}
	}
	for _, targets := range tiers {
		for _, t := range targets {
			if strings.EqualFold(path.Ext(t.path), ".md") {
				add(strings.TrimSuffix(path.Base(t.path), path.Ext(t.path)))
			}
			if t.page != nil {
				for _, alias := range t.page.aliases {
					add(alias)
				}
			}
		}
	}
	return n
}

// match returns the closest page name or alias to title, or "" when none is near: equal
// once normalized, or with the same digits and one edit for 5 to 8 letters and digits,
// two for more.
func (n *nearIndex) match(title string) string {
	key := nameKey(title)
	if len(key) == 0 {
		return ""
	}
	limit := 0
	switch {
	case len(key) > 8:
		limit = 2
	case len(key) > 4:
		limit = 1
	}
	best, bestDist := "", limit+1
	want := digits(key)
	for _, c := range n.names {
		if diff := len(c.key) - len(key); diff > limit || -diff > limit || c.digits != want {
			continue
		}
		if d := editDistance(key, c.key); d < bestDist || (d == bestDist && pathLess(c.name, best)) {
			best, bestDist = c.name, d
		}
	}
	return best
}

// nameKey lowercases a name and keeps its letters and digits.
func nameKey(name string) []rune {
	var key []rune
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			key = append(key, r)
		}
	}
	return key
}

func digits(key []rune) string {
	var b strings.Builder
	for _, r := range key {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// editDistance counts the insertions, deletions, substitutions, and swaps of adjacent
// characters that turn a into b.
func editDistance(a, b []rune) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(a)][len(b)]
}

func orphanCandidate(rel string) bool {
	if orphanExcluded[strings.ToLower(path.Base(rel))] || folderIndexes[rel] {
		return false
	}
	inner := strings.ToLower(strings.TrimPrefix(rel, "wiki/"))
	return !strings.HasPrefix(inner, "meta/") && !strings.HasPrefix(inner, "folds/")
}

// stubOf reports whether a page is a stub: a seed page with nothing under its headings, or
// an empty file a page other than the log links to. The log, the hot cache, the overview,
// index pages, task pages, meta pages, and folds are never stubs.
func stubOf(pg *page, incoming map[string]bool) (Stub, bool) {
	if !orphanCandidate(pg.path) || tasks.IsPage(pg.path) || pg.frontErr != nil {
		return Stub{}, false
	}
	from := []string{}
	for src := range incoming {
		if path.Base(src) != "log.md" {
			from = append(from, src)
		}
	}
	sort.Slice(from, func(i, j int) bool { return pathLess(from[i], from[j]) })
	empty := strings.TrimSpace(pg.text) == ""
	switch {
	case empty && len(from) > 0:
	case !empty && vault.StringField(pg.fields, "status") == "seed" && bodyEmpty(pg):
	default:
		return Stub{}, false
	}
	return Stub{Path: pg.path, Empty: empty, LinkedFrom: from}, true
}

// bodyEmpty reports whether a page holds nothing below its frontmatter but headings, block
// ids, comments, and whitespace.
func bodyEmpty(pg *page) bool {
	if pg.frontErr != nil {
		return false
	}
	body := htmlComment.ReplaceAllString(pg.body, "")
	body = atxHeading.ReplaceAllString(body, "")
	body = blockIDLine.ReplaceAllString(body, "")
	return strings.TrimSpace(body) == ""
}

func emptySections(pg *page) []SectionFinding {
	type heading struct {
		start, end, level int
		text              string
	}
	var headings []heading
	for _, m := range atxHeading.FindAllStringSubmatchIndex(pg.masked, -1) {
		text := strings.TrimSpace(regexp.MustCompile(`[ \t]+#+[ \t]*$`).ReplaceAllString(pg.masked[m[4]:m[5]], ""))
		headings = append(headings, heading{start: m[0], end: m[1], level: m[3] - m[2], text: text})
	}
	var findings []SectionFinding
	for i, h := range headings {
		end := len(pg.masked)
		for _, next := range headings[i+1:] {
			if next.level <= h.level {
				end = next.start
				break
			}
		}
		section := []byte(pg.text[h.end:end])
		for _, nested := range headings[i+1:] {
			if nested.start >= end {
				break
			}
			for p := nested.start; p < nested.end && p < end; p++ {
				if c := section[p-h.end]; c != '\n' && c != '\r' {
					section[p-h.end] = ' '
				}
			}
		}
		body := htmlComment.ReplaceAllString(string(section), "")
		body = blockIDLine.ReplaceAllString(body, "")
		if strings.TrimSpace(body) != "" {
			continue
		}
		findings = append(findings, SectionFinding{Path: pg.path, Line: lineOf(pg.masked, h.start), Heading: h.text})
	}
	return findings
}

func ledgerErrors(root string, overlay map[string][]byte, present map[string]bool, asOf time.Time) []PathFinding {
	data, ok := overlay[vault.LedgerPath]
	if !ok {
		var err error
		data, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(vault.LedgerPath)))
		if err != nil {
			return nil
		}
	}
	l, err := ledger.Parse(data)
	if err != nil {
		return []PathFinding{{Path: vault.LedgerPath, Message: err.Error()}}
	}
	var out []PathFinding
	ids := make([]string, 0, len(l.Sources))
	for id := range l.Sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := l.Sources[id]
		if s.Origin.Kind == "file" && s.ReviewStatus == "active" {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(s.Origin.Locator))); err != nil {
				out = append(out, PathFinding{Path: vault.LedgerPath, Message: fmt.Sprintf("%s: captured file is missing: %s", id, s.Origin.Locator)})
			}
		}
		for _, p := range s.Pages {
			if !present[p] {
				out = append(out, PathFinding{Path: vault.LedgerPath, Message: fmt.Sprintf("%s: linked page does not exist: %s", id, p)})
			}
		}
	}
	return out
}

func sortFindings(r *Report) {
	sort.SliceStable(r.DeadLinks, func(i, j int) bool { return linkLess(r.DeadLinks[i], r.DeadLinks[j]) })
	sort.SliceStable(r.StaleIndexEntries, func(i, j int) bool { return linkLess(r.StaleIndexEntries[i], r.StaleIndexEntries[j]) })
	sort.SliceStable(r.AmbiguousTargets, func(i, j int) bool {
		a, b := r.AmbiguousTargets[i], r.AmbiguousTargets[j]
		if a.Source != b.Source {
			return pathLess(a.Source, b.Source)
		}
		return a.Line < b.Line
	})
	sort.SliceStable(r.DuplicateBasenames, func(i, j int) bool {
		return pathLess(r.DuplicateBasenames[i].Basename, r.DuplicateBasenames[j].Basename)
	})
	sort.SliceStable(r.Orphans, func(i, j int) bool { return pathLess(r.Orphans[i].Path, r.Orphans[j].Path) })
	sort.SliceStable(r.UnindexedPages, func(i, j int) bool { return pathLess(r.UnindexedPages[i].Path, r.UnindexedPages[j].Path) })
	sort.SliceStable(r.MissingFrontmatter, func(i, j int) bool { return pathLess(r.MissingFrontmatter[i].Path, r.MissingFrontmatter[j].Path) })
	sort.SliceStable(r.EmptySections, func(i, j int) bool {
		a, b := r.EmptySections[i], r.EmptySections[j]
		if a.Path != b.Path {
			return pathLess(a.Path, b.Path)
		}
		return a.Line < b.Line
	})
	sort.SliceStable(r.ReadErrors, func(i, j int) bool { return pathLess(r.ReadErrors[i].Path, r.ReadErrors[j].Path) })
	sort.SliceStable(r.KindErrors, func(i, j int) bool { return pathLess(r.KindErrors[i].Path, r.KindErrors[j].Path) })
	sort.SliceStable(r.MountErrors, func(i, j int) bool { return pathLess(r.MountErrors[i].Path, r.MountErrors[j].Path) })
	sort.SliceStable(r.WantedPages, func(i, j int) bool { return pathLess(r.WantedPages[i].Title, r.WantedPages[j].Title) })
	sort.SliceStable(r.Stubs, func(i, j int) bool { return pathLess(r.Stubs[i].Path, r.Stubs[j].Path) })
}

func linkLess(a, b LinkFinding) bool {
	if a.Source != b.Source {
		return pathLess(a.Source, b.Source)
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return pathLess(a.Target, b.Target)
}

// fillEmpty turns nil slices into empty ones so the JSON reads as lists, not null.
func (r *Report) fillEmpty() {
	if r.DeadLinks == nil {
		r.DeadLinks = []LinkFinding{}
	}
	if r.AmbiguousTargets == nil {
		r.AmbiguousTargets = []Ambiguous{}
	}
	if r.DuplicateBasenames == nil {
		r.DuplicateBasenames = []Duplicate{}
	}
	if r.Orphans == nil {
		r.Orphans = []PathFinding{}
	}
	if r.UnindexedPages == nil {
		r.UnindexedPages = []PathFinding{}
	}
	if r.MissingFrontmatter == nil {
		r.MissingFrontmatter = []FrontmatterFinding{}
	}
	if r.EmptySections == nil {
		r.EmptySections = []SectionFinding{}
	}
	if r.StaleIndexEntries == nil {
		r.StaleIndexEntries = []LinkFinding{}
	}
	if r.ReadErrors == nil {
		r.ReadErrors = []PathFinding{}
	}
	if r.LedgerErrors == nil {
		r.LedgerErrors = []PathFinding{}
	}
	if r.TaskErrors == nil {
		r.TaskErrors = []PathFinding{}
	}
	if r.KindErrors == nil {
		r.KindErrors = []PathFinding{}
	}
	if r.MountErrors == nil {
		r.MountErrors = []PathFinding{}
	}
	if r.WantedPages == nil {
		r.WantedPages = []WantedPage{}
	}
	if r.Stubs == nil {
		r.Stubs = []Stub{}
	}
}

// JSON renders the report.
func (r *Report) JSON() []byte {
	data, _ := json.MarshalIndent(r, "", "  ")
	return append(data, '\n')
}

// Markdown renders the report for a person.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Wiki lint\n\n%d pages, %d links, %d findings (as of %s).\n", r.Summary.PagesScanned, r.Summary.LinksScanned, r.Summary.IssuesFound, r.AsOf)
	section := func(title string, n int) {
		fmt.Fprintf(&b, "\n## %s (%d)\n\n", title, n)
		if n == 0 {
			b.WriteString("None.\n")
		}
	}
	section("Dead links", len(r.DeadLinks))
	for _, f := range r.DeadLinks {
		fmt.Fprintf(&b, "- `%s:%d` → `%s` (%s%s)\n", f.Source, f.Line, f.Target, f.Reason, suggestionHint(f))
	}
	section("Ambiguous targets", len(r.AmbiguousTargets))
	for _, f := range r.AmbiguousTargets {
		fmt.Fprintf(&b, "- `%s:%d` → `%s`: %s\n", f.Source, f.Line, f.Target, strings.Join(f.Candidates, ", "))
	}
	section("Duplicate basenames", len(r.DuplicateBasenames))
	for _, f := range r.DuplicateBasenames {
		fmt.Fprintf(&b, "- `%s`: %s\n", f.Basename, strings.Join(f.Paths, ", "))
	}
	section("Orphans", len(r.Orphans))
	for _, f := range r.Orphans {
		fmt.Fprintf(&b, "- `%s`\n", f.Path)
	}
	section("Pages missing from every index or MOC", len(r.UnindexedPages))
	for _, f := range r.UnindexedPages {
		fmt.Fprintf(&b, "- `%s`\n", f.Path)
	}
	section("Missing frontmatter", len(r.MissingFrontmatter))
	for _, f := range r.MissingFrontmatter {
		fmt.Fprintf(&b, "- `%s`: %s\n", f.Path, strings.Join(f.MissingFields, ", "))
	}
	section("Empty sections", len(r.EmptySections))
	for _, f := range r.EmptySections {
		fmt.Fprintf(&b, "- `%s:%d` %s\n", f.Path, f.Line, f.Heading)
	}
	section("Stale index entries", len(r.StaleIndexEntries))
	for _, f := range r.StaleIndexEntries {
		fmt.Fprintf(&b, "- `%s:%d` → `%s` (%s)\n", f.Source, f.Line, f.Target, f.Reason)
	}
	section("Read errors", len(r.ReadErrors))
	for _, f := range r.ReadErrors {
		fmt.Fprintf(&b, "- `%s`: %s\n", f.Path, f.Message)
	}
	section("Tasks", len(r.TaskErrors))
	for _, f := range r.TaskErrors {
		fmt.Fprintf(&b, "- `%s`: %s\n", f.Path, f.Message)
	}
	section("Ledger", len(r.LedgerErrors))
	for _, f := range r.LedgerErrors {
		fmt.Fprintf(&b, "- %s\n", f.Message)
	}
	section("Kind", len(r.KindErrors))
	for _, f := range r.KindErrors {
		fmt.Fprintf(&b, "- `%s`: %s\n", f.Path, f.Message)
	}
	section("Mounts", len(r.MountErrors))
	for _, f := range r.MountErrors {
		fmt.Fprintf(&b, "- `%s`: %s\n", f.Path, f.Message)
	}
	section("Wanted pages", len(r.WantedPages))
	if len(r.WantedPages) > 0 {
		b.WriteString("Pages the wiki links to that nobody has written yet. Not findings; the stub tool creates them.\n\n")
	}
	for _, w := range r.WantedPages {
		var refs []string
		for _, l := range w.Links {
			refs = append(refs, fmt.Sprintf("`%s:%d`", l.Source, l.Line))
		}
		fmt.Fprintf(&b, "- %s ← %s\n", w.Title, strings.Join(refs, ", "))
	}
	section("Stubs to fill", len(r.Stubs))
	if len(r.Stubs) > 0 {
		b.WriteString("Pages that hold nothing yet. Not findings.\n\n")
	}
	for _, s := range r.Stubs {
		fmt.Fprintf(&b, "- `%s`", s.Path)
		if s.Empty {
			b.WriteString(" (empty file)")
		}
		if len(s.LinkedFrom) > 0 {
			fmt.Fprintf(&b, " ← %s", strings.Join(s.LinkedFrom, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func suggestionHint(f LinkFinding) string {
	if f.Suggestion == "" {
		return ""
	}
	return fmt.Sprintf("; did you mean %q?", f.Suggestion)
}

// Problems lists every finding whose source or path is one of the given files, as short
// messages. Plans use it to warn about the pages they are about to write.
func (r *Report) Problems(paths []string) []string {
	set := map[string]bool{}
	for _, p := range paths {
		set[p] = true
	}
	var out []string
	for _, f := range r.DeadLinks {
		if set[f.Source] {
			out = append(out, fmt.Sprintf("%s:%d links to %q, which does not resolve (%s%s)", f.Source, f.Line, f.Target, f.Reason, suggestionHint(f)))
		}
	}
	for _, w := range r.WantedPages {
		for _, l := range w.Links {
			if set[l.Source] {
				out = append(out, fmt.Sprintf("%s:%d links to %q, which has no page yet", l.Source, l.Line, w.Title))
			}
		}
	}
	for _, f := range r.AmbiguousTargets {
		if set[f.Source] {
			out = append(out, fmt.Sprintf("%s:%d links to %q, which matches %s", f.Source, f.Line, f.Target, strings.Join(f.Candidates, " and ")))
		}
	}
	for _, f := range r.EmptySections {
		if set[f.Path] {
			out = append(out, fmt.Sprintf("%s:%d section %q is empty", f.Path, f.Line, f.Heading))
		}
	}
	for _, f := range r.UnindexedPages {
		if set[f.Path] {
			out = append(out, fmt.Sprintf("%s is not linked from any index or MOC", f.Path))
		}
	}
	return out
}
