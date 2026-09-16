package tui

import (
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

func TestKindColorsAndBoxes(t *testing.T) {
	if kindColor(vault.Project) == kindColor(vault.Knowledge) {
		t.Fatal("the kinds share a color")
	}
	if got := boxStyle(vault.Knowledge, true).GetBorderTopForeground(); got != knowledgeColor {
		t.Fatalf("selected knowledge border %v", got)
	}
	if got := boxStyle(vault.Project, true).GetBorderTopForeground(); got != projectColor {
		t.Fatalf("selected project border %v", got)
	}
	if got := boxStyle(vault.Project, false).GetBorderTopForeground(); got != muted {
		t.Fatalf("unselected border %v", got)
	}
	if kindStyle(vault.Knowledge).GetForeground() != knowledgeColor || kindStyle(vault.Project).GetForeground() != projectColor {
		t.Fatal("kindStyle wears the wrong color")
	}
}

func TestFocusKeepsColorOnlyUnderTheCursor(t *testing.T) {
	styled := []string{"\x1b[34mname\x1b[0m │", "\x1b[33mdetail\x1b[0m"}
	if got := focus(styled, false); got[0] != "name │" || got[1] != "detail" {
		t.Fatalf("a row not under the cursor is plain: %q", got)
	}
	if got := focus(styled, true); got[0] != styled[0] || got[1] != styled[1] {
		t.Fatalf("the row under the cursor keeps its colors: %q", got)
	}
}

func TestAHostedBoardHasNoHeaderAndNamesTheTabs(t *testing.T) {
	hooks := Hooks{Tasks: func(registry.Entry) (tasks.Ledger, []string, error) { return tasks.Empty(), nil, nil }}
	items := []Item{{Entry: registry.Entry{Kind: vault.Project, Name: "p3", Path: "/v/p3"}}}
	s := newTasks(hooks, Opener{}, nil, items, 80)
	if out := s.view(); !strings.Contains(out, "Atlas") || !strings.Contains(out, "Esc back") {
		t.Fatalf("a board of its own has a header and Esc:\n%s", out)
	}
	s.hosted = true
	out := s.view()
	if strings.Contains(out, "Atlas") || strings.Contains(out, "Esc back") || !strings.Contains(out, "p plant") {
		t.Fatalf("hosted:\n%s", out)
	}
	s.quiet = true
	if out := s.view(); strings.Contains(out, "p plant") {
		t.Fatalf("quiet drops the hints:\n%s", out)
	}
}
