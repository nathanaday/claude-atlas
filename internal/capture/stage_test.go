package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStagePlansOnlyNewFiles(t *testing.T) {
	v := newVault(t)
	src := filepath.Join(t.TempDir(), "Papers")
	os.MkdirAll(filepath.Join(src, "2024", ".hidden"), 0o755)
	os.WriteFile(filepath.Join(src, "a.pdf"), []byte("aaa"), 0o644)
	os.WriteFile(filepath.Join(src, "2024", "b.md"), []byte("bbb"), 0o644)
	os.WriteFile(filepath.Join(src, ".DS_Store"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(src, "2024", ".hidden", "c.md"), []byte("ccc"), 0o644)

	plan, err := PlanStage(v, []string{src}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.New) != 2 || plan.New[0].To != "inbox/Papers/2024/b.md" || plan.New[1].To != "inbox/Papers/a.pdf" || len(plan.Dirs) != 1 {
		t.Fatalf("plan %+v", plan)
	}
	res, err := ApplyStage(v, plan, now)
	if err != nil || len(res.Staged) != 2 {
		t.Fatalf("apply %+v %v", res, err)
	}
	if data, _ := os.ReadFile(v.Path("inbox/Papers/a.pdf")); string(data) != "aaa" {
		t.Fatal("bytes must match")
	}
	if _, err := os.Stat(filepath.Join(src, "a.pdf")); err != nil {
		t.Fatal("the original must stay")
	}
	if from := StagedFrom(v); from[filepath.Join(src, "a.pdf")] == "" {
		t.Fatalf("map %v", from)
	}

	// Staging again: both files are waiting in the inbox, so nothing is new.
	plan, _ = PlanStage(v, []string{src}, now)
	if len(plan.New) != 0 || len(plan.Unchanged) != 2 || plan.Waiting != 2 {
		t.Fatalf("second plan %+v", plan)
	}

	// After ingest (capture) and inbox removal, unchanged files still count as known.
	if _, err := Capture(v, []string{"inbox/Papers/a.pdf"}, now); err != nil {
		t.Fatal(err)
	}
	os.Remove(v.Path("inbox/Papers/a.pdf"))
	os.WriteFile(filepath.Join(src, "2024", "b.md"), []byte("bbb v2"), 0o644)
	os.WriteFile(filepath.Join(src, "d.md"), []byte("ddd"), 0o644)
	plan, _ = PlanStage(v, []string{src}, now)
	var to []string
	for _, f := range plan.New {
		to = append(to, f.To)
	}
	if strings.Join(to, ",") != "inbox/Papers/2024/b (2).md,inbox/Papers/d.md" || len(plan.Unchanged) != 1 {
		t.Fatalf("third plan new=%v unchanged=%v", to, plan.Unchanged)
	}
}

func TestStageRefusesVaultPathsAndMissingSources(t *testing.T) {
	v := newVault(t)
	if _, err := PlanStage(v, nil, now); err == nil {
		t.Fatal("no sources")
	}
	if _, err := PlanStage(v, []string{v.Path("wiki")}, now); err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Fatalf("inside: %v", err)
	}
	if _, err := PlanStage(v, []string{filepath.Join(t.TempDir(), "nope.md")}, now); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing: %v", err)
	}
	single := filepath.Join(t.TempDir(), "note.md")
	os.WriteFile(single, []byte("n"), 0o644)
	plan, err := PlanStage(v, []string{single}, now)
	if err != nil || len(plan.New) != 1 || plan.New[0].To != "inbox/note.md" || len(plan.Dirs) != 0 {
		t.Fatalf("single file %+v %v", plan, err)
	}
	other := newVault(t)
	if _, err := ApplyStage(other, plan, now); err == nil {
		t.Fatal("a plan applies only to its own vault")
	}
}
