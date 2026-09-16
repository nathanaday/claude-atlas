// Package txn turns a requested change into a reviewed plan and applies it as one git
// commit. Git is the safety net: hand edits are committed before an operation runs, a
// failed operation is restored from HEAD, and undo is a revert.
package txn

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/ledger"
	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// Kind names the workflow that produced a plan. It bounds what the plan may write.
type Kind string

const (
	Ingest   Kind = "ingest"
	Save     Kind = "save"
	Markdown Kind = "markdown"
	Repair   Kind = "repair"
	Fold     Kind = "fold"
	Canvas   Kind = "canvas"
	Base     Kind = "base"
	Config   Kind = "config"
	Capture  Kind = "capture"
	Undo     Kind = "undo"
	Task     Kind = "task"
	Stub     Kind = "stub"
)

// ModelKinds are the kinds a plan from the model may use. Capture, undo, and stub are the core's own.
var ModelKinds = []Kind{Ingest, Save, Markdown, Repair, Fold, Canvas, Base, Config, Task}

func validKind(k Kind) bool {
	switch k {
	case Ingest, Save, Markdown, Repair, Fold, Canvas, Base, Config, Capture, Task, Stub:
		return true
	}
	return false
}

// WriteMode says what a write does to its path.
type WriteMode string

const (
	Create  WriteMode = "create"
	Replace WriteMode = "replace"
	Delete  WriteMode = "delete"
)

// Write is one requested file change.
type Write struct {
	Path    string
	Mode    WriteMode
	Content []byte
	// BaseSHA256 is the hash of the content the author last saw. Empty means "whatever is
	// there now"; the plan then pins the current hash.
	BaseSHA256 string
}

// Request is what a workflow asks for.
type Request struct {
	Kind    Kind
	Summary string
	Writes  []Write
	Sources []ledger.Update
	// Mounts names the knowledge bases the vault mounts, a mount name to that knowledge
	// base's wiki path, for the lint run that produces the plan's warnings. Nil reads the
	// symlinks under kb/, which are absent until refresh makes them.
	Mounts map[string]string
}

const (
	MaxWrites    = 256
	MaxWriteSize = 64 << 20
)

// Change is one line of a preview.
type Change struct {
	Path   string    `json:"path"`
	Mode   WriteMode `json:"mode"`
	Bytes  int       `json:"bytes"`
	Before int       `json:"before_bytes,omitempty"`
	// Title is the page's frontmatter title, when the write is a wiki page that has one.
	Title string `json:"title,omitempty"`
}

// Preview is what the user reviews before apply.
type Preview struct {
	Creates  []Change `json:"creates"`
	Replaces []Change `json:"replaces"`
	Deletes  []Change `json:"deletes"`
	Sources  []string `json:"sources,omitempty"`
}

type prepared struct {
	Path    string
	Mode    WriteMode
	Content []byte
	Base    string // sha256 of the current content, "" when absent
	Existed bool
}

// Plan is a validated request bound to one vault and the state it saw.
type Plan struct {
	ID          string    `json:"id"`
	OperationID string    `json:"operation_id"`
	Vault       string    `json:"vault"`
	Kind        Kind      `json:"kind"`
	Summary     string    `json:"summary"`
	Preview     Preview   `json:"preview"`
	Warnings    []string  `json:"warnings"`
	CreatedAt   time.Time `json:"created_at"`

	writes  []prepared
	sources []ledger.Update
}

// Result reports an applied operation.
type Result struct {
	OperationID  string   `json:"operation_id"`
	Commit       string   `json:"commit"`
	ChangedPaths []string `json:"changed_paths"`
	// ManualCommit is the commit that captured hand edits before this operation, if any.
	ManualCommit string `json:"manual_commit,omitempty"`
}

// ErrConflict means a file changed after the plan was made.
var ErrConflict = errors.New("conflict")

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newPlanID() string {
	var b [3]byte
	rand.Read(b[:])
	return "plan-" + hex.EncodeToString(b[:])
}

// normalizePath validates a vault-relative path from a request.
func normalizePath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is empty")
	}
	if strings.ContainsAny(p, "\x00\n\r\\") {
		return "", fmt.Errorf("path %q contains a forbidden character", p)
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") {
		return "", fmt.Errorf("path %q must be relative to the vault", p)
	}
	if path.Clean(p) != p || strings.HasPrefix(p, "../") || p == ".." || p == "." {
		return "", fmt.Errorf("path %q must be a clean vault-relative path", p)
	}
	if len(p) > 1024 {
		return "", fmt.Errorf("path %q is too long", p)
	}
	return p, nil
}

