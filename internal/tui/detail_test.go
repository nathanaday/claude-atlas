package tui

import (
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
)

func TestBoxLinesForEachKind(t *testing.T) {
	items := sample()
	kb := strings.Join(boxLines(items[0].Entry, 60), "\n")
	for _, want := range []string{"papers", "papers sources", "4 pages", "1 in inbox", "2 projects: webapp, firmware"} {
		if !strings.Contains(kb, want) {
			t.Errorf("knowledge box misses %q:\n%s", want, kb)
		}
	}
	pr := strings.Join(boxLines(items[2].Entry, 60), "\n")
	for _, want := range []string{"webapp", "/code/webapp", "the webapp work", "touched today", "3 tasks open", "phase: Alarm quality"} {
		if !strings.Contains(pr, want) {
			t.Errorf("project box misses %q:\n%s", want, pr)
		}
	}
	problem := strings.Join(boxLines(items[5].Entry, 60), "\n")
	if !strings.Contains(problem, "✗ gateway") || !strings.Contains(problem, "not found") {
		t.Errorf("problem box:\n%s", problem)
	}
	fresh := registry.Entry{Kind: registry.Project, Name: "new", Path: "/code/new"}
	if got := strings.Join(boxLines(fresh, 60), "\n"); !strings.Contains(got, "not refreshed") {
		t.Errorf("before a refresh:\n%s", got)
	}
}

func TestDetailLinesForAProject(t *testing.T) {
	e := sample()[2].Entry
	dirty := 2
	e.State.Git = &links.Link{OK: true, Branch: "main", Dirty: &dirty, LastCommit: "2026-09-16"}
	e.State.Tasks.Open = append(e.State.Tasks.Open, registry.TaskLine{ID: "task-20260917-0002", Title: "Blocked one", Status: "blocked", Stale: true})
	out := strings.Join(detailLines(e), "\n")
	for _, want := range []string{"Path", "/code/webapp", "Knowledge", "papers", "Git", "main · 2 uncommitted · last commit 2026-09-16", "Described", "2 commits behind", "Tasks", "3 open", "Phases", "Alarm quality → Launch", "[active] Filter vehicle false alarms", "Alarm quality", "[blocked] Blocked one", "stale", "Signal", "1 blocked task"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	e.State.Described = nil
	if out := strings.Join(detailLines(e), "\n"); !strings.Contains(out, registry.NotDescribed) {
		t.Errorf("an undescribed project says so:\n%s", out)
	}
	e.Knowledge = &registry.Ref{ID: "gone", Name: "old-kb", Error: "no knowledge base with id gone on this machine"}
	if out := strings.Join(detailLines(e), "\n"); !strings.Contains(out, "old-kb  no knowledge base with id gone") {
		t.Errorf("a missing knowledge base is named:\n%s", out)
	}
	e.State = nil
	if out := strings.Join(detailLines(e), "\n"); !strings.Contains(out, "never refreshed") {
		t.Errorf("no state:\n%s", out)
	}
}

func TestDetailLinesBoundTheOpenTasks(t *testing.T) {
	e := sample()[2].Entry
	e.State.Tasks.Open = nil
	for i := 0; i < maxDetailTasks+3; i++ {
		e.State.Tasks.Open = append(e.State.Tasks.Open, registry.TaskLine{Title: "t", Status: "planted"})
	}
	out := strings.Join(detailLines(e), "\n")
	if strings.Count(out, "[planted] t") != maxDetailTasks || !strings.Contains(out, "… and 3 more") {
		t.Errorf("bounded:\n%s", out)
	}
}

func TestTaskSummaryText(t *testing.T) {
	if got := taskSummaryText(&registry.TaskSummary{}); got != "none open" {
		t.Fatal(got)
	}
	if got := taskSummaryText(&registry.TaskSummary{Counts: tasks.Counts{Notes: 2}}); got != "none open · 2 notes waiting" {
		t.Fatal(got)
	}
	got := taskSummaryText(&registry.TaskSummary{Counts: tasks.Counts{Open: 2, Active: 1, Planted: 1, Stale: 1, Notes: 1}})
	if !strings.Contains(got, "2 open: 1 active · 0 blocked · 0 planned · 1 planted") || !strings.Contains(got, "1 stale") || !strings.Contains(got, "1 note waiting") {
		t.Fatal(got)
	}
}

func TestProblemFixNamesTheCommand(t *testing.T) {
	cases := map[string]string{
		registry.ReasonV1:         "claude-atlas adopt",
		registry.ReasonV2Project:  "claude-atlas init",
		registry.ReasonMissing:    "claude-atlas forget",
		registry.ReasonNotProject: "claude-atlas init",
		registry.ReasonSchema:     "update the binary",
	}
	for reason, want := range cases {
		if got := problemFix(registry.Entry{Path: "/x", Reason: reason, Error: "e"}); !strings.Contains(got, want) {
			t.Errorf("%s: %q lacks %q", reason, got, want)
		}
	}
}
