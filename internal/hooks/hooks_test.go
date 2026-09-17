package hooks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/lint"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "v")
	if _, err := vault.Init(root, vault.Options{Kind: vault.Project, Mode: vault.Generic}, time.Now()); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.Open(root)
	return v
}

// env answers CLAUDE_ATLAS_HOME with a temp path that does not exist, so a test that
// names no home reads no atlas at all instead of the developer's ~/.claude-atlas.
func env(t *testing.T, values map[string]string) Env {
	t.Helper()
	noAtlas := filepath.Join(t.TempDir(), "no-atlas")
	return func(k string) string {
		if k == home.EnvHome && values[k] == "" {
			return noAtlas
		}
		return values[k]
	}
}

func TestSessionStart(t *testing.T) {
	v := newVault(t)
	e := env(t, nil)
	if _, err := os.Stat(e(home.EnvHome)); !os.IsNotExist(err) {
		t.Fatalf("the default test home must not exist on disk: %v", err)
	}
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+v.Path("wiki")+`"}`), &out, e, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"claude-atlas project: v (generic mode)", "<vault-context>", "Active Threads", "/claude-atlas:wiki"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "type: meta") {
		t.Fatal("frontmatter should be stripped")
	}
	out.Reset()
	SessionStart(strings.NewReader(`{"cwd":"`+t.TempDir()+`"}`), &out, env(t, nil), true, time.Now())
	if out.Len() != 0 {
		t.Fatal("silent outside a vault")
	}
	out.Reset()
	SessionStart(strings.NewReader(`{"cwd":"/nowhere"}`), &out, env(t, map[string]string{vault.EnvVault: v.Root, "CLAUDE_ATLAS_SESSION_CONTEXT": "0"}), true, time.Now())
	if !strings.Contains(out.String(), "claude-atlas project") || strings.Contains(out.String(), "<vault-context>") {
		t.Fatalf("env vault with context off:\n%s", out.String())
	}
	os.MkdirAll(v.Path(".vault-meta"), 0o755)
	os.WriteFile(v.Path(".vault-meta/inflight.json"), []byte(`{"operation_id":"save-x","paths":[]}`), 0o644)
	out.Reset()
	SessionStart(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(t, nil), false, time.Now())
	if !strings.Contains(out.String(), "WARNING: operation save-x was interrupted") {
		t.Fatalf("recovery warning:\n%s", out.String())
	}
	out.Reset()
	Stop(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(t, nil))
	if !strings.Contains(out.String(), `"systemMessage"`) || !strings.Contains(out.String(), "save-x") {
		t.Fatalf("stop:\n%s", out.String())
	}
}

func TestGuard(t *testing.T) {
	v := newVault(t)
	cases := map[string]bool{
		v.Path("wiki/concepts/A.md"):            true,
		v.Path(".raw/captured/x.pdf"):           true,
		v.Path(".claude-atlas.json"):            true,
		v.Path("inbox/paper.md"):                false,
		v.Path("notes.md"):                      false,
		filepath.Join(t.TempDir(), "wiki/x.md"): false,
		v.Path("kb/ai-ml/concepts/A.md"):        true,
		v.Path("repos/code/main.go"):            false,
	}
	for path, deny := range cases {
		var out bytes.Buffer
		if err := Guard(strings.NewReader(`{"tool_name":"Write","tool_input":{"file_path":"`+path+`"}}`), &out); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(out.String(), `"deny"`); got != deny {
			t.Errorf("%s: deny=%v, got %q", path, deny, out.String())
		}
	}
	var out bytes.Buffer
	Guard(strings.NewReader(`{"tool_name":"Edit","cwd":"`+v.Root+`","tool_input":{"file_path":"wiki/hot.md"}}`), &out)
	if !strings.Contains(out.String(), "deny") {
		t.Fatal("relative paths resolve against cwd")
	}
	var kbOut bytes.Buffer
	Guard(strings.NewReader(`{"tool_name":"Write","tool_input":{"file_path":"`+v.Path("kb/ai-ml/concepts/A.md")+`"}}`), &kbOut)
	if !strings.Contains(kbOut.String(), "mounted knowledge base") {
		t.Errorf("kb reason missing: %q", kbOut.String())
	}
}

