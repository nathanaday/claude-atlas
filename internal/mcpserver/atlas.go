package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nathanaday/claude-atlas/internal/actions"
	"github.com/nathanaday/claude-atlas/internal/capture"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/refresh"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// The atlas tools: the whole atlas as one read, and the writes that configure it. They
// bind the same functions the view calls, once per call over a config loaded for that
// call, because the CLI in another process may change config.json between two calls. A
// write ends in a refresh, so the stored registry `claude-atlas list` and the view read
// carries the change, and the tool answers from the index that refresh derived.

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

type VaultToolArgs struct {
	Action  string    `json:"action" jsonschema:"create, adopt, edit, or forget"`
	Target  string    `json:"target,omitempty" jsonschema:"edit, forget: the vault, by name, id, or path"`
	Kind    string    `json:"kind,omitempty" jsonschema:"create, adopt: project (default) or knowledge; a cluster is a knowledge base with members"`
	Name    string    `json:"name,omitempty" jsonschema:"create: the vault's name; adopt: its display name, default the folder's; edit: the new name, which renames the folder too"`
	Path    string    `json:"path,omitempty" jsonschema:"create: where the vault goes, default <vaults dir>/projects/<name> or knowledge/<name>; adopt: the folder to adopt"`
	Mode    string    `json:"mode,omitempty" jsonschema:"create, adopt: the filing mode, generic (default) or lyt"`
	Tags    *[]string `json:"tags,omitempty" jsonschema:"create, edit: a project's tags; on edit an empty list clears them"`
	Scope   *string   `json:"scope,omitempty" jsonschema:"create, edit: what a knowledge base covers, one or two sentences; on edit an empty string clears it"`
	Access  *string   `json:"access,omitempty" jsonschema:"create, edit: a knowledge base's access, open (default) or guarded"`
	InRepo  string    `json:"in_repo,omitempty" jsonschema:"create: a git repository's top level; the project goes to REPO/atlas/ and shares its git; not with path"`
	Mount   string    `json:"mount,omitempty" jsonschema:"create: a knowledge base a new project mounts for writing, by name, id, or path"`
	Members []string  `json:"members,omitempty" jsonschema:"create: the knowledge bases a new cluster gathers, by name, id, or path"`
}

// VaultToolOut is the vault as the atlas sees it after the change, or the path forget dropped.
type VaultToolOut struct {
	Vault     *registry.Entry `json:"vault,omitempty"`
	Forgotten string          `json:"forgotten,omitempty" jsonschema:"the path the atlas no longer lists; the folder stays"`
}

func (s *Server) vaultTool(ctx context.Context, req *mcp.CallToolRequest, a VaultToolArgs) (*mcp.CallToolResult, VaultToolOut, error) {
	acts, cfg, err := s.bind()
	if err != nil {
		return nil, VaultToolOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, VaultToolOut{}, err
	}
	switch a.Action {
	case "create", "adopt":
		choice, err := vaultChoice(ix, cfg, a)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		path, err := acts.Create(choice)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		return s.entryOut(acts, path)
	case "edit":
		en, err := entryOf(ix, a.Target)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		path, err := acts.Edit(en, vaults.Edit{Name: a.Name, Tags: a.Tags, Scope: a.Scope, Access: a.Access})
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		return s.entryOut(acts, path)
	case "forget":
		en, err := anyEntryOf(ix, a.Target)
		if err != nil {
			return nil, VaultToolOut{}, err
		}
		if err := acts.Unregister(en); err != nil {
			return nil, VaultToolOut{}, err
		}
		if _, _, err := acts.Refresh(); err != nil {
			return nil, VaultToolOut{}, err
		}
		return nil, VaultToolOut{Forgotten: en.Path}, nil
	}
	return nil, VaultToolOut{}, fmt.Errorf("action must be create, adopt, edit, or forget, not %q", a.Action)
}

