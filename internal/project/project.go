// Package project knows what a v3 project is: a folder atlas/<name>/ inside the user's
// work, holding an identity file, threads with their stage documents, phases, and an
// inbox for notes. The folder takes the project's name so that Obsidian, which names a
// vault after its folder, tells one project from another. A project is files. It has no
// git of its own and no engine; the threads package writes its pages, and this package
// writes only the identity file, the folder's name, and the Obsidian snippet that colors
// the pages.
package project

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

const (
	// Dir is the folder under the work folder that holds the project's folder.
	Dir = "atlas"
	// Marker is the identity file inside the project's folder. It is visible because the
	// folder is the user's and the file says what the folder is.
	Marker = "project.json"
	Schema = "claude-atlas.project.v3"

	ThreadsDir   = "threads"
	ArchiveDir   = "threads/archive"
	ThreadsIndex = "threads/threads.md"
	StubsDir     = "stubs"
	SpecsDir     = "specs"
	PlansDir     = "plans"
	ReceiptsDir  = "receipts"
	PhasesDir    = "phases"
	InboxDir     = "inbox"

	// LegacyTasksDir held the task pages of 2.x; threads.Migrate turns them into threads.
	LegacyTasksDir = "tasks"
)

// Folders are the folders every project holds, relative to its folder.
var Folders = []string{ThreadsDir, ArchiveDir, StubsDir, SpecsDir, PlansDir, ReceiptsDir, PhasesDir, InboxDir}

//go:embed templates
var templates embed.FS

// Snippet is the CSS that gives each stage its callout color and icon when the project
// folder is open in Obsidian; Appearance enables it.
const (
	Snippet    = ".obsidian/snippets/claude-atlas.css"
	Appearance = ".obsidian/appearance.json"
)

func template(rel string) []byte {
	data, err := templates.ReadFile("templates/" + strings.TrimPrefix(rel, ".obsidian/"))
	if err != nil {
		panic(err)
	}
	return data
}

