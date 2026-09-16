package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

func TestMountsScreenMountsAndUnmounts(t *testing.T) {
	cfg, _, v := atlasView(t)
	v = keyV(v, "m")
	if v.mounts == nil || !strings.Contains(v.View(), "no mounts yet") || !strings.Contains(v.View(), "reading   mounts") {
		t.Fatalf("m should open the mounts screen:\n%s", v.View())
	}
	// The knowledge bases are listed, so a name is chosen and never typed.
	v = keyV(v, "a")
	if v.mounts.mode != mountsPickVault || len(v.mounts.picks) != 1 || v.mounts.picks[0].Name != "ai-ml" {
		t.Fatalf("pick mode: mode=%d picks=%+v", v.mounts.mode, v.mounts.picks)
	}
	if out := v.View(); !strings.Contains(out, "Choose one") || !strings.Contains(out, "▸ ai-ml") {
		t.Fatalf("the picker lists the knowledge bases:\n%s", out)
	}
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

func TestMountsScreenNeedsHooks(t *testing.T) {
	none := keyV(newView(sample(), Opener{}, Hooks{}), "m")
	if none.mounts != nil || !strings.Contains(none.errMsg, "not available") {
		t.Fatalf("m without hooks reports why: err=%q", none.errMsg)
	}
	_, _, hooks := atlasFixture(t)
	// A vault the scan could not read has no mounts screen, and says why.
	bad := keyV(findVault(t, newView(sample(), Opener{}, hooks), "old-notes"), "m")
	if bad.mounts != nil || !strings.Contains(bad.errMsg, v1Start) {
		t.Fatalf("m on a vault the scan could not read: err=%q", bad.errMsg)
	}
	kb := findVault(t, openView(t, hooks), "ai-ml")
	if r := kb.current(); r == nil || r.Entry.Kind != vault.Knowledge {
		t.Fatalf("cursor on %+v", kb.current())
	}
	kb = keyV(kb, "m")
	if kb.mounts == nil || !strings.Contains(kb.View(), "nothing mounts this knowledge base yet") {
		t.Fatalf("m on a knowledge base opens the screen: err=%q\n%s", kb.errMsg, kb.View())
	}
}

// guardKB mounts ai-ml on reading and makes ai-ml guarded, so a grant decides what
// reading gets.
func guardKB(t *testing.T, cfg *home.Config) {
	t.Helper()
	ix, err := registry.Scan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	project, err := ix.Find("reading")
	if err != nil {
		t.Fatal(err)
	}
	kb, err := ix.Find("ai-ml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vaults.Mount(*project, *kb, vault.AccessWrite, "", testNow); err != nil {
		t.Fatal(err)
	}
	guarded := vault.AccessGuarded
	if err := vaults.EditIdentity(*kb, vaults.Edit{Access: &guarded}, testNow); err != nil {
		t.Fatal(err)
	}
}

func TestMountsScreenOnAKnowledgeBaseGrantsAndRevokes(t *testing.T) {
	cfg, _, hooks := atlasFixture(t)
	guardKB(t, cfg)
	v := findVault(t, openView(t, hooks), "ai-ml")
	v = keyV(v, "m")
	if v.mounts == nil {
		t.Fatalf("m should open the mounts screen: err=%q", v.errMsg)
	}
	out := v.View()
	for _, want := range []string{"ai-ml   mounts   guarded", "reading", "mounts it (effective read)", "no grant", "w grant write", "x revoke"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// w grants write; reading's effective access follows.
	v = keyV(v, "w")
	if !v.changed || v.mounts == nil || v.mounts.err != "" {
		t.Fatalf("after w: changed=%v screen=%v", v.changed, v.mounts)
	}
	out = v.View()
	for _, want := range []string{"grant: write", "mounts it (effective write)", "granted reading write"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if grants := entryNamed(t, cfg, "ai-ml").Grants; len(grants) != 1 || grants[0].Name != "reading" || grants[0].Access != vault.AccessWrite {
		t.Fatalf("identity file: %+v", grants)
	}
	// x asks first; the mount stays and falls back to read.
	v = keyV(v, "x")
	if v.mounts.mode != mountsConfirm || !strings.Contains(v.View(), "Revoke reading's grant?") {
		t.Fatalf("confirm: mode=%d\n%s", v.mounts.mode, v.View())
	}
	v = keyV(v, "y")
	out = v.View()
	for _, want := range []string{"no grant", "mounts it (effective read)", "revoked reading"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if grants := entryNamed(t, cfg, "ai-ml").Grants; len(grants) != 0 {
		t.Fatalf("identity file: %+v", grants)
	}
	// There is nothing left to revoke on that row.
	v = keyV(v, "x")
	if v.mounts.mode != mountsList || v.mounts.err != "reading has no grant" {
		t.Fatalf("x without a grant: mode=%d err=%q", v.mounts.mode, v.mounts.err)
	}
	// a grants a project that does not mount it, chosen from the list.
	v = keyV(v, "a")
	if v.mounts.mode != mountsPickVault || !strings.Contains(v.View(), "the project that may reach this knowledge base") {
		t.Fatalf("pick mode: %d\n%s", v.mounts.mode, v.View())
	}
	at := -1
	for i, e := range v.mounts.picks {
		if e.Name == "welcome" {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("welcome should be a candidate: %+v", v.mounts.picks)
	}
	for i := 0; i < at; i++ {
		v = pressV(v, tea.KeyDown)
	}
	v = pressV(v, tea.KeyEnter)
	if v.mounts.mode != mountsPickAccess {
		t.Fatalf("access step: mode=%d\n%s", v.mounts.mode, v.View())
	}
	v = keyV(v, "r")
	if v.mounts.mode != mountsList || len(v.mounts.rows) != 2 || v.mounts.err != "" {
		t.Fatalf("after the grant: mode=%d rows=%d err=%q", v.mounts.mode, len(v.mounts.rows), v.mounts.err)
	}
	out = v.View()
	for _, want := range []string{"welcome", "does not mount it", "grant: read"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if grants := entryNamed(t, cfg, "ai-ml").Grants; len(grants) != 1 || grants[0].Name != "welcome" || grants[0].Access != vault.AccessRead {
		t.Fatalf("identity file: %+v", grants)
	}
	v = pressV(v, tea.KeyEsc)
	if v.mounts != nil {
		t.Fatal("esc should close the screen")
	}
}

func TestMountsScreenGrantsOnAnOpenKnowledgeBase(t *testing.T) {
	cfg, _, hooks := atlasFixture(t)
	v := keyV(findVault(t, openView(t, hooks), "ai-ml"), "m")
	if v.mounts == nil || len(v.mounts.rows) != 0 {
		t.Fatalf("a new knowledge base grants nothing yet: err=%q screen=%v", v.errMsg, v.mounts)
	}
	v = keyV(v, "a")
	for v.mounts.picks[v.mounts.pick].Name != "reading" {
		v = pressV(v, tea.KeyDown)
	}
	v = pressV(v, tea.KeyEnter)
	if v.mounts.mode != mountsPickAccess {
		t.Fatalf("access step: mode=%d err=%q", v.mounts.mode, v.mounts.err)
	}
	v = keyV(v, "w")
	if !strings.Contains(v.mounts.status, "granted reading write; ai-ml is open, so the grant applies when it is guarded") {
		t.Fatalf("status: %q err=%q", v.mounts.status, v.mounts.err)
	}
	out := v.View()
	for _, want := range []string{"ai-ml   mounts   open", "reading", "does not mount it", "grant: write"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if grants := entryNamed(t, cfg, "ai-ml").Grants; len(grants) != 1 || grants[0].Name != "reading" || grants[0].Access != vault.AccessWrite {
		t.Fatalf("identity file: %+v", grants)
	}
}

// TestMountsScreenKindGuardsIgnoreTheWrongKeys holds the screen's central design
// decision: x grants and revokes only on a knowledge base; u unmounts only on a
// project, where w and r change what the mount asks for instead of granting.
func TestMountsScreenKindGuardsIgnoreTheWrongKeys(t *testing.T) {
	// Each screen holds one row, so the kind guard, not an empty list, is what stops a key.
	cfg, _, hooks := atlasFixture(t)
	guardKB(t, cfg)
	v := keyV(findVault(t, openView(t, hooks), "reading"), "m")
	if v.mounts == nil || len(v.mounts.rows) != 1 {
		t.Fatalf("m should open the project's mounts with one row: err=%q", v.errMsg)
	}
	v = keyV(v, "x")
	if v.mounts == nil || v.mounts.mode != mountsList || v.mounts.err != "" || v.mounts.changed || len(v.mounts.rows) != 1 {
		t.Fatalf("x on a project: mode=%d err=%q changed=%v rows=%d", v.mounts.mode, v.mounts.err, v.mounts.changed, len(v.mounts.rows))
	}
	if kb := entryNamed(t, cfg, "ai-ml"); len(kb.Grants) != 0 {
		t.Fatalf("x on a project granted something: %+v", kb.Grants)
	}
	// w and r change what the mount asks for; the guarded knowledge base still decides.
	v = keyV(v, "r")
	if v.mounts.err != "" || !v.changed || !strings.Contains(v.mounts.status, "ai-ml asks for read") {
		t.Fatalf("r on a project: err=%q status=%q", v.mounts.err, v.mounts.status)
	}
	if p := entryNamed(t, cfg, "reading"); len(p.Mounts) != 1 || p.Mounts[0].Access != vault.AccessRead {
		t.Fatalf("r should ask for read: %+v", p.Mounts)
	}
	v = keyV(v, "w")
	if p := entryNamed(t, cfg, "reading"); len(p.Mounts) != 1 || p.Mounts[0].Access != vault.AccessWrite {
		t.Fatalf("w should ask for write: %+v", p.Mounts)
	}
	if kb := entryNamed(t, cfg, "ai-ml"); len(kb.Grants) != 0 {
		t.Fatalf("neither key grants anything: %+v", kb.Grants)
	}

	kv := keyV(findVault(t, openView(t, hooks), "ai-ml"), "m")
	if kv.mounts == nil || len(kv.mounts.rows) != 1 {
		t.Fatalf("m should open the knowledge base's mounters with one row: err=%q", kv.errMsg)
	}
	kv = keyV(kv, "u")
	if kv.mounts == nil || kv.mounts.mode != mountsList || kv.mounts.err != "" || kv.mounts.changed || len(kv.mounts.rows) != 1 {
		t.Fatalf("u on a knowledge base: mode=%d err=%q changed=%v rows=%d", kv.mounts.mode, kv.mounts.err, kv.mounts.changed, len(kv.mounts.rows))
	}
	if p := entryNamed(t, cfg, "reading"); len(p.Mounts) != 1 {
		t.Fatalf("u on a knowledge base unmounted something: %+v", p.Mounts)
	}
}

func TestMountsScreenNamesAStaleGrant(t *testing.T) {
	cfg, _, hooks := atlasFixture(t)
	guardKB(t, cfg)
	kb := entryNamed(t, cfg, "ai-ml")
	if err := vault.UpdateConfig(kb.Path, "grant gone read", testNow, func(c *vault.Config) error {
		c.Grants = append(c.Grants, vault.Grant{ID: "gone-0000", Name: "gone", Access: vault.AccessRead})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	v := keyV(findVault(t, openView(t, hooks), "ai-ml"), "m")
	if v.mounts == nil || len(v.mounts.rows) != 2 {
		t.Fatalf("m should list the mounter and the stale grant: err=%q screen=%v", v.errMsg, v.mounts)
	}
	out := v.View()
	for _, want := range []string{"gone-0000", "does not mount it", "no project with id gone-0000"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The stale grant is the second row: it cannot be granted, only revoked.
	v = pressV(v, tea.KeyDown)
	v = keyV(v, "w")
	if v.changed || !strings.Contains(v.mounts.err, "no project with id gone-0000") {
		t.Fatalf("w on a stale grant: changed=%v err=%q", v.changed, v.mounts.err)
	}
	v = keyV(v, "x")
	if v.mounts.mode != mountsConfirm {
		t.Fatalf("confirm: mode=%d\n%s", v.mounts.mode, v.View())
	}
	v = keyV(v, "y")
	if len(v.mounts.rows) != 1 || v.mounts.rows[0].name != "reading" || !strings.Contains(v.mounts.status, "revoked gone-0000") {
		t.Fatalf("the stale grant should be gone: rows=%d status=%q\n%s", len(v.mounts.rows), v.mounts.status, v.View())
	}
	if grants := entryNamed(t, cfg, "ai-ml").Grants; len(grants) != 0 {
		t.Fatalf("identity file: %+v", grants)
	}
}
