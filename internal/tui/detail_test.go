package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func TestTouchedAndTaskCounts(t *testing.T) {
	zero, three := 0, 3
	for _, c := range []struct {
		s    *registry.State
		want string
	}{
		{nil, "not refreshed"},
		{&registry.State{}, "new"},
		{&registry.State{DaysIdle: &zero}, "touched today"},
		{&registry.State{DaysIdle: &three}, "idle 3d"},
	} {
		if got := touchedText(c.s); got != c.want {
			t.Errorf("touchedText(%+v) = %q, want %q", c.s, got, c.want)
		}
	}
	for _, c := range []struct {
		s    *registry.State
		want string
	}{
		{nil, ""},
		{&registry.State{}, ""},
		{&registry.State{Tasks: &registry.TaskSummary{}}, "no tasks"},
		{&registry.State{Tasks: &registry.TaskSummary{Counts: tasks.Counts{Open: 1}}}, "1 task open"},
		{&registry.State{Tasks: &registry.TaskSummary{Counts: tasks.Counts{Open: 2}}}, "2 tasks open"},
	} {
		if got := taskCountText(c.s); got != c.want {
			t.Errorf("taskCountText = %q, want %q", got, c.want)
		}
	}
	if got := clip("a-very-long-knowledge-base-name", 12); got != "a-very-long…" {
		t.Errorf("clip = %q", got)
	}
	if got := clip("short", 12); got != "short" {
		t.Errorf("clip = %q", got)
	}
}

func TestBoxLinesByKind(t *testing.T) {
	four, zero := 4, 0
	p := registry.Entry{Kind: vault.Project, Name: "p3", Path: "/v/p3",
		State: &registry.State{Heat: "hot", Pages: &four, DaysIdle: &zero, Tasks: &registry.TaskSummary{Counts: tasks.Counts{Open: 3}}}}
	if lines := boxLines(p); len(lines) != 2 || lines[0] != "🔥 p3" || lines[1] != "touched today · 3 tasks open" {
		t.Fatalf("project box %q", lines)
	}
	kb := registry.Entry{Kind: vault.Knowledge, Name: "papers", Path: "/v/papers", State: &registry.State{Heat: "warm", Pages: &four}}
	if lines := boxLines(kb); len(lines) != 2 || lines[0] != "🌤️ papers   open" || lines[1] != "4 pages · new" {
		t.Fatalf("knowledge box %q", lines)
	}
	kb.Access = vault.AccessGuarded
	kb.State = nil
	if lines := boxLines(kb); lines[0] != "— papers   guarded" || lines[1] != "— pages · not refreshed" {
		t.Fatalf("guarded, unrefreshed box %q", lines)
	}
	bad := registry.Entry{Path: "/v/old-notes", Error: "v1 vault; run claude-atlas adopt", Reason: registry.ReasonV1}
	if lines := boxLines(bad); lines[0] != "old-notes" || lines[1] != "v1 vault; run claude-atlas adopt" {
		t.Fatalf("problem box %q", lines)
	}
	// A cluster counts its members before its pages, and its name wears the mark.
	cluster := registry.Entry{Kind: vault.Knowledge, Name: "papers", Path: "/v/papers", State: &registry.State{Heat: "warm", Pages: &four},
		Members: []registry.Ref{{ID: "a", Name: "ai-ml"}, {ID: "b", Name: "notes"}}}
	lines := boxLines(cluster)
	if lines[0] != "🌤️ "+clusterMark+"papers   open" || lines[1] != "2 members · 4 pages · new" {
		t.Fatalf("cluster box %q", lines)
	}
}

