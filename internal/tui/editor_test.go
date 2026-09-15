package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/tree"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

func fakeAtlas(t *testing.T) (*home.Config, Hooks) {
	t.Helper()
	root := t.TempDir()
	cfg := &home.Config{Schema: home.ConfigSchema, VaultsDir: filepath.Join(root, "Vaults"), AtlasVault: filepath.Join(root, "Atlas")}
	os.MkdirAll(cfg.TreeRoot(), 0o755)
	for _, spec := range []struct{ name, cat string }{{"capstone", "university/cs566"}, {"reading", "personal"}, {"welcome", ""}} {
		vault := filepath.Join(cfg.VaultsDir, spec.name)
		os.MkdirAll(vault, 0o755)
		os.WriteFile(filepath.Join(vault, ".claude-atlas.json"), []byte(`{"schema":"claude-atlas.vault.v2","id":"00000000-0000-4000-8000-000000000001","kind":"project","name":"v","mode":"generic","created":"2026-09-12"}`), 0o644)
		if _, err := vaults.RegisterProject(cfg, vault, vaults.RegisterOptions{Name: spec.name, Category: spec.cat}); err != nil {
			t.Fatal(err)
		}
	}
	hooks := Hooks{
		Load: func() ([]*tree.Project, error) {
			projects, _, err := tree.Walk(cfg.TreeRoot())
			return projects, err
		},
		Categories: func() []string { return Categories(cfg.TreeRoot()) },
		State: func(rel string) *tree.State {
			if rel == "personal/reading" {
				return &tree.State{Heat: "cold"}
			}
			return nil
		},
		Update: func(p *tree.Project, edit vaults.TreeEdit) error { return vaults.Update(cfg, p, edit) },
		Unlink: vaults.Unlink,
		Links: func() []links.Page {
			pages, _, _ := links.Walk(cfg.AtlasVault)
			return pages
		},
		AddLink: func(p *tree.Project, target string, initGit bool) (links.Page, error) {
			return vaults.AddLink(cfg, p, target, initGit)
		},
		NewRepo:    func(p *tree.Project, name, at string) (links.Page, error) { return vaults.NewRepo(cfg, p, name, at) },
		CloneRepo:  func(p *tree.Project, url, at string) (links.Page, error) { return vaults.CloneRepoPage(cfg, p, url, at) },
		SetChanges: func(page links.Page, policy string) (links.Page, error) { return vaults.SetChanges(cfg, page, policy) },
		RemoveLink: func(p *tree.Project, target string) error { return vaults.RemoveLink(cfg, p, target) },
		EditLink: func(page links.Page, edit vaults.LinkEdit) (links.Page, error) {
			return vaults.UpdateLink(cfg, page, edit)
		},
		Refresh:   func() error { return nil },
		VaultsDir: cfg.VaultsDir,
	}
	return cfg, hooks
}

// atlasView opens the tree over the fake atlas with the cursor on "reading" (rows: welcome, personal, reading, ...).
func atlasView(t *testing.T) (*home.Config, view) {
	t.Helper()
	cfg, hooks := fakeAtlas(t)
	projects, _ := hooks.Load()
	var items []Item
	for _, p := range projects {
		items = append(items, Item{Project: p, State: hooks.State(p.Rel)})
	}
	v := newView(items, Opener{}, hooks)
	v = pressV(v, tea.KeyDown, tea.KeyDown)
	if r := v.current(); r.kind != rowProject || r.item.Project.ID() != "reading" {
		t.Fatalf("cursor on %s", v.rows[v.cursor].item.Project.Rel)
	}
	return cfg, v
}

func keyV(v view, s string) view {
	next, _ := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(view)
}

func typeV(v view, text string) view {
	for _, r := range text {
		v = keyV(v, string(r))
	}
	return v
}