func TestSessionStartListsTasksAndFindsAVaultThroughTheAtlas(t *testing.T) {
	v := newVault(t)
	now := time.Now()
	req, _, err := txn.PlantRequest(v, tasks.Plant{Title: "Fix the dialog", Text: "It quits on Enter."}, "", now)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := txn.Prepare(v, req, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := txn.Apply(v, plan, now); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(v.Path("inbox/tasks"), 0o755)
	os.WriteFile(v.Path("inbox/tasks/idea.md"), []byte("An idea."), 0o644)
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(t, nil), false, now); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"Open tasks: 1 (active 0, blocked 0, planned 0, planted 1)", "- [planted] Fix the dialog (task-", "1 task note waits in inbox/tasks/", "task-plant"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	// A repository the registry mounts on the vault's project gets the same, through
	// discovery.
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
	outside := filepath.Join(root, "code")
	if _, _, err := vaults.CreateRepo(h, cfg, *entry, "code", outside, now); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	e := env(t, map[string]string{home.EnvHome: h.Root})
	if err := SessionStart(strings.NewReader(`{"cwd":"`+outside+`"}`), &out, e, true, now); err != nil {
		t.Fatal(err)
	}
	text = out.String()
	for _, want := range []string{"the repository code of the project", "Open tasks: 1", "<vault-context>", "This folder is the repository code. In it, changes land as commits on the current branch. The repos tool says the same."} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in repo session:\n%s", want, text)
		}
	}
	// With a remote and no policy recorded, the session is told to open pull requests.
	if err := vault.UpdateConfig(v.Root, "remote", now, func(c *vault.Config) error {
		c.Repos[0].Remote = "git@example.com:a/code.git"
		c.Repos[0].Changes = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	SessionStart(strings.NewReader(`{"cwd":"`+outside+`"}`), &out, e, false, now)
	if !strings.Contains(out.String(), "changes land as pull requests") {
		t.Fatalf("policy line:\n%s", out.String())
	}
	out.Reset()
	SessionStart(strings.NewReader(`{"cwd":"`+root+`"}`), &out, e, true, now)
	if out.Len() != 0 {
		t.Fatalf("silent outside linked folders:\n%s", out.String())
	}
	// A repository mounted inside the vault's own folder: the vault is found directly,
	// and the session is still told it sits in a repository.
	ix, err = registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	entry = ix.ByPath(v.Root)
	if entry == nil {
		t.Fatal("project not scanned")
	}
	if _, _, err := vaults.CreateRepo(h, cfg, *entry, "inside", "", now); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	SessionStart(strings.NewReader(`{"cwd":"`+v.Path("repos/inside")+`"}`), &out, e, false, now)
	if !strings.Contains(out.String(), "claude-atlas project: v") || !strings.Contains(out.String(), "This folder is the repository inside. In it, changes land as commits") {
		t.Fatalf("repo inside the vault:\n%s", out.String())
	}
}

func TestSessionStartInAKnowledgeBase(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	now := time.Now()
	root := filepath.Join(t.TempDir(), "kb")
	if _, err := vault.Init(root, vault.Options{Kind: vault.Knowledge, Name: "ai-ml"}, now); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+root+`"}`), &out, env(t, nil), true, now); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"claude-atlas knowledge base: ai-ml (generic mode)", "Knowledge enters through a project", "<vault-context>", KnowledgeSkills} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, absent := range []string{"Open tasks", "task-plant", "inbox/tasks", "Stubs:", "Wanted:"} {
		if strings.Contains(text, absent) {
			t.Errorf("a knowledge base session mentions %q:\n%s", absent, text)
		}
	}

	// A knowledge base session prints the counts line too, right after the first line.
	os.MkdirAll(filepath.Join(root, "wiki", "concepts"), 0o755)
	training := vault.Skeleton("concept", "Training", now)
	training = strings.Replace(training, "## Related\n\n", "## Related\n\n[[Optimizer]]\n\n", 1)
	if err := os.WriteFile(filepath.Join(root, "wiki", "concepts", "Training.md"), []byte(training), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+root+`"}`), &out, env(t, nil), false, now); err != nil {
		t.Fatal(err)
	}
	text = out.String()
	want := "Wanted: 1 linked page does not exist yet (Optimizer). Fill or stub them with the wiki-lint skill."
	if !strings.Contains(text, want) {
		t.Errorf("missing %q in:\n%s", want, text)
	}
	if i, j := strings.Index(text, want), strings.Index(text, "Knowledge enters through a project"); i < 0 || j < 0 || i > j {
		t.Errorf("counts line should come right after the first line, before the skills line:\n%s", text)
	}
}

