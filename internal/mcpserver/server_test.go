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
	"github.com/nathanaday/claude-atlas/internal/ledger"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

var now = time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)

type client struct {
	t    *testing.T
	sess *mcp.ClientSession
}

// connectIn starts a server whose atlas home is h and whose session started in projectDir.
func connectIn(t *testing.T, h home.Home, projectDir string) *client {
	t.Helper()
	s := New(Options{Version: "test", ProjectDir: projectDir, Env: func(k string) string {
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

// connect starts a server with no atlas: a home directory that holds no config.
func connect(t *testing.T, projectDir string) *client {
	t.Helper()
	return connectIn(t, home.Home{Root: filepath.Join(t.TempDir(), "home")}, projectDir)
}

// mounted builds an atlas whose vaults directory holds a project p and a knowledge base
// kb, with kb mounted on p for writing.
func mounted(t *testing.T) (home.Home, *home.Config, *vault.Vault, *vault.Vault) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default(filepath.Join(root, "Vaults"))
	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	projectPath := vaults.PathFor(cfg.VaultsDir, vault.Project, "p")
	kbPath := vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "kb")
	if _, err := vault.Init(projectPath, vault.Options{Kind: vault.Project, Name: "p"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Init(kbPath, vault.Options{Kind: vault.Knowledge, Name: "kb"}, now); err != nil {
		t.Fatal(err)
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pe, ke := ix.ByPath(projectPath), ix.ByPath(kbPath)
	if pe == nil || ke == nil {
		t.Fatalf("the scan found %d entries, not both vaults", len(ix.Entries))
	}
	if _, err := vaults.Mount(*pe, *ke, vault.AccessWrite, "", now); err != nil {
		t.Fatal(err)
	}
	p, err := vault.Open(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := vault.Open(kbPath)
	if err != nil {
		t.Fatal(err)
	}
	return h, cfg, p, kb
}

// rescan reads the vaults afresh and returns the entries for the project and the
// knowledge base an identity change just touched.
func rescan(t *testing.T, cfg *home.Config, p, kb *vault.Vault) (project, knowledge registry.Entry) {
	t.Helper()
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pe, ke := ix.ByPath(p.Root), ix.ByPath(kb.Root)
	if pe == nil || ke == nil {
		t.Fatal("the scan lost a vault")
	}
	return *pe, *ke
}

const kbPage = "---\ntype: concept\ntitle: Backprop\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# Backprop\n\ntext\n"

// linkPage writes a concept page whose body carries the links a stub needs to want.
func linkPage(t *testing.T, v *vault.Vault, title, body string) {
	t.Helper()
	front := "---\ntitle: " + title + "\ntype: concept\nstatus: developing\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# " + title + "\n\n"
	os.MkdirAll(v.Path("wiki/concepts"), 0o755)
	if err := os.WriteFile(v.Path("wiki/concepts/"+title+".md"), []byte(front+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

func newKnowledge(t *testing.T) *vault.Vault {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "kb")
	if _, err := vault.Init(root, vault.Options{Kind: vault.Knowledge}, now); err != nil {
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
	if strings.Join(names, ",") != "apply,capture,history,inbox,lint,mode,mounts,plan,plant,repos,route,status,stub,tasks,undo" {
		t.Fatalf("tools %v", names)
	}
	if strings.Join(ToolNames(), ",") != strings.Join(names, ",") {
		t.Fatalf("ToolNames %v, registered %v", ToolNames(), names)
	}
	var st Status
	if msg := c.call("status", nil, &st); msg != "" {
		t.Fatal(msg)
	}
	if st.Vault != v.Root || st.Kind != "project" || st.ID == "" || st.Mode != "generic" || st.Pages != 5 || !st.Git.HasHistory || st.LastOperation == nil || st.LastOperation.Kind != "setup" {
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
	cfg := h.Default(filepath.Join(root, "Vaults"))
	os.MkdirAll(h.Root, 0o755)
	h.Save(cfg)
	if _, err := vaults.Register(h, cfg, v.Root); err != nil {
		t.Fatal(err)
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	entry := ix.ByPath(v.Root)
	if entry == nil {
		t.Fatal("project not scanned")
	}
	if _, _, err := vaults.CreateRepo(h, cfg, *entry, "code", "", now); err != nil {
		t.Fatal(err)
	}
	if err := vault.UpdateConfig(v.Root, "remote", now, func(c *vault.Config) error {
		c.Repos[0].Remote = "git@example.com:a/code.git"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c := connectIn(t, h, v.Path("repos/code"))
	var status Status
	if msg := c.call("status", nil, &status); msg != "" {
		t.Fatal(msg)
	}
	if status.Vault != v.Root || status.Repository == nil || status.Repository.Name != "code" || status.Repository.Changes != "pr" || status.Repository.Branch != "main" {
		t.Fatalf("status in a repo: %+v %+v", status, status.Repository)
	}
	var repos ReposOut
	c.call("repos", nil, &repos)
	if len(repos.Repos) != 1 || repos.Repos[0].Path != v.Path("repos/code") || repos.Repos[0].Changes != "pr" || !strings.Contains(repos.Repos[0].Policy, "pull request") {
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

func TestKnowledgeBaseTools(t *testing.T) {
	v := newKnowledge(t)
	c := connect(t, v.Root)
	var st Status
	if msg := c.call("status", nil, &st); msg != "" || st.Kind != "knowledge" || st.ID == "" || st.Name != "kb" || st.Tasks.Open != 0 {
		t.Fatalf("%s %+v", msg, st)
	}
	refused := map[string]map[string]any{
		"inbox":   {},
		"capture": {"paths": []string{"x.md"}},
		"plant":   {"title": "T", "text": "t"},
		"tasks":   {},
		"repos":   {},
		"route":   {"type": "task", "title": "T"},
	}
	for name, args := range refused {
		if msg := c.call(name, args, nil); !strings.Contains(msg, "knowledge base") {
			t.Errorf("%s in a knowledge base: %q", name, msg)
		}
	}
	if msg := c.call("route", map[string]any{"type": "question", "title": "Q"}, nil); !strings.Contains(msg, "knowledge base") {
		t.Errorf("route question: %q", msg)
	}
	page := "---\ntype: concept\ntitle: A\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# A\n\ntext\n"
	write := []map[string]any{{"path": "wiki/concepts/A.md", "mode": "create", "content": page}}
	for _, kind := range []string{"ingest", "save"} {
		msg := c.call("plan", map[string]any{"kind": kind, "summary": "x", "writes": write}, nil)
		if !strings.Contains(msg, "knowledge enters through a project") || !strings.Contains(msg, "nothing mounts it yet") {
			t.Errorf("%s in a knowledge base: %q", kind, msg)
		}
	}
	var po PlanOut
	if msg := c.call("plan", map[string]any{"kind": "repair", "summary": "add A", "writes": write}, &po); msg != "" {
		t.Fatal(msg)
	}
	var res txn.Result
	if msg := c.call("apply", map[string]any{"plan_id": po.PlanID}, &res); msg != "" || res.Commit == "" {
		t.Fatalf("apply: %s %+v", msg, res)
	}
	var mo ModeOut
	if msg := c.call("mode", nil, &mo); msg != "" || strings.Join(mo.Types, ",") != "source,entity,concept" {
		t.Fatalf("mode: %s %+v", msg, mo)
	}
}

func TestKnowledgeBaseThroughAProjectSession(t *testing.T) {
	h, _, p, kb := mounted(t)
	c := connectIn(t, h, p.Root)

	var list MountsOut
	if msg := c.call("mounts", nil, &list); msg != "" {
		t.Fatal(msg)
	}
	if len(list.Mounts) != 1 {
		t.Fatalf("mounts %+v", list)
	}
	m := list.Mounts[0]
	if m.Name != "kb" || m.ID != kb.Config.ID || m.Access != "write" || m.Effective != "write" || m.Path != kb.Path("wiki") || m.Link != "kb/kb" || m.Error != "" {
		t.Fatalf("mount %+v", m)
	}
	if m.Pages == nil || *m.Pages == 0 {
		t.Fatalf("mount pages %+v", m.Pages)
	}

	os.WriteFile(p.Path("inbox/paper.md"), []byte("# A paper\n\nThe claim.\n"), 0o644)
	var captured struct {
		Sources []struct {
			SourceID string `json:"source_id"`
		} `json:"sources"`
		Commit string `json:"commit"`
	}
	if msg := c.call("capture", map[string]any{"vault": kb.Root, "paths": []string{"paper.md"}}, &captured); msg != "" {
		t.Fatal(msg)
	}
	if len(captured.Sources) != 1 || captured.Commit == "" {
		t.Fatalf("capture %+v", captured)
	}
	led, err := ledger.Load(kb.Path(vault.LedgerPath), now)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := led.Sources[captured.Sources[0].SourceID]
	if !ok || rec.Via == nil || rec.Via.Name != p.Name() || rec.Via.ID != p.Config.ID {
		t.Fatalf("the knowledge base's ledger names the project: %+v", rec)
	}
	if _, err := os.Stat(p.Path("inbox/paper.md")); err != nil {
		t.Fatal("the project's inbox file stays until its ingest operation removes it")
	}

	var po PlanOut
	writes := []map[string]any{{"path": "wiki/concepts/Backprop.md", "mode": "create", "content": kbPage}}
	if msg := c.call("plan", map[string]any{"vault": kb.Root, "kind": "save", "summary": "save Backprop", "writes": writes}, &po); msg != "" {
		t.Fatal(msg)
	}
	var res txn.Result
	if msg := c.call("apply", map[string]any{"plan_id": po.PlanID}, &res); msg != "" || res.Commit == "" {
		t.Fatalf("apply: %s %+v", msg, res)
	}
	if _, err := os.Stat(kb.Path("wiki/concepts/Backprop.md")); err != nil {
		t.Fatal("the page belongs to the knowledge base")
	}

	var st Status
	if msg := c.call("status", map[string]any{"vault": kb.Root}, &st); msg != "" {
		t.Fatal(msg)
	}
	if st.Kind != "knowledge" || st.Access != "open" || len(st.MountedBy) != 1 || st.MountedBy[0].Name != "p" || st.MountedBy[0].Access != "write" {
		t.Fatalf("knowledge base status %+v %+v", st, st.MountedBy)
	}
	st = Status{}
	if msg := c.call("status", nil, &st); msg != "" {
		t.Fatal(msg)
	}
	if len(st.Mounts) != 1 || st.Mounts[0].Name != "kb" || st.Mounts[0].Effective != "write" || st.Mounts[0].Pages != nil {
		t.Fatalf("project status %+v", st.Mounts)
	}
}

func TestReadMountRefusesWrites(t *testing.T) {
	h, cfg, p, kb := mounted(t)
	guarded := vault.AccessGuarded
	_, ke := rescan(t, cfg, p, kb)
	if err := vaults.EditIdentity(ke, vaults.Edit{Access: &guarded}, now); err != nil {
		t.Fatal(err)
	}
	pe, ke := rescan(t, cfg, p, kb)
	if err := vaults.Grant(ke, pe, vault.AccessRead, now); err != nil {
		t.Fatal(err)
	}
	linkPage(t, p, "Training", "See [[Backprop]] and [[Attention]].")
	c := connectIn(t, h, p.Root)

	writes := []map[string]any{{"path": "wiki/concepts/Backprop.md", "mode": "create", "content": kbPage}}
	if msg := c.call("plan", map[string]any{"vault": kb.Root, "kind": "save", "summary": "save Backprop", "writes": writes}, nil); !strings.Contains(msg, "read-only") {
		t.Errorf("plan through a read mount: %q", msg)
	}
	os.WriteFile(p.Path("inbox/paper.md"), []byte("# A paper\n\nThe claim.\n"), 0o644)
	if msg := c.call("capture", map[string]any{"vault": kb.Root, "paths": []string{"paper.md"}}, nil); !strings.Contains(msg, "read-only") {
		t.Errorf("capture through a read mount: %q", msg)
	}
	if msg := c.call("stub", map[string]any{"titles": []map[string]any{{"title": "Backprop", "target": "kb"}}}, nil); !strings.Contains(msg, "read-only") {
		t.Errorf("stub through a read mount: %q", msg)
	}
	if msg := c.call("mode", map[string]any{"vault": kb.Root, "set": "lyt"}, nil); !strings.Contains(msg, "read-only") {
		t.Errorf("mode set through a read mount: %q", msg)
	}
	ops, err := txn.History(kb, 1, false)
	if err != nil || len(ops) == 0 {
		t.Fatal(err)
	}
	if msg := c.call("undo", map[string]any{"vault": kb.Root, "operation_id": ops[0].ID}, nil); !strings.Contains(msg, "read-only") {
		t.Errorf("undo through a read mount: %q", msg)
	}
	// A refused mount leaves the project's own titles unwritten: every mount is checked
	// before the first operation.
	mixed := []map[string]any{{"title": "Attention"}, {"title": "Backprop", "target": "kb"}}
	if msg := c.call("stub", map[string]any{"titles": mixed}, nil); !strings.Contains(msg, "read-only") {
		t.Errorf("a mixed stub through a read mount: %q", msg)
	}
	if _, err := os.Stat(p.Path("wiki/concepts/Attention.md")); err == nil {
		t.Fatal("a refused mount leaves the project's own stub unwritten")
	}
	if _, err := os.Stat(kb.Path("wiki/concepts/Backprop.md")); err == nil {
		t.Fatal("a read mount writes nothing")
	}
}

func TestRouteAcrossMounts(t *testing.T) {
	h, cfg, p, kb := mounted(t)
	page := "---\ntitle: Backpropagation\ntype: concept\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\naliases:\n  - backprop\n---\n\n# Backpropagation\n"
	os.MkdirAll(kb.Path("wiki/concepts"), 0o755)
	if err := os.WriteFile(kb.Path("wiki/concepts/Backpropagation.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	c := connectIn(t, h, p.Root)

	var out RouteOut
	if msg := c.call("route", map[string]any{"type": "concept", "title": "backprop"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Mounts) != 1 || out.Mounts[0].Match == nil || out.Mounts[0].Match.Path != "wiki/concepts/Backpropagation.md" {
		t.Fatalf("mount match %+v", out.Mounts)
	}
	if !strings.HasSuffix(out.Mounts[0].Path, "wiki/concepts/backprop.md") {
		t.Fatalf("mount path %+v", out.Mounts[0])
	}

	out = RouteOut{}
	if msg := c.call("route", map[string]any{"type": "concept", "title": "Fresh"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Match != nil || out.Mounts[0].Match != nil {
		t.Fatalf("no match anywhere: target %+v mount %+v", out.Match, out.Mounts[0])
	}
	if out.Mounts[0].Path == "" {
		t.Fatalf("a writable mount routes a concept: %+v", out.Mounts[0])
	}

	out = RouteOut{}
	if msg := c.call("route", map[string]any{"type": "question", "title": "Q"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Mounts[0].Path != "" || out.Mounts[0].Error != "" {
		t.Fatalf("a question is not filed in a knowledge base: %+v", out.Mounts[0])
	}

	// A type the knowledge base never files reports no Match either, even when the
	// title is one an alias there would otherwise match.
	out = RouteOut{}
	if msg := c.call("route", map[string]any{"type": "question", "title": "backprop"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Mounts[0].Match != nil || out.Mounts[0].Path != "" {
		t.Fatalf("an unfiled type reports neither: %+v", out.Mounts[0])
	}

	guarded := vault.AccessGuarded
	_, ke := rescan(t, cfg, p, kb)
	if err := vaults.EditIdentity(ke, vaults.Edit{Access: &guarded}, now); err != nil {
		t.Fatal(err)
	}
	pe, ke := rescan(t, cfg, p, kb)
	if err := vaults.Grant(ke, pe, vault.AccessRead, now); err != nil {
		t.Fatal(err)
	}
	out = RouteOut{}
	if msg := c.call("route", map[string]any{"type": "concept", "title": "backprop"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Mounts[0].Path != "" || out.Mounts[0].Effective != "read" {
		t.Fatalf("a read mount reports no path: %+v", out.Mounts[0])
	}
}

func TestPlanRefusesAProjectThatDoesNotMount(t *testing.T) {
	h, cfg, _, kb := mounted(t)
	other := vaults.PathFor(cfg.VaultsDir, vault.Project, "q")
	if _, err := vault.Init(other, vault.Options{Kind: vault.Project, Name: "q"}, now); err != nil {
		t.Fatal(err)
	}
	c := connectIn(t, h, other)
	writes := []map[string]any{{"path": "wiki/concepts/Backprop.md", "mode": "create", "content": kbPage}}
	if msg := c.call("plan", map[string]any{"vault": kb.Root, "kind": "save", "summary": "save Backprop", "writes": writes}, nil); !strings.Contains(msg, "q does not mount kb") {
		t.Errorf("a project with no mount: %q", msg)
	}
	if _, err := os.Stat(kb.Path("wiki/concepts/Backprop.md")); err == nil {
		t.Fatal("nothing is written")
	}
}

func TestMountsNeedsAVaultTheAtlasKnows(t *testing.T) {
	h, _, _, _ := mounted(t)
	outside := newVault(t) // a project the vaults directory does not hold
	c := connectIn(t, h, outside.Root)
	if msg := c.call("mounts", nil, nil); !strings.Contains(msg, "the atlas does not know") {
		t.Errorf("mounts: %q", msg)
	}
	if msg := c.call("stub", map[string]any{"titles": []map[string]any{{"title": "Backprop", "target": "kb"}}}, nil); !strings.Contains(msg, "the atlas does not know") {
		t.Errorf("stub with a target: %q", msg)
	}
}

func TestKnowledgeBaseSessionRefusesIngestAndNamesMounts(t *testing.T) {
	h, _, _, kb := mounted(t)
	c := connectIn(t, h, kb.Root)

	writes := []map[string]any{{"path": "wiki/concepts/Backprop.md", "mode": "create", "content": kbPage}}
	if msg := c.call("plan", map[string]any{"kind": "save", "summary": "save Backprop", "writes": writes}, nil); !strings.Contains(msg, "mounted by: p") {
		t.Errorf("save in a knowledge base session: %q", msg)
	}
	var po PlanOut
	if msg := c.call("plan", map[string]any{"kind": "repair", "summary": "add Backprop", "writes": writes}, &po); msg != "" {
		t.Fatalf("repair in a knowledge base session: %q", msg)
	}
	var res txn.Result
	if msg := c.call("apply", map[string]any{"plan_id": po.PlanID}, &res); msg != "" || res.Commit == "" {
		t.Fatalf("apply: %s %+v", msg, res)
	}
	if msg := c.call("mounts", nil, nil); !strings.Contains(msg, "mounted by") {
		t.Errorf("mounts in a knowledge base: %q", msg)
	}
}

func TestStubIntoAMount(t *testing.T) {
	h, _, p, kb := mounted(t)
	linkPage(t, p, "Training", "See [[Backprop]] and [[Attention]].")
	c := connectIn(t, h, p.Root)

	if msg := c.call("stub", map[string]any{"titles": []map[string]any{{"title": "Backprop", "target": "none"}}}, nil); !strings.Contains(msg, "no mount named") {
		t.Errorf("unknown mount: %q", msg)
	}
	var out StubOut
	// Two spellings of one mount name make one operation.
	titles := []map[string]any{{"title": "Backprop", "target": "kb"}, {"title": "Attention", "target": "KB"}}
	if msg := c.call("stub", map[string]any{"titles": titles}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Stubs) != 2 || out.Stubs[0].Path != "kb/kb/concepts/Backprop.md" || out.Stubs[1].Path != "kb/kb/concepts/Attention.md" {
		t.Fatalf("stub into a mount %+v", out.Stubs)
	}
	if out.OperationID != "" || out.Commit != "" {
		t.Fatalf("nothing committed in the project, so no top-level operation: %+v", out.StubResult)
	}
	if len(out.Operations) != 1 || out.Operations[0].Vault != kb.Root || out.Operations[0].Commit == "" {
		t.Fatalf("operations %+v", out.Operations)
	}
	for _, title := range []string{"Backprop", "Attention"} {
		if _, err := os.Stat(kb.Path("wiki/concepts/" + title + ".md")); err != nil {
			t.Fatalf("%s belongs to the knowledge base: %v", title, err)
		}
	}
	if _, err := os.Stat(p.Path(out.Stubs[0].Path)); err != nil {
		t.Fatalf("the project reads the stub through its mount: %v", err)
	}
	var report struct {
		Summary struct {
			Wanted int `json:"wanted_pages"`
		} `json:"summary"`
		DeadLinks []any `json:"dead_links"`
	}
	if msg := c.call("lint", nil, &report); msg != "" {
		t.Fatal(msg)
	}
	if report.Summary.Wanted != 0 || len(report.DeadLinks) != 0 {
		t.Fatalf("the project's links resolve through the mount: %+v", report)
	}
}

// A mount's symlink is local state: a project cloned onto another machine has none until
// refresh runs. The server hands lint the registry's mounts instead, so the project's
// links still resolve and stub refuses to copy a knowledge base page into the project.
func TestMountsReachLintWithoutTheSymlink(t *testing.T) {
	h, _, p, kb := mounted(t)
	linkPage(t, kb, "Backprop", "The knowledge base holds this page.")
	linkPage(t, p, "Training", "See [[Backprop]].")
	if err := os.Remove(filepath.Join(p.Root, vault.KbDir, "kb")); err != nil {
		t.Fatal(err)
	}
	c := connectIn(t, h, p.Root)

	var report struct {
		Summary struct {
			Wanted int `json:"wanted_pages"`
		} `json:"summary"`
		DeadLinks []any `json:"dead_links"`
	}
	if msg := c.call("lint", nil, &report); msg != "" {
		t.Fatal(msg)
	}
	if report.Summary.Wanted != 0 || len(report.DeadLinks) != 0 {
		t.Fatalf("the link resolves through the registry's mount: %+v", report)
	}

	msg := c.call("stub", map[string]any{"vault": p.Root, "titles": []map[string]any{{"title": "Backprop"}}}, nil)
	if !strings.Contains(msg, `nothing in the wiki links to "Backprop"`) {
		t.Fatalf("stub refuses a title the knowledge base already holds: %q", msg)
	}
	if _, err := os.Stat(p.Path("wiki/concepts/Backprop.md")); !os.IsNotExist(err) {
		t.Fatalf("the project got a copy of the knowledge base's page: %v", err)
	}

	content := "---\ntitle: Notes\ntype: concept\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# Notes\n\nSee [[Backprop]].\n"
	writes := []map[string]any{{"path": "wiki/concepts/Notes.md", "mode": "create", "content": content}}
	var out PlanOut
	if msg := c.call("plan", map[string]any{"kind": "save", "summary": "save Notes", "writes": writes}, &out); msg != "" {
		t.Fatal(msg)
	}
	for _, w := range out.Warnings {
		if strings.Contains(w, "has no page yet") {
			t.Fatalf("the preview wants a page the knowledge base already holds: %v", out.Warnings)
		}
	}
}

func TestStubNamesWhatCommittedWhenALaterOperationFails(t *testing.T) {
	h, _, p, kb := mounted(t)
	linkPage(t, p, "Training", "See [[Attention]].")
	c := connectIn(t, h, p.Root)

	titles := []map[string]any{{"title": "Attention"}, {"title": "Nowhere", "target": "kb"}}
	msg := c.call("stub", map[string]any{"titles": titles}, nil)
	if !strings.Contains(msg, "nothing in the wiki links to") || !strings.Contains(msg, "already committed") || !strings.Contains(msg, p.Root) {
		t.Fatalf("the refusal names the operation that landed: %q", msg)
	}
	if _, err := os.Stat(p.Path("wiki/concepts/Attention.md")); err != nil {
		t.Fatal("the project's own stub committed before the knowledge base refused")
	}
	if _, err := os.Stat(kb.Path("wiki/concepts/Nowhere.md")); err == nil {
		t.Fatal("the knowledge base got nothing")
	}
}

func TestStubDirectlyIntoAKnowledgeBaseRecordsTheProject(t *testing.T) {
	h, _, p, kb := mounted(t)
	linkPage(t, p, "Training", "See [[Backprop]].") // only the project links Backprop
	linkPage(t, kb, "Seed", "See [[Something]].")   // the knowledge base wants Something
	c := connectIn(t, h, p.Root)

	var out StubOut
	if msg := c.call("stub", map[string]any{"vault": kb.Root}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Stubs) != 1 || out.Stubs[0].Path != "wiki/concepts/Something.md" || out.OperationID == "" {
		t.Fatalf("the knowledge base's own wanted pages %+v", out)
	}
	ops, err := txn.History(kb, 1, false)
	if err != nil || len(ops) == 0 || ops[0].Summary != "stub Something (via p)" {
		t.Fatalf("the knowledge base's log names the project: %+v %v", ops, err)
	}
	if msg := c.call("stub", map[string]any{"vault": kb.Root, "titles": []map[string]any{{"title": "Backprop"}}}, nil); !strings.Contains(msg, "nothing in the wiki links to") {
		t.Errorf("a title only the project links needs the mount as its target: %q", msg)
	}
}
