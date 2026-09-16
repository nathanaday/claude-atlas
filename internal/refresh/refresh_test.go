package refresh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func fakeVault(t *testing.T, log, hot string, pages map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	os.MkdirAll(filepath.Join(root, "wiki"), 0o755)
	os.WriteFile(filepath.Join(root, ".claude-atlas.json"), []byte(`{"schema":"claude-atlas.vault.v2","id":"00000000-0000-4000-8000-000000000001","kind":"project","name":"v","mode":"generic"}`), 0o644)
	os.WriteFile(filepath.Join(root, "wiki", "log.md"), []byte(log), 0o644)
	os.WriteFile(filepath.Join(root, "wiki", "hot.md"), []byte(hot), 0o644)
	for name, text := range pages {
		os.WriteFile(filepath.Join(root, "wiki", name), []byte(text), 0o644)
	}
	return root
}

func TestHeat(t *testing.T) {
	for days, want := range map[int]string{0: "hot", 6: "hot", 7: "warm", 29: "warm", 30: "cold"} {
		d := days
		if got := Heat(&d, nil, 7); got != want {
			t.Errorf("%d days: got %s want %s", days, got, want)
		}
	}
	if Heat(nil, p(0), 7) != "" {
		t.Error("nil idleness should be unknown even when new")
	}
	if Heat(p(0), p(3), 7) != "new" || Heat(p(40), p(6), 7) != "new" {
		t.Error("a vault under 7 days old is new whatever its idleness")
	}
	if Heat(p(0), p(7), 7) != "hot" {
		t.Error("7 days old is no longer new")
	}
	if Heat(p(0), p(3), 1) != "hot" || Heat(p(0), p(0), 1) != "new" || Heat(p(0), p(0), 0) != "hot" {
		t.Error("the threshold is configurable; 0 turns new off")
	}
}

func p(v int) *int { return &v }

func TestCreatedDateComesFromTheIndexPage(t *testing.T) {
	root := fakeVault(t, "", "", map[string]string{
		"index.md":    "---\ntitle: Wiki Index\ncreated: 2026-09-01\nupdated: 2026-09-10\n---\n",
		"overview.md": "---\ncreated: 2020-01-01\n---\n",
	})
	got, ok := CreatedDate(root)
	if !ok || got.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := CreatedDate(fakeVault(t, "", "", nil)); ok {
		t.Fatal("no created field should report none")
	}
}

