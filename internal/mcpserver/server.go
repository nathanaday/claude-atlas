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
	"github.com/nathanaday/claude-atlas/internal/threads"
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

// projectOf resolves the project a thread tool acts on: the session's own when arg is
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

// ProjectThreads is a project's thread counts and its phases in order.
type ProjectThreads struct {
	Counts threads.Counts `json:"counts"`
	Phases []string       `json:"phases"`
}

// ProjectInfo is one project as a knowledge base sees it.
type ProjectInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	HotTopics int    `json:"open_threads"`
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
	Description    string          `json:"description,omitempty"`
	Git            *GitInfo        `json:"git,omitempty"`
	Knowledge      *KnowledgeRef   `json:"knowledge,omitempty"`
	KnowledgeError string          `json:"knowledge_error,omitempty"`
	Described      *DescribedInfo  `json:"described,omitempty"`
	Threads        *ProjectThreads `json:"threads,omitempty"`
	Notes          int             `json:"notes"`
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
	out.Notes = len(threads.Notes(p))
	if threads.Legacy(p) {
		out.Warnings = append(out.Warnings, "this project holds task pages from before threads; `claude-atlas upgrade` turns each one into a thread")
	}
	if board, err := threads.Load(p); err == nil {
		pt := &ProjectThreads{Counts: board.Counts(now), Phases: []string{}}
		pt.Counts.Notes = out.Notes
		for _, ph := range board.Phases {
			pt.Phases = append(pt.Phases, ph.Title)
		}
		out.Threads = pt
		var stale []string
		for _, t := range board.Open() {
			if threads.Stale(t, now) {
				stale = append(stale, t.Title)
			}
		}
		if len(stale) > 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%d thread%s with a plan untouched for %d days: %s", len(stale), plural(len(stale)), threads.StaleDays, strings.Join(stale, "; ")))
		}
		for _, pr := range board.Problems {
			out.Warnings = append(out.Warnings, pr.Path+": "+pr.Reason)
		}
	}
	if out.Notes > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%d note%s wait in %s/%s/; the thread-stub skill opens a thread from each", out.Notes, plural(out.Notes), pl.Project.Rel(), project.InboxDir))
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
				if board, err := threads.Load(p); err == nil {
					info.HotTopics = board.Counts(now).Open
				}
			}
			out.Projects = append(out.Projects, info)
			if !info.Described {
				out.Warnings = append(out.Warnings, "project "+e.Name+" is "+registry.NotDescribed+"; the describe skill writes the page")
			}
		}
	}
}

// InboxOut is the knowledge base's inbox, and in a project session the notes too.
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
		out.Notes = threads.Notes(pl.Project)
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

// ProjectArg names the project a thread tool acts on.
type ProjectArg struct {
	Project string `json:"project,omitempty" jsonschema:"the project, by name, id, or path; omit in a project session. In a knowledge base session it must be one of the projects that use it"`
}

// ThreadInfo is a thread with the absolute path of its card and of each document.
type ThreadInfo struct {
	threads.Thread
	Stale bool `json:"stale,omitempty"`
	// Card is the card's absolute path; code owns the card.
	Card string `json:"card"`
	// Files maps each stage that has a document to its absolute path.
	Files map[string]string `json:"files"`
}

func threadInfo(p *project.Project, t threads.Thread, now time.Time) ThreadInfo {
	info := ThreadInfo{Thread: t, Stale: threads.Stale(t, now), Card: p.Path(t.Path), Files: map[string]string{}}
	for _, d := range t.Docs {
		info.Files[d.Stage] = p.Path(d.Path)
	}
	return info
}

func threadInfos(p *project.Project, list []threads.Thread, now time.Time) []ThreadInfo {
	out := make([]ThreadInfo, 0, len(list))
	for _, t := range list {
		out = append(out, threadInfo(p, t, now))
	}
	return out
}

