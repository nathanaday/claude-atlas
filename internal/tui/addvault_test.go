package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

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

func TestAddFormPathFollowsTheKindAndTheName(t *testing.T) {
	root := t.TempDir()
	m := newModel(root, vault.Project)
	m = press(m, tea.KeyEnter) // kind: project
	if m.step() != stepName {
		t.Fatalf("step %d", m.step())
	}
	m = typeText(m, "notes")
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter) // name, mode, tags
	if m.step() != stepPath || m.path.value() != home.Display(filepath.Join(root, "projects", "notes")) {
		t.Fatalf("step %d path %q", m.step(), m.path.value())
	}
	// Back to the kind: a knowledge base goes under knowledge/ instead.
	m = press(m, tea.KeyEsc, tea.KeyEsc, tea.KeyEsc, tea.KeyEsc)
	if m.step() != stepKind {
		t.Fatalf("esc should walk back, step %d", m.step())
	}
	m = press(m, tea.KeyRight, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	if m.kind != vault.Knowledge || m.step() != stepPath || m.path.value() != home.Display(filepath.Join(root, "knowledge", "notes")) {
		t.Fatalf("kind %q step %d path %q", m.kind, m.step(), m.path.value())
	}
	// A typed path stays, whatever the kind and the name say.
	elsewhere := filepath.Join(t.TempDir(), "notes")
	m.path.setValue("")
	m = typeText(m, elsewhere)
	m = press(m, tea.KeyEsc, tea.KeyEsc, tea.KeyEsc, tea.KeyEsc, tea.KeyLeft, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	if m.kind != vault.Project || m.step() != stepPath || m.path.value() != elsewhere {
		t.Fatalf("a typed path should stay: %q", m.path.value())
	}
	m = press(m, tea.KeyEnter)
	if m.step() != stepConfirm || !strings.Contains(m.View(), elsewhere) {
		t.Fatalf("step %d\n%s", m.step(), m.View())
	}
}

func TestAddFormRefusesBadNamesAndTakenPaths(t *testing.T) {
	root := t.TempDir()
	taken := filepath.Join(root, "projects", "taken")
	os.MkdirAll(taken, 0o755)
	os.WriteFile(filepath.Join(taken, ".claude-atlas.json"), []byte("{}"), 0o644)
	m := press(newModel(root, vault.Project), tea.KeyEnter)
	m = press(m, tea.KeyEnter) // an empty name is refused
	if m.step() != stepName || m.err == "" {
		t.Fatalf("empty name: step %d err %q", m.step(), m.err)
	}
	m = typeText(m, "a/b")
	m = press(m, tea.KeyEnter)
	if m.step() != stepName || !strings.Contains(m.err, "/") {
		t.Fatalf("a name is not a path: step %d err %q", m.step(), m.err)
	}
	m.name.SetValue("taken")
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	if m.step() != stepPath || !strings.Contains(m.err, "already exists") {
		t.Fatalf("taken path: step %d err %q", m.step(), m.err)
	}
	m.path.setValue(filepath.Join(taken, "inner"))
	m = press(m, tea.KeyEnter)
	if m.step() != stepPath || !strings.Contains(m.err, "inside the vault") || !strings.Contains(m.View(), "inside the vault") {
		t.Fatalf("nested vault: step %d err %q\n%s", m.step(), m.err, m.View())
	}
}

// The temp directory a test writes into carries the test's own name, and the form shows
// that path, so this name avoids the words the assertions look for.
func TestAddFormCarriesWhatTheKindNeeds(t *testing.T) {
	root := t.TempDir()
	m := press(newModel(root, vault.Project), tea.KeyEnter)
	m = typeText(m, "Sensor Triage")
	m = press(m, tea.KeyEnter, tea.KeyEnter) // name, mode: generic
	if !strings.Contains(m.View(), "Tags") || strings.Contains(m.View(), "Scope") {
		t.Fatalf("a project is asked for tags:\n%s", m.View())
	}
	m = typeText(m, " usc , fall ")
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter) // tags, path, confirm
	r := m.result()
	if r == nil || r.Kind != vault.Project || r.Name != "Sensor Triage" || r.Mode != "generic" || r.Adopt ||
		strings.Join(r.Tags, ",") != "usc,fall" || r.Scope != "" || r.Path != filepath.Join(root, "projects", "Sensor Triage") {
		t.Fatalf("project result %+v", r)
	}

	m = press(newModel(root, vault.Knowledge), tea.KeyEnter)
	m = typeText(m, "ai-ml")
	m = press(m, tea.KeyEnter, tea.KeyRight, tea.KeyEnter) // name; mode: lyt
	if !strings.Contains(m.View(), "Scope") || strings.Contains(m.View(), "Tags") {
		t.Fatalf("a knowledge base is asked for a scope:\n%s", m.View())
	}
	m = typeText(m, "Retrieval papers.")
	m = press(m, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter)
	r = m.result()
	if r == nil || r.Kind != vault.Knowledge || r.Mode != "lyt" || r.Scope != "Retrieval papers." || len(r.Tags) != 0 ||
		r.Path != filepath.Join(root, "knowledge", "ai-ml") {
		t.Fatalf("knowledge result %+v", r)
	}
}

