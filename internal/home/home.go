// Package home locates and manages the atlas home directory and its config.
package home

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ConfigSchema   = "claude-atlas.config.v3"
	ConfigSchemaV2 = "claude-atlas.config.v2"
	ConfigSchemaV1 = "claude-atlas.config.v1"
	EnvHome        = "CLAUDE_ATLAS_HOME"
	defaultHome    = "~/.claude-atlas"
	DefaultVaults  = "~/Vaults"

	// DefaultPluginID is the claude-atlas plugin as Claude Code names it.
	DefaultPluginID = "claude-atlas@nathanaday-claude-atlas"
	// DefaultPluginSource is what `claude plugin marketplace add` takes: this repository.
	DefaultPluginSource = "nathanaday/claude-atlas"
	// DefaultNewDays is how many days after its creation a vault counts as new.
	DefaultNewDays = 7
)

// HeatConfig tunes how the overview reads a vault's activity. NewDays is the age, in
// days, under which a vault is "new" whatever its activity; 0 turns that off.
type HeatConfig struct {
	NewDays int `json:"new_days"`
}

// PluginConfig says where the claude-atlas plugin comes from.
type PluginConfig struct {
	// ID is the plugin id, name@marketplace.
	ID string `json:"id"`
	// Source is passed to `claude plugin marketplace add`: a GitHub slug or a local path.
	Source string `json:"source"`
}

// LaunchConfig says how to start Claude Code inside a vault. It mirrors
// claudecode.LaunchConfig; home cannot import that package.
type LaunchConfig struct {
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	Prompt         string   `json:"prompt,omitempty"`
	SessionContext bool     `json:"session_context"`
}

// Config is the contents of config.json. Paths are absolute.
type Config struct {
	Schema     string       `json:"schema"`
	VaultsDir  string       `json:"vaults_dir"`
	Plugin     PluginConfig `json:"plugin"`
	ClaudeCode LaunchConfig `json:"claude_code"`
	// Heat is nil in a config written before the section existed; NewDays reads it.
	Heat *HeatConfig `json:"heat,omitempty"`
	// Knowledge holds knowledge base roots outside VaultsDir; the scan cannot find them
	// there.
	Knowledge []string `json:"knowledge,omitempty"`
	// Projects holds every project's work folder, the parent of its atlas/ folder. The
	// atlas never scans for projects: they live where the user's work lives.
	Projects []string `json:"projects,omitempty"`
}

// AddKnowledge records root as a knowledge base outside VaultsDir; it reports whether
// root was added.
func (c *Config) AddKnowledge(root string) bool {
	root = Expand(root)
	if contains(c.Knowledge, root) {
		return false
	}
	c.Knowledge = append(c.Knowledge, root)
	return true
}

// RemoveKnowledge drops root from Knowledge; it reports whether root was present.
func (c *Config) RemoveKnowledge(root string) bool {
	var ok bool
	c.Knowledge, ok = remove(c.Knowledge, Expand(root))
	return ok
}

// AddProject records a project's work folder; it reports whether it was added.
func (c *Config) AddProject(work string) bool {
	work = Expand(work)
	if contains(c.Projects, work) {
		return false
	}
	c.Projects = append(c.Projects, work)
	return true
}

// RemoveProject drops a project's work folder; it reports whether it was present.
func (c *Config) RemoveProject(work string) bool {
	var ok bool
	c.Projects, ok = remove(c.Projects, Expand(work))
	return ok
}

// HasProject reports whether the config lists work.
func (c *Config) HasProject(work string) bool { return contains(c.Projects, Expand(work)) }

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func remove(list []string, s string) ([]string, bool) {
	for i, item := range list {
		if item == s {
			return append(list[:i], list[i+1:]...), true
		}
	}
	return list, false
}

