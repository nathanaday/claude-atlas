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

// StubTitle names a page to stub and, optionally, its type.
type StubTitle struct {
	Title string `json:"title" jsonschema:"the page's title as the link writes it"`
	Type  string `json:"type,omitempty" jsonschema:"concept, entity, question, or session; note or moc in lyt mode"`
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
	req, stubbed, err := StubRequest(v, titles, defaultType, now)
	if err != nil {
		return StubResult{}, err
	}
	out := StubResult{Stubs: []Stubbed{}}
	if len(req.Writes) == 0 {
		return out, nil
	}
	plan, err := Prepare(v, req, now)
	if err != nil {
		return StubResult{}, err
	}
	res, err := Apply(v, plan, now)
	if err != nil {
		return StubResult{}, err
	}
	out.Stubs, out.OperationID, out.Commit = stubbed, res.OperationID, res.Commit
	return out, nil
}

// StubRequest builds the request StubPages applies. A title must name a wanted page or an
// empty page a link points to; the type defaults to defaultType, then to the mode's.
func StubRequest(v *vault.Vault, titles []StubTitle, defaultType string, now time.Time) (Request, []Stubbed, error) {
	report, err := lint.Run(v.Root, lint.Options{AsOf: now})
	if err != nil {
		return Request{}, nil, err
	}
	type candidate struct {
		title string
		empty string // the empty page's path; "" for a wanted page
	}
	candidates := map[string]candidate{}
	var order []string
	for _, w := range report.WantedPages {
		key := strings.ToLower(w.Title)
		candidates[key] = candidate{title: w.Title}
		order = append(order, key)
	}
	for _, s := range report.Stubs {
		if !s.Empty {
			continue
		}
		title := vault.PageTitle(s.Path)
		key := strings.ToLower(title)
		if _, dup := candidates[key]; !dup {
			order = append(order, key)
		}
		candidates[key] = candidate{title: title, empty: s.Path}
	}
	if len(titles) == 0 {
		for _, key := range order {
			titles = append(titles, StubTitle{Title: candidates[key].title})
		}
	}
	if defaultType == "" {
		defaultType = defaultStubType(v.Config.Mode)
	}
	req := Request{Kind: Stub}
	var stubbed []Stubbed
	seen := map[string]bool{}
	for _, t := range titles {
		key := strings.ToLower(strings.TrimSpace(t.Title))
		c, ok := candidates[key]
		if !ok {
			return Request{}, nil, notWanted(v, report, strings.TrimSpace(t.Title))
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
			return Request{}, nil, fmt.Errorf("a source page comes from ingest, with a captured file and a ledger record; stub %q as another type", c.title)
		}
		route, err := v.RouteFor(pageType, c.title, now)
		if err != nil {
			return Request{}, nil, err
		}
		content := []byte(vault.Skeleton(pageType, c.title, now))
		switch {
		case c.empty == route.Path:
			req.Writes = append(req.Writes, Write{Path: route.Path, Mode: Replace, Content: content})
		case route.Exists:
			return Request{}, nil, fmt.Errorf("%s already exists; link to it instead", route.Path)
		default:
			if c.empty != "" {
				req.Writes = append(req.Writes, Write{Path: c.empty, Mode: Delete})
			}
			req.Writes = append(req.Writes, Write{Path: route.Path, Mode: Create, Content: content})
		}
		stubbed = append(stubbed, Stubbed{Title: c.title, Type: pageType, Path: route.Path})
	}
	if len(stubbed) > 0 {
		req.Summary = stubSummary(stubbed)
	}
	return req, stubbed, nil
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
