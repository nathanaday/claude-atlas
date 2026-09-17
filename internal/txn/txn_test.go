package txn

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nathanaday/claude-atlas/internal/gitx"
	"github.com/nathanaday/claude-atlas/internal/ledger"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var now = time.Date(2026, 9, 12, 15, 4, 5, 0, time.UTC)

const pageFront = "---\ntitle: %s\ntype: concept\nstatus: seed\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags:\n  - concept\n---\n"

func mkpage(title, body string) []byte {
	return []byte(strings.Replace(pageFront, "%s", title, 1) + body)
}

func needGit(t *testing.T) {
	t.Helper()
	if !gitx.Available() {
		t.Skip("git is not installed")
	}
}

func newVault(t *testing.T) *vault.Vault {
	t.Helper()
	needGit(t)
	root := filepath.Join(t.TempDir(), "v")
	if _, err := vault.Init(root, vault.Options{Mode: vault.Generic}, now); err != nil {
		t.Fatal(err)
	}
	v, err := vault.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func read(t *testing.T, v *vault.Vault, rel string) string {
	t.Helper()
	data, err := os.ReadFile(v.Path(rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestPrepareValidates(t *testing.T) {
	v := newVault(t)
	index := read(t, v, vault.IndexPage)
	cases := []struct {
		name string
		req  Request
		want string
	}{
		{"kind", Request{Kind: "bogus", Summary: "x", Writes: []Write{{Path: "wiki/a.md", Mode: Create, Content: mkpage("A", "")}}}, "unknown operation kind"},
		{"summary", Request{Kind: Save, Summary: " ", Writes: []Write{{Path: "wiki/a.md", Mode: Create, Content: mkpage("A", "")}}}, "summary is required"},
		{"empty", Request{Kind: Save, Summary: "x"}, "at least one write"},
		{"log", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: vault.LogPage, Mode: Replace, Content: mkpage("L", "")}}}, "written by the core"},
		{"ledger", Request{Kind: Ingest, Summary: "x", Writes: []Write{{Path: vault.LedgerPath, Mode: Replace, Content: []byte("{}")}}}, "sources field"},
		{"scope", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "notes/a.md", Mode: Create, Content: mkpage("A", "")}}}, "only under wiki/"},
		{"inbox write", Request{Kind: Ingest, Summary: "x", Writes: []Write{{Path: "inbox/a.md", Mode: Create, Content: []byte("x")}}}, "only remove files"},
		{"canvas scope", Request{Kind: Canvas, Summary: "x", Writes: []Write{{Path: "wiki/a.canvas", Mode: Create, Content: []byte("{}")}}}, "wiki/canvases"},
		{"exists", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: vault.IndexPage, Mode: Create, Content: mkpage("I", "")}}}, "already exists"},
		{"missing", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "wiki/nope.md", Mode: Replace, Content: mkpage("N", "")}}}, "does not exist"},
		{"stale base", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: vault.IndexPage, Mode: Replace, Content: mkpage("I", ""), BaseSHA256: hashOf("old")}}}, "conflict"},
		{"frontmatter", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "wiki/a.md", Mode: Create, Content: []byte("# no front\n")}}}, "no frontmatter"},
		{"required keys", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "wiki/a.md", Mode: Create, Content: []byte("---\ntitle: A\n---\n")}}}, "lacks type"},
		{"json", Request{Kind: Canvas, Summary: "x", Writes: []Write{{Path: "wiki/canvases/a.canvas", Mode: Create, Content: []byte("{")}}}, "not valid JSON"},
		{"dup", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "wiki/a.md", Mode: Create, Content: mkpage("A", "")}, {Path: "wiki/A.md", Mode: Create, Content: mkpage("A", "")}}}, "twice"},
		{"dotdot", Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "wiki/../x.md", Mode: Create, Content: mkpage("A", "")}}}, "clean vault-relative"},
		{"unknown source", Request{Kind: Ingest, Summary: "x", Sources: []ledger.Update{{ID: "src-nope", Ingested: true}}}, "capture it first"},
		{"uncaptured inbox", Request{Kind: Ingest, Summary: "x", Writes: []Write{{Path: "inbox/.gitkeep", Mode: Delete}}}, "has not been captured"},
		{"ideas", Request{Kind: Repair, Summary: "x", Writes: []Write{{Path: "ideas/a.md", Mode: Create, Content: []byte("x")}}}, "scratch space"},
		{"task kind", Request{Kind: "task", Summary: "x", Writes: []Write{{Path: "wiki/a.md", Mode: Create, Content: mkpage("A", "")}}}, "unknown operation kind"},
	}
	for _, c := range cases {
		_, err := Prepare(v, c.req, now)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
		if c.name == "stale base" && !errors.Is(err, ErrConflict) {
			t.Errorf("stale base should be ErrConflict")
		}
	}
	plan, err := Prepare(v, Request{Kind: Save, Summary: "add A", Writes: []Write{
		{Path: "wiki/concepts/A.md", Mode: Create, Content: mkpage("A", "# A\n\nSee [[Nowhere]].\n\n## Empty\n")},
		{Path: vault.IndexPage, Mode: Replace, Content: []byte(index), BaseSHA256: strings.ToUpper(hashOf(index))},
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Warnings, "\n")
	for _, want := range []string{`"Nowhere"`, `"Empty" is empty`, "not linked from any index"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings missing %q:\n%s", want, joined)
		}
	}
	if len(plan.Preview.Creates) != 1 || len(plan.Preview.Replaces) != 1 || plan.Preview.Replaces[0].Before == 0 || !strings.HasPrefix(plan.ID, "plan-") || !strings.HasPrefix(plan.OperationID, "save-20260912-150405-") {
		t.Fatalf("plan %+v", plan)
	}
}