func TestEditRenameRepriorityAndMoveCategory(t *testing.T) {
	cfg, v := atlasView(t)
	v = keyV(v, "e")
	if v.edit == nil || v.edit.current.ID() != "reading" || !strings.Contains(v.View(), "tree/personal/reading.md") {
		t.Fatalf("editor not open:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEnter) // edit name
	v = typeV(v, " List")
	v = pressV(v, tea.KeyEnter)
	if v.edit.draft.Name != "reading List" {
		t.Fatalf("name %q", v.edit.draft.Name)
	}
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyEnter) // category picker
	v = typeV(v, "leisure")
	v = pressV(v, tea.KeyEnter)
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyRight) // priority normal → low
	if v.edit.draft.Category != "leisure" || v.edit.draft.Priority != "low" || !v.edit.dirty() {
		t.Fatalf("draft %+v", v.edit.draft)
	}
	v = keyV(v, "s")
	if v.edit != nil || !v.changed || v.errMsg != "" || v.status != "saved reading List" {
		t.Fatalf("save: edit=%v changed=%v err=%q status=%q", v.edit, v.changed, v.errMsg, v.status)
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	moved := tree.FindByRel(projects, "leisure/reading")
	if moved == nil || moved.Name != "reading List" || moved.Priority != "low" {
		t.Fatalf("not applied: %+v", moved)
	}
	if r := v.rows[v.cursor]; r.kind != rowProject || r.item.Project.Rel != "leisure/reading" {
		t.Fatalf("cursor not on moved project: %+v", r)
	}
	if !strings.Contains(v.View(), "leisure") || !strings.Contains(v.View(), "reading List") {
		t.Fatalf("tree not reloaded:\n%s", v.View())
	}
}

func TestCategoryChangeOffersToMoveTheVault(t *testing.T) {
	for _, answer := range []string{"y", "n"} {
		cfg, v := atlasView(t)
		v = pressV(v, tea.KeyUp, tea.KeyUp) // welcome, whose vault sits in the top level's folder
		v = keyV(v, "e")
		v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyEnter) // category picker
		v = typeV(v, "archive")
		v = pressV(v, tea.KeyEnter)
		v = keyV(v, "s")
		if v.edit == nil || v.edit.mode != confirmMove || !strings.Contains(v.View(), "sits in its category's folder") {
			t.Fatalf("%s: expected the offer:\n%s", answer, v.View())
		}
		v = keyV(v, answer)
		if v.edit != nil || v.errMsg != "" {
			t.Fatalf("%s: save: edit=%v err=%q", answer, v.edit, v.errMsg)
		}
		want := filepath.Join(cfg.VaultsDir, "welcome")
		if answer == "y" {
			want = filepath.Join(cfg.VaultsDir, "archive", "welcome")
		}
		projects, _, _ := tree.Walk(cfg.TreeRoot())
		if p := tree.FindByRel(projects, "archive/welcome"); p == nil || p.VaultPath() != want {
			t.Fatalf("%s: page %+v, want vault %s", answer, p, want)
		}
		if _, err := os.Stat(filepath.Join(want, ".claude-atlas.json")); err != nil {
			t.Fatalf("%s: vault not at %s", answer, want)
		}
	}
}

func TestEscWarnsBeforeDiscarding(t *testing.T) {
	_, v := atlasView(t)
	v = keyV(v, "e")
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyRight) // priority changed
	v = pressV(v, tea.KeyEsc)
	if v.edit == nil || !strings.Contains(v.edit.err, "unsaved") {
		t.Fatalf("first esc should warn: %+v", v.edit)
	}
	v = pressV(v, tea.KeyEsc)
	if v.edit != nil || v.changed || v.detail != nil {
		t.Fatalf("second esc should discard: edit=%v changed=%v", v.edit, v.changed)
	}
}

func TestQuitKeyTypesInsideAField(t *testing.T) {
	_, v := atlasView(t)
	v = keyV(v, "e")
	v = pressV(v, tea.KeyEnter) // name field
	v = keyV(v, "q")
	if v.edit == nil || !strings.HasSuffix(v.edit.text.Value(), "q") {
		t.Fatalf("q should be typed, not quit: %+v", v.edit)
	}
}

func TestRemoveUnlinksOnly(t *testing.T) {
	cfg, v := atlasView(t)
	v = keyV(v, "e")
	v = keyV(v, "r")
	if v.edit.mode != confirmRemove || !strings.Contains(v.View(), "stays on disk") {
		t.Fatalf("mode %d\n%s", v.edit.mode, v.View())
	}
	v = keyV(v, "y")
	if v.edit != nil || !v.changed || len(v.items) != 2 || !strings.Contains(v.status, "removed reading") {
		t.Fatalf("remove: edit=%v changed=%v items=%d status=%q", v.edit, v.changed, len(v.items), v.status)
	}
	if _, err := os.Stat(filepath.Join(cfg.VaultsDir, "reading", ".claude-atlas.json")); err != nil {
		t.Fatal("vault was deleted")
	}
}

func TestMoveVaultAsksFirst(t *testing.T) {
	cfg, v := atlasView(t)
	v = keyV(v, "e")
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyEnter) // vault field
	target := filepath.Join(cfg.VaultsDir, "archive", "reading")
	if v.edit.mode != editPath {
		t.Fatalf("the vault field completes paths: mode %d", v.edit.mode)
	}
	v.edit.path.setValue(target)
	v = pressV(v, tea.KeyEnter)
	v = keyV(v, "s")
	if v.edit == nil || v.edit.mode != confirmMove {
		t.Fatalf("expected move confirmation: %+v", v.edit)
	}
	v = keyV(v, "y")
	if v.edit != nil || v.errMsg != "" || !v.changed {
		t.Fatalf("move: edit=%v err=%q", v.edit, v.errMsg)
	}
	if _, err := os.Stat(filepath.Join(target, ".claude-atlas.json")); err != nil {
		t.Fatal("vault not moved")
	}
}

