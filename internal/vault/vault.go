// Package vault knows what a claude-atlas vault is: its identity file, its layout, how
// one is created or adopted, and where a new page of a given type belongs.
package vault

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/ledger"
)

const (
	// Marker is the identity file at the vault root.
	Marker = ".claude-atlas.json"
	// LegacyMarker is claude-obsidian's identity file; adopt converts such vaults.
	LegacyMarker = ".claude-obsidian.json"
	Schema       = "claude-atlas.vault.v3"
	// SchemaV2 is the identity file v2 wrote: a knowledge base opens and upgrades, a
	// project vault is refused, because v3 projects are folders, not vaults.
	SchemaV2 = "claude-atlas.vault.v2"
	// SchemaV1 is the identity file older versions wrote; adopt rewrites it.
	SchemaV1 = "claude-atlas.vault.v1"
	// EnvVault names the vault explicitly for the MCP server and hooks.
	EnvVault = "CLAUDE_ATLAS_VAULT"

	MetaDir      = ".vault-meta"
	InboxDir     = "inbox"
	IdeasDir     = "ideas"
	RawDir       = ".raw"
	CapturedDir  = ".raw/captured"
	WikiDir      = "wiki"
	LogPage      = "wiki/log.md"
	HotPage      = "wiki/hot.md"
	IndexPage    = "wiki/index.md"
	OverviewPage = "wiki/overview.md"
	LedgerPath   = ledger.VaultPath

	// A folder's index page takes the folder's name, so no page shares the basename of
	// wiki/index.md.
	CanvasIndex = "wiki/canvases/canvases.md"
)

// Mode is the filing methodology for new pages.
type Mode string

const (
	Generic Mode = "generic"
	LYT     Mode = "lyt"
)

var Modes = []Mode{Generic, LYT}

// ParseMode validates a mode name.
func ParseMode(s string) (Mode, error) {
	for _, m := range Modes {
		if string(m) == s {
			return m, nil
		}
	}
	return "", fmt.Errorf("mode must be generic or lyt, not %q", s)
}

// Kind is the one kind of vault: a knowledge base. The identity file still names it, so
// a file from another tool, or a v2 project vault, is refused rather than misread.
const Kind = "knowledge"

// Config is the content of the identity file: the facts that travel with the vault. It
// never holds a path; paths are facts about one machine and live in the atlas config.
type Config struct {
	Schema  string `json:"schema"`
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Mode    Mode   `json:"mode"`
	Created string `json:"created"`
	// Scope is one sentence saying what knowledge the vault holds.
	Scope string `json:"scope,omitempty"`
}

// Encode renders the identity file.
func (c Config) Encode() []byte {
	data, _ := json.MarshalIndent(c, "", "  ")
	return append(data, '\n')
}

// NewID mints a random UUID (version 4) for a vault.
func NewID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Vault is an opened claude-atlas vault.
type Vault struct {
	Root   string
	Config Config
}

// Name is the vault's name from its identity file.
func (v *Vault) Name() string { return v.Config.Name }

// Path joins a vault-relative path onto the root.
func (v *Vault) Path(rel string) string { return filepath.Join(v.Root, filepath.FromSlash(rel)) }

// Repo is the vault's git repository.
func (v *Vault) Repo() gitx.Repo { return RepoAt(v.Root) }

// RepoAt is the git repository of the vault at root: the vault's own, or the working tree
// that holds the vault, scoped to the vault's folder. Every vault command goes through it,
// so none can miss the prefix. Nothing stores which, so a clone answers the same way.
func RepoAt(root string) gitx.Repo { return gitx.At(root) }

// Host is the top of the working tree that holds the vault at root, or "" when the vault
// is its own repository or in none.
func Host(root string) string {
	if repo := RepoAt(root); repo.Prefix != "" {
		return repo.Dir
	}
	return ""
}

