package txn

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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

// Skipped is a candidate a stub with no titles passed over, and why. A title the user
// names is refused instead.
type Skipped struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// StubResult reports a stub operation; OperationID is empty when nothing was wanted.
type StubResult struct {
	Stubs       []Stubbed `json:"stubs"`
	Skipped     []Skipped `json:"skipped,omitempty"`
	OperationID string    `json:"operation_id,omitempty"`
	Commit      string    `json:"commit,omitempty"`
}

// StubPages creates seed pages for the pages the wiki links to but nobody has written, and
// for the empty pages a link points to, and commits them as one operation. With no titles
// it stubs all of them. mounts is what v mounts, for the lint run that says what is wanted;
// nil reads the symlinks under kb/.
func StubPages(v *vault.Vault, titles []StubTitle, defaultType string, mounts map[string]string, now time.Time) (StubResult, error) {
	return stubOperation(v, v, titles, defaultType, "", mounts, now)
}

// stubOperation applies one stub operation: the request from stubRequest, committed in
// dest. via names the project the session came through, and ends the summary.
func stubOperation(source, dest *vault.Vault, titles []StubTitle, defaultType, via string, mounts map[string]string, now time.Time) (StubResult, error) {
	req, stubbed, skipped, err := stubRequest(source, dest, titles, defaultType, mounts, now)
	if err != nil {
		return StubResult{}, err
	}
	out := StubResult{Stubs: []Stubbed{}, Skipped: skipped}
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
// points to, and "" for a page nobody has written; hash is that page's content as the
// stub read it, so a plan made later sees the user's typing as a conflict.
type candidate struct {
	title string
	empty string
	hash  string
}

// stubSet is what one lint report offers to stub: the candidates by lowercased title, in
// the report's order, the empty pages whose file name cannot come from a title, and the
// titles the set refuses, each with its reason.
type stubSet struct {
	report     *lint.Report
	candidates map[string]candidate
	unnameable map[string]string
	conflicts  map[string]string
	order      []string
}

// stubCandidates reads a vault's lint report. withEmpty adds the empty pages a link
// points to; a stub that lands in another vault leaves them out, because moving a file
// between two vaults is not one operation.
func stubCandidates(v *vault.Vault, withEmpty bool, mounts map[string]string, now time.Time) (*stubSet, error) {
	report, err := lint.Run(v.Root, lint.Options{AsOf: now, Mounts: mounts})
	if err != nil {
		return nil, err
	}
	set := &stubSet{report: report, candidates: map[string]candidate{}, unnameable: map[string]string{}, conflicts: map[string]string{}}
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
		key := strings.ToLower(title)
		if vault.SanitizeTitle(title) != title {
			set.unnameable[key] = title
			continue
		}
		if _, taken := set.conflicts[key]; taken {
			continue
		}
		prior, dup := set.candidates[key]
		if dup && prior.empty != "" {
			set.conflicts[key] = fmt.Sprintf("two empty pages are named %s: %s, %s; keep one", title, prior.empty, s.Path)
			continue
		}
		data, err := os.ReadFile(v.Path(s.Path))
		if err != nil {
			return nil, err
		}
		if !dup {
			set.order = append(set.order, key)
		}
		set.candidates[key] = candidate{title: title, empty: s.Path, hash: sha(data)}
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
	if reason, taken := set.conflicts[key]; taken {
		return candidate{}, errors.New(reason)
	}
	if c, ok := set.candidates[key]; ok {
		return c, nil
	}
	if text, unnamed := set.unnameable[key]; unnamed {
		return candidate{}, fmt.Errorf("the link text %q cannot be a file name; rename the link, then stub it", text)
	}
	return candidate{}, notWanted(v, set.report, strings.TrimSpace(title))
}

// StubRequest builds the request StubPages applies. A title must name a wanted page or an
// empty page a link points to; the type defaults to defaultType, then to the mode's.
func StubRequest(v *vault.Vault, titles []StubTitle, defaultType string, mounts map[string]string, now time.Time) (Request, []Stubbed, []Skipped, error) {
	return stubRequest(v, v, titles, defaultType, mounts, now)
}

// stubRequest builds one stub operation's request: the candidates come from source's lint
// report and the pages are routed in dest. A stub that stays in one vault also files the
// empty pages a link points to, and with no titles it stubs every candidate it can and
// reports the rest; a stub that crosses vaults names its titles.
func stubRequest(source, dest *vault.Vault, titles []StubTitle, defaultType string, mounts map[string]string, now time.Time) (Request, []Stubbed, []Skipped, error) {
	home := source.Root == dest.Root
	set, err := stubCandidates(source, home, mounts, now)
	if err != nil {
		return Request{}, nil, nil, err
	}
	everything := len(titles) == 0
	if everything {
		if !home {
			return Request{}, nil, nil, fmt.Errorf("name the titles to stub in %s", dest.Name())
		}
		titles = set.all()
	}
	if defaultType == "" {
		defaultType = defaultStubType(dest.Config.Mode)
	}
	writes, stubbed, skipped, err := set.writes(source, dest, titles, defaultType, everything, now)
	if err != nil {
		return Request{}, nil, nil, err
	}
	req := Request{Kind: Stub, Writes: writes}
	if home {
		req.Mounts = mounts
	}
	if len(stubbed) > 0 {
		req.Summary = stubSummary(stubbed)
	}
	return req, stubbed, skipped, nil
}

// writes turns titles into one operation's writes: a seed page for each, routed in dest.
// dest is the source vault itself for a stub that stays home, and then an empty page a
// link points to is replaced where it lies or moved to its routed path. A stub that lands
// in another vault leaves such a page alone, because the set holds none. With everything
// set the titles are every candidate, and one the vault cannot file is skipped rather
// than refused, so the others still land.
func (set *stubSet) writes(source, dest *vault.Vault, titles []StubTitle, defaultType string, everything bool, now time.Time) ([]Write, []Stubbed, []Skipped, error) {
	var writes []Write
	var stubbed []Stubbed
	var skipped []Skipped
	seen := map[string]string{}
	for _, t := range titles {
		title := strings.TrimSpace(t.Title)
		key := strings.ToLower(title)
		c, err := set.find(source, title)
		if err != nil {
			if !everything {
				return nil, nil, nil, err
			}
			skipped = append(skipped, Skipped{Title: title, Reason: err.Error()})
			continue
		}
		pageType := t.Type
		if pageType == "" {
			pageType = defaultType
		}
		if prior, dup := seen[key]; dup {
			if prior != pageType {
				return nil, nil, nil, fmt.Errorf("%s is given twice with different types: %s and %s", c.title, prior, pageType)
			}
			continue
		}
		seen[key] = pageType
		if pageType == "source" {
			return nil, nil, nil, fmt.Errorf("a source page comes from ingest, with a captured file and a ledger record; stub %q as another type", c.title)
		}
		route, err := dest.RouteFor(pageType, c.title, now)
		if err == nil {
			err = routeTaken(source, dest, route, c)
		}
		if err != nil {
			if !everything {
				return nil, nil, nil, err
			}
			skipped = append(skipped, Skipped{Title: c.title, Reason: err.Error()})
			continue
		}
		content := []byte(route.Skeleton)
		if c.empty == route.Path {
			writes = append(writes, Write{Path: route.Path, Mode: Replace, Content: content, BaseSHA256: c.hash})
		} else {
			if c.empty != "" {
				writes = append(writes, Write{Path: c.empty, Mode: Delete, BaseSHA256: c.hash})
			}
			writes = append(writes, Write{Path: route.Path, Mode: Create, Content: content})
		}
		stubbed = append(stubbed, Stubbed{Title: c.title, Type: pageType, Path: route.Path})
	}
	return writes, stubbed, skipped, nil
}

// routeTaken refuses a stub whose routed path holds a page already, unless that page is
// the empty one the link points to, which the stub replaces.
func routeTaken(source, dest *vault.Vault, route *vault.Route, c candidate) error {
	if !route.Exists || c.empty == route.Path {
		return nil
	}
	if dest.Root != source.Root {
		return fmt.Errorf("%s already exists in %s; link to it instead", route.Path, dest.Name())
	}
	return fmt.Errorf("%s already exists; link to it instead", route.Path)
}

// StubInto creates, in the knowledge base dest, seed pages for titles source's wiki links
// to but nobody has written, so the links resolve through the project's mount of dest.
// via names the project in the operation's summary. One operation, in dest. Pass the same
// knowledge base as source and dest for its own wanted pages, which is what a project
// session stubs when it names the knowledge base as the vault. mounts is what source
// mounts, as in StubPages.
func StubInto(source, dest *vault.Vault, titles []StubTitle, defaultType string, via string, mounts map[string]string, now time.Time) (StubResult, error) {
	if dest.Config.Kind != vault.Knowledge {
		return StubResult{}, fmt.Errorf("%s is not a knowledge base", dest.Name())
	}
	return stubOperation(source, dest, titles, defaultType, via, mounts, now)
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
	p, err := pageNamed(v, title)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", vault.WikiDir, err)
	}
	if p != "" {
		return fmt.Errorf("%q already has a page: %s", title, p)
	}
	return fmt.Errorf("nothing in the wiki links to %q; link to it from a page first, or write the page with save", title)
}

// pageNamed returns the wiki page whose file name is title, compared without case.
func pageNamed(v *vault.Vault, title string) (string, error) {
	found := ""
	err := filepath.WalkDir(v.Path(vault.WikiDir), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(d.Name())
		if strings.EqualFold(ext, ".md") && strings.EqualFold(strings.TrimSuffix(d.Name(), ext), title) {
			rel, _ := filepath.Rel(v.Root, p)
			found = filepath.ToSlash(rel)
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return found, nil
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