// vaultChoice turns the create and adopt arguments into what Create takes, with the
// mount and the members resolved to ids before anything is written. An adopt with no
// kind keeps the kind the vault already has; a create with no kind makes a project.
func vaultChoice(ix *registry.Index, cfg *home.Config, a VaultToolArgs) (actions.AddVault, error) {
	adopt := a.Action == "adopt"
	var kind vault.Kind
	switch {
	case a.Kind != "":
		var err error
		if kind, err = vault.ParseKind(a.Kind); err != nil {
			return actions.AddVault{}, err
		}
	case !adopt:
		kind = vault.Project
	}
	choice := actions.AddVault{Kind: kind, Name: a.Name, Mode: a.Mode, Adopt: adopt}
	if a.Tags != nil {
		choice.Tags = *a.Tags
	}
	if a.Scope != nil {
		choice.Scope = *a.Scope
	}
	if a.Access != nil {
		choice.Access = *a.Access
	}
	if len(choice.Tags) > 0 && kind == vault.Knowledge {
		return actions.AddVault{}, errors.New("tags are a project's; a knowledge base has a scope")
	}
	if choice.Scope != "" && kind == vault.Project {
		return actions.AddVault{}, errors.New("scope is a knowledge base's; a project has tags")
	}
	if choice.Access != "" && !vault.ValidAccess(choice.Access, true) {
		return actions.AddVault{}, fmt.Errorf("access must be open or guarded, not %q", choice.Access)
	}
	switch {
	case choice.Adopt:
		if a.Path == "" {
			return actions.AddVault{}, errors.New("adopt needs path: the folder to adopt")
		}
		if a.Mount != "" || len(a.Members) > 0 {
			return actions.AddVault{}, errors.New("mount and members are for create; mount an adopted vault with the mount tool, and gather members with the cluster tool")
		}
		abs, err := filepath.Abs(home.Expand(a.Path))
		if err != nil {
			return actions.AddVault{}, err
		}
		choice.Path = abs
		if choice.Name == "" {
			choice.Name = filepath.Base(abs)
		}
	case a.InRepo != "":
		if a.Path != "" {
			return actions.AddVault{}, errors.New("give path or in_repo, not both")
		}
		if kind != vault.Project {
			return actions.AddVault{}, errors.New("only a project lives inside a repository")
		}
		if a.Name == "" {
			return actions.AddVault{}, errors.New("create needs name")
		}
		choice.InRepo = home.Expand(a.InRepo)
	default:
		if a.Name == "" {
			return actions.AddVault{}, errors.New("create needs name")
		}
		arg := a.Path
		if arg == "" {
			arg = a.Name
		}
		path, err := vaults.ResolvePath(arg, cfg.VaultsDir, kind)
		if err != nil {
			return actions.AddVault{}, err
		}
		choice.Path = path
	}
	if a.Mount != "" {
		if kind != vault.Project {
			return actions.AddVault{}, errors.New("only a project mounts a knowledge base")
		}
		kb, err := entryOf(ix, a.Mount)
		if err != nil {
			return actions.AddVault{}, err
		}
		if kb.Kind != vault.Knowledge {
			return actions.AddVault{}, fmt.Errorf("%s is a project; a project mounts knowledge bases", kb.Name)
		}
		choice.MountID = kb.ID
	}
	for _, m := range a.Members {
		if kind != vault.Knowledge {
			return actions.AddVault{}, errors.New("only a knowledge base gathers members")
		}
		kb, err := entryOf(ix, m)
		if err != nil {
			return actions.AddVault{}, err
		}
		if kb.Kind != vault.Knowledge {
			return actions.AddVault{}, fmt.Errorf("%s is a project; a cluster gathers knowledge bases", kb.Name)
		}
		choice.Cluster = true
		choice.MemberIDs = append(choice.MemberIDs, kb.ID)
	}
	return choice, nil
}

