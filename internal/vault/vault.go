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
	Schema       = "claude-atlas.vault.v2"
	// SchemaV1 is the identity file older versions wrote; adopt rewrites it.
	SchemaV1 = "claude-atlas.vault.v1"
	// EnvVault names the vault explicitly for the MCP server and hooks.
	EnvVault = "CLAUDE_ATLAS_VAULT"

	MetaDir      = ".vault-meta"
	InboxDir     = "inbox"
	RawDir       = ".raw"
	CapturedDir  = ".raw/captured"
	WikiDir      = "wiki"
	LogPage      = "wiki/log.md"
	HotPage      = "wiki/hot.md"
	IndexPage    = "wiki/index.md"
	OverviewPage = "wiki/overview.md"
	LedgerPath   = "wiki/meta/ledgers/source-ledger.json"

	// Tasks: open task pages, their archive, the generated index, the derived ledger,
	// the inbox folder for task notes, and the user's scratch space.
	TasksDir       = "wiki/tasks"
	TaskArchiveDir = "wiki/tasks/archive"
	TasksIndex     = "wiki/tasks/tasks.md"
	TaskLedgerPath = "wiki/meta/ledgers/task-ledger.json"
	InboxTasksDir  = "inbox/tasks"
	IdeasDir       = "ideas"

	// A folder's index page takes the folder's name, so no page shares the basename of
	// wiki/index.md. LegacyTasksIndex is where older versions wrote the task index.
	CanvasIndex      = "wiki/canvases/canvases.md"
	LegacyTasksIndex = "wiki/tasks/index.md"
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

// Kind says what a vault is for. A knowledge base holds sources, entities, and concepts.
// A project holds tasks, questions, and notes, and mounts knowledge bases.
type Kind string

const (
	Knowledge Kind = "knowledge"
	Project   Kind = "project"
)

var Kinds = []Kind{Knowledge, Project}

// ParseKind validates a kind name.
func ParseKind(s string) (Kind, error) {
	for _, k := range Kinds {
		if string(k) == s {
			return k, nil
		}
	}
	return "", fmt.Errorf("kind must be knowledge or project, not %q", s)
}

// Access levels. A knowledge base is open or guarded; a mount and a grant are read or write.
const (
	AccessOpen    = "open"
	AccessGuarded = "guarded"
	AccessRead    = "read"
	AccessWrite   = "write"
)

// Grant is a knowledge base's word on one project.
type Grant struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Access string `json:"access"`
}

// Mount is a project's use of one knowledge base.
type Mount struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Access string `json:"access"`
}

// Repo is a repository a project works in.
type Repo struct {
	Name    string `json:"name"`
	Remote  string `json:"remote,omitempty"`
	Changes string `json:"changes,omitempty"`
}