// allowed enforces each kind's write scope, and the vault kind's: a knowledge base has
// no inbox, ideas, tasks, questions, or sessions.
func allowed(vk vault.Kind, kind Kind, p string, mode WriteMode) error {
	under := func(dir string) bool { return strings.HasPrefix(p, dir+"/") }
	if vk == vault.Knowledge {
		for _, rel := range vault.ProjectOnly {
			if rel == vault.TaskLedgerPath {
				if p == rel {
					return fmt.Errorf("%s belongs to a project, not a knowledge base", p)
				}
				continue
			}
			if under(rel) {
				return fmt.Errorf("%s/ belongs to a project, not a knowledge base: %s", rel, p)
			}
		}
	}
	if under(vault.KbDir) {
		return fmt.Errorf("%s/ holds mounted knowledge bases; change their pages in the knowledge base's own operation: %s", vault.KbDir, p)
	}
	if under(vault.ReposDir) {
		return fmt.Errorf("%s/ holds repositories; they are not the vault's files: %s", vault.ReposDir, p)
	}
	switch {
	case p == ".git" || strings.HasPrefix(p, ".git/"),
		p == vault.MetaDir || strings.HasPrefix(p, vault.MetaDir+"/"):
		return fmt.Errorf("%s is internal and cannot be written", p)
	case p == vault.LogPage:
		return fmt.Errorf("%s is written by the core from the plan's summary; do not write it", p)
	case p == vault.LedgerPath:
		return fmt.Errorf("%s is updated through the plan's sources field; do not write it", p)
	case p == vault.TaskLedgerPath, p == vault.TasksIndex:
		return fmt.Errorf("%s is written by the core from the task pages; do not write it", p)
	case p == vault.LegacyTasksIndex:
		return fmt.Errorf("%s is the task index's old path; the core moves it to %s", p, vault.TasksIndex)
	}
	if under(vault.TasksDir) && kind != Task && kind != Repair {
		return fmt.Errorf("task pages change only through a task operation (or a repair): %s", p)
	}
	switch kind {
	case Config:
		if p != vault.Marker || mode != Replace {
			return fmt.Errorf("a config operation replaces only %s", vault.Marker)
		}
	case Capture:
		if !under(vault.CapturedDir) || mode != Create || strings.Count(p, "/") != 2 {
			return fmt.Errorf("a capture operation creates only files under %s/", vault.CapturedDir)
		}
	case Ingest:
		if under(vault.InboxDir) {
			if mode != Delete {
				return fmt.Errorf("an ingest may only remove files from %s/, not write them", vault.InboxDir)
			}
			return nil
		}
		if !under(vault.WikiDir) {
			return fmt.Errorf("an ingest writes only under wiki/ (and removes from inbox/): %s", p)
		}
	case Canvas:
		if !(under("wiki/canvases") && strings.HasSuffix(p, ".canvas")) && p != vault.CanvasIndex {
			return fmt.Errorf("a canvas operation writes only wiki/canvases/*.canvas and %s: %s", vault.CanvasIndex, p)
		}
	case Base:
		if !under(vault.WikiDir) || !strings.HasSuffix(p, ".base") {
			return fmt.Errorf("a base operation writes only .base files under wiki/: %s", p)
		}
	case Save, Markdown, Repair, Fold:
		if !under(vault.WikiDir) {
			return fmt.Errorf("a %s operation writes only under wiki/: %s", kind, p)
		}
	case Task:
		switch {
		case under(vault.InboxTasksDir):
			if mode != Delete {
				return fmt.Errorf("a task operation may only remove notes from %s/, not write them", vault.InboxTasksDir)
			}
		case p == vault.HotPage:
			if mode != Replace {
				return fmt.Errorf("a task operation may replace %s, not create or delete it", p)
			}
		case !tasks.IsPage(p):
			return fmt.Errorf("a task operation writes task pages under %s/ (and %s/), %s, and removes notes from %s/: %s", vault.TasksDir, vault.TaskArchiveDir, vault.HotPage, vault.InboxTasksDir, p)
		}
	case Stub:
		if !under(vault.WikiDir) || !strings.EqualFold(path.Ext(p), ".md") {
			return fmt.Errorf("a stub operation writes only pages under wiki/: %s", p)
		}
	default:
		return fmt.Errorf("unknown operation kind %q", kind)
	}
	return nil
}