// WriteSnippet writes the current CSS snippet over the one there. It enables the snippet
// only when Obsidian has no appearance file yet, so a user who turned it off keeps it off.
func (p *Project) WriteSnippet() error {
	if err := os.MkdirAll(filepath.Dir(p.Path(Snippet)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p.Path(Snippet), template(Snippet), 0o644); err != nil {
		return err
	}
	// Obsidian rewrites its workspace files on every click; the work's repository
	// should track the snippet and not those.
	ignore := p.Path(".obsidian/.gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(ignore, []byte("workspace*.json\n"), 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Stat(p.Path(Appearance)); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(p.Path(Appearance), template(Appearance), 0o644)
	}
	return nil
}

// Knowledge names the one knowledge base a project uses. The id is the reference; the
// name is for people and for messages when the id is not found on this machine.
type Knowledge struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Config is the content of the identity file: the facts that travel with the project.
// It never holds a path.
type Config struct {
	Schema      string     `json:"schema"`
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Created     string     `json:"created"`
	Knowledge   *Knowledge `json:"knowledge,omitempty"`
}

// Encode renders the identity file.
func (c Config) Encode() []byte {
	data, _ := json.MarshalIndent(c, "", "  ")
	return append(data, '\n')
}

// Project is an opened project. Root is the work folder; Folder is the name of the
// project's folder under Root/atlas/.
type Project struct {
	Root   string
	Folder string
	Config Config
}

// Name is the project's name from its identity file.
func (p *Project) Name() string { return p.Config.Name }

// Atlas is the project's folder, atlas/<name>/.
func (p *Project) Atlas() string { return filepath.Join(p.Root, Dir, p.Folder) }

// Rel is the project's folder relative to the work folder, with slashes.
func (p *Project) Rel() string { return Dir + "/" + p.Folder }

// Path joins a path relative to the project's folder onto it.
func (p *Project) Path(rel string) string { return filepath.Join(p.Atlas(), filepath.FromSlash(rel)) }

// FolderName is the name of the folder a project with this name sits in: the name
// cleaned so Obsidian can name a vault after it. It is empty when nothing usable is left.
func FolderName(name string) string { return links.CleanName(strings.TrimSpace(name)) }

var (
	ErrNotProject = errors.New("not a claude-atlas project")
	// ErrFlat means the project sits directly in atlas/, as 2.2.0 and earlier made it;
	// Upgrade moves it into atlas/<name>/.
	ErrFlat = errors.New("the project sits directly in atlas/")
)

// Locate returns the name of the project's folder under work/atlas/: the one child that
// holds the identity file. It refuses a project in the flat layout and an atlas/ folder
// that holds more than one project.
func Locate(work string) (string, error) {
	atlas := filepath.Join(work, Dir)
	if isFile(filepath.Join(atlas, Marker)) {
		return "", fmt.Errorf("%w; run claude-atlas upgrade %s", ErrFlat, home.Display(work))
	}
	// A file named atlas, such as a binary, is not a project.
	if info, err := os.Stat(atlas); err == nil && !info.IsDir() {
		return "", fmt.Errorf("%w: %s has no %s/<name>/%s", ErrNotProject, work, Dir, Marker)
	}
	entries, err := os.ReadDir(atlas)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	var found []string
	for _, e := range entries {
		if isFile(filepath.Join(atlas, e.Name(), Marker)) {
			found = append(found, e.Name())
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("%w: %s has no %s/<name>/%s", ErrNotProject, work, Dir, Marker)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("%s holds %d projects (%s); a work folder holds one", atlas, len(found), strings.Join(found, ", "))
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// IsProject reports whether work holds a project's identity file under atlas/, in any
// layout; Open says whether it can be used.
func IsProject(work string) bool {
	_, err := Locate(work)
	return !errors.Is(err, ErrNotProject)
}

// ReadConfig parses the identity file without validating it. ok is false when there is
// none, the layout is not the current one, or it is not JSON.
func ReadConfig(work string) (Config, bool) {
	folder, err := Locate(work)
	if err != nil {
		return Config{}, false
	}
	cfg, err := readConfig(filepath.Join(work, Dir, folder, Marker))
	return cfg, err == nil
}

func readConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Open reads the project whose work folder is work.
func Open(work string) (*Project, error) {
	abs, err := filepath.Abs(work)
	if err != nil {
		return nil, err
	}
	folder, err := Locate(abs)
	if err != nil {
		return nil, err
	}
	marker := filepath.Join(abs, Dir, folder, Marker)
	cfg, err := readConfig(marker)
	if err != nil {
		return nil, err
	}
	if cfg.Schema != Schema {
		return nil, fmt.Errorf("%s: unsupported schema %q", marker, cfg.Schema)
	}
	if cfg.ID == "" {
		return nil, fmt.Errorf("%s has no id", marker)
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = folder
	}
	if cfg.Knowledge != nil && cfg.Knowledge.ID == "" {
		cfg.Knowledge = nil
	}
	return &Project{Root: abs, Folder: folder, Config: cfg}, nil
}

// FindAbove returns the work folder of the nearest project at or above start, or "".
// A session anywhere inside the work belongs to the project.
func FindAbove(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if IsProject(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Options say what to make. The name is the folder's by default.
type Options struct {
	Name        string
	Description string
	Knowledge   *Knowledge
}

// CheckNew says why a project cannot be made at work: the folder is not there, it is a
// project already, it sits inside another project, or it sits inside a knowledge base.
// An atlas/ folder that holds something else is fine; Init refuses only a taken
// atlas/<name>/.
func CheckNew(work string) error {
	abs, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("%s: not found", abs)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", abs)
	}
	if IsProject(abs) {
		return fmt.Errorf("%s is a project already", abs)
	}
	if outer := FindAbove(filepath.Dir(abs)); outer != "" {
		return fmt.Errorf("%s is inside the project %s; a project does not go inside another", abs, outer)
	}
	if outer := vault.FindAbove(abs); outer != "" {
		return fmt.Errorf("%s is inside the knowledge base %s; a project does not go inside one", abs, outer)
	}
	return nil
}

// Init makes the folder at work a project: atlas/<name>/ with the identity file, the
// folders, and the Obsidian snippet. It writes nothing outside that folder and never
// touches git. It returns the project and the paths it wrote, relative to the folder.
func Init(work string, opts Options, now time.Time) (*Project, []string, error) {
	abs, err := filepath.Abs(work)
	if err != nil {
		return nil, nil, err
	}
	if err := CheckNew(abs); err != nil {
		return nil, nil, err
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = filepath.Base(abs)
	}
	folder := FolderName(name)
	if folder == "" {
		return nil, nil, fmt.Errorf("%q leaves no usable folder name", name)
	}
	if entries, err := os.ReadDir(filepath.Join(abs, Dir, folder)); err == nil && len(entries) > 0 {
		return nil, nil, fmt.Errorf("%s/%s/ holds something that is not a project; move it aside or choose another name", Dir, folder)
	}
	cfg := Config{Schema: Schema, ID: vault.NewID(), Name: name, Description: strings.TrimSpace(opts.Description), Created: now.Format("2006-01-02")}
	if opts.Knowledge != nil && opts.Knowledge.ID != "" {
		cfg.Knowledge = &Knowledge{ID: opts.Knowledge.ID, Name: opts.Knowledge.Name}
	}
	p := &Project{Root: abs, Folder: folder, Config: cfg}
	var written []string
	for _, dir := range Folders {
		if err := os.MkdirAll(p.Path(dir), 0o755); err != nil {
			return nil, nil, err
		}
		written = append(written, dir+"/")
	}
	if err := os.WriteFile(p.Path(Marker), cfg.Encode(), 0o644); err != nil {
		return nil, nil, err
	}
	written = append(written, Marker)
	if err := p.WriteSnippet(); err != nil {
		return nil, nil, err
	}
	written = append(written, Snippet)
	return p, written, nil
}

// Save rewrites the identity file. It is how link, unlink, and edit change a project. A
// new name moves the project's folder to match, and a taken folder refuses the save.
func (p *Project) Save() error {
	if strings.TrimSpace(p.Config.Name) == "" {
		return errors.New("name must not be blank")
	}
	p.Config.Schema = Schema
	p.Config.Name = strings.TrimSpace(p.Config.Name)
	p.Config.Description = strings.TrimSpace(p.Config.Description)
	if p.Config.Knowledge != nil && p.Config.Knowledge.ID == "" {
		p.Config.Knowledge = nil
	}
	from := p.Folder
	if err := p.moveFolder(FolderName(p.Config.Name)); err != nil {
		return err
	}
	if err := os.WriteFile(p.Path(Marker), p.Config.Encode(), 0o644); err != nil {
		if p.Folder != from {
			// The name and the folder never disagree: put the folder back.
			os.Rename(p.Atlas(), filepath.Join(p.Root, Dir, from))
			p.Folder = from
		}
		return err
	}
	return nil
}

// moveFolder renames the project's folder under atlas/ to folder.
func (p *Project) moveFolder(folder string) error {
	if folder == "" {
		return fmt.Errorf("%q leaves no usable folder name", p.Config.Name)
	}
	if folder == p.Folder {
		return nil
	}
	target := filepath.Join(p.Root, Dir, folder)
	if taken, err := os.Stat(target); err == nil {
		// On a case-insensitive filesystem the project's own folder answers to the new
		// name already; renaming it to change its case is still a rename.
		here, err := os.Stat(p.Atlas())
		if err != nil || !os.SameFile(taken, here) {
			return fmt.Errorf("%s already exists; move it aside or choose another name", home.Display(target))
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(p.Atlas(), target); err != nil {
		return err
	}
	p.Folder = folder
	return nil
}

// Upgrade moves a project in the flat layout, atlas/project.json, into atlas/<name>/,
// with everything else atlas/ held. It reports whether it moved anything; a project in
// the current layout is left as it is.
func Upgrade(work string) (bool, error) {
	abs, err := filepath.Abs(work)
	if err != nil {
		return false, err
	}
	if _, err := Locate(abs); !errors.Is(err, ErrFlat) {
		return false, err
	}
	atlas := filepath.Join(abs, Dir)
	cfg, err := readConfig(filepath.Join(atlas, Marker))
	if err != nil {
		return false, err
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = filepath.Base(abs)
	}
	folder := FolderName(name)
	if folder == "" {
		return false, fmt.Errorf("%q leaves no usable folder name; set a name in %s first", name, filepath.Join(atlas, Marker))
	}
	// atlas/ moves aside whole and comes back as atlas/<name>/, so a project whose name
	// matches one of its own folders (stubs, inbox) moves as cleanly as any other.
	aside := filepath.Join(abs, "."+Dir+"-upgrade")
	if _, err := os.Lstat(aside); err == nil {
		return false, fmt.Errorf("%s exists; move it aside and run upgrade again", aside)
	}
	if err := os.Rename(atlas, aside); err != nil {
		return false, err
	}
	if err := os.Mkdir(atlas, 0o755); err != nil {
		os.Rename(aside, atlas)
		return false, err
	}
	if err := os.Rename(aside, filepath.Join(atlas, folder)); err != nil {
		os.Remove(atlas)
		os.Rename(aside, atlas)
		return false, err
	}
	return true, nil
}

// EnsureFolders creates the folders a project should hold but may lack, as after a
// clone that did not carry empty folders, and the snippet when there is none.
func (p *Project) EnsureFolders() error {
	for _, dir := range Folders {
		if err := os.MkdirAll(p.Path(dir), 0o755); err != nil {
			return err
		}
	}
	if _, err := os.Stat(p.Path(Snippet)); errors.Is(err, os.ErrNotExist) {
		return p.WriteSnippet()
	}
	return nil
}
