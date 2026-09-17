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

// home is the atlas home the session reads.
func (s *Server) home() home.Home { return home.Resolve(s.opts.Env(home.EnvHome)) }

// session is what the server knows about one tool call's vaults: the vault the call
// acts on, the session's own project (nil in a knowledge base session or with no atlas),
// and, when the target is a knowledge base mounted by that project, the mount's
// effective access.
type session struct {
	target *vault.Vault
	// project is whose mounts this call answers for: the target itself when it is a
	// project, otherwise the session's own project from the working directory or
	// CLAUDE_ATLAS_VAULT.
	project *registry.Entry
	mount   *registry.Mount // the project's mount of target, when target is a knowledge base
	// entry is the target's own registry entry, and ix the scan both come from. Both are
	// nil with no atlas or a failed scan. No read-only tool depends on them.
	entry *registry.Entry
	ix    *registry.Index
	// mountPaths is what mounts returns.
	mountPaths map[string]string
}

// open resolves the target (explicit, env, nearest identity file, then discovery) and
// the session's project (the same resolution with no explicit vault), then the mount.
func (s *Server) open(explicit string) (*session, error) {
	target, err := s.resolve(explicit)
	if err != nil {
		return nil, err
	}
	sess := &session{target: target}
	cfg, err := s.home().Load()
	if err != nil {
		return sess, nil
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return sess, nil
	}
	sess.ix = ix
	sess.entry = dropUnreadable(ix.ByPath(target.Root))
	if target.Config.Kind == vault.Project {
		sess.project = sess.entry
		sess.mountPaths = mountPaths(sess.project)
		return sess, nil
	}
	sess.project = s.sessionProject(ix)
	if sess.project == nil {
		return sess, nil
	}
	for i := range sess.project.Mounts {
		if sess.project.Mounts[i].ID == target.Config.ID {
			sess.mount = &sess.project.Mounts[i]
			break
		}
	}
	return sess, nil
}

// sessionProject is the project the session started in: the vault the environment or the
// working directory names, or the project whose repository holds that directory.
func (s *Server) sessionProject(ix *registry.Index) *registry.Entry {
	if v, err := vault.Resolve("", s.opts.Env(vault.EnvVault), s.opts.ProjectDir); err == nil {
		if v.Config.Kind != vault.Project {
			return nil
		}
		return dropUnreadable(ix.ByPath(v.Root))
	}
	match, _, err := discover.Vault(s.home(), s.opts.ProjectDir)
	if err != nil || match == nil {
		return nil
	}
	return dropUnreadable(ix.ByPath(match.Project.Path))
}

// dropUnreadable returns nil for an entry the scan found but could not read.
func dropUnreadable(e *registry.Entry) *registry.Entry {
	if e == nil || e.Error != "" {
		return nil
	}
	return e
}

// mountPaths maps each resolved mount's name to the knowledge base wiki it leads to.
func mountPaths(project *registry.Entry) map[string]string {
	if project == nil {
		return nil
	}
	paths := map[string]string{}
	for _, m := range project.Mounts {
		if m.Error == "" && m.Path != "" {
			paths[m.Name] = m.Path
		}
	}
	return paths
}

// mounts is what lint reads instead of the symlinks under kb/, so a project whose links
// refresh has not made yet still resolves into its knowledge bases. It is nil unless the
// target is the project the mounts belong to; a knowledge base has none.
func (sess *session) mounts() map[string]string { return sess.mountPaths }

// writable says whether a page-writing kind may run: in a project, always; in a
// knowledge base, only through a project session whose mount is effectively write.
// It returns the refusal to show the model.
func (sess *session) writable(kind txn.Kind) error {
	if sess.target.Config.Kind != vault.Knowledge {
		return nil
	}
	kb := sess.target.Name()
	if sess.project == nil {
		if entersThroughAProject(kind) {
			return fmt.Errorf("knowledge enters through a project: %s is a knowledge base; run this in a project that mounts it (%s)", kb, sess.mountedBy())
		}
		return nil
	}
	switch {
	case sess.mount == nil && sess.entry == nil:
		// mount takes a name or a path, and the scan does not hold this knowledge base.
		return fmt.Errorf("the atlas does not know %s; run `claude-atlas adopt %s`, then `claude-atlas mount %s %s`", kb, sess.target.Root, sess.project.Path, sess.target.Root)
	case sess.mount == nil:
		return fmt.Errorf("%s does not mount %s; run `claude-atlas mount %s %s`", sess.project.Name, kb, sess.project.Path, sess.target.Root)
	case sess.mount.Error != "":
		return fmt.Errorf("mount %s: %s", sess.mount.Name, sess.mount.Error)
	case sess.mount.Effective != vault.AccessWrite:
		return fmt.Errorf("%s mounts %s read-only", sess.project.Name, kb)
	}
	return nil
}

