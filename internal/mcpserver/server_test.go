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
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

var now = time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)

type client struct {
	t    *testing.T
	sess *mcp.ClientSession
}

func connect(t *testing.T, projectDir string) *client {
	t.Helper()
	s := New(Options{Version: "test", ProjectDir: projectDir, Env: func(string) string { return "" }, Now: func() time.Time { return now }})
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

// call invokes a tool and decodes its structured result into out. It returns the error text for tool errors.
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

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "v")
	if _, err := vault.Init(root, vault.Options{Kind: vault.Project, Mode: vault.Generic}, now); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.Open(root)
	return v
}

const page = "---\ntitle: %s\ntype: %s\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - x\n---\n# %s\n\n%s\n"

func TestToolsListAndStatus(t *testing.T) {
	v := newVault(t)
	c := connect(t, v.Path("wiki"))
	tools, err := c.sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "apply,capture,history,inbox,lint,mode,plan,plant,repos,route,status,stub,tasks,undo" {
		t.Fatalf("tools %v", names)
	}
	var st Status
	if msg := c.call("status", nil, &st); msg != "" {
		t.Fatal(msg)
	}
	if st.Vault != v.Root || st.Mode != "generic" || st.Pages != 5 || !st.Git.HasHistory || st.LastOperation == nil || st.LastOperation.Kind != "setup" {
		t.Fatalf("status %+v", st)
	}
	if msg := c.call("status", map[string]any{"vault": t.TempDir()}, nil); !strings.Contains(msg, "not a claude-atlas vault") {
		t.Fatalf("non-vault: %q", msg)
	}
}

