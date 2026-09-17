package vaults

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/nathanaday/claude-atlas/internal/claudecode"
	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/obsidian"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// Relocating moves the whole vaults directory to another folder. It is the one atlas
// action that moves a vault's bytes without touching a vault's contents: an identity
// file holds no path, so nothing inside a vault has to change. What changes is the
// atlas config, and what follows is a refresh, which rewrites the registry and
// recreates each project's kb/ symlinks at the new root.

// Rewrite is one recorded path the move changes in the atlas config.
type Rewrite struct {
	// What names the setting: "vaults dir", "vault NAME", or "repo PROJECT/NAME".
	What string
	From string
	To   string
}

// The kinds of state a move breaks. A caller groups warnings by kind; every warning of
// one kind carries the same Reason.
const (
	WarnObsidian   = "obsidian"
	WarnClaudeCode = "claude-code"
	WarnWorktree   = "worktree"
	WarnVenv       = "venv"
	WarnCMake      = "cmake"
	WarnPackages   = "packages"
	WarnUnreadable = "unreadable"
)

// warnOrder is the order a caller shows the kinds in: what the atlas itself notices
// first, then what lives inside a repository.
var warnOrder = []string{WarnObsidian, WarnClaudeCode, WarnWorktree, WarnVenv, WarnCMake, WarnPackages, WarnUnreadable}

// Warning is state the move breaks that the atlas does not own. Nothing repairs these;
// the plan reports them so the user can.
type Warning struct {
	Kind   string
	Path   string
	Reason string
}

// WarningGroup is every warning of one kind, with the one remedy they share.
type WarningGroup struct {
	Kind   string
	Reason string
	Paths  []string
}

// GroupWarnings gathers a plan's warnings by kind, in a fixed order, so a preview shows
// one remedy and a count instead of one line per file.
func (p *RelocatePlan) GroupWarnings() []WarningGroup {
	byKind := map[string]*WarningGroup{}
	for _, w := range p.Warnings {
		g, ok := byKind[w.Kind]
		if !ok {
			g = &WarningGroup{Kind: w.Kind, Reason: w.Reason}
			byKind[w.Kind] = g
		}
		g.Paths = append(g.Paths, w.Path)
	}
	var out []WarningGroup
	for _, kind := range warnOrder {
		if g, ok := byKind[kind]; ok {
			out = append(out, *g)
		}
	}
	return out
}

// RelocatePlan is what relocating would do, before it does it.
type RelocatePlan struct {
	From string
	To   string
	// SameVolume says the move is a rename. When it is false the move copies, verifies,
	// and only then removes the old tree.
	SameVolume bool
	// Vaults counts the vaults found under From.
	Vaults int
	Files  int
	Bytes  int64
	// Free is the space left where the tree is going, 0 when it cannot be read.
	Free     int64
	Rewrites []Rewrite
	Warnings []Warning
}

