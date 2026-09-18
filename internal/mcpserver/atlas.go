package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// The atlas tools: the whole atlas as one read, and the writes that configure it. They
// bind the same functions the view calls, once per call over a config loaded for that
// call, because the CLI in another process may change config.json between two calls. A
// write ends in a refresh, so the stored registry `claude-atlas list` and the view read
// carries the change, and the tool answers from the index that refresh derived. They
// work with no place at all, because init is how a plain folder becomes a project.

// bind loads the config and binds the atlas actions for one call.
func (s *Server) bind() (actions.Atlas, *home.Config, error) {
	cfg, err := s.home().Load()
	if err != nil {
		return actions.Atlas{}, nil, err
	}
	return actions.Bind(s.home(), cfg, nil), cfg, nil
}

// entryOf resolves an entry a tool names: by name, by id, or by path. One the atlas
// cannot read is an error that says why.
func entryOf(ix *registry.Index, arg string, kind registry.Kind) (registry.Entry, error) {
	en, err := anyEntryOf(ix, arg, kind)
	if err != nil {
		return registry.Entry{}, err
	}
	if en.Error != "" {
		return registry.Entry{}, fmt.Errorf("%s: %s", home.Display(en.Path), en.Error)
	}
	return en, nil
}

// anyEntryOf is entryOf for an entry the atlas cannot read too, so forget can drop it.
func anyEntryOf(ix *registry.Index, arg string, kind registry.Kind) (registry.Entry, error) {
	if strings.TrimSpace(arg) == "" {
		return registry.Entry{}, fmt.Errorf("name a %s: its name, id, or path", kind.Noun())
	}
	found, err := ix.Find(arg, kind)
	if err == nil {
		return *found, nil
	}
	if !errors.Is(err, registry.ErrNotFound) {
		return registry.Entry{}, err
	}
	abs, aerr := filepath.Abs(home.Expand(arg))
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error == "" {
			continue
		}
		if (aerr == nil && e.Path == abs) || strings.EqualFold(filepath.Base(e.Path), arg) {
			return *e, nil
		}
	}
	return registry.Entry{}, fmt.Errorf("no %s named %q; the atlas tool lists them", kind.Noun(), arg)
}

// Settings are the atlas settings a session may read and set.
type Settings struct {
	NewDays int `json:"new_days"`
}

func settingsOf(cfg *home.Config) Settings {
	return Settings{NewDays: cfg.NewDays()}
}

type AtlasArgs struct {
	Refresh bool `json:"refresh,omitempty" jsonschema:"also rewrite the registry: what claude-atlas refresh does"`
}

// AtlasOut is the whole atlas: every knowledge base and project with its state, the
// folders the atlas cannot read, and the settings.
type AtlasOut struct {
	Knowledge []registry.Entry   `json:"knowledge"`
	Projects  []registry.Entry   `json:"projects"`
	Problems  []registry.Problem `json:"problems"`
	Settings  Settings           `json:"settings"`
}

func (s *Server) atlasTool(ctx context.Context, req *mcp.CallToolRequest, a AtlasArgs) (*mcp.CallToolResult, AtlasOut, error) {
	acts, cfg, err := s.bind()
	if err != nil {
		return nil, AtlasOut{}, err
	}
	var ix *registry.Index
	if a.Refresh {
		ix, err = acts.Refresh()
	} else {
		ix, err = acts.Scan()
	}
	if err != nil {
		return nil, AtlasOut{}, err
	}
	out := AtlasOut{Knowledge: ix.Knowledge(), Projects: ix.Projects(), Problems: ix.Problems, Settings: settingsOf(cfg)}
	if out.Knowledge == nil {
		out.Knowledge = []registry.Entry{}
	}
	if out.Projects == nil {
		out.Projects = []registry.Entry{}
	}
	if out.Problems == nil {
		out.Problems = []registry.Problem{}
	}
	return nil, out, nil
}

type VaultToolArgs struct {
	Action string  `json:"action" jsonschema:"create, adopt, edit, or forget"`
	Target string  `json:"target,omitempty" jsonschema:"edit, forget: the knowledge base, by name, id, or path"`
	Name   string  `json:"name,omitempty" jsonschema:"create: the knowledge base's name; adopt: its display name, default the folder's; edit: the new name, which renames the folder too"`
	Path   string  `json:"path,omitempty" jsonschema:"create: the new folder, an absolute or ~ path; adopt: the folder to adopt"`
	Mode   string  `json:"mode,omitempty" jsonschema:"create, adopt: the filing mode, generic (default) or lyt"`
	Scope  *string `json:"scope,omitempty" jsonschema:"create, adopt, edit: what the knowledge base covers, one or two sentences; on edit an empty string clears it"`
}