// entryOut rewrites the registry after a write, so `claude-atlas list` and the view show
// the change, and returns the vault at path as the atlas now sees it.
func (s *Server) entryOut(acts actions.Atlas, path string) (*mcp.CallToolResult, VaultToolOut, error) {
	ix, _, err := acts.Refresh()
	if err != nil {
		return nil, VaultToolOut{}, err
	}
	en := ix.ByPath(path)
	if en == nil {
		return nil, VaultToolOut{}, fmt.Errorf("%s was written but the scan does not list it; call atlas with refresh", home.Display(path))
	}
	return nil, VaultToolOut{Vault: en}, nil
}

type MountToolArgs struct {
	Action    string `json:"action" jsonschema:"mount, unmount, access, grant, or revoke"`
	Project   string `json:"project,omitempty" jsonschema:"the project, by name, id, or path; on revoke a stale grant's id works too"`
	Knowledge string `json:"knowledge,omitempty" jsonschema:"the knowledge base, by name, id, or path; on unmount and access the mount's own name works too"`
	Access    string `json:"access,omitempty" jsonschema:"mount: what the project asks for, write (default) or read; access and grant: read or write"`
	As        string `json:"as,omitempty" jsonschema:"mount: the name the project reaches it under, default the knowledge base's name"`
}

// MountToolOut is the mount as it now stands, with its effective access; after unmount,
// what the project still mounts; after grant or revoke, the knowledge base's grants.
type MountToolOut struct {
	Mount  *registry.Mount  `json:"mount,omitempty"`
	Mounts []registry.Mount `json:"mounts,omitempty"`
	Grants []registry.Grant `json:"grants,omitempty"`
}

func checkAccess(access string) error {
	if access != vault.AccessRead && access != vault.AccessWrite {
		return fmt.Errorf("access must be read or write, not %q", access)
	}
	return nil
}

// mountTarget is what Unmount and SetMountAccess take: the knowledge base's id when the
// argument names a vault, else the argument as a mount name.
func mountTarget(ix *registry.Index, arg string) string {
	if en, err := ix.Find(arg); err == nil {
		return en.ID
	}
	return arg
}

func (s *Server) mountTool(ctx context.Context, req *mcp.CallToolRequest, a MountToolArgs) (*mcp.CallToolResult, MountToolOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, MountToolOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, MountToolOut{}, err
	}
	switch a.Action {
	case "mount":
		project, err := entryOf(ix, a.Project)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		kb, err := entryOf(ix, a.Knowledge)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		access := a.Access
		if access == "" {
			access = vault.AccessWrite
		}
		if err := checkAccess(access); err != nil {
			return nil, MountToolOut{}, err
		}
		m, err := acts.Mount(project, kb, access, a.As)
		if err != nil {
			if m.ID != "" {
				return nil, MountToolOut{}, fmt.Errorf("%w; the mount is recorded, so atlas with refresh can make kb/%s", err, m.Name)
			}
			return nil, MountToolOut{}, err
		}
		return s.mountOut(acts, project.ID, m.Name)
	case "access":
		project, err := entryOf(ix, a.Project)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		if err := checkAccess(a.Access); err != nil {
			return nil, MountToolOut{}, err
		}
		m, err := acts.EditMount(project, mountTarget(ix, a.Knowledge), a.Access)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		return s.mountOut(acts, project.ID, m.Name)
	case "unmount":
		project, err := entryOf(ix, a.Project)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		if err := acts.Unmount(project, mountTarget(ix, a.Knowledge)); err != nil {
			return nil, MountToolOut{}, err
		}
		after, _, err := acts.Refresh()
		if err != nil {
			return nil, MountToolOut{}, err
		}
		out := MountToolOut{Mounts: []registry.Mount{}}
		if p := after.ByID(project.ID); p != nil && p.Mounts != nil {
			out.Mounts = p.Mounts
		}
		return nil, out, nil
	case "grant", "revoke":
		kb, err := entryOf(ix, a.Knowledge)
		if err != nil {
			return nil, MountToolOut{}, err
		}
		if a.Action == "grant" {
			project, err := entryOf(ix, a.Project)
			if err != nil {
				return nil, MountToolOut{}, err
			}
			if err := checkAccess(a.Access); err != nil {
				return nil, MountToolOut{}, err
			}
			if err := acts.Grant(kb, project, a.Access); err != nil {
				return nil, MountToolOut{}, err
			}
		} else {
			// A stale grant's id first, so a project the scan lost can still be revoked.
			id := ""
			for _, g := range kb.Grants {
				if g.ID == a.Project {
					id = g.ID
				}
			}
			if id == "" {
				project, err := entryOf(ix, a.Project)
				if err != nil {
					return nil, MountToolOut{}, err
				}
				id = project.ID
			}
			if err := acts.Revoke(kb, id); err != nil {
				return nil, MountToolOut{}, err
			}
		}
		after, _, err := acts.Refresh()
		if err != nil {
			return nil, MountToolOut{}, err
		}
		out := MountToolOut{Grants: []registry.Grant{}}
		if k := after.ByID(kb.ID); k != nil && k.Grants != nil {
			out.Grants = k.Grants
		}
		return nil, out, nil
	}
	return nil, MountToolOut{}, fmt.Errorf("action must be mount, unmount, access, grant, or revoke, not %q", a.Action)
}

