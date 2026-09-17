// Package mcpserver exposes the core to Claude Code as MCP tools. One server process
// lives for one session and holds the plans the model has built but not yet applied.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/describe"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/ledger"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/place"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// Name is the MCP server name; Claude Code exposes tools as mcp__plugin_claude-atlas_atlas__<tool>.
const Name = "atlas"

// Options configure a server.
type Options struct {
	// Version of the binary.
	Version string
	// PluginRoot is ${CLAUDE_PLUGIN_ROOT}; used to read the plugin's version.
	PluginRoot string
	// ProjectDir is where the session started; the place is found by walking up from it.
	ProjectDir string
	// Env resolves environment variables. nil means os.Getenv.
	Env func(string) string
	// Now returns the current time. nil means time.Now.
	Now func() time.Time
}

// Server holds session state.
type Server struct {
	opts  Options
	mu    sync.Mutex
	plans map[string]*txn.Plan
}

// New builds a server.
func New(opts Options) *Server {
	if opts.Env == nil {
		opts.Env = os.Getenv
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Server{opts: opts, plans: map[string]*txn.Plan{}}
}

// home is the atlas home the session reads.
func (s *Server) home() home.Home { return home.Resolve(s.opts.Env(home.EnvHome)) }

// where finds the session's place once per call: a project, anywhere inside its work,
// or a knowledge base. Nothing is registered here; the session-start hook heals the
// config.
func (s *Server) where() (*place.Place, error) {
	return place.Resolve(s.home(), "", s.opts.Env(place.EnvPlace), s.opts.ProjectDir, false)
}

// knowledge is the knowledge base a call acts on: the session's own, or the project's.
// A project without one is refused with the reason.
func (s *Server) knowledge() (*place.Place, *vault.Vault, error) {
	pl, err := s.where()
	if err != nil {
		return nil, nil, err
	}
	if pl.Vault != nil {
		return pl, pl.Vault, nil
	}
	if pl.KnowledgeError != "" {
		return nil, nil, fmt.Errorf("%s: %s", pl.Project.Name(), pl.KnowledgeError)
	}
	return nil, nil, fmt.Errorf("%s uses no knowledge base; link one with `claude-atlas link KB`", pl.Project.Name())
}

// via names the project a knowledge base write came through, or nil in a knowledge
// base session.
func via(pl *place.Place) *ledger.Via {
	if pl == nil || pl.Project == nil {
		return nil
	}
	return &ledger.Via{ID: pl.Project.Config.ID, Name: pl.Project.Name()}
}

// projectOf resolves the project a task tool acts on: the session's own when arg is
// empty, else one the atlas knows by name, id, or path. In a knowledge base session the
// project must use that knowledge base.
func (s *Server) projectOf(pl *place.Place, arg string) (*project.Project, *registry.Entry, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		if pl.Project == nil {
			return nil, nil, errors.New("name the project: this is a knowledge base session, so pass project (a name, id, or path of one that uses it)")
		}
		return pl.Project, pl.Entry, nil
	}
	if pl.Index == nil {
		return nil, nil, errors.New("no atlas config on this machine; run claude-atlas setup")
	}
	e, err := pl.Index.Find(arg, registry.Project)
	if err != nil {
		return nil, nil, err
	}
	if pl.Project == nil && pl.Vault != nil {
		if e.Knowledge == nil || e.Knowledge.ID != pl.Vault.Config.ID {
			return nil, nil, fmt.Errorf("%s does not use the knowledge base %s", e.Name, pl.Vault.Name())
		}
	}
	p, err := project.Open(e.Path)
	if err != nil {
		return nil, nil, err
	}
	return p, e, nil
}

func (s *Server) pluginVersion() string {
	if s.opts.PluginRoot == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(s.opts.PluginRoot, ".claude-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	json.Unmarshal(data, &m)
	return m.Version
}

// Empty is the argument of a tool that takes none.
type Empty struct{}

// GitInfo is what git says about a project's work folder.
type GitInfo struct {
	Branch string `json:"branch,omitempty"`
	Dirty  int    `json:"dirty"`
}

// KnowledgeRef names the knowledge base a project uses.
type KnowledgeRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// DescribedInfo is the page that describes a project, and how current it is.
type DescribedInfo struct {
	registry.Description
	Summary string `json:"summary"`
}

// ProjectTasks is a project's task counts and its phases in order.
type ProjectTasks struct {
	Counts tasks.Counts `json:"counts"`
	Phases []string     `json:"phases"`
}

// ProjectInfo is one project as a knowledge base sees it.
type ProjectInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	OpenTasks int    `json:"open_tasks"`
	Described bool   `json:"described"`
}

// Versions are the binary's and the plugin's.
type Versions struct {
	Binary string `json:"binary"`
	Plugin string `json:"plugin,omitempty"`
}

