package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPathSuggestions(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Documents", "Downloads", ".hidden"} {
		os.MkdirAll(filepath.Join(root, name), 0o755)
	}
	os.WriteFile(filepath.Join(root, "notes.md"), nil, 0o644)
	got := pathSuggestions(root+"/", nil)
	if strings.Join(got, " ") != root+"/Documents/ "+root+"/Downloads/ "+root+"/notes.md" {
		t.Fatalf("got %v", got)
	}
	if got := pathSuggestions(root+"/do", nil); len(got) != 2 {
		t.Fatalf("case-insensitive prefix: %v", got)
	}
	if got := pathSuggestions(root+"/.h", nil); len(got) != 1 || !strings.HasSuffix(got[0], ".hidden/") {
		t.Fatalf("hidden entries only when asked: %v", got)
	}
	if got := pathSuggestions("co", []string{"code", "Course"}); strings.Join(got, " ") != "code Course" {
		t.Fatalf("names: %v", got)
	}
	if got := pathSuggestions("~", nil); len(got) != 1 || got[0] != "~/" {
		t.Fatalf("tilde: %v", got)
	}
	if got := pathSuggestions("~/", nil); len(got) == 0 {
		t.Fatal("home should list")
	}
}

func TestPathFieldCompletesWithTab(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Papers", "2026"), 0o755)
	f := newPathField("", 40)
	f.focus()
	for _, r := range root + "/P" {
		f, _ = f.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if !strings.Contains(f.view(""), "Papers/") {
		t.Fatalf("match line:\n%s", f.view(""))
	}
	f, _ = f.update(tea.KeyMsg{Type: tea.KeyTab})
	if f.value() != root+"/Papers/" {
		t.Fatalf("after tab %q", f.value())
	}
	f, _ = f.update(tea.KeyMsg{Type: tea.KeyTab})
	if f.value() != root+"/Papers/2026/" {
		t.Fatalf("second tab %q", f.value())
	}
	f.names = []string{"code"}
	f.setValue("")
	if !strings.Contains(f.view(""), "code") {
		t.Fatal("names show when the field is empty")
	}
}