func TestIngestWorkflow(t *testing.T) {
	v := newVault(t)
	c := connect(t, v.Root)
	os.WriteFile(v.Path("inbox/paper.md"), []byte("# A paper\n\nThe claim.\n"), 0o644)

	var inbox InboxOut
	c.call("inbox", nil, &inbox)
	if len(inbox.Files) != 1 || inbox.Files[0].Captured {
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
	var route vault.Route
	c.call("route", map[string]any{"type": "source", "title": "A paper"}, &route)
	if route.Path != "wiki/sources/A paper.md" || route.Exists || !strings.Contains(route.Skeleton, "type: source") {
		t.Fatalf("route %+v", route)
	}
	index, _ := os.ReadFile(v.Path(vault.IndexPage))
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
	if len(plan.Preview.Creates) != 1 || len(plan.Preview.Replaces) != 1 || len(plan.Preview.Deletes) != 1 || len(plan.Warnings) != 0 || !strings.Contains(plan.Next, plan.PlanID) {
		t.Fatalf("plan %+v", plan)
	}
	var applied struct {
		OperationID  string   `json:"operation_id"`
		Commit       string   `json:"commit"`
		ChangedPaths []string `json:"changed_paths"`
	}
	if msg := c.call("apply", map[string]any{"plan_id": plan.PlanID}, &applied); msg != "" {
		t.Fatal(msg)
	}
	if applied.OperationID != plan.OperationID || len(applied.ChangedPaths) != 5 {
		t.Fatalf("applied %+v", applied)
	}
	if msg := c.call("apply", map[string]any{"plan_id": plan.PlanID}, nil); !strings.Contains(msg, "no plan") {
		t.Fatalf("second apply: %q", msg)
	}
	if _, err := os.Stat(v.Path("inbox/paper.md")); err == nil {
		t.Fatal("inbox file should be gone")
	}
	var hist HistoryOut
	c.call("history", map[string]any{"limit": 2}, &hist)
	if len(hist.Operations) != 2 || hist.Operations[0].ID != applied.OperationID || hist.Operations[1].Kind != "capture" {
		t.Fatalf("history %+v", hist)
	}
	var report struct {
		Summary struct {
			Pages int `json:"pages_scanned"`
		} `json:"summary"`
		DeadLinks []any `json:"dead_links"`
	}
	c.call("lint", nil, &report)
	if report.Summary.Pages != 6 || len(report.DeadLinks) != 0 {
		t.Fatalf("lint %+v", report)
	}
	var undone struct {
		Commit string `json:"commit"`
	}
	if msg := c.call("undo", map[string]any{"operation_id": applied.OperationID}, &undone); msg != "" {
		t.Fatal(msg)
	}
	if _, err := os.Stat(v.Path(route.Path)); err == nil {
		t.Fatal("undo should remove the page")
	}
}

func TestPlanErrorsAndReplacement(t *testing.T) {
	v := newVault(t)
	c := connect(t, v.Root)
	if msg := c.call("plan", map[string]any{"kind": "setup", "summary": "x", "writes": []map[string]any{{"path": "wiki/a.md", "mode": "create", "content": "x"}}}, nil); !strings.Contains(msg, "kind must be one of") {
		t.Fatalf("kind: %q", msg)
	}
	if msg := c.call("plan", map[string]any{"kind": "save", "summary": "x", "writes": []map[string]any{{"path": "wiki/a.md", "mode": "create", "content": "no front"}}}, nil); !strings.Contains(msg, "no frontmatter") {
		t.Fatalf("content: %q", msg)
	}
	content := strings.NewReplacer("%s", "B").Replace(page)
	var first, second PlanOut
	c.call("plan", map[string]any{"kind": "save", "summary": "one", "writes": []map[string]any{{"path": "wiki/B.md", "mode": "create", "content": content}}}, &first)
	c.call("plan", map[string]any{"kind": "save", "summary": "two", "writes": []map[string]any{{"path": "wiki/B.md", "mode": "create", "content": content}}}, &second)
	if msg := c.call("apply", map[string]any{"plan_id": first.PlanID}, nil); !strings.Contains(msg, "no plan") {
		t.Fatal("a newer plan for the same vault replaces the older one")
	}
	if msg := c.call("apply", map[string]any{"plan_id": second.PlanID}, nil); msg != "" {
		t.Fatal(msg)
	}
	var mode ModeOut
	c.call("mode", nil, &mode)
	if mode.Mode != "generic" || len(mode.Types) != 5 {
		t.Fatalf("mode %+v", mode)
	}
	c.call("mode", map[string]any{"set": "lyt"}, &mode)
	if mode.Plan == nil || mode.Previous != "generic" || mode.Mode != "lyt" {
		t.Fatalf("mode set %+v", mode)
	}
	if msg := c.call("apply", map[string]any{"plan_id": mode.Plan.PlanID}, nil); msg != "" {
		t.Fatal(msg)
	}
	if again, _ := vault.Open(v.Root); again.Config.Mode != vault.LYT {
		t.Fatal("mode should be lyt")
	}
	if msg := c.call("mode", map[string]any{"set": "para"}, nil); !strings.Contains(msg, "generic or lyt") {
		t.Fatalf("bad mode: %q", msg)
	}
}

func TestTaskTools(t *testing.T) {
	v := newVault(t)
	c := connect(t, v.Root)
	var route struct {
		Path     string `json:"path"`
		Type     string `json:"type"`
		Skeleton string `json:"skeleton"`
		Exists   bool   `json:"exists"`
	}
	if msg := c.call("route", map[string]any{"type": "task", "title": "Fix the dialog"}, &route); msg != "" {
		t.Fatal(msg)
	}
	if route.Path != "wiki/tasks/Fix the dialog.md" || route.Type != "task" || !strings.Contains(route.Skeleton, "task_id: task-") || route.Exists {
		t.Fatalf("route %+v", route)
	}
	os.MkdirAll(v.Path("inbox/tasks"), 0o755)
	os.WriteFile(v.Path("inbox/tasks/idea.md"), []byte("# Fix the dialog\n\nIt quits."), 0o644)
	var inbox struct {
		Files []struct {
			Path string `json:"path"`
			Area string `json:"area"`
		} `json:"files"`
	}
	c.call("inbox", nil, &inbox)
	if len(inbox.Files) != 1 || inbox.Files[0].Area != "tasks" {
		t.Fatalf("inbox %+v", inbox)
	}
	var st Status
	c.call("status", nil, &st)
	if st.Tasks.Notes != 1 || len(st.Warnings) == 0 || !strings.Contains(strings.Join(st.Warnings, " "), "task note") {
		t.Fatalf("status %+v", st)
	}
	var planted PlantOut
	if msg := c.call("plant", map[string]any{"text": "# Fix the dialog\n\nIt quits.", "from": "inbox/tasks/idea.md", "priority": "high"}, &planted); msg != "" {
		t.Fatal(msg)
	}
	if planted.Path != "wiki/tasks/Fix the dialog.md" || !strings.HasPrefix(planted.ID, "task-") || planted.OperationID == "" {
		t.Fatalf("planted %+v", planted)
	}
	if _, err := os.Stat(v.Path("inbox/tasks/idea.md")); err == nil {
		t.Fatal("the note should be gone")
	}
	var list TasksOut
	c.call("tasks", nil, &list)
	if list.Counts.Open != 1 || list.Counts.Planted != 1 || len(list.Tasks) != 1 || list.Tasks[0].Priority != "high" || len(list.Tasks[0].History) != 1 || len(list.Notes) != 0 {
		t.Fatalf("tasks %+v", list)
	}
	if msg := c.call("route", map[string]any{"type": "task", "title": "Fix the dialog"}, &route); msg != "" || !route.Exists {
		t.Fatalf("route sees the page: %s %+v", msg, route)
	}
	if msg := c.call("plant", map[string]any{"text": "", "title": ""}, nil); msg == "" {
		t.Fatal("empty plant should fail")
	}
	if msg := c.call("plant", map[string]any{"text": "x", "priority": "urgent"}, nil); msg == "" {
		t.Fatal("bad priority should fail")
	}
	// A task plan through plan and apply: move it to active with a plan section.
	page, _ := os.ReadFile(v.Path(planted.Path))
	active := strings.Replace(string(page), "status: planted", "status: active", 1) + "\n## Plan\n\n1. Look.\n"
	var out PlanOut
	if msg := c.call("plan", map[string]any{"kind": "task", "summary": "start Fix the dialog", "writes": []map[string]any{{"path": planted.Path, "mode": "replace", "content": active}}}, &out); msg != "" {
		t.Fatal(msg)
	}
	c.call("apply", map[string]any{"plan_id": out.PlanID}, nil)
	c.call("tasks", nil, &list)
	if list.Counts.Active != 1 || !list.Tasks[0].HasPlan || len(list.Tasks[0].History) != 2 {
		t.Fatalf("after apply %+v", list)
	}
}

func TestReposToolAndStatusInARepository(t *testing.T) {
	v := newVault(t)
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default(filepath.Join(root, "Vaults"), filepath.Join(root, "Atlas"))
	os.MkdirAll(h.Root, 0o755)
	h.Save(cfg)
	os.MkdirAll(cfg.TreeRoot(), 0o755)
	p, err := vaults.Register(cfg, v.Root, vaults.RegisterOptions{Name: "V"})
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "code")
	os.MkdirAll(filepath.Join(repo, "src"), 0o755)
	if _, err := vaults.AddLink(cfg, p, repo, true); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Version: "test", ProjectDir: filepath.Join(repo, "src"), Env: func(k string) string {
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
	defer sess.Close()
	c := &client{t: t, sess: sess}
	var status Status
	if msg := c.call("status", nil, &status); msg != "" {
		t.Fatal(msg)
	}
	if status.Vault != v.Root || status.Repository == nil || status.Repository.Name != "code" || status.Repository.Changes != "commit" || status.Repository.Branch != "main" {
		t.Fatalf("status in a repo: %+v %+v", status, status.Repository)
	}
	var repos ReposOut
	c.call("repos", nil, &repos)
	if len(repos.Repos) != 1 || repos.Repos[0].Path != repo || !strings.Contains(repos.Repos[0].Policy, "current branch") {
		t.Fatalf("repos %+v", repos)
	}
}

func TestStubTool(t *testing.T) {
	v := newVault(t)
	os.MkdirAll(v.Path("wiki/concepts"), 0o755)
	os.WriteFile(v.Path("wiki/concepts/Training.md"), []byte("---\ntitle: Training\ntype: concept\nstatus: developing\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# Training\n\nSee [[vanishing gradient problem]].\n"), 0o644)
	c := connect(t, v.Path("wiki"))
	var report struct {
		Summary struct {
			Wanted int `json:"wanted_pages"`
		} `json:"summary"`
	}
	c.call("lint", nil, &report)
	if report.Summary.Wanted != 1 {
		t.Fatalf("lint %+v", report)
	}
	if msg := c.call("stub", map[string]any{"titles": []map[string]any{{"title": "Nowhere"}}}, nil); !strings.Contains(msg, "nothing in the wiki links to") {
		t.Fatalf("refusal %q", msg)
	}
	var out txn.StubResult
	if msg := c.call("stub", map[string]any{"type": "question"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Stubs) != 1 || out.Stubs[0].Path != "wiki/questions/vanishing gradient problem.md" || out.OperationID == "" {
		t.Fatalf("stub %+v", out)
	}
	out = txn.StubResult{}
	if msg := c.call("stub", nil, &out); msg != "" || len(out.Stubs) != 0 || out.OperationID != "" {
		t.Fatalf("nothing left: %q %+v", msg, out)
	}
}