// Status is the status tool's output: a project's facts or a knowledge base's.
type Status struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	// A project's fields.
	Description    string         `json:"description,omitempty"`
	Git            *GitInfo       `json:"git,omitempty"`
	Knowledge      *KnowledgeRef  `json:"knowledge,omitempty"`
	KnowledgeError string         `json:"knowledge_error,omitempty"`
	Described      *DescribedInfo `json:"described,omitempty"`
	Tasks          *ProjectTasks  `json:"tasks,omitempty"`
	Notes          int            `json:"notes"`
	// A knowledge base's fields.
	Mode          string         `json:"mode,omitempty"`
	Scope         string         `json:"scope,omitempty"`
	Pages         int            `json:"pages"`
	InboxWaiting  int            `json:"inbox_waiting"`
	Projects      []ProjectInfo  `json:"projects,omitempty"`
	VaultGit      *txn.Status    `json:"vault_git,omitempty"`
	LastOperation *txn.Operation `json:"last_operation,omitempty"`
	Stubs         int            `json:"stubs"`
	WantedPages   int            `json:"wanted_pages"`
	// Both.
	PendingRecovery bool     `json:"pending_recovery"`
	Versions        Versions `json:"versions"`
	Warnings        []string `json:"warnings"`
}

func (s *Server) status(ctx context.Context, req *mcp.CallToolRequest, a Empty) (*mcp.CallToolResult, Status, error) {
	pl, err := s.where()
	if err != nil {
		return nil, Status{}, err
	}
	now := s.opts.Now()
	out := Status{Warnings: []string{}, Versions: Versions{Binary: s.opts.Version, Plugin: s.pluginVersion()}}
	if pl.InProject() {
		s.projectStatus(pl, &out, now)
	} else {
		s.knowledgeStatus(pl, &out, now)
	}
	if pl.Vault != nil {
		if pending, _ := txn.Pending(pl.Vault); pending != nil {
			out.PendingRecovery = true
			out.Warnings = append(out.Warnings, "an operation was interrupted in "+pl.Vault.Name()+"; run `claude-atlas recover "+pl.Vault.Root+"` before changing it")
		}
	}
	if out.Versions.Plugin != "" && out.Versions.Binary != "dev" && out.Versions.Plugin != out.Versions.Binary {
		out.Warnings = append(out.Warnings, fmt.Sprintf("plugin %s and binary %s differ; update one of them", out.Versions.Plugin, out.Versions.Binary))
	}
	return nil, out, nil
}

func (s *Server) projectStatus(pl *place.Place, out *Status, now time.Time) {
	p := pl.Project
	out.Kind, out.ID, out.Name, out.Path, out.Description = "project", p.Config.ID, p.Name(), p.Root, p.Config.Description
	if fact := links.Inspect(links.Repo, p.Root); fact.OK {
		git := &GitInfo{Branch: fact.Branch}
		if fact.Dirty != nil {
			git.Dirty = *fact.Dirty
		}
		out.Git = git
	}
	switch {
	case pl.Vault != nil:
		out.Knowledge = &KnowledgeRef{ID: pl.Vault.Config.ID, Name: pl.Vault.Name(), Path: pl.Vault.Root}
		if pl.Entry != nil {
			if d := describe.Page(*pl.Entry); d != nil {
				out.Described = &DescribedInfo{Description: *d, Summary: d.Summary()}
				if d.Behind > describe.BehindThreshold {
					out.Warnings = append(out.Warnings, fmt.Sprintf("the page describing this project is %d commits behind; the describe skill brings it up to date", d.Behind))
				}
			} else {
				out.Warnings = append(out.Warnings, registry.NotDescribed+"; the describe skill writes the page")
			}
		}
	case pl.KnowledgeError != "":
		out.KnowledgeError = pl.KnowledgeError
		out.Warnings = append(out.Warnings, pl.KnowledgeError)
	default:
		out.Warnings = append(out.Warnings, "this project uses no knowledge base; link one with `claude-atlas link KB`")
	}
	out.Notes = len(tasks.Notes(p))
	if board, err := tasks.Load(p); err == nil {
		pt := &ProjectTasks{Counts: board.Counts(now), Phases: []string{}}
		pt.Counts.Notes = out.Notes
		for _, ph := range board.Phases {
			pt.Phases = append(pt.Phases, ph.Title)
		}
		out.Tasks = pt
		var stale []string
		for _, t := range board.Open() {
			if tasks.Stale(t, now) {
				stale = append(stale, t.Title)
			}
		}
		if len(stale) > 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%d active task%s untouched for %d days: %s", len(stale), plural(len(stale)), tasks.StaleDays, strings.Join(stale, "; ")))
		}
		for _, pr := range board.Problems {
			out.Warnings = append(out.Warnings, "task page "+pr.Path+": "+pr.Reason)
		}
	}
	if out.Notes > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%d task note%s wait in %s/%s/; the task-plant skill turns them into tasks", out.Notes, plural(out.Notes), project.Dir, project.InboxDir))
	}
}

