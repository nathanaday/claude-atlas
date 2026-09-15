package txn

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// StubTitle names a page to stub and, optionally, its type and the knowledge base it
// lands in.
type StubTitle struct {
	Title  string `json:"title" jsonschema:"the page's title as the link writes it"`
	Type   string `json:"type,omitempty" jsonschema:"concept, entity, question, or session; note or moc in lyt mode"`
	Target string `json:"target,omitempty" jsonschema:"a mount name; the stub lands in that knowledge base"`
}

// Stubbed says where a stub went.
type Stubbed struct {
	Title string `json:"title"`
	Type  string `json:"type"`
	Path  string `json:"path"`
}

// StubResult reports a stub operation; OperationID is empty when nothing was wanted.
type StubResult struct {
	Stubs       []Stubbed `json:"stubs"`
	OperationID string    `json:"operation_id,omitempty"`
	Commit      string    `json:"commit,omitempty"`
}

// StubPages creates seed pages for the pages the wiki links to but nobody has written, and
// for the empty pages a link points to, and commits them as one operation. With no titles
// it stubs all of them.
func StubPages(v *vault.Vault, titles []StubTitle, defaultType string, now time.Time) (StubResult, error) {
	return stubOperation(v, v, titles, defaultType, "", now)
}

// stubOperation applies one stub operation: the request from stubRequest, committed in
// dest. via names the project the session came through, and ends the summary.
func stubOperation(source, dest *vault.Vault, titles []StubTitle, defaultType, via string, now time.Time) (StubResult, error) {
	req, stubbed, err := stubRequest(source, dest, titles, defaultType, now)
	if err != nil {
		return StubResult{}, err
	}
	out := StubResult{Stubs: []Stubbed{}}
	if len(req.Writes) == 0 {
		return out, nil
	}
	if via != "" {
		req.Summary += " (via " + via + ")"
	}
	plan, err := Prepare(dest, req, now)
	if err != nil {
		return StubResult{}, err
	}
	res, err := Apply(dest, plan, now)
	if err != nil {
		return StubResult{}, err
	}
	out.Stubs, out.OperationID, out.Commit = stubbed, res.OperationID, res.Commit
	return out, nil
}

// candidate is a title a vault can stub. empty is the path of the empty page a link
// points to, and "" for a page nobody has written.
type candidate struct {
	title string
	empty string
}

// stubSet is what one lint report offers to stub: the candidates by lowercased title, in
// the report's order, and the empty pages whose file name cannot come from a title.
type stubSet struct {
	report     *lint.Report
	candidates map[string]candidate
	unnameable map[string]string
	order      []string
}

// stubCandidates reads a vault's lint report. withEmpty adds the empty pages a link
// points to; a stub that lands in another vault leaves them out, because moving a file
// between two vaults is not one operation.
func stubCandidates(v *vault.Vault, withEmpty bool, now time.Time) (*stubSet, error) {
	report, err := lint.Run(v.Root, lint.Options{AsOf: now})
	if err != nil {
		return nil, err
	}
	set := &stubSet{report: report, candidates: map[string]candidate{}, unnameable: map[string]string{}}
	for _, w := range report.WantedPages {
		key := strings.ToLower(w.Title)
		set.candidates[key] = candidate{title: w.Title}
		set.order = append(set.order, key)
	}
	if !withEmpty {
		return set, nil
	}
	for _, s := range report.Stubs {
		if !s.Empty {
			continue
		}
		title := vault.PageTitle(s.Path)
		if vault.SanitizeTitle(title) != title {
			set.unnameable[strings.ToLower(title)] = s.Path
			continue
		}
		key := strings.ToLower(title)
		if _, dup := set.candidates[key]; !dup {
			set.order = append(set.order, key)
		}
		set.candidates[key] = candidate{title: title, empty: s.Path}
	}
	return set, nil
}

// all lists every candidate as a title, in the report's order.
func (set *stubSet) all() []StubTitle {
	var titles []StubTitle
	for _, key := range set.order {
		titles = append(titles, StubTitle{Title: set.candidates[key].title})
	}
	return titles
}

// find matches a title without regard to case. It says why a title cannot be stubbed.
func (set *stubSet) find(v *vault.Vault, title string) (candidate, error) {
	key := strings.ToLower(strings.TrimSpace(title))
	if c, ok := set.candidates[key]; ok {
		return c, nil
	}
	if p, unnamed := set.unnameable[key]; unnamed {
		return candidate{}, fmt.Errorf("%s cannot be a page's file name; rename the link so its text is a name a file system accepts, then stub it", p)
	}
	return candidate{}, notWanted(v, set.report, strings.TrimSpace(title))
}

// StubRequest builds the request StubPages applies. A title must name a wanted page or an
// empty page a link points to; the type defaults to defaultType, then to the mode's.
func StubRequest(v *vault.Vault, titles []StubTitle, defaultType string, now time.Time) (Request, []Stubbed, error) {
	return stubRequest(v, v, titles, defaultType, now)
}

