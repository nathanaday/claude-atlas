package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/tree"
)

func TestLinksScreenAddsEditsAndUnlinks(t *testing.T) {
	cfg, v := atlasView(t)
	repo := filepath.Join(cfg.VaultsDir, "code")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	v = keyV(v, "l")
	if v.links == nil || !strings.Contains(v.View(), "no links yet") || !strings.Contains(v.View(), "reading   links") {
		t.Fatalf("l should open the links screen:\n%s", v.View())
	}
	// Add: a missing folder is refused in place; a repo is linked and the box shows it.
	v = keyV(v, "a")
	if v.links.mode != linksAdd || !strings.Contains(v.View(), "Enter link") {
		t.Fatalf("add mode:\n%s", v.View())
	}
	v.links.source.setValue(filepath.Join(cfg.VaultsDir, "missing"))
	v = pressV(v, tea.KeyEnter)
	if v.links.mode != linksAdd || v.links.err == "" {
		t.Fatalf("missing folder: mode=%d err=%q", v.links.mode, v.links.err)
	}
	v.links.source.setValue(repo)
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyEnter})
	v = next.(view)
	if v.links.mode != linksList || len(v.links.rows) != 1 || v.links.rows[0].link.Name != "code" || cmd == nil || !v.changed {
		t.Fatalf("after add: mode=%d rows=%+v cmd=%v changed=%v err=%q", v.links.mode, v.links.rows, cmd, v.changed, v.links.err)
	}
	out := v.View()
	for _, want := range []string{"╭", "code", "repo", "Vaults/code", "not refreshed yet", "linked code (repo)", "refreshing", "u unlink"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	if p := tree.FindByRel(projects, "personal/reading"); len(p.Linked) != 1 || p.Linked[0].Path != repo {
		t.Fatalf("page not updated: %+v", p.Linked)
	}
	// The background refresh lands: the screen stays open and reloads.
	next, _ = v.Update(refreshedMsg{})
	v = next.(view)
	if v.links == nil || len(v.links.rows) != 1 || v.links.status != "refreshed" {
		t.Fatalf("after refresh: %+v", v.links)
	}
	// Edit: rename the page and change its kind.
	v = keyV(v, "e")
	if v.links.mode != linksEdit || !strings.Contains(v.View(), "Edit code") {
		t.Fatalf("edit mode:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEnter) // name
	v.links.edit.text.SetValue("Atlas code")
	v = pressV(v, tea.KeyEnter, tea.KeyDown, tea.KeyRight) // kind → materials
	if !strings.Contains(v.View(), "◂ materials ▸") || !strings.Contains(v.View(), "s save") {
		t.Fatalf("edit form:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEsc)
	if v.links.mode != linksEdit || !strings.Contains(v.links.edit.err, "unsaved") {
		t.Fatalf("esc should warn first: mode=%d err=%q", v.links.mode, v.links.edit.err)
	}
	v = keyV(v, "s")
	if v.links.mode != linksList || v.links.rows[0].link.Name != "Atlas code" || v.links.rows[0].link.Kind != "materials" {
		t.Fatalf("after edit: mode=%d rows=%+v err=%q", v.links.mode, v.links.rows, v.links.edit.err)
	}
	if _, err := os.Stat(filepath.Join(cfg.AtlasVault, "materials", "Atlas code.md")); err != nil {
		t.Fatal("page should be renamed and moved")
	}
	// Another project links the same page by name and sees who shares it.
	v = pressV(v, tea.KeyEsc)
	if v.links != nil {
		t.Fatal("esc should close the screen")
	}
	v = pressV(v, tea.KeyUp, tea.KeyUp) // welcome
	v = keyV(v, "l")
	v = keyV(v, "a")
	if !strings.Contains(v.View(), "Atlas code") {
		t.Fatalf("known pages should be offered:\n%s", v.View())
	}
	v = typeV(v, "atlas code")
	v = pressV(v, tea.KeyEnter)
	if len(v.links.rows) != 1 || !strings.Contains(v.View(), "also reading") {
		t.Fatalf("link by name: rows=%+v err=%q\n%s", v.links.rows, v.links.err, v.View())
	}
	// Unlink asks first; the page stays for the other project.
	v = keyV(v, "u")
	if v.links.mode != linksConfirmUnlink || !strings.Contains(v.View(), "Unlink Atlas code from welcome?") {
		t.Fatalf("confirm:\n%s", v.View())
	}
	v = keyV(v, "n")
	if v.links.mode != linksList || len(v.links.rows) != 1 {
		t.Fatal("n should keep the link")
	}
	v = keyV(v, "u")
	v = keyV(v, "y")
	if len(v.links.rows) != 0 || !strings.Contains(v.links.status, "unlinked Atlas code") {
		t.Fatalf("after unlink: rows=%+v status=%q err=%q", v.links.rows, v.links.status, v.links.err)
	}
	if _, err := os.Stat(filepath.Join(cfg.AtlasVault, "materials", "Atlas code.md")); err != nil {
		t.Fatal("the page stays for the other project")
	}
	projects, _, _ = tree.Walk(cfg.TreeRoot())
	if p := tree.FindByRel(projects, "personal/reading"); len(p.Linked) != 1 || p.Linked[0].Name != "Atlas code" {
		t.Fatalf("reading should still link it: %+v", p.Linked)
	}
}

func TestLinksScreenNeedsHooks(t *testing.T) {
	none := keyV(pressV(newView(sample(), Opener{}, Hooks{}), tea.KeyDown, tea.KeyDown, tea.KeyDown), "l")
	if none.links != nil || !strings.Contains(none.errMsg, "not available") {
		t.Fatal("l without hooks reports why")
	}
}
