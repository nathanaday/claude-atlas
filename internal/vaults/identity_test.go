package vaults

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var identityNow = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// fixtureEntries makes a project and a knowledge base under a temp vaults directory and
// returns the config, the home, and their scanned entries.
func fixtureEntries(t *testing.T) (*home.Config, home.Home, registry.Entry, registry.Entry) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := home.Home{Root: filepath.Join(root, "home")}
	cfg := &home.Config{Schema: home.ConfigSchema, VaultsDir: filepath.Join(root, "Vaults")}
	if _, err := vault.Init(filepath.Join(cfg.VaultsDir, "projects", "cs566"), vault.Options{Kind: vault.Project, Name: "cs566"}, identityNow); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Init(filepath.Join(cfg.VaultsDir, "knowledge", "ai-ml"), vault.Options{Kind: vault.Knowledge, Name: "ai-ml"}, identityNow); err != nil {
		t.Fatal(err)
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var project, kb registry.Entry
	for _, e := range ix.Entries {
		switch e.Kind {
		case vault.Project:
			project = e
		case vault.Knowledge:
			kb = e
		}
	}
	if project.ID == "" || kb.ID == "" {
		t.Fatalf("fixture missing entries: %+v", ix.Entries)
	}
	return cfg, h, project, kb
}

// refreshEntry re-scans and returns the entry named id, so a test sees what the last
// mutation wrote to the identity file.
func refreshEntry(t *testing.T, cfg *home.Config, id string) registry.Entry {
	t.Helper()
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e := ix.ByID(id)
	if e == nil {
		t.Fatalf("entry %s not found after rescan", id)
	}
	return *e
}

func lastCommitSubject(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%s").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %s", out)
	}
	return strings.TrimSpace(string(out))
}

func strPtr(s string) *string { return &s }

func TestPathForAndResolvePath(t *testing.T) {
	dir := filepath.FromSlash("/vaults")
	if got := PathFor(dir, vault.Knowledge, "ai-ml"); !strings.HasSuffix(got, filepath.Join("knowledge", "ai-ml")) {
		t.Fatalf("PathFor knowledge: %s", got)
	}
	if got, err := ResolvePath("cs566", dir, vault.Project); err != nil || !strings.HasSuffix(got, filepath.Join("projects", "cs566")) {
		t.Fatalf("ResolvePath bare name: %s, %v", got, err)
	}
	rel, err := ResolvePath("./x", dir, vault.Project)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(rel) != "x" || strings.HasPrefix(rel, dir) {
		t.Fatalf("ResolvePath relative: %s", rel)
	}
	tilde, err := ResolvePath("~/x", dir, vault.Project)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(tilde) != "x" || strings.HasPrefix(tilde, "~") {
		t.Fatalf("ResolvePath tilde: %s", tilde)
	}
}

func TestRegisterAndUnregister(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)

	// A vault under VaultsDir needs no entry.
	changed, err := Register(h, cfg, project.Path)
	if err != nil || changed {
		t.Fatalf("register inside: changed=%v err=%v", changed, err)
	}
	if len(cfg.Vaults) != 0 {
		t.Fatalf("cfg.Vaults should stay empty: %v", cfg.Vaults)
	}

	outside := t.TempDir()
	if _, err := vault.Init(outside, vault.Options{Kind: vault.Project, Name: "outside"}, identityNow); err != nil {
		t.Fatal(err)
	}
	changed, err = Register(h, cfg, outside)
	if err != nil || !changed {
		t.Fatalf("register outside: changed=%v err=%v", changed, err)
	}
	reloaded, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range reloaded.Vaults {
		if v == outside {
			found = true
		}
	}
	if !found {
		t.Fatalf("outside vault not saved: %v", reloaded.Vaults)
	}
	changed, err = Register(h, cfg, outside)
	if err != nil || changed {
		t.Fatalf("register again: changed=%v err=%v", changed, err)
	}

	if err := Unregister(h, cfg, outside); err != nil {
		t.Fatal(err)
	}
	reloaded, err = h.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range reloaded.Vaults {
		if v == outside {
			t.Fatalf("outside vault still registered: %v", reloaded.Vaults)
		}
	}

	if err := Unregister(h, cfg, project.Path); err == nil || !strings.Contains(err.Error(), "vaults directory") {
		t.Fatalf("unregister inside: %v", err)
	}
}