func TestApplyCommitsOneOperation(t *testing.T) {
	v := newVault(t)
	index := strings.Replace(read(t, v, vault.IndexPage), "- No concepts yet.", "- [[A]]", 1)
	plan, err := Prepare(v, Request{Kind: Save, Summary: "add A\nand more", Writes: []Write{
		{Path: "wiki/concepts/A.md", Mode: Create, Content: mkpage("A", "# A\n\ntext\n")},
		{Path: vault.IndexPage, Mode: Replace, Content: []byte(index)},
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("unexpected warnings %v", plan.Warnings)
	}
	res, err := Apply(v, plan, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.ManualCommit != "" || res.OperationID != plan.OperationID || strings.Join(res.ChangedPaths, ",") != "wiki/concepts/A.md,wiki/index.md,wiki/log.md" {
		t.Fatalf("result %+v", res)
	}
	log := read(t, v, vault.LogPage)
	entry := "## 2026-09-12 — " + plan.OperationID + "\n\nadd A and more\n\n- Created: [[A]]\n- Updated: [[index|Wiki Index]]\n"
	if !strings.Contains(log, entry) || !strings.Contains(log, "updated: 2026-09-12") {
		t.Fatalf("log:\n%s", log)
	}
	if dirty, _ := v.Repo().Dirty(); dirty {
		t.Fatal("tree must be clean after apply")
	}
	if _, err := os.Stat(v.Path(".vault-meta/inflight.json")); err == nil {
		t.Fatal("inflight marker must be removed")
	}
	ops, err := History(v, 5, true)
	if err != nil || len(ops) != 2 || ops[0].ID != plan.OperationID || ops[0].Kind != "save" || ops[0].Summary != "add A and more" || len(ops[0].Paths) != 3 || ops[1].Kind != "setup" {
		t.Fatalf("history %+v %v", ops, err)
	}
	// A second entry lands above the first.
	plan2, _ := Prepare(v, Request{Kind: Markdown, Summary: "tweak A", Writes: []Write{{Path: "wiki/concepts/A.md", Mode: Replace, Content: mkpage("A", "# A\n\nmore\n")}}}, now.Add(time.Hour))
	if _, err := Apply(v, plan2, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	log = read(t, v, vault.LogPage)
	if strings.Index(log, plan2.OperationID) > strings.Index(log, plan.OperationID) || strings.Count(log, "## 2026-09-12") != 2 {
		t.Fatalf("log order:\n%s", log)
	}
	st, _ := Inspect(v)
	if !st.HasHistory || st.Dirty != 0 || st.Pending || st.LastSubject != "markdown: tweak A" {
		t.Fatalf("status %+v", st)
	}
}

func TestApplyCommitsManualEditsFirstAndDetectsConflicts(t *testing.T) {
	v := newVault(t)
	os.WriteFile(v.Path(vault.HotPage), []byte("---\ntitle: Hot\ntype: meta\nstatus: x\ncreated: 2026-09-12\nupdated: 2026-09-12\ntags: []\n---\nhand edit\n"), 0o644)
	plan, err := Prepare(v, Request{Kind: Save, Summary: "s", Writes: []Write{{Path: "wiki/concepts/B.md", Mode: Create, Content: mkpage("B", "# B\n\nb\n")}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Apply(v, plan, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.ManualCommit == "" {
		t.Fatal("hand edits should be committed first")
	}
	ops, _ := History(v, 3, false)
	if ops[1].Kind != "manual" || !strings.Contains(ops[1].Summary, "1 file changed by hand") {
		t.Fatalf("history %+v", ops)
	}
	if !strings.Contains(read(t, v, vault.HotPage), "hand edit") {
		t.Fatal("hand edit must survive")
	}
	// A plan made before someone edits its target must not apply.
	plan2, _ := Prepare(v, Request{Kind: Markdown, Summary: "edit B", Writes: []Write{{Path: "wiki/concepts/B.md", Mode: Replace, Content: mkpage("B", "# B\n\nnew\n")}}}, now)
	os.WriteFile(v.Path("wiki/concepts/B.md"), mkpage("B", "# B\n\nsomeone else\n"), 0o644)
	_, err = Apply(v, plan2, now)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if !strings.Contains(read(t, v, "wiki/concepts/B.md"), "someone else") {
		t.Fatal("the other edit must be untouched")
	}
	if dirty, _ := v.Repo().Dirty(); dirty {
		t.Fatal("the other edit should have been committed as manual")
	}
}

func TestRecoverRestoresAnInterruptedApply(t *testing.T) {
	v := newVault(t)
	before := read(t, v, vault.IndexPage)
	os.MkdirAll(v.Path(".vault-meta"), 0o755)
	in := Inflight{OperationID: "save-x", Kind: Save, Paths: []InflightPath{{Path: vault.IndexPage, Existed: true}, {Path: "wiki/concepts/New.md", Existed: false}, {Path: "wiki/never-written.md", Existed: false}}}
	if err := writeInflight(v, in); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(v.Path(vault.IndexPage), []byte("half written"), 0o644)
	os.MkdirAll(v.Path("wiki/concepts"), 0o755)
	os.WriteFile(v.Path("wiki/concepts/New.md"), []byte("partial"), 0o644)
	if p, _ := Pending(v); p == nil || p.OperationID != "save-x" {
		t.Fatal("pending marker should be visible")
	}
	res, err := Recover(v)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || strings.Join(res.Restored, ",") != "wiki/concepts/New.md,wiki/index.md" {
		t.Fatalf("restored %+v", res)
	}
	if read(t, v, vault.IndexPage) != before {
		t.Fatal("index should be back to HEAD")
	}
	if _, err := os.Stat(v.Path("wiki/concepts/New.md")); err == nil {
		t.Fatal("partial new file should be removed")
	}
	if p, _ := Pending(v); p != nil {
		t.Fatal("marker should be gone")
	}
	if res, err := Recover(v); err != nil || res != nil {
		t.Fatal("nothing pending should be nil, nil")
	}
}

func TestUndoRevertsAnOperation(t *testing.T) {
	v := newVault(t)
	plan, _ := Prepare(v, Request{Kind: Save, Summary: "add C", Writes: []Write{{Path: "wiki/concepts/C.md", Mode: Create, Content: mkpage("C", "# C\n\nc\n")}}}, now)
	applied, err := Apply(v, plan, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UndoOperation(v, "nope", now); err == nil {
		t.Fatal("unknown operation")
	}
	res, err := UndoOperation(v, applied.OperationID, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v.Path("wiki/concepts/C.md")); err == nil {
		t.Fatal("undo should remove the page")
	}
	log := read(t, v, vault.LogPage)
	if !strings.Contains(log, "Undid "+applied.OperationID+": add C") || strings.Contains(log, "- Created: [[C]]") {
		t.Fatalf("log after undo:\n%s", log)
	}
	ops, _ := History(v, 1, false)
	if ops[0].Kind != "undo" || ops[0].Undoes != applied.OperationID || ops[0].ID != res.OperationID {
		t.Fatalf("history %+v", ops)
	}
	if dirty, _ := v.Repo().Dirty(); dirty {
		t.Fatal("clean after undo")
	}
}

func TestConfigAndSources(t *testing.T) {
	v := newVault(t)
	plan, err := Prepare(v, ConfigRequest(v, vault.LYT), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(v, plan, now); err != nil {
		t.Fatal(err)
	}
	again, _ := vault.Open(v.Root)
	if again.Config.Mode != vault.LYT {
		t.Fatal("mode should change")
	}
	// Seed a captured source the way capture does, then ingest against it.
	os.MkdirAll(v.Path("inbox"), 0o755)
	os.WriteFile(v.Path("inbox/paper.md"), []byte("paper"), 0o644)
	sum := hashOf("paper")
	locator := ".raw/captured/" + sum + ".md"
	id := ledger.ID("file", locator, sum)
	cap, err := Prepare(v, Request{Kind: Capture, Summary: "capture paper", Writes: []Write{{Path: locator, Mode: Create, Content: []byte("paper")}},
		Sources: []ledger.Update{{ID: id, Title: "paper", Origin: &ledger.Origin{Kind: "file", Locator: locator}, ContentSHA256: sum, ContentKind: "markdown"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(v, cap, now); err != nil {
		t.Fatal(err)
	}
	ing, err := Prepare(v, Request{Kind: Ingest, Summary: "ingest paper", Writes: []Write{
		{Path: "wiki/notes/Paper.md", Mode: Create, Content: mkpage("Paper", "# Paper\n\np\n")},
		{Path: "inbox/paper.md", Mode: Delete},
	}, Sources: []ledger.Update{{ID: id, Ingested: true, Pages: []string{"wiki/notes/Paper.md", "wiki/notes/Missing.md"}, Authority: "primary"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(ing.Warnings) == 0 || !strings.Contains(ing.Warnings[0], "wiki/notes/Missing.md") {
		t.Fatalf("warnings %v", ing.Warnings)
	}
	res, err := Apply(v, ing, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.ChangedPaths, ","), vault.LedgerPath) {
		t.Fatalf("changed %v", res.ChangedPaths)
	}
	if _, err := os.Stat(v.Path("inbox/paper.md")); err == nil {
		t.Fatal("inbox file should be removed")
	}
	l, _ := ledger.Load(v.Path(vault.LedgerPath), now)
	rec := l.Sources[id]
	if rec.IngestedAt != "2026-09-12" || rec.Authority != "primary" || len(rec.Pages) != 1 || rec.Pages[0] != "wiki/notes/Paper.md" {
		t.Fatalf("ledger %+v", rec)
	}
	if !strings.Contains(read(t, v, vault.LogPage), "- Removed: `inbox/paper.md`\n- Sources: "+id) {
		t.Fatalf("log:\n%s", read(t, v, vault.LogPage))
	}
}

func TestApplyDropsLedgerPagesThatNoLongerExist(t *testing.T) {
	v := newVault(t)
	sum := hashOf("paper")
	locator := ".raw/captured/" + sum + ".md"
	id := ledger.ID("file", locator, sum)
	for _, req := range []Request{
		{Kind: Capture, Summary: "capture paper", Writes: []Write{{Path: locator, Mode: Create, Content: []byte("paper")}},
			Sources: []ledger.Update{{ID: id, Origin: &ledger.Origin{Kind: "file", Locator: locator}, ContentSHA256: sum}}},
		{Kind: Ingest, Summary: "ingest paper", Writes: []Write{{Path: "wiki/notes/Paper.md", Mode: Create, Content: mkpage("Paper", "# Paper\n\np\n")}},
			Sources: []ledger.Update{{ID: id, Ingested: true, Pages: []string{"wiki/notes/Paper.md"}}}},
	} {
		plan, err := Prepare(v, req, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Apply(v, plan, now); err != nil {
			t.Fatal(err)
		}
	}
	move, err := Prepare(v, Request{Kind: Markdown, Summary: "archive paper", Writes: []Write{
		{Path: "wiki/notes/Paper.md", Mode: Delete},
		{Path: "wiki/archive/Paper.md", Mode: Create, Content: mkpage("Paper", "# Paper\n\np\n")},
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(move.Warnings, "\n"), "source "+id+" lists wiki/notes/Paper.md, which does not exist; apply removes it") {
		t.Fatalf("warnings %v", move.Warnings)
	}
	res, err := Apply(v, move, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(res.ChangedPaths, ","), vault.LedgerPath) {
		t.Fatalf("changed %v", res.ChangedPaths)
	}
	l, _ := ledger.Load(v.Path(vault.LedgerPath), now)
	if pages := l.Sources[id].Pages; len(pages) != 0 {
		t.Fatalf("pages %v", pages)
	}
	if dirty, _ := v.Repo().Dirty(); dirty {
		t.Fatal("the ledger change must be in the operation's commit")
	}
}

func TestPrependLogHandlesEmptyAndHeaderOnly(t *testing.T) {
	out := string(prependLog(nil, "## 2026-09-12 — a\n\nfirst\n", now))
	if !strings.HasPrefix(out, "---\n") || !strings.HasSuffix(out, "Newest completed operations appear first.\n\n## 2026-09-12 — a\n\nfirst\n") {
		t.Fatalf("empty:\n%s", out)
	}
	out = string(prependLog([]byte(out), "## 2026-09-13 — b\n\nsecond\n", now.Add(24*time.Hour)))
	if !strings.Contains(out, "updated: 2026-09-13") || strings.Index(out, "— b") > strings.Index(out, "— a") {
		t.Fatalf("second:\n%s", out)
	}
}

func TestCanvasKindWritesTheCanvasIndex(t *testing.T) {
	v := newVault(t)
	page := mkpage("Canvases", "# Canvases\n")
	if _, err := Prepare(v, Request{Kind: Canvas, Summary: "x", Writes: []Write{{Path: vault.CanvasIndex, Mode: Create, Content: page}}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(v, Request{Kind: Canvas, Summary: "x", Writes: []Write{{Path: "wiki/canvases/index.md", Mode: Create, Content: page}}}, now); err == nil {
		t.Fatal("the canvas index is canvases.md, not index.md")
	}
}

func TestAnInboxDeleteNeedsACapture(t *testing.T) {
	v := newVault(t)
	ok := []struct {
		name string
		req  Request
	}{
		{"a page", Request{Kind: Ingest, Summary: "add A",
			Writes: []Write{{Path: "wiki/concepts/A.md", Mode: Create, Content: mkpage("A", "# A\n\ntext\n")}}}},
		{"another kind", Request{Kind: Save, Summary: "save A",
			Writes: []Write{{Path: "wiki/concepts/B.md", Mode: Create, Content: mkpage("B", "# B\n\ntext\n")}}}},
	}
	for _, c := range ok {
		if _, err := Prepare(v, c.req, now); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	del := Request{Kind: Ingest, Summary: "clear the inbox", Writes: []Write{{Path: "inbox/.gitkeep", Mode: Delete}}}
	if _, err := Prepare(v, del, now); err == nil || !strings.Contains(err.Error(), "has not been captured") {
		t.Errorf("an inbox delete needs the ledger to hold the file: %v", err)
	}
	// Task pages under wiki/ are ordinary pages now; nothing reserves the folder.
	if _, err := Prepare(v, Request{Kind: Save, Summary: "x", Writes: []Write{{Path: "wiki/tasks/A.md", Mode: Create, Content: mkpage("A", "")}}}, now); err != nil {
		t.Fatalf("wiki/tasks is not reserved: %v", err)
	}
}
