// Package ledger reads and updates the source ledger: one record per captured source,
// keyed by a stable identity, saying where the bytes are and which pages came from them.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const Schema = "claude-atlas.source-ledger.v1"

// VaultPath is where a vault keeps its source ledger, relative to the vault root. It is
// vault.LedgerPath; the constant lives here so this package can reach a mounted knowledge
// base's ledger without importing vault, which imports this one.
const VaultPath = "wiki/meta/ledgers/source-ledger.json"

// LegacySchema is claude-obsidian's; its records read the same way.
const LegacySchema = "claude-obsidian.source-ledger.v1"

var Authorities = []string{"official", "primary", "secondary", "community", "synthetic", "unknown"}

// Origin says where a source came from.
type Origin struct {
	Kind    string `json:"kind"`    // file, url, or text
	Locator string `json:"locator"` // vault-relative path for file; the URL for url
}

// Via names the project a source came through into a knowledge base. Provenance, not a link.
type Via struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Source is one ledger record.
type Source struct {
	Title         string   `json:"title"`
	Origin        Origin   `json:"origin"`
	ContentSHA256 string   `json:"content_sha256,omitempty"`
	ContentKind   string   `json:"content_kind,omitempty"`
	Authority     string   `json:"authority"`
	ReviewStatus  string   `json:"review_status"`
	CapturedAt    string   `json:"captured_at,omitempty"`
	IngestedAt    string   `json:"ingested_at,omitempty"`
	Pages         []string `json:"pages"`
	Notes         string   `json:"notes,omitempty"`
	Via           *Via     `json:"via,omitempty"`
	// Extra keeps fields we do not model, so an adopted ledger loses nothing.
	Extra map[string]json.RawMessage `json:"-"`
}

// Ledger is the file.
type Ledger struct {
	Schema      string            `json:"schema"`
	GeneratedAt string            `json:"generated_at"`
	Sources     map[string]Source `json:"sources"`
}

// Empty is a new ledger.
func Empty(now time.Time) *Ledger {
	return &Ledger{Schema: Schema, GeneratedAt: timestamp(now), Sources: map[string]Source{}}
}

func timestamp(now time.Time) string { return now.UTC().Format("2006-01-02T15:04:05Z") }

// ID is the stable identity of a source: `src-` plus the first 20 hex digits of
// sha256(kind, locator, content hash). It matches claude-obsidian's formula.
func ID(kind, locator, contentSHA256 string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(kind) + "\x00" + locator + "\x00" + strings.ToLower(contentSHA256)))
	return "src-" + hex.EncodeToString(sum[:])[:20]
}

// Parse decodes a ledger, keeping unknown record fields.
func Parse(data []byte) (*Ledger, error) {
	var raw struct {
		Schema      string                     `json:"schema"`
		GeneratedAt string                     `json:"generated_at"`
		Sources     map[string]json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("source ledger: %w", err)
	}
	if raw.Schema != Schema && raw.Schema != LegacySchema {
		return nil, fmt.Errorf("source ledger: unsupported schema %q", raw.Schema)
	}
	l := &Ledger{Schema: Schema, GeneratedAt: raw.GeneratedAt, Sources: map[string]Source{}}
	for id, rec := range raw.Sources {
		var s Source
		if err := json.Unmarshal(rec, &s); err != nil {
			return nil, fmt.Errorf("source ledger: %s: %w", id, err)
		}
		var all map[string]json.RawMessage
		json.Unmarshal(rec, &all)
		known := map[string]bool{"title": true, "origin": true, "content_sha256": true, "content_kind": true, "authority": true,
			"review_status": true, "captured_at": true, "ingested_at": true, "pages": true, "notes": true, "via": true}
		for k, v := range all {
			if !known[k] {
				if s.Extra == nil {
					s.Extra = map[string]json.RawMessage{}
				}
				s.Extra[k] = v
			}
		}
		if s.Pages == nil {
			s.Pages = []string{}
		}
		l.Sources[id] = s
	}
	return l, nil
}

// Load reads a ledger file; a missing file is an empty ledger.
func Load(path string, now time.Time) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Empty(now), nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Encode renders the ledger with sorted keys and a trailing newline.
func (l *Ledger) Encode() []byte {
	type record map[string]any
	sources := map[string]record{}
	for id, s := range l.Sources {
		r := record{
			"title": s.Title, "origin": s.Origin, "authority": s.Authority, "review_status": s.ReviewStatus, "pages": s.Pages,
		}
		if s.Pages == nil {
			r["pages"] = []string{}
		}
		if s.ContentSHA256 != "" {
			r["content_sha256"] = s.ContentSHA256
		}
		if s.ContentKind != "" {
			r["content_kind"] = s.ContentKind
		}
		if s.CapturedAt != "" {
			r["captured_at"] = s.CapturedAt
		}
		if s.IngestedAt != "" {
			r["ingested_at"] = s.IngestedAt
		}
		if s.Notes != "" {
			r["notes"] = s.Notes
		}
		if s.Via != nil {
			r["via"] = s.Via
		}
		for k, v := range s.Extra {
			r[k] = v
		}
		sources[id] = r
	}
	data, _ := json.MarshalIndent(map[string]any{"schema": Schema, "generated_at": l.GeneratedAt, "sources": sources}, "", "  ")
	return append(data, '\n')
}

