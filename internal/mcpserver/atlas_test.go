package mcpserver

import (
	"os"
	"strings"
	"testing"

	"github.com/nathanaday/claude-atlas/internal/registry"
)

func TestAtlasReadsWithoutWritingAndRefreshWrites(t *testing.T) {
	if msg := connect(t, t.TempDir()).call("atlas", map[string]any{}, nil); !strings.Contains(msg, "no atlas") {
		t.Fatalf("without an atlas the tool says so: %q", msg)
	}
	h, cfg, p, _ := mounted(t)
	c := connectIn(t, h, p.Root)
	var out AtlasOut
	if msg := c.call("atlas", map[string]any{}, &out); msg != "" {
		t.Fatal(msg)
	}
	if len(out.Vaults) != 2 || out.Settings.VaultsDir != cfg.VaultsDir || out.Settings.RepoChanges != "commit" {
		t.Fatalf("atlas: %+v", out)
	}
	for _, e := range out.Vaults {
		if e.State == nil {
			t.Fatalf("every entry carries its state: %+v", e)
		}
	}
	if _, err := os.Stat(registry.File(h.StateDir())); !os.IsNotExist(err) {
		t.Fatalf("a plain read writes no registry: %v", err)
	}
	if msg := c.call("atlas", map[string]any{"refresh": true}, &out); msg != "" {
		t.Fatal(msg)
	}
	if _, err := os.Stat(registry.File(h.StateDir())); err != nil {
		t.Fatalf("refresh writes the registry: %v", err)
	}
}