func TestSessionStartListsMountsAndMountedBy(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	now := time.Now()
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default(filepath.Join(root, "Vaults"))
	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	projectRoot := vaults.PathFor(cfg.VaultsDir, vault.Project, "cs566")
	if _, err := vault.Init(projectRoot, vault.Options{Kind: vault.Project, Name: "cs566"}, now); err != nil {
		t.Fatal(err)
	}
	kbRoot := vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "ai-ml")
	if _, err := vault.Init(kbRoot, vault.Options{Kind: vault.Knowledge, Name: "ai-ml"}, now); err != nil {
		t.Fatal(err)
	}

	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	kb := ix.ByPath(kbRoot)
	if kb == nil {
		t.Fatal("knowledge base not scanned")
	}
	scope := "Machine learning: models, training, evaluation, deployment, agents"
	if _, err := vaults.EditIdentity(home.Home{}, &home.Config{}, *kb, vaults.Edit{Scope: &scope}, now); err != nil {
		t.Fatal(err)
	}

	ix, err = registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	project := ix.ByPath(projectRoot)
	kb = ix.ByPath(kbRoot)
	if project == nil || kb == nil {
		t.Fatal("fixture not scanned")
	}
	if _, err := vaults.Mount(*project, *kb, "", "", now); err != nil {
		t.Fatal(err)
	}

	e := env(t, map[string]string{home.EnvHome: h.Root})
	report, err := lint.Run(kbRoot, lint.Options{AsOf: now})
	if err != nil {
		t.Fatal(err)
	}
	wantLine := fmt.Sprintf("Knowledge: ai-ml (write) · %s · %d pages · kb/ai-ml", scope, report.Summary.PagesScanned)

	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+projectRoot+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{wantLine, SearchSentence} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in project session:\n%s", want, text)
		}
	}

	// A project page linking a page that exists only in the mounted knowledge base is not
	// wanted: the mounts reach lint.
	if err := os.MkdirAll(filepath.Join(kbRoot, "wiki", "concepts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kbRoot, "wiki", "concepts", "Gradient Descent.md"), []byte(vault.Skeleton("concept", "Gradient Descent", now)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectRoot, "wiki", "concepts"), 0o755); err != nil {
		t.Fatal(err)
	}
	uses := vault.Skeleton("concept", "Uses", now)
	uses = strings.Replace(uses, "## Related\n\n", "## Related\n\n[[Gradient Descent]]\n\n", 1)
	if err := os.WriteFile(filepath.Join(projectRoot, "wiki", "concepts", "Uses.md"), []byte(uses), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+projectRoot+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Wanted:") {
		t.Errorf("a page in the mounted knowledge base counted as wanted:\n%s", out.String())
	}

	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+kbRoot+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "mounted by cs566 (write)") {
		t.Errorf("missing mounted-by in kb session:\n%s", out.String())
	}

	// Guarding the knowledge base drops the project's mount to read, since cs566 has no
	// grant, and the knowledge base's own line names the new access.
	guarded := vault.AccessGuarded
	ix, err = registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	kb = ix.ByPath(kbRoot)
	if _, err := vaults.EditIdentity(home.Home{}, &home.Config{}, *kb, vaults.Edit{Access: &guarded}, now); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+projectRoot+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Knowledge: ai-ml (read)") {
		t.Errorf("missing read access after guard:\n%s", out.String())
	}
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+kbRoot+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "guarded") {
		t.Errorf("missing guarded in kb session:\n%s", out.String())
	}

	// A symlink that leads somewhere else is not the same problem as one that is gone.
	link := filepath.Join(projectRoot, vault.KbDir, "ai-ml")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+projectRoot+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "symlink points elsewhere; run claude-atlas refresh") {
		t.Errorf("wrong-target symlink warning:\n%s", out.String())
	}

	// A missing symlink warns the project session to refresh.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+projectRoot+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "symlink missing; run claude-atlas refresh") {
		t.Errorf("missing symlink warning:\n%s", out.String())
	}
}

// A project that mounts a cluster gets a Knowledge: line per member, each naming the
// cluster it came through.
func TestSessionStartListsAClustersMembers(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	now := time.Now()
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default(filepath.Join(root, "Vaults"))
	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	projectRoot := vaults.PathFor(cfg.VaultsDir, vault.Project, "cs566")
	if _, err := vault.Init(projectRoot, vault.Options{Kind: vault.Project, Name: "cs566"}, now); err != nil {
		t.Fatal(err)
	}
	clusterRoot := vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "papers")
	if _, err := vault.Init(clusterRoot, vault.Options{Kind: vault.Knowledge, Name: "papers"}, now); err != nil {
		t.Fatal(err)
	}
	memberRoot := vaults.PathFor(cfg.VaultsDir, vault.Knowledge, "ai-ml")
	if _, err := vault.Init(memberRoot, vault.Options{Kind: vault.Knowledge, Name: "ai-ml"}, now); err != nil {
		t.Fatal(err)
	}

	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cluster := ix.ByPath(clusterRoot)
	member := ix.ByPath(memberRoot)
	if cluster == nil || member == nil {
		t.Fatal("fixture not scanned")
	}
	if err := vaults.AddMember(*cluster, *member, now); err != nil {
		t.Fatal(err)
	}

	ix, err = registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	project := ix.ByPath(projectRoot)
	cluster = ix.ByPath(clusterRoot)
	if project == nil || cluster == nil {
		t.Fatal("fixture not scanned")
	}
	if _, err := vaults.Mount(*project, *cluster, vault.AccessWrite, "", now); err != nil {
		t.Fatal(err)
	}

	e := env(t, map[string]string{home.EnvHome: h.Root})
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+projectRoot+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "Knowledge: ai-ml (write, through papers)") {
		t.Errorf("missing a member's line:\n%s", text)
	}
	if !strings.Contains(text, "Knowledge: papers (write)") {
		t.Errorf("missing the cluster's own line:\n%s", text)
	}
}