// entersThroughAProject names the kinds that bring knowledge in. They need a project
// session even when there is no mount to check.
func entersThroughAProject(kind txn.Kind) bool {
	return kind == txn.Ingest || kind == txn.Save || kind == txn.Capture
}

// mountedBy names the projects that mount the target: "mounted by: a, b", or the phrase
// for a knowledge base nobody mounts.
func (sess *session) mountedBy() string {
	var names []string
	if sess.entry != nil {
		for _, ref := range sess.entry.MountedBy {
			names = append(names, ref.Name)
		}
	}
	if len(names) == 0 {
		return "nothing mounts it yet"
	}
	return "mounted by: " + strings.Join(names, ", ")
}

// unknownVault says the scan does not hold the target, so its mounts cannot resolve.
func (sess *session) unknownVault() error {
	return fmt.Errorf("the atlas does not know %s, so its mounts do not resolve; run `claude-atlas adopt %s`", sess.target.Name(), sess.target.Root)
}

// mountNamed finds the session project's mount by name and refuses one it may not write.
func (sess *session) mountNamed(name string) (*registry.Mount, error) {
	if sess.project == nil {
		return nil, sess.unknownVault()
	}
	for i := range sess.project.Mounts {
		m := &sess.project.Mounts[i]
		if !strings.EqualFold(m.Name, name) {
			continue
		}
		switch {
		case m.Error != "":
			return nil, fmt.Errorf("mount %s: %s", m.Name, m.Error)
		case m.Effective != vault.AccessWrite:
			return nil, fmt.Errorf("%s mounts %s read-only", sess.project.Name, m.Name)
		}
		return m, nil
	}
	return nil, fmt.Errorf("no mount named %q; the mounts tool lists them", name)
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
	// Mounts are a project's knowledge bases; Access and MountedBy are a knowledge base's
	// own access and the projects that mount it.
	Mounts    []MountInfo `json:"mounts,omitempty"`
	Access    string      `json:"access,omitempty"`
	MountedBy []MountedBy `json:"mounted_by,omitempty"`
	// Repository is set when the session runs inside a repository the project mounts.
	Repository *RepoInfo `json:"repository,omitempty"`
	Versions   Versions  `json:"versions"`
	Warnings   []string  `json:"warnings"`
}

