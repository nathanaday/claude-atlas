package place

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

var now = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

// atlas makes a home with one knowledge base under its vaults directory and one project
// on it, registered, and returns the home, the config, the knowledge base's root, and
// the project's work folder.
func atlas(t *testing.T) (home.Home, *home.Config, string, string) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := h.Default()
	kb := filepath.Join(root, "Vaults", "ai-ml")
	if _, err := vault.Init(kb, vault.Options{Name: "ai-ml"}, now); err != nil {
		t.Fatal(err)
	}
	cfg.AddKnowledge(kb)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "Code", "webapp")
	if err := os.MkdirAll(filepath.Join(work, "src", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := vaults.InitProject(h, cfg, work, project.Options{}, "ai-ml", false, now); err != nil {
		t.Fatal(err)
	}
	return h, cfg, kb, work
}

func TestResolveFindsProjectsAndKnowledgeBases(t *testing.T) {
	h, _, kb, work := atlas(t)
	nested := filepath.Join(work, "src", "deep")
	pl, err := Resolve(h, "", "", nested, false)
	if err != nil {
		t.Fatal(err)
	}
	if !pl.InProject() || pl.Project.Root != work || pl.Vault == nil || pl.Vault.Root != kb || pl.Entry == nil || pl.Entry.Name != "webapp" || pl.Index == nil || pl.Heal != vaults.HealNone || pl.KnowledgeError != "" {
		t.Fatalf("walk up from inside the work: %+v", pl)
	}
	pl, err = Resolve(h, "", "", filepath.Join(kb, "wiki", "concepts"), false)
	if err != nil {
		t.Fatal(err)
	}
	if pl.InProject() || pl.Vault == nil || pl.Vault.Root != kb || pl.Entry == nil || pl.Entry.Name != "ai-ml" {
		t.Fatalf("walk up inside the knowledge base: %+v", pl)
	}
	if pl, err := Resolve(h, work, "", t.TempDir(), false); err != nil || !pl.InProject() || pl.Project.Root != work {
		t.Fatalf("explicit work folder: %+v %v", pl, err)
	}
	if pl, err := Resolve(h, "", kb, t.TempDir(), false); err != nil || pl.InProject() || pl.Vault.Root != kb {
		t.Fatalf("env knowledge base: %+v %v", pl, err)
	}
	if pl, err := Resolve(h, kb, work, t.TempDir(), false); err != nil || pl.InProject() {
		t.Fatalf("the explicit path wins over the environment: %+v %v", pl, err)
	}
	if _, err := Resolve(h, t.TempDir(), work, work, false); !errors.Is(err, ErrNoPlace) {
		t.Fatalf("an explicit path that is neither fails even when the environment names one: %v", err)
	}
	if _, err := Resolve(h, "", "", t.TempDir(), false); !errors.Is(err, ErrNoPlace) {
		t.Fatalf("nothing above: %v", err)
	}
	if _, err := Resolve(h, "", "", "", false); !errors.Is(err, ErrNoPlace) {
		t.Fatalf("no start: %v", err)
	}
	if (*Place)(nil).InProject() {
		t.Fatal("a nil place is nowhere")
	}
	if Cwd() == "" {
		t.Fatal("Cwd")
	}
}

func TestResolveWithoutAnAtlasOrAKnowledgeBase(t *testing.T) {
	_, _, kb, work := atlas(t)
	nowhere := home.Home{Root: filepath.Join(t.TempDir(), "no-atlas")}
	pl, err := Resolve(nowhere, "", "", work, true)
	if err != nil {
		t.Fatal(err)
	}
	if !pl.InProject() || pl.Vault != nil || pl.Entry != nil || !strings.Contains(pl.KnowledgeError, "no atlas config") {
		t.Fatalf("no atlas: %+v", pl)
	}
	pl, err = Resolve(nowhere, "", "", kb, false)
	if err != nil || pl.Vault == nil || pl.Entry != nil || pl.Index != nil {
		t.Fatalf("a knowledge base without an atlas: %+v %v", pl, err)
	}
	// A project that uses no knowledge base, and one whose knowledge base is not here.
	h, cfg, _, _ := atlas(t)
	solo := filepath.Join(t.TempDir(), "solo")
	os.MkdirAll(solo, 0o755)
	if _, err := vaults.InitProject(h, cfg, solo, project.Options{}, "", false, now); err != nil {
		t.Fatal(err)
	}
	if pl, err := Resolve(h, "", "", solo, false); err != nil || pl.Vault != nil || pl.KnowledgeError != "" || pl.Entry == nil {
		t.Fatalf("no knowledge base: %+v %v", pl, err)
	}
	p, _ := project.Open(solo)
	p.Config.Knowledge = &project.Knowledge{ID: "gone-0000", Name: "papers"}
	p.Save()
	if pl, err := Resolve(h, "", "", solo, false); err != nil || pl.Vault != nil || !strings.Contains(pl.KnowledgeError, "papers") || !strings.Contains(pl.KnowledgeError, "gone-0000") {
		t.Fatalf("a knowledge base not on this machine: %+v %v", pl, err)
	}
}

func TestResolveHealsTheConfig(t *testing.T) {
	h, cfg, _, work := atlas(t)
	// Not registered: a clone.
	clone := filepath.Join(t.TempDir(), "clone")
	os.MkdirAll(clone, 0o755)
	if _, _, err := project.Init(clone, project.Options{}, now); err != nil {
		t.Fatal(err)
	}
	if pl, err := Resolve(h, "", "", clone, false); err != nil || pl.Heal != vaults.HealNone || pl.Entry != nil {
		t.Fatalf("without register nothing changes: %+v %v", pl, err)
	}
	pl, err := Resolve(h, "", "", clone, true)
	if err != nil || pl.Heal != vaults.HealAdded || pl.Entry == nil || pl.Entry.Path != clone {
		t.Fatalf("added: %+v %v", pl, err)
	}
	saved, _ := h.Load()
	if !saved.HasProject(clone) {
		t.Fatalf("the heal is saved: %v", saved.Projects)
	}
	// Moved: the same id at another readable path.
	copied := filepath.Join(t.TempDir(), "copied")
	os.MkdirAll(filepath.Join(copied, project.Dir, "p"), 0o755)
	orig, _ := project.Open(work)
	data, _ := os.ReadFile(orig.Path(project.Marker))
	os.WriteFile(filepath.Join(copied, project.Dir, "p", project.Marker), data, 0o644)
	pl, err = Resolve(h, "", "", copied, true)
	if err != nil || pl.Heal != vaults.HealMoved || pl.Entry == nil || pl.Entry.Path != copied || pl.Vault == nil {
		t.Fatalf("moved by id: %+v %v", pl, err)
	}
	saved, _ = h.Load()
	if !saved.HasProject(copied) || saved.HasProject(work) {
		t.Fatalf("the old entry is gone: %v", saved.Projects)
	}
	// Moved: the only listed path that is gone.
	os.RemoveAll(copied)
	moved := filepath.Join(t.TempDir(), "moved")
	os.MkdirAll(filepath.Join(moved, project.Dir, "p"), 0o755)
	os.WriteFile(filepath.Join(moved, project.Dir, "p", project.Marker), data, 0o644)
	pl, err = Resolve(h, "", "", moved, true)
	if err != nil || pl.Heal != vaults.HealMoved || pl.Entry == nil || pl.Entry.Path != moved {
		t.Fatalf("moved by absence: %+v %v", pl, err)
	}
	saved, _ = h.Load()
	if !saved.HasProject(moved) || saved.HasProject(copied) || len(saved.Projects) != 2 {
		t.Fatalf("projects %v (cfg %v)", saved.Projects, cfg.Projects)
	}
	if pl, err := Resolve(h, "", "", moved, true); err != nil || pl.Heal != vaults.HealNone {
		t.Fatalf("a second session heals nothing: %+v %v", pl, err)
	}
}

func TestResolveAKnowledgeBaseInsideAProject(t *testing.T) {
	h, _, _, work := atlas(t)
	kb := filepath.Join(work, "notes")
	if _, err := vault.Init(kb, vault.Options{Name: "notes"}, now); err != nil {
		t.Fatal(err)
	}
	pl, err := Resolve(h, "", "", filepath.Join(kb, "wiki", "concepts"), false)
	if err != nil || pl.InProject() || pl.Vault == nil || pl.Vault.Root != kb {
		t.Fatalf("inside the knowledge base, the knowledge base is nearer: %+v %v", pl, err)
	}
	pl, err = Resolve(h, "", "", filepath.Join(work, "src", "deep"), false)
	if err != nil || !pl.InProject() || pl.Project.Root != work {
		t.Fatalf("elsewhere in the work, the project: %+v %v", pl, err)
	}
}