func validateContent(p string, content []byte) error {
	ext := strings.ToLower(path.Ext(p))
	switch {
	case strings.HasPrefix(p, "wiki/") && ext == ".md":
		if !utf8Valid(content) {
			return fmt.Errorf("%s is not UTF-8", p)
		}
		fields, _, err := vault.Frontmatter(string(content))
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if fields == nil {
			return fmt.Errorf("%s has no frontmatter; wiki pages start with a YAML block", p)
		}
		if missing := vault.MissingFrontmatter(fields); len(missing) > 0 {
			return fmt.Errorf("%s frontmatter lacks %s", p, strings.Join(missing, ", "))
		}
		if tasks.IsPage(p) {
			if _, err := tasks.Parse(p, content); err != nil {
				return err
			}
		}
	case ext == ".json" || ext == ".canvas":
		if !json.Valid(content) {
			return fmt.Errorf("%s is not valid JSON", p)
		}
	case ext == ".base":
		var doc any
		if err := yaml.Unmarshal(content, &doc); err != nil {
			return fmt.Errorf("%s is not valid YAML: %w", p, err)
		}
	}
	return nil
}

func utf8Valid(b []byte) bool { return strings.ToValidUTF8(string(b), "�") == string(b) }

// fileState returns the hash of a vault file, or "" and false when it is absent.
func fileState(v *vault.Vault, rel string) (string, int, bool, error) {
	info, err := os.Lstat(v.Path(rel))
	if errors.Is(err, os.ErrNotExist) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, false, fmt.Errorf("%s is not a regular file", rel)
	}
	data, err := os.ReadFile(v.Path(rel))
	if err != nil {
		return "", 0, false, err
	}
	return sha(data), len(data), true, nil
}

// Prepare validates a request against the vault's current state and returns a plan.
func Prepare(v *vault.Vault, req Request, now time.Time) (*Plan, error) {
	if err := v.Repo().CheckIdle(); err != nil {
		return nil, err
	}
	if !validKind(req.Kind) {
		return nil, fmt.Errorf("unknown operation kind %q", req.Kind)
	}
	if v.Config.Kind == vault.Knowledge && req.Kind == Task {
		return nil, errors.New("a knowledge base has no tasks; plant the task in a project that mounts it")
	}
	summary := strings.Join(strings.Fields(req.Summary), " ")
	if summary == "" {
		return nil, errors.New("summary is required: one line saying what the operation does")
	}
	if len(req.Writes) == 0 && len(req.Sources) == 0 {
		return nil, errors.New("a plan needs at least one write or source update")
	}
	if len(req.Writes) > MaxWrites {
		return nil, fmt.Errorf("a plan may hold at most %d writes", MaxWrites)
	}
	plan := &Plan{ID: newPlanID(), OperationID: vault.NewOperationID(string(req.Kind), now), Vault: v.Root, Kind: req.Kind, Summary: summary, CreatedAt: now, Warnings: []string{}}
	seen := map[string]string{}
	overlay := map[string][]byte{}
	led, err := ledger.Load(v.Path(vault.LedgerPath), now)
	if err != nil {
		return nil, err
	}
	mounted := ledger.Mounts(req.Mounts, now)
	for _, w := range req.Writes {
		p, err := normalizePath(w.Path)
		if err != nil {
			return nil, err
		}
		if prior, dup := seen[strings.ToLower(p)]; dup {
			return nil, fmt.Errorf("plan writes %s twice (as %s and %s)", p, prior, p)
		}
		seen[strings.ToLower(p)] = p
		if w.Mode != Create && w.Mode != Replace && w.Mode != Delete {
			return nil, fmt.Errorf("%s: mode must be create, replace, or delete", p)
		}
		if err := allowed(v.Config.Kind, req.Kind, p, w.Mode); err != nil {
			return nil, err
		}
		current, size, exists, err := fileState(v, p)
		if err != nil {
			return nil, err
		}
		switch w.Mode {
		case Create:
			if exists {
				return nil, fmt.Errorf("%s already exists; use replace with its base hash", p)
			}
		case Replace, Delete:
			if !exists {
				return nil, fmt.Errorf("%s does not exist; use create", p)
			}
			if w.BaseSHA256 != "" && strings.ToLower(w.BaseSHA256) != current {
				return nil, fmt.Errorf("%w: %s changed since it was read; read it again", ErrConflict, p)
			}
		}
		if w.Mode != Delete {
			if len(w.Content) > MaxWriteSize {
				return nil, fmt.Errorf("%s exceeds %d bytes", p, MaxWriteSize)
			}
			if err := validateContent(p, w.Content); err != nil {
				return nil, err
			}
		}
		// An ingest removes an inbox file the vault captured, or one a knowledge base it
		// mounts captured: knowledge enters through a project, and the source is durable
		// wherever it landed. The mounts' ledgers are read here and nowhere else.
		if req.Kind == Ingest && strings.HasPrefix(p, vault.InboxDir+"/") {
			if id, _ := led.FindBySHA(current); id == "" && mounted.Find(current) == nil {
				return nil, fmt.Errorf("%s has not been captured; capture it before removing it from the inbox", p)
			}
		}
		pw := prepared{Path: p, Mode: w.Mode, Content: w.Content, Base: current, Existed: exists}
		plan.writes = append(plan.writes, pw)
		change := Change{Path: p, Mode: w.Mode, Bytes: len(w.Content), Before: size, Title: pageTitleOf(p, w.Content)}
		switch w.Mode {
		case Create:
			plan.Preview.Creates = append(plan.Preview.Creates, change)
			overlay[p] = w.Content
		case Replace:
			plan.Preview.Replaces = append(plan.Preview.Replaces, change)
			overlay[p] = w.Content
		case Delete:
			change.Bytes = 0
			plan.Preview.Deletes = append(plan.Preview.Deletes, change)
			overlay[p] = nil
		}
	}
	if err := checkTaskIDs(v, plan, overlay); err != nil {
		return nil, err
	}
	trial, err := ledger.Parse(led.Encode())
	if err != nil {
		return nil, err
	}
	if len(req.Sources) > 0 {
		if err := trial.Apply(req.Sources, now); err != nil {
			return nil, err
		}
		for _, u := range req.Sources {
			plan.Preview.Sources = append(plan.Preview.Sources, u.ID)
		}
		plan.sources = req.Sources
	}
	for _, d := range trial.DropPages(pageExists(v, plan.writes), now) {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("source %s lists %s, which does not exist; apply removes it from the ledger", d.ID, d.Page))
	}
	var written []string
	for p := range overlay {
		if strings.HasPrefix(p, "wiki/") && strings.HasSuffix(strings.ToLower(p), ".md") && overlay[p] != nil {
			written = append(written, p)
		}
	}
	if len(written) > 0 {
		report, err := lint.Run(v.Root, lint.Options{Overlay: overlay, AsOf: now, Mounts: req.Mounts})
		if err == nil {
			plan.Warnings = append(plan.Warnings, report.Problems(written)...)
		}
	}
	sort.Strings(plan.Warnings)
	return plan, nil
}