func TestEditIdentityByKind(t *testing.T) {
	_, _, project, kb := fixtureEntries(t)

	tags := []string{" usc ", "", "fall"}
	if err := EditIdentity(project, Edit{Tags: &tags}, identityNow); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(v.Config.Tags, ",") != "usc,fall" {
		t.Fatalf("tags: %v", v.Config.Tags)
	}
	if subject := lastCommitSubject(t, project.Path); subject != "setup: edit tags" {
		t.Fatalf("commit subject: %q", subject)
	}

	if err := EditIdentity(project, Edit{Scope: strPtr("s")}, identityNow); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("scope on a project: %v", err)
	}

	if err := EditIdentity(kb, Edit{Access: strPtr("sometimes")}, identityNow); err == nil {
		t.Fatal("expected an error for an invalid access value")
	}

	kbScope := "machine learning"
	kbAccess := vault.AccessGuarded
	if err := EditIdentity(kb, Edit{Scope: &kbScope, Access: &kbAccess}, identityNow); err != nil {
		t.Fatal(err)
	}
	v, err = vault.Open(kb.Path)
	if err != nil {
		t.Fatal(err)
	}
	if v.Config.Scope != kbScope || v.Config.Access != kbAccess {
		t.Fatalf("scope/access: %+v", v.Config)
	}
	// One edit is one operation, whatever it changes, and its summary names the fields.
	if subject := lastCommitSubject(t, kb.Path); subject != "setup: edit scope, access" {
		t.Fatalf("commit subject: %q", subject)
	}

	if err := EditIdentity(project, Edit{Name: "cs566-renamed"}, identityNow); err != nil {
		t.Fatal(err)
	}
	if err := EditIdentity(kb, Edit{Name: "ai-ml-renamed"}, identityNow); err != nil {
		t.Fatal(err)
	}
	v, err = vault.Open(project.Path)
	if err != nil || v.Config.Name != "cs566-renamed" {
		t.Fatalf("project name: %+v %v", v, err)
	}
	v, err = vault.Open(kb.Path)
	if err != nil || v.Config.Name != "ai-ml-renamed" {
		t.Fatalf("kb name: %+v %v", v, err)
	}
}

// TestAddRepoRefusesADuplicateFromAStaleEntry proves the identity file decides: the entry
// a caller holds may be older than the file it describes.
func TestAddRepoRefusesADuplicateFromAStaleEntry(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	first := filepath.Join(t.TempDir(), "docs")
	second := filepath.Join(t.TempDir(), "docs")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := AddRepo(h, cfg, project, first, true, identityNow); err != nil {
		t.Fatal(err)
	}
	// The same entry value again: its snapshot has no repositories, the file has one.
	if _, _, err := AddRepo(h, cfg, project, second, true, identityNow); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("stale entry: %v", err)
	}
	v, err := vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Repos) != 1 {
		t.Fatalf("the file should hold one repository: %+v", v.Config.Repos)
	}
	if cfg.RepoPath(project.ID, "docs") != first {
		t.Fatalf("the config should still point at the first folder: %q", cfg.RepoPath(project.ID, "docs"))
	}
}

