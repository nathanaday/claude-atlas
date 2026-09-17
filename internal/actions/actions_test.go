package actions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

var now = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

// atlas builds an atlas home whose vaults directory holds one knowledge base, kb.
func atlas(t *testing.T) (home.Home, *home.Config, registry.Entry) {
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
	kbPath := vaults.PathFor(cfg.VaultsDir, "kb")
	if _, err := vault.Init(kbPath, vault.Options{Name: "kb"}, now); err != nil {
		t.Fatal(err)
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	kb := ix.ByPath(kbPath)
	if kb == nil {
		t.Fatal("the scan did not find kb")
	}
	return h, cfg, *kb
}

func entry(t *testing.T, a Atlas, path string) registry.Entry {
	t.Helper()
	ix, err := a.Scan()
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByPath(path)
	if e == nil {
		t.Fatalf("no entry at %s", path)
	}
	return *e
}

func TestBindSetsEveryField(t *testing.T) {
	a := Bind(home.Home{Root: t.TempDir()}, &home.Config{}, nil)
	v := reflect.ValueOf(a)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() == reflect.Func && f.IsNil() {
			t.Errorf("Bind leaves %s nil", v.Type().Field(i).Name)
		}
	}
}

func TestKnowledgeBasesCreateEditAndForget(t *testing.T) {
	h, cfg, _ := atlas(t)
	a := Bind(h, cfg, nil)
	if a.VaultsDir != cfg.VaultsDir {
		t.Fatal("VaultsDir")
	}
	path, err := a.CreateKnowledge(AddKnowledge{Name: "papers", Path: vaults.PathFor(cfg.VaultsDir, "papers"), Mode: "lyt", Scope: "Papers."})
	if err != nil {
		t.Fatal(err)
	}
	e := entry(t, a, path)
	if e.Kind != registry.Knowledge || e.Scope != "Papers." || e.Mode != vault.LYT || e.State == nil {
		t.Fatalf("knowledge base: %+v", e)
	}
	if _, err := a.CreateKnowledge(AddKnowledge{Name: "x", Path: filepath.Join(t.TempDir(), "x"), Mode: "other"}); err == nil {
		t.Fatal("a bad mode is refused")
	}
	moved, err := a.EditKnowledge(e, vaults.Edit{Name: "Papers"})
	if err != nil || filepath.Base(moved) != "Papers" {
		t.Fatalf("rename: %q %v", moved, err)
	}
	// An adopted vault outside the vaults directory is registered and can be forgotten;
	// one inside cannot.
	outside := filepath.Join(t.TempDir(), "outside")
	os.MkdirAll(filepath.Join(outside, "wiki"), 0o755)
	adopted, err := a.CreateKnowledge(AddKnowledge{Path: outside, Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg2, _ := h.Load(); len(cfg2.Knowledge) != 1 || cfg2.Knowledge[0] != adopted {
		t.Fatalf("registered: %+v", cfg2.Knowledge)
	}
	if err := a.ForgetKnowledge(entry(t, a, adopted)); err != nil {
		t.Fatalf("forget outside: %v", err)
	}
	if err := a.ForgetKnowledge(entry(t, a, moved)); err == nil || !strings.Contains(err.Error(), "vaults directory") {
		t.Fatalf("forget inside: %v", err)
	}
	if ix, err := a.Refresh(); err != nil || len(ix.Knowledge()) != 2 {
		t.Fatalf("refresh: %v %v", ix, err)
	}
	if entries, err := a.Load(); err != nil || len(entries) != 2 {
		t.Fatalf("load: %v %v", entries, err)
	}
}

func TestProjectsInitLinkTasksAndPhases(t *testing.T) {
	h, cfg, kb := atlas(t)
	a := Bind(h, cfg, nil)
	work := filepath.Join(t.TempDir(), "webapp")
	os.MkdirAll(work, 0o755)
	p, written, err := a.InitProject(InitProject{Work: work, Description: "The web app.", Knowledge: "kb"})
	if err != nil || p.Config.Knowledge == nil || p.Config.Knowledge.ID != kb.ID || len(written) != 5 {
		t.Fatalf("init: %+v %v %v", p, written, err)
	}
	e := entry(t, a, work)
	if e.Kind != registry.Project || e.KnowledgePath() != kb.Path || e.State == nil || e.State.Tasks == nil {
		t.Fatalf("project: %+v", e)
	}
	if err := a.UnlinkProject(e); err != nil {
		t.Fatal(err)
	}
	if e = entry(t, a, work); e.Knowledge != nil {
		t.Fatalf("unlinked: %+v", e)
	}
	if _, err := a.StageProject(e); err == nil || !strings.Contains(err.Error(), "uses no knowledge base") {
		t.Fatalf("no knowledge base to stage into: %v", err)
	}
	if _, err := a.LinkProject(e, "kb"); err != nil {
		t.Fatal(err)
	}
	desc := "Described."
	if err := a.EditProject(e, vaults.ProjectEdit{Name: "Web App", Description: &desc}); err != nil {
		t.Fatal(err)
	}
	e = entry(t, a, work)
	if e.Name != "Web App" || e.Description != desc || e.KnowledgePath() != kb.Path {
		t.Fatalf("edited: %+v", e)
	}
	staged, err := a.StageProject(e)
	if err != nil || !staged.New || !strings.HasPrefix(staged.To, "inbox/Web App-") {
		t.Fatalf("stage: %+v %v", staged, err)
	}
	// Tasks and phases.
	ph, err := a.AddPhase(e, "Alpha", "First.", nil)
	if err != nil || ph.Order != 1 {
		t.Fatalf("phase: %+v %v", ph, err)
	}
	task, err := a.Plant(e, tasks.Plant{Title: "Fix it", Phase: "Alpha"})
	if err != nil || task.Phase != "Alpha" {
		t.Fatalf("plant: %+v %v", task, err)
	}
	board, notes, err := a.Tasks(e)
	if err != nil || len(board.Tasks) != 1 || len(board.Phases) != 1 || len(notes) != 0 {
		t.Fatalf("tasks: %+v %v %v", board, notes, err)
	}
	high := "high"
	set, err := a.SetTask(e, task.ID, tasks.Changes{Priority: &high})
	if err != nil || set.Priority != "high" {
		t.Fatalf("set: %+v %v", set, err)
	}
	if err := a.RemovePhase(e, "Alpha"); err == nil {
		t.Fatal("a phase with a task is not removed")
	}
	none := ""
	if _, err := a.SetTask(e, task.ID, tasks.Changes{Phase: &none}); err != nil {
		t.Fatal(err)
	}
	if err := a.RemovePhase(e, "Alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddPhase(e, "Beta", "", nil); err != nil {
		t.Fatal(err)
	}
	if ph, err := a.OrderPhase(e, "Beta", 5); err != nil || ph.Order != 5 {
		t.Fatalf("order: %+v %v", ph, err)
	}
	if ph, err := a.RenamePhase(e, "Beta", "Gamma"); err != nil || ph.Title != "Gamma" {
		t.Fatalf("rename: %+v %v", ph, err)
	}
	if err := a.ForgetProject(e); err != nil {
		t.Fatal(err)
	}
	if cfg2, _ := h.Load(); cfg2.HasProject(work) {
		t.Fatal("forgotten")
	}
	if !project.IsProject(work) {
		t.Fatal("forget keeps the folder")
	}
}
