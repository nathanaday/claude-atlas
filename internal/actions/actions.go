// Package actions is every change to the atlas the view and the tools can make, as one
// struct of functions over the packages that own them. Bind builds it in one place; the
// CLI, the view, and the MCP server never bind a function a second time.
package actions

import (
	"time"

	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// AddKnowledge is what a caller chose for a new or adopted knowledge base.
type AddKnowledge struct {
	Name  string // display name as typed, or the folder's name when adopting
	Path  string // where the vault will be created, or the vault being adopted
	Mode  string // generic or lyt
	Scope string
	Adopt bool // Path exists already and is adopted rather than created
}

// InitProject is what a caller chose for a new project.
type InitProject struct {
	Work        string // the folder that becomes the project
	Name        string
	Description string
	Knowledge   string // a knowledge base by name, id, or path; "" for none
	NoGit       bool   // leave a work folder that is in no repository without one
}

// Atlas is every action the CLI, the view, and the tools reach. Each field is one
// function from the package that owns the action, bound to the atlas home and its config.
type Atlas struct {
	// Load reads the registry, refreshing it first when no refresh has run yet. Scan
	// reads everything afresh, with its state derived, and writes nothing. Refresh reads
	// everything again and rewrites the registry.
	Load    func() ([]registry.Entry, error)
	Scan    func() (*registry.Index, error)
	Refresh func() (*registry.Index, error)
	// CreateKnowledge makes or adopts a knowledge base and registers it; it returns the
	// vault's path. EditKnowledge changes its identity and returns the path it sits at
	// afterwards; a rename moves the folder. ForgetKnowledge forgets a knowledge base the
	// config names; the folder stays.
	CreateKnowledge func(AddKnowledge) (string, error)
	EditKnowledge   func(registry.Entry, vaults.Edit) (string, error)
	ForgetKnowledge func(registry.Entry) error
	// The project calls: init a folder, set or clear its knowledge base, change its name
	// or description, forget it.
	InitProject   func(InitProject) (*vaults.ProjectInit, error)
	LinkProject   func(registry.Entry, string) (*registry.Entry, error)
	UnlinkProject func(registry.Entry) error
	EditProject   func(registry.Entry, vaults.ProjectEdit) error
	ForgetProject func(registry.Entry) error
	// StagePlan says which files under the sources are new to a knowledge base; no
	// sources means the folders it staged from before. Stage copies a plan's files into
	// the inbox and reports the folders the vault now remembers. Sources lists them.
	StagePlan func(registry.Entry, []string) (*capture.StagePlan, error)
	Stage     func(registry.Entry, *capture.StagePlan) (*capture.StageResult, []string, error)
	Sources   func(registry.Entry) []string
	// StageProject writes a snapshot of a project into the inbox of its knowledge base,
	// for the describe skill to ingest.
	StageProject func(registry.Entry) (*capture.ProjectStage, error)
	// The task calls, on a project: read the board and the notes waiting in its inbox,
	// plant a task, change one, and the phase calls.
	Tasks       func(registry.Entry) (*tasks.Board, []string, error)
	Plant       func(registry.Entry, tasks.Plant) (*tasks.Task, error)
	SetTask     func(registry.Entry, string, tasks.Changes) (*tasks.Task, error)
	AddPhase    func(registry.Entry, string, string, *int) (*tasks.Phase, error)
	RenamePhase func(registry.Entry, string, string) (*tasks.Phase, error)
	OrderPhase  func(registry.Entry, string, int) (*tasks.Phase, error)
	RemovePhase func(registry.Entry, string) error
}

// Bind builds the struct over an atlas home and its loaded config. The console is for
// CreateKnowledge's preview when a caller wants one; nil and the view pass none.
func Bind(h home.Home, cfg *home.Config, c *console.Console) Atlas {
	openProject := func(en registry.Entry) (*project.Project, error) { return project.Open(en.Path) }
	return Atlas{
		Load: func() ([]registry.Entry, error) { return refresh.Entries(h, cfg, time.Now()) },
		Scan: func() (*registry.Index, error) { return refresh.Derived(cfg, time.Now()) },
		Refresh: func() (*registry.Index, error) {
			_, ix, err := refresh.All(h, cfg, time.Now())
			return ix, err
		},
		CreateKnowledge: func(choice AddKnowledge) (string, error) { return createOrAdopt(h, cfg, c, choice) },
		EditKnowledge: func(en registry.Entry, edit vaults.Edit) (string, error) {
			return vaults.EditIdentity(h, cfg, en, edit, time.Now())
		},
		ForgetKnowledge: func(en registry.Entry) error { return vaults.Unregister(h, cfg, en.Path) },
		InitProject: func(choice InitProject) (*vaults.ProjectInit, error) {
			opts := project.Options{Name: choice.Name, Description: choice.Description}
			return vaults.InitProject(h, cfg, choice.Work, opts, choice.Knowledge, !choice.NoGit, time.Now())
		},
		LinkProject: func(en registry.Entry, knowledge string) (*registry.Entry, error) {
			p, err := openProject(en)
			if err != nil {
				return nil, err
			}
			return vaults.LinkKnowledge(cfg, p, knowledge)
		},
		UnlinkProject: func(en registry.Entry) error {
			p, err := openProject(en)
			if err != nil {
				return err
			}
			return vaults.UnlinkKnowledge(p)
		},
		EditProject: func(en registry.Entry, edit vaults.ProjectEdit) error {
			p, err := openProject(en)
			if err != nil {
				return err
			}
			return vaults.EditProject(p, edit)
		},
		ForgetProject: func(en registry.Entry) error { return vaults.ForgetProject(h, cfg, en.Path) },
		StagePlan: func(en registry.Entry, given []string) (*capture.StagePlan, error) {
			v, err := vault.Open(en.Path)
			if err != nil {
				return nil, err
			}
			sources, err := capture.SourcesFor(v, given)
			if err != nil {
				return nil, err
			}
			return capture.PlanStage(v, sources, time.Now())
		},
		Stage: func(en registry.Entry, plan *capture.StagePlan) (*capture.StageResult, []string, error) {
			v, err := vault.Open(en.Path)
			if err != nil {
				return nil, nil, err
			}
			res, err := capture.ApplyStage(v, plan, time.Now())
			if err != nil {
				return res, nil, err
			}
			return res, res.Remembered, nil
		},
		Sources: func(en registry.Entry) []string {
			v, err := vault.Open(en.Path)
			if err != nil {
				return nil
			}
			return capture.Sources(v)
		},
		StageProject: func(en registry.Entry) (*capture.ProjectStage, error) {
			kb := en.KnowledgePath()
			if kb == "" {
				return nil, errNoKnowledge(en)
			}
			v, err := vault.Open(kb)
			if err != nil {
				return nil, err
			}
			return capture.StageProject(v, en, time.Now())
		},
		Tasks: func(en registry.Entry) (*tasks.Board, []string, error) {
			p, err := openProject(en)
			if err != nil {
				return nil, nil, err
			}
			board, err := tasks.Load(p)
			if err != nil {
				return nil, nil, err
			}
			return board, tasks.Notes(p), nil
		},
		Plant: func(en registry.Entry, plant tasks.Plant) (*tasks.Task, error) {
			p, err := openProject(en)
			if err != nil {
				return nil, err
			}
			return tasks.PlantTask(p, plant, time.Now())
		},
		SetTask: func(en registry.Entry, id string, ch tasks.Changes) (*tasks.Task, error) {
			p, err := openProject(en)
			if err != nil {
				return nil, err
			}
			return tasks.Set(p, id, ch, time.Now())
		},
		AddPhase: func(en registry.Entry, title, goal string, order *int) (*tasks.Phase, error) {
			p, err := openProject(en)
			if err != nil {
				return nil, err
			}
			return tasks.CreatePhase(p, title, goal, order, time.Now())
		},
		RenamePhase: func(en registry.Entry, old, title string) (*tasks.Phase, error) {
			p, err := openProject(en)
			if err != nil {
				return nil, err
			}
			return tasks.RenamePhase(p, old, title, time.Now())
		},
		OrderPhase: func(en registry.Entry, title string, order int) (*tasks.Phase, error) {
			p, err := openProject(en)
			if err != nil {
				return nil, err
			}
			return tasks.ReorderPhase(p, title, order, time.Now())
		},
		RemovePhase: func(en registry.Entry, title string) error {
			p, err := openProject(en)
			if err != nil {
				return err
			}
			return tasks.RemovePhase(p, title, time.Now())
		},
	}
}
