package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func labels(opts []option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.label
		if o.create {
			out[i] = "+" + o.label
		}
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCategoryOptions(t *testing.T) {
	known := []string{"personal", "university", "university/cs566"}
	if got := labels(categoryOptions(known, "")); !equal(got, []string{topLevel, "personal", "university", "university/cs566"}) {
		t.Fatalf("empty filter: %v", got)
	}
	if got := labels(categoryOptions(known, "CS5")); !equal(got, []string{"university/cs566", "+CS5"}) {
		t.Fatalf("partial match offers create: %v", got)
	}
	if got := labels(categoryOptions(known, "personal")); !equal(got, []string{"personal"}) {
		t.Fatalf("exact match offers no create: %v", got)
	}
	if got := labels(categoryOptions(known, " work/ ")); !equal(got, []string{"+work"}) {
		t.Fatalf("new category is trimmed: %v", got)
	}
}

func TestCategoriesListsNestedDirectoriesAndSkipsHidden(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"university/cs566", "personal", ".obsidian/plugins", "university/.hidden"} {
		os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
	if got := Categories(root); !equal(got, []string{"personal", "university", "university/cs566"}) {
		t.Fatalf("got %v", got)
	}
	if got := Categories(filepath.Join(root, "missing")); len(got) != 0 {
		t.Fatalf("missing root: %v", got)
	}
}

func TestModelValidatesNameAndBuildsPaths(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "taken"), 0o755)
	m := newModel(root, []string{"work"})
	if m.nameError() == "" {
		t.Fatal("empty name should error")
	}
	m.name.SetValue("!!!")
	if m.nameError() == "" {
		t.Fatal("unsluggable name should error")
	}
	m.name.SetValue("Taken")
	if m.nameError() == "" {
		t.Fatal("existing path should error")
	}
	m.name.SetValue("Sensor Triage")
	if m.nameError() != "" || m.slug() != "sensor-triage" || m.path() != filepath.Join(root, "sensor-triage") {
		t.Fatalf("name handling: err=%q slug=%q path=%q", m.nameError(), m.slug(), m.path())
	}
	if m.pagePath() != "tree/sensor-triage.md" {
		t.Fatalf("top-level page path %q", m.pagePath())
	}
	m.chosen = option{value: "work"}
	if m.pagePath() != "tree/work/sensor-triage.md" {
		t.Fatalf("category page path %q", m.pagePath())
	}
}

func press(m model, keys ...tea.KeyType) model {
	for _, k := range keys {
		next, _ := m.Update(tea.KeyMsg{Type: k})
		m = next.(model)
	}
	return m
}

func typeText(m model, text string) model {
	for _, r := range text {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(model)
	}
	return m
}

func TestFlowCreatesUnderNewCategory(t *testing.T) {
	root := t.TempDir()
	m := newModel(root, []string{"personal", "university/cs566"})
	m = press(m, tea.KeyEnter) // empty name is refused
	if m.step != stepName || m.err == "" {
		t.Fatalf("empty name accepted: step=%d err=%q", m.step, m.err)
	}
	m = typeText(m, "Sensor Triage")
	m = press(m, tea.KeyEnter)
	if m.step != stepCategory {
		t.Fatalf("expected category step, got %d", m.step)
	}
	t.Logf("\n%s", m.View())
	m = typeText(m, "work")
	t.Logf("\n%s", m.View())
	m = press(m, tea.KeyEnter)
	if m.step != stepMode || !m.chosen.create || m.chosen.value != "work" {
		t.Fatalf("expected new category work, got step=%d chosen=%+v", m.step, m.chosen)
	}
	m = press(m, tea.KeyRight)
	if m.mode != "lyt" || !strings.Contains(m.View(), "◂ lyt ▸") {
		t.Fatalf("mode toggle: %q\n%s", m.mode, m.View())
	}
	m = press(m, tea.KeyLeft, tea.KeyEnter)
	if m.step != stepPurpose || m.mode != "generic" {
		t.Fatalf("expected purpose step in generic mode, got step=%d mode=%q", m.step, m.mode)
	}
	m = typeText(m, "Sort sensors.")
	m = press(m, tea.KeyEnter)
	if m.step != stepConfirm {
		t.Fatalf("expected confirm step, got %d", m.step)
	}
	t.Logf("\n%s", m.View())
	m = press(m, tea.KeyEnter)
	if !m.done || m.pagePath() != "tree/work/sensor-triage.md" {
		t.Fatalf("done=%v page=%s", m.done, m.pagePath())
	}
	r := m.result()
	if r == nil || r.Name != "Sensor Triage" || r.Mode != "generic" || r.Purpose != "Sort sensors." || r.Adopt {
		t.Fatalf("result %+v", r)
	}
}