func TestMountLineSaysWhatTheLinkIs(t *testing.T) {
	p := registry.Entry{Kind: vault.Project, Name: "p3", Path: t.TempDir()}
	m := registry.Mount{ID: "kb1", Name: "ai-ml", Access: "write", Effective: "write", Path: "/v/ai-ml/wiki"}
	if got := mountLine(p, m, "ai-ml", false, 40); got != "╌╌╌╌▶ ai-ml   write · link missing" {
		t.Fatalf("missing link: %q", got)
	}
	if got := mountLine(p, m, "ai-ml", true, 40); got != "╌╌╌╌▶ ai-ml   write · cluster · link missing" {
		t.Fatalf("cluster mount: %q", got)
	}
	link := filepath.Join(p.Path, "kb", "ai-ml")
	os.MkdirAll(filepath.Dir(link), 0o755)
	os.Symlink("/v/ai-ml/wiki", link)
	if got := mountLine(p, m, "ai-ml", false, 40); got != "╌╌╌╌▶ ai-ml   write" {
		t.Fatalf("good link: %q", got)
	}
	os.Remove(link)
	os.Symlink("/elsewhere", link)
	if got := mountLine(p, m, "ai-ml", false, 40); got != "╌╌╌╌▶ ai-ml   write · link wrong" {
		t.Fatalf("wrong link: %q", got)
	}
	reduced := registry.Mount{ID: "kb2", Name: "papers", Access: "write", Effective: "read", Path: "/v/papers/wiki"}
	if got := mountLine(p, reduced, "papers", false, 40); got != "╌╌╌╌▶ papers   read (write not granted) · link missing" {
		t.Fatalf("reduced: %q", got)
	}
	renamed := registry.Mount{ID: "kb2", Name: "old", Access: "read", Effective: "read", Path: "/v/papers/wiki"}
	if got := mountLine(p, renamed, "papers", false, 40); !strings.HasPrefix(got, "╌╌╌╌▶ papers as kb/old   read") {
		t.Fatalf("renamed: %q", got)
	}
	gone := registry.Mount{ID: "x", Name: "x", Access: "write", Error: "no knowledge base with id x"}
	if got := mountLine(p, gone, "x", false, 40); got != "╌╌╌╌▶ no knowledge base with id x" {
		t.Fatalf("gone: %q", got)
	}
	// A long name gives way to the access and the link text, so the whole line fits.
	big := registry.Mount{ID: "kb3", Name: "a-very-long-knowledge-base-name", Access: "read", Effective: "read", Path: "/v/big/wiki"}
	long := strings.TrimPrefix(stripANSI(mountLine(p, big, "a-very-long-knowledge-base-name", false, 40)), arrowOut)
	if !strings.Contains(long, "a-very-long") || !strings.Contains(long, "…") || len([]rune(long)) > 40 {
		t.Fatalf("clipped: %q is %d runes", long, len([]rune(long)))
	}
}

func TestMountedByLinesCountThenList(t *testing.T) {
	kb := registry.Entry{Kind: vault.Knowledge, Name: "papers",
		MountedBy: []registry.Ref{{ID: "a", Name: "course", Access: "read"}, {ID: "b", Name: "p3", Access: "write"}},
		Grants:    []registry.Grant{{ID: "gone", Name: "gone", Access: "read", Error: "no project with id gone"}}}
	if got := mountedByLines(kb, false); len(got) != 1 || got[0] != "◀╌╌╌╌ 2 projects · 1 grant stale" {
		t.Fatalf("collapsed: %q", got)
	}
	want := []string{"◀╌╌╌╌ course   read", "◀╌╌╌╌ p3   write", "      grant  gone   read · no project with id gone"}
	if got := mountedByLines(kb, true); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("expanded: %q", got)
	}
	one := registry.Entry{Kind: vault.Knowledge, MountedBy: kb.MountedBy[:1]}
	if got := mountedByLines(one, false); got[0] != "◀╌╌╌╌ 1 project" {
		t.Fatalf("one: %q", got)
	}
	none := registry.Entry{Kind: vault.Knowledge}
	if got := mountedByLines(none, false); got[0] != "◀╌╌╌╌ not mounted by any project" {
		t.Fatalf("none collapsed: %q", got)
	}
	if got := mountedByLines(none, true); len(got) != 1 || got[0] != "◀╌╌╌╌ not mounted by any project" {
		t.Fatalf("none expanded: %q", got)
	}
	// A cluster counts its members beside its projects, and lists them after them.
	cluster := registry.Entry{Kind: vault.Knowledge, Name: "papers", MountedBy: kb.MountedBy[:1],
		Members: []registry.Ref{{ID: "a", Name: "ai-ml"}, {ID: "b", Name: "gone", Error: "no knowledge base with id b"}}}
	if got := mountedByLines(cluster, false); len(got) != 1 || got[0] != "◀╌╌╌╌ 1 project · 2 members" {
		t.Fatalf("cluster collapsed: %q", got)
	}
	want = []string{"◀╌╌╌╌ course   read", "      ai-ml   member", "      gone   no knowledge base with id b"}
	if got := mountedByLines(cluster, true); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("cluster expanded: %q", got)
	}
}

