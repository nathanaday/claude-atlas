// Package actions is every change to the atlas the view and the tools can make, as one
// struct of functions over the packages that own them. Bind builds it in one place; the
// CLI, the view, and the MCP server never bind a function a second time.
package actions

import (
	"time"

	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// AddVault is what a caller chose for a new or adopted vault.
type AddVault struct {
	Kind  vault.Kind
	Name  string // display name as typed, or the folder's name when adopting
	Path  string // where the vault will be created, or the vault being adopted
	Mode  string // generic or lyt
	Tags  []string
	Scope string
	// Access is a knowledge base's access, open or guarded; "" keeps the template's open.
	Access string
	// InRepo is a repository's top level; the project goes to REPO/atlas and Path is ignored.
	InRepo string
	Adopt  bool // Path exists already and is adopted rather than created
	// MountID is the knowledge base a new project mounts once it exists; "" mounts none.
	// The mount asks for write, and the mounts screen changes that.
	MountID string
	// Cluster is set when the user asked for a knowledge base that gathers others, and
	// MemberIDs are the knowledge bases it gathers. The vault is an ordinary knowledge
	// base until it has a member.
	Cluster   bool
	MemberIDs []string
}

// Atlas is every action the view and the tools reach. Each field is one function from
// the package that owns the action, bound to the atlas home and its config.
type Atlas struct {
	// Load reads the registry, refreshing it first when no refresh has run yet. Scan
	// reads every vault afresh, with its state derived, and writes nothing.
	Load func() ([]registry.Entry, error)
	Scan func() (*registry.Index, error)
	// Create makes or adopts a vault and registers it; it returns the vault's path.
	Create func(AddVault) (string, error)
	// Refresh reads every vault again, rewrites the registry, and repairs each project's
	// local state; it returns the index and what each project needed.
	Refresh func() (*registry.Index, []refresh.ProjectChange, error)
	// Edit changes a vault's identity and returns the path it sits at afterwards; a
	// rename moves the folder, so that path may not be the one it was given. Unregister
	// forgets a vault the config names; the folder stays, and a vault inside the vaults
	// directory cannot be forgotten.
	Edit       func(registry.Entry, vaults.Edit) (string, error)
	Unregister func(registry.Entry) error
	// StagePlan says which files under the sources are new to a project's vault; no
	// sources means the folders it staged from before. Stage copies a plan's files into
	// the inbox and reports the folders the vault now remembers. Sources lists them.
	StagePlan func(registry.Entry, []string) (*capture.StagePlan, error)
	Stage     func(registry.Entry, *capture.StagePlan) (*capture.StageResult, []string, error)
	Sources   func(registry.Entry) []string
	// The repository calls: mount a folder, initializing git there when asked; create
	// one; clone one from a URL; drop one; and edit its remote, its folder, or how
	// changes land. The first three also report the folder the repository sits in.
	AddRepo    func(registry.Entry, string, bool) (vault.Repo, string, error)
	NewRepo    func(registry.Entry, string, string) (vault.Repo, string, error)
	CloneRepo  func(registry.Entry, string, string) (vault.Repo, string, error)
	RemoveRepo func(registry.Entry, string) error
	EditRepo   func(registry.Entry, string, vaults.RepoEdit) (vault.Repo, error)
	// The mount calls: mount a knowledge base on a project with an access and a mount name
	// (empty means write and the knowledge base's name); unmount by knowledge base id, name,
	// or mount name; grant a project write or read on a knowledge base; revoke by project id.
	Mount   func(project, kb registry.Entry, access, name string) (vault.Mount, error)
	Unmount func(project registry.Entry, target string) error
	// EditMount changes what a mount asks for, read or write.
	EditMount func(project registry.Entry, target, access string) (vault.Mount, error)
	Grant     func(kb, project registry.Entry, access string) error
	Revoke    func(kb registry.Entry, projectID string) error
	// The cluster calls: add a knowledge base to a cluster's member list, and drop one by
	// id or name.
	AddMember    func(cluster, kb registry.Entry) error
	RemoveMember func(cluster registry.Entry, target string) error
	// Tasks reads a project's task ledger and the notes waiting in inbox/tasks/; Plant
	// plants a task in its vault.
	Tasks func(registry.Entry) (tasks.Ledger, []string, error)
	Plant func(registry.Entry, tasks.Plant) (txn.Planted, error)
	// VaultsDir is where a new vault goes by default.
	VaultsDir string
}

// Bind builds the struct over an atlas home and its loaded config. The console is for
// Create's preview when a caller wants one; nil and the view pass none.
func Bind(h home.Home, cfg *home.Config, c *console.Console) Atlas {
	return Atlas{
		Load:   func() ([]registry.Entry, error) { return refresh.Entries(h, cfg, time.Now()) },
		Scan:   func() (*registry.Index, error) { return refresh.Derived(cfg, time.Now()) },
		Create: func(choice AddVault) (string, error) { return createOrAdopt(h, cfg, c, choice) },
		Refresh: func() (*registry.Index, []refresh.ProjectChange, error) {
			_, ix, changes, err := refresh.All(h, cfg, time.Now())
			return ix, changes, err
		},
		Edit: func(en registry.Entry, edit vaults.Edit) (string, error) {
			return vaults.EditIdentity(h, cfg, en, edit, time.Now())
		},
		Unregister: func(en registry.Entry) error { return vaults.Unregister(h, cfg, en.Path) },
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
		AddRepo: func(en registry.Entry, target string, initGit bool) (vault.Repo, string, error) {
			return vaults.AddRepo(h, cfg, en, target, initGit, time.Now())
		},
		NewRepo: func(en registry.Entry, name, at string) (vault.Repo, string, error) {
			return vaults.CreateRepo(h, cfg, en, name, at, time.Now())
		},
		CloneRepo: func(en registry.Entry, url, at string) (vault.Repo, string, error) {
			return vaults.CloneRepo(h, cfg, en, url, at, time.Now())
		},
		RemoveRepo: func(en registry.Entry, name string) error {
			return vaults.RemoveRepo(h, cfg, en, name, time.Now())
		},
		EditRepo: func(en registry.Entry, name string, edit vaults.RepoEdit) (vault.Repo, error) {
			return vaults.EditRepo(h, cfg, en, name, edit, time.Now())
		},
		Mount: func(project, kb registry.Entry, access, name string) (vault.Mount, error) {
			return vaults.Mount(project, kb, access, name, time.Now())
		},
		Unmount: func(project registry.Entry, target string) error {
			return vaults.Unmount(project, target, time.Now())
		},
		EditMount: func(project registry.Entry, target, access string) (vault.Mount, error) {
			return vaults.SetMountAccess(project, target, access, time.Now())
		},
		Grant: func(kb, project registry.Entry, access string) error {
			return vaults.Grant(kb, project, access, time.Now())
		},
		Revoke: func(kb registry.Entry, projectID string) error {
			return vaults.RevokeID(kb, projectID, time.Now())
		},
		AddMember: func(cluster, kb registry.Entry) error {
			return vaults.AddMember(cluster, kb, time.Now())
		},
		RemoveMember: func(cluster registry.Entry, target string) error {
			return vaults.RemoveMember(cluster, target, time.Now())
		},
		Tasks: func(en registry.Entry) (tasks.Ledger, []string, error) {
			v, err := vault.Open(en.Path)
			if err != nil {
				return tasks.Ledger{}, nil, err
			}
			led, err := tasks.Current(v, time.Now())
			return led, tasks.Notes(v), err
		},
		Plant: func(en registry.Entry, plant tasks.Plant) (txn.Planted, error) {
			v, err := vault.Open(en.Path)
			if err != nil {
				return txn.Planted{}, err
			}
			return plantTask(v, plant)
		},
		VaultsDir: cfg.VaultsDir,
	}
}
