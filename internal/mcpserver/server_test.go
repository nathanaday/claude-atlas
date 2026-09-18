package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/threads"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var now = time.Date(2026, 9, 17, 15, 0, 0, 0, time.UTC)

type client struct {
	t    *testing.T
	sess *mcp.ClientSession
}

// connectIn starts a server whose atlas home is h and whose session started in dir.
func connectIn(t *testing.T, h home.Home, dir string) *client {
	t.Helper()
	s := New(Options{Version: "test", ProjectDir: dir, Env: func(k string) string {
		if k == home.EnvHome {
			return h.Root
		}
		return ""
	}, Now: func() time.Time { return now }})
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := s.MCP().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	sess, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	return &client{t: t, sess: sess}
}

// call invokes a tool and decodes its structured result into out. It returns the error
// text for tool errors.
func (c *client) call(name string, args map[string]any, out any) string {
	c.t.Helper()
	res, err := c.sess.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		c.t.Fatalf("%s: protocol error %v", name, err)
	}
	if res.IsError {
		var parts []string
		for _, content := range res.Content {
			if tc, ok := content.(*mcp.TextContent); ok {
				parts = append(parts, tc.Text)
			}
		}
		return strings.Join(parts, " ")
	}
	if out != nil {
		data, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("%s: decode %v: %s", name, err, data)
		}
	}
	return ""
}

// atlas is one machine: a home with a config, a knowledge base kb under the vaults
// directory, and a project webapp in a work folder that uses kb. withGit makes the work
// folder a repository with one commit.
type atlas struct {
	h    home.Home
	cfg  *home.Config
	kb   *vault.Vault
	work string
	p    *project.Project
}