// mountOut rewrites the registry after a write and returns the project's mount by name,
// with its effective access.
func (s *Server) mountOut(acts actions.Atlas, projectID, mountName string) (*mcp.CallToolResult, MountToolOut, error) {
	ix, _, err := acts.Refresh()
	if err != nil {
		return nil, MountToolOut{}, err
	}
	if p := ix.ByID(projectID); p != nil {
		for i := range p.Mounts {
			if p.Mounts[i].Name == mountName {
				return nil, MountToolOut{Mount: &p.Mounts[i]}, nil
			}
		}
	}
	return nil, MountToolOut{}, fmt.Errorf("kb/%s is recorded but the scan does not list it; call atlas with refresh", mountName)
}

type ClusterToolArgs struct {
	Action    string `json:"action" jsonschema:"add or remove"`
	Cluster   string `json:"cluster" jsonschema:"the cluster, by name, id, or path; a knowledge base becomes a cluster with its first member"`
	Knowledge string `json:"knowledge" jsonschema:"add: the knowledge base to gather, by name, id, or path; remove: a member by name or id, even one the scan lost"`
}

// ClusterToolOut is the cluster's members after the change.
type ClusterToolOut struct {
	Cluster string         `json:"cluster"`
	Members []registry.Ref `json:"members"`
}

func (s *Server) clusterTool(ctx context.Context, req *mcp.CallToolRequest, a ClusterToolArgs) (*mcp.CallToolResult, ClusterToolOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, ClusterToolOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, ClusterToolOut{}, err
	}
	cluster, err := entryOf(ix, a.Cluster)
	if err != nil {
		return nil, ClusterToolOut{}, err
	}
	switch a.Action {
	case "add":
		kb, err := entryOf(ix, a.Knowledge)
		if err != nil {
			return nil, ClusterToolOut{}, err
		}
		if err := acts.AddMember(cluster, kb); err != nil {
			return nil, ClusterToolOut{}, err
		}
	case "remove":
		// The member first, by name or id, so one the scan lost still drops; only when
		// the cluster holds no such member does the argument name a vault.
		target := ""
		for _, m := range cluster.Members {
			if m.ID == a.Knowledge || strings.EqualFold(m.Name, a.Knowledge) {
				target = m.ID
			}
		}
		if target == "" {
			kb, err := entryOf(ix, a.Knowledge)
			if err != nil {
				return nil, ClusterToolOut{}, fmt.Errorf("%s holds no member named %q", cluster.Name, a.Knowledge)
			}
			target = kb.ID
			held := false
			for _, m := range cluster.Members {
				held = held || m.ID == target
			}
			if !held {
				return nil, ClusterToolOut{}, fmt.Errorf("%s holds no member named %q", cluster.Name, kb.Name)
			}
		}
		if err := acts.RemoveMember(cluster, target); err != nil {
			return nil, ClusterToolOut{}, err
		}
	default:
		return nil, ClusterToolOut{}, fmt.Errorf("action must be add or remove, not %q", a.Action)
	}
	after, _, err := acts.Refresh()
	if err != nil {
		return nil, ClusterToolOut{}, err
	}
	out := ClusterToolOut{Cluster: cluster.Name, Members: []registry.Ref{}}
	if c := after.ByID(cluster.ID); c != nil && c.Members != nil {
		out.Members = c.Members
	}
	return nil, out, nil
}

