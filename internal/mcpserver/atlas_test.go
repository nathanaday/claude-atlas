package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

func TestAtlasReadsWithoutWritingAndRefreshWrites(t *testing.T) {
	bare := home.Home{Root: filepath.Join(t.TempDir(), "home")}
	if msg := connectIn(t, bare, t.TempDir()).call("atlas", map[string]any{}, nil); !strings.Contains(msg, "no atlas") {
		t.Fatalf("without an atlas the tool says so: %q", msg)
	}
	a := newAtlas(t, false)
	c := a.inProject(t)
	var out AtlasOut
	if msg := c.call("atlas", map[string]any{}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Knowledge) != 1 || len(out.Projects) != 1 || out.Settings.VaultsDir != a.cfg.VaultsDir || out.Settings.NewDays != home.DefaultNewDays {
		t.Fatalf("atlas: %+v", out)
	}
	if out.Projects[0].Knowledge == nil || out.Projects[0].Knowledge.Path != a.kb.Root || len(out.Knowledge[0].Projects) != 1 {
		t.Fatalf("the project resolves to its knowledge base and back: %+v", out)
	}
	for _, e := range append(out.Knowledge, out.Projects...) {
		if e.State == nil {
			t.Fatalf("every entry carries its state: %+v", e)
		}
	}
	if _, err := os.Stat(registry.File(a.h.StateDir())); !os.IsNotExist(err) {
		t.Fatalf("a plain read writes no registry: %v", err)
	}
	if msg := c.call("atlas", map[string]any{"refresh": true}, &out); msg != "" {
		t.Fatal(msg)
	}
	if _, err := os.Stat(registry.File(a.h.StateDir())); err != nil {
		t.Fatalf("refresh writes the registry: %v", err)
	}
}