// VaultToolOut is the knowledge base as the atlas sees it after the change, or the path
// forget dropped.
type VaultToolOut struct {
	Vault     *registry.Entry `json:"vault,omitempty"`
	Forgotten string          `json:"forgotten,omitempty" jsonschema:"the path the atlas no longer lists; the folder stays"`
}

func (s *Server) vaultTool(ctx context.Context, req *mcp.CallToolRequest, a VaultToolArgs) (*mcp.CallToolResult, VaultToolOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, VaultToolOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, VaultToolOut{}, err
	}
	switch a.Action {
	case "create", "adopt":
		choice, err := knowledgeChoice(a)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		path, err := acts.CreateKnowledge(choice)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		return s.entryOut(acts, path)
	case "edit":
		en, err := entryOf(ix, a.Target, registry.Knowledge)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		if a.Name == "" && a.Scope == nil {
			return nil, VaultToolOut{}, errors.New("edit needs name or scope")
		}
		path, err := acts.EditKnowledge(en, vaults.Edit{Name: a.Name, Scope: a.Scope})
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		return s.entryOut(acts, path)
	case "forget":
		en, err := anyEntryOf(ix, a.Target, registry.Knowledge)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		if err := acts.ForgetKnowledge(en); err != nil {
			return nil, VaultToolOut{}, err
		}
		if _, err := acts.Refresh(); err != nil {
			return nil, VaultToolOut{}, err
		}
		return nil, VaultToolOut{Forgotten: en.Path}, nil
	}
	return nil, VaultToolOut{}, fmt.Errorf("action must be create, adopt, edit, or forget, not %q", a.Action)
}

// knowledgeChoice turns the create and adopt arguments into what CreateKnowledge takes.
func knowledgeChoice(a VaultToolArgs) (actions.AddKnowledge, error) {
	choice := actions.AddKnowledge{Name: a.Name, Mode: a.Mode, Adopt: a.Action == "adopt"}
	if a.Scope != nil {
		choice.Scope = *a.Scope
	}
	if choice.Adopt {
		if a.Path == "" {
			return actions.AddKnowledge{}, errors.New("adopt needs path: the folder to adopt")
		}
		abs, err := filepath.Abs(home.Expand(a.Path))
		if err != nil {
			return actions.AddKnowledge{}, err
		}
		choice.Path = abs
		if choice.Name == "" {
			choice.Name = filepath.Base(abs)
		}
		return choice, nil
	}
	if a.Name == "" {
		return actions.AddKnowledge{}, errors.New("create needs name")
	}
	if !filepath.IsAbs(a.Path) && a.Path != "~" && !strings.HasPrefix(a.Path, "~/") {
		return actions.AddKnowledge{}, errors.New("create needs path: the new folder, as an absolute or ~ path; ask the user where the knowledge base goes")
	}
	path, err := vaults.ResolvePath(a.Path)
	if err != nil {
		return actions.AddKnowledge{}, err
	}
	choice.Path = path
	return choice, nil
}

// entryOut rewrites the registry after a write, so `claude-atlas list` and the view show
// the change, and returns the entry at path as the atlas now sees it.
func (s *Server) entryOut(acts actions.Atlas, path string) (*mcp.CallToolResult, VaultToolOut, error) {
	ix, err := acts.Refresh()
	if err != nil {
		return nil, VaultToolOut{}, err
	}
	en := ix.ByPath(path)
	if en == nil {
		return nil, VaultToolOut{}, fmt.Errorf("%s was written but the scan does not list it; call atlas with refresh", home.Display(path))
	}
	return nil, VaultToolOut{Vault: en}, nil
}

type ProjectToolArgs struct {
	Action      string  `json:"action" jsonschema:"init, link, unlink, edit, or forget"`
	Work        string  `json:"work,omitempty" jsonschema:"the project's folder: on init the folder that becomes one, default the session's folder; otherwise the project by name, id, or path, default the session's project"`
	Name        string  `json:"name,omitempty" jsonschema:"init: the project's name, default the folder's; edit: the new name"`
	Description *string `json:"description,omitempty" jsonschema:"init, edit: one sentence saying what the project is; on edit an empty string clears it"`
	Knowledge   string  `json:"knowledge,omitempty" jsonschema:"init, link: the knowledge base the project uses, by name, id, or path"`
	NoGit       bool    `json:"no_git,omitempty" jsonschema:"init: leave a folder that is in no git repository without one"`
}