// Update is what an operation may change on a record. A missing record is created only
// when Origin and ContentSHA256 are given (capture); otherwise the ID must exist.
type Update struct {
	ID            string   `json:"id"`
	Title         string   `json:"title,omitempty"`
	Origin        *Origin  `json:"origin,omitempty"`
	ContentSHA256 string   `json:"content_sha256,omitempty"`
	ContentKind   string   `json:"content_kind,omitempty"`
	Authority     string   `json:"authority,omitempty"`
	Ingested      bool     `json:"ingested,omitempty"`
	Pages         []string `json:"pages,omitempty"`
	Notes         string   `json:"notes,omitempty"`
	Via           *Via     `json:"via,omitempty"`
}

// Apply merges updates into the ledger. It validates each update and returns the first error.
func (l *Ledger) Apply(updates []Update, now time.Time) error {
	for _, u := range updates {
		if !strings.HasPrefix(u.ID, "src-") {
			return fmt.Errorf("source id %q must start with src-", u.ID)
		}
		rec, exists := l.Sources[u.ID]
		if !exists {
			if u.Origin == nil {
				return fmt.Errorf("source %s is not in the ledger; capture it first", u.ID)
			}
			if got := ID(u.Origin.Kind, u.Origin.Locator, u.ContentSHA256); got != u.ID {
				return fmt.Errorf("source id %s does not match its origin (expected %s)", u.ID, got)
			}
			rec = Source{Origin: *u.Origin, ContentSHA256: u.ContentSHA256, ContentKind: u.ContentKind,
				Authority: "unknown", ReviewStatus: "unreviewed", CapturedAt: now.Format("2006-01-02"), Pages: []string{}}
		}
		if u.Title != "" {
			rec.Title = u.Title
		}
		if u.Authority != "" {
			if !contains(Authorities, u.Authority) {
				return fmt.Errorf("source %s: authority must be one of %s", u.ID, strings.Join(Authorities, ", "))
			}
			rec.Authority = u.Authority
		}
		if u.Ingested {
			rec.IngestedAt = now.Format("2006-01-02")
			rec.ReviewStatus = "active"
		}
		for _, p := range u.Pages {
			if !strings.HasPrefix(p, "wiki/") {
				return fmt.Errorf("source %s: page %q must be under wiki/", u.ID, p)
			}
			if !contains(rec.Pages, p) {
				rec.Pages = append(rec.Pages, p)
			}
		}
		sort.Strings(rec.Pages)
		if u.Notes != "" {
			rec.Notes = u.Notes
		}
		if u.Via != nil {
			rec.Via = u.Via
		}
		if rec.Title == "" {
			rec.Title = rec.Origin.Locator
		}
		l.Sources[u.ID] = rec
	}
	l.GeneratedAt = timestamp(now)
	return nil
}

// Dropped names a page path removed from a record.
type Dropped struct {
	ID   string
	Page string
}

// DropPages removes every page path for which exists reports false.
func (l *Ledger) DropPages(exists func(page string) bool, now time.Time) []Dropped {
	ids := make([]string, 0, len(l.Sources))
	for id := range l.Sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var dropped []Dropped
	for _, id := range ids {
		rec := l.Sources[id]
		kept := []string{}
		for _, p := range rec.Pages {
			if exists(p) {
				kept = append(kept, p)
			} else {
				dropped = append(dropped, Dropped{ID: id, Page: p})
			}
		}
		if len(kept) != len(rec.Pages) {
			rec.Pages = kept
			l.Sources[id] = rec
		}
	}
	if len(dropped) > 0 {
		l.GeneratedAt = timestamp(now)
	}
	return dropped
}

// FindBySHA returns the record whose content hash matches, if any.
func (l *Ledger) FindBySHA(sha string) (string, *Source) {
	for id, s := range l.Sources {
		if s.ContentSHA256 == sha {
			rec := s
			return id, &rec
		}
	}
	return "", nil
}

// Holder names the vault whose ledger records a source, and the record.
type Holder struct {
	Root   string // the vault's root
	Mount  string // the mount name the vault was reached through
	ID     string // the source id in that vault's ledger
	Source *Source
}

// Mounted looks a content hash up in the ledgers of the knowledge bases a project mounts.
// Knowledge enters through a project, so a source one of them captured is captured, and
// the project may remove it from its inbox.
type Mounted struct {
	paths   map[string]string
	now     time.Time
	loaded  bool
	reads   int
	ledgers []mountedLedger
}

type mountedLedger struct {
	mount  string
	root   string
	ledger *Ledger
}

// Mounts prepares the lookup over a project's mounts: a mount name to that knowledge
// base's wiki path, the map the atlas resolves. It reads nothing; the first Find loads the
// ledgers, and a caller that never asks reads none.
func Mounts(paths map[string]string, now time.Time) *Mounted {
	return &Mounted{paths: paths, now: now}
}

// Find returns the mounted vault whose ledger records the content hash, or nil. Mounts are
// visited in name order, so two knowledge bases that hold one source answer the same way
// every time; a ledger that cannot be read holds nothing.
func (m *Mounted) Find(sha string) *Holder {
	if m == nil || sha == "" {
		return nil
	}
	m.load()
	for _, l := range m.ledgers {
		if id, rec := l.ledger.FindBySHA(sha); id != "" {
			return &Holder{Root: l.root, Mount: l.mount, ID: id, Source: rec}
		}
	}
	return nil
}

func (m *Mounted) load() {
	if m.loaded {
		return
	}
	m.loaded = true
	names := make([]string, 0, len(m.paths))
	for name := range m.paths {
		if m.paths[name] != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		root := filepath.Dir(m.paths[name])
		m.reads++
		led, err := Load(filepath.Join(root, filepath.FromSlash(VaultPath)), m.now)
		if err != nil {
			continue
		}
		m.ledgers = append(m.ledgers, mountedLedger{mount: name, root: root, ledger: led})
	}
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