func newAtlas(t *testing.T, withGit bool) *atlas {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default()
	kbPath := filepath.Join(root, "Vaults", "kb")
	if _, err := vault.Init(kbPath, vault.Options{Name: "kb", Scope: "Test knowledge."}, now); err != nil {
		t.Fatal(err)
	}
	cfg.AddKnowledge(kbPath)
	kb, err := vault.Open(kbPath)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "code", "webapp")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# webapp\n\nThe app.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withGit {
		repo := gitx.Repo{Dir: work}
		if err := repo.Init(); err != nil {
			t.Fatal(err)
		}
		if err := repo.AddAll(); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Commit("initial"); err != nil {
			t.Fatal(err)
		}
	}
	p, _, err := project.Init(work, project.Options{Name: "webapp", Description: "The app.", Knowledge: &project.Knowledge{ID: kb.Config.ID, Name: "kb"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddProject(work)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return &atlas{h: h, cfg: cfg, kb: kb, work: work, p: p}
}

func (a *atlas) inProject(t *testing.T) *client {
	return connectIn(t, a.h, filepath.Join(a.work, "src"))
}
func (a *atlas) inKnowledge(t *testing.T) *client { return connectIn(t, a.h, a.kb.Path("wiki")) }

const page = "---\ntitle: %s\ntype: %s\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - x\n---\n# %s\n\n%s\n"

func TestToolsListAndStatus(t *testing.T) {
	a := newAtlas(t, true)
	c := a.inProject(t)
	tools, err := c.sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != strings.Join(ToolNames(), ",") {
		t.Fatalf("ToolNames %v, registered %v", ToolNames(), names)
	}
	var st Status
	if msg := c.call("status", nil, &st); msg != "" {
		t.Fatal(msg)
	}
	if st.Kind != "project" || st.Name != "webapp" || st.Path != a.work || st.Description != "The app." || st.Git == nil || st.Knowledge == nil || st.Knowledge.Path != a.kb.Root || st.Threads == nil || st.Described != nil {
		t.Fatalf("project status %+v", st)
	}
	if !strings.Contains(strings.Join(st.Warnings, " "), registry.NotDescribed) {
		t.Fatalf("an undescribed project warns: %v", st.Warnings)
	}

	k := a.inKnowledge(t)
	st = Status{}
	if msg := k.call("status", nil, &st); msg != "" {
		t.Fatal(msg)
	}
	if st.Kind != "knowledge" || st.Name != "kb" || st.Scope != "Test knowledge." || st.Pages != 4 || st.VaultGit == nil || !st.VaultGit.HasHistory || st.LastOperation == nil || len(st.Projects) != 1 || st.Projects[0].Name != "webapp" || st.Projects[0].Described {
		t.Fatalf("knowledge status %+v", st)
	}

	none := connectIn(t, a.h, t.TempDir())
	if msg := none.call("status", nil, nil); !strings.Contains(msg, "not in a claude-atlas") {
		t.Fatalf("no place: %q", msg)
	}
}

func TestIngestWorkflowFromAProjectRecordsVia(t *testing.T) {
	a := newAtlas(t, false)
	c := a.inProject(t)
	os.WriteFile(a.kb.Path("inbox/paper.md"), []byte("# A paper\n\nThe claim.\n"), 0o644)
	os.WriteFile(a.p.Path("inbox/idea.md"), []byte("Try the thing.\n"), 0o644)

	var inbox InboxOut
	if msg := c.call("inbox", nil, &inbox); msg != "" {
		t.Fatal(msg)
	}
	if inbox.Vault != a.kb.Root || len(inbox.Files) != 1 || inbox.Files[0].Captured || len(inbox.Notes) != 1 || inbox.Notes[0] != "inbox/idea.md" {
		t.Fatalf("inbox %+v", inbox)
	}
	var cap struct {
		Sources []struct {
			SourceID   string `json:"source_id"`
			StoredPath string `json:"stored_path"`
		} `json:"sources"`
		Commit string `json:"commit"`
	}
	if msg := c.call("capture", map[string]any{"paths": []string{"paper.md"}}, &cap); msg != "" {
		t.Fatal(msg)
	}
	if len(cap.Sources) != 1 || cap.Commit == "" {
		t.Fatalf("capture %+v", cap)
	}
	led, _ := os.ReadFile(a.kb.Path(vault.LedgerPath))
	if !strings.Contains(string(led), `"via"`) || !strings.Contains(string(led), a.p.Config.ID) {
		t.Fatalf("the record names the project it came through: %s", led)
	}
	var route RouteOut
	c.call("route", map[string]any{"type": "source", "title": "A paper"}, &route)
	if route.Path != "wiki/sources/A paper.md" || route.Exists || route.Vault != a.kb.Root {
		t.Fatalf("route %+v", route)
	}
	index, _ := os.ReadFile(a.kb.Path(vault.IndexPage))
	newIndex := strings.Replace(string(index), "- No sources yet.", "- [[A paper]]", 1)
	var plan PlanOut
	msg := c.call("plan", map[string]any{
		"kind": "ingest", "summary": "Ingest A paper",
		"writes": []map[string]any{
			{"path": route.Path, "mode": "create", "content": strings.NewReplacer("%s", "A paper").Replace(page)},
			{"path": vault.IndexPage, "mode": "replace", "content": newIndex},
			{"path": "inbox/paper.md", "mode": "delete"},
		},
		"sources": []map[string]any{{"id": cap.Sources[0].SourceID, "ingested": true, "pages": []string{route.Path}, "authority": "primary"}},
	}, &plan)
	if msg != "" {
		t.Fatal(msg)
	}
	if len(plan.Preview.Creates) != 1 || len(plan.Preview.Replaces) != 1 || len(plan.Preview.Deletes) != 1 || len(plan.Warnings) != 0 || !strings.Contains(plan.Summary, "(via webapp)") {
		t.Fatalf("plan %+v", plan)
	}
	var res struct {
		Commit string `json:"commit"`
	}
	if msg := c.call("apply", map[string]any{"plan_id": plan.PlanID}, &res); msg != "" || res.Commit == "" {
		t.Fatalf("apply %q %+v", msg, res)
	}
	if _, err := os.Stat(a.kb.Path("inbox/paper.md")); !os.IsNotExist(err) {
		t.Fatal("the inbox file is gone after the ingest")
	}
	if msg := c.call("apply", map[string]any{"plan_id": plan.PlanID}, nil); !strings.Contains(msg, "single-use") {
		t.Fatalf("a plan applies once: %q", msg)
	}
	var hist HistoryOut
	c.call("history", map[string]any{"limit": 5}, &hist)
	if hist.Vault != a.kb.Root || len(hist.Operations) < 2 || hist.Operations[0].Kind != "ingest" {
		t.Fatalf("history %+v", hist)
	}
	var lintOut struct {
		Summary struct {
			PagesScanned int `json:"pages_scanned"`
		} `json:"summary"`
	}
	if msg := c.call("lint", nil, &lintOut); msg != "" || lintOut.Summary.PagesScanned != 5 {
		t.Fatalf("lint %q %+v", msg, lintOut)
	}
	if msg := c.call("undo", map[string]any{"operation_id": hist.Operations[0].ID}, nil); msg != "" {
		t.Fatal(msg)
	}
	if _, err := os.Stat(a.kb.Path(route.Path)); !os.IsNotExist(err) {
		t.Fatal("undo removed the page")
	}
}

func TestPlanRefusalsAndModeFromAKnowledgeBase(t *testing.T) {
	a := newAtlas(t, false)
	k := a.inKnowledge(t)
	if msg := k.call("plan", map[string]any{"kind": "config", "summary": "x", "writes": []map[string]any{{"path": vault.Marker, "mode": "replace", "content": "{}"}}}, nil); !strings.Contains(msg, "mode tool") {
		t.Fatalf("config through plan: %q", msg)
	}
	if msg := k.call("plan", map[string]any{"kind": "save", "summary": "x", "writes": []map[string]any{{"path": "ideas/x.md", "mode": "create", "content": "x"}}}, nil); !strings.Contains(msg, "scratch") {
		t.Fatalf("ideas are the user's: %q", msg)
	}
	var mode ModeOut
	if msg := k.call("mode", nil, &mode); msg != "" || mode.Mode != "generic" || len(mode.Types) != 3 {
		t.Fatalf("mode %q %+v", msg, mode)
	}
	if msg := k.call("mode", map[string]any{"set": "lyt"}, &mode); msg != "" || mode.Plan == nil || mode.Previous != "generic" {
		t.Fatalf("mode set %q %+v", msg, mode)
	}
	if msg := k.call("apply", map[string]any{"plan_id": mode.Plan.PlanID}, nil); msg != "" {
		t.Fatal(msg)
	}
	kb, _ := vault.Open(a.kb.Root)
	if kb.Config.Mode != vault.LYT {
		t.Fatalf("mode after apply: %s", kb.Config.Mode)
	}
}

func TestThreadToolsInAProject(t *testing.T) {
	a := newAtlas(t, true)
	c := a.inProject(t)
	os.WriteFile(a.p.Path("inbox/note.md"), []byte("# Fix the login\n\nIt loops.\n"), 0o644)

	var ph PhaseOut
	if msg := c.call("phase", map[string]any{"action": "create", "title": "Alarm quality", "goal": "Fewer false alarms."}, &ph); msg != "" {
		t.Fatal(msg)
	}
	if ph.Project != "webapp" || ph.Phase == nil || ph.Phase.Order != 1 || ph.File != a.p.Path("phases/Alarm quality.md") {
		t.Fatalf("phase %+v", ph)
	}
	if msg := c.call("thread", map[string]any{"title": "Filter vehicles", "text": "Cars trip the alarm.", "phase": "Nope"}, nil); !strings.Contains(msg, "no phase named") {
		t.Fatalf("an unknown phase is refused: %q", msg)
	}
	var opened ThreadOut
	if msg := c.call("thread", map[string]any{"title": "Filter vehicles", "text": "Cars trip the alarm.", "phase": "alarm quality", "priority": "high"}, &opened); msg != "" {
		t.Fatal(msg)
	}
	if opened.Stage != threads.Stub || opened.Phase != "Alarm quality" || opened.Priority != "high" || opened.Files["stub"] != a.p.Path("stubs/Filter vehicles.md") || opened.Card != a.p.Path("threads/Filter vehicles.md") {
		t.Fatalf("opened %+v", opened)
	}
	var fromNote ThreadOut
	if msg := c.call("thread", map[string]any{"from": "inbox/note.md"}, &fromNote); msg != "" {
		t.Fatal(msg)
	}
	if fromNote.Title != "Fix the login" {
		t.Fatalf("title from the note: %+v", fromNote)
	}
	if _, err := os.Stat(a.p.Path("inbox/note.md")); !os.IsNotExist(err) {
		t.Fatal("the note is removed once the stub exists")
	}
	if msg := c.call("thread", map[string]any{"title": "x", "stage": "plan", "text": "x"}, nil); !strings.Contains(msg, "starts with its stub") {
		t.Fatalf("a new thread with a later stage: %q", msg)
	}
	if msg := c.call("thread", map[string]any{"id": opened.ID, "text": "x"}, nil); !strings.Contains(msg, "text needs stage") {
		t.Fatalf("text with no stage: %q", msg)
	}

	// File documents, with a card change in the same call.
	var filed ThreadOut
	if msg := c.call("thread", map[string]any{"id": opened.ID, "stage": "spec", "text": "Vehicles never alarm.", "priority": "normal"}, &filed); msg != "" {
		t.Fatal(msg)
	}
	if filed.Stage != threads.Spec || filed.Priority != "normal" || filed.Files["spec"] != a.p.Path("specs/Filter vehicles.md") {
		t.Fatalf("filed %+v", filed)
	}
	if msg := c.call("thread", map[string]any{"id": opened.ID, "stage": "spec", "text": "again"}, nil); !strings.Contains(msg, "revise it with Edit") {
		t.Fatalf("a second spec: %q", msg)
	}
	var board ThreadsOut
	if msg := c.call("threads", nil, &board); msg != "" {
		t.Fatal(msg)
	}
	pb := board.Projects[0]
	if len(board.Projects) != 1 || pb.Name != "webapp" || len(pb.Open) != 2 || pb.Open[0].ID != opened.ID || len(pb.Phases) != 1 || pb.Phases[0].Open != 1 || pb.Counts.Open != 2 || pb.Counts.Spec != 1 || pb.Board != a.p.Path(project.ThreadsIndex) {
		t.Fatalf("threads %+v", board)
	}
	if msg := c.call("threads", map[string]any{"id": "fix the"}, &board); msg != "" || len(board.Projects[0].Open) != 1 || board.Projects[0].Open[0].ID != fromNote.ID {
		t.Fatalf("one thread %q %+v", msg, board)
	}

	var set ThreadOut
	if msg := c.call("thread", map[string]any{"id": "Fix the login", "priority": "low", "blocked": "the vendor"}, &set); msg != "" || set.Priority != "low" || set.Blocked != "the vendor" {
		t.Fatalf("set by title %q %+v", msg, set)
	}
	if msg := c.call("thread", map[string]any{"id": fromNote.ID}, &set); msg != "" || set.ID != fromNote.ID {
		t.Fatalf("a touch %q %+v", msg, set)
	}
	if msg := c.call("thread", map[string]any{"id": opened.ID, "stage": "receipt", "text": "Shipped."}, nil); !strings.Contains(msg, "needs an outcome") {
		t.Fatalf("a receipt with no outcome: %q", msg)
	}
	if msg := c.call("thread", map[string]any{"id": opened.ID, "stage": "receipt", "outcome": "completed", "text": "Shipped."}, &set); msg != "" {
		t.Fatal(msg)
	}
	if !set.Closed() || set.Outcome != threads.Completed || set.Path != "threads/archive/Filter vehicles.md" {
		t.Fatalf("a receipt closes the thread: %+v", set)
	}
	index, _ := os.ReadFile(a.p.Path(project.ThreadsIndex))
	if !strings.Contains(string(index), "finished") || !strings.Contains(string(index), "Fix the login") {
		t.Fatalf("the board follows: %s", index)
	}
	if msg := c.call("phase", map[string]any{"action": "remove", "title": "Alarm quality"}, nil); !strings.Contains(msg, "still names") {
		t.Fatalf("remove refuses while a thread names the phase: %q", msg)
	}
	if msg := c.call("phase", map[string]any{"action": "rename", "title": "Alarm quality", "new_title": "Alarms"}, &ph); msg != "" || ph.Phase.Title != "Alarms" {
		t.Fatalf("rename %q %+v", msg, ph)
	}
	moved, _ := os.ReadFile(a.p.Path("threads/archive/Filter vehicles.md"))
	if !strings.Contains(string(moved), `phase: "Alarms"`) {
		t.Fatalf("rename follows the thread: %s", moved)
	}
	if msg := c.call("thread", map[string]any{"id": opened.ID, "reopen": true}, &set); msg != "" || set.Closed() || set.Stage != threads.Spec {
		t.Fatalf("reopen %q %+v", msg, set)
	}
	order := 5
	if msg := c.call("phase", map[string]any{"action": "reorder", "title": "Alarms", "order": order}, &ph); msg != "" || ph.Phase.Order != 5 {
		t.Fatalf("reorder %q %+v", msg, ph)
	}
	if msg := c.call("phase", map[string]any{"action": "grow", "title": "x"}, nil); !strings.Contains(msg, "action must be") {
		t.Fatalf("unknown action: %q", msg)
	}
}

func TestThreadToolsFromAKnowledgeBaseNameTheProject(t *testing.T) {
	a := newAtlas(t, false)
	k := a.inKnowledge(t)
	if msg := k.call("thread", map[string]any{"title": "Do it"}, nil); !strings.Contains(msg, "name the project") {
		t.Fatalf("a knowledge base session names the project: %q", msg)
	}
	var opened ThreadOut
	if msg := k.call("thread", map[string]any{"project": "webapp", "title": "Do it", "text": "Now."}, &opened); msg != "" || opened.Project != "webapp" {
		t.Fatalf("open a thread in a project %q %+v", msg, opened)
	}
	// A project that uses another knowledge base is refused.
	other := filepath.Join(filepath.Dir(a.work), "other")
	os.MkdirAll(other, 0o755)
	if _, _, err := project.Init(other, project.Options{Name: "other"}, now); err != nil {
		t.Fatal(err)
	}
	a.cfg.AddProject(other)
	a.h.Save(a.cfg)
	if msg := k.call("thread", map[string]any{"project": "other", "title": "x"}, nil); !strings.Contains(msg, "does not use") {
		t.Fatalf("a project of another knowledge base: %q", msg)
	}
	var board ThreadsOut
	if msg := k.call("threads", nil, &board); msg != "" || len(board.Projects) != 1 || len(board.Projects[0].Open) != 1 {
		t.Fatalf("threads lists every project of the knowledge base: %q %+v", msg, board)
	}
}

func TestStubAndStageInBothSessions(t *testing.T) {
	a := newAtlas(t, true)
	front := "---\ntitle: Backprop\ntype: concept\nstatus: developing\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# Backprop\n\nSee [[Gradient]].\n"
	os.MkdirAll(a.kb.Path("wiki/concepts"), 0o755)
	os.WriteFile(a.kb.Path("wiki/concepts/Backprop.md"), []byte(front), 0o644)
	c := a.inProject(t)
	var stub struct {
		Stubs []struct {
			Title string `json:"title"`
			Path  string `json:"path"`
		} `json:"stubs"`
		OperationID string `json:"operation_id"`
	}
	if msg := c.call("stub", map[string]any{"titles": []map[string]any{{"title": "Gradient", "target": "x"}}}, nil); !strings.Contains(msg, "target is gone") {
		t.Fatalf("target: %q", msg)
	}
	if msg := c.call("stub", nil, &stub); msg != "" || len(stub.Stubs) != 1 || stub.Stubs[0].Path != "wiki/concepts/Gradient.md" {
		t.Fatalf("stub %q %+v", msg, stub)
	}
	var hist HistoryOut
	c.call("history", map[string]any{"limit": 1}, &hist)
	if len(hist.Operations) != 1 || !strings.Contains(hist.Operations[0].Summary, "via webapp") {
		t.Fatalf("a project session's stub names the project: %+v", hist.Operations)
	}

	// Stage this project's snapshot into the knowledge base's inbox.
	var staged StageOut
	if msg := c.call("stage", nil, &staged); msg != "" {
		t.Fatal(msg)
	}
	if staged.Snapshot == nil || !staged.Snapshot.New || staged.Snapshot.Commit == "" || !strings.HasPrefix(staged.Snapshot.To, "inbox/webapp-") {
		t.Fatalf("snapshot %+v", staged)
	}
	if msg := c.call("stage", nil, &staged); msg != "" || staged.Snapshot.New {
		t.Fatalf("the same snapshot is not written twice: %q %+v", msg, staged)
	}
	// Stage files from a folder into the inbox, from the knowledge base session.
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "notes.md"), []byte("notes\n"), 0o644)
	k := a.inKnowledge(t)
	if msg := k.call("stage", map[string]any{"paths": []string{src}, "dry_run": true}, &staged); msg != "" || staged.Plan == nil || len(staged.Plan.New) != 1 || staged.Result != nil {
		t.Fatalf("dry run %q %+v", msg, staged)
	}
	if msg := k.call("stage", map[string]any{"paths": []string{src}}, &staged); msg != "" || staged.Result == nil || len(staged.Result.Staged) != 1 {
		t.Fatalf("stage %q %+v", msg, staged)
	}
	if _, err := os.Stat(a.kb.Path("inbox/" + filepath.Base(src) + "/notes.md")); err != nil {
		t.Fatal("the file is in the inbox")
	}
	if msg := k.call("stage", map[string]any{"paths": []string{src}, "project": "webapp"}, nil); !strings.Contains(msg, "not both") {
		t.Fatalf("project or paths: %q", msg)
	}
	if msg := k.call("stage", map[string]any{"project": "webapp"}, &staged); msg != "" || staged.Snapshot == nil {
		t.Fatalf("a knowledge base session stages a project by name: %q", msg)
	}
	if msg := k.call("stage", map[string]any{}, &staged); msg != "" || staged.Plan == nil || len(staged.Plan.New) != 0 || len(staged.Plan.Unchanged) != 1 {
		t.Fatalf("no paths stages what is new in the remembered folder: %q %+v", msg, staged.Plan)
	}
}

