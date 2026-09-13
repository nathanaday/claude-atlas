package tree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func rels(projects []*Project) string {
	out := make([]string, len(projects))
	for i, p := range projects {
		out[i] = p.Rel
	}
	return strings.Join(out, ",")
}

func TestSlugify(t *testing.T) {
	got, err := Slugify("Sensor Triage (2026)")
	if err != nil || got != "sensor-triage-2026" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := Slugify("!!!"); err == nil {
		t.Fatal("expected an error for an empty slug")
	}
}

func TestCreateUnderCategoryMakesDirectories(t *testing.T) {
	root := t.TempDir()
	path, err := Create(root, ProjectOptions{ID: "capstone", Name: "Capstone", Vault: "/v", Category: "university/cs566"})
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, "university", "cs566", "capstone.md") {
		t.Fatalf("unexpected path %s", path)
	}
	projects, problems, err := Walk(root)
	if err != nil || len(problems) != 0 {
		t.Fatalf("walk: %v %v", err, problems)
	}
	if rels(projects) != "university/cs566/capstone" {
		t.Fatalf("got %s", rels(projects))
	}
	p := projects[0]
	if p.ID() != "capstone" || p.Category() != "university/cs566" || p.Name != "Capstone" || p.VaultPath() != "/v" {
		t.Fatalf("got %+v", p)
	}
}

func TestWalkSortsAndSkipsHiddenDirectories(t *testing.T) {
	root := t.TempDir()
	for _, spec := range []struct{ id, cat, vault string }{{"zeta", "", "/1"}, {"alpha", "b", "/2"}, {"mid", "a", "/3"}} {
		if _, err := Create(root, ProjectOptions{ID: spec.id, Name: spec.id, Vault: spec.vault, Category: spec.cat}); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(root, ".obsidian"), 0o755)
	os.WriteFile(filepath.Join(root, ".obsidian", "note.md"), []byte("x"), 0o644)
	projects, problems, err := Walk(root)
	if err != nil || len(problems) != 0 {
		t.Fatalf("walk: %v %v", err, problems)
	}
	if rels(projects) != "a/mid,b/alpha,zeta" {
		t.Fatalf("order %s", rels(projects))
	}
	if p := projects[2]; p.Category() != "" {
		t.Fatalf("top-level category should be empty, got %q", p.Category())
	}
}

func TestCreateRefusesDuplicateAndEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, ProjectOptions{ID: "a", Name: "a", Vault: "/v"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, ProjectOptions{ID: "a", Name: "a", Vault: "/v2"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	if _, err := Create(root, ProjectOptions{ID: "b", Name: "b", Vault: "/v", Category: "../out"}); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := Create(root, ProjectOptions{ID: "b", Name: "b", Vault: "/v", Priority: "urgent"}); err == nil {
		t.Fatal("expected priority error")
	}
}

func TestWalkReportsProblemsInsteadOfFailing(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, ProjectOptions{ID: "good", Name: "good", Vault: "/v"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, ProjectOptions{ID: "twin", Name: "twin", Vault: "/v"}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "plain.md"), []byte("# just notes\n"), 0o644)
	os.WriteFile(filepath.Join(root, "novault.md"), []byte("---\nschema: atlas.project.v1\n---\n"), 0o644)
	os.WriteFile(filepath.Join(root, "badprio.md"), []byte("---\nschema: atlas.project.v1\nvault: /x\npriority: urgent\n---\n"), 0o644)
	projects, problems, err := Walk(root)
	if err != nil {
		t.Fatal(err)
	}
	if rels(projects) != "good" {
		t.Fatalf("projects %s", rels(projects))
	}
	got := map[string]string{}
	for _, p := range problems {
		got[p.Rel] = p.Reason
	}
	for rel, want := range map[string]string{"plain": "missing frontmatter", "novault": "vault", "badprio": "priority", "twin": "same vault as good"} {
		if !strings.Contains(got[rel], want) {
			t.Errorf("%s: want %q in %q", rel, want, got[rel])
		}
	}
}