type RepoToolArgs struct {
	Action  string  `json:"action" jsonschema:"link, new, clone, unlink, or edit"`
	Project string  `json:"project" jsonschema:"the project, by name, id, or path"`
	Path    string  `json:"path,omitempty" jsonschema:"link: the repository's folder; edit: point the entry at another folder"`
	Init    bool    `json:"init,omitempty" jsonschema:"link: make a plain folder a git repository first, with one commit of what it holds"`
	URL     string  `json:"url,omitempty" jsonschema:"clone: an https, ssh, git, or file URL, or git@host:path"`
	Name    string  `json:"name,omitempty" jsonschema:"new: the repository's name; unlink and edit: which repository"`
	At      string  `json:"at,omitempty" jsonschema:"new and clone: where the repository goes, default repos/ inside the project"`
	Remote  *string `json:"remote,omitempty" jsonschema:"edit: the remote URL; an empty string clears it"`
	Changes *string `json:"changes,omitempty" jsonschema:"edit: how changes land there, pr or commit; an empty string returns to the atlas default"`
}

// RepoToolOut is the repository as the atlas now sees it, or the name unlink dropped.
type RepoToolOut struct {
	Repo     *RepoInfo `json:"repo,omitempty"`
	Unlinked string    `json:"unlinked,omitempty" jsonschema:"the repository dropped from the project; its folder stays"`
}

func (s *Server) repoTool(ctx context.Context, req *mcp.CallToolRequest, a RepoToolArgs) (*mcp.CallToolResult, RepoToolOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, RepoToolOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, RepoToolOut{}, err
	}
	project, err := entryOf(ix, a.Project)
	if err != nil {
		return nil, RepoToolOut{}, err
	}
	if project.Kind != vault.Project {
		return nil, RepoToolOut{}, fmt.Errorf("%s is a knowledge base and has no repositories; they belong to a project", project.Name)
	}
	var name string
	switch a.Action {
	case "link":
		if links.IsRemoteURL(a.Path) {
			return nil, RepoToolOut{}, fmt.Errorf("%s is a URL; pass it as url with action clone", a.Path)
		}
		repo, _, err := acts.AddRepo(project, home.Expand(a.Path), a.Init)
		var notRepo *vaults.NotRepoError
		if errors.As(err, &notRepo) {
			return nil, RepoToolOut{}, fmt.Errorf("%w; pass init to make it one", err)
		}
		if err != nil {
			return nil, RepoToolOut{}, err
		}
		name = repo.Name
	case "new":
		repo, _, err := acts.NewRepo(project, a.Name, home.Expand(a.At))
		if err != nil {
			return nil, RepoToolOut{}, err
		}
		name = repo.Name
	case "clone":
		repo, _, err := acts.CloneRepo(project, a.URL, home.Expand(a.At))
		if err != nil {
			return nil, RepoToolOut{}, err
		}
		name = repo.Name
	case "unlink":
		// The name as the project records it, like every other action returns; when the
		// project records no such repository, RemoveRepo says so.
		unlinked := a.Name
		for _, r := range project.Repos {
			if r.Name == a.Name {
				unlinked = r.Name
			}
		}
		if err := acts.RemoveRepo(project, a.Name); err != nil {
			return nil, RepoToolOut{}, err
		}
		if _, _, err := acts.Refresh(); err != nil {
			return nil, RepoToolOut{}, err
		}
		return nil, RepoToolOut{Unlinked: unlinked}, nil
	case "edit":
		repo, err := acts.EditRepo(project, a.Name, vaults.RepoEdit{Remote: a.Remote, Changes: a.Changes, Path: home.Expand(a.Path)})
		if err != nil {
			return nil, RepoToolOut{}, err
		}
		name = repo.Name
	default:
		return nil, RepoToolOut{}, fmt.Errorf("action must be link, new, clone, unlink, or edit, not %q", a.Action)
	}
	after, _, err := acts.Refresh()
	if err != nil {
		return nil, RepoToolOut{}, err
	}
	if p := after.ByID(project.ID); p != nil {
		for _, r := range p.Repos {
			if r.Name == name {
				info := repoInfo(p, r)
				return nil, RepoToolOut{Repo: &info}, nil
			}
		}
	}
	return nil, RepoToolOut{}, fmt.Errorf("%s is recorded but the scan does not list it; call atlas with refresh", name)
}