// TestEditRepoRemoteAndPath covers the two edits beyond the change policy: the remote, and
// the folder the project reaches under that name.
func TestEditRepoRemoteAndPath(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	if _, _, err := CreateRepo(h, cfg, project, "hw", "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)

	url := "git@github.com:me/hw.git"
	updated, err := EditRepo(h, cfg, project, "hw", RepoEdit{Remote: &url}, identityNow)
	if err != nil || updated.Remote != url {
		t.Fatalf("set remote: %+v %v", updated, err)
	}
	updated, err = EditRepo(h, cfg, project, "hw", RepoEdit{Remote: strPtr("")}, identityNow)
	if err != nil || updated.Remote != "" {
		t.Fatalf("clear remote: %+v %v", updated, err)
	}
	v, err := vault.Open(project.Path)
	if err != nil || len(v.Config.Repos) != 1 || v.Config.Repos[0].Remote != "" {
		t.Fatalf("identity file: %+v %v", v.Config.Repos, err)
	}

	// Point the entry at a folder outside the vault: the config records the mapping.
	outside := filepath.Join(t.TempDir(), "hw")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", outside).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", out)
	}
	if _, err := EditRepo(h, cfg, project, "hw", RepoEdit{Path: outside}, identityNow); err != nil {
		t.Fatal(err)
	}
	if cfg.RepoPath(project.ID, "hw") != outside {
		t.Fatalf("mapping: %q", cfg.RepoPath(project.ID, "hw"))
	}
	reloaded, err := h.Load()
	if err != nil || reloaded.RepoPath(project.ID, "hw") != outside {
		t.Fatalf("mapping not saved: %+v %v", reloaded.Repos, err)
	}

	// Point it back at repos/<name>: the mapping goes away, since that is the default.
	if _, err := EditRepo(h, cfg, project, "hw", RepoEdit{Path: project.RepoDir("hw")}, identityNow); err != nil {
		t.Fatal(err)
	}
	if cfg.RepoPath(project.ID, "hw") != "" {
		t.Fatalf("mapping should be cleared: %q", cfg.RepoPath(project.ID, "hw"))
	}
	reloaded, err = h.Load()
	if err != nil || reloaded.RepoPath(project.ID, "hw") != "" {
		t.Fatalf("cleared mapping not saved: %+v %v", reloaded.Repos, err)
	}
}