// ProjectToolOut is the project as the atlas sees it after the change, the files init
// wrote, or the path forget dropped.
type ProjectToolOut struct {
	Project   *registry.Entry `json:"project,omitempty"`
	Written   []string        `json:"written,omitempty" jsonschema:"init: what was written under atlas/<name>/"`
	Git       string          `json:"git,omitempty" jsonschema:"init: created (the work is now a repository with no commit), existing, or enclosed (the work sits inside another repository)"`
	Forgotten string          `json:"forgotten,omitempty" jsonschema:"the path the atlas no longer lists; the folder and its atlas/<name>/ stay"`
}

func (s *Server) projectTool(ctx context.Context, req *mcp.CallToolRequest, a ProjectToolArgs) (*mcp.CallToolResult, ProjectToolOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, ProjectToolOut{}, err
	}
	if a.Action == "init" {
		work := a.Work
		if work == "" {
			work = s.opts.ProjectDir
		}
		if work == "" {
			return nil, ProjectToolOut{}, errors.New("init needs work: the folder that becomes a project")
		}
		choice := actions.InitProject{Work: home.Expand(work), Name: a.Name, Knowledge: a.Knowledge, NoGit: a.NoGit}
		if a.Description != nil {
			choice.Description = *a.Description
		}
		made, err := acts.InitProject(choice)
		if err != nil {
			return nil, ProjectToolOut{}, err
		}
		res, out, err := s.projectOut(acts, made.Project.Root)
		out.Written, out.Git = made.Written, string(made.Git)
		return res, out, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, ProjectToolOut{}, err
	}
	var en registry.Entry
	if a.Work != "" {
		if a.Action == "forget" {
			en, err = anyEntryOf(ix, a.Work, registry.Project)
		} else {
			en, err = entryOf(ix, a.Work, registry.Project)
		}
		if err != nil {
			return nil, ProjectToolOut{}, err
		}
	} else {
		pl, err := s.where()
		if err != nil || !pl.InProject() {
			return nil, ProjectToolOut{}, errors.New("name the project with work; this session is not in one")
		}
		found := ix.ByPath(pl.Project.Root)
		if found == nil || found.Error != "" {
			return nil, ProjectToolOut{}, fmt.Errorf("the atlas does not list %s; start a session in it, or pass work", pl.Project.Root)
		}
		en = *found
	}
	switch a.Action {
	case "link":
		if a.Knowledge == "" {
			return nil, ProjectToolOut{}, errors.New("link needs knowledge: the knowledge base, by name, id, or path")
		}
		if _, err := acts.LinkProject(en, a.Knowledge); err != nil {
			return nil, ProjectToolOut{}, err
		}
	case "unlink":
		if err := acts.UnlinkProject(en); err != nil {
			return nil, ProjectToolOut{}, err
		}
	case "edit":
		if a.Name == "" && a.Description == nil {
			return nil, ProjectToolOut{}, errors.New("edit needs name or description")
		}
		if err := acts.EditProject(en, vaults.ProjectEdit{Name: a.Name, Description: a.Description}); err != nil {
			return nil, ProjectToolOut{}, err
		}
	case "forget":
		if err := acts.ForgetProject(en); err != nil {
			return nil, ProjectToolOut{}, err
		}
		if _, err := acts.Refresh(); err != nil {
			return nil, ProjectToolOut{}, err
		}
		return nil, ProjectToolOut{Forgotten: en.Path}, nil
	default:
		return nil, ProjectToolOut{}, fmt.Errorf("action must be init, link, unlink, edit, or forget, not %q", a.Action)
	}
	return s.projectOut(acts, en.Path)
}

// projectOut rewrites the registry after a write and returns the project at work as the
// atlas now sees it.
func (s *Server) projectOut(acts actions.Atlas, work string) (*mcp.CallToolResult, ProjectToolOut, error) {
	ix, err := acts.Refresh()
	if err != nil {
		return nil, ProjectToolOut{}, err
	}
	en := ix.ByPath(work)
	if en == nil {
		return nil, ProjectToolOut{}, fmt.Errorf("%s was written but the scan does not list it; call atlas with refresh", home.Display(work))
	}
	return nil, ProjectToolOut{Project: en}, nil
}

