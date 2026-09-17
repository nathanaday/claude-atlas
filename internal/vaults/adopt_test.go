package vaults

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/registry"
)

func TestAdoptReposLinksAGitFolderDroppedIntoRepos(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	initRepo(t, filepath.Join(project.Path, "repos", "my_git_project"))

	adopted, err := AdoptRepos(h, cfg, project, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopted) != 1 || adopted[0].Name != "my_git_project" {
		t.Fatalf("one repository should be adopted, got %+v", adopted)
	}
	if adopted[0].Changes != cfg.DefaultChanges() {
		t.Fatalf("the adopted repository takes the configured policy, got %q", adopted[0].Changes)
	}
	e := refreshEntry(t, cfg, project.ID)
	var found *registry.Repo
	for i := range e.Repos {
		if e.Repos[i].Name == "my_git_project" {
			found = &e.Repos[i]
		}
	}
	if found == nil {
		t.Fatalf("the identity file should now name it: %+v", e.Repos)
	}
	if found.Path != filepath.Join(project.Path, "repos", "my_git_project") {
		t.Fatalf("it should resolve to its folder, got %q", found.Path)
	}
	if subject := lastCommitSubject(t, project.Path); subject != "setup: add repository my_git_project" {
		t.Fatalf("one commit per adoption, got %q", subject)
	}
}

func TestAdoptReposIsIdempotent(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	initRepo(t, filepath.Join(project.Path, "repos", "again"))
	if _, err := AdoptRepos(h, cfg, project, identityNow); err != nil {
		t.Fatal(err)
	}
	adopted, err := AdoptRepos(h, cfg, refreshEntry(t, cfg, project.ID), identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopted) != 0 {
		t.Fatalf("a second run adopts nothing, got %+v", adopted)
	}
}

func TestAdoptReposSkipsAFolderThatIsNotARepository(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	if err := os.MkdirAll(filepath.Join(project.Path, "repos", "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	adopted, err := AdoptRepos(h, cfg, project, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopted) != 0 {
		t.Fatalf("a plain folder is not a repository, got %+v", adopted)
	}
}

func TestAdoptReposHonoursTheConfiguredPolicy(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	if err := cfg.SetDefaultChanges("pr"); err != nil {
		t.Fatal(err)
	}
	initRepo(t, filepath.Join(project.Path, "repos", "service"))
	adopted, err := AdoptRepos(h, cfg, project, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopted) != 1 || adopted[0].Changes != "pr" {
		t.Fatalf("the configured policy should be written, got %+v", adopted)
	}
}

func TestAdoptReposRefusesAKnowledgeBase(t *testing.T) {
	cfg, h, _, kb := fixtureEntries(t)
	if _, err := AdoptRepos(h, cfg, kb, identityNow); err == nil {
		t.Fatal("a knowledge base has no repositories")
	}
}

func TestAdoptReposSkipsTheHostRepositorysName(t *testing.T) {
	cfg, h, code, project := fixtureInRepo(t, "atlas-host")
	initRepo(t, filepath.Join(project.Path, "repos", filepath.Base(code)))
	adopted, err := AdoptRepos(h, cfg, project, identityNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(adopted) != 0 {
		t.Fatalf("a folder that collides with the host is skipped, got %+v", adopted)
	}
}

func TestRemoveRepoRefusesAFolderStillUnderRepos(t *testing.T) {
	cfg, h, project, _ := fixtureEntries(t)
	if _, _, err := CreateRepo(h, cfg, project, "paper", "", identityNow); err != nil {
		t.Fatal(err)
	}
	project = refreshEntry(t, cfg, project.ID)
	err := RemoveRepo(h, cfg, project, "paper", identityNow)
	if err == nil {
		t.Fatal("unlinking a folder still under repos/ would be undone by the next refresh")
	}
	if !strings.Contains(err.Error(), "repos/") {
		t.Fatalf("the error should say where the folder is: %v", err)
	}
	moved := filepath.Join(t.TempDir(), "paper")
	if err := os.Rename(filepath.Join(project.Path, "repos", "paper"), moved); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRepo(h, cfg, project, "paper", identityNow); err != nil {
		t.Fatalf("with the folder gone from repos/, unlink works: %v", err)
	}
}