func TestAddCreateCloneRemoveAndEditRepos(t *testing.T) {
	cfg, h, project, kb := fixtureEntries(t)

	// CreateRepo(e, "hw", "") makes <project>/repos/hw/.git, identity gains
	// {Name: "hw", Changes: ""}, no config entry.
	hw, hwPath, err := CreateRepo(h, cfg, project, "hw", "", identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(project.Path, "repos", "hw"); hwPath != want {
		t.Fatalf("create path: %s, want %s", hwPath, want)
	}
	if _, err := os.Stat(filepath.Join(hwPath, ".git")); err != nil {
		t.Fatalf("no .git: %v", err)
	}
	if hw.Name != "hw" || hw.Changes != "" || hw.Remote != "" {
		t.Fatalf("repo: %+v", hw)
	}
	v, err := vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Config.Repos) != 1 || v.Config.Repos[0].Name != "hw" {
		t.Fatalf("identity repos: %+v", v.Config.Repos)
	}
	if cfg.RepoPath(project.ID, "hw") != "" {
		t.Fatalf("hw should need no config entry: %q", cfg.RepoPath(project.ID, "hw"))
	}
	project = refreshEntry(t, cfg, project.ID)

	// AddRepo on a plain folder outside → NotRepoError.
	outsideParent := t.TempDir()
	outside := filepath.Join(outsideParent, "extern")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddRepo(h, cfg, project, outside, false, identityNow); err == nil {
		t.Fatal("expected an error")
	} else if _, ok := err.(*NotRepoError); !ok {
		t.Fatalf("expected NotRepoError, got %T: %v", err, err)
	}

	// with initGit → mounted, cfg.Repos[id/name] set and saved.
	extern, externPath, err := AddRepo(h, cfg, project, outside, true, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if extern.Name != "extern" || externPath != outside {
		t.Fatalf("add: %+v %s", extern, externPath)
	}
	if cfg.RepoPath(project.ID, "extern") != outside {
		t.Fatalf("config path not recorded: %q", cfg.RepoPath(project.ID, "extern"))
	}
	reloaded, err := h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.RepoPath(project.ID, "extern") != outside {
		t.Fatalf("config not saved: %q", reloaded.RepoPath(project.ID, "extern"))
	}
	project = refreshEntry(t, cfg, project.ID)

	// AddRepo of the same folder again → error "already".
	if _, _, err := AddRepo(h, cfg, project, outside, true, identityNow); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("re-add: %v", err)
	}

	// A folder inside the vault root not under repos/ → error "under repos/".
	insideNotRepos := filepath.Join(project.Path, "elsewhere")
	if err := os.Mkdir(insideNotRepos, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddRepo(h, cfg, project, insideNotRepos, false, identityNow); err == nil || !strings.Contains(err.Error(), "under repos/") {
		t.Fatalf("inside not repos/: %v", err)
	}

	// CloneRepo from a local bare repository into repos/ → remote recorded.
	bare := filepath.Join(t.TempDir(), "paper.git")
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %s", out)
	}
	paper, paperPath, err := CloneRepo(h, cfg, project, bare, "", identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if paper.Name != "paper" || paper.Remote != bare {
		t.Fatalf("clone: %+v", paper)
	}
	if want := filepath.Join(project.Path, "repos", "paper"); paperPath != want {
		t.Fatalf("clone path: %s, want %s", paperPath, want)
	}
	project = refreshEntry(t, cfg, project.ID)

	// EditRepo Changes: "commit" → written.
	updated, err := EditRepo(h, cfg, project, "hw", RepoEdit{Changes: strPtr("commit")}, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Changes != "commit" {
		t.Fatalf("edit changes: %+v", updated)
	}

	// Changes: "later" → error.
	if _, err := EditRepo(h, cfg, project, "hw", RepoEdit{Changes: strPtr("later")}, identityNow); err == nil {
		t.Fatal("expected an error for an invalid changes policy")
	}

	// A Path inside the vault but not under repos/ → error "under repos/", config
	// mapping unchanged.
	insideWiki := filepath.Join(project.Path, "wiki", "x")
	if err := os.MkdirAll(insideWiki, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", insideWiki).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", out)
	}
	hwPathBefore := cfg.RepoPath(project.ID, "hw")
	if _, err := EditRepo(h, cfg, project, "hw", RepoEdit{Path: insideWiki}, identityNow); err == nil || !strings.Contains(err.Error(), "under repos/") {
		t.Fatalf("edit path under wiki: %v", err)
	}
	if cfg.RepoPath(project.ID, "hw") != hwPathBefore {
		t.Fatalf("config mapping changed: %q, want %q", cfg.RepoPath(project.ID, "hw"), hwPathBefore)
	}

	// RemoveRepo("hw") → gone from identity, folder still exists.
	if err := RemoveRepo(h, cfg, project, "hw", identityNow); err != nil {
		t.Fatal(err)
	}
	v, err = vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range v.Config.Repos {
		if r.Name == "hw" {
			t.Fatalf("hw should be gone: %+v", v.Config.Repos)
		}
	}
	if _, err := os.Stat(filepath.Join(project.Path, "repos", "hw")); err != nil {
		t.Fatalf("hw folder should still exist: %v", err)
	}

	// RemoveRepo also clears a repository's config mapping: "extern" was added from
	// outside the vault, so it has one.
	if cfg.RepoPath(project.ID, "extern") == "" {
		t.Fatal("extern should have a config mapping before removal")
	}
	if err := RemoveRepo(h, cfg, project, "extern", identityNow); err != nil {
		t.Fatal(err)
	}
	v, err = vault.Open(project.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range v.Config.Repos {
		if r.Name == "extern" {
			t.Fatalf("extern should be gone: %+v", v.Config.Repos)
		}
	}
	reloaded, err = h.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.RepoPath(project.ID, "extern") != "" {
		t.Fatalf("extern config mapping should be cleared: %q", reloaded.RepoPath(project.ID, "extern"))
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("extern folder should still exist: %v", err)
	}

	// On the knowledge base entry every function errors with "knowledge base".
	if _, _, err := AddRepo(h, cfg, kb, outside, false, identityNow); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("add on kb: %v", err)
	}
	if _, _, err := CreateRepo(h, cfg, kb, "x", "", identityNow); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("create on kb: %v", err)
	}
	if _, _, err := CloneRepo(h, cfg, kb, bare, "", identityNow); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("clone on kb: %v", err)
	}
	if err := RemoveRepo(h, cfg, kb, "x", identityNow); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("remove on kb: %v", err)
	}
	if _, err := EditRepo(h, cfg, kb, "x", RepoEdit{}, identityNow); err == nil || !strings.Contains(err.Error(), "knowledge base") {
		t.Fatalf("edit on kb: %v", err)
	}
}