// Inside reports whether root is VaultsDir or a descendant of it.
func (c *Config) Inside(root string) bool {
	rel, err := filepath.Rel(c.VaultsDir, root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// NewDays is the configured age under which a vault is new, or the default.
func (c *Config) NewDays() int {
	if c.Heat == nil {
		return DefaultNewDays
	}
	return c.Heat.NewDays
}

// SetNewDays records the threshold; it must not be negative.
func (c *Config) SetNewDays(days int) error {
	if days < 0 {
		return fmt.Errorf("new_days must be 0 or more, got %d", days)
	}
	c.Heat = &HeatConfig{NewDays: days}
	return nil
}

// Home is the atlas home directory.
type Home struct {
	Root string
}

// Resolve picks the home: an explicit flag, then $CLAUDE_ATLAS_HOME, then ~/.claude-atlas.
func Resolve(explicit string) Home {
	value := explicit
	if value == "" {
		value = os.Getenv(EnvHome)
	}
	if value == "" {
		value = defaultHome
	}
	abs, err := filepath.Abs(Expand(value))
	if err != nil {
		abs = Expand(value)
	}
	return Home{Root: abs}
}

func (h Home) ConfigPath() string { return filepath.Join(h.Root, "config.json") }

// StateDir holds the derived registry file. Safe to delete.
func (h Home) StateDir() string { return filepath.Join(h.Root, "state") }

func (h Home) Exists() bool {
	info, err := os.Stat(h.ConfigPath())
	return err == nil && info.Mode().IsRegular()
}

func defaultPlugin() PluginConfig {
	return PluginConfig{ID: DefaultPluginID, Source: DefaultPluginSource}
}

func defaultLaunch() LaunchConfig {
	return LaunchConfig{Command: "claude", SessionContext: true}
}

// Default is the config a fresh setup starts from. An empty vaultsDir takes the default.
func (h Home) Default(vaultsDir string) *Config {
	if vaultsDir == "" {
		vaultsDir = DefaultVaults
	}
	return &Config{
		Schema:     ConfigSchema,
		VaultsDir:  Expand(vaultsDir),
		Plugin:     defaultPlugin(),
		ClaudeCode: defaultLaunch(),
		Heat:       &HeatConfig{NewDays: DefaultNewDays},
	}
}

var ErrNoAtlas = errors.New("no atlas here; run `claude-atlas setup` first")

func (h Home) Load() (*Config, error) {
	data, err := os.ReadFile(h.ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w (looked in %s)", ErrNoAtlas, Display(h.Root))
	}
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", h.ConfigPath(), err)
	}
	switch cfg.Schema {
	case ConfigSchema:
	case ConfigSchemaV2:
		// A v2 config listed vaults of both kinds outside the vaults directory; the
		// knowledge bases among them still resolve, and a v2 project vault reports itself
		// as one when the scan reads it. Repositories and their policies are gone.
		var v2 struct {
			Vaults []string `json:"vaults"`
		}
		json.Unmarshal(data, &v2)
		cfg.Knowledge = append(cfg.Knowledge, v2.Vaults...)
		cfg.Schema = ConfigSchema
	case ConfigSchemaV1:
		cfg.Schema = ConfigSchema
	default:
		return nil, fmt.Errorf("%s: unsupported schema %q", h.ConfigPath(), cfg.Schema)
	}
	cfg.VaultsDir = Expand(cfg.VaultsDir)
	for i, v := range cfg.Knowledge {
		cfg.Knowledge[i] = Expand(v)
	}
	for i, v := range cfg.Projects {
		cfg.Projects[i] = Expand(v)
	}
	// Configs written before these sections existed keep working with the defaults.
	if cfg.Plugin.ID == "" {
		cfg.Plugin = defaultPlugin()
	}
	cfg.Plugin.Source = Expand(cfg.Plugin.Source)
	if cfg.ClaudeCode.Command == "" {
		cfg.ClaudeCode = defaultLaunch()
	}
	if cfg.Heat != nil && cfg.Heat.NewDays < 0 {
		return nil, fmt.Errorf("%s: heat.new_days must be 0 or more", h.ConfigPath())
	}
	return &cfg, nil
}

func (h Home) Save(cfg *Config) error {
	if err := os.MkdirAll(h.Root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.ConfigPath(), append(data, '\n'), 0o644)
}

// Expand replaces a leading ~ with the user's home directory.
func Expand(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if dir, err := os.UserHomeDir(); err == nil {
			return filepath.Join(dir, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// Display shortens a path under the home directory to ~/... for output.
func Display(path string) string {
	dir, err := os.UserHomeDir()
	if err != nil || dir == "" {
		return path
	}
	if path == dir {
		return "~"
	}
	if strings.HasPrefix(path, dir+string(filepath.Separator)) {
		return "~" + path[len(dir):]
	}
	return path
}