func (s *Server) knowledgeStatus(pl *place.Place, out *Status, now time.Time) {
	v := pl.Vault
	out.Kind, out.ID, out.Name, out.Path, out.Mode, out.Scope = "knowledge", v.Config.ID, v.Name(), v.Root, string(v.Config.Mode), v.Config.Scope
	out.Projects = []ProjectInfo{}
	if st, err := txn.Inspect(v); err == nil {
		out.VaultGit = st
		if !st.HasHistory {
			out.Warnings = append(out.Warnings, "the knowledge base has no git history; run `claude-atlas adopt "+v.Root+"`")
		}
	}
	if report, err := lint.Run(v.Root, lint.Options{AsOf: now}); err == nil {
		out.Pages = report.Summary.PagesScanned
		out.Stubs = len(report.Stubs)
		out.WantedPages = len(report.WantedPages)
	}
	if files, err := capture.ListInbox(v, now); err == nil {
		for _, f := range files {
			if !f.Captured {
				out.InboxWaiting++
			}
		}
	}
	if ops, err := txn.History(v, 1, false); err == nil && len(ops) > 0 {
		out.LastOperation = &ops[0]
	}
	if pl.Index != nil && pl.Entry != nil {
		for _, e := range pl.Index.ProjectsOf(pl.Entry.ID) {
			info := ProjectInfo{ID: e.ID, Name: e.Name, Path: e.Path, Described: describe.Page(e) != nil}
			if p, err := project.Open(e.Path); err == nil {
				if board, err := tasks.Load(p); err == nil {
					info.OpenTasks = board.Counts(now).Open
				}
			}
			out.Projects = append(out.Projects, info)
			if !info.Described {
				out.Warnings = append(out.Warnings, "project "+e.Name+" is "+registry.NotDescribed+"; the describe skill writes the page")
			}
		}
	}
}

// InboxOut is the knowledge base's inbox, and in a project session the task notes too.
type InboxOut struct {
	Vault string              `json:"vault"`
	Files []capture.InboxFile `json:"files"`
	Notes []string            `json:"notes,omitempty"`
}

func (s *Server) inbox(ctx context.Context, req *mcp.CallToolRequest, a Empty) (*mcp.CallToolResult, InboxOut, error) {
	pl, err := s.where()
	if err != nil {
		return nil, InboxOut{}, err
	}
	out := InboxOut{Files: []capture.InboxFile{}}
	if pl.Vault != nil {
		files, err := capture.ListInbox(pl.Vault, s.opts.Now())
		if err != nil {
			return nil, InboxOut{}, err
		}
		out.Vault, out.Files = pl.Vault.Root, files
	}
	if pl.InProject() {
		out.Notes = tasks.Notes(pl.Project)
		if out.Notes == nil {
			out.Notes = []string{}
		}
	} else if pl.Vault == nil {
		return nil, InboxOut{}, place.ErrNoPlace
	}
	return nil, out, nil
}

type CaptureArgs struct {
	Paths []string `json:"paths" jsonschema:"files in the knowledge base's inbox/ to capture, as inbox-relative or vault-relative paths"`
}

func (s *Server) capture(ctx context.Context, req *mcp.CallToolRequest, a CaptureArgs) (*mcp.CallToolResult, capture.Result, error) {
	pl, v, err := s.knowledge()
	if err != nil {
		return nil, capture.Result{}, err
	}
	res, err := capture.Capture(v, a.Paths, via(pl), s.opts.Now())
	if err != nil {
		return nil, capture.Result{}, err
	}
	return nil, *res, nil
}

type RouteArgs struct {
	Type  string `json:"type" jsonschema:"page type: source, entity, concept; in lyt mode also note or moc"`
	Title string `json:"title" jsonschema:"the page title; it becomes the file name"`
}

// routeNext tells the model what a match means: a reason to link, not to duplicate.
const routeNext = "A match means link to it instead of creating a page."

// RouteOut is where a page belongs and whether one by that title or alias already exists.
type RouteOut struct {
	vault.Route
	Vault string       `json:"vault"`
	Match *vault.Match `json:"match,omitempty"`
	Next  string       `json:"next"`
}

func (s *Server) route(ctx context.Context, req *mcp.CallToolRequest, a RouteArgs) (*mcp.CallToolResult, RouteOut, error) {
	_, v, err := s.knowledge()
	if err != nil {
		return nil, RouteOut{}, err
	}
	r, err := v.RouteFor(a.Type, a.Title, s.opts.Now())
	if err != nil {
		return nil, RouteOut{}, err
	}
	match, err := vault.FindPage(v.Root, a.Title)
	if err != nil {
		return nil, RouteOut{}, err
	}
	return nil, RouteOut{Route: *r, Vault: v.Root, Match: match, Next: routeNext}, nil
}

type StubArgs struct {
	Titles []txn.StubTitle `json:"titles,omitempty" jsonschema:"the pages to stub; omit to stub every wanted page and every empty page a link points to"`
	Type   string          `json:"type,omitempty" jsonschema:"the type for titles that name none: concept or entity; in lyt mode note or moc as well; the default is concept, or note in lyt mode"`
}

