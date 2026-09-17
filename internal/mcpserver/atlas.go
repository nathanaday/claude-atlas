package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
)

// The atlas tools: the whole atlas as one read, and the writes that configure it. They
// bind the same functions the view calls, once per call over a config loaded for that
// call, because the CLI in another process may change config.json between two calls.

// bind loads the config and binds the atlas actions for one call.
func (s *Server) bind() (actions.Atlas, *home.Config, error) {
	cfg, err := s.home().Load()
	if err != nil {
		return actions.Atlas{}, nil, err
	}
	return actions.Bind(s.home(), cfg, nil), cfg, nil
}

// entryOf resolves a vault a tool names: by name, by id, or by path. A vault the atlas
// cannot read is an error that says why.
func entryOf(ix *registry.Index, arg string) (registry.Entry, error) {
	en, err := anyEntryOf(ix, arg)
	if err != nil {
		return registry.Entry{}, err
	}
	if en.Error != "" {
		return registry.Entry{}, fmt.Errorf("%s: %s", home.Display(en.Path), en.Error)
	}
	return en, nil
}

// anyEntryOf is entryOf for a vault the atlas cannot read too, so forget can drop it.
func anyEntryOf(ix *registry.Index, arg string) (registry.Entry, error) {
	if strings.TrimSpace(arg) == "" {
		return registry.Entry{}, errors.New("name a vault: its name, id, or path")
	}
	found, err := ix.Find(arg)
	if err == nil {
		return *found, nil
	}
	if !errors.Is(err, registry.ErrNotFound) {
		return registry.Entry{}, err
	}
	abs, aerr := filepath.Abs(home.Expand(arg))
	for i := range ix.Entries {
		e := &ix.Entries[i]
		if e.Error == "" {
			continue
		}
		if (aerr == nil && e.Path == abs) || strings.EqualFold(filepath.Base(e.Path), arg) {
			return *e, nil
		}
	}
	return registry.Entry{}, fmt.Errorf("no vault named %q; the atlas tool lists them", arg)
}

// Settings are the atlas settings a session may read and set.
type Settings struct {
	VaultsDir   string `json:"vaults_dir"`
	NewDays     int    `json:"new_days"`
	RepoChanges string `json:"repo_changes"`
}

func settingsOf(cfg *home.Config) Settings {
	return Settings{VaultsDir: cfg.VaultsDir, NewDays: cfg.NewDays(), RepoChanges: cfg.DefaultChanges()}
}

type AtlasArgs struct {
	Refresh bool `json:"refresh,omitempty" jsonschema:"also rewrite the registry, recreate each project's kb/ links, and adopt repositories waiting under repos/: what claude-atlas refresh does"`
}

// AtlasOut is the whole atlas: every vault with its state, the folders the atlas cannot
// read, the settings, and, after a refresh, what each project needed.
type AtlasOut struct {
	Vaults   []registry.Entry        `json:"vaults"`
	Problems []registry.Problem      `json:"problems"`
	Settings Settings                `json:"settings"`
	Changes  []refresh.ProjectChange `json:"changes,omitempty"`
}

func (s *Server) atlasTool(ctx context.Context, req *mcp.CallToolRequest, a AtlasArgs) (*mcp.CallToolResult, AtlasOut, error) {
	acts, cfg, err := s.bind()
	if err != nil {
		return nil, AtlasOut{}, err
	}
	var ix *registry.Index
	var changes []refresh.ProjectChange
	if a.Refresh {
		ix, changes, err = acts.Refresh()
	} else {
		ix, err = acts.Scan()
	}
	if err != nil {
		return nil, AtlasOut{}, err
	}
	out := AtlasOut{Vaults: ix.Entries, Problems: ix.Problems, Settings: settingsOf(cfg), Changes: changes}
	if out.Vaults == nil {
		out.Vaults = []registry.Entry{}
	}
	if out.Problems == nil {
		out.Problems = []registry.Problem{}
	}
	return nil, out, nil
}