type SettingsArgs struct {
	NewDays *int `json:"new_days,omitempty" jsonschema:"an entry is new for this many days after its creation; 0 turns it off"`
}

func (s *Server) settingsTool(ctx context.Context, req *mcp.CallToolRequest, a SettingsArgs) (*mcp.CallToolResult, Settings, error) {
	acts, cfg, err := s.bind()
	if err != nil {
		return nil, Settings{}, err
	}
	if a.NewDays != nil {
		if err := cfg.SetNewDays(*a.NewDays); err != nil {
			return nil, Settings{}, err
		}
		if err := s.home().Save(cfg); err != nil {
			return nil, Settings{}, err
		}
		if _, err := acts.Refresh(); err != nil {
			return nil, Settings{}, err
		}
	}
	return nil, settingsOf(cfg), nil
}

type StageArgs struct {
	Paths   []string `json:"paths,omitempty" jsonschema:"files or folders outside the knowledge base; omit to stage what is new in the folders it staged from before"`
	Project string   `json:"project,omitempty" jsonschema:"a project that uses the knowledge base, by name, id, or path: write a snapshot of it into the inbox (its CLAUDE.md, README, file list, docs headings, and the log since the page describing it was written) for the describe skill; not with paths"`
	DryRun  bool     `json:"dry_run,omitempty" jsonschema:"plan only: say what would be copied and copy nothing"`
}

// StageOut is the plan, and after a copy, what was copied and the folders the knowledge
// base now stages from when paths is omitted; or the snapshot a stage of a project wrote.
type StageOut struct {
	Vault      string                `json:"vault"`
	Plan       *capture.StagePlan    `json:"plan,omitempty"`
	Result     *capture.StageResult  `json:"result,omitempty"`
	Remembered []string              `json:"remembered,omitempty"`
	Snapshot   *capture.ProjectStage `json:"snapshot,omitempty"`
}

func (s *Server) stageTool(ctx context.Context, req *mcp.CallToolRequest, a StageArgs) (*mcp.CallToolResult, StageOut, error) {
	pl, v, err := s.knowledge()
	if err != nil {
		return nil, StageOut{}, err
	}
	acts, _, err := s.bind()
	if err != nil {
		return nil, StageOut{}, err
	}
	if pl.Index == nil {
		return nil, StageOut{}, errors.New("no atlas config on this machine; run claude-atlas setup")
	}
	kb := pl.Index.ByPath(v.Root)
	if kb == nil || kb.Error != "" {
		return nil, StageOut{}, fmt.Errorf("the atlas does not list %s; run `claude-atlas adopt %s`", v.Name(), v.Root)
	}
	out := StageOut{Vault: v.Root}
	if a.Project != "" || (pl.InProject() && len(a.Paths) == 0) {
		if len(a.Paths) > 0 {
			return nil, StageOut{}, errors.New("stage takes project or paths, not both")
		}
		if a.DryRun {
			return nil, StageOut{}, errors.New("a project snapshot has no dry run; it writes one file into the inbox or finds it already there")
		}
		_, en, err := s.projectOf(pl, a.Project)
		if err != nil {
			return nil, StageOut{}, err
		}
		if en == nil {
			return nil, StageOut{}, errors.New("the atlas does not list this project; start a session in it first")
		}
		snap, err := acts.StageProject(*en)
		if err != nil {
			return nil, StageOut{}, err
		}
		if snap.New {
			if _, err := acts.Refresh(); err != nil {
				return nil, StageOut{}, err
			}
		}
		out.Snapshot = snap
		return nil, out, nil
	}
	plan, err := acts.StagePlan(*kb, a.Paths)
	if err != nil {
		return nil, StageOut{}, err
	}
	out.Plan = plan
	if a.DryRun {
		return nil, out, nil
	}
	res, remembered, err := acts.Stage(*kb, plan)
	if err != nil {
		return nil, StageOut{}, err
	}
	// The inbox count is part of the derived state, so the registry is rewritten.
	if _, err := acts.Refresh(); err != nil {
		return nil, StageOut{}, err
	}
	out.Result, out.Remembered = res, remembered
	return nil, out, nil
}
