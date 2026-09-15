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
	Schema       = "claude-atlas.vault.v1"
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

// Config is the content of the identity file.
type Config struct {
	Schema  string `json:"schema"`
	Mode    Mode   `json:"mode"`
	Created string `json:"created"`
}

// Encode renders the identity file.
func (c Config) Encode() []byte {
	data, _ := json.MarshalIndent(c, "", "  ")
	return append(data, '\n')
}

// Vault is an opened claude-atlas vault.
type Vault struct {
	Root   string
	Config Config
}

// Name is the directory name, which Obsidian shows as the vault name.
func (v *Vault) Name() string { return filepath.Base(v.Root) }

// Path joins a vault-relative path onto the root.
func (v *Vault) Path(rel string) string { return filepath.Join(v.Root, filepath.FromSlash(rel)) }

// Repo is the vault's git repository.
func (v *Vault) Repo() gitx.Repo { return gitx.Repo{Dir: v.Root} }

var ErrNotVault = errors.New("not a claude-atlas vault")

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
	if cfg.Schema != Schema {
		return nil, fmt.Errorf("%s: unsupported schema %q", filepath.Join(abs, Marker), cfg.Schema)
	}
	if cfg.Mode == "" {
		cfg.Mode = Generic
	}
	if _, err := ParseMode(string(cfg.Mode)); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(abs, Marker), err)
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

// TemplateFiles lists the vault-relative paths the template provides.
func TemplateFiles() []string {
	var out []string
	fs.WalkDir(templates, "templates", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, strings.TrimPrefix(path, "templates/"))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func renderTemplate(rel string, now time.Time) ([]byte, error) {
	data, err := templates.ReadFile("templates/" + rel)
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

// Init creates a vault at root: the template, the identity file, an empty source ledger,
// and a git repository with one commit. root must not exist or must be an empty directory.
func Init(root string, mode Mode, now time.Time) (*InitResult, error) {
	if err := requireGit(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
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
	files, err := writeMissing(abs, mode, now, true)
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
	sha, err := repo.Commit(CommitMessage("setup", fmt.Sprintf("initialize vault (%s mode)", mode), id))
	if err != nil {
		return nil, err
	}
	return &InitResult{Root: abs, OperationID: id, Commit: sha, Files: files}, nil
}

// writeMissing writes template, identity, and ledger files that do not exist yet.
// With overwrite set (a fresh init) every file is written.
func writeMissing(root string, mode Mode, now time.Time, overwrite bool) ([]string, error) {
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
		if rel == AppearanceFile && !overwrite {
			changed, err := mergeAppearance(root, data)
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
	if err := put(Marker, Config{Schema: Schema, Mode: mode, Created: now.Format("2006-01-02")}.Encode()); err != nil {
		return nil, err
	}
	if err := put(LedgerPath, ledger.Empty(now).Encode()); err != nil {
		return nil, err
	}
	if err := put(TaskLedgerPath, []byte(EmptyTaskLedger)); err != nil {
		return nil, err
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
	if res.Added, err = writeMissing(abs, v.Config.Mode, now, false); err != nil {
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

// SnippetName is the vault's own CSS snippet, .obsidian/snippets/claude-atlas.css.
const SnippetName = "claude-atlas"

// mergeAppearance enables the vault's snippet in an existing appearance file, keeping
// every other setting, and writes the template when there is none. It reports whether
// the file changed.
func mergeAppearance(root string, template []byte) (bool, error) {
	path := filepath.Join(root, filepath.FromSlash(AppearanceFile))
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, writeFile(root, AppearanceFile, template)
	}
	if err != nil {
		return false, err
	}
	var settings map[string]any
	if err := json.Unmarshal(existing, &settings); err != nil || settings == nil {
		settings = map[string]any{}
	}
	var enabled []any
	if list, ok := settings["enabledCssSnippets"].([]any); ok {
		enabled = list
	}
	for _, item := range enabled {
		if item == SnippetName {
			return false, nil
		}
	}
	settings["enabledCssSnippets"] = append(enabled, SnippetName)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, append(data, '\n'), 0o644)
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
	Moved          []Move
	GitInitialized bool
	WasLegacy      bool
	AlreadyAdopted bool
}

// Adopt turns an existing directory, an Obsidian vault, or a claude-obsidian vault into
// a claude-atlas vault. It adds only what is missing and commits a baseline that includes
// every file already there. It never replaces or removes a file, except that a vault an
// older claude-atlas made has its files moved to their current paths.
func Adopt(root string, mode Mode, now time.Time) (*AdoptResult, error) {
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
	res := &AdoptResult{Root: abs, WasLegacy: IsLegacy(abs), AlreadyAdopted: IsVault(abs)}
	if res.WasLegacy && mode == "" {
		mode = legacyMode(abs)
	}
	if mode == "" {
		mode = Generic
	}
	repo := gitx.Repo{Dir: abs}
	if repo.InsideOtherRepo() {
		return nil, fmt.Errorf("%s is inside another git repository; a vault keeps its own history", abs)
	}
	if res.AlreadyAdopted {
		if res.Moved, err = moveLegacy(abs); err != nil {
			return nil, err
		}
	}
	added, err := writeMissing(abs, mode, now, false)
	if err != nil {
		return nil, err
	}
	res.Added = added
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
	what := "adopt vault"
	if res.WasLegacy {
		what = "adopt claude-obsidian vault"
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