func TestAProjectWithoutAKnowledgeBase(t *testing.T) {
	a := newAtlas(t, false)
	a.p.Config.Knowledge = nil
	if err := a.p.Save(); err != nil {
		t.Fatal(err)
	}
	c := a.inProject(t)
	var st Status
	if msg := c.call("status", nil, &st); msg != "" || st.Knowledge != nil || !strings.Contains(strings.Join(st.Warnings, " "), "uses no knowledge base") {
		t.Fatalf("status %q %+v", msg, st)
	}
	if msg := c.call("capture", map[string]any{"paths": []string{"x"}}, nil); !strings.Contains(msg, "uses no knowledge base") {
		t.Fatalf("capture: %q", msg)
	}
	var opened ThreadOut
	if msg := c.call("thread", map[string]any{"title": "Still works"}, &opened); msg != "" || opened.ID == "" {
		t.Fatalf("threads work without a knowledge base: %q", msg)
	}
	var inbox InboxOut
	if msg := c.call("inbox", nil, &inbox); msg != "" || inbox.Vault != "" || len(inbox.Files) != 0 {
		t.Fatalf("inbox %q %+v", msg, inbox)
	}
	// A knowledge base the atlas cannot find says so.
	a.p.Config.Knowledge = &project.Knowledge{ID: "0000", Name: "gone"}
	a.p.Save()
	if msg := c.call("route", map[string]any{"type": "concept", "title": "x"}, nil); !strings.Contains(msg, "gone") {
		t.Fatalf("a missing knowledge base is named: %q", msg)
	}
	var pl struct {
		Warnings []string `json:"warnings"`
	}
	c.call("status", nil, &pl)
	if !strings.Contains(strings.Join(pl.Warnings, " "), "gone") {
		t.Fatalf("status warns: %v", pl.Warnings)
	}
}