// Config is the content of the identity file: the facts that travel with the vault. It
// never holds a path; paths are facts about one machine and live in the atlas config.
type Config struct {
	Schema  string `json:"schema"`
	ID      string `json:"id"`
	Kind    Kind   `json:"kind"`
	Name    string `json:"name"`
	Mode    Mode   `json:"mode"`
	Created string `json:"created"`
	// A knowledge base's fields.
	Scope  string  `json:"scope,omitempty"`
	Access string  `json:"access,omitempty"`
	Grants []Grant `json:"grants,omitempty"`
	// A project's fields.
	Tags   []string `json:"tags,omitempty"`
	Mounts []Mount  `json:"mounts,omitempty"`
	Repos  []Repo   `json:"repos,omitempty"`
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
func (v *Vault) Repo() gitx.Repo { return gitx.Repo{Dir: v.Root} }

var ErrNotVault = errors.New("not a claude-atlas vault")

// ErrV1 means the identity file is from before v2; adopt rewrites it.
var ErrV1 = errors.New("v1 vault")

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
func IsVault(root string) bool {
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
	case SchemaV1:
		return nil, fmt.Errorf("%w: %s was made by claude-atlas v1; run `claude-atlas adopt %s --as knowledge` or `--as project`", ErrV1, abs, abs)
	default:
		return nil, fmt.Errorf("%s: unsupported schema %q", marker, cfg.Schema)
	}
	if _, err := ParseKind(string(cfg.Kind)); err != nil {
		return nil, fmt.Errorf("%s: %w", marker, err)
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

// TemplateFiles lists the vault-relative paths the template provides for a kind: the
// files every vault has, then the kind's own. A kind's file wins over a common one.
func TemplateFiles(kind Kind) []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range templateDirs(kind) {
		fs.WalkDir(templates, dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				rel := strings.TrimPrefix(path, dir+"/")
				if !seen[rel] {
					seen[rel] = true
					out = append(out, rel)
				}
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

// templateDirs are the embedded folders a kind draws on, the kind's own first.
func templateDirs(kind Kind) []string {
	return []string{"templates/" + string(kind), "templates/common"}
}

func renderTemplate(kind Kind, rel string, now time.Time) ([]byte, error) {
	var data []byte
	var err error
	for _, dir := range templateDirs(kind) {
		if data, err = templates.ReadFile(dir + "/" + rel); err == nil {
			break
		}
	}
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

// Options say what to make: the kind (required), the mode (generic by default), and the
// name (the directory's by default).
type Options struct {
	Kind Kind
	Mode Mode
	Name string
}

// newConfig is the identity file of a vault made now.
func newConfig(root string, opts Options, now time.Time) (Config, error) {
	kind, err := ParseKind(string(opts.Kind))
	if err != nil {
		return Config{}, fmt.Errorf("kind is required: %w", err)
	}
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
	cfg := Config{Schema: Schema, ID: NewID(), Kind: kind, Name: name, Mode: mode, Created: now.Format("2006-01-02")}
	if kind == Knowledge {
		cfg.Access = AccessOpen
	}
	return cfg, nil
}

// Init creates a vault at root: the template, the identity file, an empty source ledger,
// and a git repository with one commit. root must not exist or must be an empty directory.
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
	if entries, err := os.ReadDir(abs); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("%s already exists and is not empty; use `claude-atlas adopt` for an existing vault", abs)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	repo := gitx.Repo{Dir: abs}
	if repo.InsideOtherRepo() {
		return nil, fmt.Errorf("%s is inside another git repository; a vault keeps its own history", abs)
	}
	files, err := writeMissing(abs, cfg, now, true)
	if err != nil {
		return nil, err
	}
	if err := repo.Init(); err != nil {
		return nil, err
	}
	if err := repo.AddAll(); err != nil {
		return nil, err
	}
	id := NewOperationID("setup", now)
	sha, err := repo.Commit(CommitMessage("setup", fmt.Sprintf("initialize %s %s (%s mode)", cfg.Kind, cfg.Name, cfg.Mode), id))
	if err != nil {
		return nil, err
	}
	return &InitResult{Root: abs, OperationID: id, Commit: sha, Files: files}, nil
}

// writeMissing writes template, identity, and ledger files that do not exist yet.
// With overwrite set (a fresh init) every file is written.
func writeMissing(root string, cfg Config, now time.Time, overwrite bool) ([]string, error) {
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
	for _, rel := range TemplateFiles(cfg.Kind) {
		data, err := renderTemplate(cfg.Kind, rel, now)
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
	if cfg.Kind == Project {
		if err := put(TaskLedgerPath, []byte(EmptyTaskLedger)); err != nil {
			return nil, err
		}
	}
	sort.Strings(written)
	return written, nil
}

// EmptyTaskLedger is the task ledger of a vault with no tasks; the tasks package owns
// the full format.
const EmptyTaskLedger = "{\n  \"schema\": \"claude-atlas.task-ledger.v1\",\n  \"tasks\": []\n}\n"

// Move is a file an older version wrote at a path the layout has since changed.
type Move struct {
	From string
	To   string
}

var legacyPaths = []Move{{From: LegacyTasksIndex, To: TasksIndex}}

// moveLegacy puts the files an older version wrote at their current paths. When the
// current path already exists, the file at the old path is a stale copy and goes, but
// only if git holds it unchanged; otherwise it may be the user's and stays.
func moveLegacy(root string) ([]Move, error) {
	repo := gitx.Repo{Dir: root}
	var moved []Move
	for _, m := range legacyPaths {
		from, to := filepath.Join(root, filepath.FromSlash(m.From)), filepath.Join(root, filepath.FromSlash(m.To))
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if _, err := os.Stat(to); err != nil {
			if err := os.Rename(from, to); err != nil {
				return moved, err
			}
			moved = append(moved, m)
			continue
		}
		if !committed(repo, m.From) {
			continue
		}
		if err := os.Remove(from); err != nil {
			return moved, err
		}
		moved = append(moved, m)
	}
	return moved, nil
}

// committed reports whether HEAD holds rel and the working tree has not changed it since.
func committed(repo gitx.Repo, rel string) bool {
	if !repo.Tracked(rel) {
		return false
	}
	entries, err := repo.Status()
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Path == rel {
			return false
		}
	}
	return true
}

// UpgradeResult reports what Upgrade changed; an empty result means the vault was current.
type UpgradeResult struct {
	Added []string
	Moved []Move
}

// Upgrade brings a vault made by an older version to the current layout, as one commit:
// it moves the files whose path changed and adds the template files the vault lacks.
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
	if res.Moved, err = moveLegacy(abs); err != nil {
		return res, err
	}
	if res.Added, err = writeMissing(abs, v.Config, now, false); err != nil {
		return res, err
	}
	if len(res.Added)+len(res.Moved) == 0 {
		return res, nil
	}
	repo := gitx.Repo{Dir: abs}
	stage := append([]string{}, res.Added...)
	var what []string
	if len(res.Added) > 0 {
		what = append(what, "add "+strings.Join(res.Added, ", "))
	}
	for _, m := range res.Moved {
		stage = append(stage, m.To)
		if repo.Tracked(m.From) {
			stage = append(stage, m.From)
		}
		what = append(what, fmt.Sprintf("move %s to %s", m.From, m.To))
	}
	if err := repo.Add(stage...); err != nil {
		return res, err
	}
	if _, err := repo.Commit(CommitMessage("setup", strings.Join(what, "; "), NewOperationID("setup", now))); err != nil {
		return res, err
	}
	return res, nil
}

// Ignore adds a pattern to the vault's .gitignore and commits it, so a repository
// mounted inside the vault keeps its own history apart from the vault's. It reports
// whether the file changed.
func Ignore(root, pattern string, now time.Time) (bool, error) {
	path := filepath.Join(root, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == pattern {
			return false, nil
		}
	}
	var b bytes.Buffer
	b.Write(existing)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		b.WriteString("\n")
	}
	b.WriteString("\n# a repository mounted in the vault; it keeps its own history\n" + pattern + "\n")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return false, err
	}
	repo := gitx.Repo{Dir: root}
	if !repo.IsRepo() {
		return true, nil
	}
	if err := repo.Add(".gitignore"); err != nil {
		return true, err
	}
	_, err = repo.Commit(CommitMessage("setup", "ignore the mounted repository "+pattern, NewOperationID("setup", now)))
	return true, err
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

// mergeSettings applies merge to an existing Obsidian settings file, keeping every other
// setting, and writes the template when there is none. It reports whether the file changed.
func mergeSettings(root, rel string, template []byte, merge func(map[string]any) bool) (bool, error) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, writeFile(root, rel, template)
	}
	if err != nil {
		return false, err
	}
	var settings map[string]any
	if err := json.Unmarshal(existing, &settings); err != nil || settings == nil {
		settings = map[string]any{}
	}
	if !merge(settings) {
		return false, nil
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(data, '\n'), 0o644)
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
	Kind           Kind
	OperationID    string
	Commit         string
	Added          []string
	Moved          []Move
	GitInitialized bool
	WasLegacy      bool
	AlreadyAdopted bool
	// FromV1 is set when a v1 identity file was rewritten.
	FromV1 bool
}

