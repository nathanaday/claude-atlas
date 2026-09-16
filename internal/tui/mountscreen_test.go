package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/vault"
)

func TestMountsScreenMountsAndUnmounts(t *testing.T) {
	cfg, _, v := atlasView(t)
	v = keyV(v, "m")
	if v.mounts == nil || !strings.Contains(v.View(), "no mounts yet") || !strings.Contains(v.View(), "reading   mounts") {
		t.Fatalf("m should open the mounts screen:\n%s", v.View())
	}
	// A name no knowledge base carries is refused in place.
	v = keyV(v, "a")
	if v.mounts.mode != mountsPickKB {
		t.Fatalf("pick mode: %d", v.mounts.mode)
	}
	v.mounts.name.SetValue("nope")
	v = pressV(v, tea.KeyEnter)
	if v.mounts.mode != mountsPickKB || !strings.Contains(v.mounts.err, "no knowledge base named nope") {
		t.Fatalf("unknown name: mode=%d err=%q", v.mounts.mode, v.mounts.err)
	}
	v.mounts.name.SetValue("AI-ML")
	v = pressV(v, tea.KeyEnter)
	if v.mounts.mode != mountsPickAccess || !strings.Contains(v.View(), "w write") {
		t.Fatalf("access step: mode=%d\n%s", v.mounts.mode, v.View())
	}
	v = keyV(v, "r")
	if v.mounts.mode != mountsList || len(v.mounts.rows) != 1 || !v.changed || !strings.Contains(v.mounts.status, "mounted ai-ml as kb/ai-ml") {
		t.Fatalf("after mount: mode=%d rows=%d changed=%v status=%q err=%q", v.mounts.mode, len(v.mounts.rows), v.changed, v.mounts.status, v.mounts.err)
	}
	out := v.View()
	for _, want := range []string{"╭", "ai-ml", "read → read", "ok", "u unmount"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	project := entryNamed(t, cfg, "reading")
	if len(project.Mounts) != 1 || project.Mounts[0].Access != vault.AccessRead || project.Mounts[0].Name != "ai-ml" {
		t.Fatalf("identity file: %+v", project.Mounts)
	}
	target, err := os.Readlink(project.KbDir("ai-ml"))
	if err != nil || target != entryNamed(t, cfg, "ai-ml").Wiki() {
		t.Fatalf("symlink %q: %v", target, err)
	}
	// Unmount asks first; the knowledge base stays.
	v = keyV(v, "u")
	if v.mounts.mode != mountsConfirm || !strings.Contains(v.View(), "Unmount ai-ml?") {
		t.Fatalf("confirm:\n%s", v.View())
	}
	v = keyV(v, "n")
	if v.mounts.mode != mountsList || len(v.mounts.rows) != 1 {
		t.Fatal("n should keep the mount")
	}
	v = keyV(v, "u")
	v = keyV(v, "y")
	if v.mounts.mode != mountsList || len(v.mounts.rows) != 0 || !strings.Contains(v.mounts.status, "unmounted ai-ml") {
		t.Fatalf("after unmount: mode=%d rows=%d status=%q err=%q", v.mounts.mode, len(v.mounts.rows), v.mounts.status, v.mounts.err)
	}
	if _, err := os.Lstat(project.KbDir("ai-ml")); !os.IsNotExist(err) {
		t.Fatalf("the symlink should be gone: %v", err)
	}
	if mounts := entryNamed(t, cfg, "reading").Mounts; len(mounts) != 0 {
		t.Fatalf("identity file: %+v", mounts)
	}
	if kb := entryNamed(t, cfg, "ai-ml"); kb.Name != "ai-ml" {
		t.Fatalf("the knowledge base stays: %+v", kb)
	}
	v = pressV(v, tea.KeyEsc)
	if v.mounts != nil {
		t.Fatal("esc should close the screen")
	}
}

func TestMountsScreenNeedsHooksAndAProject(t *testing.T) {
	none := keyV(pressV(newView(sample(), Opener{}, Hooks{}), tea.KeyDown), "m")
	if none.mounts != nil || !strings.Contains(none.errMsg, "not available") {
		t.Fatalf("m without hooks reports why: err=%q", none.errMsg)
	}
	_, _, hooks := atlasFixture(t)
	kb := pressV(openView(t, hooks), tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown)
	if r := kb.current(); r == nil || r.item.Entry.Kind != vault.Knowledge {
		t.Fatalf("cursor on %+v", kb.current())
	}
	kb = keyV(kb, "m")
	if kb.mounts != nil || !strings.Contains(kb.errMsg, "press m on a project") {
		t.Fatalf("a knowledge base is mounted by projects: err=%q", kb.errMsg)
	}
}
