// Package claudecode reads Claude Code's plugin registry and drives `claude plugin`.
package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ConfigDir is Claude Code's config directory: $CLAUDE_CONFIG_DIR or ~/.claude.
func ConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// CLI returns the path of the `claude` command, or "" when it is not on PATH.
func CLI() string {
	path, err := exec.LookPath("claude")
	if err != nil {
		return ""
	}
	return path
}

// Install describes one installed plugin as Claude Code records it.
type Install struct {
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"`
	Version     string `json:"version"`
}

// InstalledPlugin looks a plugin id up in installed_plugins.json.
func InstalledPlugin(id string) (*Install, error) {
	path := filepath.Join(ConfigDir(), "plugins", "installed_plugins.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var registry struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	raw, ok := registry.Plugins[id]
	if !ok {
		return nil, nil
	}
	// Claude Code records either one object or a list of them per plugin.
	var many []Install
	if err := json.Unmarshal(raw, &many); err == nil {
		if len(many) == 0 {
			return nil, nil
		}
		return &many[0], nil
	}
	var one Install
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, fmt.Errorf("%s: plugin %s: %w", path, id, err)
	}
	return &one, nil
}

// MarketplaceKnown reports whether a marketplace name is registered.
func MarketplaceKnown(name string) bool {
	path := filepath.Join(ConfigDir(), "plugins", "known_marketplaces.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var known map[string]json.RawMessage
	if err := json.Unmarshal(data, &known); err != nil {
		return false
	}
	_, ok := known[name]
	return ok
}

// MarketplaceName is the registry key for a plugin id like name@marketplace.
func MarketplaceName(pluginID string) string {
	if i := strings.LastIndex(pluginID, "@"); i >= 0 {
		return pluginID[i+1:]
	}
	return ""
}

// Commands are the shell commands that install a plugin, for display.
func Commands(marketplace, pluginID string) []string {
	return []string{
		"claude plugin marketplace add " + marketplace,
		"claude plugin install " + pluginID,
	}
}

func run(args ...string) error {
	cli := CLI()
	if cli == "" {
		return errors.New("the `claude` command is not on PATH")
	}
	cmd := exec.Command(cli, append([]string{"plugin"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude plugin %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

// InstallPlugin registers the marketplace if needed and installs the plugin.
// It returns the commands it ran.
func InstallPlugin(marketplace, pluginID string) ([]string, error) {
	var ran []string
	if !MarketplaceKnown(MarketplaceName(pluginID)) {
		ran = append(ran, Commands(marketplace, pluginID)[0])
		if err := run("marketplace", "add", marketplace); err != nil {
			return ran, err
		}
	}
	if inst, _ := InstalledPlugin(pluginID); inst == nil {
		ran = append(ran, Commands(marketplace, pluginID)[1])
		if err := run("install", pluginID); err != nil {
			return ran, err
		}
	}
	return ran, nil
}

// ConfigFile is Claude Code's main config, where it keys per-project state (session
// history, allowed tools, MCP servers) by the project's absolute path.
func ConfigFile() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude.json"
	}
	return filepath.Join(home, ".claude.json")
}

// ProjectsUnder lists the project paths Claude Code records at or below root, sorted.
// Moving such a folder orphans what Claude Code keeps for it; nothing here rewrites
// another program's state, so callers report the paths.
func ProjectsUnder(root string) []string {
	data, err := os.ReadFile(ConfigFile())
	if err != nil {
		return nil
	}
	var file struct {
		Projects map[string]json.RawMessage `json:"projects"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil
	}
	root = filepath.Clean(root)
	var out []string
	for path := range file.Projects {
		clean := filepath.Clean(path)
		rel, err := filepath.Rel(root, clean)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}