func TestAdoptFormValidatesThePathAndAsksTheKind(t *testing.T) {
	m := newAdoptModel()
	if m.step() != stepPath || m.pathError() != "type the vault's path" {
		t.Fatalf("step %d err %q", m.step(), m.pathError())
	}
	plain := t.TempDir()
	m.path.setValue(plain)
	if !strings.Contains(m.pathError(), "not a vault") {
		t.Fatalf("plain dir: %q", m.pathError())
	}
	m.path.setValue(filepath.Join(plain, "missing"))
	if !strings.Contains(m.pathError(), "not a directory") {
		t.Fatalf("missing: %q", m.pathError())
	}
	old := filepath.Join(plain, "My Vault")
	os.MkdirAll(filepath.Join(old, "wiki"), 0o755)
	m.path.setValue(old)
	m = press(m, tea.KeyEnter)
	if m.step() != stepKind {
		t.Fatalf("step %d err %q", m.step(), m.err)
	}
	m = press(m, tea.KeyRight, tea.KeyEnter) // kind: knowledge
	if m.step() != stepName || m.name.Value() != "My Vault" {
		t.Fatalf("the folder's name is the default: step %d name %q", m.step(), m.name.Value())
	}
	m = press(m, tea.KeyEnter, tea.KeyEnter)
	if m.step() != stepConfirm || !strings.Contains(m.View(), "adopt this vault") {
		t.Fatalf("step %d\n%s", m.step(), m.View())
	}
	m = press(m, tea.KeyEnter)
	r := m.result()
	if r == nil || !r.Adopt || r.Kind != vault.Knowledge || r.Name != "My Vault" || r.Path != old || r.Mode != "generic" {
		t.Fatalf("result %+v", r)
	}
}

func TestAdoptPathCompletes(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Vaults", "old"), 0o755)
	os.MkdirAll(filepath.Join(root, "Videos"), 0o755)
	m := newAdoptModel()
	m = typeText(m, filepath.Join(root, "V"))
	if got := m.path.input.MatchedSuggestions(); len(got) != 2 || got[0] != filepath.Join(root, "Vaults")+"/" {
		t.Fatalf("suggestions %v", got)
	}
	m = press(m, tea.KeyTab)
	if m.path.value() != filepath.Join(root, "Vaults")+"/" {
		t.Fatalf("tab should complete: %q", m.path.value())
	}
	if !strings.Contains(m.View(), "old/") {
		t.Fatalf("matches should show under the line:\n%s", m.View())
	}
	m = press(m, tea.KeyTab)
	if m.path.value() != filepath.Join(root, "Vaults", "old")+"/" {
		t.Fatalf("second tab: %q", m.path.value())
	}
}

func TestAddFormCancels(t *testing.T) {
	m := press(newModel(t.TempDir(), vault.Project), tea.KeyEsc)
	if !m.cancelled {
		t.Fatal("esc on the first step should cancel")
	}
	if m2 := press(newModel(t.TempDir(), vault.Project), tea.KeyEnter, tea.KeyCtrlC); !m2.cancelled {
		t.Fatal("ctrl+c should cancel")
	}
}
