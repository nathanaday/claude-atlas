package vaults

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// relocateFixture is fixtureEntries plus the paths the tests need: the atlas home, the
// config, and a target beside the vaults directory.
func relocateFixture(t *testing.T) (*home.Config, home.Home, string) {
	t.Helper()
	cfg, h, project, _ := fixtureEntries(t)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg, h, filepath.Join(filepath.Dir(filepath.Dir(project.Path)), "..", "Moved")
}

func TestPlanRelocateCountsTheTreeAndNamesTheRewrite(t *testing.T) {
	cfg, h, _ := relocateFixture(t)
	to := filepath.Join(t.TempDir(), "Moved")
	p, err := PlanRelocate(h, cfg, to)
	if err != nil {
		t.Fatal(err)
	}
	if p.From != cfg.VaultsDir || p.To != to {
		t.Fatalf("plan should carry both roots, got %q -> %q", p.From, p.To)
	}
	if p.Vaults != 2 {
		t.Fatalf("the fixture holds 2 vaults, plan found %d", p.Vaults)
	}
	if p.Files == 0 || p.Bytes == 0 {
		t.Fatalf("the walk should count files and bytes, got %d files, %d bytes", p.Files, p.Bytes)
	}
	if len(p.Rewrites) != 1 || p.Rewrites[0].What != "vaults dir" || p.Rewrites[0].To != to {
		t.Fatalf("the vaults dir is the first rewrite, got %+v", p.Rewrites)
	}
}

func TestPlanRelocateTakesAnEmptyTargetAndRefusesAFullOne(t *testing.T) {
	cfg, h, _ := relocateFixture(t)
	empty := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanRelocate(h, cfg, empty); err != nil {
		t.Fatalf("an empty folder is a usable target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(empty, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PlanRelocate(h, cfg, empty)
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("a folder holding files should refuse the move: %v", err)
	}
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanRelocate(h, cfg, file); err == nil {
		t.Fatal("a file cannot be the target")
	}
}

func TestPlanRelocateRefusesATargetThatOverlapsTheVaultsDirectory(t *testing.T) {
	cfg, h, _ := relocateFixture(t)
	for _, tc := range []struct{ name, to string }{
		{"inside", filepath.Join(cfg.VaultsDir, "inner")},
		{"itself", cfg.VaultsDir},
		{"above", filepath.Dir(cfg.VaultsDir)},
	} {
		if _, err := PlanRelocate(h, cfg, tc.to); err == nil {
			t.Fatalf("%s: a target overlapping the vaults directory should refuse", tc.name)
		}
	}
}

func TestPlanRelocateRefusesATargetInsideAVault(t *testing.T) {
	cfg, h, _ := relocateFixture(t)
	outside := filepath.Join(t.TempDir(), "side")
	if _, err := vault.Init(outside, vault.Options{Kind: vault.Project, Name: "side"}, identityNow); err != nil {
		t.Fatal(err)
	}
	_, err := PlanRelocate(h, cfg, filepath.Join(outside, "Vaults"))
	if err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Fatalf("a target inside a vault should refuse: %v", err)
	}
}

func TestPlanRelocateRefusesAVaultWithAnOperationInFlight(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(project.Path, vault.MetaDir)
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meta, "inflight.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := PlanRelocate(h, cfg, filepath.Join(t.TempDir(), "Moved"))
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("a vault mid-operation should refuse the move: %v", err)
	}
}

