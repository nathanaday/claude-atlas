package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

var testNow = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// makeVault creates one vault under the fixture's vaults directory.
func makeVault(t *testing.T, cfg *home.Config, kind vault.Kind, name string, tags []string) string {
	t.Helper()
	path := vaults.PathFor(cfg.VaultsDir, kind, name)
	if _, err := vault.Init(path, vault.Options{Kind: kind, Name: name}, testNow); err != nil {
		t.Fatal(err)
	}
	if len(tags) > 0 {
		if err := vaults.EditIdentity(registry.Entry{Path: path, Kind: kind}, vaults.Edit{Tags: &tags}, testNow); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// atlasFixture makes two projects and a knowledge base, and the hooks the screens use
// over them: every hook is the call the CLI makes.
func atlasFixture(t *testing.T) (*home.Config, home.Home, Hooks) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := &home.Config{Schema: home.ConfigSchema, VaultsDir: filepath.Join(root, "Vaults")}
	makeVault(t, cfg, vault.Project, "reading", []string{"personal"})
	makeVault(t, cfg, vault.Project, "welcome", nil)
	makeVault(t, cfg, vault.Knowledge, "ai-ml", nil)
	hooks := Hooks{
		Load: func() ([]registry.Entry, error) {
			ix, err := registry.Scan(cfg)
			if err != nil {
				return nil, err
			}
			return ix.Entries, nil
		},
		Refresh: func() error { _, _, _, err := refresh.Registry(h, cfg, h.StateDir(), testNow, false); return err },
		Edit: func(e registry.Entry, edit vaults.Edit) error {
			return vaults.EditIdentity(e, edit, testNow)
		},
		Unregister: func(e registry.Entry) error { return vaults.Unregister(h, cfg, e.Path) },
		AddRepo: func(e registry.Entry, target string, initGit bool) (vault.Repo, string, error) {
			return vaults.AddRepo(h, cfg, e, target, initGit, testNow)
		},
		NewRepo: func(e registry.Entry, name, at string) (vault.Repo, string, error) {
			return vaults.CreateRepo(h, cfg, e, name, at, testNow)
		},
		CloneRepo: func(e registry.Entry, url, at string) (vault.Repo, string, error) {
			return vaults.CloneRepo(h, cfg, e, url, at, testNow)
		},
		RemoveRepo: func(e registry.Entry, name string) error {
			return vaults.RemoveRepo(h, cfg, e, name, testNow)
		},
		EditRepo: func(e registry.Entry, name string, edit vaults.RepoEdit) (vault.Repo, error) {
			return vaults.EditRepo(h, cfg, e, name, edit, testNow)
		},
		Mount: func(project, kb registry.Entry, access, name string) (vault.Mount, error) {
			return vaults.Mount(project, kb, access, name, testNow)
		},
		Unmount: func(project registry.Entry, target string) error {
			return vaults.Unmount(project, target, testNow)
		},
		EditMount: func(project registry.Entry, target, access string) (vault.Mount, error) {
			return vaults.SetMountAccess(project, target, access, testNow)
		},
		Grant: func(kb, project registry.Entry, access string) error {
			return vaults.Grant(kb, project, access, testNow)
		},
		Revoke: func(kb registry.Entry, projectID string) error {
			return vaults.RevokeID(kb, projectID, testNow)
		},
		Sources: func(e registry.Entry) []string {
			v, err := vault.Open(e.Path)
			if err != nil {
				return nil
			}
			return capture.Sources(v)
		},
		Tasks: func(e registry.Entry) (tasks.Ledger, []string, error) {
			v, err := vault.Open(e.Path)
			if err != nil {
				return tasks.Ledger{}, nil, err
			}
			led, err := tasks.Current(v, testNow)
			return led, tasks.Notes(v), err
		},
		Plant:     func(registry.Entry, tasks.Plant) (txn.Planted, error) { return txn.Planted{}, nil },
		VaultsDir: cfg.VaultsDir,
	}
	return cfg, h, hooks
}

// openView loads the entries and shows the boards, cursor on the first vault.
func openView(t *testing.T, hooks Hooks) view {
	t.Helper()
	entries, err := hooks.Load()
	if err != nil {
		t.Fatal(err)
	}
	return newView(Items(entries), Opener{}, hooks)
}

// atlasView opens the boards over the fixture with the cursor on the project "reading".
func atlasView(t *testing.T) (*home.Config, home.Home, view) {
	t.Helper()
	cfg, h, hooks := atlasFixture(t)
	v := findVault(t, openView(t, hooks), "reading")
	if it := v.current(); it == nil || it.Entry.Name != "reading" {
		t.Fatalf("cursor on %+v", v.current())
	}
	return cfg, h, v
}

// entryNamed re-scans and returns one vault's entry.
func entryNamed(t *testing.T, cfg *home.Config, name string) registry.Entry {
	t.Helper()
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	found, err := ix.Find(name)
	if err != nil {
		t.Fatalf("find %s: %v", name, err)
	}
	return *found
}

func TestEditRenamesAndRetagsAProject(t *testing.T) {
	cfg, _, v := atlasView(t)
	v = keyV(v, "e")
	if v.edit == nil || v.edit.entry.Name != "reading" || !strings.Contains(v.View(), "projects/personal/reading") {
		t.Fatalf("editor not open:\n%s", v.View())
	}
	if strings.Contains(v.View(), "Scope") || strings.Contains(v.View(), "Access") {
		t.Fatalf("a project has no scope or access:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEnter) // name
	v = typeV(v, " List")
	v = pressV(v, tea.KeyEnter)
	if v.edit.draft.Name != "reading List" {
		t.Fatalf("name %q", v.edit.draft.Name)
	}
	v = pressV(v, tea.KeyDown, tea.KeyEnter) // tags
	v.edit.text.SetValue("usc, fall")
	v = pressV(v, tea.KeyEnter)
	if !v.edit.dirty() {
		t.Fatalf("draft %+v", v.edit.draft)
	}
	v = keyV(v, "s")
	if v.edit != nil || !v.changed || v.errMsg != "" || v.status != "saved reading List" {
		t.Fatalf("save: edit=%v changed=%v err=%q status=%q", v.edit, v.changed, v.errMsg, v.status)
	}
	e := entryNamed(t, cfg, "reading List")
	if strings.Join(e.Tags, ",") != "usc,fall" || e.Rel() != "projects/usc/reading List" {
		t.Fatalf("identity file: %+v", e)
	}
	if it := v.current(); it == nil || it.Entry.Path != e.Path {
		t.Fatalf("cursor should follow the vault: %+v", v.current())
	}
	if out := v.View(); !strings.Contains(out, "  usc\n") || !strings.Contains(out, "reading List") {
		t.Fatalf("tree not reloaded:\n%s", out)
	}
}

func TestEditScopeAndAccessOnAKnowledgeBase(t *testing.T) {
	cfg, _, v := atlasView(t)
	v = findVault(t, v, "ai-ml")
	if r := v.current(); r == nil || r.Entry.Kind != vault.Knowledge {
		t.Fatalf("cursor on %+v", v.current())
	}
	v = keyV(v, "e")
	if v.edit == nil || strings.Contains(v.View(), "Tags") {
		t.Fatalf("a knowledge base has scope and access, not tags:\n%s", v.View())
	}
	v = pressV(v, tea.KeyDown, tea.KeyEnter) // scope
	v = typeV(v, "Retrieval papers.")
	v = pressV(v, tea.KeyEnter)
	v = pressV(v, tea.KeyDown, tea.KeyRight) // access: open → guarded
	if v.edit.draft.Access != vault.AccessGuarded || !strings.Contains(v.View(), "guarded") {
		t.Fatalf("access %q\n%s", v.edit.draft.Access, v.View())
	}
	v = keyV(v, "s")
	if v.edit != nil || v.errMsg != "" {
		t.Fatalf("save: edit=%v err=%q", v.edit, v.errMsg)
	}
	e := entryNamed(t, cfg, "ai-ml")
	if e.Scope != "Retrieval papers." || e.Access != vault.AccessGuarded {
		t.Fatalf("identity file: %+v", e)
	}
}

func TestEscWarnsBeforeDiscarding(t *testing.T) {
	_, _, v := atlasView(t)
	v = keyV(v, "e")
	v = pressV(v, tea.KeyDown, tea.KeyEnter) // tags
	v = typeV(v, "x")
	v = pressV(v, tea.KeyEnter, tea.KeyEsc)
	if v.edit == nil || !strings.Contains(v.edit.err, "unsaved") {
		t.Fatalf("first esc should warn: %+v", v.edit)
	}
	v = pressV(v, tea.KeyEsc)
	if v.edit != nil || v.changed {
		t.Fatalf("second esc should discard: edit=%v changed=%v", v.edit, v.changed)
	}
}

func TestQuitKeyTypesInsideAField(t *testing.T) {
	_, _, v := atlasView(t)
	v = keyV(v, "e")
	v = pressV(v, tea.KeyEnter) // name field
	v = keyV(v, "q")
	if v.edit == nil || !strings.HasSuffix(v.edit.text.Value(), "q") {
		t.Fatalf("q should be typed, not quit: %+v", v.edit)
	}
}

func TestUnregisterFromTheEditor(t *testing.T) {
	cfg, h, v := atlasView(t)
	v = keyV(v, "e")
	v = keyV(v, "r")
	if v.edit.mode != confirmRemove || !strings.Contains(v.View(), "stays on disk") {
		t.Fatalf("mode %d\n%s", v.edit.mode, v.View())
	}
	v = keyV(v, "n")
	if v.edit == nil || v.edit.mode != editFields {
		t.Fatal("n should keep the vault")
	}
	// A vault inside the vaults directory cannot be forgotten; the editor says why.
	v = keyV(v, "r")
	v = keyV(v, "y")
	if v.edit == nil || !strings.Contains(v.edit.err, "vaults directory") {
		t.Fatalf("inside the vaults directory: edit=%v err=%q", v.edit, v.edit.err)
	}
	if len(v.items) != 3 {
		t.Fatalf("nothing should be forgotten: %d vaults", len(v.items))
	}
	// One registered from outside is forgotten, and its folder stays.
	outside := filepath.Join(t.TempDir(), "outside")
	if _, err := vault.Init(outside, vault.Options{Kind: vault.Project, Name: "outside"}, testNow); err != nil {
		t.Fatal(err)
	}
	cfg.AddVault(outside)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	w := findVault(t, openView(t, v.hooks), "outside")
	if r := w.current(); r == nil || r.Entry.Name != "outside" {
		t.Fatalf("cursor on %+v", w.current())
	}
	w = keyV(w, "e")
	w = keyV(w, "r")
	w = keyV(w, "y")
	if w.edit != nil || !w.changed || !strings.Contains(w.status, "outside") || len(w.items) != 3 {
		t.Fatalf("forget: edit=%v status=%q vaults=%d", w.edit, w.status, len(w.items))
	}
	if _, err := os.Stat(filepath.Join(outside, ".claude-atlas.json")); err != nil {
		t.Fatal("the vault was deleted")
	}
}

func TestEditFromAnExpandedVaultKeepsItExpanded(t *testing.T) {
	_, _, v := atlasView(t)
	path := v.current().Entry.Path
	v = pressV(v, tea.KeyEnter)
	if !v.boards[0].expanded[path] {
		t.Fatal("enter should expand")
	}
	v = keyV(v, "e")
	v = pressV(v, tea.KeyEsc)
	if v.edit != nil || !v.boards[0].expanded[path] || v.current().Entry.Name != "reading" {
		t.Fatalf("esc should return to the expanded vault: edit=%v expanded=%v", v.edit, v.boards[0].expanded)
	}
	v = keyV(v, "e")
	v = pressV(v, tea.KeyEnter) // name
	v = typeV(v, " 2")
	v = pressV(v, tea.KeyEnter)
	v = keyV(v, "s")
	if v.edit != nil || v.current() == nil || v.current().Entry.Name != "reading 2" || !v.boards[0].expanded[path] || !strings.Contains(v.View(), "reading 2") {
		t.Fatalf("a saved edit keeps the vault expanded under the cursor: current=%+v", v.current())
	}
}

func TestEditNeedsHooks(t *testing.T) {
	v := newView(sample(), Opener{}, Hooks{})
	v = keyV(v, "e")
	if v.edit != nil || !strings.Contains(v.errMsg, "not available") {
		t.Fatalf("edit without hooks: %+v %q", v.edit, v.errMsg)
	}
}

// The CLI's Load reads the registry file, which only a refresh rewrites, so a write
// shows after the refresh that follows it. The cursor waits on the vault it wrote.
func TestASaveRefreshesAndKeepsTheCursor(t *testing.T) {
	entries := entriesOf(sample())
	var edited vaults.Edit
	hooks := Hooks{
		Load: func() ([]registry.Entry, error) { return entries, nil },
		Edit: func(e registry.Entry, edit vaults.Edit) error { edited = edit; return nil },
		Refresh: func() error {
			for i := range entries {
				if entries[i].Path == "/v/p3" && edited.Name != "" {
					entries[i].Name = edited.Name
				}
			}
			return nil
		},
	}
	v := findVault(t, newView(Items(entries), Opener{}, hooks), "p3")
	v = keyV(v, "e")
	v = pressV(v, tea.KeyEnter)
	v.edit.text.SetValue("p3 again")
	v = pressV(v, tea.KeyEnter)
	next, cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	v = next.(view)
	if cmd == nil || v.focus != "/v/p3" || edited.Name != "p3 again" {
		t.Fatalf("save should start a refresh: focus=%q edit=%+v", v.focus, edited)
	}
	if r := v.current(); r == nil || r.Entry.Name != "p3" {
		t.Fatalf("the registry has not been rewritten yet: %+v", r)
	}
	v = runCmd(v, cmd)
	if r := v.current(); r == nil || r.Entry.Name != "p3 again" {
		t.Fatalf("the cursor should land on the renamed vault: %+v", r)
	}
	if v.focus != "" {
		t.Fatalf("the focus is spent: %q", v.focus)
	}
}