// pageExists reports whether a path exists once the writes are applied.
func pageExists(v *vault.Vault, writes []prepared) func(string) bool {
	planned := map[string]bool{}
	for _, w := range writes {
		planned[w.Path] = w.Mode != Delete
	}
	return func(p string) bool {
		if present, ok := planned[p]; ok {
			return present
		}
		_, _, exists, _ := fileState(v, p)
		return exists
	}
}

// checkTaskIDs refuses a plan whose task pages reuse an id another page holds.
func checkTaskIDs(v *vault.Vault, plan *Plan, overlay map[string][]byte) error {
	ids := map[string]string{}
	for _, w := range plan.writes {
		if w.Mode == Delete || !tasks.IsPage(w.Path) {
			continue
		}
		t, err := tasks.Parse(w.Path, w.Content)
		if err != nil {
			return err
		}
		if other, dup := ids[t.ID]; dup {
			return fmt.Errorf("%s and %s both carry task_id %s", other, w.Path, t.ID)
		}
		ids[t.ID] = w.Path
	}
	if len(ids) == 0 {
		return nil
	}
	led, err := tasks.LoadLedger(v)
	if err != nil {
		return err
	}
	for _, rec := range led.Tasks {
		if p, dup := ids[rec.ID]; dup && rec.Path != p {
			if content, planned := overlay[rec.Path]; planned && content == nil {
				continue // the page moves in this plan
			}
			return fmt.Errorf("%s reuses task_id %s, which belongs to %s", p, rec.ID, rec.Path)
		}
	}
	return nil
}

// touchesTasks reports whether a plan writes a task page or a task note.
func touchesTasks(plan *Plan) bool {
	for _, w := range plan.writes {
		if tasks.IsPage(w.Path) {
			return true
		}
	}
	return false
}

// Planted says where a plant put its page.
type Planted struct {
	Path string `json:"path"`
	ID   string `json:"task_id"`
}