func (s *Server) stub(ctx context.Context, req *mcp.CallToolRequest, a StubArgs) (*mcp.CallToolResult, txn.StubResult, error) {
	pl, v, err := s.knowledge()
	if err != nil {
		return nil, txn.StubResult{}, err
	}
	for _, t := range a.Titles {
		if strings.TrimSpace(t.Target) != "" {
			return nil, txn.StubResult{}, errors.New("target is gone: a session has one knowledge base, and every stub lands there")
		}
	}
	now := s.opts.Now()
	var res txn.StubResult
	if pl.InProject() {
		res, err = txn.StubVia(v, a.Titles, a.Type, pl.Project.Name(), now)
	} else {
		res, err = txn.StubPages(v, a.Titles, a.Type, now)
	}
	if err != nil {
		return nil, txn.StubResult{}, err
	}
	if res.Stubs == nil {
		res.Stubs = []txn.Stubbed{}
	}
	return nil, res, nil
}

// ProjectArg names the project a task tool acts on.
type ProjectArg struct {
	Project string `json:"project,omitempty" jsonschema:"the project, by name, id, or path; omit in a project session. In a knowledge base session it must be one of the projects that use it"`
}

type PlantArgs struct {
	ProjectArg
	Title    string `json:"title,omitempty" jsonschema:"the task's title; taken from the text when omitted"`
	Text     string `json:"text,omitempty" jsonschema:"the idea in the user's words; kept verbatim on the page"`
	Priority string `json:"priority,omitempty" jsonschema:"high, normal, low, or someday; default normal"`
	Phase    string `json:"phase,omitempty" jsonschema:"the phase the task belongs to, by title; it must exist"`
	Due      string `json:"due,omitempty" jsonschema:"YYYY-MM-DD"`
	From     string `json:"from,omitempty" jsonschema:"the note under atlas/inbox/ this task comes from, atlas-relative; it is removed once the page exists"`
	Plan     string `json:"plan,omitempty" jsonschema:"the Plan section's text: the approach, the steps, what done looks like; with it the task is planned"`
	Start    bool   `json:"start,omitempty" jsonschema:"with plan: make the task active now, with a first Progress line; the work skill uses it"`
}

// PlantOut is the task planted and where its page is.
type PlantOut struct {
	tasks.Task
	Project string `json:"project"`
	// File is the page's absolute path.
	File string `json:"file"`
}

func (s *Server) plant(ctx context.Context, req *mcp.CallToolRequest, a PlantArgs) (*mcp.CallToolResult, PlantOut, error) {
	pl, err := s.where()
	if err != nil {
		return nil, PlantOut{}, err
	}
	p, _, err := s.projectOf(pl, a.Project)
	if err != nil {
		return nil, PlantOut{}, err
	}
	t, err := tasks.PlantTask(p, tasks.Plant{Title: a.Title, Text: a.Text, Priority: a.Priority, Phase: a.Phase, Due: a.Due, Plan: a.Plan, Start: a.Start, From: a.From}, s.opts.Now())
	if err != nil {
		return nil, PlantOut{}, err
	}
	return nil, PlantOut{Task: *t, Project: p.Name(), File: p.Path(t.Path)}, nil
}

// PhaseInfo is one phase as the tasks tool lists it.
type PhaseInfo struct {
	Title    string `json:"title"`
	Order    int    `json:"order"`
	Finished bool   `json:"finished"`
	Open     int    `json:"open"`
	Path     string `json:"path"`
}

// ProjectBoard is one project's tasks.
type ProjectBoard struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	Atlas    string          `json:"atlas"`
	Counts   tasks.Counts    `json:"counts"`
	Phases   []PhaseInfo     `json:"phases"`
	Open     []tasks.Task    `json:"open"`
	Archived []tasks.Task    `json:"archived"`
	Problems []tasks.Problem `json:"problems,omitempty"`
	Notes    []string        `json:"notes"`
}

// TasksOut is one board per project.
type TasksOut struct {
	Projects []ProjectBoard `json:"projects"`
}

