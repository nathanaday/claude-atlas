package capture

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/repomap"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// RepoStage is what staging a repository did.
type RepoStage struct {
	Repo     registry.Repo     `json:"repo"`
	Snapshot *repomap.Snapshot `json:"-"`
	Commit   string            `json:"commit"`
	Since    string            `json:"since,omitempty"`
	To       string            `json:"to"` // vault-relative inbox path
	// New is false when the same snapshot already waits in the inbox or its bytes were
	// captured before; nothing was written then.
	New bool `json:"new"`
	// Described is the page that describes the repository today, when one does.
	Described *registry.RepoDescription `json:"described,omitempty"`
}

// StageRepo writes a snapshot of one of the project's repositories into its inbox, with
// the log since the commit the page describing it was written from. The snapshot is a
// source like any file; nothing is remembered for a later stage with no paths.
func StageRepo(v *vault.Vault, project registry.Entry, name string, now time.Time) (*RepoStage, error) {
	var repo *registry.Repo
	for i := range project.Repos {
		if strings.EqualFold(project.Repos[i].Name, name) {
			repo = &project.Repos[i]
		}
	}
	if repo == nil {
		names := make([]string, 0, len(project.Repos))
		for _, r := range project.Repos {
			names = append(names, r.Name)
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("%s has no repositories", project.Name)
		}
		return nil, fmt.Errorf("%s has no repository named %s; it has %s", project.Name, name, strings.Join(names, ", "))
	}
	if repo.Error != "" {
		return nil, fmt.Errorf("repository %s: %s", repo.Name, repo.Error)
	}
	described := repomap.Describe(project, *repo)
	since := ""
	if described != nil {
		since = described.Commit
	}
	snap, err := repomap.TakeSnapshot(*repo, since, now)
	if err != nil {
		return nil, err
	}
	out := &RepoStage{Repo: *repo, Snapshot: snap, Commit: snap.Commit, Since: snap.Since, Described: described}
	known, err := knownHashes(v, now)
	if err != nil {
		return nil, err
	}
	taken := map[string]bool{}
	files, _ := ListInbox(v, nil, now)
	for _, f := range files {
		taken[strings.ToLower(f.Path)] = true
	}
	to := "inbox/" + snap.FileName
	if taken[strings.ToLower(to)] {
		out.To = to
		return out, nil
	}
	sum := hashBytes(snap.Content)
	if known[sum] {
		out.To = to
		return out, nil
	}
	if err := os.MkdirAll(filepath.Dir(v.Path(to)), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(v.Path(to), snap.Content, 0o644); err != nil {
		return nil, err
	}
	out.To, out.New = to, true
	return out, nil
}
