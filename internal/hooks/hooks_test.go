package hooks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
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

func env(values map[string]string) Env {
	return func(k string) string { return values[k] }
}

func TestSessionStart(t *testing.T) {
	v := newVault(t)
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+v.Path("wiki")+`"}`), &out, env(nil), true, time.Now()); err != nil {
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
	SessionStart(strings.NewReader(`{"cwd":"`+t.TempDir()+`"}`), &out, env(nil), true, time.Now())
	if out.Len() != 0 {
		t.Fatal("silent outside a vault")
	}
	out.Reset()
	SessionStart(strings.NewReader(`{"cwd":"/nowhere"}`), &out, env(map[string]string{vault.EnvVault: v.Root, "CLAUDE_ATLAS_SESSION_CONTEXT": "0"}), true, time.Now())
	if !strings.Contains(out.String(), "claude-atlas project") || strings.Contains(out.String(), "<vault-context>") {
		t.Fatalf("env vault with context off:\n%s", out.String())
	}
	os.MkdirAll(v.Path(".vault-meta"), 0o755)
	os.WriteFile(v.Path(".vault-meta/inflight.json"), []byte(`{"operation_id":"save-x","paths":[]}`), 0o644)
	out.Reset()
	SessionStart(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(nil), false, time.Now())
	if !strings.Contains(out.String(), "WARNING: operation save-x was interrupted") {
		t.Fatalf("recovery warning:\n%s", out.String())
	}
	out.Reset()
	Stop(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(nil))
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
	if err := SessionStart(strings.NewReader(`{"cwd":"`+v.Root+`"}`), &out, env(nil), false, now); err != nil {
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
	cfg := h.Default(filepath.Join(root, "Vaults"), filepath.Join(root, "Atlas"))
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
	e := env(map[string]string{home.EnvHome: h.Root})
	if err := SessionStart(strings.NewReader(`{"cwd":"`+outside+`"}`), &out, e, true, now); err != nil {
		t.Fatal(err)
	}
	text = out.String()
	for _, want := range []string{"the repository code of the project", "Open tasks: 1", "<vault-context>", "This folder is the repository code. In it, changes land as commits on the current branch. The repos tool says the same."} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in repo session:\n%s", want, text)
		}
	}
	// With a remote and no policy, the session is told to open pull requests.
	if err := vault.UpdateConfig(v.Root, "remote", now, func(c *vault.Config) error {
		c.Repos[0].Remote = "git@example.com:a/code.git"
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
	root := filepath.Join(t.TempDir(), "kb")
	if _, err := vault.Init(root, vault.Options{Kind: vault.Knowledge, Name: "ai-ml"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+root+`"}`), &out, env(nil), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"claude-atlas knowledge base: ai-ml (generic mode)", "Knowledge enters through a project", "<vault-context>", KnowledgeSkills} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	for _, absent := range []string{"Open tasks", "task-plant", "inbox/tasks"} {
		if strings.Contains(text, absent) {
			t.Errorf("a knowledge base session mentions %q:\n%s", absent, text)
		}
	}
}

func TestSessionStartNamesAV1Vault(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "wiki"), 0o755)
	os.WriteFile(filepath.Join(root, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	var out bytes.Buffer
	if err := SessionStart(strings.NewReader(`{"cwd":"`+filepath.Join(root, "wiki")+`"}`), &out, env(nil), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"v1 vault", "adopt", root} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	out.Reset()
	if err := SessionStart(strings.NewReader(`{"cwd":"`+t.TempDir()+`"}`), &out, env(nil), true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("silent without a vault:\n%s", out.String())
	}
}
