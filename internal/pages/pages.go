// Package pages writes the static orientation pages into the atlas vault.
//
// The pages are markdown templates under templates/. Placeholders: %REPO%, %VERSION%,
// %VAULTS%, %ATLAS%, %HOME%, %NEW_DAYS%.
package pages

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nathanaday/claude-atlas/internal/home"
)

// Repository is the project's home on GitHub.
const Repository = "https://github.com/nathanaday/claude-atlas"

//go:embed templates/*.md
var templates embed.FS

//go:embed graph.json
var graphDefaults []byte

// WriteGraph writes the atlas vault's graph view settings when it has none: the
// generated hub pages filtered out, and one color per kind of node. It reports whether
// it wrote the file.
func WriteGraph(atlas string) (bool, error) {
	path := filepath.Join(atlas, ".obsidian", "graph.json")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, graphDefaults, 0o644)
}

// Write renders every template into the atlas vault.
func Write(cfg *home.Config, homeRoot, version string) error {
	r := strings.NewReplacer(
		"%REPO%", Repository,
		"%VERSION%", version,
		"%VAULTS%", home.Display(cfg.VaultsDir),
		"%ATLAS%", home.Display(cfg.AtlasVault),
		"%HOME%", home.Display(homeRoot),
		"%NEW_DAYS%", fmt.Sprint(cfg.NewDays()),
	)
	entries, err := templates.ReadDir("templates")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		text, err := templates.ReadFile("templates/" + entry.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(cfg.AtlasVault, entry.Name()), []byte(r.Replace(string(text))), 0o644); err != nil {
			return err
		}
	}
	return nil
}
