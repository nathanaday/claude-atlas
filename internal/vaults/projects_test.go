package vaults

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

func TestInitProjectRegistersAndLinks(t *testing.T) {
	cfg, h, kb, _ := fixtureEntries(t)
	work := filepath.Join(t.TempDir(), "firmware")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InitProject(h, cfg, work, project.Options{}, "nope", true, identityNow); err == nil {
		t.Fatal("an unknown knowledge base refuses the init")
	}
	if project.IsProject(work) || cfg.HasProject(work) || exists(filepath.Join(work, ".git")) {
		t.Fatal("a refused init writes nothing")
	}
	made, err := InitProject(h, cfg, work, project.Options{Description: "The firmware."}, "AI-ML", false, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	p, written := made.Project, made.Written
	if p.Name() != "firmware" || p.Config.Knowledge == nil || p.Config.Knowledge.ID != kb.ID || p.Config.Knowledge.Name != "ai-ml" || len(written) != len(project.Folders)+2 {
		t.Fatalf("project %+v written %v", p.Config, written)
	}
	if made.Git != GitSkipped || exists(filepath.Join(work, ".git")) {
		t.Fatalf("without git, init makes no repository: %s", made.Git)
	}
	if !cfg.HasProject(work) {
		t.Fatal("the config lists the project")
	}
	saved, err := h.Load()
	if err != nil || !saved.HasProject(work) {
		t.Fatalf("and it is saved: %+v %v", saved, err)
	}
	ix, _ := registry.Scan(cfg)
	e := ix.ByPath(work)
	if e == nil || e.Knowledge == nil || e.Knowledge.Path != kb.Path {
		t.Fatalf("the scan resolves the knowledge base: %+v", e)
	}
	// A project without a knowledge base, then link and unlink.
	solo := filepath.Join(t.TempDir(), "solo")
	os.MkdirAll(solo, 0o755)
	soloMade, err := InitProject(h, cfg, solo, project.Options{}, "", false, identityNow)
	if err != nil || soloMade.Project.Config.Knowledge != nil {
		t.Fatalf("solo %+v %v", soloMade, err)
	}
	q := soloMade.Project
	if err := UnlinkKnowledge(q); err == nil {
		t.Fatal("nothing to unlink")
	}
	linked, err := LinkKnowledge(cfg, q, kb.ID[:8])
	if err != nil || linked.ID != kb.ID {
		t.Fatalf("link %+v %v", linked, err)
	}
	if again, _ := project.Open(solo); again.Config.Knowledge == nil || again.Config.Knowledge.ID != kb.ID {
		t.Fatalf("the link is saved: %+v", again.Config)
	}
	if _, err := LinkKnowledge(cfg, q, work); err == nil {
		t.Fatal("a project is not a knowledge base")
	}
	if err := UnlinkKnowledge(q); err != nil {
		t.Fatal(err)
	}
	if again, _ := project.Open(solo); again.Config.Knowledge != nil {
		t.Fatalf("the unlink is saved: %+v", again.Config)
	}
	// Edit and forget.
	desc := "Renamed and described."
	if err := EditProject(q, ProjectEdit{Name: "Solo", Description: &desc}); err != nil {
		t.Fatal(err)
	}
	if again, _ := project.Open(solo); again.Name() != "Solo" || again.Config.Description != desc {
		t.Fatalf("edit %+v", again.Config)
	}
	if err := ForgetProject(h, cfg, solo); err != nil {
		t.Fatal(err)
	}
	if cfg.HasProject(solo) || !project.IsProject(solo) {
		t.Fatal("forget drops the entry and keeps the folder")
	}
	if err := ForgetProject(h, cfg, solo); err == nil || !strings.Contains(err.Error(), "not a registered project") {
		t.Fatalf("forget twice: %v", err)
	}
}

func TestRegisterProjectHeals(t *testing.T) {
	cfg, h, _, proj := fixtureEntries(t)
	p, err := project.Open(proj.Path)
	if err != nil {
		t.Fatal(err)
	}
	if heal, err := RegisterProject(h, cfg, p); err != nil || heal != HealNone {
		t.Fatalf("a listed project needs nothing: %q %v", heal, err)
	}
	// A clone the config does not know is added.
	clone := filepath.Join(t.TempDir(), "clone")
	os.MkdirAll(clone, 0o755)
	q, _, err := project.Init(clone, project.Options{}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if heal, err := RegisterProject(h, cfg, q); err != nil || heal != HealAdded || !cfg.HasProject(clone) {
		t.Fatalf("added: %q %v %v", heal, err, cfg.Projects)
	}
	if saved, _ := h.Load(); !saved.HasProject(clone) {
		t.Fatal("the heal is saved")
	}
	// A copy of a listed project at another readable path replaces the old entry.
	copied := filepath.Join(t.TempDir(), "copied")
	if err := os.MkdirAll(filepath.Join(copied, project.Dir, "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig, _ := project.Open(proj.Path)
	data, _ := os.ReadFile(orig.Path(project.Marker))
	os.WriteFile(filepath.Join(copied, project.Dir, "p", project.Marker), data, 0o644)
	c, _ := project.Open(copied)
	if heal, err := RegisterProject(h, cfg, c); err != nil || heal != HealMoved || !cfg.HasProject(copied) || cfg.HasProject(proj.Path) {
		t.Fatalf("moved by id: %q %v %v", heal, err, cfg.Projects)
	}
	// The only listed path that is gone is taken for a moved project's old home.
	os.RemoveAll(copied)
	moved := filepath.Join(t.TempDir(), "moved")
	os.MkdirAll(filepath.Join(moved, project.Dir, "p"), 0o755)
	os.WriteFile(filepath.Join(moved, project.Dir, "p", project.Marker), data, 0o644)
	m, _ := project.Open(moved)
	if heal, err := RegisterProject(h, cfg, m); err != nil || heal != HealMoved || !cfg.HasProject(moved) || cfg.HasProject(copied) {
		t.Fatalf("moved by absence: %q %v %v", heal, err, cfg.Projects)
	}
	// Two gone paths are left alone: either may be on a drive that is not mounted.
	cfg.AddProject(filepath.Join(t.TempDir(), "gone-a"))
	cfg.AddProject(filepath.Join(t.TempDir(), "gone-b"))
	fresh := filepath.Join(t.TempDir(), "fresh")
	os.MkdirAll(fresh, 0o755)
	f, _, _ := project.Init(fresh, project.Options{}, identityNow)
	before := len(cfg.Projects)
	if heal, err := RegisterProject(h, cfg, f); err != nil || heal != HealAdded || len(cfg.Projects) != before+1 {
		t.Fatalf("two gone paths stay: %q %v %v", heal, err, cfg.Projects)
	}
}

func TestInitProjectMakesARepository(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	cfg, h, _, _ := fixtureEntries(t)
	root := t.TempDir()

	plain := filepath.Join(root, "plain")
	os.MkdirAll(plain, 0o755)
	made, err := InitProject(h, cfg, plain, project.Options{}, "", true, identityNow)
	if err != nil || made.Git != GitCreated {
		t.Fatalf("a plain folder becomes a repository: %+v %v", made, err)
	}
	repo := gitx.Repo{Dir: plain}
	if !repo.IsRepo() || repo.HasHead() {
		t.Fatal("the repository is at the work's top, with no commit")
	}
	if head, err := os.ReadFile(filepath.Join(plain, ".git", "HEAD")); err != nil || strings.TrimSpace(string(head)) != "ref: refs/heads/main" {
		t.Fatalf("its branch is main: %q %v", head, err)
	}

	existing := filepath.Join(root, "existing")
	os.MkdirAll(existing, 0o755)
	if err := (gitx.Repo{Dir: existing}).Init(); err != nil {
		t.Fatal(err)
	}
	if made, err := InitProject(h, cfg, existing, project.Options{}, "", true, identityNow); err != nil || made.Git != GitExisting {
		t.Fatalf("a repository stays as it is: %+v %v", made, err)
	}

	monorepo := filepath.Join(root, "monorepo")
	inner := filepath.Join(monorepo, "tools", "cli")
	os.MkdirAll(inner, 0o755)
	if err := (gitx.Repo{Dir: monorepo}).Init(); err != nil {
		t.Fatal(err)
	}
	made, err = InitProject(h, cfg, inner, project.Options{}, "", true, identityNow)
	if err != nil || made.Git != GitEnclosed || exists(filepath.Join(inner, ".git")) {
		t.Fatalf("a folder inside a repository gets no repository of its own: %+v %v", made, err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
