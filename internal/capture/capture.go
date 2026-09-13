// Package capture moves sources from the inbox into the immutable raw store and records
// them in the source ledger. Nothing in inbox/ is removed here; an ingest plan does that
// once the source has a captured copy.
package capture

import (
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// MaxFileBytes is the largest file capture accepts.
const MaxFileBytes = txn.MaxWriteSize

// WarnFileBytes is the size above which capture warns that git history will grow.
const WarnFileBytes = 50 << 20

// InboxFile describes one file waiting in inbox/.
type InboxFile struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Kind       string `json:"kind"`
	SHA256     string `json:"sha256"`
	Captured   bool   `json:"captured"`
	SourceID   string `json:"source_id,omitempty"`
	StoredPath string `json:"stored_path,omitempty"`
}

// KindOf classifies a file by extension.
func KindOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown":
		return "markdown"
	case ".txt":
		return "text"
	case ".pdf":
		return "pdf"
	case ".epub":
		return "epub"
	case ".html", ".htm":
		return "html"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg":
		return "image"
	case ".mp3", ".wav", ".m4a", ".flac":
		return "audio"
	case ".mp4", ".mov", ".webm", ".mkv":
		return "video"
	case ".json", ".csv", ".yaml", ".yml", ".tsv":
		return "data"
	}
	return "other"
}

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// storedPath is where a capture lands: content-addressed, extension kept.
func storedPath(sum, name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ext == "" || len(ext) > 12 {
		ext = ".bin"
	}
	return vault.CapturedDir + "/" + sum + ext
}

// ListInbox walks inbox/ and says which files already have a captured copy.
func ListInbox(v *vault.Vault, now time.Time) ([]InboxFile, error) {
	led, err := ledger.Load(v.Path(vault.LedgerPath), now)
	if err != nil {
		return nil, err
	}
	root := v.Path(vault.InboxDir)
	var files []InboxFile
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return nil
			}
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(v.Root, p)
		sum, size, err := hashFile(p)
		if err != nil {
			return err
		}
		f := InboxFile{Path: filepath.ToSlash(rel), Size: size, Kind: KindOf(d.Name()), SHA256: sum}
		if id, rec := led.FindBySHA(sum); id != "" {
			f.Captured, f.SourceID, f.StoredPath = true, id, rec.Origin.Locator
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if files == nil {
		files = []InboxFile{}
	}
	return files, nil
}

// Captured reports one file after capture.
type Captured struct {
	Path            string `json:"path"`
	StoredPath      string `json:"stored_path"`
	SourceID        string `json:"source_id"`
	SHA256          string `json:"sha256"`
	Kind            string `json:"kind"`
	Size            int64  `json:"size"`
	AlreadyCaptured bool   `json:"already_captured"`
	Warning         string `json:"warning,omitempty"`
}

// Result is one capture operation.
type Result struct {
	Sources     []Captured `json:"sources"`
	OperationID string     `json:"operation_id,omitempty"`
	Commit      string     `json:"commit,omitempty"`
}

// resolveInbox turns a user-supplied path into a vault-relative inbox path.
func resolveInbox(v *vault.Vault, arg string) (string, error) {
	p := arg
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(v.Root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			return "", fmt.Errorf("%s is outside the vault; place sources in %s/ first", arg, vault.InboxDir)
		}
		p = filepath.ToSlash(rel)
	}
	// Clean resolves any ".." segment, so a path that still leaves inbox/ afterwards
	// is a traversal; a name that merely contains dots, like "Cont..md", is fine.
	p = path.Clean(p)
	if !strings.HasPrefix(p, vault.InboxDir+"/") {
		p = path.Clean(vault.InboxDir + "/" + p)
	}
	if !strings.HasPrefix(p, vault.InboxDir+"/") {
		return "", fmt.Errorf("%s is not a path inside %s/", arg, vault.InboxDir)
	}
	info, err := os.Lstat(v.Path(p))
	if err != nil {
		return "", fmt.Errorf("%s: not found in %s/", arg, vault.InboxDir)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", p)
	}
	return p, nil
}

// Capture copies the named inbox files into .raw/captured/ and records them in the ledger,
// as one commit. A file already captured is reported, not copied again.
func Capture(v *vault.Vault, paths []string, now time.Time) (*Result, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("name at least one file in %s/", vault.InboxDir)
	}
	led, err := ledger.Load(v.Path(vault.LedgerPath), now)
	if err != nil {
		return nil, err
	}
	res := &Result{}
	req := txn.Request{Kind: txn.Capture}
	seen := map[string]bool{}
	var names []string
	for _, arg := range paths {
		rel, err := resolveInbox(v, arg)
		if err != nil {
			return nil, err
		}
		sum, size, err := hashFile(v.Path(rel))
		if err != nil {
			return nil, err
		}
		if size > MaxFileBytes {
			return nil, fmt.Errorf("%s is %d bytes; capture accepts up to %d", rel, size, MaxFileBytes)
		}
		c := Captured{Path: rel, SHA256: sum, Kind: KindOf(rel), Size: size}
		if id, rec := led.FindBySHA(sum); id != "" {
			c.AlreadyCaptured, c.SourceID, c.StoredPath = true, id, rec.Origin.Locator
			res.Sources = append(res.Sources, c)
			continue
		}
		c.StoredPath = storedPath(sum, rel)
		c.SourceID = ledger.ID("file", c.StoredPath, sum)
		if size > WarnFileBytes {
			c.Warning = fmt.Sprintf("%s is large; the vault's git history grows by its size", rel)
		}
		if !seen[sum] {
			seen[sum] = true
			data, err := os.ReadFile(v.Path(rel))
			if err != nil {
				return nil, err
			}
			req.Writes = append(req.Writes, txn.Write{Path: c.StoredPath, Mode: txn.Create, Content: data})
			req.Sources = append(req.Sources, ledger.Update{
				ID: c.SourceID, Title: vault.PageTitle(rel), Origin: &ledger.Origin{Kind: "file", Locator: c.StoredPath},
				ContentSHA256: sum, ContentKind: c.Kind,
			})
			names = append(names, path.Base(rel))
		}
		res.Sources = append(res.Sources, c)
	}
	if len(req.Writes) == 0 {
		return res, nil
	}
	req.Summary = "capture " + strings.Join(names, ", ")
	plan, err := txn.Prepare(v, req, now)
	if err != nil {
		return nil, err
	}
	applied, err := txn.Apply(v, plan, now)
	if err != nil {
		return nil, err
	}
	res.OperationID, res.Commit = applied.OperationID, applied.Commit
	return res, nil
}
