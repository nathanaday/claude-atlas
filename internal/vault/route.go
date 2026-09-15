package vault

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Route is where a new page belongs and what it starts as.
type Route struct {
	Path     string `json:"path"`
	Type     string `json:"type"`
	Mode     Mode   `json:"mode"`
	Skeleton string `json:"skeleton"`
	// Exists is set when a page already sits at Path.
	Exists bool `json:"exists"`
}

var genericFolders = map[string]string{
	"source":   "wiki/sources",
	"entity":   "wiki/entities",
	"concept":  "wiki/concepts",
	"question": QuestionsDir,
	"session":  SessionsDir,
}

// RoutableTypes lists the page types a vault of a kind files in a mode. A knowledge base
// holds sources, entities, and concepts; a project adds questions and sessions.
func RoutableTypes(kind Kind, mode Mode) []string {
	types := []string{"source", "entity", "concept"}
	if kind == Project {
		types = append(types, "question", "session")
	}
	if mode == LYT {
		types = append([]string{"note", "moc"}, types...)
	}
	return types
}

// Noun is the kind as a person says it.
func (k Kind) Noun() string {
	if k == Knowledge {
		return "knowledge base"
	}
	return string(k)
}

var (
	unsafeChars = regexp.MustCompile(`[/\\:*?"<>|]+`)
	spaces      = regexp.MustCompile(`\s+`)
)

// SanitizeTitle turns a title into a file stem Obsidian and every desktop filesystem accept.
func SanitizeTitle(title string) string {
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title)
	s = unsafeChars.ReplaceAllString(s, " ")
	s = strings.TrimSpace(spaces.ReplaceAllString(s, " "))
	s = strings.Trim(s, ". ")
	for len(s) > 200 {
		_, size := lastRune(s)
		s = strings.TrimSpace(s[:len(s)-size])
	}
	if s == "" {
		return "Untitled"
	}
	return s
}

func lastRune(s string) (rune, int) {
	for i := len(s) - 1; i >= 0; i-- {
		if r := rune(s[i]); r < 0x80 || (s[i]&0xC0) != 0x80 {
			return r, len(s) - i
		}
	}
	return 0, len(s)
}

// RouteFor says where a page of pageType titled title goes in this vault, and returns a
// skeleton with the vault's frontmatter conventions. It reads nothing but the target path.
func (v *Vault) RouteFor(pageType, title string, now time.Time) (*Route, error) {
	mode := v.Config.Mode
	folder, err := folderFor(v.Config.Kind, mode, pageType)
	if err != nil {
		return nil, err
	}
	stem := SanitizeTitle(title)
	route := &Route{Path: folder + "/" + stem + ".md", Type: pageType, Mode: mode}
	if _, err := os.Stat(v.Path(route.Path)); err == nil {
		route.Exists = true
	}
	route.Skeleton = Skeleton(pageType, stem, now)
	return route, nil
}

func folderFor(kind Kind, mode Mode, pageType string) (string, error) {
	types := RoutableTypes(kind, mode)
	found := false
	for _, t := range types {
		if t == pageType {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("type %q is not filed in a %s in %s mode; use one of %s", pageType, kind.Noun(), mode, strings.Join(types, ", "))
	}
	if mode == LYT {
		if pageType == "moc" {
			return "wiki/mocs", nil
		}
		return "wiki/notes", nil
	}
	folder, ok := genericFolders[pageType]
	if !ok {
		return "", fmt.Errorf("type %q is not filed in a %s in %s mode; use one of %s", pageType, kind.Noun(), mode, strings.Join(types, ", "))
	}
	return folder, nil
}

// Skeleton is the starting text for a new page: frontmatter plus the headings that type
// usually carries. Headings left empty show up in lint, so the author removes what is unused.
func Skeleton(pageType, title string, now time.Time) string {
	date := now.Format("2006-01-02")
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntype: %s\ntitle: %q\nstatus: seed\ncreated: %s\nupdated: %s\ntags:\n  - %s\n", pageType, title, date, date, pageType)
	switch pageType {
	case "source":
		b.WriteString("source_type: \nauthor: \ndate_published: \nurl: \nsource_id: \n")
	case "question":
		b.WriteString("question: \"\"\n")
	case "note":
		b.WriteString("mocs: []\n")
	}
	b.WriteString("---\n\n# " + title + "\n\n")
	for _, h := range headingsFor(pageType) {
		b.WriteString("## " + h + "\n\n")
	}
	return b.String()
}

func headingsFor(pageType string) []string {
	switch pageType {
	case "source":
		return []string{"Summary", "Key claims", "Notes"}
	case "entity":
		return []string{"Overview", "Relationships", "Sources"}
	case "concept":
		return []string{"Definition", "Why it matters", "Related", "Sources"}
	case "question":
		return []string{"Answer", "Evidence", "Open"}
	case "session":
		return []string{"Context", "Outcome", "Follow-ups"}
	case "note":
		return []string{"Idea", "Sources", "See also"}
	case "moc":
		return []string{"Why this map exists", "Core notes", "Adjacent maps", "Open questions"}
	}
	return nil
}

// PageTitle derives a page's title from its path: the file stem.
func PageTitle(rel string) string {
	return strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
}

// Match is an existing page that a title names: by its file stem or by an alias in its
// frontmatter, compared without regard to case.
type Match struct {
	Path    string `json:"path"`
	ByAlias string `json:"by_alias,omitempty"`
}

// FindPage looks for a page named title under wiki/: the file stem first, then the
// aliases in every page's frontmatter. It returns nil when none matches.
func FindPage(root, title string) (*Match, error) {
	wikiRoot := filepath.Join(root, WikiDir)
	if _, err := os.Stat(wikiRoot); err != nil {
		return nil, nil
	}
	sanitized := SanitizeTitle(title)
	var stemMatch, aliasMatch *Match
	err := filepath.WalkDir(wikiRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != wikiRoot && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		stem := strings.TrimSuffix(d.Name(), ".md")
		if strings.EqualFold(stem, title) || strings.EqualFold(stem, sanitized) {
			stemMatch = &Match{Path: rel}
			return fs.SkipAll
		}
		if aliasMatch != nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			// The page cannot be read (a broken symlink, permissions); skip it and
			// keep looking rather than fail the whole search.
			return nil
		}
		fields, _, err := Frontmatter(string(data))
		if err != nil || fields == nil {
			return nil
		}
		for _, alias := range StringList(fields, "aliases") {
			if strings.EqualFold(alias, title) {
				aliasMatch = &Match{Path: rel, ByAlias: alias}
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if stemMatch != nil {
		return stemMatch, nil
	}
	return aliasMatch, nil
}