func TestWalkOfMissingRootIsEmpty(t *testing.T) {
	projects, problems, err := Walk(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(projects) != 0 || len(problems) != 0 {
		t.Fatalf("got %v %v %v", projects, problems, err)
	}
}

func TestFindHelpers(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, ProjectOptions{ID: "a", Name: "a", Vault: "/v", Category: "x"}); err != nil {
		t.Fatal(err)
	}
	projects, _, _ := Walk(root)
	if FindByVault(projects, "/v").Rel != "x/a" || FindByRel(projects, "x/a").ID() != "a" || FindByRel(projects, "a").Rel != "x/a" || FindByRel(projects, "x/a.md").Rel != "x/a" {
		t.Fatal("lookup failed")
	}
	if FindByRel(projects, "missing") != nil || FindByVault(projects, "/nope") != nil {
		t.Fatal("expected nil for unknown")
	}
}

func TestLoadDefaultsAndBody(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "n.md")
	os.WriteFile(path, []byte("---\nschema: atlas.project.v1\nvault: ~/v\n---\n\n# Notes\n"), 0o644)
	p, err := Load(path, root)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "n" || p.Priority != "normal" || p.State != "active" || p.Rel != "n" {
		t.Fatalf("defaults not applied: %+v", p)
	}
	if strings.HasPrefix(p.VaultPath(), "~") {
		t.Fatal("vault path not expanded")
	}
	if p.Body != "# Notes\n" {
		t.Fatalf("body %q", p.Body)
	}
}

func TestRenderRoundTrip(t *testing.T) {
	front := Frontmatter{Schema: ProjectSchema, Name: "N: with colon", Vault: "/v", Purpose: "Two\nlines", Priority: "high", State: "blocked", BlockedOn: "hw", Repos: []string{"https://x"}}
	data, err := Render(front, "body")
	if err != nil {
		t.Fatal(err)
	}
	head, body, ok := SplitFrontmatter(string(data))
	if !ok || body != "body\n" {
		t.Fatalf("split failed: ok=%v body=%q", ok, body)
	}
	var back Frontmatter
	if err := yaml.Unmarshal([]byte(head), &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != front.Name || back.Purpose != front.Purpose || back.Repos[0] != "https://x" || back.State != "blocked" {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	if _, body, ok := SplitFrontmatter("no front\n"); ok || body != "no front\n" {
		t.Fatal("plain text should not split")
	}
	if front, body, ok := SplitFrontmatter("---\n---\n\nrest\n"); !ok || front != "" || body != "rest\n" {
		t.Fatalf("empty front: %q %q %v", front, body, ok)
	}
	if front, _, ok := SplitFrontmatter("---\na: 1\n---"); !ok || front != "a: 1" {
		t.Fatalf("eof front: %q %v", front, ok)
	}
}

func TestStateRoundTripMirrorsTree(t *testing.T) {
	dir := t.TempDir()
	one := 1
	if err := WriteState(dir, "uni/cs566/capstone", &State{Schema: StateSchema, Heat: "hot", DaysIdle: &one}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "uni", "cs566", "capstone.json")); err != nil {
		t.Fatal("state file not where expected")
	}
	state, err := ReadState(dir, "uni/cs566/capstone")
	if err != nil || state.Heat != "hot" || *state.DaysIdle != 1 || state.OpenThreads == nil || state.Pages != nil {
		t.Fatalf("got %+v, %v", state, err)
	}
	if _, err := ReadState(dir, "missing"); err == nil {
		t.Fatal("expected error for missing state")
	}
}

func TestCategoriesMayContainDotsButNotClimb(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, ProjectOptions{ID: "a", Name: "A", Vault: "/v/a", Category: "v1..v2"}); err != nil {
		t.Fatalf("dots inside a name are fine: %v", err)
	}
	if _, err := Create(root, ProjectOptions{ID: "b", Name: "B", Vault: "/v/b", Category: "../outside"}); err == nil {
		t.Fatal("a category must not climb out of the tree")
	}
}