// HostFor is the top of the working tree a new vault at path would commit into, or ""
// when it would get a repository of its own. path need not exist. It refuses a path the
// working tree ignores, because no commit could record it.
func HostFor(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
	repo := gitx.At(dir)
	if repo.Prefix == "" && !repo.IsRepo() {
		return "", nil
	}
	rest, err := filepath.Rel(dir, abs)
	if err != nil {
		return "", err
	}
	if rest == "." {
		if repo.Prefix == "" {
			return "", nil
		}
		rest = ""
	} else {
		rest = filepath.ToSlash(rest) + "/"
	}
	if repo.Ignored(rest) {
		return "", fmt.Errorf("%s is ignored by the git repository %s; a knowledge base needs its history", abs, repo.Dir)
	}
	return repo.Dir, nil
}

// joinRepo readies the repository a new or adopted vault at abs commits into: the working
// tree that holds it, or a new one at abs when there is none. It refuses a folder the
// holding tree ignores, because no commit could record it. It reports whether it ran
// git init.
func joinRepo(abs string) (gitx.Repo, bool, error) {
	repo := RepoAt(abs)
	if repo.Prefix != "" {
		if repo.Ignored("") {
			return repo, false, fmt.Errorf("%s is ignored by the git repository %s; a knowledge base needs its history", abs, repo.Dir)
		}
		return repo, false, nil
	}
	if repo.IsRepo() {
		return repo, false, nil
	}
	if err := repo.Init(); err != nil {
		return repo, false, err
	}
	return repo, true, nil
}

var ErrNotVault = errors.New("not a claude-atlas vault")

// ErrV1 means the identity file is from before v2; adopt rewrites it.
var ErrV1 = errors.New("v1 vault")

// ErrProjectVault means the identity file is a v2 project vault. v3 projects are folders
// inside the work; the vault is recreated with `claude-atlas init`.
var ErrProjectVault = errors.New("v2 project vault")

// ReadConfig parses the identity file without validating it. ok is false when there is
// none or it is not JSON. Lint and adopt read a vault this way; everything else opens it.
func ReadConfig(root string) (Config, bool) {
	data, err := os.ReadFile(filepath.Join(root, Marker))
	if err != nil {
		return Config{}, false
	}
	var cfg Config
	if json.Unmarshal(data, &cfg) != nil {
		return Config{}, false
	}
	return cfg, true
}

// IsVault reports whether root carries the identity file.
func IsVault(root string) bool { return markerExists(root) }

// markerExists reports whether the identity file is there, readable or not. Adopt tells a
// damaged file from a directory that has none this way.
func markerExists(root string) bool {
	info, err := os.Stat(filepath.Join(root, Marker))
	return err == nil && info.Mode().IsRegular()
}

// IsLegacy reports whether root is a claude-obsidian vault that has not been adopted.
func IsLegacy(root string) bool {
	if IsVault(root) {
		return false
	}
	info, err := os.Stat(filepath.Join(root, LegacyMarker))
	return err == nil && info.Mode().IsRegular()
}