func (s *Server) tasks(ctx context.Context, req *mcp.CallToolRequest, a ProjectArg) (*mcp.CallToolResult, TasksOut, error) {
	pl, err := s.where()
	if err != nil {
		return nil, TasksOut{}, err
	}
	out := TasksOut{Projects: []ProjectBoard{}}
	var projects []*project.Project
	switch {
	case a.Project != "" || pl.InProject():
		p, _, err := s.projectOf(pl, a.Project)
		if err != nil {
			return nil, TasksOut{}, err
		}
		projects = append(projects, p)
	case pl.Index != nil && pl.Entry != nil:
		for _, e := range pl.Index.ProjectsOf(pl.Entry.ID) {
			p, err := project.Open(e.Path)
			if err != nil {
				continue
			}
			projects = append(projects, p)
		}
	}
	now := s.opts.Now()
	for _, p := range projects {
		board, err := tasks.Load(p)
		if err != nil {
			return nil, TasksOut{}, err
		}
		pb := ProjectBoard{Name: p.Name(), Path: p.Root, Atlas: p.Atlas(), Counts: board.Counts(now), Phases: []PhaseInfo{}, Open: board.Open(), Archived: board.Archived(), Problems: board.Problems, Notes: tasks.Notes(p)}
		pb.Counts.Notes = len(pb.Notes)
		for _, ph := range board.Phases {
			pb.Phases = append(pb.Phases, PhaseInfo{Title: ph.Title, Order: ph.Order, Finished: board.Finished(ph.Title), Open: len(board.In(ph.Title)), Path: ph.Path})
		}
		if pb.Open == nil {
			pb.Open = []tasks.Task{}
		}
		if pb.Archived == nil {
			pb.Archived = []tasks.Task{}
		}
		if pb.Notes == nil {
			pb.Notes = []string{}
		}
		out.Projects = append(out.Projects, pb)
	}
	return nil, out, nil
}

type TaskArgs struct {
	ProjectArg
	ID       string  `json:"id" jsonschema:"the task's id, or its title"`
	Status   *string `json:"status,omitempty" jsonschema:"planted, planned, active, blocked, done, or cancelled; done and cancelled move the page to tasks/archive/"`
	Priority *string `json:"priority,omitempty" jsonschema:"high, normal, low, or someday"`
	Phase    *string `json:"phase,omitempty" jsonschema:"a phase title; an empty string clears it"`
	Due      *string `json:"due,omitempty" jsonschema:"YYYY-MM-DD; an empty string clears it"`
}

func (s *Server) task(ctx context.Context, req *mcp.CallToolRequest, a TaskArgs) (*mcp.CallToolResult, PlantOut, error) {
	pl, err := s.where()
	if err != nil {
		return nil, PlantOut{}, err
	}
	p, _, err := s.projectOf(pl, a.Project)
	if err != nil {
		return nil, PlantOut{}, err
	}
	if a.Status == nil && a.Priority == nil && a.Phase == nil && a.Due == nil {
		return nil, PlantOut{}, errors.New("give at least one of status, priority, phase, or due")
	}
	t, err := tasks.Set(p, strings.TrimSpace(a.ID), tasks.Changes{Status: a.Status, Priority: a.Priority, Phase: a.Phase, Due: a.Due}, s.opts.Now())
	if err != nil {
		return nil, PlantOut{}, err
	}
	return nil, PlantOut{Task: *t, Project: p.Name(), File: p.Path(t.Path)}, nil
}

type PhaseArgs struct {
	ProjectArg
	Action   string `json:"action" jsonschema:"create, rename, reorder, or remove"`
	Title    string `json:"title" jsonschema:"the phase's title; on rename, the current one"`
	Goal     string `json:"goal,omitempty" jsonschema:"create: what the phase delivers, in the user's words"`
	Order    *int   `json:"order,omitempty" jsonschema:"create, reorder: its place in the timeline; create takes the next one when omitted"`
	NewTitle string `json:"new_title,omitempty" jsonschema:"rename: the new title; every task that names the phase follows"`
}

// PhaseOut is the phase after the change, or what remove dropped.
type PhaseOut struct {
	Project string       `json:"project"`
	Phase   *tasks.Phase `json:"phase,omitempty"`
	File    string       `json:"file,omitempty"`
	Removed string       `json:"removed,omitempty"`
}

