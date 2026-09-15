package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/tree"
)

func TestLinksScreenLinksCreatesEditsAndUnlinks(t *testing.T) {
	cfg, v := atlasView(t)
	repo := filepath.Join(cfg.VaultsDir, "code")
	os.MkdirAll(repo, 0o755)
	v = keyV(v, "l")
	if v.links == nil || !strings.Contains(v.View(), "no repositories yet") || !strings.Contains(v.View(), "reading   repositories") {
		t.Fatalf("l should open the repositories screen:\n%s", v.View())
	}
	// Link a plain folder: refused in place, then a yes makes it a repository.
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
	v = pressV(v, tea.KeyEnter)
	if v.links.mode != linksConfirmInit || !strings.Contains(v.View(), "is not a git repository. Initialize one there") {
		t.Fatalf("init confirmation:\n%s", v.View())
	}
	v = keyV(v, "n")
	if v.links.mode != linksList || len(v.links.rows) != 0 {
		t.Fatal("n should link nothing")
	}
	v = keyV(v, "a")
	v.links.source.setValue(repo)
	v = pressV(v, tea.KeyEnter)
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	v = next.(view)
	if v.links.mode != linksList || len(v.links.rows) != 1 || v.links.rows[0].link.Name != "code" || cmd == nil || !v.changed || !strings.Contains(v.links.status, "git repository now") {
		t.Fatalf("after init and link: mode=%d rows=%+v status=%q err=%q", v.links.mode, v.links.rows, v.links.status, v.links.err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		t.Fatal("the folder should be a repository now")
	}
	out := v.View()
	for _, want := range []string{"╭", "code", "repository", "Vaults/code", "not refreshed yet", "u unlink"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	if p := tree.FindByRel(projects, "personal/reading"); len(p.Linked) != 1 || p.Linked[0].Path != repo || p.Linked[0].Kind != "repo" {
		t.Fatalf("page not updated: %+v", p.Linked)
	}
	next, _ = v.Update(refreshedMsg{})
	v = next.(view)
	if v.links == nil || len(v.links.rows) != 1 || v.links.status != "refreshed" {
		t.Fatalf("after refresh: %+v", v.links)
	}
	// Create a new repository beside the wiki.
	v = keyV(v, "n")
	if v.links.mode != linksNewName || !strings.Contains(v.View(), "own git history") {
		t.Fatalf("new name mode:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEnter)
	if v.links.mode != linksNewName || v.links.err == "" {
		t.Fatal("an empty name is refused")
	}
	v = typeV(v, "paper")
	v = pressV(v, tea.KeyEnter)
	vaultPath := tree.FindByRel(projects, "personal/reading").VaultPath()
	if v.links.mode != linksNewPath || !strings.HasSuffix(v.links.where.value(), "/reading/paper") || !strings.Contains(v.View(), "beside the wiki") {
		t.Fatalf("new path mode: %q\n%s", v.links.where.value(), v.View())
	}
	v = pressV(v, tea.KeyEnter)
	if v.links.mode != linksList || len(v.links.rows) != 2 || !strings.Contains(v.links.status, "created") {
		t.Fatalf("after create: mode=%d rows=%d status=%q err=%q", v.links.mode, len(v.links.rows), v.links.status, v.links.err)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, "paper", ".git")); err != nil {
		t.Fatal("paper should be a repository in the vault")
	}
	// Edit: rename the page.
	v = pressV(v, tea.KeyUp)
	v = keyV(v, "e")
	if v.links.mode != linksEdit || !strings.Contains(v.View(), "Edit code") || strings.Contains(v.View(), "Kind") {
		t.Fatalf("edit mode:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEnter)
	v.links.edit.text.SetValue("Atlas code")
	v = pressV(v, tea.KeyEnter)
	if !strings.Contains(v.View(), "s save") {
		t.Fatalf("edit form:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEsc)
	if v.links.mode != linksEdit || !strings.Contains(v.links.edit.err, "unsaved") {
		t.Fatalf("esc should warn first: mode=%d err=%q", v.links.mode, v.links.edit.err)
	}
	v = keyV(v, "s")
	if v.links.mode != linksList {
		t.Fatalf("after edit: mode=%d err=%q", v.links.mode, v.links.edit.err)
	}
	if _, err := os.Stat(filepath.Join(cfg.AtlasVault, "repos", "Atlas code.md")); err != nil {
		t.Fatal("page should be renamed")
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
	if _, err := os.Stat(filepath.Join(cfg.AtlasVault, "repos", "Atlas code.md")); err != nil {
		t.Fatal("the page stays for the other project")
	}
}

func TestLinksScreenNeedsHooks(t *testing.T) {
	none := keyV(pressV(newView(sample(), Opener{}, Hooks{}), tea.KeyDown, tea.KeyDown, tea.KeyDown), "l")
	if none.links != nil || !strings.Contains(none.errMsg, "not available") {
		t.Fatal("l without hooks reports why")
	}
}

func TestLinksScreenClonesAndAsksHowChangesLand(t *testing.T) {
	cfg, v := atlasView(t)
	upstream := filepath.Join(cfg.VaultsDir, "upstream")
	os.MkdirAll(upstream, 0o755)
	os.WriteFile(filepath.Join(upstream, "hello.txt"), []byte("hi"), 0o644)
	if err := links.InitRepo(upstream, "upstream"); err != nil {
		t.Fatal(err)
	}
	url := "file://" + upstream
	v = keyV(v, "l")
	v = keyV(v, "a")
	v.links.source.setValue(url)
	v = pressV(v, tea.KeyEnter)
	if v.links.mode != linksClonePath || !strings.HasSuffix(v.links.where.value(), "/reading/upstream") || !strings.Contains(v.View(), "Enter clone") {
		t.Fatalf("clone path step: mode=%d where=%q\n%s", v.links.mode, v.links.where.value(), v.View())
	}
	v = pressV(v, tea.KeyEnter)
	if v.links.mode != linksChanges || !strings.Contains(v.View(), "How should claude-atlas land its changes there?") || !strings.Contains(v.links.status, "cloned upstream") {
		t.Fatalf("changes step: mode=%d status=%q err=%q\n%s", v.links.mode, v.links.status, v.links.err, v.View())
	}
	v = keyV(v, "c")
	if v.links.mode != linksList || !strings.Contains(v.links.status, "commits on the current branch") || !strings.Contains(v.View(), "changes: commit") {
		t.Fatalf("after choosing: mode=%d status=%q\n%s", v.links.mode, v.links.status, v.View())
	}
	projects, _, _ := tree.Walk(cfg.TreeRoot())
	p := tree.FindByRel(projects, "personal/reading")
	if _, err := os.Stat(filepath.Join(p.VaultPath(), "upstream", "hello.txt")); err != nil {
		t.Fatal("the clone should hold the upstream files")
	}
	pages, _, _ := links.Walk(cfg.AtlasVault)
	page := links.FindByPath(pages, filepath.Join(p.VaultPath(), "upstream"))
	if page == nil || page.Remote != url || page.Changes != "commit" {
		t.Fatalf("page %+v", page)
	}
	// The editor shows remote and changes; ←→ cycles the policy.
	v = keyV(v, "e")
	if !strings.Contains(v.View(), "Remote") || !strings.Contains(v.View(), "◂ commit ▸") {
		t.Fatalf("edit form:\n%s", v.View())
	}
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyRight)
	if !strings.Contains(v.View(), "◂ default: pr ▸") {
		t.Fatalf("cycle to default:\n%s", v.View())
	}
	v = keyV(v, "s")
	pages, _, _ = links.Walk(cfg.AtlasVault)
	if page := links.FindByPath(pages, filepath.Join(p.VaultPath(), "upstream")); page == nil || page.Changes != "" || page.Policy() != "pr" {
		t.Fatalf("after edit %+v", page)
	}
}