// adoptConfig decides the identity file adopt writes: a current one is kept, a v1 one is
// rewritten with its mode and creation date, and a vault without one gets a new one.
// Without a kind, adopt keeps the vault's kind or makes it a project.
func adoptConfig(root string, existing Config, hasMarker bool, opts Options, now time.Time) (Config, error) {
	if hasMarker && existing.Schema == Schema {
		if opts.Kind != "" && opts.Kind != existing.Kind {
			return Config{}, fmt.Errorf("%s is already a %s; a vault's kind does not change", root, existing.Kind)
		}
		return existing, nil
	}
	if opts.Kind == "" {
		opts.Kind = Project
	}
	if opts.Mode == "" {
		opts.Mode = existing.Mode
	}
	if opts.Mode == "" && IsLegacy(root) {
		opts.Mode = legacyMode(root)
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
// vault into a claude-atlas vault of a kind. It adds only what is missing and commits a
// baseline that includes every file already there. It never replaces or removes a file,
// except that a v1 identity file is rewritten and files an older version put at other
// paths are moved.
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
		return nil, fmt.Errorf("%s is not a vault: it has no .obsidian/, wiki/, or vault identity file; create one with `claude-atlas new-vault`", abs)
	}
	existing, hasMarker := ReadConfig(abs)
	res := &AdoptResult{Root: abs, WasLegacy: IsLegacy(abs), AlreadyAdopted: hasMarker && existing.Schema == Schema, FromV1: hasMarker && existing.Schema != Schema}
	cfg, err := adoptConfig(abs, existing, hasMarker, opts, now)
	if err != nil {
		return nil, err
	}
	res.Kind = cfg.Kind
	repo := gitx.Repo{Dir: abs}
	if repo.InsideOtherRepo() {
		return nil, fmt.Errorf("%s is inside another git repository; a vault keeps its own history", abs)
	}
	if res.AlreadyAdopted {
		if res.Moved, err = moveLegacy(abs); err != nil {
			return nil, err
		}
	}
	if res.FromV1 {
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
	if !repo.IsRepo() {
		if err := repo.Init(); err != nil {
			return nil, err
		}
		res.GitInitialized = true
	}
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
	what := fmt.Sprintf("adopt %s %s", cfg.Kind, cfg.Name)
	switch {
	case res.WasLegacy:
		what = fmt.Sprintf("adopt claude-obsidian vault as %s %s", cfg.Kind, cfg.Name)
	case res.FromV1:
		what = fmt.Sprintf("adopt v1 vault as %s %s", cfg.Kind, cfg.Name)
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
