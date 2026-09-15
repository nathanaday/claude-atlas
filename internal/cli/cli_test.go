package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tui"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

type harness struct {
	t    *testing.T
	home string
	out  bytes.Buffer
	err  bytes.Buffer
}

func (h *harness) run(args ...string) int {
	h.out.Reset()
	h.err.Reset()
	c := console.NewWith(true, strings.NewReader(""), &h.out, false)
	return run(append([]string{"--home", h.home}, args...), strings.NewReader(""), &h.out, &h.err, c)
}

func setup(t *testing.T) (*harness, string) {
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	h := &harness{t: t, home: filepath.Join(root, "home")}
	vaults := filepath.Join(root, "Vaults")
	code := h.run("setup", "--no-plugin", "--vaults-dir", vaults, "--first-vault", "welcome")
	if code != 0 {
		t.Fatalf("setup exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	return h, vaults
}

// project is where a project vault of that name goes by default.
func project(vaults, name string) string { return filepath.Join(vaults, "projects", name) }

func (h *harness) config(t *testing.T) *home.Config {
	t.Helper()
	cfg, err := home.Home{Root: h.home}.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestSetupCreatesHomeAndFirstProject(t *testing.T) {
	h, vaults := setup(t)
	if !strings.Contains(h.out.String(), "Setup complete.") {
		t.Fatalf("output:\n%s", h.out.String())
	}
	for _, path := range []string{
		filepath.Join(project(vaults, "welcome"), ".claude-atlas.json"),
		filepath.Join(project(vaults, "welcome"), ".git", "HEAD"),
		filepath.Join(h.home, "config.json"),
		filepath.Join(h.home, "state", "registry.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s", path)
		}
	}
	identity, _ := os.ReadFile(filepath.Join(project(vaults, "welcome"), ".claude-atlas.json"))
	if !strings.Contains(string(identity), `"kind": "project"`) {
		t.Fatalf("identity file:\n%s", identity)
	}
	cfg, _ := os.ReadFile(filepath.Join(h.home, "config.json"))
	if !strings.Contains(string(cfg), `"schema": "claude-atlas.config.v2"`) || strings.Contains(string(cfg), "atlas_vault") {
		t.Fatalf("config.json:\n%s", cfg)
	}
	reg, _ := os.ReadFile(registry.File(filepath.Join(h.home, "state")))
	if !strings.Contains(string(reg), `"name": "welcome"`) {
		t.Fatalf("registry.json:\n%s", reg)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(h.home), "Atlas")); err == nil {
		t.Fatal("setup must not create an atlas vault")
	}
	if code := h.run("setup", "--no-plugin"); code != 0 || !strings.Contains(h.out.String(), "keep       1 found") {
		t.Fatalf("rerun exit %d:\n%s", code, h.out.String())
	}
}

// setup names a vault it cannot read instead of counting it as one that works.
func TestSetupNamesAV1Vault(t *testing.T) {
	h, vaults := setup(t)
	legacy := project(vaults, "legacy")
	os.MkdirAll(legacy, 0o755)
	os.WriteFile(filepath.Join(legacy, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	code := h.run("setup", "--no-plugin")
	out := h.out.String()
	if code != 0 || !strings.Contains(out, "keep       1 found, 1 need adopting") {
		t.Fatalf("setup exit %d:\n%s%s", code, out, h.err.String())
	}
	if !strings.Contains(out, "✗ vault") || !strings.Contains(out, legacy) || !strings.Contains(out, "v1 vault") {
		t.Fatalf("setup should name the v1 vault:\n%s", out)
	}
	if !strings.Contains(out, "refreshed        1 vault\n") {
		t.Fatalf("the count is of the vaults that work:\n%s", out)
	}
}

func TestNewProjectNewKnowledgeAndAdoptAs(t *testing.T) {
	h, vaults := setup(t)
	if code := h.run("new-project", "cs566", "--tags", "usc,fall"); code != 0 {
		t.Fatalf("new-project exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	v, err := vault.Open(project(vaults, "cs566"))
	if err != nil || v.Config.Kind != vault.Project || strings.Join(v.Config.Tags, ",") != "usc,fall" {
		t.Fatalf("cs566: %+v %v", v, err)
	}
	if code := h.run("new-knowledge", "ai-ml", "--scope", "ML."); code != 0 {
		t.Fatalf("new-knowledge exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	kb, err := vault.Open(filepath.Join(vaults, "knowledge", "ai-ml"))
	if err != nil || kb.Config.Kind != vault.Knowledge || kb.Config.Scope != "ML." || kb.Config.Access != vault.AccessOpen {
		t.Fatalf("ai-ml: %+v %v", kb, err)
	}
	if code := h.run("new-knowledge", "x", "--access", "sometimes"); code != 2 {
		t.Fatalf("bad access exit %d %s", code, h.err.String())
	}
	if code := h.run("repos", "ai-ml"); code != 0 || !strings.Contains(h.out.String(), "a knowledge base has no repositories; mount it in a project instead") || strings.Contains(h.out.String(), "claude-atlas link") {
		t.Fatalf("repos on a knowledge base: exit %d\n%s", code, h.out.String())
	}
	outside := filepath.Join(t.TempDir(), "scratch")
	if code := h.run("new-project", outside); code != 0 {
		t.Fatalf("new-project PATH exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if cfg := h.config(t); len(cfg.Vaults) != 1 || cfg.Vaults[0] != outside {
		t.Fatalf("a vault outside the vaults directory is registered: %+v", cfg.Vaults)
	}
	if code := h.run("list"); code != 0 || !strings.Contains(h.out.String(), "scratch") {
		t.Fatalf("list exit %d:\n%s", code, h.out.String())
	}
	old := filepath.Join(t.TempDir(), "old")
	os.MkdirAll(filepath.Join(old, "wiki"), 0o755)
	os.WriteFile(filepath.Join(old, "wiki", "index.md"), []byte("---\ntitle: I\n---\n# I\n"), 0o644)
	if code := h.run("adopt", old, "--as", "knowledge"); code != 0 || !strings.Contains(h.out.String(), "as knowledge base") {
		t.Fatalf("adopt exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	adopted, err := vault.Open(old)
	if err != nil || adopted.Config.Kind != vault.Knowledge {
		t.Fatalf("adopted %+v %v", adopted, err)
	}
	if cfg := h.config(t); len(cfg.Vaults) != 2 {
		t.Fatalf("adopt should register the vault: %+v", cfg.Vaults)
	}
	if code := h.run("adopt", old, "--as", "project"); code != 1 || !strings.Contains(h.err.String(), "does not change") {
		t.Fatalf("kind is fixed: exit %d %s", code, h.err.String())
	}
}

func TestListShowEditRemove(t *testing.T) {
	h, vaults := setup(t)
	if code := h.run("list"); code != 0 || !strings.Contains(h.out.String(), "project   new     welcome") {
		t.Fatalf("list exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("show", "welcome"); code != 0 {
		t.Fatalf("show exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	for _, want := range []string{"Kind", "Id", "Path", "Heat", project(vaults, "welcome")} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("show missing %q:\n%s", want, h.out.String())
		}
	}
	if code := h.run("edit", "welcome", "--tags", "a,b"); code != 0 {
		t.Fatalf("edit exit %d %s", code, h.err.String())
	}
	if code := h.run("show", "welcome"); code != 0 || !strings.Contains(h.out.String(), "a, b") {
		t.Fatalf("tags: exit %d\n%s", code, h.out.String())
	}
	if code := h.run("edit", "welcome", "--scope", "x"); code != 1 || !strings.Contains(h.err.String(), "knowledge base") {
		t.Fatalf("scope on a project: exit %d %s", code, h.err.String())
	}
	if code := h.run("remove", "welcome"); code != 1 || !strings.Contains(h.err.String(), "vaults directory") {
		t.Fatalf("remove inside the vaults directory: exit %d %s", code, h.err.String())
	}
	outside := filepath.Join(t.TempDir(), "scratch")
	if code := h.run("new-project", outside); code != 0 {
		t.Fatalf("new-project exit %d %s", code, h.err.String())
	}
	if code := h.run("remove", "scratch"); code != 0 || !strings.Contains(h.out.String(), "removed") {
		t.Fatalf("remove exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if cfg := h.config(t); len(cfg.Vaults) != 0 {
		t.Fatalf("remove should forget the vault: %+v", cfg.Vaults)
	}
	if code := h.run("list"); code != 0 || strings.Contains(h.out.String(), "scratch") {
		t.Fatalf("list still shows it:\n%s", h.out.String())
	}
	if _, err := os.Stat(filepath.Join(outside, ".claude-atlas.json")); err != nil {
		t.Fatal("remove must leave the vault on disk")
	}
}

func TestRepoCommands(t *testing.T) {
	h, vaults := setup(t)
	welcome := project(vaults, "welcome")
	docs := filepath.Join(filepath.Dir(vaults), "docs")
	os.MkdirAll(docs, 0o755)
	os.WriteFile(filepath.Join(docs, "a.pdf"), []byte("x"), 0o644)
	if code := h.run("link", "welcome", docs); code != 1 || !strings.Contains(h.err.String(), "not a git repository") {
		t.Fatalf("plain folder: %d %s", code, h.err.String())
	}
	if code := h.run("link", "welcome", docs, "--init"); code != 0 || !strings.Contains(h.out.String(), "linked") {
		t.Fatalf("link --init exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(docs, ".git")); err != nil {
		t.Fatal("--init should make the folder a repository")
	}
	id := ""
	if v, err := vault.Open(welcome); err == nil {
		id = v.Config.ID
	}
	if got := h.config(t).RepoPath(id, "docs"); got != docs {
		t.Fatalf("the config should record a repository outside the project: %q", got)
	}
	if code := h.run("repos", "welcome"); code != 0 || !strings.Contains(h.out.String(), "docs") || !strings.Contains(h.out.String(), "changes: commit") {
		t.Fatalf("repos exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("link", "welcome", docs); code != 1 || !strings.Contains(h.err.String(), "already") {
		t.Fatalf("duplicate: %d %s", code, h.err.String())
	}
	if code := h.run("new-repo", "welcome", "paper"); code != 0 {
		t.Fatalf("new-repo exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(welcome, "repos", "paper", ".git")); err != nil {
		t.Fatal("paper should be a repository under repos/")
	}
	if got := h.config(t).RepoPath(id, "paper"); got != "" {
		t.Fatalf("a repository under repos/ needs no config entry, got %q", got)
	}
	bare := filepath.Join(t.TempDir(), "upstream.git")
	if err := exec.Command("git", "init", "--bare", bare).Run(); err != nil {
		t.Fatal(err)
	}
	url := "file://" + bare
	if code := h.run("link", "welcome", url); code != 0 || !strings.Contains(h.out.String(), "cloning") {
		t.Fatalf("clone exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(welcome, "repos", "upstream")); err != nil {
		t.Fatal("the clone lands under repos/")
	}
	if code := h.run("repos", "welcome"); code != 0 || !strings.Contains(h.out.String(), "changes: pr") || !strings.Contains(h.out.String(), url) {
		t.Fatalf("a clone records its remote and lands pull requests:\n%s", h.out.String())
	}
	if code := h.run("edit-repo", "welcome", "upstream", "--changes", "commit"); code != 0 {
		t.Fatalf("edit-repo exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("repos", "welcome"); code != 0 || strings.Contains(h.out.String(), "changes: pr") {
		t.Fatalf("after edit-repo:\n%s", h.out.String())
	}
	if code := h.run("edit-repo", "welcome", "upstream", "--changes", "later"); code != 2 {
		t.Fatalf("bad policy exit %d", code)
	}
	if code := h.run("unlink", "welcome", "paper"); code != 0 {
		t.Fatalf("unlink exit %d %s", code, h.err.String())
	}
	if code := h.run("repos", "welcome"); code != 0 || strings.Contains(h.out.String(), "paper") {
		t.Fatalf("paper should be gone:\n%s", h.out.String())
	}
	if _, err := os.Stat(filepath.Join(welcome, "repos", "paper", ".git")); err != nil {
		t.Fatal("unlink must leave the folder")
	}
	if code := h.run("repos"); code != 0 || !strings.Contains(h.out.String(), "welcome") || !strings.Contains(h.out.String(), "docs") {
		t.Fatalf("every project's repositories: exit %d\n%s", code, h.out.String())
	}

	// --changes settles the policy on the spot; nothing is asked.
	notes := filepath.Join(filepath.Dir(vaults), "notes")
	os.MkdirAll(notes, 0o755)
	os.WriteFile(filepath.Join(notes, "a.md"), []byte("x"), 0o644)
	if code := h.run("link", "welcome", notes, "--init", "--changes", "commit"); code != 0 || strings.Contains(h.out.String(), "How should claude-atlas land") {
		t.Fatalf("link --changes exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("repos", "welcome"); code != 0 || !strings.Contains(repoLineFor(h.out.String(), "notes"), "changes: commit") {
		t.Fatalf("link --changes records the policy:\n%s", h.out.String())
	}

	// edit-repo --remote records a remote, and "" clears it.
	remote := "https://example.com/notes.git"
	if code := h.run("edit-repo", "welcome", "notes", "--remote", remote); code != 0 {
		t.Fatalf("edit-repo --remote exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("repos", "welcome"); code != 0 || !strings.Contains(repoLineFor(h.out.String(), "notes"), "remote "+remote) {
		t.Fatalf("the remote should show:\n%s", h.out.String())
	}
	if code := h.run("edit-repo", "welcome", "notes", "--remote", ""); code != 0 {
		t.Fatalf("clear remote exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("repos", "welcome"); code != 0 || strings.Contains(h.out.String(), remote) {
		t.Fatalf("the remote should be gone:\n%s", h.out.String())
	}
}

// repoLineFor is the line `repos` printed for one repository.
func repoLineFor(out, name string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), name+" ") {
			return line
		}
	}
	return ""
}

func TestRefreshAndDoctorReportProblems(t *testing.T) {
	h, vaults := setup(t)
	legacy := project(vaults, "legacy")
	os.MkdirAll(legacy, 0o755)
	os.WriteFile(filepath.Join(legacy, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	if code := h.run("refresh"); code != 0 || !strings.Contains(h.out.String(), "✗") || !strings.Contains(h.out.String(), "legacy") {
		t.Fatalf("refresh exit %d:\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("list"); code != 0 || !strings.Contains(h.out.String(), "?         v1      legacy") {
		t.Fatalf("list exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "v1      legacy") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	os.RemoveAll(legacy)
	err := vault.UpdateConfig(project(vaults, "welcome"), "test mount", time.Now(), func(c *vault.Config) error {
		c.Mounts = append(c.Mounts, vault.Mount{ID: "00000000-0000-4000-8000-000000000000", Name: "ghost", Access: vault.AccessRead})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "ghost") || !strings.Contains(h.out.String(), "no knowledge base with id") {
		t.Fatalf("an unresolved mount: exit %d\n%s", code, h.out.String())
	}
}

// A registered vault whose folder is gone is still something the atlas knows: refresh,
// list, and doctor all name it, and remove forgets it.
func TestDoctorAndRemoveSeeAMissingRegisteredVault(t *testing.T) {
	h, _ := setup(t)
	outside := filepath.Join(t.TempDir(), "scratch")
	if code := h.run("new-project", outside); code != 0 {
		t.Fatalf("new-project exit %d %s", code, h.err.String())
	}
	if err := os.RemoveAll(outside); err != nil {
		t.Fatal(err)
	}
	if code := h.run("refresh"); code != 0 || !strings.Contains(h.out.String(), "✗") || !strings.Contains(h.out.String(), "scratch") {
		t.Fatalf("refresh exit %d:\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("list"); code != 0 || !strings.Contains(h.out.String(), "?         missing scratch") {
		t.Fatalf("list exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "missing scratch") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("remove", outside); code != 0 || !strings.Contains(h.out.String(), "removed") {
		t.Fatalf("remove exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if cfg := h.config(t); len(cfg.Vaults) != 0 {
		t.Fatalf("remove should forget a vault whose folder is gone: %+v", cfg.Vaults)
	}
	if code := h.run("list"); code != 0 || strings.Contains(h.out.String(), "scratch") {
		t.Fatalf("list still shows it:\n%s", h.out.String())
	}
	if code := h.run("doctor"); code != 0 {
		t.Fatalf("doctor after the removal: exit %d\n%s", code, h.out.String())
	}
}

// A registered folder that exists but is not a vault has no entry to hang on, so doctor
// and info report the scan's own problem instead of passing over it.
func TestDoctorAndInfoReportAProblemWithNoEntry(t *testing.T) {
	h, _ := setup(t)
	outside := filepath.Join(t.TempDir(), "scratch")
	if code := h.run("new-project", outside); code != 0 {
		t.Fatalf("new-project exit %d %s", code, h.err.String())
	}
	os.Remove(filepath.Join(outside, vault.Marker))
	os.RemoveAll(filepath.Join(outside, "wiki"))
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "scratch") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("info"); code != 0 || !strings.Contains(h.out.String(), "scratch") {
		t.Fatalf("info exit %d:\n%s", code, h.out.String())
	}
}

func TestOpenVaultResolvesNamesAndPaths(t *testing.T) {
	h, vaults := setup(t)
	ix, err := registry.Scan(h.config(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, label, err := resolveVault(ix, "welcome"); err != nil || got != project(vaults, "welcome") || label != "welcome" {
		t.Fatalf("name: %s %s %v", got, label, err)
	}
	if got, _, err := resolveVault(ix, vaults); err != nil || got != vaults {
		t.Fatalf("path: %s %v", got, err)
	}
	if _, _, err := resolveVault(ix, "nope"); err == nil {
		t.Fatal("unknown name should fail")
	}
}

// The view reaches the backend only through these hooks; this is the wiring the screens
// get, over a real atlas.
func TestViewHooksCreateEditAndForget(t *testing.T) {
	h, dir := setup(t)
	cfg := h.config(t)
	e := &env{
		home:    home.Home{Root: h.home},
		console: console.NewWith(true, strings.NewReader(""), &h.out, false),
		stdin:   strings.NewReader(""),
		stdout:  &h.out,
		stderr:  &h.err,
	}
	hooks := e.hooks(cfg)
	path := project(dir, "ghost")
	got, err := hooks.Create(tui.AddVault{Kind: vault.Project, Name: "ghost", Path: path, Mode: "generic", Tags: []string{"usc"}})
	if err != nil || got != path {
		t.Fatalf("create: %q %v", got, err)
	}
	if err := hooks.Refresh(); err != nil {
		t.Fatal(err)
	}
	entries, err := hooks.Load()
	if err != nil {
		t.Fatal(err)
	}
	var ghost registry.Entry
	for _, en := range entries {
		if en.Path == path {
			ghost = en
		}
	}
	if ghost.Name != "ghost" || ghost.Rel() != "projects/usc/ghost" || ghost.State == nil {
		t.Fatalf("the registry should carry the new project with its state: %+v", ghost)
	}
	if err := hooks.Edit(ghost, vaults.Edit{Name: "Ghost"}); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(path)
	if err != nil || v.Config.Name != "Ghost" {
		t.Fatalf("identity file: %+v %v", v.Config, err)
	}
	// A vault inside the vaults directory cannot be forgotten; one outside can.
	if err := hooks.Unregister(ghost); err == nil || !strings.Contains(err.Error(), "vaults directory") {
		t.Fatalf("forget inside: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if _, err := hooks.Create(tui.AddVault{Kind: vault.Knowledge, Name: "outside", Path: outside, Mode: "generic", Scope: "Papers."}); err != nil {
		t.Fatal(err)
	}
	if cfg := h.config(t); len(cfg.Vaults) != 1 || cfg.Vaults[0] != outside {
		t.Fatalf("a vault outside the vaults directory is registered: %+v", cfg.Vaults)
	}
	kb, err := vault.Open(outside)
	if err != nil || kb.Config.Kind != vault.Knowledge || kb.Config.Scope != "Papers." {
		t.Fatalf("knowledge base: %+v %v", kb.Config, err)
	}
	if err := hooks.Unregister(registry.Entry{Path: outside}); err != nil {
		t.Fatalf("forget outside: %v", err)
	}
	if cfg := h.config(t); len(cfg.Vaults) != 0 {
		t.Fatalf("still registered: %+v", cfg.Vaults)
	}
}

func TestCommandsNeedSetupFirst(t *testing.T) {
	h := &harness{t: t, home: filepath.Join(t.TempDir(), "none")}
	if code := h.run("refresh"); code != 1 || !strings.Contains(h.err.String(), "claude-atlas setup") {
		t.Fatalf("exit %d err %s", code, h.err.String())
	}
	if code := h.run("info"); code != 0 || !strings.Contains(h.out.String(), "not set up") {
		t.Fatalf("info before setup: %d %s", code, h.out.String())
	}
	if code := h.run("new-project"); code != 2 {
		t.Fatalf("new-project with no name and no terminal should be a usage error, got %d", code)
	}
	if code := h.run("new-knowledge"); code != 2 {
		t.Fatalf("new-knowledge needs a name, got %d", code)
	}
	if code := h.run("bogus"); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
	if code := h.run("version"); code != 0 || strings.TrimSpace(h.out.String()) != Version {
		t.Fatalf("version: %q", h.out.String())
	}
}

func TestIngestStagesNewFilesAndRemembersTheFolder(t *testing.T) {
	h, vaults := setup(t)
	welcome := project(vaults, "welcome")
	src := filepath.Join(filepath.Dir(vaults), "Papers")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.md"), []byte("aaa"), 0o644)
	if code := h.run("ingest", "welcome"); code != 1 || !strings.Contains(h.err.String(), "has not ingested from a folder yet") {
		t.Fatalf("no sources: exit %d err %s", code, h.err.String())
	}
	if code := h.run("ingest", "welcome", src, "--dry-run"); code != 0 || !strings.Contains(h.out.String(), "new        Papers/a.md") {
		t.Fatalf("dry run exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(welcome, "inbox", "Papers", "a.md")); err == nil {
		t.Fatal("dry run must not stage")
	}
	if code := h.run("ingest", "welcome", src, "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "staged") || !strings.Contains(h.out.String(), "remembered") || !strings.Contains(h.out.String(), "wiki-ingest") {
		t.Fatalf("ingest exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if data, _ := os.ReadFile(filepath.Join(welcome, "inbox", "Papers", "a.md")); string(data) != "aaa" {
		t.Fatal("file not staged")
	}
	if code := h.run("repos", "welcome"); code != 0 || !strings.Contains(h.out.String(), "no repositories") {
		t.Fatalf("ingesting from a folder does not mount it:\n%s", h.out.String())
	}
	// With no path, the remembered folder is the source; nothing is new, but the staged file still waits.
	if code := h.run("ingest", "welcome", "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "nothing new to stage; 1 file already waiting") {
		t.Fatalf("second ingest exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	os.WriteFile(filepath.Join(src, "b.md"), []byte("bbb"), 0o644)
	if code := h.run("ingest", "welcome", "--no-claude"); code != 0 || !strings.Contains(h.out.String(), "new        Papers/b.md") || strings.Contains(h.out.String(), "new        Papers/a.md") {
		t.Fatalf("third ingest exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("new-knowledge", "ai-ml"); code != 0 {
		t.Fatalf("new-knowledge exit %d %s", code, h.err.String())
	}
	if code := h.run("ingest", "ai-ml", src); code != 1 || !strings.Contains(h.err.String(), "knowledge enters through a project") {
		t.Fatalf("a knowledge base takes no sources: exit %d %s", code, h.err.String())
	}
}

func TestTaskCommands(t *testing.T) {
	h, vaults := setup(t)
	welcome := project(vaults, "welcome")
	if code := h.run("tasks"); code != 0 || !strings.Contains(h.out.String(), "no open tasks in any project") {
		t.Fatalf("tasks exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("plant", "welcome", "Fix", "the", "dialog", "--priority", "high"); code != 0 || !strings.Contains(h.out.String(), "planted") || !strings.Contains(h.out.String(), "wiki/tasks/Fix the dialog.md") {
		t.Fatalf("plant exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if _, err := os.Stat(filepath.Join(welcome, "wiki", "tasks", "Fix the dialog.md")); err != nil {
		t.Fatal("page missing")
	}
	if code := h.run("tasks", "welcome"); code != 0 || !strings.Contains(h.out.String(), "planted   high     Fix the dialog") {
		t.Fatalf("tasks welcome exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("tasks"); code != 0 || !strings.Contains(h.out.String(), "welcome\n") || !strings.Contains(h.out.String(), "Fix the dialog") {
		t.Fatalf("all tasks:\n%s", h.out.String())
	}
	if code := h.run("plant", "welcome"); code != 2 {
		t.Fatalf("plant without text exit %d", code)
	}
	if code := h.run("plant", "welcome", "x", "--priority", "urgent"); code != 1 {
		t.Fatalf("bad priority exit %d %s", code, h.err.String())
	}
	// An older vault gains the scratch folder and the snippet through upgrade, and its task
	// index moves to its current path; an appearance file it already has keeps its settings
	// and gains the snippet.
	os.RemoveAll(filepath.Join(welcome, "ideas"))
	os.Rename(filepath.Join(welcome, "wiki", "tasks", "tasks.md"), filepath.Join(welcome, "wiki", "tasks", "index.md"))
	os.Remove(filepath.Join(welcome, ".obsidian", "snippets", "claude-atlas.css"))
	os.WriteFile(filepath.Join(welcome, ".obsidian", "appearance.json"), []byte(`{"baseFontSize": 15, "enabledCssSnippets": ["vault-colors"]}`), 0o644)
	if code := h.run("upgrade", "welcome"); code != 0 || !strings.Contains(h.out.String(), "added .obsidian/appearance.json, .obsidian/snippets/claude-atlas.css, ideas/.gitkeep; moved wiki/tasks/index.md to wiki/tasks/tasks.md") || !strings.Contains(h.out.String(), "reload Obsidian") {
		t.Fatalf("upgrade exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	appearance, _ := os.ReadFile(filepath.Join(welcome, ".obsidian", "appearance.json"))
	if !strings.Contains(string(appearance), `"baseFontSize": 15`) || !strings.Contains(string(appearance), `"vault-colors"`) || !strings.Contains(string(appearance), `"claude-atlas"`) {
		t.Fatalf("appearance should keep its settings and enable the snippet:\n%s", appearance)
	}
	if code := h.run("upgrade", "--all"); code != 0 || !strings.Contains(h.out.String(), "current") {
		t.Fatalf("upgrade --all exit %d\n%s", code, h.out.String())
	}
}

func TestAV1VaultIsNamedByDoctorAndUpgrade(t *testing.T) {
	h, vaults := setup(t)
	welcome := project(vaults, "welcome")
	os.WriteFile(filepath.Join(welcome, vault.Marker), []byte(`{"schema":"claude-atlas.vault.v1","mode":"generic"}`), 0o644)
	if code := h.run("doctor"); code != 1 || !strings.Contains(h.out.String(), "v1      welcome") {
		t.Fatalf("doctor exit %d:\n%s", code, h.out.String())
	}
	if code := h.run("upgrade", "--all"); code != 0 || !strings.Contains(h.out.String(), "v1 vault; adopt it") {
		t.Fatalf("upgrade --all exit %d:\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("upgrade", "welcome"); code != 1 || !strings.Contains(h.err.String(), "adopt") {
		t.Fatalf("one v1 vault still fails: exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
}

func TestBareCommandOpensTheTreeOrExplains(t *testing.T) {
	// Piped: the usage, as before.
	var out, errOut bytes.Buffer
	c := console.NewWith(false, strings.NewReader(""), &out, false)
	if code := run([]string{"--home", filepath.Join(t.TempDir(), "none")}, strings.NewReader(""), &out, &errOut, c); code != 0 || !strings.Contains(out.String(), "Usage:") || strings.Contains(out.String(), "No atlas yet") {
		t.Fatalf("piped: %d\n%s", code, out.String())
	}
	// A terminal with no atlas: the usage and the missing step.
	out.Reset()
	c = console.NewWith(false, strings.NewReader(""), &out, true)
	if code := run([]string{"--home", filepath.Join(t.TempDir(), "none")}, strings.NewReader(""), &out, &errOut, c); code != 0 || !strings.Contains(out.String(), "claude-atlas                       open the view") || !strings.Contains(out.String(), "No atlas yet; run `claude-atlas setup`") {
		t.Fatalf("no atlas: %d\n%s", code, out.String())
	}
	// help still prints the usage whatever the terminal.
	out.Reset()
	if code := run([]string{"help"}, strings.NewReader(""), &out, &errOut, c); code != 0 || !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("help: %d", code)
	}
}

func TestConfigNewDays(t *testing.T) {
	h, _ := setup(t)
	if code := h.run("config"); code != 0 || !strings.Contains(h.out.String(), "new-days           7") {
		t.Fatalf("config exit %d\n%s", code, h.out.String())
	}
	if code := h.run("config", "new-days", "1"); code != 0 || !strings.Contains(h.out.String(), "refreshed") {
		t.Fatalf("set exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if cfg := h.config(t); cfg.NewDays() != 1 {
		t.Fatalf("saved %+v", cfg.Heat)
	}
	for _, bad := range [][]string{{"config", "new-days", "-1"}, {"config", "new-days", "soon"}, {"config", "hot-days", "3"}, {"config", "new-days"}} {
		if code := h.run(bad...); code != 2 {
			t.Fatalf("%v exit %d", bad, code)
		}
	}
	if code := h.run("config", "new-days", "0"); code != 0 {
		t.Fatalf("zero exit %d %s", code, h.err.String())
	}
	if code := h.run("list"); code != 0 || strings.Contains(h.out.String(), "new  ") {
		t.Fatalf("with 0, a fresh vault is not new:\n%s", h.out.String())
	}
}

func TestStubCommand(t *testing.T) {
	h, vaults := setup(t)
	welcome := project(vaults, "welcome")
	os.MkdirAll(filepath.Join(welcome, "wiki", "concepts"), 0o755)
	os.WriteFile(filepath.Join(welcome, "wiki", "concepts", "Training.md"), []byte("---\ntitle: Training\ntype: concept\nstatus: developing\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n\n# Training\n\nSee [[vanishing gradient problem]] and [[Adam]].\n"), 0o644)
	index, _ := os.ReadFile(filepath.Join(welcome, "wiki", "index.md"))
	os.WriteFile(filepath.Join(welcome, "wiki", "index.md"), append(index, []byte("\n- [[Training]]\n")...), 0o644)
	if code := h.run("lint", "welcome", "--strict"); code != 0 || !strings.Contains(h.out.String(), "## Wanted pages (2)") {
		t.Fatalf("wanted pages are not findings: exit %d\n%s", code, h.out.String())
	}
	if code := h.run("stub", "welcome", "Adam", "--type", "entity"); code != 0 || !strings.Contains(h.out.String(), "wiki/entities/Adam.md (entity)") {
		t.Fatalf("stub Adam exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("stub", "welcome"); code != 0 || !strings.Contains(h.out.String(), "wiki/concepts/vanishing gradient problem.md (concept)") || !strings.Contains(h.out.String(), "committed") {
		t.Fatalf("stub all exit %d\n%s%s", code, h.out.String(), h.err.String())
	}
	if code := h.run("lint", "welcome", "--strict"); code != 0 || !strings.Contains(h.out.String(), "## Stubs to fill (2)") {
		t.Fatalf("stubs are not findings: exit %d\n%s", code, h.out.String())
	}
	if code := h.run("stub", "welcome"); code != 0 || !strings.Contains(h.out.String(), "nothing to stub") {
		t.Fatalf("nothing left exit %d\n%s", code, h.out.String())
	}
	if code := h.run("stub", "welcome", "Nowhere"); code != 1 || !strings.Contains(h.err.String(), "nothing in the wiki links to") {
		t.Fatalf("unlinked title exit %d\n%s", code, h.err.String())
	}
	if code := h.run("stub"); code != 2 {
		t.Fatalf("usage exit %d", code)
	}
}