// PlanRelocate validates a move of the vaults directory to `to` and reports what it
// would change. It writes nothing.
//
// It refuses a target that overlaps the vaults directory, one inside a vault, one that
// holds files, and a move while a vault has an interrupted operation to recover. It
// cannot see a vault another process holds the lock on without writing, so a caller
// confirms with the user before applying.
func PlanRelocate(h home.Home, cfg *home.Config, to string) (*RelocatePlan, error) {
	from := filepath.Clean(home.Expand(cfg.VaultsDir))
	if from == "" || from == "." {
		return nil, errors.New("the config names no vaults directory")
	}
	if info, err := os.Stat(from); err != nil {
		return nil, fmt.Errorf("the vaults directory %s cannot be read: %w", home.Display(from), err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", home.Display(from))
	}
	target, err := filepath.Abs(home.Expand(to))
	if err != nil {
		return nil, err
	}
	if err := checkTargetRoot(from, target); err != nil {
		return nil, err
	}
	p := &RelocatePlan{From: from, To: target}
	if err := countVaults(cfg, from, p); err != nil {
		return nil, err
	}
	if err := walkTree(from, p); err != nil {
		return nil, err
	}
	p.SameVolume, p.Free = volumeFacts(from, target)
	if !p.SameVolume && p.Free > 0 && p.Free < p.Bytes {
		return nil, fmt.Errorf("%s is on another volume with %s free; the vaults need %s",
			home.Display(target), console.Size(p.Free), console.Size(p.Bytes))
	}
	p.Rewrites = rewritesFor(cfg, from, target)
	p.Warnings = append(p.Warnings, outsideWarnings(from)...)
	sort.Slice(p.Warnings, func(i, j int) bool { return p.Warnings[i].Path < p.Warnings[j].Path })
	return p, nil
}

// checkTargetRoot refuses a target the vaults cannot go to.
func checkTargetRoot(from, target string) error {
	if _, ok := under(from, target); ok {
		return fmt.Errorf("%s is inside the vaults directory; choose a folder outside %s", home.Display(target), home.Display(from))
	}
	if _, ok := under(target, from); ok {
		return fmt.Errorf("%s holds the vaults directory; choose a folder outside %s", home.Display(target), home.Display(from))
	}
	switch info, err := os.Stat(target); {
	case err == nil && !info.IsDir():
		return fmt.Errorf("%s is a file; the vaults need a folder", home.Display(target))
	case err == nil:
		entries, rerr := os.ReadDir(target)
		if rerr != nil {
			return rerr
		}
		if len(entries) > 0 {
			return fmt.Errorf("%s exists and is not empty; choose an empty folder or one that does not exist yet", home.Display(target))
		}
	case !os.IsNotExist(err):
		return err
	}
	return checkNotInsideVault(target)
}

// countVaults counts the vaults under from and refuses the move while one has an
// operation to recover: moving the tree would take the recovery's ground with it.
func countVaults(cfg *home.Config, from string, p *RelocatePlan) error {
	ix, err := registry.Scan(cfg)
	if err != nil {
		return err
	}
	for _, e := range ix.Entries {
		if _, ok := under(from, e.Path); !ok {
			continue
		}
		p.Vaults++
		if e.Error != "" {
			continue
		}
		v, err := vault.Open(e.Path)
		if err != nil {
			continue
		}
		pending, err := txn.Pending(v)
		if err == nil && pending != nil {
			return fmt.Errorf("%s has an interrupted operation; run `claude-atlas recover %s` first", e.Name, e.Name)
		}
	}
	return nil
}

// walkTree counts the tree and collects every warning the files themselves carry.
func walkTree(from string, p *RelocatePlan) error {
	sep := string(filepath.Separator)
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == from {
				return err
			}
			p.Warnings = append(p.Warnings, Warning{Kind: WarnUnreadable, Path: path, Reason: "could not be read while planning; check it after the move"})
			return nil
		}
		rel, _ := filepath.Rel(from, path)
		switch {
		case d.IsDir():
			// One warning per outermost node_modules; the nested ones come with it.
			if d.Name() == "node_modules" && !strings.Contains(filepath.Dir(rel)+sep, sep+"node_modules"+sep) {
				p.Warnings = append(p.Warnings, Warning{Kind: WarnPackages, Path: path, Reason: "installed packages that may hold the old path; reinstall if anything misbehaves"})
			}
		case d.Name() == "pyvenv.cfg":
			p.Warnings = append(p.Warnings, Warning{Kind: WarnVenv, Path: filepath.Dir(path), Reason: "Python virtual environments; each holds the old path in pyvenv.cfg, bin/activate, and every shebang, so recreate them"})
		case d.Name() == "CMakeCache.txt":
			p.Warnings = append(p.Warnings, Warning{Kind: WarnCMake, Path: filepath.Dir(path), Reason: "CMake build folders; each cache holds the old source and build paths, so delete the folder and configure again"})
		case d.Name() == ".git":
			p.Warnings = append(p.Warnings, Warning{Kind: WarnWorktree, Path: filepath.Dir(path), Reason: "git worktrees or submodules; each names its git directory by absolute path, so run `git worktree repair` in the repository that owns it"})
		}
		if d.Type().IsRegular() {
			p.Files++
			if info, ierr := d.Info(); ierr == nil {
				p.Bytes += info.Size()
			}
		}
		return nil
	})
}

// rewritesFor lists the config settings the move changes: the vaults directory, and
// every recorded vault or repository path that sat under the old root. A path outside
// the old root does not move, so it keeps its entry.
func rewritesFor(cfg *home.Config, from, target string) []Rewrite {
	swap := func(what, path string) (Rewrite, bool) {
		rel, ok := under(from, path)
		if !ok {
			return Rewrite{}, false
		}
		return Rewrite{What: what, From: path, To: filepath.Join(target, rel)}, true
	}
	out := []Rewrite{{What: "vaults dir", From: from, To: target}}
	for _, v := range cfg.Vaults {
		if r, ok := swap("vault "+filepath.Base(v), v); ok {
			out = append(out, r)
		}
	}
	keys := make([]string, 0, len(cfg.Repos))
	for key := range cfg.Repos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if r, ok := swap("repo "+key, cfg.Repos[key]); ok {
			out = append(out, r)
		}
	}
	return out
}

// outsideWarnings asks the two other programs that key their state by absolute path
// what they hold under the old root. Neither is rewritten: the atlas does not edit
// another program's state file.
func outsideWarnings(from string) []Warning {
	var out []Warning
	if reg, err := obsidian.LoadRegistry(); err == nil {
		for _, path := range reg.Under(from) {
			out = append(out, Warning{Kind: WarnObsidian, Path: path, Reason: "vaults Obsidian knows at the old path; it drops a missing entry at its next launch, and `claude-atlas open-vault` registers them again"})
		}
	}
	for _, path := range claudecode.ProjectsUnder(from) {
		out = append(out, Warning{Kind: WarnClaudeCode, Path: path, Reason: "projects Claude Code keys by path; their session history, permissions, and memory stay under the old one"})
	}
	return out
}