func TestSessionStartCountsStubsAndWantedPages(t *testing.T) {
	v := newVault(t)
	now := time.Now()

	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(t, nil), false, now); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Stubs:") || strings.Contains(out.String(), "Wanted:") {
		t.Fatalf("a fresh project has nothing to count:\n%s", out.String())
	}

	os.MkdirAll(v.Path("wiki/concepts"), 0o755)
	training := vault.Skeleton("concept", "Training", now)
	training = strings.Replace(training, "## Related\n\n", "## Related\n\n[[Optimizer]], [[Backpropagation]]\n\n", 1)
	if err := os.WriteFile(v.Path("wiki/concepts/Training.md"), []byte(training), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(v.Path("wiki/concepts/Backpropagation.md"), []byte(vault.Skeleton("concept", "Backpropagation", now)), 0o644); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(t, nil), false, now); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	want := "Stubs: 1 page to fill (Backpropagation). Wanted: 1 linked page does not exist yet (Optimizer). Fill or stub them with the wiki-lint skill."
	if !strings.Contains(text, want) {
		t.Errorf("missing %q in:\n%s", want, text)
	}

	// Four wanted pages: three names, then an ellipsis.
	os.MkdirAll(v.Path("wiki/concepts"), 0o755)
	four := vault.Skeleton("concept", "Four Wants", now)
	four = strings.Replace(four, "## Related\n\n", "## Related\n\n[[Alpha]], [[Bravo]], [[Charlie]], [[Delta]]\n\n", 1)
	if err := os.WriteFile(v.Path("wiki/concepts/Four Wants.md"), []byte(four), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(t, nil), false, now); err != nil {
		t.Fatal(err)
	}
	text = out.String()
	if !strings.Contains(text, "Wanted: 5 linked pages do not exist yet (Alpha, Bravo, Charlie, …). Fill or stub them with the wiki-lint skill.") {
		t.Errorf("missing capped wanted list in:\n%s", text)
	}
}

func TestSessionStartNamesAV1Vault(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "wiki"), 0o755)
	os.WriteFile(filepath.Join(root, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+filepath.Join(root, "wiki")+`"}`), &out, env(t, nil), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"v1 vault", "adopt", root} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+t.TempDir()+`"}`), &out, env(t, nil), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("silent without a vault:\n%s", out.String())
	}
}

// TestSessionStartInsideAHostRepository covers a project that lives at REPO/atlas: a
// session in the code finds the vault through the repository the project lives in.
func TestSessionStartInsideAHostRepository(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	now := time.Now()
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default(filepath.Join(root, "Vaults"))
	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	code := filepath.Join(root, "code")
	src := filepath.Join(code, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := gitx.Repo{Dir: code}
	if err := repo.Init(); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	res, err := vault.InitIn(code, vault.Options{Kind: vault.Project, Name: "Notes"}, now)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddVault(res.Root)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	e := env(t, map[string]string{home.EnvHome: h.Root})
	if err := SessionStart(strings.NewReader(`{"cwd":"`+src+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"this folder is the repository code of the project Notes", res.Root, SearchSentence} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}

	// In the vault itself the nearest identity file wins, and the host still matches.
	// The folder there is the vault, not the repository, so the policy line says so.
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+res.Root+`"}`), &out, e, false, now); err != nil {
		t.Fatal(err)
	}
	text = out.String()
	if !strings.Contains(text, "This project lives in the repository code. In it, changes land as commits on the current branch.") {
		t.Errorf("the policy line in the vault:\n%s", text)
	}
	if strings.Contains(text, "This folder is the repository") {
		t.Errorf("the vault folder is not the repository:\n%s", text)
	}
}
