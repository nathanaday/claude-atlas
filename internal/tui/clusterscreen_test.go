package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// M on a knowledge base opens its members: a picker adds one, x drops one, and the
// knowledge base's own identity file is what changes.
func TestClusterScreenAddsAndRemovesMembers(t *testing.T) {
	cfg, _, hooks := atlasFixture(t)
	makeVault(t, cfg, vault.Knowledge, "notes", nil)
	v := keyV(findVault(t, openView(t, hooks), "ai-ml"), "M")
	if v.cluster == nil {
		t.Fatalf("M should open the members screen: err=%q", v.errMsg)
	}
	out := v.View()
	for _, want := range []string{"ai-ml   members", "no members yet", "a add · x drop"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// a lists the other knowledge bases, so a name is chosen and never typed.
	v = keyV(v, "a")
	if v.cluster.mode != clusterPick || len(v.cluster.picks) != 1 || v.cluster.picks[0].Name != "notes" {
		t.Fatalf("pick mode: mode=%d picks=%+v", v.cluster.mode, v.cluster.picks)
	}
	if out := v.View(); !strings.Contains(out, "Choose one") || !strings.Contains(out, "▸ notes") {
		t.Fatalf("the picker lists the knowledge bases:\n%s", out)
	}
	v = pressV(v, tea.KeyEnter)
	if v.cluster.mode != clusterList || len(v.cluster.rows) != 1 || !v.changed {
		t.Fatalf("after the add: mode=%d rows=%d changed=%v err=%q", v.cluster.mode, len(v.cluster.rows), v.changed, v.cluster.err)
	}
	if !strings.Contains(v.cluster.status, "added notes to ai-ml") {
		t.Fatalf("status: %q err=%q", v.cluster.status, v.cluster.err)
	}
	if out := v.View(); !strings.Contains(out, "╭") || !strings.Contains(out, "notes") {
		t.Fatalf("the member should have a box:\n%s", out)
	}
	if members := entryNamed(t, cfg, "ai-ml").Members; len(members) != 1 || members[0].Name != "notes" {
		t.Fatalf("identity file: %+v", members)
	}
	// x asks first; the knowledge base stays either way.
	v = keyV(v, "x")
	if v.cluster.mode != clusterAsk || !strings.Contains(v.View(), "Drop notes from ai-ml?") {
		t.Fatalf("confirm: mode=%d\n%s", v.cluster.mode, v.View())
	}
	v = keyV(v, "n")
	if v.cluster.mode != clusterList || len(v.cluster.rows) != 1 {
		t.Fatal("n should keep the member")
	}
	v = keyV(v, "x")
	v = keyV(v, "y")
	if v.cluster.mode != clusterList || len(v.cluster.rows) != 0 || !strings.Contains(v.cluster.status, "dropped notes from ai-ml") {
		t.Fatalf("after the drop: mode=%d rows=%d status=%q err=%q", v.cluster.mode, len(v.cluster.rows), v.cluster.status, v.cluster.err)
	}
	if members := entryNamed(t, cfg, "ai-ml").Members; len(members) != 0 {
		t.Fatalf("identity file: %+v", members)
	}
	if kb := entryNamed(t, cfg, "notes"); kb.Name != "notes" {
		t.Fatalf("the knowledge base stays: %+v", kb)
	}
	v = pressV(v, tea.KeyEsc)
	if v.cluster != nil {
		t.Fatal("esc should close the screen")
	}
	// A project holds no members, and neither side opens without the hooks.
	p := keyV(findVault(t, openView(t, hooks), "reading"), "M")
	if p.cluster != nil || p.errMsg != "a project holds no members" {
		t.Fatalf("M on a project: err=%q", p.errMsg)
	}
	none := keyV(findVault(t, newView(sample(), Opener{}, Hooks{}), "papers"), "M")
	if none.cluster != nil || !strings.Contains(none.errMsg, "not available") {
		t.Fatalf("M without hooks reports why: err=%q", none.errMsg)
	}
}

// boxLine is the second line of a vault's box: the line under the one that names it.
func boxLine(t *testing.T, lines []string, name string) string {
	t.Helper()
	for i, line := range lines {
		if strings.Contains(line, " "+name+"   ") && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	t.Fatalf("no box for %s in:\n%s", name, strings.Join(lines, "\n"))
	return ""
}

// A cluster's members show on the Knowledge tab, and a member names its cluster.
func TestTheKnowledgeTabShowsAClusterAndItsMembers(t *testing.T) {
	items := sample()
	items[3].Entry.Members = []registry.Ref{{ID: "id-ai-ml", Name: "ai-ml"}}
	items[4].Entry.Clusters = []registry.Ref{{ID: "id-papers", Name: "papers"}}
	v := findVault(t, newView(items, Opener{}, Hooks{}), "papers")
	out := v.View()
	for _, want := range []string{"◀╌╌╌╌ 1 project · 1 member", "cluster · 1 member"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The member count takes the page count's place; the ordinary knowledge base keeps it.
	if lines := strings.Split(out, "\n"); !strings.Contains(boxLine(t, lines, "papers"), "cluster · 1 member") ||
		strings.Contains(boxLine(t, lines, "papers"), "pages") || !strings.Contains(boxLine(t, lines, "ai-ml"), "4 pages") {
		t.Errorf("a cluster's box names its members in place of its pages:\n%s", out)
	}
	// Expanded, the connectors name the project that mounts it and then the member.
	v = pressV(v, tea.KeyEnter)
	out = v.View()
	for _, want := range []string{"◀╌╌╌╌ course   read", "ai-ml   member"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The relation reads from the member's side too.
	m := pressV(findVault(t, newView(items, Opener{}, Hooks{}), "ai-ml"), tea.KeyEnter)
	out = m.View()
	if !strings.Contains(out, "In cluster") || !strings.Contains(out, "papers") {
		t.Errorf("a member names its cluster:\n%s", out)
	}
}

// A project that mounts a cluster shows the cluster once, with its members under it.
func TestTheProjectsTabSummarisesAClusterMount(t *testing.T) {
	items := sample()
	items[1].Entry.Mounts = []registry.Mount{
		{ID: "id-papers", Name: "papers", Access: vault.AccessWrite, Effective: vault.AccessWrite, Path: "/v/papers/wiki"},
		{ID: "id-ai-ml", Name: "ai-ml", Access: vault.AccessWrite, Effective: vault.AccessWrite, Path: "/v/ai-ml/wiki", Through: "papers"},
		{ID: "id-notes", Name: "notes", Access: vault.AccessWrite, Effective: vault.AccessRead, Path: "/v/notes/wiki", Through: "papers"},
	}
	v := findVault(t, newView(items, Opener{}, Hooks{}), "p3")
	out := v.View()
	for _, want := range []string{"╌╌╌╌▶ papers   write · cluster", "through papers: ai-ml, notes"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"╌╌╌╌▶ ai-ml", "╌╌╌╌▶ notes"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a member takes no connector of its own; found %q in:\n%s", unwanted, out)
		}
	}
}
