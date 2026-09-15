package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func TestReposScreenMountsCreatesEditsAndUnlinks(t *testing.T) {
	cfg, _, v := atlasView(t)
	vaultPath := entryNamed(t, cfg, "reading").Path
	code := filepath.Join(t.TempDir(), "code")
	os.MkdirAll(code, 0o755)
	v = keyV(v, "l")
	if v.links == nil || !strings.Contains(v.View(), "no repositories yet") || !strings.Contains(v.View(), "reading   repositories") {
		t.Fatalf("l should open the repositories screen:\n%s", v.View())
	}
	// Mount a plain folder: refused in place, then a yes makes it a repository.
	v = keyV(v, "a")
	if v.links.mode != linksAdd || !strings.Contains(v.View(), "Enter mount") {
		t.Fatalf("add mode:\n%s", v.View())
	}
	v.links.source.setValue(filepath.Join(code, "missing"))
	v = pressV(v, tea.KeyEnter)
	if v.links.mode != linksAdd || v.links.err == "" {
		t.Fatalf("missing folder: mode=%d err=%q", v.links.mode, v.links.err)
	}
	v.links.source.setValue(code)
	v = pressV(v, tea.KeyEnter)
	if v.links.mode != linksConfirmInit || !strings.Contains(v.View(), "is not a git repository. Initialize one there") {
		t.Fatalf("init confirmation:\n%s", v.View())
	}
	v = keyV(v, "n")
	if v.links.mode != linksList || len(v.links.rows) != 0 {
		t.Fatal("n should mount nothing")
	}
	v = keyV(v, "a")
	v.links.source.setValue(code)
	v = pressV(v, tea.KeyEnter)
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	v = next.(view)
	if v.links.mode != linksList || len(v.links.rows) != 1 || v.links.rows[0].repo.Name != "code" || cmd == nil || !v.changed || !strings.Contains(v.links.status, "git repository now") {
		t.Fatalf("after init and mount: mode=%d rows=%+v status=%q err=%q", v.links.mode, v.links.rows, v.links.status, v.links.err)
	}
	if _, err := os.Stat(filepath.Join(code, ".git")); err != nil {
		t.Fatal("the folder should be a repository now")
	}
	out := v.View()
	for _, want := range []string{"╭", "code", "changes: commit", "not refreshed yet", "u unlink"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if repos := entryNamed(t, cfg, "reading").Repos; len(repos) != 1 || repos[0].Name != "code" || repos[0].Path != code {
		t.Fatalf("identity file: %+v", repos)
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
	if v.links.mode != linksNewPath || !strings.HasSuffix(v.links.where.value(), filepath.Join("reading", "repos", "paper")) {
		t.Fatalf("new path mode: %q\n%s", v.links.where.value(), v.View())
	}
	v = pressV(v, tea.KeyEnter)
	if v.links.mode != linksList || len(v.links.rows) != 2 || !strings.Contains(v.links.status, "created") {
		t.Fatalf("after create: mode=%d rows=%d status=%q err=%q", v.links.mode, len(v.links.rows), v.links.status, v.links.err)
	}
	if _, err := os.Stat(filepath.Join(vaultPath, "repos", "paper", ".git")); err != nil {
		t.Fatal("paper should be a repository in the vault")
	}
	// Edit: give the first repository a remote and choose how changes land.
	v = pressV(v, tea.KeyUp)
	v = keyV(v, "e")
	if v.links.mode != linksEdit || !strings.Contains(v.View(), "Edit code") || strings.Contains(v.View(), "Name") {
		t.Fatalf("edit mode:\n%s", v.View())
	}
	v = pressV(v, tea.KeyDown, tea.KeyEnter) // remote
	v.links.edit.text.SetValue("git@example.com:me/code.git")
	v = pressV(v, tea.KeyEnter)
	if !strings.Contains(v.View(), "s save") {
		t.Fatalf("edit form:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEsc)
	if v.links.mode != linksEdit || !strings.Contains(v.links.edit.err, "unsaved") {
		t.Fatalf("esc should warn first: mode=%d err=%q", v.links.mode, v.links.edit.err)
	}
	v = keyV(v, "s")
	if v.links.mode != linksList || v.links.err != "" {
		t.Fatalf("after edit: mode=%d err=%q", v.links.mode, v.links.edit.err)
	}
	repos := entryNamed(t, cfg, "reading").Repos
	if len(repos) != 2 || repos[0].Remote != "git@example.com:me/code.git" {
		t.Fatalf("remote not saved: %+v", repos)
	}
	// Unlink asks first; the folder stays.
	v = keyV(v, "u")
	if v.links.mode != linksConfirmUnlink || !strings.Contains(v.View(), "Unlink code from reading?") {
		t.Fatalf("confirm:\n%s", v.View())
	}
	v = keyV(v, "n")
	if v.links.mode != linksList || len(v.links.rows) != 2 {
		t.Fatal("n should keep the repository")
	}
	v = keyV(v, "u")
	v = keyV(v, "y")
	if len(v.links.rows) != 1 || !strings.Contains(v.links.status, "unlinked code") {
		t.Fatalf("after unlink: rows=%+v status=%q err=%q", v.links.rows, v.links.status, v.links.err)
	}
	if _, err := os.Stat(filepath.Join(code, ".git")); err != nil {
		t.Fatal("the folder stays")
	}
	v = pressV(v, tea.KeyEsc)
	if v.links != nil {
		t.Fatal("esc should close the screen")
	}
}

func TestReposScreenClonesAndAsksHowChangesLand(t *testing.T) {
	cfg, _, v := atlasView(t)
	upstream := filepath.Join(t.TempDir(), "upstream")
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
	if v.links.mode != linksClonePath || !strings.HasSuffix(v.links.where.value(), filepath.Join("reading", "repos", "upstream")) || !strings.Contains(v.View(), "Enter clone") {
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
	e := entryNamed(t, cfg, "reading")
	if _, err := os.Stat(filepath.Join(e.Path, "repos", "upstream", "hello.txt")); err != nil {
		t.Fatal("the clone should hold the upstream files")
	}
	if len(e.Repos) != 1 || e.Repos[0].Remote != url || e.Repos[0].Changes != "commit" {
		t.Fatalf("identity file: %+v", e.Repos)
	}
	// The editor shows the remote and cycles the policy back to the default.
	v = keyV(v, "e")
	if !strings.Contains(v.View(), "Remote") || !strings.Contains(v.View(), "◂ commit ▸") {
		t.Fatalf("edit form:\n%s", v.View())
	}
	v = pressV(v, tea.KeyDown, tea.KeyDown, tea.KeyRight)
	if !strings.Contains(v.View(), "◂ default: pr ▸") {
		t.Fatalf("cycle to default:\n%s", v.View())
	}
	v = keyV(v, "s")
	if repos := entryNamed(t, cfg, "reading").Repos; len(repos) != 1 || repos[0].Changes != "" {
		t.Fatalf("after edit %+v", repos)
	}
}

func TestReposScreenNeedsHooksAndAProject(t *testing.T) {
	none := keyV(pressV(newView(sample(), Opener{}, Hooks{}), tea.KeyDown), "l")
	if none.links != nil || !strings.Contains(none.errMsg, "not available") {
		t.Fatal("l without hooks reports why")
	}
	_, _, hooks := atlasFixture(t)
	kb := pressV(openView(t, hooks), tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown)
	if r := kb.current(); r == nil || r.item.Entry.Kind != vault.Knowledge {
		t.Fatalf("cursor on %+v", kb.current())
	}
	kb = keyV(kb, "l")
	if kb.links != nil || !strings.Contains(kb.errMsg, "knowledge base") {
		t.Fatalf("a knowledge base has no repositories: links=%v err=%q", kb.links, kb.errMsg)
	}
}

// A refresh can land while a confirmation is up and take the row away with it.
func TestUnlinkSurvivesAReloadThatDroppedTheRepository(t *testing.T) {
	entries := entriesOf(sample())
	removed := 0
	hooks := Hooks{
		Load:       func() ([]registry.Entry, error) { return entries, nil },
		Refresh:    func() error { return nil },
		AddRepo:    func(registry.Entry, string, bool) (vault.Repo, string, error) { return vault.Repo{}, "", nil },
		RemoveRepo: func(registry.Entry, string) error { removed++; return nil },
	}
	v := pressV(newView(sample(), Opener{}, hooks), tea.KeyDown, tea.KeyDown, tea.KeyDown) // p3
	v = keyV(v, "l")
	if v.links == nil || len(v.links.rows) != 1 {
		t.Fatalf("one repository to start: %+v", v.links)
	}
	v = keyV(v, "u")
	if v.links.mode != linksConfirmUnlink {
		t.Fatalf("mode %d", v.links.mode)
	}
	for i := range entries {
		if entries[i].Path == "/v/p3" {
			entries[i].Repos = nil
		}
	}
	next, _ := v.Update(refreshedMsg{})
	v = next.(view)
	if v.links == nil || len(v.links.rows) != 0 {
		t.Fatalf("the refresh takes the row away: %+v", v.links)
	}
	_ = v.View() // the prompt has no row to name and must still render
	v = keyV(v, "y")
	if v.links == nil || v.links.mode != linksList || removed != 0 || !strings.Contains(v.links.status, "the list changed") {
		t.Fatalf("y after the row is gone: mode=%d removed=%d status=%q", v.links.mode, removed, v.links.status)
	}
}