// IsAdoptable reports whether root looks like a vault of some kind: ours, claude-obsidian's,
// an Obsidian vault, or a directory that already has a wiki/.
func IsAdoptable(root string) bool {
	if IsVault(root) || IsLegacy(root) {
		return true
	}
	for _, name := range []string{".obsidian", WikiDir} {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// Open reads the identity file at root.
func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(abs, Marker))
	if errors.Is(err, os.ErrNotExist) {
		if IsLegacy(abs) {
			return nil, fmt.Errorf("%w: %s is a claude-obsidian vault; run `claude-atlas adopt %s`", ErrNotVault, abs, abs)
		}
		return nil, fmt.Errorf("%w: %s has no %s", ErrNotVault, abs, Marker)
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(abs, Marker), err)
	}
	marker := filepath.Join(abs, Marker)
	switch cfg.Schema {
	case Schema:
	case SchemaV2:
		// A v2 knowledge base reads as it is; Upgrade raises the schema. Its access and
		// grants are ignored, because v3 has none.
		if cfg.Kind == "project" {
			return nil, fmt.Errorf("%w: %s is a v2 project vault; v3 projects are folders, so run `claude-atlas init` in the work and delete this vault", ErrProjectVault, abs)
		}
	case SchemaV1:
		return nil, fmt.Errorf("%w: %s was made by claude-atlas v1; run `claude-atlas adopt %s`", ErrV1, abs, abs)
	default:
		return nil, fmt.Errorf("%s: unsupported schema %q", marker, cfg.Schema)
	}
	if cfg.Kind != Kind {
		return nil, fmt.Errorf("%s: kind must be %s, not %q", marker, Kind, cfg.Kind)
	}
	if cfg.ID == "" {
		return nil, fmt.Errorf("%s has no id; run `claude-atlas adopt %s`", marker, abs)
	}
	if cfg.Name == "" {
		cfg.Name = filepath.Base(abs)
	}
	if cfg.Mode == "" {
		cfg.Mode = Generic
	}
	if _, err := ParseMode(string(cfg.Mode)); err != nil {
		return nil, fmt.Errorf("%s: %w", marker, err)
	}
	return &Vault{Root: abs, Config: cfg}, nil
}

