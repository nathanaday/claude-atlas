// Package gitx runs the few git commands the core needs and parses their output.
package gitx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Repo is a git working tree rooted at Dir.
type Repo struct {
	Dir string
}

// Available reports whether the git command is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// ErrNotRepo is returned when Dir is not the top level of a git working tree.
var ErrNotRepo = errors.New("not a git repository")

func (r Repo) cmd(args ...string) *exec.Cmd {
	full := append([]string{"-c", "core.quotePath=false", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	return cmd
}

func (r Repo) run(args ...string) (string, error) {
	cmd := r.cmd(args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("git %s: %s", args[0], detail)
	}
	return stdout.String(), nil
}

// Init creates a repository at Dir with main as its first branch.
func (r Repo) Init() error {
	if _, err := r.run("init", "-q"); err != nil {
		return err
	}
	_, err := r.run("symbolic-ref", "HEAD", "refs/heads/main")
	return err
}

// IsRepo reports whether Dir itself is the top level of a working tree.
// A vault inside another repository does not count: its history must be its own.
func (r Repo) IsRepo() bool {
	out, err := r.run("rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(out))
	if err != nil {
		return false
	}
	dir, err := filepath.EvalSymlinks(r.Dir)
	if err != nil {
		return false
	}
	return top == dir
}

// InsideOtherRepo reports whether Dir is inside a working tree whose top level is above it.
func (r Repo) InsideOtherRepo() bool {
	out, err := r.run("rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	return !r.IsRepo() && strings.TrimSpace(out) != ""
}

// HasHead reports whether at least one commit exists.
func (r Repo) HasHead() bool {
	_, err := r.run("rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// Head returns the current commit.
func (r Repo) Head() (string, error) {
	out, err := r.run("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Entry is one line of git status: a two-letter code and a path relative to Dir.
type Entry struct {
	Code string
	Path string
}

// Status lists every changed or untracked path. Untracked directories are expanded to files.
func (r Repo) Status() ([]Entry, error) {
	out, err := r.run("status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return nil, err
	}
	var entries []Entry
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		code, path := f[:2], f[3:]
		if code[0] == 'R' || code[0] == 'C' {
			// A rename carries the source path in the next field.
			i++
		}
		entries = append(entries, Entry{Code: code, Path: path})
	}
	return entries, nil
}

// Dirty reports whether the working tree has any change git would notice.
func (r Repo) Dirty() (bool, error) {
	entries, err := r.Status()
	return len(entries) > 0, err
}

// AddAll stages every change in the tree, deletions included.
func (r Repo) AddAll() error {
	_, err := r.run("add", "-A", "--", ".")
	return err
}

// Add stages the given paths, deletions included.
func (r Repo) Add(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := r.run(append([]string{"add", "-A", "--"}, paths...)...)
	return err
}

func (r Repo) identityArgs() []string {
	out, err := r.run("config", "--get", "user.email")
	if err == nil && strings.TrimSpace(out) != "" {
		return nil
	}
	return []string{"-c", "user.name=claude-atlas", "-c", "user.email=claude-atlas@localhost"}
}

// Commit records the index with message and returns the new commit. It falls back to a
// local identity when the user has none configured, and skips commit hooks.
func (r Repo) Commit(message string) (string, error) {
	args := append(r.identityArgs(), "commit", "-q", "--no-verify", "-m", message)
	if _, err := r.run(args...); err != nil {
		return "", err
	}
	return r.Head()
}

// RestoreFromHead puts the given tracked paths back to their HEAD content.
func (r Repo) RestoreFromHead(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	_, err := r.run(append([]string{"checkout", "HEAD", "--"}, paths...)...)
	return err
}

// Tracked reports whether HEAD contains path.
func (r Repo) Tracked(path string) bool {
	_, err := r.run("cat-file", "-e", "HEAD:"+path)
	return err == nil
}

// Commit is one entry of the log with the trailers the core wrote.
type Commit struct {
	SHA      string
	Date     time.Time
	Subject  string
	Body     string
	Trailers map[string]string
}

// Log returns the newest n commits, or all of them when n is 0.
func (r Repo) Log(n int) ([]Commit, error) {
	if !r.HasHead() {
		return nil, nil
	}
	args := []string{"log", "--format=%H%x00%aI%x00%s%x00%b%x1e"}
	if n > 0 {
		args = append(args, fmt.Sprintf("-n%d", n))
	}
	out, err := r.run(args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

func parseLog(out string) []Commit {
	var commits []Commit
	for _, record := range strings.Split(out, "\x1e") {
		record = strings.TrimLeft(record, "\n")
		fields := strings.SplitN(record, "\x00", 4)
		if len(fields) < 4 {
			continue
		}
		date, _ := time.Parse(time.RFC3339, fields[1])
		body := strings.TrimSpace(fields[3])
		commits = append(commits, Commit{
			SHA: fields[0], Date: date, Subject: fields[2], Body: body, Trailers: trailers(body),
		})
	}
	return commits
}

// LogFollow lists the commits that touched one path, newest first, across renames.
func (r Repo) LogFollow(path string) ([]Commit, error) {
	if !r.HasHead() {
		return nil, nil
	}
	out, err := r.run("log", "--follow", "--format=%H%x00%aI%x00%s%x00%b%x1e", "--", path)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// trailers parses `key: value` lines from the last paragraph of a commit body.
func trailers(body string) map[string]string {
	out := map[string]string{}
	paragraphs := strings.Split(strings.TrimSpace(body), "\n\n")
	if len(paragraphs) == 0 {
		return out
	}
	for _, line := range strings.Split(paragraphs[len(paragraphs)-1], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.ContainsAny(key, " \t") {
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

// RevertNoCommit applies the inverse of sha to the index and tree without committing.
// A conflict aborts the revert and returns an error.
func (r Repo) RevertNoCommit(sha string) error {
	if _, err := r.run("revert", "--no-commit", sha); err != nil {
		r.run("revert", "--abort")
		return err
	}
	return nil
}

// ShowFile returns the content of path at rev.
func (r Repo) ShowFile(rev, path string) ([]byte, error) {
	cmd := r.cmd("show", rev+":"+path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git show %s:%s: %s", rev, path, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ChangedPaths lists the paths one commit touched.
func (r Repo) ChangedPaths(sha string) ([]string, error) {
	out, err := r.run("show", "--format=", "--name-only", "-z", sha)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}