func TestEditRelatedThroughThePicker(t *testing.T) {
	cfg, v := atlasView(t)
	v = keyV(v, "e")
	for i := 0; i < fieldRelated; i++ {
		v = pressV(v, tea.KeyDown)
	}
	v = pressV(v, tea.KeyEnter)
	if v.edit.mode != editList || !strings.Contains(v.View(), "none yet; press a to relate") {
		t.Fatalf("related list:\n%s", v.View())
	}
	v = keyV(v, "a")
	if v.edit.mode != editListPick || !strings.Contains(v.View(), "capstone") || strings.Contains(v.View(), "▸ reading") {
		t.Fatalf("picker should list the other projects:\n%s", v.View())
	}
	v = typeV(v, "cap")
	v = pressV(v, tea.KeyEnter)
	if len(v.edit.draft.Related) != 1 || v.edit.draft.Related[0] != "university/cs566/capstone" {
		t.Fatalf("related %v", v.edit.draft.Related)
	}
	v = pressV(v, tea.KeyEsc)
	v = keyV(v, "s")
	if v.edit != nil || v.errMsg != "" {
		t.Fatalf("save: err=%q", v.errMsg)
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	if p := tree.FindByRel(projects, "personal/reading"); len(p.RelatedTo) != 1 || p.Related[0] != "[[tree/university/cs566/capstone|capstone]]" {
		t.Fatalf("page: %v", p.Related)
	}
	// The other side sees it as related from, in the editor and the details.
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyDown) // university › cs566 › capstone
	if r := v.current(); r == nil || r.kind != rowProject || r.item.Project.ID() != "capstone" {
		t.Fatalf("cursor on %+v", r)
	}
	v = pressV(v, tea.KeyEnter)
	if !strings.Contains(v.View(), "related from its page") {
		t.Fatalf("details:\n%s", v.View())
	}
	v = keyV(v, "e")
	if !strings.Contains(v.View(), "related from reading") {
		t.Fatalf("editor:\n%s", v.View())
	}
}

func TestEditFromDetailReturnsToDetail(t *testing.T) {
	_, v := atlasView(t)
	v = pressV(v, tea.KeyEnter) // detail
	if v.detail == nil {
		t.Fatal("detail should open")
	}
	v = keyV(v, "e")
	v = pressV(v, tea.KeyEsc)
	if v.edit != nil || v.detail == nil || v.detail.Project.ID() != "reading" {
		t.Fatalf("esc should return to the detail page: edit=%v detail=%v", v.edit, v.detail)
	}
	v = keyV(v, "e")
	v = pressV(v, tea.KeyEnter) // name
	v = typeV(v, " 2")
	v = pressV(v, tea.KeyEnter)
	v = keyV(v, "s")
	if v.edit != nil || v.detail == nil || v.detail.Project.Name != "reading 2" || !strings.Contains(v.View(), "reading 2") {
		t.Fatalf("saved edit should refresh the detail page: detail=%+v", v.detail)
	}
}

func TestEditNeedsHooks(t *testing.T) {
	v := newView(sample(), Opener{}, Hooks{})
	v = keyV(v, "e")
	if v.edit != nil || !strings.Contains(v.errMsg, "not available") {
		t.Fatalf("edit without hooks: %+v %q", v.edit, v.errMsg)
	}
}

func TestEditIntentFieldsAndDateValidation(t *testing.T) {
	cfg, v := atlasView(t)
	v = keyV(v, "e")
	for i := 0; i < fieldBlockedOn; i++ {
		v = pressV(v, tea.KeyDown)
	}
	v = pressV(v, tea.KeyEnter)
	v = typeV(v, "hardware")
	v = pressV(v, tea.KeyEnter, tea.KeyDown, tea.KeyEnter) // review after
	v = typeV(v, "soon")
	v = pressV(v, tea.KeyEnter)
	v = keyV(v, "s")
	if v.edit == nil || !strings.Contains(v.edit.err, "date like") || v.edit.field != fieldReviewAfter {
		t.Fatalf("bad date should be refused: %+v", v.edit)
	}
	v = pressV(v, tea.KeyEnter)
	v.edit.text.SetValue("2026-10-01")
	v = pressV(v, tea.KeyEnter, tea.KeyDown, tea.KeyEnter) // done when
	v = typeV(v, "Ships.")
	v = pressV(v, tea.KeyEnter)
	if !strings.Contains(v.View(), "Blocked on") || !strings.Contains(v.View(), "Done when") {
		t.Fatalf("fields missing:\n%s", v.View())
	}
	v = keyV(v, "s")
	if v.edit != nil || v.errMsg != "" {
		t.Fatalf("save: %+v %q", v.edit, v.errMsg)
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	p := tree.FindByRel(projects, "personal/reading")
	if p.BlockedOn != "hardware" || p.ReviewAfter != "2026-10-01" || p.DefinitionOfDone != "Ships." {
		t.Fatalf("page: %+v", p.Frontmatter)
	}
}
