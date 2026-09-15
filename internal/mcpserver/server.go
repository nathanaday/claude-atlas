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
	"github.com/nathanaday/claude-atlas/internal/discover"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/ledger"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/lint"
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
	// ProjectDir is where the session started; vault discovery walks up from it.
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

// resolve picks the vault: an explicit path, the environment, the nearest identity file,
// and last the atlas: a session started in a folder one project links uses that
// project's vault.
func (s *Server) resolve(explicit string) (*vault.Vault, error) {
	v, err := vault.Resolve(explicit, s.opts.Env(vault.EnvVault), s.opts.ProjectDir)
	if err == nil || explicit != "" || !errors.Is(err, vault.ErrNotVault) {
		return v, err
	}
	match, candidates, derr := discover.Vault(home.Resolve(s.opts.Env(home.EnvHome)), s.opts.ProjectDir)
	switch {
	case derr != nil:
		return nil, err
	case match != nil:
		return vault.Open(match.Project.Path)
	case len(candidates) > 1:
		return nil, fmt.Errorf("%s is linked by several atlas projects: %s; pass vault", s.opts.ProjectDir, discover.Describe(candidates))
	}
	return nil, err
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

// requireProject refuses what only a project has.
func requireProject(v *vault.Vault, what string) error {
	if v.Config.Kind == vault.Project {
		return nil
	}
	return fmt.Errorf("%s is a knowledge base and has no %s; sources, tasks, and repositories belong to a project that mounts it", v.Name(), what)
}

// VaultArg is the argument every tool shares.
type VaultArg struct {
	Vault string `json:"vault,omitempty" jsonschema:"absolute path of the vault; omit to use the session's vault"`
}

// Status is the status tool's output.
type Status struct {
	Vault         string         `json:"vault"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	ID            string         `json:"id"`
	Mode          string         `json:"mode"`
	Pages         int            `json:"pages"`
	InboxWaiting  int            `json:"inbox_waiting"`
	Git           txn.Status     `json:"git"`
	LastOperation *txn.Operation `json:"last_operation,omitempty"`
	Tasks         tasks.Counts   `json:"tasks"`
	// Repository is set when the session runs inside a repository the project mounts.
	Repository *RepoInfo `json:"repository,omitempty"`
	Versions   Versions  `json:"versions"`
	Warnings   []string  `json:"warnings"`
}

// RepoInfo describes a mounted repository and how changes land in it.
type RepoInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Remote  string `json:"remote,omitempty"`
	Changes string `json:"changes"`
	Policy  string `json:"policy"`
	Branch  string `json:"branch,omitempty"`
	Dirty   int    `json:"dirty"`
}

func repoInfo(r registry.Repo) RepoInfo {
	info := RepoInfo{Name: r.Name, Path: r.Path, Remote: r.Remote, Changes: links.Policy(r.Changes, r.Remote)}
	info.Policy = links.PolicyText(info.Changes)
	if r.Path != "" {
		if fact := links.Inspect(links.Repo, r.Path); fact.OK {
			info.Branch = fact.Branch
			if fact.Dirty != nil {
				info.Dirty = *fact.Dirty
			}
		}
	}
	return info
}

type Versions struct {
	Binary string `json:"binary"`
	Plugin string `json:"plugin,omitempty"`
}

func (s *Server) status(ctx context.Context, req *mcp.CallToolRequest, a VaultArg) (*mcp.CallToolResult, Status, error) {
	v, err := s.resolve(a.Vault)
	if err != nil {
		return nil, Status{}, err
	}
	out := Status{Vault: v.Root, Name: v.Name(), Kind: string(v.Config.Kind), ID: v.Config.ID, Mode: string(v.Config.Mode), Warnings: []string{}, Versions: Versions{Binary: s.opts.Version, Plugin: s.pluginVersion()}}
	st, err := txn.Inspect(v)
	if err != nil {
		return nil, Status{}, err
	}
	out.Git = *st
	if !st.HasHistory {
		out.Warnings = append(out.Warnings, "the vault has no git history; run `claude-atlas adopt "+v.Root+"`")
	}
	if st.Pending {
		out.Warnings = append(out.Warnings, "an operation was interrupted; run `claude-atlas recover "+v.Root+"` before changing the vault")
	}
	if report, err := lint.Run(v.Root, lint.Options{AsOf: s.opts.Now()}); err == nil {
		out.Pages = report.Summary.PagesScanned
	}
	if v.Config.Kind == vault.Project {
		if files, err := capture.ListInbox(v, s.opts.Now()); err == nil {
			for _, f := range files {
				if !f.Captured {
					out.InboxWaiting++
				}
			}
		}
	}
	if ops, err := txn.History(v, 1, false); err == nil && len(ops) > 0 {
		out.LastOperation = &ops[0]
	}
	if v.Config.Kind == vault.Project {
		if led, err := tasks.Current(v, s.opts.Now()); err == nil {
			out.Tasks = led.Counts(s.opts.Now())
			out.Tasks.Notes = len(tasks.Notes(v))
			var stale []string
			for _, r := range led.Open() {
				if tasks.Stale(r, s.opts.Now()) {
					stale = append(stale, r.Title)
				}
			}
			if len(stale) > 0 {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%d active task%s untouched for %d days: %s", len(stale), plural(len(stale)), tasks.StaleDays, strings.Join(stale, "; ")))
			}
			for _, p := range led.Problems {
				out.Warnings = append(out.Warnings, "task page "+p.Path+": "+p.Reason)
			}
			if out.Tasks.Notes > 0 {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%d task note%s wait in %s/; the task-plant skill turns them into tasks", out.Tasks.Notes, plural(out.Tasks.Notes), vault.InboxTasksDir))
			}
		}
	}
	if match, _, err := discover.Vault(home.Resolve(s.opts.Env(home.EnvHome)), s.opts.ProjectDir); err == nil && match != nil && match.Project.Path == v.Root {
		info := repoInfo(match.Repo)
		out.Repository = &info
	}
	if out.Versions.Plugin != "" && out.Versions.Binary != "dev" && out.Versions.Plugin != out.Versions.Binary {
		out.Warnings = append(out.Warnings, fmt.Sprintf("plugin %s and binary %s differ; update one of them", out.Versions.Plugin, out.Versions.Binary))
	}
	return nil, out, nil
}

type InboxOut struct {
	Files []capture.InboxFile `json:"files"`
}

func (s *Server) inbox(ctx context.Context, req *mcp.CallToolRequest, a VaultArg) (*mcp.CallToolResult, InboxOut, error) {
	v, err := s.resolve(a.Vault)
	if err != nil {
		return nil, InboxOut{}, err
	}
	if err := requireProject(v, "inbox"); err != nil {
		return nil, InboxOut{}, err
	}
	files, err := capture.ListInbox(v, s.opts.Now())
	if err != nil {
		return nil, InboxOut{}, err
	}
	return nil, InboxOut{Files: files}, nil
}

type CaptureArgs struct {
	VaultArg
	Paths []string `json:"paths" jsonschema:"files in inbox/ to capture, as inbox-relative or vault-relative paths"`
}

func (s *Server) capture(ctx context.Context, req *mcp.CallToolRequest, a CaptureArgs) (*mcp.CallToolResult, capture.Result, error) {
	v, err := s.resolve(a.Vault)
	if err != nil {
		return nil, capture.Result{}, err
	}
	if err := requireProject(v, "inbox"); err != nil {
		return nil, capture.Result{}, err
	}
	res, err := capture.Capture(v, a.Paths, s.opts.Now())
	if err != nil {
		return nil, capture.Result{}, err
	}
	return nil, *res, nil
}

type RouteArgs struct {
	VaultArg
	Type  string `json:"type" jsonschema:"page type: source, entity, concept; in a project also question, session; in lyt mode also note or moc"`
	Title string `json:"title" jsonschema:"the page title; it becomes the file name"`
}

func (s *Server) route(ctx context.Context, req *mcp.CallToolRequest, a RouteArgs) (*mcp.CallToolResult, vault.Route, error) {
	v, err := s.resolve(a.Vault)
	if err != nil {
		return nil, vault.Route{}, err
	}
	if a.Type == "task" {
		if err := requireProject(v, "tasks"); err != nil {
			return nil, vault.Route{}, err
		}
		now := s.opts.Now()
		plain := vault.TasksDir + "/" + vault.SanitizeTitle(a.Title) + ".md"
		r := vault.Route{Path: plain, Type: "task", Mode: v.Config.Mode, Skeleton: tasks.Skeleton(tasks.Plant{Title: a.Title}, tasks.NewID(now), now)}
		if _, err := os.Stat(v.Path(plain)); err == nil {
			r.Exists = true
		}
		return nil, r, nil
	}
	r, err := v.RouteFor(a.Type, a.Title, s.opts.Now())
	if err != nil {
		return nil, vault.Route{}, err
	}
	return nil, *r, nil
}

type PlantArgs struct {
	VaultArg
	Title    string `json:"title,omitempty" jsonschema:"the task's title; taken from the text when omitted"`
	Text     string `json:"text,omitempty" jsonschema:"the idea in the user's words; kept verbatim on the page"`
	Priority string `json:"priority,omitempty" jsonschema:"high, normal, low, or someday; default normal"`
	Workdir  string `json:"workdir,omitempty" jsonschema:"the folder the work happens in, usually a linked repository"`
	Due      string `json:"due,omitempty" jsonschema:"YYYY-MM-DD"`
	From     string `json:"from,omitempty" jsonschema:"the note under inbox/tasks/ this task comes from; it is removed in the same commit"`
}

type PlantOut struct {
	txn.Planted
	OperationID string `json:"operation_id"`
	Commit      string `json:"commit"`
}

func (s *Server) plant(ctx context.Context, req *mcp.CallToolRequest, a PlantArgs) (*mcp.CallToolResult, PlantOut, error) {
	v, err := s.resolve(a.Vault)
	if err != nil {
		return nil, PlantOut{}, err
	}
	if err := requireProject(v, "tasks"); err != nil {
		return nil, PlantOut{}, err
	}
	now := s.opts.Now()
	request, planted, err := txn.PlantRequest(v, tasks.Plant{Title: a.Title, Text: a.Text, Priority: a.Priority, Workdir: a.Workdir, Due: a.Due}, a.From, now)
	if err != nil {
		return nil, PlantOut{}, err
	}
	plan, err := txn.Prepare(v, request, now)
	if err != nil {
		return nil, PlantOut{}, err
	}
	res, err := txn.Apply(v, plan, now)
	if err != nil {
		return nil, PlantOut{}, err
	}
	return nil, PlantOut{Planted: planted, OperationID: res.OperationID, Commit: res.Commit}, nil
}

type StubArgs struct {
	VaultArg
	Titles []txn.StubTitle `json:"titles,omitempty" jsonschema:"the pages to stub; omit to stub every wanted page and every empty page a link points to"`
	Type   string          `json:"type,omitempty" jsonschema:"the type for titles that name none; default concept, or note in lyt mode"`
}

func (s *Server) stub(ctx context.Context, req *mcp.CallToolRequest, a StubArgs) (*mcp.CallToolResult, txn.StubResult, error) {
	v, err := s.resolve(a.Vault)
	if err != nil {
		return nil, txn.StubResult{}, err
	}
	res, err := txn.StubPages(v, a.Titles, a.Type, s.opts.Now())
	if err != nil {
		return nil, txn.StubResult{}, err
	}
	return nil, res, nil
}

type TasksArgs struct {
	VaultArg
	Status string `json:"status,omitempty" jsonschema:"only tasks with this status"`
	All    bool   `json:"all,omitempty" jsonschema:"include done and cancelled tasks"`
}

type TasksOut struct {
	Counts   tasks.Counts    `json:"counts"`
	Tasks    []tasks.Record  `json:"tasks"`
	Notes    []string        `json:"notes"`
	Problems []tasks.Problem `json:"problems,omitempty"`
}

func (s *Server) tasks(ctx context.Context, req *mcp.CallToolRequest, a TasksArgs) (*mcp.CallToolResult, TasksOut, error) {
	v, err := s.resolve(a.Vault)
	if err != nil {
		return nil, TasksOut{}, err
	}
	if err := requireProject(v, "tasks"); err != nil {
		return nil, TasksOut{}, err
	}
	now := s.opts.Now()
	led, err := tasks.Current(v, now)
	if err != nil {
		return nil, TasksOut{}, err
	}
	out := TasksOut{Counts: led.Counts(now), Tasks: []tasks.Record{}, Notes: tasks.Notes(v), Problems: led.Problems}
	out.Counts.Notes = len(out.Notes)
	list := led.Open()
	if a.All {
		list = append(list, led.Archived()...)
	}
	for _, r := range list {
		if a.Status == "" || r.Status == a.Status {
			out.Tasks = append(out.Tasks, r)
		}
	}
	if out.Notes == nil {
		out.Notes = []string{}
	}
	return nil, out, nil
}

type ReposOut struct {
	Repos []RepoInfo `json:"repos"`
}

func (s *Server) repos(ctx context.Context, req *mcp.CallToolRequest, a VaultArg) (*mcp.CallToolResult, ReposOut, error) {
	v, err := s.resolve(a.Vault)
	if err != nil {
		return nil, ReposOut{}, err
	}
	if err := requireProject(v, "repositories"); err != nil {
		return nil, ReposOut{}, err
	}
	repos, err := discover.Repos(home.Resolve(s.opts.Env(home.EnvHome)), v.Root)
	if err != nil {
		return nil, ReposOut{}, err
	}
	out := ReposOut{Repos: []RepoInfo{}}
	for _, r := range repos {
		out.Repos = append(out.Repos, repoInfo(r))
	}
	return nil, out, nil
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
	VaultArg
	Kind    string       `json:"kind" jsonschema:"ingest, save, markdown, repair, fold, canvas, base, or config"`
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
	v, err := s.resolve(a.Vault)
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
		return nil, PlanOut{}, fmt.Errorf("kind must be one of ingest, save, markdown, repair, fold, canvas, base, config")
	}
	if v.Config.Kind == vault.Knowledge && (kind == txn.Ingest || kind == txn.Save) {
		return nil, PlanOut{}, fmt.Errorf("knowledge enters through a project: %s is a knowledge base, so ingest and save run in a project session that mounts it", v.Name())
	}
	r := txn.Request{Kind: kind, Summary: a.Summary}
	for _, w := range a.Writes {
		r.Writes = append(r.Writes, txn.Write{Path: w.Path, Mode: txn.WriteMode(w.Mode), Content: []byte(w.Content), BaseSHA256: w.BaseSHA256})
	}
	for _, src := range a.Sources {
		r.Sources = append(r.Sources, ledger.Update{ID: src.ID, Ingested: src.Ingested, Pages: src.Pages, Authority: src.Authority, Title: src.Title, Notes: src.Notes})
	}
	if kind == txn.Config {
		return nil, PlanOut{}, errors.New("to change the mode, call the mode tool with set")
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
		return nil, txn.Result{}, fmt.Errorf("no plan %q is pending; plans are single-use and the newest plan for a vault replaces older ones, so call plan again", a.PlanID)
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
	VaultArg
	OperationID string `json:"operation_id" jsonschema:"the operation to revert, from history"`
}

func (s *Server) undo(ctx context.Context, req *mcp.CallToolRequest, a UndoArgs) (*mcp.CallToolResult, txn.Result, error) {
	v, err := s.resolve(a.Vault)
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
	VaultArg
	Limit int `json:"limit,omitempty" jsonschema:"how many operations, newest first (default 10)"`
}

type HistoryOut struct {
	Operations []txn.Operation `json:"operations"`
}

func (s *Server) history(ctx context.Context, req *mcp.CallToolRequest, a HistoryArgs) (*mcp.CallToolResult, HistoryOut, error) {
	v, err := s.resolve(a.Vault)
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
	return nil, HistoryOut{Operations: ops}, nil
}

type LintArgs struct {
	VaultArg
	Exclude []string `json:"exclude,omitempty" jsonschema:"path globs to leave out, e.g. wiki/scratch/*"`
}

func (s *Server) lint(ctx context.Context, req *mcp.CallToolRequest, a LintArgs) (*mcp.CallToolResult, lint.Report, error) {
	v, err := s.resolve(a.Vault)
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
	VaultArg
	Set string `json:"set,omitempty" jsonschema:"generic or lyt; omit to read the current mode"`
}

type ModeOut struct {
	Mode     string   `json:"mode"`
	Types    []string `json:"types"`
	Plan     *PlanOut `json:"plan,omitempty"`
	Previous string   `json:"previous,omitempty"`
}

func (s *Server) mode(ctx context.Context, req *mcp.CallToolRequest, a ModeArgs) (*mcp.CallToolResult, ModeOut, error) {
	v, err := s.resolve(a.Vault)
	if err != nil {
		return nil, ModeOut{}, err
	}
	out := ModeOut{Mode: string(v.Config.Mode), Types: vault.RoutableTypes(v.Config.Kind, v.Config.Mode)}
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
	out.Types = vault.RoutableTypes(v.Config.Kind, mode)
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
		Description: "Describe the current vault: kind (knowledge base or project), path, mode, page count, files waiting in the inbox, git state, last operation, and warnings. Call this first."}, s.status)
	mcp.AddTool(server, &mcp.Tool{Name: "inbox", Annotations: ro(),
		Description: "List files waiting in inbox/ with size, kind, hash, and whether each already has a captured copy and source id."}, s.inbox)
	mcp.AddTool(server, &mcp.Tool{Name: "capture",
		Description: "Copy inbox files into the immutable raw store and record them in the source ledger, as one commit. Returns each file's source id and stored path. Read the stored file afterwards with Read."}, s.capture)
	mcp.AddTool(server, &mcp.Tool{Name: "route", Annotations: ro(),
		Description: "Say where a new page of a type belongs under the vault's mode, whether a page with that title already exists, and give a skeleton with the vault's frontmatter conventions."}, s.route)
	mcp.AddTool(server, &mcp.Tool{Name: "plan", Annotations: ro(),
		Description: "Validate a set of file changes against the vault and hold them as a plan. Returns a plan_id, a preview of creates, replaces, and deletes, and warnings such as links that do not resolve. Nothing is written. Show the preview to the user before apply."}, s.plan)
	mcp.AddTool(server, &mcp.Tool{Name: "apply",
		Description: "Apply a held plan as one git commit and write its log entry. Hand edits made outside atlas are committed first, so the operation can always be undone exactly. The plan is consumed."}, s.apply)
	mcp.AddTool(server, &mcp.Tool{Name: "undo",
		Description: "Revert one applied operation as a new commit. Fails if later changes overlap it."}, s.undo)
	mcp.AddTool(server, &mcp.Tool{Name: "history", Annotations: ro(),
		Description: "List recent operations, newest first, with their kind, summary, date, commit, and changed paths."}, s.history)
	mcp.AddTool(server, &mcp.Tool{Name: "lint", Annotations: ro(),
		Description: "Run the deterministic wiki health check: dead and ambiguous links, duplicate basenames, orphans, pages missing from every index, missing frontmatter, empty sections, stale index entries, and ledger problems. Read-only."}, s.lint)
	mcp.AddTool(server, &mcp.Tool{Name: "plant",
		Description: "Plant a task: create a task page with status planted from a title and the idea's text, as one commit. Give from to remove the inbox/tasks/ note it came from. No plan preview is needed; undo covers it."}, s.plant)
	mcp.AddTool(server, &mcp.Tool{Name: "stub",
		Description: "Create seed pages for the pages the wiki links to but nobody has written (lint's wanted pages), and give frontmatter to the empty pages a link points to, as one commit. Omit titles to stub all of them with the mode's default type; pass titles with a type when a name is a person, product, project, or organization (entity). No plan preview is needed; undo covers it."}, s.stub)
	mcp.AddTool(server, &mcp.Tool{Name: "tasks", Annotations: ro(),
		Description: "List the vault's tasks from the task ledger: open ones by status, priority, and age, with each task's page, workdir, last touch, and history; counts; and the notes waiting in inbox/tasks/. Pass all to include finished tasks."}, s.tasks)
	mcp.AddTool(server, &mcp.Tool{Name: "repos", Annotations: ro(),
		Description: "List the repositories mounted on the vault's project: path, remote, branch, uncommitted changes, and how changes land there (pr: branch and pull request; commit: on the current branch). Read it before changing files in a repository."}, s.repos)
	mcp.AddTool(server, &mcp.Tool{Name: "mode",
		Description: "Read the vault's filing mode (generic or lyt) and the page types it files. Pass set to prepare a plan that changes it; apply that plan to make the change."}, s.mode)
	return server
}

// Run serves over stdio until the client disconnects.
func Run(ctx context.Context, opts Options) error {
	return New(opts).MCP().Run(ctx, &mcp.StdioTransport{})
}

// ToolNames lists the tools, for docs and tests.
func ToolNames() []string {
	names := []string{"status", "inbox", "capture", "route", "plan", "apply", "undo", "history", "lint", "mode"}
	sort.Strings(names)
	return names
}

var _ = strings.TrimSpace
