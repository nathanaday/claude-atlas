package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/ledger"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var now = time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
	root := filepath.Join(t.TempDir(), "v")
	if _, err := vault.Init(root, vault.Generic, now); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.Open(root)
	return v
}

func TestListAndCapture(t *testing.T) {
	v := newVault(t)
	os.WriteFile(v.Path("inbox/paper.PDF"), []byte("%PDF fake"), 0o644)
	os.MkdirAll(v.Path("inbox/notes"), 0o755)
	os.WriteFile(v.Path("inbox/notes/a.md"), []byte("# a"), 0o644)
	os.WriteFile(v.Path("inbox/.hidden"), []byte("x"), 0o644)
	files, err := ListInbox(v, now)
	if err != nil || len(files) != 2 || files[0].Path != "inbox/notes/a.md" || files[1].Kind != "pdf" || files[1].Captured {
		t.Fatalf("list %+v %v", files, err)
	}
	res, err := Capture(v, []string{"paper.PDF", v.Path("inbox/notes/a.md")}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 2 || res.Commit == "" || !strings.HasPrefix(res.OperationID, "capture-") {
		t.Fatalf("result %+v", res)
	}
	pdf := res.Sources[0]
	if !strings.HasSuffix(pdf.StoredPath, ".pdf") || !strings.HasPrefix(pdf.StoredPath, ".raw/captured/") || pdf.AlreadyCaptured {
		t.Fatalf("pdf %+v", pdf)
	}
	if data, _ := os.ReadFile(v.Path(pdf.StoredPath)); string(data) != "%PDF fake" {
		t.Fatal("bytes must match")
	}
	if _, err := os.Stat(v.Path("inbox/paper.PDF")); err != nil {
		t.Fatal("capture must not remove inbox files")
	}
	l, _ := ledger.Load(v.Path(vault.LedgerPath), now)
	rec, ok := l.Sources[pdf.SourceID]
	if !ok || rec.Title != "paper" || rec.ContentKind != "pdf" || rec.CapturedAt != "2026-09-12" || rec.ReviewStatus != "unreviewed" {
		t.Fatalf("ledger %+v %v", rec, ok)
	}
	if !v.Repo().Tracked(pdf.StoredPath) {
		t.Fatal("captured bytes are committed")
	}
	log, _ := os.ReadFile(v.Path(vault.LogPage))
	if !strings.Contains(string(log), "capture paper.PDF, a.md") {
		t.Fatalf("log:\n%s", log)
	}
	again, err := Capture(v, []string{"inbox/paper.PDF"}, now)
	if err != nil || len(again.Sources) != 1 || !again.Sources[0].AlreadyCaptured || again.Commit != "" {
		t.Fatalf("second capture %+v %v", again, err)
	}
	files, _ = ListInbox(v, now)
	if !files[1].Captured || files[1].SourceID != pdf.SourceID {
		t.Fatalf("list after %+v", files)
	}
	if _, err := Capture(v, []string{"../outside.md"}, now); err == nil {
		t.Fatal("paths outside inbox must fail")
	}
	if _, err := Capture(v, []string{t.TempDir()}, now); err == nil {
		t.Fatal("absolute paths outside the vault must fail")
	}
}

func TestCaptureAcceptsDotsInNamesAndRefusesTraversal(t *testing.T) {
	v := newVault(t)
	name := "L3.1 - Dynamical Sys Cont..md"
	os.WriteFile(v.Path("inbox/"+name), []byte("lecture"), 0o644)
	res, err := Capture(v, []string{name}, now)
	if err != nil {
		t.Fatalf("a name with consecutive dots must capture: %v", err)
	}
	if len(res.Sources) != 1 || res.Sources[0].Path != "inbox/"+name || !strings.HasSuffix(res.Sources[0].StoredPath, ".md") {
		t.Fatalf("result %+v", res.Sources)
	}
	if _, err := Capture(v, []string{"inbox/" + name}, now); err != nil {
		t.Fatalf("vault-relative form: %v", err)
	}
	if _, err := Capture(v, []string{v.Path("inbox/" + name)}, now); err != nil {
		t.Fatalf("absolute form: %v", err)
	}
	for _, bad := range []string{"../wiki/index.md", "inbox/../wiki/index.md", "inbox/../../etc/passwd", filepath.Join(v.Root, "wiki", "index.md")} {
		if _, err := Capture(v, []string{bad}, now); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}