// FindAbove returns the nearest vault root at or above start, or "".
func FindAbove(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if IsVault(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Resolve picks a vault: the explicit path, then $CLAUDE_ATLAS_VAULT, then the nearest
// vault at or above start. It fails closed when none applies.
func Resolve(explicit, envValue, start string) (*Vault, error) {
	switch {
	case explicit != "":
		return Open(explicit)
	case envValue != "":
		return Open(envValue)
	}
	if root := FindAbove(start); root != "" {
		return Open(root)
	}
	return nil, fmt.Errorf("%w: no vault at or above %s; pass a vault path or set %s", ErrNotVault, start, EnvVault)
}

//go:embed all:templates
var templates embed.FS

const templateDir = "templates/knowledge"

// TemplateFiles lists the vault-relative paths the template provides.
func TemplateFiles() []string {
	var out []string
	fs.WalkDir(templates, templateDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, strings.TrimPrefix(path, templateDir+"/"))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func renderTemplate(rel string, now time.Time) ([]byte, error) {
	data, err := templates.ReadFile(templateDir + "/" + rel)
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(data, []byte("{{generated_date}}"), []byte(now.Format("2006-01-02"))), nil
}

// NewOperationID mints `<kind>-<yyyymmdd>-<hhmmss>-<4 hex>`.
func NewOperationID(kind string, now time.Time) string {
	var b [2]byte
	rand.Read(b[:])
	return fmt.Sprintf("%s-%s-%s", kind, now.UTC().Format("20060102-150405"), hex.EncodeToString(b[:]))
}

// CommitMessage is the subject and trailer format every core commit uses.
func CommitMessage(kind, summary, operationID string) string {
	summary = strings.TrimSpace(strings.ReplaceAll(summary, "\n", " "))
	return fmt.Sprintf("%s: %s\n\natlas-operation: %s\n", kind, summary, operationID)
}

// InitResult reports what Init created.
type InitResult struct {
	Root        string
	OperationID string
	Commit      string
	Files       []string
	// Host is the working tree the vault commits into, or "" when it has its own.
	Host string
}

func writeFile(root, rel string, data []byte) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func requireGit() error {
	if !gitx.Available() {
		return errors.New("git is required and is not on PATH")
	}
	return nil
}

// Options say what to make: the mode (generic by default), the name (the directory's by
// default), and the scope.
type Options struct {
	Mode  Mode
	Name  string
	Scope string
}

// newConfig is the identity file of a vault made now.
func newConfig(root string, opts Options, now time.Time) (Config, error) {
	mode := opts.Mode
	if mode == "" {
		mode = Generic
	}
	if _, err := ParseMode(string(mode)); err != nil {
		return Config{}, err
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = filepath.Base(root)
	}
	return Config{Schema: Schema, ID: NewID(), Kind: Kind, Name: name, Mode: mode, Created: now.Format("2006-01-02"), Scope: strings.TrimSpace(opts.Scope)}, nil
}

// Init creates a vault at root: the template, the identity file, an empty source ledger,
// and one commit. The commit goes into the working tree that holds root, or into a new
// repository at root when there is none. root must not exist or must be an empty directory.
func Init(root string, opts Options, now time.Time) (*InitResult, error) {
	if err := requireGit(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cfg, err := newConfig(abs, opts, now)
	if err != nil {
		return nil, err
	}
	if err := checkEmpty(abs); err != nil {
		return nil, err
	}
	_, statErr := os.Stat(abs)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	repo, _, err := joinRepo(abs)
	if err == nil {
		err = repo.CheckIdle()
	}
	if err != nil {
		if statErr != nil {
			os.Remove(abs)
		}
		return nil, err
	}
	files, err := writeMissing(abs, cfg, now, true)
	if err != nil {
		return nil, err
	}
	if err := repo.AddAll(); err != nil {
		return nil, err
	}
	id := NewOperationID("setup", now)
	sha, err := repo.Commit(CommitMessage("setup", fmt.Sprintf("initialize knowledge base %s (%s mode)", cfg.Name, cfg.Mode), id))
	if err != nil {
		return nil, err
	}
	return &InitResult{Root: abs, OperationID: id, Commit: sha, Files: files, Host: Host(abs)}, nil
}

// checkEmpty refuses a root that exists and holds anything.
func checkEmpty(abs string) error {
	entries, err := os.ReadDir(abs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty; use `claude-atlas adopt` for an existing vault", abs)
	}
	return nil
}

// writeMissing writes template, identity, and ledger files that do not exist yet.
// With overwrite set (a fresh init) every file is written.
func writeMissing(root string, cfg Config, now time.Time, overwrite bool) ([]string, error) {
	// A settings file the merge cannot read stops the pass before it writes anything, so a
	// refused upgrade leaves the vault as it was.
	if !overwrite {
		if err := checkSettings(root); err != nil {
			return nil, err
		}
	}
	var written []string
	put := func(rel string, data []byte) error {
		if !overwrite {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
				return nil
			}
		}
		if err := writeFile(root, rel, data); err != nil {
			return err
		}
		written = append(written, rel)
		return nil
	}
	for _, rel := range TemplateFiles() {
		data, err := renderTemplate(rel, now)
		if err != nil {
			return nil, err
		}
		if rel == ".gitignore" && !overwrite {
			if err := mergeGitignore(root, data); err != nil {
				return nil, err
			}
			continue
		}
		if merge, ok := settingsMerges[rel]; ok && !overwrite {
			changed, err := mergeSettings(root, rel, data, merge)
			if err != nil {
				return nil, err
			}
			if changed {
				written = append(written, rel)
			}
			continue
		}
		if err := put(rel, data); err != nil {
			return nil, err
		}
	}
	if err := put(Marker, cfg.Encode()); err != nil {
		return nil, err
	}
	if err := put(LedgerPath, ledger.Empty(now).Encode()); err != nil {
		return nil, err
	}
	sort.Strings(written)
	return written, nil
}

// UpgradeResult reports what Upgrade changed; an empty result means the vault was current.
type UpgradeResult struct {
	Added []string
	// Schema is set when the identity file was raised to the current schema.
	Schema bool
}

// Upgrade brings a vault made by an older version to the current layout, as one commit:
// it raises a v2 identity file to v3, dropping access and grants, and adds the template
// files the vault lacks.
func Upgrade(root string, now time.Time) (*UpgradeResult, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	v, err := Open(abs)
	if err != nil {
		return nil, err
	}
	res := &UpgradeResult{}
	repo := v.Repo()
	if err := repo.CheckIdle(); err != nil {
		return res, err
	}
	if v.Config.Schema != Schema {
		v.Config.Schema = Schema
		if err := writeFile(abs, Marker, v.Config.Encode()); err != nil {
			return res, err
		}
		res.Schema = true
	}
	if res.Added, err = writeMissing(abs, v.Config, now, false); err != nil {
		return res, err
	}
	if len(res.Added) == 0 && !res.Schema {
		return res, nil
	}
	stage := append([]string{}, res.Added...)
	var what []string
	if len(res.Added) > 0 {
		what = append(what, "add "+strings.Join(res.Added, ", "))
	}
	if res.Schema {
		stage = append(stage, Marker)
		what = append(what, "raise the identity file to "+Schema)
	}
	if err := repo.Add(stage...); err != nil {
		return res, err
	}
	if _, err := repo.Commit(CommitMessage("setup", strings.Join(what, "; "), NewOperationID("setup", now))); err != nil {
		return res, err
	}
	return res, nil
}

// UpdateConfig rewrites the identity file through change and commits it as one setup
// operation named by summary. An unchanged file makes no commit. It is the one way a
// vault's own facts (name, scope) change.
func UpdateConfig(root, summary string, now time.Time, change func(*Config) error) error {
	unlock, err := Lock(root)
	if err != nil {
		return err
	}
	defer unlock()
	v, err := Open(root)
	if err != nil {
		return err
	}
	if err := v.Repo().CheckIdle(); err != nil {
		return err
	}
	cfg := v.Config
	if err := change(&cfg); err != nil {
		return err
	}
	if _, err := ParseMode(string(cfg.Mode)); err != nil {
		return err
	}
	if cfg.Kind != Kind {
		return fmt.Errorf("a vault's kind does not change")
	}
	if cfg.ID != v.Config.ID {
		return fmt.Errorf("a vault's id does not change")
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("name must not be blank")
	}
	cfg.Schema = Schema
	cfg.Scope = strings.TrimSpace(cfg.Scope)
	existing, err := os.ReadFile(filepath.Join(v.Root, Marker))
	if err == nil && bytes.Equal(cfg.Encode(), existing) {
		return nil
	}
	if err := writeFile(v.Root, Marker, cfg.Encode()); err != nil {
		return err
	}
	repo := v.Repo()
	if err := repo.Add(Marker); err != nil {
		return err
	}
	_, err = repo.Commit(CommitMessage("setup", summary, NewOperationID("setup", now)))
	return err
}

// AppearanceFile is Obsidian's appearance settings, where CSS snippets are enabled.
const AppearanceFile = ".obsidian/appearance.json"

// AppFile is Obsidian's app settings, where the folder for new notes is set.
const AppFile = ".obsidian/app.json"

// SnippetName is the vault's own CSS snippet, .obsidian/snippets/claude-atlas.css.
const SnippetName = "claude-atlas"

// settingsMerges are the Obsidian settings files an existing vault keeps, with the change
// each one needs.
var settingsMerges = map[string]func(settings map[string]any) bool{
	AppearanceFile: enableSnippet,
	AppFile:        newNotesInWiki,
}

// checkSettings reads every Obsidian settings file the vault holds and refuses one that
// is not a JSON object.
func checkSettings(root string) error {
	for _, rel := range TemplateFiles() {
		if _, ok := settingsMerges[rel]; !ok {
			continue
		}
		if _, _, err := readSettings(root, rel); err != nil {
			return err
		}
	}
	return nil
}

// readSettings parses an existing Obsidian settings file. It reports whether the file is
// there; an empty one parses as no settings at all.
func readSettings(root, rel string) (map[string]any, bool, error) {
	existing, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	settings := map[string]any{}
	if len(bytes.TrimSpace(existing)) > 0 {
		var holds map[string]any
		if err := json.Unmarshal(existing, &holds); err != nil || holds == nil {
			return nil, true, fmt.Errorf("%s is not a JSON object; Obsidian wrote it, so fix or remove the file, then try again", rel)
		}
		settings = holds
	}
	return settings, true, nil
}

// mergeSettings applies merge to an existing Obsidian settings file, keeping every other
// setting, and writes the template when there is none. It reports whether the file changed.
func mergeSettings(root, rel string, template []byte, merge func(map[string]any) bool) (bool, error) {
	settings, found, err := readSettings(root, rel)
	if err != nil {
		return false, err
	}
	if !found {
		return true, writeFile(root, rel, template)
	}
	if !merge(settings) {
		return false, nil
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), append(data, '\n'), 0o644)
}

// enableSnippet turns on the vault's CSS snippet.
func enableSnippet(settings map[string]any) bool {
	var enabled []any
	if list, ok := settings["enabledCssSnippets"].([]any); ok {
		enabled = list
	}
	for _, item := range enabled {
		if item == SnippetName {
			return false
		}
	}
	settings["enabledCssSnippets"] = append(enabled, SnippetName)
	return true
}

// newNotesInWiki puts the notes Obsidian creates, from a click on a link to a missing page
// or from a new note, under wiki/, unless the user chose a location.
func newNotesInWiki(settings map[string]any) bool {
	if _, chosen := settings["newFileLocation"]; chosen {
		return false
	}
	settings["newFileLocation"] = "folder"
	settings["newFileFolderPath"] = WikiDir
	return true
}

// mergeGitignore appends the lines of the template ignore file that are missing.
func mergeGitignore(root string, template []byte) error {
	path := filepath.Join(root, ".gitignore")
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(path, template, 0o644)
	}
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, line := range strings.Split(string(template), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || have[trimmed] {
			continue
		}
		missing = append(missing, trimmed)
	}
	if len(missing) == 0 {
		return nil
	}
	var b bytes.Buffer
	b.Write(existing)
	if !bytes.HasSuffix(existing, []byte("\n")) {
		b.WriteString("\n")
	}
	b.WriteString("\n# added by claude-atlas\n")
	b.WriteString(strings.Join(missing, "\n") + "\n")
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// AdoptResult reports what Adopt changed.
type AdoptResult struct {
	Root           string
	OperationID    string
	Commit         string
	Added          []string
	GitInitialized bool
	// Host is the working tree the vault commits into, or "" when it has its own.
	Host           string
	WasLegacy      bool
	AlreadyAdopted bool
	// FromV1 is set when a v1 identity file was rewritten.
	FromV1 bool
	// Repaired is set when a damaged identity file was rebuilt.
	Repaired bool
	// FromV2 is set when a v2 identity file was raised to v3.
	FromV2 bool
}

// markerState reads the identity file's condition into res: a current file means the vault
// is adopted, a v1 file is rewritten, and a file that does not parse, names no id, or names
// no valid kind is repaired. Any other schema is refused.
func markerState(root string, existing Config, parsed bool, res *AdoptResult) error {
	if !markerExists(root) {
		return nil
	}
	if !parsed {
		res.Repaired = true
		return nil
	}
	switch existing.Schema {
	case Schema:
		res.Repaired = existing.ID == "" || existing.Kind != Kind
		res.AlreadyAdopted = !res.Repaired
	case SchemaV2:
		if existing.Kind == "project" {
			return fmt.Errorf("%w: %s; v3 projects are folders, so run `claude-atlas init` in the work and delete this vault", ErrProjectVault, root)
		}
		res.FromV2 = existing.ID != "" && existing.Kind == Kind
		res.Repaired = !res.FromV2
	case SchemaV1:
		res.FromV1 = true
	default:
		return fmt.Errorf("%s: unsupported schema %q", filepath.Join(root, Marker), existing.Schema)
	}
	return nil
}

// adoptConfig decides the identity file adopt writes: a current one is kept, a v2 one
// keeps its id and loses its access and grants, a v1 or a damaged one is rebuilt with its
// mode and creation date, and a vault without one gets a new one.
func adoptConfig(root string, existing Config, res *AdoptResult, opts Options, now time.Time) (Config, error) {
	if res.AlreadyAdopted {
		return existing, nil
	}
	if res.FromV2 {
		existing.Schema = Schema
		if opts.Name != "" {
			existing.Name = opts.Name
		}
		if opts.Scope != "" {
			existing.Scope = opts.Scope
		}
		return existing, nil
	}
	if opts.Mode == "" {
		opts.Mode = existing.Mode
	}
	if opts.Mode == "" && IsLegacy(root) {
		opts.Mode = legacyMode(root)
	}
	if opts.Name == "" {
		opts.Name = existing.Name
	}
	if opts.Scope == "" {
		opts.Scope = existing.Scope
	}
	cfg, err := newConfig(root, opts, now)
	if err != nil {
		return Config{}, err
	}
	if existing.Created != "" {
		cfg.Created = existing.Created
	}
	return cfg, nil
}

// Adopt turns an existing directory, an Obsidian vault, a claude-obsidian vault, or a v1
// or v2 vault into a claude-atlas knowledge base. It adds only what is missing and
// commits every file already there. It rewrites a v1 identity file, raises a v2 one, and
// repairs a damaged one; it never replaces or removes any other file.
func Adopt(root string, opts Options, now time.Time) (*AdoptResult, error) {
	if err := requireGit(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	if !IsAdoptable(abs) {
		return nil, fmt.Errorf("%s is not a vault: it has no .obsidian/, wiki/, or vault identity file; create one with `claude-atlas new-knowledge`", abs)
	}
	existing, parsed := ReadConfig(abs)
	res := &AdoptResult{Root: abs, WasLegacy: IsLegacy(abs)}
	if err := markerState(abs, existing, parsed, res); err != nil {
		return nil, err
	}
	cfg, err := adoptConfig(abs, existing, res, opts, now)
	if err != nil {
		return nil, err
	}
	if err := RepoAt(abs).CheckIdle(); err != nil {
		return nil, err
	}
	repo, created, err := joinRepo(abs)
	if err != nil {
		return nil, err
	}
	res.GitInitialized = created
	if repo.Prefix != "" {
		res.Host = repo.Dir
	}
	if res.FromV1 || res.FromV2 || res.Repaired {
		if err := writeFile(abs, Marker, cfg.Encode()); err != nil {
			return nil, err
		}
		res.Added = append(res.Added, Marker)
	}
	added, err := writeMissing(abs, cfg, now, false)
	if err != nil {
		return nil, err
	}
	res.Added = append(res.Added, added...)
	sort.Strings(res.Added)
	dirty, err := repo.Dirty()
	if err != nil {
		return nil, err
	}
	if !dirty && repo.HasHead() {
		return res, nil
	}
	if err := repo.AddAll(); err != nil {
		return nil, err
	}
	res.OperationID = NewOperationID("setup", now)
	what := fmt.Sprintf("adopt knowledge base %s", cfg.Name)
	switch {
	case res.WasLegacy:
		what = fmt.Sprintf("adopt claude-obsidian vault as knowledge base %s", cfg.Name)
	case res.FromV1:
		what = fmt.Sprintf("adopt v1 vault as knowledge base %s", cfg.Name)
	case res.FromV2:
		what = fmt.Sprintf("adopt v2 vault as knowledge base %s", cfg.Name)
	}
	if res.Repaired {
		what += "; repaired identity file"
	}
	sha, err := repo.Commit(CommitMessage("setup", what, res.OperationID))
	if err != nil {
		return nil, err
	}
	res.Commit = sha
	return res, nil
}

// legacyMode reads claude-obsidian's mode file when it names a mode we support.
func legacyMode(root string) Mode {
	data, err := os.ReadFile(filepath.Join(root, MetaDir, "mode.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		Mode string `json:"mode"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return ""
	}
	if m, err := ParseMode(doc.Mode); err == nil {
		return m
	}
	return ""
}
