package links

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/gitx"
)

func TestDetectKind(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "repo", ".git"), 0o755)
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	if DetectKind(filepath.Join(dir, "repo")) != Repo || DetectKind(filepath.Join(dir, "docs")) != Materials {
		t.Fatal("kind detection wrong")
	}
}

func TestInspectMaterials(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.pdf"), make([]byte, 1500), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "b.png"), make([]byte, 500), 0o644)
	os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("x"), 0o644)
	link := Inspect(Materials, dir)
	if !link.OK || *link.Files != 2 || *link.Bytes != 2000 || link.Newest == "" {
		t.Fatalf("got %+v", link)
	}
	if _, ok := link.Touched(); !ok {
		t.Fatal("materials with files should report a touched date")
	}
	missing := Inspect(Materials, filepath.Join(dir, "nope"))
	if missing.OK || missing.Error != "not found" {
		t.Fatalf("got %+v", missing)
	}
}

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

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{500: "500 B", 1536: "1.5 KB", 5 * 1024 * 1024: "5.0 MB"} {
		if got := HumanBytes(n); got != want {
			t.Errorf("%d: got %s want %s", n, got, want)
		}
	}
}
