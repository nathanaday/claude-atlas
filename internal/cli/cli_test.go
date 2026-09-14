package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/tree"
)

type harness struct {
	t    *testing.T
	home string
	out  bytes.Buffer
	err  bytes.Buffer
}

func (h *harness) run(args ...string) int {
	h.out.Reset()
	h.err.Reset()
	c := console.NewWith(true, strings.NewReader(""), &h.out, false)
	return run(append([]string{"--home", h.home}, args...), strings.NewReader(""), &h.out, &h.err, c)
}

func setup(t *testing.T) (*harness, string) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := &harness{t: t, home: filepath.Join(root, "home")}
	vaults := filepath.Join(root, "Vaults")
	code := h.run("setup", "--no-plugin", "--vaults-dir", vaults, "--atlas-vault", filepath.Join(root, "Atlas"), "--first-vault", "welcome")
	if code != 0 {
		t.Fatalf("setup exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	return h, vaults
}

func TestSetupCreatesHomeAtlasAndFirstVault(t *testing.T) {
	h, vaults := setup(t)
	if !strings.Contains(h.out.String(), "Setup complete.") {
		t.Fatalf("output:\n%s", h.out.String())
	}
	for _, path := range []string{
		filepath.Join(vaults, "welcome", ".claude-atlas.json"),
		filepath.Join(vaults, "welcome", ".git", "HEAD"),
		filepath.Join(filepath.Dir(h.home), "Atlas", ".obsidian", "app.json"),
		filepath.Join(filepath.Dir(h.home), "Atlas", "tree", "welcome.md"),
		filepath.Join(h.home, "state", "welcome.json"),
		filepath.Join(h.home, "config.json"),
		filepath.Join(filepath.Dir(h.home), "Atlas", "About.md"),
		filepath.Join(filepath.Dir(h.home), "Atlas", "Reference.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s", path)
		}
	}
	page, _ := os.ReadFile(filepath.Join(filepath.Dir(h.home), "Atlas", "Overview.md"))
	if !strings.Contains(string(page), "[[tree/welcome\\|welcome]]") {
		t.Fatalf("atlas page:\n%s", page)
	}
	if code := h.run("setup", "--no-plugin"); code != 0 || !strings.Contains(h.out.String(), "keep       1 registered") {
		t.Fatalf("rerun exit %d:\n%s", code, h.out.String())
	}
}

