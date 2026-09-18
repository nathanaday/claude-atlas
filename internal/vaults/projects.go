package vaults

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

// Git says what InitProject found or did about the work's repository.
type Git string

const (
	GitCreated  Git = "created"  // init made the work a repository
	GitExisting Git = "existing" // the work is a repository already
	GitEnclosed Git = "enclosed" // the work sits inside another repository, which holds its history
	GitSkipped  Git = "skipped"  // the caller asked for no repository
)

// ProjectInit is what InitProject made.
type ProjectInit struct {
	Project *project.Project
	Written []string // what it wrote under atlas/
	Git     Git
}

// InitProject makes work a project and lists it in the config. knowledge, when given,
// names the knowledge base the project uses; it must be one the scan knows. With git,
// a work folder that is in no repository becomes one, with no commit.
func InitProject(h home.Home, cfg *home.Config, work string, opts project.Options, knowledge string, git bool, now time.Time) (*ProjectInit, error) {
	abs, err := filepath.Abs(home.Expand(work))
	if err != nil {
		return nil, err
	}
	if knowledge != "" {
		kb, err := findKnowledge(cfg, knowledge)
		if err != nil {
			return nil, err
		}
		opts.Knowledge = &project.Knowledge{ID: kb.ID, Name: kb.Name}
	}
	if err := project.CheckNew(abs); err != nil {
		return nil, err
	}
	res := &ProjectInit{Git: GitSkipped}
	repo := gitx.Repo{Dir: abs}
	switch {
	case !git:
	case repo.IsRepo():
		res.Git = GitExisting
	case repo.InsideOtherRepo():
		res.Git = GitEnclosed
	default:
		if err := repo.Init(); err != nil {
			return nil, fmt.Errorf("git init %s: %w", abs, err)
		}
		res.Git = GitCreated
	}
	res.Project, res.Written, err = project.Init(abs, opts, now)
	if err != nil {
		if res.Git == GitCreated {
			os.RemoveAll(filepath.Join(abs, ".git"))
		}
		return nil, err
	}
	if cfg.AddProject(abs) {
		if err := h.Save(cfg); err != nil {
			return res, err
		}
	}
	return res, nil
}

// findKnowledge resolves a knowledge base by name, id, or path against a fresh scan.
func findKnowledge(cfg *home.Config, arg string) (*registry.Entry, error) {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return nil, err
	}
	kb, err := ix.Find(arg, registry.Knowledge)
	if err != nil {
		return nil, err
	}
	return kb, nil
}

// LinkKnowledge sets the knowledge base a project uses.
func LinkKnowledge(cfg *home.Config, p *project.Project, knowledge string) (*registry.Entry, error) {
	kb, err := findKnowledge(cfg, knowledge)
	if err != nil {
		return nil, err
	}
	p.Config.Knowledge = &project.Knowledge{ID: kb.ID, Name: kb.Name}
	return kb, p.Save()
}

// UnlinkKnowledge clears the knowledge base a project uses.
func UnlinkKnowledge(p *project.Project) error {
	if p.Config.Knowledge == nil {
		return errors.New(p.Name() + " uses no knowledge base")
	}
	p.Config.Knowledge = nil
	return p.Save()
}

// ProjectEdit changes a project's own facts. Nil means unchanged.
type ProjectEdit struct {
	Name        string
	Description *string
}

// EditProject rewrites a project's identity file.
func EditProject(p *project.Project, edit ProjectEdit) error {
	if edit.Name != "" {
		p.Config.Name = edit.Name
	}
	if edit.Description != nil {
		p.Config.Description = *edit.Description
	}
	return p.Save()
}

// ForgetProject drops a work folder from the config. The folder and its atlas/ stay.
func ForgetProject(h home.Home, cfg *home.Config, work string) error {
	abs, err := filepath.Abs(home.Expand(work))
	if err != nil {
		return err
	}
	if !cfg.RemoveProject(abs) {
		return fmt.Errorf("%s is not a registered project", home.Display(abs))
	}
	return h.Save(cfg)
}

// Heal is what Register did to the config for a project a session started in.
type Heal string

const (
	HealNone  Heal = ""      // the config listed the project at this path already
	HealMoved Heal = "moved" // the config listed the project's id at another path
	HealAdded Heal = "added" // the config did not list the project
)

// RegisterProject makes sure the config lists the project at p.Root: it adds one the
// config does not know, as after a clone, and moves one whose id the config knows at
// another path, as after the work folder moved. A listed folder that is gone is taken
// for this project's old home when it is the only one gone; otherwise it stays until
// forget or a later heal, because it may be another project on a drive that is not
// mounted. It reports what it did.
func RegisterProject(h home.Home, cfg *home.Config, p *project.Project) (Heal, error) {
	if cfg.HasProject(p.Root) {
		return HealNone, nil
	}
	heal := HealAdded
	var gone []string
	for _, work := range append([]string(nil), cfg.Projects...) {
		if other, ok := project.ReadConfig(work); ok {
			if other.ID == p.Config.ID {
				cfg.RemoveProject(work)
				heal = HealMoved
			}
			continue
		}
		if _, err := os.Stat(work); err != nil {
			gone = append(gone, work)
		}
	}
	if heal == HealAdded && len(gone) == 1 {
		cfg.RemoveProject(gone[0])
		heal = HealMoved
	}
	cfg.AddProject(p.Root)
	return heal, h.Save(cfg)
}