func (s *Server) phase(ctx context.Context, req *mcp.CallToolRequest, a PhaseArgs) (*mcp.CallToolResult, PhaseOut, error) {
	pl, err := s.where()
	if err != nil {
		return nil, PhaseOut{}, err
	}
	p, _, err := s.projectOf(pl, a.Project)
	if err != nil {
		return nil, PhaseOut{}, err
	}
	now := s.opts.Now()
	var ph *tasks.Phase
	switch a.Action {
	case "create":
		ph, err = tasks.CreatePhase(p, a.Title, a.Goal, a.Order, now)
	case "rename":
		ph, err = tasks.RenamePhase(p, a.Title, a.NewTitle, now)
	case "reorder":
		if a.Order == nil {
			return nil, PhaseOut{}, errors.New("reorder needs order")
		}
		ph, err = tasks.ReorderPhase(p, a.Title, *a.Order, now)
	case "remove":
		if err := tasks.RemovePhase(p, a.Title, now); err != nil {
			return nil, PhaseOut{}, err
		}
		return nil, PhaseOut{Project: p.Name(), Removed: a.Title}, nil
	default:
		return nil, PhaseOut{}, fmt.Errorf("action must be create, rename, reorder, or remove, not %q", a.Action)
	}
	if err != nil {
		return nil, PhaseOut{}, err
	}
	return nil, PhaseOut{Project: p.Name(), Phase: ph, File: p.Path(ph.Path)}, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

type PlanWrite struct {
	Path       string `json:"path" jsonschema:"vault-relative path, e.g. wiki/concepts/Contextual Retrieval.md"`
	Mode       string `json:"mode" jsonschema:"create, replace, or delete"`
	Content    string `json:"content,omitempty" jsonschema:"the complete new file content for create and replace"`
	BaseSHA256 string `json:"base_sha256,omitempty" jsonschema:"sha256 of the content you read, for replace and delete; omit to accept the current file"`
}

type PlanSource struct {
	ID        string   `json:"id" jsonschema:"source id from the capture or inbox tool"`
	Ingested  bool     `json:"ingested,omitempty" jsonschema:"true once the source's knowledge is in the wiki"`
	Pages     []string `json:"pages,omitempty" jsonschema:"wiki pages derived from this source"`
	Authority string   `json:"authority,omitempty" jsonschema:"official, primary, secondary, community, synthetic, or unknown"`
	Title     string   `json:"title,omitempty"`
	Notes     string   `json:"notes,omitempty"`
}

type PlanArgs struct {
	Kind    string       `json:"kind" jsonschema:"ingest, save, markdown, repair, fold, canvas, or base"`
	Summary string       `json:"summary" jsonschema:"one line saying what the operation does; it becomes the log entry and commit subject"`
	Writes  []PlanWrite  `json:"writes,omitempty"`
	Sources []PlanSource `json:"sources,omitempty" jsonschema:"ledger updates for sources this operation ingests"`
}

type PlanOut struct {
	PlanID      string      `json:"plan_id"`
	OperationID string      `json:"operation_id"`
	Vault       string      `json:"vault"`
	Kind        string      `json:"kind"`
	Summary     string      `json:"summary"`
	Preview     txn.Preview `json:"preview"`
	Warnings    []string    `json:"warnings"`
	Next        string      `json:"next"`
}

func (s *Server) plan(ctx context.Context, req *mcp.CallToolRequest, a PlanArgs) (*mcp.CallToolResult, PlanOut, error) {
	pl, v, err := s.knowledge()
	if err != nil {
		return nil, PlanOut{}, err
	}
	kind := txn.Kind(a.Kind)
	allowedKind := false
	for _, k := range txn.ModelKinds {
		if k == kind {
			allowedKind = true
		}
	}
	if !allowedKind {
		return nil, PlanOut{}, fmt.Errorf("kind must be one of ingest, save, markdown, repair, fold, canvas, base")
	}
	if kind == txn.Config {
		return nil, PlanOut{}, errors.New("to change the mode, call the mode tool with set")
	}
	summary := a.Summary
	if pl.InProject() && summary != "" {
		summary += " (via " + pl.Project.Name() + ")"
	}
	r := txn.Request{Kind: kind, Summary: summary}
	for _, w := range a.Writes {
		r.Writes = append(r.Writes, txn.Write{Path: w.Path, Mode: txn.WriteMode(w.Mode), Content: []byte(w.Content), BaseSHA256: w.BaseSHA256})
	}
	for _, src := range a.Sources {
		r.Sources = append(r.Sources, ledger.Update{ID: src.ID, Ingested: src.Ingested, Pages: src.Pages, Authority: src.Authority, Title: src.Title, Notes: src.Notes})
	}
	plan, err := txn.Prepare(v, r, s.opts.Now())
	if err != nil {
		return nil, PlanOut{}, err
	}
	s.hold(plan)
	return nil, s.planOut(plan), nil
}

func (s *Server) hold(plan *txn.Plan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.plans {
		if p.Vault == plan.Vault {
			delete(s.plans, id)
		}
	}
	s.plans[plan.ID] = plan
}

func (s *Server) planOut(plan *txn.Plan) PlanOut {
	next := "Show the user this preview. If they approve, call apply with plan_id " + plan.ID + "."
	if len(plan.Warnings) > 0 {
		next = "Resolve or explain the warnings, show the user the preview, then call apply with plan_id " + plan.ID + " if they approve."
	}
	return PlanOut{PlanID: plan.ID, OperationID: plan.OperationID, Vault: plan.Vault, Kind: string(plan.Kind), Summary: plan.Summary, Preview: plan.Preview, Warnings: plan.Warnings, Next: next}
}

type ApplyArgs struct {
	PlanID string `json:"plan_id" jsonschema:"the plan_id returned by plan"`
}

func (s *Server) apply(ctx context.Context, req *mcp.CallToolRequest, a ApplyArgs) (*mcp.CallToolResult, txn.Result, error) {
	s.mu.Lock()
	plan, ok := s.plans[a.PlanID]
	if ok {
		delete(s.plans, a.PlanID)
	}
	s.mu.Unlock()
	if !ok {
		return nil, txn.Result{}, fmt.Errorf("no plan %q is pending; plans are single-use and the newest plan for a knowledge base replaces older ones, so call plan again", a.PlanID)
	}
	v, err := vault.Open(plan.Vault)
	if err != nil {
		return nil, txn.Result{}, err
	}
	res, err := txn.Apply(v, plan, s.opts.Now())
	if err != nil {
		return nil, txn.Result{}, err
	}
	return nil, *res, nil
}

type UndoArgs struct {
	OperationID string `json:"operation_id" jsonschema:"the operation to revert, from history"`
}

func (s *Server) undo(ctx context.Context, req *mcp.CallToolRequest, a UndoArgs) (*mcp.CallToolResult, txn.Result, error) {
	_, v, err := s.knowledge()
	if err != nil {
		return nil, txn.Result{}, err
	}
	res, err := txn.UndoOperation(v, a.OperationID, s.opts.Now())
	if err != nil {
		return nil, txn.Result{}, err
	}
	return nil, *res, nil
}

type HistoryArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"how many operations, newest first (default 10)"`
}

type HistoryOut struct {
	Vault      string          `json:"vault"`
	Operations []txn.Operation `json:"operations"`
}

func (s *Server) history(ctx context.Context, req *mcp.CallToolRequest, a HistoryArgs) (*mcp.CallToolResult, HistoryOut, error) {
	_, v, err := s.knowledge()
	if err != nil {
		return nil, HistoryOut{}, err
	}
	limit := a.Limit
	if limit <= 0 {
		limit = 10
	}
	ops, err := txn.History(v, limit, true)
	if err != nil {
		return nil, HistoryOut{}, err
	}
	if ops == nil {
		ops = []txn.Operation{}
	}
	return nil, HistoryOut{Vault: v.Root, Operations: ops}, nil
}

type LintArgs struct {
	Exclude []string `json:"exclude,omitempty" jsonschema:"path globs to leave out, e.g. wiki/scratch/*"`
}

func (s *Server) lint(ctx context.Context, req *mcp.CallToolRequest, a LintArgs) (*mcp.CallToolResult, lint.Report, error) {
	_, v, err := s.knowledge()
	if err != nil {
		return nil, lint.Report{}, err
	}
	report, err := lint.Run(v.Root, lint.Options{Exclude: a.Exclude, AsOf: s.opts.Now()})
	if err != nil {
		return nil, lint.Report{}, err
	}
	return nil, *report, nil
}

type ModeArgs struct {
	Set string `json:"set,omitempty" jsonschema:"generic or lyt; omit to read the current mode"`
}

type ModeOut struct {
	Mode     string   `json:"mode"`
	Types    []string `json:"types"`
	Plan     *PlanOut `json:"plan,omitempty"`
	Previous string   `json:"previous,omitempty"`
}

func (s *Server) mode(ctx context.Context, req *mcp.CallToolRequest, a ModeArgs) (*mcp.CallToolResult, ModeOut, error) {
	_, v, err := s.knowledge()
	if err != nil {
		return nil, ModeOut{}, err
	}
	out := ModeOut{Mode: string(v.Config.Mode), Types: vault.RoutableTypes(v.Config.Mode)}
	if a.Set == "" {
		return nil, out, nil
	}
	mode, err := vault.ParseMode(a.Set)
	if err != nil {
		return nil, ModeOut{}, err
	}
	if mode == v.Config.Mode {
		return nil, out, nil
	}
	plan, err := txn.Prepare(v, txn.ConfigRequest(v, mode), s.opts.Now())
	if err != nil {
		return nil, ModeOut{}, err
	}
	s.hold(plan)
	po := s.planOut(plan)
	out.Previous = string(v.Config.Mode)
	out.Mode = string(mode)
	out.Types = vault.RoutableTypes(mode)
	out.Plan = &po
	return nil, out, nil
}

func ro() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true}
}

// MCP builds the protocol server with every tool registered.
func (s *Server) MCP() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: Name, Title: "claude-atlas", Version: s.opts.Version}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "status", Annotations: ro(),
		Description: "Describe where the session is. In a project: its knowledge base, the page that describes it, its task counts and phases, and warnings. In a knowledge base: mode, scope, page count, inbox, the projects that use it, git state, and warnings. Call this first."}, s.status)
	mcp.AddTool(server, &mcp.Tool{Name: "inbox", Annotations: ro(),
		Description: "List the files waiting in the knowledge base's inbox/ with size, kind, hash, and whether each already has a captured copy; in a project, the task notes waiting in atlas/inbox/ too."}, s.inbox)
	mcp.AddTool(server, &mcp.Tool{Name: "capture",
		Description: "Copy files from the knowledge base's inbox into the immutable raw store and record them in the source ledger, as one commit. From a project the record names the project the source came through. Returns each file's source id and stored path; read the stored file afterwards with Read."}, s.capture)
	mcp.AddTool(server, &mcp.Tool{Name: "route", Annotations: ro(),
		Description: "Say where a new page of a type belongs under the knowledge base's mode, whether a page with that title or alias already exists, and give a skeleton with the frontmatter conventions."}, s.route)
	mcp.AddTool(server, &mcp.Tool{Name: "plan", Annotations: ro(),
		Description: "Validate a set of file changes against the knowledge base and hold them as a plan. Returns a plan_id, a preview of creates, replaces, and deletes, and warnings such as links that do not resolve. Nothing is written. Show the preview to the user before apply."}, s.plan)
	mcp.AddTool(server, &mcp.Tool{Name: "apply",
		Description: "Apply a held plan as one git commit and write its log entry. Edits made by hand are committed first, so the operation can always be undone exactly. The plan is consumed."}, s.apply)
	mcp.AddTool(server, &mcp.Tool{Name: "undo",
		Description: "Revert one applied operation in the knowledge base as a new commit. Fails if later changes overlap it."}, s.undo)
	mcp.AddTool(server, &mcp.Tool{Name: "history", Annotations: ro(),
		Description: "List the knowledge base's recent operations, newest first, with their kind, summary, date, commit, and changed paths."}, s.history)
	mcp.AddTool(server, &mcp.Tool{Name: "lint", Annotations: ro(),
		Description: "Run the deterministic wiki health check on the knowledge base: dead and ambiguous links, duplicate basenames, orphans, pages missing from every index, missing frontmatter, empty sections, stale index entries, and ledger problems. Read-only."}, s.lint)
	mcp.AddTool(server, &mcp.Tool{Name: "stub",
		Description: "Create seed pages in the knowledge base for the pages the wiki links to but nobody has written (lint's wanted pages). One commit, no plan preview; undo reverts it. Omit titles to stub every one of them with the mode's default type. Pass a title with a type when the name is a person, product, project, or organization (entity)."}, s.stub)
	mcp.AddTool(server, &mcp.Tool{Name: "plant",
		Description: "Plant a task in a project: write its page from a title and the idea's text; planted, or planned when plan is given, or active when start is set too. Give from to remove the atlas/inbox/ note it came from. In a knowledge base session, name the project."}, s.plant)
	mcp.AddTool(server, &mcp.Tool{Name: "tasks", Annotations: ro(),
		Description: "List a project's tasks from its pages: counts, the phases in order, the open tasks by status, priority, and age, the archive, and the notes waiting in atlas/inbox/. In a knowledge base session with no project, every project that uses it."}, s.tasks)
	mcp.AddTool(server, &mcp.Tool{Name: "task",
		Description: "Change a task's status, priority, phase, or due date by its id or title; the page's body stays. Done and cancelled move the page to tasks/archive/. Write Plan, Progress, and Outcome on the page with Edit."}, s.task)
	mcp.AddTool(server, &mcp.Tool{Name: "phase",
		Description: "Create, rename, reorder, or remove a phase of a project: a named slice of the timeline that tasks belong to. Rename follows every task that names it; remove refuses while one does."}, s.phase)
	mcp.AddTool(server, &mcp.Tool{Name: "mode",
		Description: "Read the knowledge base's filing mode (generic or lyt) and the page types it files. Pass set to prepare a plan that changes it; apply that plan to make the change."}, s.mode)
	mcp.AddTool(server, &mcp.Tool{Name: "atlas",
		Description: "Read the whole atlas: every knowledge base with its scope, path, projects, and state; every project with its path, knowledge base, and open tasks; the folders the atlas cannot read; and the settings. Pass refresh to also rewrite the registry."}, s.atlasTool)
	mcp.AddTool(server, &mcp.Tool{Name: "vault",
		Description: "Create or adopt a knowledge base, edit its name or scope, or forget one the atlas lists (the folder stays); action is create, adopt, edit, or forget. State the change and get a yes before calling."}, s.vaultTool)
	mcp.AddTool(server, &mcp.Tool{Name: "project",
		Description: "Make a folder a project (init), set or clear the knowledge base it uses (link, unlink), change its name or description (edit), or forget it (the folder and its atlas/ stay). State the change and get a yes before calling."}, s.projectTool)
	mcp.AddTool(server, &mcp.Tool{Name: "settings",
		Description: "Set an atlas setting and return them all: new_days, how long a knowledge base or project counts as new. With no arguments it only reads."}, s.settingsTool)
	mcp.AddTool(server, &mcp.Tool{Name: "stage",
		Description: "Copy files or folders from outside the knowledge base into its inbox, skipping what it already captured or holds; omit paths to stage what is new in the folders it staged from before. Or, with project (or in a project session with no arguments), write a snapshot of that project into the inbox for the describe skill. dry_run plans and copies nothing."}, s.stageTool)
	return server
}

// Run serves over stdio until the client disconnects.
func Run(ctx context.Context, opts Options) error {
	return New(opts).MCP().Run(ctx, &mcp.StdioTransport{})
}

// ToolNames lists every tool MCP registers, sorted, for docs and tests.
func ToolNames() []string {
	names := []string{"apply", "atlas", "capture", "history", "inbox", "lint", "mode", "phase", "plan", "plant", "project", "route", "settings", "stage", "status", "stub", "task", "tasks", "undo", "vault"}
	sort.Strings(names)
	return names
}