func TestVaultCommands(t *testing.T) {
	h, vaults := setup(t)
	if code := h.run("new-vault", "triage", "--category", "work", "--purpose", "Sort sensors."); code != 0 {
		t.Fatalf("new-vault exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if !strings.Contains(h.out.String(), "tree/work/triage.md") {
		t.Fatalf("output:\n%s", h.out.String())
	}
	if _, err := os.Stat(filepath.Join(vaults, "triage", "wiki", "hot.md")); err != nil {
		t.Fatal("vault not created")
	}
	if code := h.run("new-vault", "--from", filepath.Join(vaults, "triage")); code != 0 || !strings.Contains(h.out.String(), "already tree/work/triage.md") {
		t.Fatalf("exit %d out %s err %s", code, h.out.String(), h.err.String())
	}
	if code := h.run("new-vault", "--from", t.TempDir(), "--name", "Plain"); code != 1 || !strings.Contains(h.err.String(), "is not a vault") {
		t.Fatalf("exit %d err %s", code, h.err.String())
	}
	legacy := filepath.Join(t.TempDir(), "old")
	os.MkdirAll(filepath.Join(legacy, "wiki"), 0o755)
	os.WriteFile(filepath.Join(legacy, ".claude-obsidian.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(legacy, "wiki", "index.md"), []byte("---\ntitle: I\n---\n"), 0o644)
	if code := h.run("adopt", legacy, "--category", "archive"); code != 0 || !strings.Contains(h.out.String(), "adopted") || !strings.Contains(h.out.String(), "tree/archive/old.md") {
		t.Fatalf("adopt exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(legacy, ".claude-atlas.json")); err != nil {
		t.Fatal("adopt should add the identity file")
	}
	if code := h.run("adopt", legacy); code != 0 || !strings.Contains(h.out.String(), "already") {
		t.Fatalf("second adopt exit %d\n%s", code, h.out.String())
	}
	if code := h.run("list"); code != 0 {
		t.Fatal("list failed")
	}
	for _, want := range []string{"work/triage", "welcome"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("list missing %q:\n%s", want, h.out.String())
		}
	}
	if code := h.run("doctor"); !strings.Contains(h.out.String(), "3 registered") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("lint", "work/triage"); code != 0 || !strings.Contains(h.out.String(), "# Wiki lint") {
		t.Fatalf("lint exit %d:\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("history", filepath.Join(vaults, "triage")); code != 0 || !strings.Contains(h.out.String(), "setup") {
		t.Fatalf("history exit %d:\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("mode", "work/triage"); code != 0 || strings.TrimSpace(h.out.String()) != "generic" {
		t.Fatalf("mode exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("mode", "work/triage", "lyt"); code != 0 || !strings.Contains(h.out.String(), "generic → lyt") {
		t.Fatalf("mode set exit %d:\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("recover", "work/triage"); code != 0 || !strings.Contains(h.out.String(), "nothing was interrupted") {
		t.Fatalf("recover exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("refresh"); code != 0 || !strings.Contains(h.out.String(), "work/triage") {
		t.Fatalf("refresh exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("info"); code != 0 {
		t.Fatalf("info exit %d:\n%s", code, h.out.String())
	}
	for _, want := range []string{"overview", "Overview.md", "plugin", "3 registered", "work/triage"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("info missing %q:\n%s", want, h.out.String())
		}
	}
}

func TestLinkCommands(t *testing.T) {
	h, vaults := setup(t)
	docs := filepath.Join(vaults, "docs")
	os.MkdirAll(docs, 0o755)
	os.WriteFile(filepath.Join(docs, "a.pdf"), []byte("x"), 0o644)
	atlas := filepath.Join(filepath.Dir(h.home), "Atlas")
	// A plain folder is not a repository; piped, the command says so and stops.
	if code := h.run("link", "welcome", docs); code != 1 || !strings.Contains(h.err.String(), "not a git repository") {
		t.Fatalf("plain folder: %d %s", code, h.err.String())
	}
	if code := h.run("link", "welcome", docs, "--init"); code != 0 || !strings.Contains(h.out.String(), "docs (") || !strings.Contains(h.out.String(), "repos/docs.md (new)") {
		t.Fatalf("link --init exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(docs, ".git")); err != nil {
		t.Fatal("--init should make the folder a repository")
	}
	if _, err := os.Stat(filepath.Join(atlas, "repos", "docs.md")); err != nil {
		t.Fatal("link should create the page")
	}
	page, _ := os.ReadFile(filepath.Join(atlas, "tree", "welcome.md"))
	if !strings.Contains(string(page), `- "[[repos/docs|docs]]"`) {
		t.Fatalf("project page should hold a wikilink:\n%s", page)
	}
	if code := h.run("link", "welcome", docs); code != 1 || !strings.Contains(h.err.String(), "already linked") {
		t.Fatalf("duplicate: %d %s", code, h.err.String())
	}
	if code := h.run("links", "welcome"); code != 0 || !strings.Contains(h.out.String(), "repo") || !strings.Contains(h.out.String(), "main") {
		t.Fatalf("links exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("links"); code != 0 || !strings.Contains(h.out.String(), "docs") || !strings.Contains(h.out.String(), "· welcome") {
		t.Fatalf("all links exit %d:\n%s", code, h.out.String())
	}
	overview, _ := os.ReadFile(filepath.Join(atlas, "Overview.md"))
	if !strings.Contains(string(overview), "## Repositories") || !strings.Contains(string(overview), "[[repos/docs\\|docs]]") {
		t.Fatalf("overview:\n%s", overview)
	}
	if code := h.run("unlink", "welcome", "docs"); code != 0 {
		t.Fatalf("unlink by name exit %d %s", code, h.err.String())
	}
	if code := h.run("links", "welcome"); code != 0 || !strings.Contains(h.out.String(), "no repositories") {
		t.Fatalf("after unlink:\n%s", h.out.String())
	}
	if code := h.run("links"); code != 0 || !strings.Contains(h.out.String(), "no project") {
		t.Fatalf("an unused page shows as such:\n%s", h.out.String())
	}
	if code := h.run("link", "welcome", docs, "--kind", "bogus"); code != 2 {
		t.Fatalf("unknown flag exit %d", code)
	}
	if code := h.run("link", "welcome", "docs"); code != 0 {
		t.Fatalf("link by page name exit %d %s", code, h.err.String())
	}
	// A new repository beside the wiki, ignored by the vault's own git.
	if code := h.run("new-repo", "welcome", "paper"); code != 0 || !strings.Contains(h.out.String(), "own git history") || !strings.Contains(h.out.String(), "repos/paper.md") {
		t.Fatalf("new-repo exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	paper := filepath.Join(vaults, "welcome", "paper")
	if _, err := os.Stat(filepath.Join(paper, ".git")); err != nil {
		t.Fatal("paper should be a repository")
	}
	ignore, _ := os.ReadFile(filepath.Join(vaults, "welcome", ".gitignore"))
	if !strings.Contains(string(ignore), "/paper/") {
		t.Fatalf("vault .gitignore:\n%s", ignore)
	}
	elsewhere := filepath.Join(filepath.Dir(vaults), "slides")
	if code := h.run("new-repo", "welcome", "slides", "--at", elsewhere); code != 0 {
		t.Fatalf("new-repo --at exit %d %s", code, h.err.String())
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "README.md")); err != nil {
		t.Fatal("slides should exist with a README")
	}
}

func TestEditLinkCommand(t *testing.T) {
	h, vaults := setup(t)
	docs := filepath.Join(vaults, "docs")
	os.MkdirAll(docs, 0o755)
	if code := h.run("link", "welcome", docs, "--init"); code != 0 {
		t.Fatalf("link exit %d %s", code, h.err.String())
	}
	if code := h.run("edit-link", "docs"); code != 2 {
		t.Fatalf("no flags exit %d", code)
	}
	if code := h.run("edit-link", "docs", "--name", "Lecture notes"); code != 0 || !strings.Contains(h.out.String(), "repos/docs → repos/Lecture notes (repo,") {
		t.Fatalf("edit-link exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	atlas := filepath.Join(filepath.Dir(h.home), "Atlas")
	page, _ := os.ReadFile(filepath.Join(atlas, "tree", "welcome.md"))
	if !strings.Contains(string(page), `- "[[repos/Lecture notes|Lecture notes]]"`) || !strings.Contains(string(page), "materials: []") {
		t.Fatalf("project page:\n%s", page)
	}
	if code := h.run("edit-link", "nope", "--name", "x"); code != 1 || !strings.Contains(h.err.String(), "no page named") {
		t.Fatalf("unknown page: %d %s", code, h.err.String())
	}
}

func TestRelateCommands(t *testing.T) {
	h, _ := setup(t)
	if code := h.run("new-vault", "triage", "--category", "work"); code != 0 {
		t.Fatalf("new-vault exit %d %s", code, h.err.String())
	}
	if code := h.run("relate", "welcome", "triage"); code != 0 || !strings.Contains(h.out.String(), "welcome ↔ triage") {
		t.Fatalf("relate exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("relate", "triage", "welcome"); code != 1 || !strings.Contains(h.err.String(), "already related") {
		t.Fatalf("relate twice: %d %s", code, h.err.String())
	}
	if code := h.run("show", "triage"); code != 0 || !strings.Contains(h.out.String(), "Related from     welcome") {
		t.Fatalf("show triage:\n%s", h.out.String())
	}
	atlas := filepath.Join(filepath.Dir(h.home), "Atlas")
	overview, _ := os.ReadFile(filepath.Join(atlas, "Overview.md"))
	if !strings.Contains(string(overview), "| Related | [[tree/work/triage\\|triage]] |") || !strings.Contains(string(overview), "| Related | [[tree/welcome\\|welcome]] |") {
		t.Fatalf("overview should show the relation on both projects:\n%s", overview)
	}
	if code := h.run("unrelate", "triage", "welcome"); code != 0 {
		t.Fatalf("unrelate exit %d %s", code, h.err.String())
	}
	if code := h.run("show", "triage"); code != 0 || strings.Contains(h.out.String(), "Related") {
		t.Fatalf("still related:\n%s", h.out.String())
	}
	if code := h.run("relate", "welcome", "welcome"); code != 1 {
		t.Fatalf("self relate exit %d", code)
	}
}

func TestRefreshWritesTheGraphPages(t *testing.T) {
	h, _ := setup(t)
	if code := h.run("new-vault", "triage", "--category", "work/field"); code != 0 {
		t.Fatalf("new-vault exit %d %s", code, h.err.String())
	}
	atlas := filepath.Join(filepath.Dir(h.home), "Atlas")
	for _, path := range []string{"Tree.md", "categories/work.md", "categories/work/field.md", ".obsidian/graph.json"} {
		if _, err := os.Stat(filepath.Join(atlas, path)); err != nil {
			t.Errorf("missing %s", path)
		}
	}
	work, _ := os.ReadFile(filepath.Join(atlas, "categories", "work.md"))
	if !strings.Contains(string(work), "Part of [[Tree]]") || !strings.Contains(string(work), "[[categories/work/field|field]] · 1 project") {
		t.Fatalf("work.md:\n%s", work)
	}
	root, _ := os.ReadFile(filepath.Join(atlas, "Tree.md"))
	if !strings.Contains(string(root), "[[tree/welcome|welcome]]") || !strings.Contains(string(root), "[[categories/work|work]] · 1 project") {
		t.Fatalf("Tree.md:\n%s", root)
	}
	if code := h.run("edit", "triage", "--category", ""); code != 0 {
		t.Fatalf("edit exit %d %s", code, h.err.String())
	}
	if _, err := os.Stat(filepath.Join(atlas, "categories", "work")); err == nil {
		t.Fatal("an empty category should be gone after refresh")
	}
}

func TestResolveVault(t *testing.T) {
	h, vaults := setup(t)
	cfg, _ := home.Home{Root: h.home}.Load()
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	if got, label, err := resolveVault(cfg, projects, ""); err != nil || got != cfg.AtlasVault || label != "the atlas" {
		t.Fatalf("atlas: %s %s %v", got, label, err)
	}
	if got, _, err := resolveVault(cfg, projects, "welcome"); err != nil || got != filepath.Join(vaults, "welcome") {
		t.Fatalf("project: %s %v", got, err)
	}
	if got, _, err := resolveVault(cfg, projects, vaults); err != nil || got != vaults {
		t.Fatalf("path: %s %v", got, err)
	}
	if _, _, err := resolveVault(cfg, projects, "nope"); err == nil {
		t.Fatal("unknown name should fail")
	}
}

func TestCommandsNeedSetupFirst(t *testing.T) {
	h := &harness{t: t, home: filepath.Join(t.TempDir(), "none")}
	if code := h.run("refresh"); code != 1 || !strings.Contains(h.err.String(), "claude-atlas setup") {
		t.Fatalf("exit %d err %s", code, h.err.String())
	}
	if code := h.run("info"); code != 0 || !strings.Contains(h.out.String(), "not set up") {
		t.Fatalf("info before setup: %d %s", code, h.out.String())
	}
	if code := h.run("new-vault", "a", "--from", "/b"); code != 2 {
		t.Fatalf("name and --from together should be a usage error, got %d", code)
	}
	if code := h.run("bogus"); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
	if code := h.run("version"); code != 0 || strings.TrimSpace(h.out.String()) != Version {
		t.Fatalf("version: %q", h.out.String())
	}
}

func TestShowEditRemove(t *testing.T) {
	h, vaults := setup(t)
	if code := h.run("new-vault", "triage", "--category", "work", "--purpose", "Sort sensors."); code != 0 {
		t.Fatalf("new-vault exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("edit", "work/triage"); code != 2 {
		t.Fatalf("edit without flags should be a usage error, got %d", code)
	}
	code := h.run("edit", "work/triage", "--priority", "high", "--state", "blocked", "--blocked-on", "hardware", "--review-after", "2026-10-01", "--done", "Ships.", "--category", "ops")
	if code != 0 || !strings.Contains(h.out.String(), "edited") {
		t.Fatalf("edit exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("edit", "ops/triage", "--review-after", "soon"); code != 1 || !strings.Contains(h.err.String(), "date like") {
		t.Fatalf("bad date exit %d err %s", code, h.err.String())
	}
	if code := h.run("show", "ops/triage"); code != 0 {
		t.Fatalf("show exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	for _, want := range []string{"tree/ops/triage.md", "high", "blocked", "hardware", "2026-10-01", "Ships.", "Sort sensors.", "Vault check", "Pages"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("show missing %q:\n%s", want, h.out.String())
		}
	}
	if code := h.run("edit", "ops/triage", "--purpose", "", "--blocked-on", ""); code != 0 {
		t.Fatalf("clear exit %d %s", code, h.err.String())
	}
	h.run("show", "ops/triage")
	if strings.Contains(h.out.String(), "Sort sensors.") || strings.Contains(h.out.String(), "hardware") {
		t.Fatalf("clearing failed:\n%s", h.out.String())
	}
	if code := h.run("remove", "ops/triage"); code != 0 || !strings.Contains(h.out.String(), "removed") {
		t.Fatalf("remove exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(vaults, "triage", ".claude-atlas.json")); err != nil {
		t.Fatal("remove must leave the vault on disk")
	}
	if code := h.run("show", "ops/triage"); code != 1 {
		t.Fatal("removed project should be gone")
	}
}

func TestIngestStagesNewFilesAndRemembersTheFolder(t *testing.T) {
	h, vaults := setup(t)
	src := filepath.Join(filepath.Dir(vaults), "Papers")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.md"), []byte("aaa"), 0o644)
	if code := h.run("ingest", "welcome"); code != 1 || !strings.Contains(h.err.String(), "has not ingested from a folder yet") {
		t.Fatalf("no sources: exit %d err %s", code, h.err.String())
	}
	if code := h.run("ingest", "welcome", src, "--dry-run"); code != 0 || !strings.Contains(h.out.String(), "new        Papers/a.md") {
		t.Fatalf("dry run exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(vaults, "welcome", "inbox", "Papers", "a.md")); err == nil {
		t.Fatal("dry run must not stage")
	}
	if code := h.run("ingest", "welcome", src, "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "staged") || !strings.Contains(h.out.String(), "remembered") || !strings.Contains(h.out.String(), "wiki-ingest") {
		t.Fatalf("ingest exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if data, _ := os.ReadFile(filepath.Join(vaults, "welcome", "inbox", "Papers", "a.md")); string(data) != "aaa" {
		t.Fatal("file not staged")
	}
	if code := h.run("links", "welcome"); code != 0 || !strings.Contains(h.out.String(), "no repositories") {
		t.Fatalf("ingesting from a folder does not link it:\n%s", h.out.String())
	}
	// With no path, the remembered folder is the source; nothing is new, but the staged file still waits.
	if code := h.run("ingest", "welcome", "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "nothing new to stage; 1 file already waiting") || !strings.Contains(h.out.String(), "wiki-ingest") {
		t.Fatalf("second ingest exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	os.WriteFile(filepath.Join(src, "b.md"), []byte("bbb"), 0o644)
	if code := h.run("ingest", "welcome", "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "new        Papers/b.md") || strings.Contains(h.out.String(), "new        Papers/a.md") {
		t.Fatalf("third ingest exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
}

func TestTaskCommands(t *testing.T) {
	h, vaults := setup(t)
	if code := h.run("tasks"); code != 0 || !strings.Contains(h.out.String(), "no open tasks in any project") {
		t.Fatalf("tasks exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("plant", "welcome", "Fix", "the", "dialog", "--priority", "high"); code != 0 || !strings.Contains(h.out.String(), "planted") || !strings.Contains(h.out.String(), "wiki/tasks/Fix the dialog.md") {
		t.Fatalf("plant exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	welcome := filepath.Join(vaults, "welcome")
	if _, err := os.Stat(filepath.Join(welcome, "wiki", "tasks", "Fix the dialog.md")); err != nil {
		t.Fatal("page missing")
	}
	if code := h.run("tasks", "welcome"); code != 0 || !strings.Contains(h.out.String(), "planted   high     Fix the dialog") {
		t.Fatalf("tasks welcome exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("tasks"); code != 0 || !strings.Contains(h.out.String(), "welcome\n") || !strings.Contains(h.out.String(), "Fix the dialog") {
		t.Fatalf("all tasks:\n%s", h.out.String())
	}
	if code := h.run("plant", "welcome"); code != 2 {
		t.Fatalf("plant without text exit %d", code)
	}
	if code := h.run("plant", "welcome", "x", "--priority", "urgent"); code != 1 {
		t.Fatalf("bad priority exit %d %s", code, h.err.String())
	}
	// An older vault gains the task files and the snippet through upgrade; an appearance
	// file it already has keeps its settings and gains the snippet.
	os.RemoveAll(filepath.Join(welcome, "ideas"))
	os.Remove(filepath.Join(welcome, "wiki", "tasks", "index.md"))
	os.Remove(filepath.Join(welcome, ".obsidian", "snippets", "claude-atlas.css"))
	os.WriteFile(filepath.Join(welcome, ".obsidian", "appearance.json"), []byte(`{"baseFontSize": 15, "enabledCssSnippets": ["vault-colors"]}`), 0o644)
	if code := h.run("upgrade", "welcome"); code != 0 || !strings.Contains(h.out.String(), "added .obsidian/appearance.json, .obsidian/snippets/claude-atlas.css, ideas/.gitkeep, wiki/tasks/index.md") || !strings.Contains(h.out.String(), "reload Obsidian") {
		t.Fatalf("upgrade exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	appearance, _ := os.ReadFile(filepath.Join(welcome, ".obsidian", "appearance.json"))
	if !strings.Contains(string(appearance), `"baseFontSize": 15`) || !strings.Contains(string(appearance), `"vault-colors"`) || !strings.Contains(string(appearance), `"claude-atlas"`) {
		t.Fatalf("appearance should keep its settings and enable the snippet:\n%s", appearance)
	}
	if code := h.run("upgrade", "--all"); code != 0 || !strings.Contains(h.out.String(), "current") {
		t.Fatalf("upgrade --all exit %d\n%s", code, h.out.String())
	}
}

func TestBareCommandOpensTheTreeOrExplains(t *testing.T) {
	// Piped: the usage, as before.
	var out, errOut bytes.Buffer
	c := console.NewWith(false, strings.NewReader(""), &out, false)
	if code := run([]string{"--home", filepath.Join(t.TempDir(), "none")}, strings.NewReader(""), &out, &errOut, c); code != 0 || !strings.Contains(out.String(), "Usage:") || strings.Contains(out.String(), "No atlas yet") {
		t.Fatalf("piped: %d\n%s", code, out.String())
	}
	// A terminal with no atlas: the usage and the missing step.
	out.Reset()
	c = console.NewWith(false, strings.NewReader(""), &out, true)
	if code := run([]string{"--home", filepath.Join(t.TempDir(), "none")}, strings.NewReader(""), &out, &errOut, c); code != 0 || !strings.Contains(out.String(), "claude-atlas                       open the atlas") || !strings.Contains(out.String(), "No atlas yet; run `claude-atlas setup`") {
		t.Fatalf("no atlas: %d\n%s", code, out.String())
	}
	// help still prints the usage whatever the terminal.
	out.Reset()
	if code := run([]string{"help"}, strings.NewReader(""), &out, &errOut, c); code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("help: %d", code)
	}
}

func TestConfigNewDays(t *testing.T) {
	h, _ := setup(t)
	if code := h.run("config"); code != 0 || !strings.Contains(h.out.String(), "new-days           7") {
		t.Fatalf("config exit %d\n%s", code, h.out.String())
	}
	if code := h.run("config", "new-days", "1"); code != 0 || !strings.Contains(h.out.String(), "refreshed") {
		t.Fatalf("set exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	cfg, err := home.Home{Root: h.home}.Load()
	if err != nil || cfg.NewDays() != 1 {
		t.Fatalf("saved %v %+v", err, cfg.Heat)
	}
	about, _ := os.ReadFile(filepath.Join(filepath.Dir(h.home), "Atlas", "About.md"))
	if !strings.Contains(string(about), "within the last 1 days") {
		t.Fatal("the About page follows the setting")
	}
	for _, bad := range [][]string{{"config", "new-days", "-1"}, {"config", "new-days", "soon"}, {"config", "hot-days", "3"}, {"config", "new-days"}} {
		if code := h.run(bad...); code != 2 {
			t.Fatalf("%v exit %d", bad, code)
		}
	}
	if code := h.run("config", "new-days", "0"); code != 0 {
		t.Fatalf("zero exit %d %s", code, h.err.String())
	}
	if code := h.run("list"); code != 0 || strings.Contains(h.out.String(), "new  ") {
		t.Fatalf("with 0, a fresh vault is not new:\n%s", h.out.String())
	}
}