// PlantRequest builds the request that plants a task: one new page from the skeleton,
// and the removal of the inbox note it came from, when there is one.
func PlantRequest(v *vault.Vault, p tasks.Plant, from string, now time.Time) (Request, Planted, error) {
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		p.Title = tasks.TitleFromText(p.Text)
	}
	if p.Title == "" {
		return Request{}, Planted{}, errors.New("a task needs a title or some text")
	}
	if p.Priority != "" && !contains(tasks.Priorities, p.Priority) {
		return Request{}, Planted{}, fmt.Errorf("priority must be one of %s", strings.Join(tasks.Priorities, ", "))
	}
	if p.Workdir != "" {
		abs, err := filepath.Abs(p.Workdir)
		if err != nil {
			return Request{}, Planted{}, err
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return Request{}, Planted{}, fmt.Errorf("workdir %s is not a directory", p.Workdir)
		}
		p.Workdir = abs
	}
	taken := func(rel string) bool { _, _, exists, _ := fileState(v, rel); return exists }
	planted := Planted{Path: tasks.PagePath(p.Title, taken), ID: tasks.NewID(now)}
	req := Request{Kind: Task, Summary: "plant " + p.Title, Writes: []Write{{Path: planted.Path, Mode: Create, Content: []byte(tasks.Skeleton(p, planted.ID, now))}}}
	if from != "" {
		rel, err := normalizePath(from)
		if err != nil {
			return Request{}, Planted{}, err
		}
		if !strings.HasPrefix(rel, vault.InboxTasksDir+"/") {
			return Request{}, Planted{}, fmt.Errorf("%s is not a note under %s/", from, vault.InboxTasksDir)
		}
		req.Writes = append(req.Writes, Write{Path: rel, Mode: Delete})
	}
	return req, planted, nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// ConfigRequest builds the request that changes the vault's mode.
func ConfigRequest(v *vault.Vault, mode vault.Mode) Request {
	cfg := v.Config
	cfg.Mode = mode
	return Request{Kind: Config, Summary: fmt.Sprintf("set mode to %s", mode), Writes: []Write{{Path: vault.Marker, Mode: Replace, Content: cfg.Encode()}}}
}

// Inflight marks an apply that has started writing. It exists only until the commit.
type Inflight struct {
	OperationID string         `json:"operation_id"`
	Kind        Kind           `json:"kind"`
	Started     string         `json:"started"`
	Paths       []InflightPath `json:"paths"`
}

type InflightPath struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
}

func inflightPath(v *vault.Vault) string { return v.Path(vault.MetaDir + "/inflight.json") }

// Pending returns the in-flight marker if an apply was interrupted.
func Pending(v *vault.Vault) (*Inflight, error) {
	data, err := os.ReadFile(inflightPath(v))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var in Inflight
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("inflight marker is unreadable: %w", err)
	}
	return &in, nil
}

// RecoverResult reports what recovery restored.
type RecoverResult struct {
	OperationID string   `json:"operation_id"`
	Restored    []string `json:"restored"`
}

