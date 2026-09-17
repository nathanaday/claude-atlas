package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// M on a knowledge base opens its members: a picker adds one, x drops one, and the
// knowledge base's own identity file is what changes.
// C makes a cluster: the add screen opens on the third kind, and the new vault's members
// screen opens the moment it exists, which is where a member is added.
func TestCNewClusterLandsOnItsMembers(t *testing.T) {
	cfg, _, hooks := atlasFixture(t)
	// The add screen needs a Create; this one writes the vault the way the CLI does.
	hooks.Create = func(c AddVault) (string, error) {
		if _, err := vault.Init(c.Path, vault.Options{Kind: c.Kind, Name: c.Name}, testNow); err != nil {
			return "", err
		}
		if c.Scope != "" {
			_, err := vaults.EditIdentity(home.Home{}, cfg, registry.Entry{Path: c.Path, Kind: c.Kind}, vaults.Edit{Scope: &c.Scope}, testNow)
			return c.Path, err
		}
		return c.Path, nil
	}
	v := pressV(openView(t, hooks), tea.KeyRight) // the Knowledge tab
	if out := v.View(); !strings.Contains(out, "C new cluster") {
		t.Fatalf("the Knowledge tab names the key:\n%s", out)
	}
	v = keyV(v, "C")
	if v.add == nil || !v.add.cluster || v.add.kind != vault.Knowledge {
		t.Fatalf("C opens the add screen on cluster: %+v", v.add)
	}
	out := v.View()
	for _, want := range []string{"◂ cluster ▸", "gathers other knowledge bases", "←→ project, knowledge base, or cluster"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The kind cycles both ways: left from cluster is a knowledge base, and right wraps
	// round to a project.
	v = pressV(v, tea.KeyLeft)
	if v.add.cluster || v.add.kind != vault.Knowledge {
		t.Fatalf("left from cluster: kind=%q cluster=%v", v.add.kind, v.add.cluster)
	}
	v = pressV(v, tea.KeyRight, tea.KeyRight)
	if v.add.kind != vault.Project || v.add.cluster {
		t.Fatalf("right from cluster wraps to a project: kind=%q cluster=%v", v.add.kind, v.add.cluster)
	}
	v = pressV(v, tea.KeyRight, tea.KeyRight) // back to cluster
	if !v.add.cluster {
		t.Fatalf("back to cluster: %+v", v.add)
	}

	v = pressV(v, tea.KeyEnter) // kind
	v = typeV(v, "p3")
	v = pressV(v, tea.KeyEnter, tea.KeyEnter) // name, mode
	v = typeV(v, "The ecosystem.")
	v = pressV(v, tea.KeyEnter, tea.KeyEnter) // scope, path
	if !strings.Contains(v.View(), "create this cluster, then choose its members") {
		t.Fatalf("the confirm line:\n%s", v.View())
	}
	v = pressV(v, tea.KeyEnter) // confirm

	if v.add != nil || !v.changed {
		t.Fatalf("the cluster should be made: add=%v changed=%v err=%q", v.add, v.changed, v.errMsg)
	}
	if v.cluster == nil {
		t.Fatalf("the members screen should open on it: err=%q\n%s", v.errMsg, v.View())
	}
	if out := v.View(); !strings.Contains(out, "p3   members") || !strings.Contains(out, "no members yet") {
		t.Fatalf("it lands on the new cluster's members:\n%s", out)
	}
	// It is an ordinary knowledge base until it has one, and the picker offers them.
	made := entryNamed(t, cfg, "p3")
	if made.Kind != vault.Knowledge || len(made.Members) != 0 || made.Scope != "The ecosystem." {
		t.Fatalf("identity file: %+v", made)
	}
	v = keyV(v, "a")
	if v.cluster.mode != clusterPick || len(v.cluster.picks) == 0 {
		t.Fatalf("a lists what it may gather: mode=%d picks=%+v", v.cluster.mode, v.cluster.picks)
	}
	for _, e := range v.cluster.picks {
		if e.Name == "p3" {
			t.Fatal("a cluster is not its own member")
		}
	}
}

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