type SettingsArgs struct {
	NewDays     *int    `json:"new_days,omitempty" jsonschema:"a vault is new for this many days after its creation; 0 turns it off"`
	RepoChanges *string `json:"repo_changes,omitempty" jsonschema:"how a newly linked repository lands its changes: commit or pr"`
}

func (s *Server) settingsTool(ctx context.Context, req *mcp.CallToolRequest, a SettingsArgs) (*mcp.CallToolResult, Settings, error) {
	acts, cfg, err := s.bind()
	if err != nil {
		return nil, Settings{}, err
	}
	if a.NewDays != nil {
		if err := cfg.SetNewDays(*a.NewDays); err != nil {
			return nil, Settings{}, err
		}
	}
	if a.RepoChanges != nil {
		if err := cfg.SetDefaultChanges(*a.RepoChanges); err != nil {
			return nil, Settings{}, err
		}
	}
	if a.NewDays != nil || a.RepoChanges != nil {
		if err := s.home().Save(cfg); err != nil {
			return nil, Settings{}, err
		}
		if _, _, err := acts.Refresh(); err != nil {
			return nil, Settings{}, err
		}
	}
	return nil, settingsOf(cfg), nil
}

type StageArgs struct {
	Project string   `json:"project" jsonschema:"the project whose inbox receives the files, by name, id, or path"`
	Paths   []string `json:"paths,omitempty" jsonschema:"files or folders outside the vault; omit to stage what is new in the folders the project staged from before"`
	DryRun  bool     `json:"dry_run,omitempty" jsonschema:"plan only: say what would be copied and copy nothing"`
}

// StageOut is the plan, and after a copy, what was copied and the folders the project
// now stages from when paths is omitted.
type StageOut struct {
	Plan       *capture.StagePlan   `json:"plan"`
	Result     *capture.StageResult `json:"result,omitempty"`
	Remembered []string             `json:"remembered,omitempty"`
}

func (s *Server) stageTool(ctx context.Context, req *mcp.CallToolRequest, a StageArgs) (*mcp.CallToolResult, StageOut, error) {
	acts, _, err := s.bind()
	if err != nil {
		return nil, StageOut{}, err
	}
	ix, err := acts.Scan()
	if err != nil {
		return nil, StageOut{}, err
	}
	project, err := entryOf(ix, a.Project)
	if err != nil {
		return nil, StageOut{}, err
	}
	if project.Kind != vault.Project {
		return nil, StageOut{}, fmt.Errorf("%s is a knowledge base and has no inbox; stage into a project that mounts it", project.Name)
	}
	plan, err := acts.StagePlan(project, a.Paths)
	if err != nil {
		return nil, StageOut{}, err
	}
	out := StageOut{Plan: plan}
	if a.DryRun {
		return nil, out, nil
	}
	res, remembered, err := acts.Stage(project, plan)
	if err != nil {
		return nil, StageOut{}, err
	}
	// The inbox count is part of a vault's derived state, so the registry is rewritten.
	if _, _, err := acts.Refresh(); err != nil {
		return nil, StageOut{}, err
	}
	out.Result, out.Remembered = res, remembered
	return nil, out, nil
}