func TestNewestLogDate(t *testing.T) {
	root := fakeVault(t, "# Log\n\n## 2026-09-04 — a\n\n## 2026-09-10 — b\n\n## not-a-date\n", "", nil)
	got, ok := NewestLogDate(root)
	if !ok || got.Format("2006-01-02") != "2026-09-10" {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := NewestLogDate(filepath.Join(t.TempDir(), "missing")); ok {
		t.Fatal("missing vault should report no date")
	}
}

func TestActiveThreadsJoinsContinuationLines(t *testing.T) {
	root := fakeVault(t, "", "# R\n\n## Recent Changes\n\n- ignored\n\n## Active Threads\n\n- First thread\n  continues here.\n- Second\n\n## Later\n\n- no\n", nil)
	got := ActiveThreads(root)
	if strings.Join(got, "|") != "First thread continues here.|Second" {
		t.Fatalf("got %v", got)
	}
}

func TestPlainTextStripsWikilinks(t *testing.T) {
	if got := PlainText("See [[Spec]] and [[Long Name|alias]]."); got != "See Spec and alias." {
		t.Fatalf("got %q", got)
	}
}

func TestDeriveTakesLaterOfLogAndMtime(t *testing.T) {
	root := fakeVault(t, "## 2026-08-01 — old\n", "", nil)
	e := registry.Entry{Kind: vault.Project, Path: root}
	state := Derive(e, time.Now(), "t", 7)
	if state.LastOperation != "2026-08-01" || state.LastTouched != time.Now().Format("2006-01-02") {
		t.Fatalf("got %+v", state)
	}
	if state.DaysIdle == nil || *state.DaysIdle != 0 || state.Heat != "hot" {
		t.Fatalf("idle %v heat %s", state.DaysIdle, state.Heat)
	}
	if !state.VaultOK || state.Pages == nil || *state.Pages != 2 {
		t.Fatalf("got %+v", state)
	}
}

func TestRegistryDerivesEveryEntry(t *testing.T) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	cfg := &home.Config{VaultsDir: filepath.Join(root, "Vaults"), Heat: &home.HeatConfig{NewDays: 7}}
	kb := filepath.Join(cfg.VaultsDir, "knowledge", "ai-ml")
	p := filepath.Join(cfg.VaultsDir, "projects", "cs566")
	for path, opts := range map[string]vault.Options{kb: {Kind: vault.Knowledge, Name: "ai-ml"}, p: {Kind: vault.Project, Name: "cs566"}} {
		if _, err := vault.Init(path, opts, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(cfg.VaultsDir, "old", "wiki"), 0o755)
	os.WriteFile(filepath.Join(cfg.VaultsDir, "old", vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1"}`), 0o644)
	stateDir := filepath.Join(root, "state")
	entries, ix, _, err := Registry(cfg, stateDir, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || len(ix.Problems) != 1 {
		t.Fatalf("entries %d problems %+v", len(entries), ix.Problems)
	}
	for _, e := range entries {
		switch e.Name {
		case "ai-ml", "cs566":
			if e.State == nil || !e.State.VaultOK || e.State.Heat != "new" || e.State.Pages == nil || *e.State.Pages < 4 {
				t.Errorf("%s state %+v", e.Name, e.State)
			}
			if e.Kind == vault.Project && e.State.Tasks == nil {
				t.Errorf("project has a task summary: %+v", e.State)
			}
			if e.Kind == vault.Knowledge && e.State.Tasks != nil {
				t.Errorf("knowledge base has no tasks: %+v", e.State)
			}
		default:
			if e.State == nil || e.State.VaultOK || !strings.Contains(e.State.VaultError, "v1") {
				t.Errorf("v1 entry %+v", e)
			}
		}
	}
	read, _, err := registry.Read(stateDir)
	if err != nil || len(read) != 3 {
		t.Fatalf("registry file %v %d", err, len(read))
	}
}

func TestSignalsOverAnEntry(t *testing.T) {
	e := registry.Entry{Name: "p", Kind: vault.Project, Path: "/v/p",
		Mounts: []registry.Mount{{Name: "gone", Error: "no knowledge base with id x"}},
		Repos:  []registry.Repo{{Name: "lost", Error: "no folder; link it with claude-atlas link"}},
		State:  &registry.State{VaultOK: true, PendingRecovery: true, Tasks: &registry.TaskSummary{Open: []registry.TaskLine{{Title: "A", Status: "blocked"}, {Title: "B", Status: "active", Stale: true}}}},
	}
	got := strings.Join(Signals(e, time.Now()), "\n")
	for _, want := range []string{"interrupted", "gone", "lost", "1 blocked task: A", "1 stale task"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if got := Signals(registry.Entry{Name: "k", Error: "v1 vault"}, time.Now()); len(got) != 1 || !strings.Contains(got[0], "v1 vault") {
		t.Fatalf("error entry %v", got)
	}
}

// A mount the atlas resolved is only reachable through its symlink. Signals names the
// symlink that is gone or points somewhere else, and says nothing when it is right.
func TestSignalsNameAMissingSymlink(t *testing.T) {
	root := t.TempDir()
	wiki := filepath.Join(root, "ai-ml", "wiki")
	e := registry.Entry{Name: "p", Kind: vault.Project, Path: filepath.Join(root, "p"),
		Mounts: []registry.Mount{{ID: "k1", Name: "ai-ml", Access: vault.AccessWrite, Effective: vault.AccessWrite, Path: wiki}},
		State:  &registry.State{VaultOK: true},
	}
	link := e.KbDir("ai-ml")
	if got := strings.Join(Signals(e, time.Now()), "\n"); !strings.Contains(got, "symlink missing") {
		t.Fatalf("no symlink:\n%s", got)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere"), link); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(Signals(e, time.Now()), "\n"); !strings.Contains(got, "points elsewhere") {
		t.Fatalf("a symlink to another folder:\n%s", got)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wiki, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(wiki, link); err != nil {
		t.Fatal(err)
	}
	if got := Signals(e, time.Now()); len(got) != 0 {
		t.Fatalf("a symlink that is right needs no signal: %v", got)
	}
}

func TestSignalsNameAStaleGrant(t *testing.T) {
	e := registry.Entry{
		Kind:   vault.Knowledge,
		Name:   "ai-ml",
		Grants: []registry.Grant{{ID: "gone-0000", Name: "x", Access: "write", Error: "no project with id gone-0000"}},
		State:  &registry.State{VaultOK: true},
	}
	got := strings.Join(Signals(e, time.Now()), "\n")
	if !strings.Contains(got, "no project with id gone-0000") || !strings.Contains(got, "revoke") {
		t.Fatalf("a stale grant:\n%s", got)
	}
}