func TestDetailLinesByKind(t *testing.T) {
	four, zero := 4, 0
	p := registry.Entry{ID: "id-p3", Kind: vault.Project, Name: "p3", Path: "/v/p3", Mode: vault.Generic, Created: "2026-09-01", Tags: []string{"itl"},
		Mounts: []registry.Mount{{ID: "id-ai-ml", Name: "ai-ml", Access: "write", Effective: "write", Path: "/v/ai-ml/wiki"}},
		Repos: []registry.Repo{{Name: "atlas", Path: "/code/atlas", Remote: "git@example.com:atlas.git", Changes: "pr"},
			{Name: "local", Path: "/code/local"}, {Name: "lost", Error: "no folder for lost"}},
		State: &registry.State{VaultOK: true, Heat: "hot", Pages: &four, DaysIdle: &zero, GeneratedAt: "2026-09-12T18:00:00Z", LastTouched: "2026-09-12",
			OpenThreads: []string{"thread"}, Tasks: &registry.TaskSummary{Counts: tasks.Counts{Open: 3, Active: 1, Planned: 2}}}}
	out := strings.Join(detailLines(p), "\n")
	for _, want := range []string{"Path", "/v/p3", "Created", "2026-09-01", "Vault check", "ok", "Last touched", "2026-09-12",
		"Open threads", "- thread", "Tasks", "3 open: 1 active",
		"Repositories", "atlas", "git@example.com:atlas.git", "local", "/code/local", "lost", "no folder for lost"} {
		if !strings.Contains(out, want) {
			t.Errorf("project detail missing %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"Mounts", "Id", "Mode", "Tags", "Heat", "Pages", "Unfinished", "Refreshed", "Signals", "changes: pr", "/code/atlas"} {
		if strings.Contains(out, gone) {
			t.Errorf("the block is short; %q belongs to show:\n%s", gone, out)
		}
	}
	if path, created := strings.Index(out, "Path"), strings.Index(out, "Created"); !(path < created && created < strings.Index(out, "Vault check") && strings.Index(out, "Tasks") < strings.Index(out, "Repositories")) {
		t.Errorf("order: path, created, vault check, last touched, open threads, tasks, repositories:\n%s", out)
	}
	kb := registry.Entry{Kind: vault.Knowledge, Name: "papers", Path: "/v/papers", Scope: "papers sources",
		Grants: []registry.Grant{{ID: "gone", Name: "gone", Access: "read", Error: "no project with id gone"}}}
	out = strings.Join(detailLines(kb), "\n")
	for _, want := range []string{"Scope", "papers sources", "Access", "open", "Grant", "gone  read  no project with id gone", "never refreshed; press R"} {
		if !strings.Contains(out, want) {
			t.Errorf("knowledge detail missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Tags") || strings.Contains(out, "Vault check") {
		t.Errorf("knowledge detail:\n%s", out)
	}
	// The relation reads from both sides: a cluster lists its members, a member its clusters.
	kb.Members = []registry.Ref{{ID: "a", Name: "ai-ml"}, {ID: "b", Name: "lost", Error: "no knowledge base with id b"}}
	kb.Clusters = []registry.Ref{{ID: "c", Name: "domain"}}
	out = strings.Join(detailLines(kb), "\n")
	for _, want := range []string{"In cluster", "domain", "Members", "ai-ml", "lost  no knowledge base with id b"} {
		if !strings.Contains(out, want) {
			t.Errorf("cluster detail missing %q:\n%s", want, out)
		}
	}
	bad := registry.Entry{Path: "/v/old", Error: "v1 vault; run claude-atlas adopt /v/old --as knowledge|project", Reason: registry.ReasonV1}
	out = strings.Join(detailLines(bad), "\n")
	for _, want := range []string{"Path", "/v/old", "Reason", "v1", "Fix", "press a to adopt it"} {
		if !strings.Contains(out, want) {
			t.Errorf("problem detail missing %q:\n%s", want, out)
		}
	}
	gone := registry.Entry{Path: "/v/gone", Error: "not found", Reason: registry.ReasonMissing}
	if out := strings.Join(detailLines(gone), "\n"); !strings.Contains(out, "press e then r to forget it") {
		t.Errorf("missing folder detail:\n%s", out)
	}
}