func TestAdoptModelValidatesPath(t *testing.T) {
	m := newAdoptModel([]string{"work"})
	if m.nameError() != "type the vault's path" {
		t.Fatalf("empty: %q", m.nameError())
	}
	plain := t.TempDir()
	m.where.setValue(plain)
	if !strings.Contains(m.nameError(), "not a vault") {
		t.Fatalf("plain dir: %q", m.nameError())
	}
	m.where.setValue(filepath.Join(plain, "missing"))
	if !strings.Contains(m.nameError(), "not a directory") {
		t.Fatalf("missing: %q", m.nameError())
	}
	old := filepath.Join(plain, "My Vault")
	os.MkdirAll(filepath.Join(old, "wiki"), 0o755)
	m.where.setValue(old)
	if m.nameError() != "" || m.slug() != "my-vault" || m.path() != old {
		t.Fatalf("adoptable: err=%q slug=%q path=%q", m.nameError(), m.slug(), m.path())
	}
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	if m.step != stepConfirm || !strings.Contains(m.View(), "adopt this vault") {
		t.Fatalf("step %d\n%s", m.step, m.View())
	}
	m = press(m, tea.KeyEnter)
	r := m.result()
	if r == nil || !r.Adopt || r.Name != "My Vault" || r.Path != old {
		t.Fatalf("result %+v", r)
	}
}

func TestAdoptPathCompletes(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Vaults", "old"), 0o755)
	os.MkdirAll(filepath.Join(root, "Videos"), 0o755)
	m := newAdoptModel(nil)
	m = typeText(m, filepath.Join(root, "V"))
	if got := m.where.input.MatchedSuggestions(); len(got) != 2 || got[0] != filepath.Join(root, "Vaults")+"/" {
		t.Fatalf("suggestions %v", got)
	}
	m = press(m, tea.KeyTab)
	if m.where.value() != filepath.Join(root, "Vaults")+"/" {
		t.Fatalf("tab should complete: %q", m.where.value())
	}
	if !strings.Contains(m.View(), "old/") {
		t.Fatalf("matches should show under the line:\n%s", m.View())
	}
	m = press(m, tea.KeyTab)
	if m.where.value() != filepath.Join(root, "Vaults", "old")+"/" {
		t.Fatalf("second tab: %q", m.where.value())
	}
}

func TestFlowBackAndPickExistingCategory(t *testing.T) {
	m := newModel(t.TempDir(), []string{"personal", "university/cs566"})
	m = typeText(m, "notes")
	m = press(m, tea.KeyEnter, tea.KeyDown, tea.KeyDown) // top level → personal → university/cs566
	m = press(m, tea.KeyEnter)
	if m.chosen.value != "university/cs566" || m.chosen.create {
		t.Fatalf("chosen %+v", m.chosen)
	}
	m = press(m, tea.KeyEsc) // back to category
	if m.step != stepCategory {
		t.Fatalf("esc should go back, step=%d", m.step)
	}
	m = press(m, tea.KeyEsc, tea.KeyEsc) // back to name, then cancel
	if !m.cancelled {
		t.Fatal("esc on the first step should cancel")
	}
	m2 := press(newModel(t.TempDir(), nil), tea.KeyCtrlC)
	if !m2.cancelled {
		t.Fatal("ctrl+c should cancel")
	}
}