// stubRequest builds one stub operation's request: the candidates come from source's lint
// report and the pages are routed in dest. A stub that stays in one vault also files the
// empty pages a link points to, and with no titles it stubs every candidate; a stub that
// crosses vaults names its titles.
func stubRequest(source, dest *vault.Vault, titles []StubTitle, defaultType string, now time.Time) (Request, []Stubbed, error) {
	home := source == dest
	set, err := stubCandidates(source, home, now)
	if err != nil {
		return Request{}, nil, err
	}
	if len(titles) == 0 {
		if !home {
			return Request{}, nil, fmt.Errorf("name the titles to stub in %s", dest.Name())
		}
		titles = set.all()
	}
	if defaultType == "" {
		defaultType = defaultStubType(dest.Config.Mode)
	}
	writes, stubbed, err := set.writes(source, dest, titles, defaultType, now)
	if err != nil {
		return Request{}, nil, err
	}
	req := Request{Kind: Stub, Writes: writes}
	if len(stubbed) > 0 {
		req.Summary = stubSummary(stubbed)
	}
	return req, stubbed, nil
}

// writes turns titles into one operation's writes: a seed page for each, routed in dest.
// dest is the source vault itself for a stub that stays home, and then an empty page a
// link points to is replaced where it lies or moved to its routed path. A stub that lands
// in another vault leaves such a page alone, because the set holds none.
func (set *stubSet) writes(source, dest *vault.Vault, titles []StubTitle, defaultType string, now time.Time) ([]Write, []Stubbed, error) {
	var writes []Write
	var stubbed []Stubbed
	seen := map[string]bool{}
	for _, t := range titles {
		key := strings.ToLower(strings.TrimSpace(t.Title))
		c, err := set.find(source, t.Title)
		if err != nil {
			return nil, nil, err
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		pageType := t.Type
		if pageType == "" {
			pageType = defaultType
		}
		if pageType == "source" {
			return nil, nil, fmt.Errorf("a source page comes from ingest, with a captured file and a ledger record; stub %q as another type", c.title)
		}
		route, err := dest.RouteFor(pageType, c.title, now)
		if err != nil {
			return nil, nil, err
		}
		content := []byte(vault.Skeleton(pageType, c.title, now))
		switch {
		case c.empty == route.Path:
			writes = append(writes, Write{Path: route.Path, Mode: Replace, Content: content})
		case route.Exists && dest != source:
			return nil, nil, fmt.Errorf("%s already exists in %s; link to it instead", route.Path, dest.Name())
		case route.Exists:
			return nil, nil, fmt.Errorf("%s already exists; link to it instead", route.Path)
		default:
			if c.empty != "" {
				writes = append(writes, Write{Path: c.empty, Mode: Delete})
			}
			writes = append(writes, Write{Path: route.Path, Mode: Create, Content: content})
		}
		stubbed = append(stubbed, Stubbed{Title: c.title, Type: pageType, Path: route.Path})
	}
	return writes, stubbed, nil
}

// StubInto creates, in the knowledge base dest, seed pages for titles source's wiki links
// to but nobody has written, so the links resolve through the project's mount of dest.
// via names the project in the operation's summary. One operation, in dest. Pass the same
// knowledge base as source and dest for its own wanted pages, which is what a project
// session stubs when it names the knowledge base as the vault.
func StubInto(source, dest *vault.Vault, titles []StubTitle, defaultType string, via string, now time.Time) (StubResult, error) {
	if dest.Config.Kind != vault.Knowledge {
		return StubResult{}, fmt.Errorf("%s is not a knowledge base", dest.Name())
	}
	return stubOperation(source, dest, titles, defaultType, via, now)
}

func defaultStubType(mode vault.Mode) string {
	if mode == vault.LYT {
		return "note"
	}
	return "concept"
}

// notWanted says why a title cannot be stubbed.
func notWanted(v *vault.Vault, report *lint.Report, title string) error {
	key := strings.ToLower(title)
	for _, d := range report.DeadLinks {
		file, _, _ := strings.Cut(d.Target, "#")
		if d.Suggestion != "" && strings.ToLower(strings.TrimSpace(file)) == key {
			return fmt.Errorf("%q nearly matches the page %q; fix the link if it means that page, or rename the link so it no longer resembles it", title, d.Suggestion)
		}
	}
	if p := pageNamed(v, title); p != "" {
		return fmt.Errorf("%q already has a page: %s", title, p)
	}
	return fmt.Errorf("nothing in the wiki links to %q; link to it from a page first, or write the page with save", title)
}

// pageNamed returns the wiki page whose file name is title, compared without case.
func pageNamed(v *vault.Vault, title string) string {
	found := ""
	filepath.WalkDir(v.Path(vault.WikiDir), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		ext := filepath.Ext(d.Name())
		if strings.EqualFold(ext, ".md") && strings.EqualFold(strings.TrimSuffix(d.Name(), ext), title) {
			rel, _ := filepath.Rel(v.Root, p)
			found = filepath.ToSlash(rel)
		}
		return nil
	})
	return found
}

func stubSummary(stubbed []Stubbed) string {
	if len(stubbed) == 1 {
		return "stub " + stubbed[0].Title
	}
	var names []string
	for i, s := range stubbed {
		if i == 3 {
			names = append(names, fmt.Sprintf("and %d more", len(stubbed)-3))
			break
		}
		names = append(names, s.Title)
	}
	return fmt.Sprintf("stub %d pages: %s", len(stubbed), strings.Join(names, ", "))
}
