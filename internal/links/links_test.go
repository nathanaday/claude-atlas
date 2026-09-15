package links

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
)

func TestInspectRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	run("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("1"), 0o644)
	run("add", "f.txt")
	run("commit", "-q", "-m", "one")
	os.WriteFile(filepath.Join(dir, "g.txt"), []byte("2"), 0o644)
	link := Inspect(Repo, dir)
	if !link.OK || link.Branch != "main" || link.LastCommit == "" || link.Dirty == nil || *link.Dirty != 1 {
		t.Fatalf("got %+v", link)
	}
	plain := Inspect(Repo, t.TempDir())
	if plain.OK || plain.Error != "not a git repository" {
		t.Fatalf("got %+v", plain)
	}
}

func TestPolicyAndRemoteURL(t *testing.T) {
	if Policy("commit", "git@x:y") != "commit" || Policy("", "git@x:y") != "pr" || Policy("", "") != "commit" {
		t.Fatal("Policy")
	}
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	if err := CreateRepo(dir, "x"); err != nil {
		t.Fatal(err)
	}
	if RemoteURL(dir) != "" {
		t.Fatal("no remote yet")
	}
	exec.Command("git", "-C", dir, "remote", "add", "origin", "git@example.com:a/x.git").Run()
	if RemoteURL(dir) != "git@example.com:a/x.git" {
		t.Fatalf("remote %q", RemoteURL(dir))
	}
}