// volumeFacts reports whether the target sits on the same filesystem as the tree, and
// how much space is free where the tree is going.
func volumeFacts(from, target string) (same bool, free int64) {
	anchor := nearestExisting(target)
	var a, b syscall.Stat_t
	if syscall.Stat(from, &a) == nil && syscall.Stat(anchor, &b) == nil {
		same = uint64(a.Dev) == uint64(b.Dev)
	}
	var fsys syscall.Statfs_t
	if syscall.Statfs(anchor, &fsys) == nil {
		free = int64(uint64(fsys.Bsize) * fsys.Bavail)
	}
	return same, free
}

// nearestExisting walks up from path to the first folder that exists.
func nearestExisting(path string) string {
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

// renameTree is os.Rename; a test replaces it to take the copy path a rename across a
// managed folder's boundary forces.
var renameTree = os.Rename

// ApplyRelocate carries out a plan: it moves the tree, then rewrites the config in one
// save. On the same volume the move is a rename and a failed save renames back. Across
// volumes the tree is copied and verified, the config is saved, and only then is the
// old tree removed, so a failure never leaves the vaults in one place only.
//
// The caller runs a refresh afterwards: the registry and every project's kb/ symlinks
// hold the old paths until it does.
func ApplyRelocate(h home.Home, cfg *home.Config, p *RelocatePlan) error {
	if p == nil {
		return errors.New("no plan")
	}
	if err := os.MkdirAll(filepath.Dir(p.To), 0o755); err != nil {
		return err
	}
	if p.SameVolume {
		// Rename wants the target gone; the plan already refused a folder holding files.
		if err := os.Remove(p.To); err != nil && !os.IsNotExist(err) {
			return err
		}
		err := renameTree(p.From, p.To)
		if err == nil {
			if serr := saveRewrites(h, cfg, p); serr != nil {
				renameTree(p.To, p.From)
				return serr
			}
			return nil
		}
		// A folder the system manages on its own, such as an iCloud container, refuses a
		// rename out of itself although both paths report the same device. Copy instead.
		if !errors.Is(err, syscall.EXDEV) {
			return fmt.Errorf("move %s to %s: %w", home.Display(p.From), home.Display(p.To), err)
		}
	}
	if err := copyTree(p.From, p.To); err != nil {
		os.RemoveAll(p.To)
		return fmt.Errorf("copy %s to %s: %w", home.Display(p.From), home.Display(p.To), err)
	}
	if err := verifyTree(p.From, p.To); err != nil {
		os.RemoveAll(p.To)
		return fmt.Errorf("the copy does not match %s: %w", home.Display(p.From), err)
	}
	if err := saveRewrites(h, cfg, p); err != nil {
		os.RemoveAll(p.To)
		return err
	}
	if err := os.RemoveAll(p.From); err != nil {
		return fmt.Errorf("the vaults are at %s and the config points there, but the old tree could not be removed: %w", home.Display(p.To), err)
	}
	return nil
}

// saveRewrites applies the plan's rewrites to the config and saves it. A failed save
// leaves the config as it was, so a caller can put the tree back.
func saveRewrites(h home.Home, cfg *home.Config, p *RelocatePlan) error {
	before := *cfg
	before.Vaults = append([]string(nil), cfg.Vaults...)
	before.Repos = map[string]string{}
	for k, v := range cfg.Repos {
		before.Repos[k] = v
	}
	cfg.VaultsDir = p.To
	for _, r := range p.Rewrites {
		switch {
		case r.What == "vaults dir":
		case strings.HasPrefix(r.What, "vault "):
			cfg.RemoveVault(r.From)
			cfg.AddVault(r.To)
		case strings.HasPrefix(r.What, "repo "):
			cfg.Repos[strings.TrimPrefix(r.What, "repo ")] = r.To
		}
	}
	if err := h.Save(cfg); err != nil {
		*cfg = before
		return err
	}
	return nil
}

// copyTree copies src to dst whole: folders with their permissions, regular files with
// their contents and mode, and symlinks as symlinks, so a project's kb/ links and a
// repository's own links survive as they were.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		case d.Type()&fs.ModeSymlink != 0:
			link, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			return copyFile(path, target, info)
		default:
			// A socket or device file is not a vault's business; leave it behind.
			return nil
		}
	})
}

func copyFile(src, dst string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// verifyTree checks that every file and symlink under src arrived at dst with the same
// size or the same link target. It runs before the old tree is removed.
func verifyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		switch {
		case d.IsDir():
			if s, serr := os.Stat(target); serr != nil || !s.IsDir() {
				return fmt.Errorf("%s is missing", rel)
			}
		case d.Type()&fs.ModeSymlink != 0:
			was, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			now, lerr := os.Readlink(target)
			if lerr != nil || now != was {
				return fmt.Errorf("%s is not the same link", rel)
			}
		case info.Mode().IsRegular():
			s, serr := os.Lstat(target)
			if serr != nil {
				return fmt.Errorf("%s is missing", rel)
			}
			if s.Size() != info.Size() {
				return fmt.Errorf("%s is %d bytes, was %d", rel, s.Size(), info.Size())
			}
		}
		return nil
	})
}