// MountInfo is one knowledge base a project mounts.
type MountInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	Link      string `json:"link"`
	Access    string `json:"access"`
	Effective string `json:"effective,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Pages     *int   `json:"pages,omitempty"`
	Error     string `json:"error,omitempty"`
	// Through is the cluster this knowledge base came through; empty when the project
	// mounts it directly. Every member of a cluster is reached the same way as any mount.
	Through string `json:"through,omitempty"`
}

// MountedBy is one project that mounts a knowledge base, with its effective access.
type MountedBy struct {
	Name   string `json:"name"`
	Access string `json:"access"`
}

// MountsOut is the mounts tool's output.
type MountsOut struct {
	Mounts []MountInfo `json:"mounts"`
}

// mountInfo describes one mount. pages counts the knowledge base's pages, which costs a
// lint run, so status leaves it out.
func (sess *session) mountInfo(m registry.Mount, pages bool, now time.Time) MountInfo {
	info := MountInfo{ID: m.ID, Name: m.Name, Path: m.Path, Link: vault.KbDir + "/" + m.Name, Access: m.Access, Effective: m.Effective, Error: m.Error, Through: m.Through}
	if sess.ix != nil {
		if kb := sess.ix.ByID(m.ID); kb != nil {
			info.Scope = kb.Scope
		}
	}
	if pages && m.Path != "" {
		if report, err := lint.Run(filepath.Dir(m.Path), lint.Options{AsOf: now}); err == nil {
			n := report.Summary.PagesScanned
			info.Pages = &n
		}
	}
	return info
}

// mountList describes every mount of the session's project.
func (sess *session) mountList(pages bool, now time.Time) []MountInfo {
	out := []MountInfo{}
	if sess.project == nil {
		return out
	}
	for _, m := range sess.project.Mounts {
		out = append(out, sess.mountInfo(m, pages, now))
	}
	return out
}

func (s *Server) mounts(ctx context.Context, req *mcp.CallToolRequest, a VaultArg) (*mcp.CallToolResult, MountsOut, error) {
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, MountsOut{}, err
	}
	if sess.target.Config.Kind != vault.Project {
		return nil, MountsOut{}, fmt.Errorf("a knowledge base has no mounts; %s", sess.mountedBy())
	}
	if sess.project == nil {
		return nil, MountsOut{}, sess.unknownVault()
	}
	return nil, MountsOut{Mounts: sess.mountList(true, s.opts.Now())}, nil
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
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, Status{}, err
	}
	v := sess.target
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
	if report, err := lint.Run(v.Root, lint.Options{AsOf: s.opts.Now(), Mounts: sess.mounts()}); err == nil {
		out.Pages = report.Summary.PagesScanned
	}
	if v.Config.Kind == vault.Project {
		if files, err := capture.ListInbox(v, sess.mounts(), s.opts.Now()); err == nil {
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
	if v.Config.Kind == vault.Project {
		out.Mounts = sess.mountList(false, s.opts.Now())
	} else {
		out.Access = v.Config.Access
		out.MountedBy = []MountedBy{}
		if sess.entry != nil {
			for _, ref := range sess.entry.MountedBy {
				out.MountedBy = append(out.MountedBy, MountedBy{Name: ref.Name, Access: ref.Access})
			}
		}
	}
	if match, _, err := discover.Vault(s.home(), s.opts.ProjectDir); err == nil && match != nil && match.Project.Path == v.Root {
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
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, InboxOut{}, err
	}
	v := sess.target
	if err := requireProject(v, "inbox"); err != nil {
		return nil, InboxOut{}, err
	}
	files, err := capture.ListInbox(v, sess.mounts(), s.opts.Now())
	if err != nil {
		return nil, InboxOut{}, err
	}
	return nil, InboxOut{Files: files}, nil
}

type CaptureArgs struct {
	VaultArg
	Paths []string `json:"paths" jsonschema:"files in the project's inbox/ to capture, as inbox-relative or vault-relative paths"`
}

func (s *Server) capture(ctx context.Context, req *mcp.CallToolRequest, a CaptureArgs) (*mcp.CallToolResult, capture.Result, error) {
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, capture.Result{}, err
	}
	now := s.opts.Now()
	if sess.target.Config.Kind == vault.Project {
		res, err := capture.Capture(sess.target, a.Paths, now)
		if err != nil {
			return nil, capture.Result{}, err
		}
		return nil, *res, nil
	}
	if err := sess.writable(txn.Capture); err != nil {
		return nil, capture.Result{}, err
	}
	project, err := vault.Open(sess.project.Path)
	if err != nil {
		return nil, capture.Result{}, err
	}
	res, err := capture.CaptureFrom(sess.target, project, a.Paths, ledger.Via{ID: sess.project.ID, Name: sess.project.Name}, now)
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

// routeNext tells the model what a match means: a reason to link, not to duplicate.
const routeNext = "A match anywhere means link to it instead of creating a page."

// RouteOut is where a page belongs and whether one by that title or alias already
// exists, in the target vault and, for a project, in every mount.
type RouteOut struct {
	vault.Route
	Vault  string       `json:"vault"`
	Match  *vault.Match `json:"match,omitempty"`
	Mounts []MountRoute `json:"mounts,omitempty"`
	Next   string       `json:"next"`
}

// MountRoute is one mount's answer to the same route question.
type MountRoute struct {
	Name      string       `json:"name"`
	Vault     string       `json:"vault,omitempty"`
	Effective string       `json:"effective,omitempty"`
	Path      string       `json:"path,omitempty"`
	Match     *vault.Match `json:"match,omitempty"`
	Error     string       `json:"error,omitempty"`
}

func (s *Server) route(ctx context.Context, req *mcp.CallToolRequest, a RouteArgs) (*mcp.CallToolResult, RouteOut, error) {
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, RouteOut{}, err
	}
	v := sess.target
	if a.Type == "task" {
		if err := requireProject(v, "tasks"); err != nil {
			return nil, RouteOut{}, err
		}
		now := s.opts.Now()
		plain := vault.TasksDir + "/" + vault.SanitizeTitle(a.Title) + ".md"
		r := vault.Route{Path: plain, Type: "task", Mode: v.Config.Mode, Skeleton: tasks.Skeleton(tasks.Plant{Title: a.Title}, tasks.NewID(now), now)}
		if _, err := os.Stat(v.Path(plain)); err == nil {
			r.Exists = true
		}
		return nil, RouteOut{Route: r, Vault: v.Root, Next: routeNext}, nil
	}
	r, err := v.RouteFor(a.Type, a.Title, s.opts.Now())
	if err != nil {
		return nil, RouteOut{}, err
	}
	match, err := vault.FindPage(v.Root, a.Title)
	if err != nil {
		return nil, RouteOut{}, err
	}
	out := RouteOut{Route: *r, Vault: v.Root, Match: match, Next: routeNext}
	if sess.project != nil && v.Config.Kind == vault.Project {
		now := s.opts.Now()
		for _, m := range sess.project.Mounts {
			out.Mounts = append(out.Mounts, mountRoute(m, a.Type, a.Title, now))
		}
	}
	return nil, out, nil
}

// mountRoute answers the route question against one mount: an unresolved mount carries
// its error; a resolved one reports whether the title or an alias already exists there,
// and where a new page would go when the mount is writable and the type is filed there.
func mountRoute(m registry.Mount, pageType, title string, now time.Time) MountRoute {
	mr := MountRoute{Name: m.Name, Effective: m.Effective}
	if m.Error != "" {
		mr.Error = m.Error
		return mr
	}
	kb, err := vault.Open(filepath.Dir(m.Path))
	if err != nil {
		mr.Error = err.Error()
		return mr
	}
	mr.Vault = kb.Root
	filed := false
	for _, t := range vault.RoutableTypes(vault.Knowledge, kb.Config.Mode) {
		if t == pageType {
			filed = true
			break
		}
	}
	if !filed {
		return mr
	}
	match, err := vault.FindPage(kb.Root, title)
	if err != nil {
		mr.Error = err.Error()
		return mr
	}
	mr.Match = match
	if m.Effective != vault.AccessWrite {
		return mr
	}
	if r, err := kb.RouteFor(pageType, title, now); err == nil {
		mr.Path = r.Path
	}
	return mr
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
	Type   string          `json:"type,omitempty" jsonschema:"the type for titles that name none: concept or entity; in a project also question or session; in lyt mode note or moc as well; the default is concept, or note in lyt mode"`
}

// StubOp is one operation a stub call made, in the vault it was committed in.
type StubOp struct {
	Vault       string `json:"vault"`
	OperationID string `json:"operation_id"`
	Commit      string `json:"commit"`
}

// StubOut lists the stubs and the operations that wrote them. A stub that landed in a
// mounted knowledge base carries its path through the mount (kb/<name>/concepts/X.md)
// and was committed in that knowledge base, which operations names with its root. The
// top-level operation_id and commit name the operation in the session's own vault, and
// stay empty when nothing committed there; operations carries the rest.
type StubOut struct {
	txn.StubResult
	Operations []StubOp `json:"operations,omitempty"`
}

// stubGroup is the titles one mounted knowledge base takes.
type stubGroup struct {
	mount  *registry.Mount
	kb     *vault.Vault
	titles []txn.StubTitle
}

func (s *Server) stub(ctx context.Context, req *mcp.CallToolRequest, a StubArgs) (*mcp.CallToolResult, StubOut, error) {
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, StubOut{}, err
	}
	now := s.opts.Now()
	// Every mount a title names is resolved and checked before the first write, so no
	// operation lands in one vault while another is still refused.
	own, groups, err := sess.groupStubs(a.Titles)
	if err != nil {
		return nil, StubOut{}, err
	}
	out := StubOut{StubResult: txn.StubResult{Stubs: []txn.Stubbed{}}}
	if len(a.Titles) == 0 || len(own) > 0 {
		if err := sess.writable(txn.Stub); err != nil {
			return nil, StubOut{}, err
		}
		res, err := sess.stubHere(own, a.Type, now)
		if err != nil {
			return nil, StubOut{}, err
		}
		out.Stubs = append(out.Stubs, res.Stubs...)
		out.Skipped = append(out.Skipped, res.Skipped...)
		out.add(sess.target.Root, res)
		out.OperationID, out.Commit = res.OperationID, res.Commit
	}
	for _, g := range groups {
		res, err := txn.StubInto(sess.target, g.kb, g.titles, a.Type, sess.project.Name, sess.mounts(), now)
		if err != nil {
			return nil, out, out.receipt(err)
		}
		for _, stubbed := range res.Stubs {
			stubbed.Path = mountPath(g.mount.Name, stubbed.Path)
			out.Stubs = append(out.Stubs, stubbed)
		}
		out.add(g.kb.Root, res)
	}
	return nil, out, nil
}

// stubHere stubs the target vault's own wanted pages. A project session that reaches a
// mounted knowledge base records the project it came through, as capture does; the pages
// are still the knowledge base's own. A title that only the project links goes to a
// mount through its target, not here.
func (sess *session) stubHere(titles []txn.StubTitle, defaultType string, now time.Time) (txn.StubResult, error) {
	if sess.target.Config.Kind == vault.Knowledge && sess.project != nil {
		return txn.StubInto(sess.target, sess.target, titles, defaultType, sess.project.Name, sess.mounts(), now)
	}
	return txn.StubPages(sess.target, titles, defaultType, sess.mounts(), now)
}

// add records one operation.
func (out *StubOut) add(root string, res txn.StubResult) {
	if res.OperationID == "" {
		return
	}
	out.Operations = append(out.Operations, StubOp{Vault: root, OperationID: res.OperationID, Commit: res.Commit})
}

// receipt names the operations that already committed, so a failure halfway through says
// what the model can still undo.
func (out *StubOut) receipt(err error) error {
	if len(out.Operations) == 0 {
		return err
	}
	var parts []string
	for _, op := range out.Operations {
		parts = append(parts, fmt.Sprintf("operation %s in %s (commit %s)", op.OperationID, op.Vault, op.Commit))
	}
	return fmt.Errorf("%w; already committed: %s", err, strings.Join(parts, "; "))
}

// groupStubs splits titles into the session vault's own and those a mount takes, resolving
// each mount name to the mount it belongs to. Two spellings of one mount name make one
// group, so one knowledge base gets one operation.
func (sess *session) groupStubs(titles []txn.StubTitle) (own []txn.StubTitle, groups []*stubGroup, err error) {
	byMount := map[string]*stubGroup{}
	for _, t := range titles {
		name := strings.TrimSpace(t.Target)
		if name == "" {
			own = append(own, t)
			continue
		}
		if sess.target.Config.Kind != vault.Project {
			return nil, nil, fmt.Errorf("only a project's stub names a mount; %s is a knowledge base", sess.target.Name())
		}
		m, err := sess.mountNamed(name)
		if err != nil {
			return nil, nil, err
		}
		g := byMount[strings.ToLower(m.Name)]
		if g == nil {
			kb, err := vault.Open(filepath.Dir(m.Path))
			if err != nil {
				return nil, nil, err
			}
			g = &stubGroup{mount: m, kb: kb}
			byMount[strings.ToLower(m.Name)] = g
			groups = append(groups, g)
		}
		g.titles = append(g.titles, t)
	}
	return own, groups, nil
}

// mountPath is where the project reads a knowledge base page: through the mount's link.
func mountPath(name, kbPath string) string {
	return vault.KbDir + "/" + name + "/" + strings.TrimPrefix(kbPath, vault.WikiDir+"/")
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
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, PlanOut{}, err
	}
	v := sess.target
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
	if kind == txn.Config {
		return nil, PlanOut{}, errors.New("to change the mode, call the mode tool with set")
	}
	if err := sess.writable(kind); err != nil {
		return nil, PlanOut{}, err
	}
	r := txn.Request{Kind: kind, Summary: a.Summary, Mounts: sess.mounts()}
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
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, txn.Result{}, err
	}
	// A revert is a commit, so it needs the same access as the operation it undoes.
	if err := sess.writable(txn.Undo); err != nil {
		return nil, txn.Result{}, err
	}
	res, err := txn.UndoOperation(sess.target, a.OperationID, s.opts.Now())
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
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, lint.Report{}, err
	}
	report, err := lint.Run(sess.target.Root, lint.Options{Exclude: a.Exclude, AsOf: s.opts.Now(), Mounts: sess.mounts()})
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
	sess, err := s.open(a.Vault)
	if err != nil {
		return nil, ModeOut{}, err
	}
	v := sess.target
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
	// A mode change rewrites the identity file, so it is a write like any other.
	if err := sess.writable(txn.Config); err != nil {
		return nil, ModeOut{}, err
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
		Description: "Copy files from the project's inbox into the immutable raw store and record them in the source ledger, as one commit. Set vault to a mounted knowledge base to capture into it; the record then names the project the source came through. Returns each file's source id and stored path. Read the stored file afterwards with Read."}, s.capture)
	mcp.AddTool(server, &mcp.Tool{Name: "route", Annotations: ro(),
		Description: "Say where a new page of a type belongs under the vault's mode, whether a page with that title or alias already exists in the vault or, for a project, in any of its mounts, and give a skeleton with the vault's frontmatter conventions."}, s.route)
	mcp.AddTool(server, &mcp.Tool{Name: "plan", Annotations: ro(),
		Description: "Validate a set of file changes against the vault and hold them as a plan. Returns a plan_id, a preview of creates, replaces, and deletes, and warnings such as links that do not resolve. Nothing is written. Show the preview to the user before apply."}, s.plan)
	mcp.AddTool(server, &mcp.Tool{Name: "apply",
		Description: "Apply a held plan as one git commit and write its log entry. Edits made by hand are committed first, so the operation can always be undone exactly. The plan is consumed."}, s.apply)
	mcp.AddTool(server, &mcp.Tool{Name: "undo",
		Description: "Revert one applied operation as a new commit. Fails if later changes overlap it."}, s.undo)
	mcp.AddTool(server, &mcp.Tool{Name: "history", Annotations: ro(),
		Description: "List recent operations, newest first, with their kind, summary, date, commit, and changed paths."}, s.history)
	mcp.AddTool(server, &mcp.Tool{Name: "lint", Annotations: ro(),
		Description: "Run the deterministic wiki health check: dead and ambiguous links, duplicate basenames, orphans, pages missing from every index, missing frontmatter, empty sections, stale index entries, and ledger problems. Read-only."}, s.lint)
	mcp.AddTool(server, &mcp.Tool{Name: "plant",
		Description: "Plant a task: create a task page with status planted from a title and the idea's text, as one commit. Give from to remove the inbox/tasks/ note it came from. No plan preview is needed; undo covers it."}, s.plant)
	mcp.AddTool(server, &mcp.Tool{Name: "stub",
		Description: "Create seed pages for the pages the wiki links to but nobody has written (lint's wanted pages). The empty pages a link points to get frontmatter too. It is one commit, with no plan preview, and undo reverts it. Omit titles to stub every one of them with the mode's default type; one the vault cannot file is skipped and reported, while a title you name is refused and says why. Pass a title with a type when the name is a person, product, project, or organization (entity). In a project, give a title a target to file its stub in that mount's knowledge base; the mounts tool names them."}, s.stub)
	mcp.AddTool(server, &mcp.Tool{Name: "tasks", Annotations: ro(),
		Description: "List the vault's tasks from the task ledger: open ones by status, priority, and age, with each task's page, workdir, last touch, and history; counts; and the notes waiting in inbox/tasks/. Pass all to include finished tasks."}, s.tasks)
	mcp.AddTool(server, &mcp.Tool{Name: "repos", Annotations: ro(),
		Description: "List the repositories mounted on the vault's project: path, remote, branch, uncommitted changes, and how changes land there (pr: branch and pull request; commit: on the current branch). Read it before changing files in a repository."}, s.repos)
	mcp.AddTool(server, &mcp.Tool{Name: "mounts", Annotations: ro(),
		Description: "List the knowledge bases the project mounts: id, name, the knowledge base's real wiki path, the mount folder, the requested and effective access, what the knowledge base is for, and its page count. Grep the real path; plan a page under a write mount like the project's own. A mount that names through came from a cluster: the project mounted that cluster, and every member is reached the same way. Read-only."}, s.mounts)
	mcp.AddTool(server, &mcp.Tool{Name: "mode",
		Description: "Read the vault's filing mode (generic or lyt) and the page types it files. Pass set to prepare a plan that changes it; apply that plan to make the change."}, s.mode)
	mcp.AddTool(server, &mcp.Tool{Name: "atlas", Annotations: ro(),
		Description: "Read the whole atlas: every vault with its kind, path, tags or scope, access, mounts with effective access, repositories and their change policy, members, clusters, who mounts it, and state; the folders the atlas cannot read; and the settings. Pass refresh to also rewrite the registry and adopt repositories waiting under repos/."}, s.atlasTool)
	return server
}

// Run serves over stdio until the client disconnects.
func Run(ctx context.Context, opts Options) error {
	return New(opts).MCP().Run(ctx, &mcp.StdioTransport{})
}

// ToolNames lists every tool MCP registers, sorted, for docs and tests.
func ToolNames() []string {
	names := []string{"apply", "atlas", "capture", "history", "inbox", "lint", "mode", "mounts", "plan", "plant", "repos", "route", "status", "stub", "tasks", "undo"}
	sort.Strings(names)
	return names
}
