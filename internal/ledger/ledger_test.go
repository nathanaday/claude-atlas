package ledger

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)

func TestIDMatchesClaudeObsidianFormula(t *testing.T) {
	// sha256("file\0.raw/captured/abc.pdf\0abc")[:20]
	got := ID("file", ".raw/captured/abc.pdf", "ABC")
	if !strings.HasPrefix(got, "src-") || len(got) != 24 || got != ID("FILE", ".raw/captured/abc.pdf", "abc") {
		t.Fatalf("id %s", got)
	}
}

func TestApplyCreatesThenUpdates(t *testing.T) {
	l := Empty(now)
	origin := &Origin{Kind: "file", Locator: ".raw/captured/aa.pdf"}
	id := ID("file", origin.Locator, "aa")
	if err := l.Apply([]Update{{ID: id, Pages: []string{"wiki/x.md"}}}, now); err == nil {
		t.Fatal("unknown id without origin must fail")
	}
	if err := l.Apply([]Update{{ID: "src-wrong", Origin: origin, ContentSHA256: "aa"}}, now); err == nil {
		t.Fatal("id must match origin")
	}
	if err := l.Apply([]Update{{ID: id, Origin: origin, ContentSHA256: "aa", ContentKind: "pdf", Title: "Paper"}}, now); err != nil {
		t.Fatal(err)
	}
	rec := l.Sources[id]
	if rec.Title != "Paper" || rec.Authority != "unknown" || rec.ReviewStatus != "unreviewed" || rec.CapturedAt != "2026-09-12" || rec.IngestedAt != "" {
		t.Fatalf("record %+v", rec)
	}
	err := l.Apply([]Update{{ID: id, Ingested: true, Pages: []string{"wiki/sources/Paper.md", "wiki/sources/Paper.md"}, Authority: "primary"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	rec = l.Sources[id]
	if rec.IngestedAt != "2026-09-12" || rec.ReviewStatus != "active" || len(rec.Pages) != 1 || rec.Authority != "primary" {
		t.Fatalf("record %+v", rec)
	}
	if err := l.Apply([]Update{{ID: id, Authority: "bogus"}}, now); err == nil {
		t.Fatal("bad authority")
	}
	if err := l.Apply([]Update{{ID: id, Pages: []string{"notes/x.md"}}}, now); err == nil {
		t.Fatal("pages must be under wiki/")
	}
	again, err := Parse(l.Encode())
	if err != nil || again.Sources[id].Title != "Paper" || len(again.Sources[id].Pages) != 1 {
		t.Fatalf("round trip %+v %v", again, err)
	}
	if got, _ := again.FindBySHA("aa"); got != id {
		t.Fatal("find by sha")
	}
}

func TestDropPagesRemovesPagesThatDoNotExist(t *testing.T) {
	l := Empty(now)
	l.Sources["src-a"] = Source{Title: "A", Pages: []string{"wiki/a.md", "wiki/gone.md"}}
	l.Sources["src-b"] = Source{Title: "B", Pages: []string{"wiki/a.md"}}
	later := now.Add(time.Hour)
	dropped := l.DropPages(func(p string) bool { return p != "wiki/gone.md" }, later)
	if len(dropped) != 1 || dropped[0] != (Dropped{ID: "src-a", Page: "wiki/gone.md"}) {
		t.Fatalf("dropped %+v", dropped)
	}
	if got := l.Sources["src-a"].Pages; len(got) != 1 || got[0] != "wiki/a.md" || len(l.Sources["src-b"].Pages) != 1 || l.GeneratedAt != timestamp(later) {
		t.Fatalf("ledger %+v", l)
	}
	if again := l.DropPages(func(string) bool { return true }, now); len(again) != 0 || l.GeneratedAt != timestamp(later) {
		t.Fatal("a ledger with nothing to drop must not change")
	}
}

func TestApplySetsViaAndUpdateWithoutViaKeepsIt(t *testing.T) {
	l := Empty(now)
	origin := &Origin{Kind: "file", Locator: ".raw/captured/bb.pdf"}
	id := ID("file", origin.Locator, "bb")
	via := &Via{ID: "p1", Name: "cs566"}
	if err := l.Apply([]Update{{ID: id, Origin: origin, ContentSHA256: "bb", Via: via}}, now); err != nil {
		t.Fatal(err)
	}
	rec := l.Sources[id]
	if rec.Via == nil || *rec.Via != *via {
		t.Fatalf("via %+v", rec.Via)
	}
	out := l.Encode()
	if !strings.Contains(string(out), `"via": {`) {
		t.Fatalf("encode:\n%s", out)
	}
	again, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Sources[id]
	if got.Via == nil || *got.Via != *via {
		t.Fatalf("round trip via %+v", got.Via)
	}
	if err := l.Apply([]Update{{ID: id, Notes: "seen"}}, now); err != nil {
		t.Fatal(err)
	}
	rec = l.Sources[id]
	if rec.Via == nil || *rec.Via != *via {
		t.Fatalf("update without via must keep the old via, got %+v", rec.Via)
	}
}

func TestParseKeepsLegacyFields(t *testing.T) {
	raw := `{"schema":"claude-obsidian.source-ledger.v1","generated_at":"2026-01-01T00:00:00Z","sources":{"src-abc":{"title":"T","origin":{"kind":"url","locator":"https://x"},"authority":"official","review_status":"active","pages":[],"independence_key":"x","refresh_due":"2027-01-01"}}}`
	l, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	out := string(l.Encode())
	if !strings.Contains(out, `"independence_key": "x"`) || !strings.Contains(out, `"refresh_due"`) || !strings.Contains(out, Schema) {
		t.Fatalf("encode:\n%s", out)
	}
}