// Recover restores every path an interrupted apply touched from HEAD and removes the marker.
// It returns nil, nil when nothing was pending.
func Recover(v *vault.Vault) (*RecoverResult, error) {
	unlock, err := vault.Lock(v.Root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return recoverLocked(v)
}

func recoverLocked(v *vault.Vault) (*RecoverResult, error) {
	in, err := Pending(v)
	if err != nil {
		return nil, err
	}
	if in == nil {
		return nil, nil
	}
	res := &RecoverResult{OperationID: in.OperationID}
	repo := v.Repo()
	var restore []string
	for _, p := range in.Paths {
		if p.Existed {
			restore = append(restore, p.Path)
		} else if err := os.Remove(v.Path(p.Path)); err == nil {
			res.Restored = append(res.Restored, p.Path)
		}
	}
	if len(restore) > 0 {
		if err := repo.RestoreFromHead(restore...); err != nil {
			return nil, fmt.Errorf("recovery could not restore %s: %w", strings.Join(restore, ", "), err)
		}
		res.Restored = append(res.Restored, restore...)
	}
	sort.Strings(res.Restored)
	return res, os.Remove(inflightPath(v))
}

func requireHistory(repo gitx.Repo) error {
	if !repo.IsRepo() || !repo.HasHead() {
		return errors.New("the vault has no git history; run `claude-atlas adopt` on it first")
	}
	return nil
}

// commitManualEdits records whatever changed in the vault by hand so the tree is clean.
func commitManualEdits(repo gitx.Repo, now time.Time) (string, error) {
	entries, err := repo.Status()
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	if err := repo.AddAll(); err != nil {
		return "", err
	}
	id := vault.NewOperationID("manual", now)
	noun := "file"
	if len(entries) != 1 {
		noun = "files"
	}
	return repo.Commit(vault.CommitMessage("manual", fmt.Sprintf("%d %s changed by hand", len(entries), noun), id))
}

// Apply writes the plan as one commit. The plan is consumed whether or not it succeeds.
func Apply(v *vault.Vault, plan *Plan, now time.Time) (*Result, error) {
	if plan.Vault != v.Root {
		return nil, fmt.Errorf("plan belongs to %s, not %s", plan.Vault, v.Root)
	}
	unlock, err := vault.Lock(v.Root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	repo := v.Repo()
	if err := requireHistory(repo); err != nil {
		return nil, err
	}
	if err := repo.CheckIdle(); err != nil {
		return nil, err
	}
	if _, err := recoverLocked(v); err != nil {
		return nil, err
	}
	res := &Result{OperationID: plan.OperationID}
	if res.ManualCommit, err = commitManualEdits(repo, now); err != nil {
		return nil, err
	}
	for _, w := range plan.writes {
		current, _, exists, err := fileState(v, w.Path)
		if err != nil {
			return nil, err
		}
		if exists != w.Existed || current != w.Base {
			return nil, fmt.Errorf("%w: %s changed after the plan was made; plan again", ErrConflict, w.Path)
		}
	}
	led, err := ledger.Load(v.Path(vault.LedgerPath), now)
	if err != nil {
		return nil, err
	}
	if err := led.Apply(plan.sources, now); err != nil {
		return nil, err
	}
	writeLedger := len(led.DropPages(pageExists(v, plan.writes), now)) > 0 || len(plan.sources) > 0
	logData, err := os.ReadFile(v.Path(vault.LogPage))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	logExisted := err == nil
	_, _, ledgerExisted, _ := fileState(v, vault.LedgerPath)

	in := Inflight{OperationID: plan.OperationID, Kind: plan.Kind, Started: now.UTC().Format(time.RFC3339)}
	for _, w := range plan.writes {
		in.Paths = append(in.Paths, InflightPath{Path: w.Path, Existed: w.Existed})
	}
	in.Paths = append(in.Paths, InflightPath{Path: vault.LogPage, Existed: logExisted})
	if writeLedger {
		in.Paths = append(in.Paths, InflightPath{Path: vault.LedgerPath, Existed: ledgerExisted})
	}
	withTasks := touchesTasks(plan)
	var prevTasks tasks.Ledger
	var indexBefore []byte
	var legacyIndexExisted bool
	if withTasks {
		if prevTasks, err = tasks.LoadLedger(v); err != nil {
			return nil, err
		}
		indexBefore, _ = os.ReadFile(v.Path(vault.TasksIndex))
		_, _, taskLedgerExisted, _ := fileState(v, vault.TaskLedgerPath)
		_, _, indexExisted, _ := fileState(v, vault.TasksIndex)
		in.Paths = append(in.Paths, InflightPath{Path: vault.TaskLedgerPath, Existed: taskLedgerExisted}, InflightPath{Path: vault.TasksIndex, Existed: indexExisted})
		if _, _, legacyIndexExisted, _ = fileState(v, vault.LegacyTasksIndex); legacyIndexExisted {
			in.Paths = append(in.Paths, InflightPath{Path: vault.LegacyTasksIndex, Existed: true})
		}
	}
	if err := writeInflight(v, in); err != nil {
		return nil, err
	}
	rollback := func(cause error) error {
		if _, rerr := recoverLocked(v); rerr != nil {
			return fmt.Errorf("%v; recovery also failed: %v", cause, rerr)
		}
		return cause
	}
	var changed []string
	for _, w := range plan.writes {
		changed = append(changed, w.Path)
		if w.Mode == Delete {
			if err := os.Remove(v.Path(w.Path)); err != nil {
				return nil, rollback(err)
			}
			continue
		}
		if err := writeAtomic(v.Path(w.Path), w.Content); err != nil {
			return nil, rollback(err)
		}
	}
	entry := logEntry(plan, now)
	if err := writeAtomic(v.Path(vault.LogPage), prependLog(logData, entry, now)); err != nil {
		return nil, rollback(err)
	}
	changed = append(changed, vault.LogPage)
	if writeLedger {
		if err := writeAtomic(v.Path(vault.LedgerPath), led.Encode()); err != nil {
			return nil, rollback(err)
		}
		changed = append(changed, vault.LedgerPath)
	}
	if withTasks {
		touch := tasks.Touch{OperationID: plan.OperationID, Date: now.Format("2006-01-02"), Summary: plan.Summary}
		taskLedger, err := tasks.Build(v, prevTasks, &touch, changed, now)
		if err != nil {
			return nil, rollback(err)
		}
		if err := writeAtomic(v.Path(vault.TaskLedgerPath), taskLedger.Encode()); err != nil {
			return nil, rollback(err)
		}
		if err := writeAtomic(v.Path(vault.TasksIndex), []byte(tasks.RenderIndex(taskLedger, indexBefore, now))); err != nil {
			return nil, rollback(err)
		}
		changed = append(changed, vault.TaskLedgerPath, vault.TasksIndex)
		if legacyIndexExisted {
			if err := os.Remove(v.Path(vault.LegacyTasksIndex)); err != nil {
				return nil, rollback(err)
			}
			changed = append(changed, vault.LegacyTasksIndex)
		}
	}
	if err := repo.Add(changed...); err != nil {
		return nil, rollback(err)
	}
	commit, err := repo.Commit(vault.CommitMessage(string(plan.Kind), plan.Summary, plan.OperationID))
	if err != nil {
		return nil, rollback(err)
	}
	if err := os.Remove(inflightPath(v)); err != nil {
		return nil, err
	}
	sort.Strings(changed)
	res.Commit = commit
	res.ChangedPaths = changed
	return res, nil
}

func writeInflight(v *vault.Vault, in Inflight) error {
	if err := os.MkdirAll(v.Path(vault.MetaDir), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(in, "", "  ")
	return writeAtomic(inflightPath(v), append(data, '\n'))
}

func writeAtomic(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".atlas-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, target)
}

var updatedLine = regexp.MustCompile(`(?m)^updated: .*$`)

// prependLog inserts an entry before the first existing entry and bumps `updated:`.
func prependLog(existing []byte, entry string, now time.Time) []byte {
	text := string(existing)
	if text == "" {
		text = "---\ntype: meta\ntitle: Wiki Log\nstatus: evergreen\ncreated: " + now.Format("2006-01-02") + "\nupdated: " + now.Format("2006-01-02") + "\ntags:\n  - meta\n  - log\n---\n\n# Wiki Log\n\nNewest completed operations appear first.\n"
	}
	if front, _, ok, err := vault.SplitFrontmatter(text); ok && err == nil {
		newFront := updatedLine.ReplaceAllString(front, "updated: "+now.Format("2006-01-02"))
		text = "---\n" + newFront + "---" + text[len("---\n")+len(front)+len("---"):]
	}
	idx := strings.Index(text, "\n## ")
	var b strings.Builder
	if idx < 0 {
		b.WriteString(strings.TrimRight(text, "\n"))
		b.WriteString("\n\n")
		b.WriteString(entry)
	} else {
		b.WriteString(strings.TrimRight(text[:idx], "\n"))
		b.WriteString("\n\n")
		b.WriteString(entry)
		b.WriteString("\n")
		b.WriteString(strings.TrimLeft(text[idx:], "\n"))
	}
	out := strings.TrimRight(b.String(), "\n") + "\n"
	return []byte(out)
}

func logEntry(plan *Plan, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — %s\n\n%s\n", now.Format("2006-01-02"), plan.OperationID, plan.Summary)
	line := func(label string, changes []Change, deleted bool) {
		if len(changes) == 0 {
			return
		}
		var refs []string
		for _, c := range changes {
			refs = append(refs, pageRef(c, deleted))
		}
		fmt.Fprintf(&b, "- %s: %s\n", label, strings.Join(refs, ", "))
	}
	if len(plan.Preview.Creates)+len(plan.Preview.Replaces)+len(plan.Preview.Deletes)+len(plan.Preview.Sources) > 0 {
		b.WriteString("\n")
	}
	line("Created", plan.Preview.Creates, false)
	line("Updated", plan.Preview.Replaces, false)
	line("Removed", plan.Preview.Deletes, true)
	if len(plan.Preview.Sources) > 0 {
		fmt.Fprintf(&b, "- Sources: %s\n", strings.Join(plan.Preview.Sources, ", "))
	}
	return b.String()
}

// pageTitleOf reads the frontmatter title of a wiki page being written.
func pageTitleOf(p string, content []byte) string {
	if !strings.HasPrefix(p, "wiki/") || !strings.EqualFold(path.Ext(p), ".md") || len(content) == 0 {
		return ""
	}
	fields, _, err := vault.Frontmatter(string(content))
	if err != nil || fields == nil {
		return ""
	}
	return strings.TrimSpace(vault.StringField(fields, "title"))
}

// pageRef links a wiki page by its stem, showing its title when that differs, and
// quotes anything else.
func pageRef(c Change, deleted bool) string {
	p := c.Path
	if !deleted && strings.HasPrefix(p, "wiki/") && strings.EqualFold(path.Ext(p), ".md") {
		stem := vault.PageTitle(p)
		if c.Title != "" && c.Title != stem && !strings.ContainsAny(c.Title, "[]|#") {
			return "[[" + stem + "|" + c.Title + "]]"
		}
		return "[[" + stem + "]]"
	}
	return "`" + p + "`"
}

// Operation is one entry of the vault's history.
type Operation struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Summary string    `json:"summary"`
	Commit  string    `json:"commit"`
	Date    time.Time `json:"date"`
	Paths   []string  `json:"paths,omitempty"`
	Undoes  string    `json:"undoes,omitempty"`
}

// History lists the newest operations, most recent first. Manual-edit commits are included.
func History(v *vault.Vault, limit int, withPaths bool) ([]Operation, error) {
	repo := v.Repo()
	commits, err := repo.Log(0)
	if err != nil {
		return nil, err
	}
	var ops []Operation
	for _, c := range commits {
		id, ok := c.Trailers["atlas-operation"]
		if !ok {
			continue
		}
		kind, summary, _ := strings.Cut(c.Subject, ": ")
		op := Operation{ID: id, Kind: kind, Summary: summary, Commit: c.SHA, Date: c.Date, Undoes: c.Trailers["atlas-undoes"]}
		if withPaths {
			op.Paths, _ = repo.ChangedPaths(c.SHA)
		}
		ops = append(ops, op)
		if limit > 0 && len(ops) >= limit {
			break
		}
	}
	return ops, nil
}

// Find returns the operation with the given id.
func Find(v *vault.Vault, id string) (*Operation, error) {
	ops, err := History(v, 0, false)
	if err != nil {
		return nil, err
	}
	for i := range ops {
		if ops[i].ID == id {
			return &ops[i], nil
		}
	}
	return nil, fmt.Errorf("no operation %q in this vault's history", id)
}

// UndoOperation reverts one operation's commit as a new commit.
func UndoOperation(v *vault.Vault, operationID string, now time.Time) (*Result, error) {
	unlock, err := vault.Lock(v.Root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	repo := v.Repo()
	if err := requireHistory(repo); err != nil {
		return nil, err
	}
	if err := repo.CheckIdle(); err != nil {
		return nil, err
	}
	if _, err := recoverLocked(v); err != nil {
		return nil, err
	}
	op, err := Find(v, operationID)
	if err != nil {
		return nil, err
	}
	res := &Result{OperationID: vault.NewOperationID("undo", now)}
	if res.ManualCommit, err = commitManualEdits(repo, now); err != nil {
		return nil, err
	}
	if err := repo.RevertNoCommit(op.Commit); err != nil {
		return nil, fmt.Errorf("cannot undo %s: later changes overlap it (%v); repair by hand or with a repair operation", operationID, err)
	}
	defer repo.ClearRevert()
	logData, _ := os.ReadFile(v.Path(vault.LogPage))
	entry := fmt.Sprintf("## %s — %s\n\nUndid %s: %s\n", now.Format("2006-01-02"), res.OperationID, op.ID, op.Summary)
	if err := writeAtomic(v.Path(vault.LogPage), prependLog(logData, entry, now)); err != nil {
		return nil, err
	}
	if err := repo.Add(vault.LogPage); err != nil {
		return nil, err
	}
	message := vault.CommitMessage("undo", op.Summary, res.OperationID) + "atlas-undoes: " + op.ID + "\n"
	res.Commit, err = repo.Commit(message)
	if err != nil {
		return nil, err
	}
	res.ChangedPaths, _ = repo.ChangedPaths(res.Commit)
	sort.Strings(res.ChangedPaths)
	return res, nil
}

// Status is a small picture of the vault's git state.
type Status struct {
	Head        string `json:"head,omitempty"`
	Dirty       int    `json:"dirty"`
	Pending     bool   `json:"pending_recovery"`
	HasHistory  bool   `json:"has_history"`
	LastCommit  string `json:"last_commit,omitempty"`
	LastSubject string `json:"last_subject,omitempty"`
}

// Inspect reports the git state without changing anything.
func Inspect(v *vault.Vault) (*Status, error) {
	repo := v.Repo()
	st := &Status{HasHistory: repo.IsRepo() && repo.HasHead()}
	if in, err := Pending(v); err == nil && in != nil {
		st.Pending = true
	}
	if !st.HasHistory {
		return st, nil
	}
	entries, err := repo.Status()
	if err != nil {
		return nil, err
	}
	st.Dirty = len(entries)
	// The head is the newest commit that touched the vault. Inside a repository that is not
	// the repository's own head, which the code moves on without the vault.
	if commits, err := repo.Log(1); err == nil && len(commits) == 1 {
		st.Head = commits[0].SHA
		st.LastCommit = commits[0].Date.Format("2006-01-02")
		st.LastSubject = commits[0].Subject
	}
	return st, nil
}

// bytesEqual is here so the file reads without importing bytes twice in tests.
var _ = bytes.Equal
