// Package project knows what a v3 project is: a folder named atlas/ inside the user's
// work, holding an identity file, tasks, phases, and an inbox for task notes. A project
// is files. It has no git of its own, no Obsidian vault, and no engine; the tasks
// package writes its pages, and this package writes only the identity file.
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/vault"
)

const (
	// Dir is the folder a project lives in, under the work folder.
	Dir = "atlas"
	// Marker is the identity file inside Dir. It is visible because the folder is the
	// user's and the file says what the folder is.
	Marker = "project.json"
	Schema = "claude-atlas.project.v3"

	TasksDir   = "tasks"
	ArchiveDir = "tasks/archive"
	TasksIndex = "tasks/tasks.md"
	PhasesDir  = "phases"
	InboxDir   = "inbox"
)

// Folders are the folders every project holds, relative to Dir.
var Folders = []string{TasksDir, ArchiveDir, PhasesDir, InboxDir}

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

// Project is an opened project. Root is the work folder, the parent of atlas/.
type Project struct {
	Root   string
	Config Config
}

// Name is the project's name from its identity file.
func (p *Project) Name() string { return p.Config.Name }

// Atlas is the project's atlas/ folder.
func (p *Project) Atlas() string { return filepath.Join(p.Root, Dir) }

// Path joins an atlas-relative path onto the atlas/ folder.
func (p *Project) Path(rel string) string { return filepath.Join(p.Root, Dir, filepath.FromSlash(rel)) }

// MarkerPath is the identity file's path for the project at work.
func MarkerPath(work string) string { return filepath.Join(work, Dir, Marker) }

var ErrNotProject = errors.New("not a claude-atlas project")

// IsProject reports whether work holds atlas/project.json.
func IsProject(work string) bool {
	info, err := os.Stat(MarkerPath(work))
	return err == nil && info.Mode().IsRegular()
}

// ReadConfig parses the identity file without validating it. ok is false when there is
// none or it is not JSON.
func ReadConfig(work string) (Config, bool) {
	data, err := os.ReadFile(MarkerPath(work))
	if err != nil {
		return Config{}, false
	}
	var cfg Config
	if json.Unmarshal(data, &cfg) != nil {
		return Config{}, false
	}
	return cfg, true
}

// Open reads the project whose work folder is work.
func Open(work string) (*Project, error) {
	abs, err := filepath.Abs(work)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(MarkerPath(abs))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s has no %s/%s", ErrNotProject, abs, Dir, Marker)
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", MarkerPath(abs), err)
	}
	if cfg.Schema != Schema {
		return nil, fmt.Errorf("%s: unsupported schema %q", MarkerPath(abs), cfg.Schema)
	}
	if cfg.ID == "" {
		return nil, fmt.Errorf("%s has no id", MarkerPath(abs))
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = filepath.Base(abs)
	}
	if cfg.Knowledge != nil && cfg.Knowledge.ID == "" {
		cfg.Knowledge = nil
	}
	return &Project{Root: abs, Config: cfg}, nil
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
// project already, it holds an atlas/ folder that is something else, it sits inside
// another project, or it sits inside a knowledge base.
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
	if entries, err := os.ReadDir(filepath.Join(abs, Dir)); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s has an %s/ folder that is not a project; move it aside first", abs, Dir)
	}
	if outer := FindAbove(filepath.Dir(abs)); outer != "" {
		return fmt.Errorf("%s is inside the project %s; a project does not go inside another", abs, outer)
	}
	if outer := vault.FindAbove(abs); outer != "" {
		return fmt.Errorf("%s is inside the knowledge base %s; a project does not go inside one", abs, outer)
	}
	return nil
}

// Init makes the folder at work a project: atlas/ with the identity file, tasks/,
// tasks/archive/, phases/, and inbox/. It writes nothing outside atlas/ and never
// touches git. It returns the project and the atlas-relative paths it wrote.
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
	cfg := Config{Schema: Schema, ID: vault.NewID(), Name: name, Description: strings.TrimSpace(opts.Description), Created: now.Format("2006-01-02")}
	if opts.Knowledge != nil && opts.Knowledge.ID != "" {
		cfg.Knowledge = &Knowledge{ID: opts.Knowledge.ID, Name: opts.Knowledge.Name}
	}
	p := &Project{Root: abs, Config: cfg}
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
	return p, written, nil
}

// Save rewrites the identity file. It is how link, unlink, and edit change a project.
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
	return os.WriteFile(p.Path(Marker), p.Config.Encode(), 0o644)
}

// EnsureFolders creates the folders a project should hold but may lack, as after a
// clone that did not carry empty folders.
func (p *Project) EnsureFolders() error {
	for _, dir := range Folders {
		if err := os.MkdirAll(p.Path(dir), 0o755); err != nil {
			return err
		}
	}
	return nil
}