func TestPlanRelocateRewritesTheRecordedPathsUnderTheOldRoot(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	// A repository recorded outside its project but still under the vaults directory,
	// and a vault registered elsewhere, which the move must leave alone.
	inside := filepath.Join(cfg.VaultsDir, "code", "shared")
	cfg.SetRepoPath(project.ID, "shared", inside)
	elsewhere := filepath.Join(t.TempDir(), "side")
	if _, err := vault.Init(elsewhere, vault.Options{Kind: vault.Project, Name: "side"}, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(h, cfg, elsewhere); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "Moved")
	p, err := PlanRelocate(h, cfg, to)
	if err != nil {
		t.Fatal(err)
	}
	var repo *Rewrite
	for i := range p.Rewrites {
		if strings.Contains(p.Rewrites[i].What, "shared") {
			repo = &p.Rewrites[i]
		}
		if p.Rewrites[i].From == elsewhere {
			t.Fatalf("a vault outside the vaults directory does not move: %+v", p.Rewrites[i])
		}
	}
	if repo == nil {
		t.Fatalf("the repository under the old root should be rewritten, got %+v", p.Rewrites)
	}
	if want := filepath.Join(to, "code", "shared"); repo.To != want {
		t.Fatalf("repo rewrite %q, want %q", repo.To, want)
	}
}

func TestApplyRelocateMovesTheTreeAndRewritesTheConfig(t *testing.T) {
	cfg, h, project, kb := fixtureEntries(t)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	from := cfg.VaultsDir
	to := filepath.Join(t.TempDir(), "Moved")
	p, err := PlanRelocate(h, cfg, to)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyRelocate(h, cfg, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("the old root should be gone: %v", err)
	}
	for _, e := range []string{project.Path, kb.Path} {
		rel, _ := filepath.Rel(from, e)
		moved := filepath.Join(to, rel)
		v, err := vault.Open(moved)
		if err != nil {
			t.Fatalf("the vault should open at %s: %v", moved, err)
		}
		if v.Config.ID == "" {
			t.Fatalf("the identity file should survive the move: %+v", v.Config)
		}
	}
	saved, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.VaultsDir != to {
		t.Fatalf("the saved config should point at %q, got %q", to, saved.VaultsDir)
	}
	if cfg.VaultsDir != to {
		t.Fatalf("the in-memory config should follow too, got %q", cfg.VaultsDir)
	}
}

func TestApplyRelocateSwapsThePrefixOfEveryRecordedPath(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	inside := filepath.Join(cfg.VaultsDir, "code", "shared")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.SetRepoPath(project.ID, "shared", inside)
	elsewhere := filepath.Join(t.TempDir(), "side")
	if _, err := vault.Init(elsewhere, vault.Options{Kind: vault.Project, Name: "side"}, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(h, cfg, elsewhere); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "Moved")
	p, err := PlanRelocate(h, cfg, to)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyRelocate(h, cfg, p); err != nil {
		t.Fatal(err)
	}
	saved, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(to, "code", "shared"); saved.RepoPath(project.ID, "shared") != want {
		t.Fatalf("repo path %q, want %q", saved.RepoPath(project.ID, "shared"), want)
	}
	if len(saved.Vaults) != 1 || saved.Vaults[0] != elsewhere {
		t.Fatalf("the vault outside the tree stays where it is, got %v", saved.Vaults)
	}
}

func TestApplyRelocateCopiesAndVerifiesWhenTheVolumeDiffers(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	from := cfg.VaultsDir
	to := filepath.Join(t.TempDir(), "Moved")
	p, err := PlanRelocate(h, cfg, to)
	if err != nil {
		t.Fatal(err)
	}
	// One volume in the test, but the copy path is what a second volume takes.
	p.SameVolume = false
	if err := ApplyRelocate(h, cfg, p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("the copy removes the old root once it verifies: %v", err)
	}
	rel, _ := filepath.Rel(from, project.Path)
	if _, err := vault.Open(filepath.Join(to, rel)); err != nil {
		t.Fatalf("the copied vault should open: %v", err)
	}
	// The copy keeps the git repository, so the vault's history survives.
	if _, err := os.Stat(filepath.Join(to, rel, ".git")); err != nil {
		t.Fatalf("the copy should carry .git: %v", err)
	}
}