// PhaseInfo is one phase as the threads tool lists it.
type PhaseInfo struct {
	Title    string `json:"title"`
	Order    int    `json:"order"`
	Finished bool   `json:"finished"`
	Open     int    `json:"open"`
	Path     string `json:"path"`
}

// ProjectBoard is one project's threads.
type ProjectBoard struct {
	Name     string            `json:"name"`
	Path     string            `json:"path"`
	Atlas    string            `json:"atlas"`
	Board    string            `json:"board"`
	Counts   threads.Counts    `json:"counts"`
	Phases   []PhaseInfo       `json:"phases"`
	Open     []ThreadInfo      `json:"open"`
	Closed   []ThreadInfo      `json:"closed"`
	Problems []threads.Problem `json:"problems,omitempty"`
	Notes    []string          `json:"notes"`
}

// ThreadsOut is one board per project.
type ThreadsOut struct {
	Projects []ProjectBoard `json:"projects"`
}

type ThreadsArgs struct {
	ProjectArg
	ID string `json:"id,omitempty" jsonschema:"only this thread, by its id, its title, or the start of its title"`
}

func (s *Server) threads(ctx context.Context, req *mcp.CallToolRequest, a ThreadsArgs) (*mcp.CallToolResult, ThreadsOut, error) {
	pl, err := s.where()
	if err != nil {
		return nil, ThreadsOut{}, err
	}
	out := ThreadsOut{Projects: []ProjectBoard{}}
	var projects []*project.Project
	switch {
	case a.Project != "" || pl.InProject():
		p, _, err := s.projectOf(pl, a.Project)
		if err != nil {
			return nil, ThreadsOut{}, err
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
		board, err := threads.Load(p)
		if err != nil {
			return nil, ThreadsOut{}, err
		}
		pb := ProjectBoard{Name: p.Name(), Path: p.Root, Atlas: p.Atlas(), Board: p.Path(project.ThreadsIndex), Counts: board.Counts(now), Phases: []PhaseInfo{}, Problems: board.Problems, Notes: threads.Notes(p)}
		pb.Counts.Notes = len(pb.Notes)
		if pb.Notes == nil {
			pb.Notes = []string{}
		}
		for _, ph := range board.Phases {
			pb.Phases = append(pb.Phases, PhaseInfo{Title: ph.Title, Order: ph.Order, Finished: board.Finished(ph.Title), Open: len(board.In(ph.Title)), Path: ph.Path})
		}
		if a.ID != "" {
			t, err := board.Resolve(a.ID)
			if err != nil {
				if len(projects) > 1 {
					continue
				}
				return nil, ThreadsOut{}, err
			}
			one := threadInfos(p, []threads.Thread{*t}, now)
			pb.Open, pb.Closed = one, []ThreadInfo{}
			if t.Closed() {
				pb.Open, pb.Closed = []ThreadInfo{}, one
			}
		} else {
			pb.Open, pb.Closed = threadInfos(p, board.Open(), now), threadInfos(p, board.Closed(), now)
		}
		out.Projects = append(out.Projects, pb)
	}
	if a.ID != "" && len(out.Projects) == 0 {
		return nil, ThreadsOut{}, fmt.Errorf("no thread %q in any project of this knowledge base", a.ID)
	}
	return nil, out, nil
}

type ThreadArgs struct {
	ProjectArg
	ID       string  `json:"id,omitempty" jsonschema:"the thread, by its id, its title, or the start of its title; omit to open a new thread"`
	Title    *string `json:"title,omitempty" jsonschema:"a new thread's title, taken from the text when omitted; on an existing thread, a new title, which renames its card and documents"`
	Stage    string  `json:"stage,omitempty" jsonschema:"on an existing thread, the document to file: spec, plan, or receipt (stub, when the thread lost its own). Filing it moves the thread to that stage. A stage may be skipped"`
	Text     string  `json:"text,omitempty" jsonschema:"the document's text in markdown, without frontmatter: on a new thread the stub, in the user's words; with stage, that document. Revise a document that exists with Edit"`
	Outcome  string  `json:"outcome,omitempty" jsonschema:"with stage receipt: completed or killed"`
	Priority *string `json:"priority,omitempty" jsonschema:"high, normal, low, or someday; default normal"`
	Phase    *string `json:"phase,omitempty" jsonschema:"the phase the thread belongs to, by title; it must exist; an empty string clears it"`
	Blocked  *string `json:"blocked,omitempty" jsonschema:"what the thread waits on, in one line; an empty string unblocks it"`
	From     string  `json:"from,omitempty" jsonschema:"a new thread only: the note under atlas/<name>/inbox/ it comes from, relative to the project folder; its content is the stub when text is omitted, and it is removed once the stub exists"`
	Reopen   bool    `json:"reopen,omitempty" jsonschema:"delete a closed thread's receipt, which opens the thread again at the stage before it"`
}

// ThreadOut is the thread after the change.
type ThreadOut struct {
	ThreadInfo
	Project string `json:"project"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s *Server) thread(ctx context.Context, req *mcp.CallToolRequest, a ThreadArgs) (*mcp.CallToolResult, ThreadOut, error) {
	pl, err := s.where()
	if err != nil {
		return nil, ThreadOut{}, err
	}
	p, _, err := s.projectOf(pl, a.Project)
	if err != nil {
		return nil, ThreadOut{}, err
	}
	now := s.opts.Now()
	id := strings.TrimSpace(a.ID)
	var t *threads.Thread
	switch {
	case id == "":
		if a.Stage != "" && a.Stage != threads.Stub || a.Outcome != "" || a.Reopen {
			return nil, ThreadOut{}, errors.New("a new thread starts with its stub; pass id to file a later document on a thread")
		}
		t, err = threads.Start(p, threads.New{Title: deref(a.Title), Text: a.Text, Priority: deref(a.Priority), Phase: deref(a.Phase), From: a.From}, now)
		if err == nil && a.Blocked != nil {
			t, err = threads.Set(p, t.ID, threads.Changes{Blocked: a.Blocked}, now)
		}
	case a.From != "":
		return nil, ThreadOut{}, errors.New("from opens a new thread; omit id")
	default:
		ch := threads.Changes{Title: a.Title, Priority: a.Priority, Phase: a.Phase, Blocked: a.Blocked}
		if a.Stage == "" && a.Text != "" {
			return nil, ThreadOut{}, errors.New("text needs stage: name the document to file; revise one that exists with Edit")
		}
		if a.Reopen {
			if t, err = threads.Reopen(p, id, now); err != nil {
				return nil, ThreadOut{}, err
			}
			id = t.ID
		}
		if a.Stage != "" {
			if t, err = threads.File(p, id, threads.Filing{Stage: a.Stage, Text: a.Text, Outcome: a.Outcome}, now); err != nil {
				return nil, ThreadOut{}, err
			}
			id = t.ID
		} else if a.Outcome != "" {
			return nil, ThreadOut{}, errors.New("outcome needs stage: receipt")
		}
		// With nothing else to do, Set marks the thread as touched today.
		if !ch.Empty() || t == nil {
			t, err = threads.Set(p, id, ch, now)
		}
	}
	if err != nil {
		return nil, ThreadOut{}, err
	}
	return nil, ThreadOut{ThreadInfo: threadInfo(p, *t, now), Project: p.Name()}, nil
}

type PhaseArgs struct {
	ProjectArg
	Action   string `json:"action" jsonschema:"create, rename, reorder, or remove"`
	Title    string `json:"title" jsonschema:"the phase's title; on rename, the current one"`
	Goal     string `json:"goal,omitempty" jsonschema:"create: what the phase delivers, in the user's words"`
	Order    *int   `json:"order,omitempty" jsonschema:"create, reorder: its place in the timeline; create takes the next one when omitted"`
	NewTitle string `json:"new_title,omitempty" jsonschema:"rename: the new title; every thread that names the phase follows"`
}

// PhaseOut is the phase after the change, or what remove dropped.
type PhaseOut struct {
	Project string         `json:"project"`
	Phase   *threads.Phase `json:"phase,omitempty"`
	File    string         `json:"file,omitempty"`
	Removed string         `json:"removed,omitempty"`
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
	var ph *threads.Phase
	switch a.Action {
	case "create":
		ph, err = threads.CreatePhase(p, a.Title, a.Goal, a.Order, now)
	case "rename":
		ph, err = threads.RenamePhase(p, a.Title, a.NewTitle, now)
	case "reorder":
		if a.Order == nil {
			return nil, PhaseOut{}, errors.New("reorder needs order")
		}
		ph, err = threads.ReorderPhase(p, a.Title, *a.Order, now)
	case "remove":
		if err := threads.RemovePhase(p, a.Title, now); err != nil {
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
		Description: "Describe where the session is. In a project: its knowledge base, the page that describes it, its thread counts by stage, its phases, and warnings. In a knowledge base: mode, scope, page count, inbox, the projects that use it, git state, and warnings. Call this first."}, s.status)
	mcp.AddTool(server, &mcp.Tool{Name: "inbox", Annotations: ro(),
		Description: "List the files waiting in the knowledge base's inbox/ with size, kind, hash, and whether each already has a captured copy; in a project, the notes waiting in atlas/<name>/inbox/ too."}, s.inbox)
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
	mcp.AddTool(server, &mcp.Tool{Name: "threads", Annotations: ro(),
		Description: "List a project's threads: counts by stage, the phases in order, the open threads with the furthest stage first, the closed ones, the notes waiting in atlas/<name>/inbox/, and the pages it could not read. Each thread carries the absolute path of each of its documents. Pass id for one thread. In a knowledge base session with no project, every project that uses it."}, s.threads)
	mcp.AddTool(server, &mcp.Tool{Name: "thread",
		Description: "Open a thread or move one along. A thread is one line of work with a document per stage: stub, spec, plan, receipt. Without id: open a thread from text, which becomes its stub. With id and stage: file that stage's document from text, which moves the thread to that stage; a receipt needs outcome (completed or killed) and closes the thread. With id and priority, phase, blocked, or title: change its card. With id alone: mark it touched today. The stage is never set directly; it is the furthest document that exists."}, s.thread)
	mcp.AddTool(server, &mcp.Tool{Name: "phase",
		Description: "Create, rename, reorder, or remove a phase of a project: a named slice of the timeline that threads belong to. Rename follows every thread that names it; remove refuses while one does."}, s.phase)
	mcp.AddTool(server, &mcp.Tool{Name: "mode",
		Description: "Read the knowledge base's filing mode (generic or lyt) and the page types it files. Pass set to prepare a plan that changes it; apply that plan to make the change."}, s.mode)
	mcp.AddTool(server, &mcp.Tool{Name: "atlas",
		Description: "Read the whole atlas: every knowledge base with its scope, path, projects, and state; every project with its path, knowledge base, and open threads; the folders the atlas cannot read; and the settings. Pass refresh to also rewrite the registry."}, s.atlasTool)
	mcp.AddTool(server, &mcp.Tool{Name: "vault",
		Description: "Create or adopt a knowledge base, edit its name or scope, or forget one the atlas lists (the folder stays); action is create, adopt, edit, or forget. State the change and get a yes before calling."}, s.vaultTool)
	mcp.AddTool(server, &mcp.Tool{Name: "project",
		Description: "Make a folder a project (init), set or clear the knowledge base it uses (link, unlink), change its name or description (edit), or forget it (the folder and its atlas/<name>/ stay). State the change and get a yes before calling."}, s.projectTool)
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
	names := []string{"apply", "atlas", "capture", "history", "inbox", "lint", "mode", "phase", "plan", "project", "route", "settings", "stage", "status", "stub", "thread", "threads", "undo", "vault"}
	sort.Strings(names)
	return names
}