func TestVaultCreateAdoptEditForget(t *testing.T) {
	a := newAtlas(t, false)
	c := connectIn(t, a.h, t.TempDir())
	var out VaultToolOut
	if msg := c.call("vault", map[string]any{"action": "create", "name": "papers", "scope": "Papers."}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Vault == nil || out.Vault.Kind != registry.Knowledge || out.Vault.Path != filepath.Join(a.cfg.VaultsDir, "papers") || out.Vault.Scope != "Papers." {
		t.Fatalf("create: %+v", out.Vault)
	}
	if msg := c.call("vault", map[string]any{"action": "create", "name": "papers"}, nil); !strings.Contains(msg, "already exists") {
		t.Fatalf("taken path: %q", msg)
	}
	if msg := c.call("vault", map[string]any{"action": "create"}, nil); !strings.Contains(msg, "needs name") {
		t.Fatalf("no name: %q", msg)
	}
	if msg := c.call("vault", map[string]any{"action": "grow", "name": "x"}, nil); !strings.Contains(msg, "action must be") {
		t.Fatalf("unknown action: %q", msg)
	}
	// Adopt an Obsidian folder outside the vaults directory; it lands in the config.
	outside := filepath.Join(t.TempDir(), "notes")
	os.MkdirAll(filepath.Join(outside, ".obsidian"), 0o755)
	out = VaultToolOut{}
	if msg := c.call("vault", map[string]any{"action": "adopt", "path": outside, "scope": "Notes."}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Vault == nil || out.Vault.Name != "notes" || out.Vault.Scope != "Notes." {
		t.Fatalf("adopt: %+v", out.Vault)
	}
	cfg, _ := a.h.Load()
	if len(cfg.Knowledge) != 1 || cfg.Knowledge[0] != outside {
		t.Fatalf("an adopted vault outside the vaults directory is listed: %+v", cfg.Knowledge)
	}
	// Edit renames the folder and the scope.
	scope := "Everything."
	out = VaultToolOut{}
	if msg := c.call("vault", map[string]any{"action": "edit", "target": "notes", "name": "Notebook", "scope": scope}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Vault.Name != "Notebook" || out.Vault.Scope != scope || filepath.Base(out.Vault.Path) != "Notebook" {
		t.Fatalf("edit: %+v", out.Vault)
	}
	if msg := c.call("vault", map[string]any{"action": "edit", "target": "Notebook"}, nil); !strings.Contains(msg, "needs name or scope") {
		t.Fatalf("empty edit: %q", msg)
	}
	// Forget drops it from the config; the folder stays. One under the vaults
	// directory cannot be forgotten.
	out = VaultToolOut{}
	if msg := c.call("vault", map[string]any{"action": "forget", "target": "Notebook"}, &out); msg != "" || out.Forgotten == "" {
		t.Fatalf("forget: %q %+v", msg, out)
	}
	if _, err := os.Stat(out.Forgotten); err != nil {
		t.Fatal("the folder stays")
	}
	if msg := c.call("vault", map[string]any{"action": "forget", "target": "papers"}, nil); !strings.Contains(msg, "inside the vaults directory") {
		t.Fatalf("forget under the vaults directory: %q", msg)
	}
}

func TestProjectInitFromAPlainFolder(t *testing.T) {
	a := newAtlas(t, false)
	work := filepath.Join(t.TempDir(), "thesis")
	os.MkdirAll(work, 0o755)
	// The session sits in the folder, with no place yet; init makes it a project.
	c := connectIn(t, a.h, work)
	if msg := c.call("status", nil, nil); !strings.Contains(msg, "not in a claude-atlas") {
		t.Fatalf("no place before init: %q", msg)
	}
	var out ProjectToolOut
	if msg := c.call("project", map[string]any{"action": "init", "description": "My thesis.", "knowledge": "kb"}, &out); msg != "" {
		t.Fatal(msg)
	}
	if out.Project == nil || out.Project.Name != "thesis" || out.Project.Path != work || out.Project.Description != "My thesis." || out.Project.Knowledge == nil || out.Project.Knowledge.Path != a.kb.Root || len(out.Written) == 0 {
		t.Fatalf("init: %+v", out)
	}
	if !project.IsProject(work) {
		t.Fatal("atlas/project.json is there")
	}
	cfg, _ := a.h.Load()
	if !cfg.HasProject(work) {
		t.Fatalf("the config lists the project: %+v", cfg.Projects)
	}
	var st Status
	if msg := c.call("status", nil, &st); msg != "" || st.Kind != "project" || st.Name != "thesis" {
		t.Fatalf("the same session is now a project session: %q %+v", msg, st)
	}
	if msg := c.call("project", map[string]any{"action": "init"}, nil); !strings.Contains(msg, "already") {
		t.Fatalf("init twice: %q", msg)
	}
	os.MkdirAll(filepath.Join(work, "chapter"), 0o755)
	if msg := c.call("project", map[string]any{"action": "init", "work": filepath.Join(work, "chapter")}, nil); !strings.Contains(msg, "inside the project") {
		t.Fatalf("a project inside a project: %q", msg)
	}
	if msg := c.call("project", map[string]any{"action": "init", "work": t.TempDir(), "knowledge": "nope"}, nil); !strings.Contains(msg, "no such") {
		t.Fatalf("an unknown knowledge base: %q", msg)
	}
}

func TestProjectLinkUnlinkEditForget(t *testing.T) {
	a := newAtlas(t, false)
	c := a.inProject(t)
	var out ProjectToolOut
	if msg := c.call("project", map[string]any{"action": "unlink"}, &out); msg != "" || out.Project.Knowledge != nil {
		t.Fatalf("unlink: %q %+v", msg, out.Project)
	}
	if msg := c.call("project", map[string]any{"action": "unlink"}, nil); !strings.Contains(msg, "uses no knowledge base") {
		t.Fatalf("unlink twice: %q", msg)
	}
	if msg := c.call("project", map[string]any{"action": "link"}, nil); !strings.Contains(msg, "needs knowledge") {
		t.Fatalf("link without a knowledge base: %q", msg)
	}
	out = ProjectToolOut{}
	if msg := c.call("project", map[string]any{"action": "link", "knowledge": a.kb.Root}, &out); msg != "" || out.Project.Knowledge == nil || out.Project.Knowledge.Name != "kb" {
		t.Fatalf("link by path: %q %+v", msg, out.Project)
	}
	desc := "The web app."
	out = ProjectToolOut{}
	if msg := c.call("project", map[string]any{"action": "edit", "name": "Web App", "description": desc}, &out); msg != "" || out.Project.Name != "Web App" || out.Project.Description != desc {
		t.Fatalf("edit: %q %+v", msg, out.Project)
	}
	if msg := c.call("project", map[string]any{"action": "edit"}, nil); !strings.Contains(msg, "needs name or description") {
		t.Fatalf("empty edit: %q", msg)
	}
	// From elsewhere, the project is named with work.
	k := a.inKnowledge(t)
	if msg := k.call("project", map[string]any{"action": "edit", "name": "x"}, nil); !strings.Contains(msg, "name the project") {
		t.Fatalf("no project in a knowledge base session: %q", msg)
	}
	out = ProjectToolOut{}
	if msg := k.call("project", map[string]any{"action": "forget", "work": "Web App"}, &out); msg != "" || out.Forgotten != a.work {
		t.Fatalf("forget by name: %q %+v", msg, out)
	}
	if project.IsProject(a.work) == false {
		t.Fatal("the folder and its atlas/ stay")
	}
	cfg, _ := a.h.Load()
	if cfg.HasProject(a.work) {
		t.Fatal("the config no longer lists the project")
	}
}

func TestSettings(t *testing.T) {
	a := newAtlas(t, false)
	c := a.inProject(t)
	var s Settings
	if msg := c.call("settings", nil, &s); msg != "" || s.NewDays != home.DefaultNewDays || s.VaultsDir != a.cfg.VaultsDir {
		t.Fatalf("read: %q %+v", msg, s)
	}
	if msg := c.call("settings", map[string]any{"new_days": 3}, &s); msg != "" || s.NewDays != 3 {
		t.Fatalf("set: %q %+v", msg, s)
	}
	cfg, _ := a.h.Load()
	if cfg.NewDays() != 3 {
		t.Fatalf("saved: %d", cfg.NewDays())
	}
	if msg := c.call("settings", map[string]any{"new_days": -1}, nil); !strings.Contains(msg, "0 or more") {
		t.Fatalf("negative: %q", msg)
	}
	if _, err := os.Stat(registry.File(a.h.StateDir())); err != nil {
		t.Fatal("a write rewrites the registry")
	}
}