func TestApplyRelocateRollsBackWhenTheConfigCannotBeSaved(t *testing.T) {
	cfg, h, _, _ := fixtureEntries(t)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	from := cfg.VaultsDir
	to := filepath.Join(t.TempDir(), "Moved")
	p, err := PlanRelocate(h, cfg, to)
	if err != nil {
		t.Fatal(err)
	}
	// A file where the atlas home is makes the save fail after the move.
	if err := os.RemoveAll(h.Root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.Root, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyRelocate(h, cfg, p); err == nil {
		t.Fatal("the save should fail")
	}
	if _, err := os.Stat(from); err != nil {
		t.Fatalf("a failed save puts the tree back: %v", err)
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Fatalf("nothing should be left at the target: %v", err)
	}
	if cfg.VaultsDir != from {
		t.Fatalf("the in-memory config should be unchanged, got %q", cfg.VaultsDir)
	}
}

func TestPlanRelocateWarnsAboutStateThatHoldsTheOldPath(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	repos := filepath.Join(project.Path, "repos", "code")
	for _, f := range []string{
		filepath.Join(repos, ".venv", "pyvenv.cfg"),
		filepath.Join(repos, "build", "CMakeCache.txt"),
		filepath.Join(repos, "web", "node_modules", "left-pad", "index.js"),
		filepath.Join(repos, "web", "node_modules", "left-pad", "node_modules", "inner", "index.js"),
	} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A worktree or submodule keeps its git directory elsewhere, by absolute path.
	if err := os.WriteFile(filepath.Join(repos, ".git"), []byte("gitdir: /somewhere/else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := PlanRelocate(h, cfg, filepath.Join(t.TempDir(), "Moved"))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, w := range p.Warnings {
		found[w.Kind] = w.Path
	}
	for kind, want := range map[string]string{
		WarnVenv:     filepath.Join(repos, ".venv"),
		WarnCMake:    filepath.Join(repos, "build"),
		WarnPackages: filepath.Join(repos, "web", "node_modules"),
		// A worktree's warning names the folder, not its .git file: that is where the
		// user runs the repair.
		WarnWorktree: repos,
	} {
		if found[kind] != want {
			t.Fatalf("the plan should warn %s about %s, got %q in %+v", kind, want, found[kind], p.Warnings)
		}
	}
	groups := p.GroupWarnings()
	if len(groups) != 4 || groups[0].Kind != WarnWorktree {
		t.Fatalf("the groups come in a fixed order, got %+v", groups)
	}
	// The nested node_modules is inside one already named; one warning is enough.
	nested := 0
	for _, w := range p.Warnings {
		if strings.Contains(w.Path, filepath.Join("left-pad", "node_modules")) {
			nested++
		}
	}
	if nested != 0 {
		t.Fatalf("a node_modules inside another should not warn again, got %+v", p.Warnings)
	}
}

func TestApplyRelocateCopiesWhenARenameOutOfAManagedFolderIsRefused(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	if err := h.Save(cfg); err != nil {
		t.Fatal(err)
	}
	from := cfg.VaultsDir
	to := filepath.Join(t.TempDir(), "Moved")
	p, err := PlanRelocate(h, cfg, to)
	if err != nil {
		t.Fatal(err)
	}
	if !p.SameVolume {
		t.Skip("the test's temporary folders are on two volumes")
	}
	// An iCloud container reports one device and still refuses the rename.
	calls := 0
	renameTree = func(oldpath, newpath string) error {
		calls++
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
	}
	defer func() { renameTree = os.Rename }()

	if err := ApplyRelocate(h, cfg, p); err != nil {
		t.Fatalf("a refused rename should fall back to a copy: %v", err)
	}
	if calls != 1 {
		t.Fatalf("the rename should be tried once, got %d", calls)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("the copy removes the old root once it verifies: %v", err)
	}
	rel, _ := filepath.Rel(from, project.Path)
	if _, err := vault.Open(filepath.Join(to, rel)); err != nil {
		t.Fatalf("the copied vault should open: %v", err)
	}
	if saved, err := h.Load(); err != nil || saved.VaultsDir != to {
		t.Fatalf("the config should point at %q: %+v %v", to, saved, err)
	}
}
