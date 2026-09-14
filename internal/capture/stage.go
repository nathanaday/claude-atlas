package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/ledger"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// Staging copies sources from outside the vault into inbox/. A file whose bytes are already
// in the source ledger, or already waiting in the inbox, is left alone, so a folder that
// grows over time can be staged again and again and only its new files move.

// MapPath is the vault-relative record of what was staged from where. It is derived state.
const MapPath = vault.MetaDir + "/ingest.json"

const mapSchema = "claude-atlas.ingest-map.v1"

// Staged is one file the plan will copy.
type Staged struct {
	From   string `json:"from"` // absolute external path
	To     string `json:"to"`   // vault-relative inbox path
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Skip is a file the plan leaves out and why.
type Skip struct {
	From   string `json:"from"`
	Reason string `json:"reason"`
}

// StagePlan is what staging would do, before it does it.
type StagePlan struct {
	Vault   string   `json:"vault"`
	Sources []string `json:"sources"`
	// Waiting counts files already in the inbox that no operation has captured yet.
	Waiting int `json:"waiting"`
	// Dirs lists the sources that are directories, so a caller can remember them.
	Dirs      []string `json:"dirs"`
	New       []Staged `json:"new"`
	Unchanged []string `json:"unchanged"` // external paths already ingested or already waiting
	Skipped   []Skip   `json:"skipped"`
}

// StageResult reports what was copied.
type StageResult struct {
	Staged []Staged `json:"staged"`
}

// stageMap is the content of MapPath.
type stageMap struct {
	Schema string              `json:"schema"`
	Staged map[string]mapEntry `json:"staged"`
}

type mapEntry struct {
	SHA256   string `json:"sha256"`
	StagedAt string `json:"staged_at"`
	Inbox    string `json:"inbox"`
}

func loadMap(v *vault.Vault) stageMap {
	m := stageMap{Schema: mapSchema, Staged: map[string]mapEntry{}}
	data, err := os.ReadFile(v.Path(MapPath))
	if err != nil {
		return m
	}
	var loaded stageMap
	if json.Unmarshal(data, &loaded) == nil && loaded.Staged != nil {
		m.Staged = loaded.Staged
	}
	return m
}

func (m stageMap) save(v *vault.Vault) error {
	if err := os.MkdirAll(v.Path(vault.MetaDir), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(v.Path(MapPath), append(data, '\n'), 0o644)
}

// knownHashes returns every content hash the vault already holds: captured sources and
// files waiting in the inbox.
func knownHashes(v *vault.Vault, now time.Time) (map[string]bool, error) {
	known := map[string]bool{}
	led, err := ledger.Load(v.Path(vault.LedgerPath), now)
	if err != nil {
		return nil, err
	}
	for _, s := range led.Sources {
		if s.ContentSHA256 != "" {
			known[s.ContentSHA256] = true
		}
	}
	files, err := ListInbox(v, now)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		known[f.SHA256] = true
	}
	return known, nil
}

// PlanStage decides which files under the given sources are new to the vault. Each source
// is a file or a directory outside the vault; hidden entries are ignored.
func PlanStage(v *vault.Vault, sources []string, now time.Time) (*StagePlan, error) {
	if len(sources) == 0 {
		return nil, errors.New("name a file or folder to ingest")
	}
	known, err := knownHashes(v, now)
	if err != nil {
		return nil, err
	}
	plan := &StagePlan{Vault: v.Root, New: []Staged{}, Unchanged: []string{}, Skipped: []Skip{}}
	taken := map[string]bool{}
	files, _ := ListInbox(v, now)
	for _, f := range files {
		taken[strings.ToLower(f.Path)] = true
		if !f.Captured {
			plan.Waiting++
		}
	}
	for _, source := range sources {
		abs, err := filepath.Abs(source)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("%s: not found", source)
		}
		if inside(v.Root, abs) {
			return nil, fmt.Errorf("%s is inside the vault; ingest reads sources from outside it", source)
		}
		plan.Sources = append(plan.Sources, abs)
		if !info.IsDir() {
			plan.consider(abs, "inbox/"+filepath.Base(abs), info.Size(), known, taken)
			continue
		}
		plan.Dirs = append(plan.Dirs, abs)
		base := filepath.Base(abs)
		err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if p == abs {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(abs, p)
			fi, err := d.Info()
			if err != nil {
				return nil
			}
			plan.consider(p, path.Join("inbox", base, filepath.ToSlash(rel)), fi.Size(), known, taken)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(plan.New, func(i, j int) bool { return plan.New[i].From < plan.New[j].From })
	sort.Strings(plan.Unchanged)
	return plan, nil
}

func inside(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func (plan *StagePlan) consider(from, to string, size int64, known, taken map[string]bool) {
	if size > MaxFileBytes {
		plan.Skipped = append(plan.Skipped, Skip{From: from, Reason: fmt.Sprintf("larger than %d MiB", MaxFileBytes>>20)})
		return
	}
	sum, _, err := hashFile(from)
	if err != nil {
		plan.Skipped = append(plan.Skipped, Skip{From: from, Reason: "unreadable"})
		return
	}
	if known[sum] {
		plan.Unchanged = append(plan.Unchanged, from)
		return
	}
	known[sum] = true
	plan.New = append(plan.New, Staged{From: from, To: uniqueInbox(to, taken), SHA256: sum, Size: size})
}

// uniqueInbox picks an inbox path that no waiting file uses yet.
func uniqueInbox(to string, taken map[string]bool) string {
	candidate := to
	for n := 2; taken[strings.ToLower(candidate)]; n++ {
		ext := path.Ext(to)
		candidate = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(to, ext), n, ext)
	}
	taken[strings.ToLower(candidate)] = true
	return candidate
}

// ApplyStage copies the plan's new files into the inbox and records them in the map.
func ApplyStage(v *vault.Vault, plan *StagePlan, now time.Time) (*StageResult, error) {
	if plan.Vault != v.Root {
		return nil, fmt.Errorf("plan belongs to %s, not %s", plan.Vault, v.Root)
	}
	m := loadMap(v)
	res := &StageResult{Staged: []Staged{}}
	for _, f := range plan.New {
		if err := copyFile(f.From, v.Path(f.To)); err != nil {
			return res, err
		}
		m.Staged[f.From] = mapEntry{SHA256: f.SHA256, StagedAt: now.UTC().Format(time.RFC3339), Inbox: f.To}
		res.Staged = append(res.Staged, f)
	}
	if err := m.save(v); err != nil {
		return res, err
	}
	return res, nil
}

func copyFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(to), ".staging-*")
	if err != nil {
		return err
	}
	name := out.Name()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(name)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, to)
}

// StagedFrom lists what was staged from an external path, for reporting.
func StagedFrom(v *vault.Vault) map[string]string {
	out := map[string]string{}
	for from, e := range loadMap(v).Staged {
		out[from] = e.StagedAt
	}
	return out
}
