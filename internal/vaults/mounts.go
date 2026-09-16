package vaults

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// MountLink is the symlink a mount makes: <project>/kb/<name> -> <knowledge base>/wiki.
func MountLink(project registry.Entry, name string) string { return project.KbDir(name) }

// What MountState reports about a mount's symlink.
const (
	MountOK      = "ok"
	MountMissing = "missing"
	MountWrong   = "wrong target"
)

// MountState is what a resolved mount's symlink is on disk. The symlink is local state:
// EnsureMounts creates it, and nothing else needs to.
func MountState(project registry.Entry, m registry.Mount) string {
	target, err := os.Readlink(project.KbDir(m.Name))
	switch {
	case err != nil:
		return MountMissing
	case target != m.Path:
		return MountWrong
	default:
		return MountOK
	}
}

// kbRoot is the folder that holds a project's mount symlinks.
func kbRoot(project registry.Entry) string { return project.KbDir("") }

// symlinkTo makes path a symlink to target: an existing correct symlink is left, an
// existing wrong symlink is replaced, and a real folder is refused.
func symlinkTo(path, target string) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		if current, rerr := os.Readlink(path); rerr == nil && current == target {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	case err == nil:
		return fmt.Errorf("%s is a folder, not a mount; move it away", home.Display(path))
	case !os.IsNotExist(err):
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.Symlink(target, path)
}

// checkMountTarget refuses a knowledge base already mounted, or a name project already
// uses for a mount.
func checkMountTarget(project, kb registry.Entry, name string) error {
	for _, m := range project.Mounts {
		if m.ID == kb.ID {
			return fmt.Errorf("%s already mounts %s", project.Name, kb.Name)
		}
		if strings.EqualFold(m.Name, name) {
			return fmt.Errorf("%s already has a mount named %q", project.Name, name)
		}
	}
	return nil
}

// Mount records kb in project's identity file with the requested access (read or
// write; write by default) under name (kb's name by default, cleaned), and creates the
// symlink. It refuses a knowledge base entry as project, a project entry as kb, a name
// already in use, and a knowledge base already mounted. When only the symlink step
// fails, the mount is still recorded and the returned value names it; EnsureMounts
// repairs the link later.
func Mount(project, kb registry.Entry, access, name string, now time.Time) (vault.Mount, error) {
	if project.Kind != vault.Project {
		return vault.Mount{}, fmt.Errorf("%s is not a project", project.Name)
	}
	if kb.Kind != vault.Knowledge {
		return vault.Mount{}, fmt.Errorf("%s is not a knowledge base", kb.Name)
	}
	if access == "" {
		access = vault.AccessWrite
	}
	if !vault.ValidAccess(access, false) {
		return vault.Mount{}, fmt.Errorf("access must be %s or %s, not %q", vault.AccessRead, vault.AccessWrite, access)
	}
	if name == "" {
		name = kb.Name
	}
	name = links.CleanName(name)
	if name == "" {
		return vault.Mount{}, fmt.Errorf("%s leaves no usable mount name", kb.Name)
	}
	if err := checkMountTarget(project, kb, name); err != nil {
		return vault.Mount{}, err
	}
	m := vault.Mount{ID: kb.ID, Name: name, Access: access}
	if err := vault.UpdateConfig(project.Path, "mount "+name, now, func(c *vault.Config) error {
		for _, existing := range c.Mounts {
			if existing.ID == kb.ID {
				return fmt.Errorf("%s already mounts %s", project.Name, kb.Name)
			}
			if strings.EqualFold(existing.Name, name) {
				return fmt.Errorf("%s already has a mount named %q", project.Name, name)
			}
		}
		c.Mounts = append(c.Mounts, m)
		return nil
	}); err != nil {
		return vault.Mount{}, err
	}
	if err := symlinkTo(project.KbDir(name), kb.Wiki()); err != nil {
		return m, err
	}
	return m, nil
}

// FindMount finds project's mount by kb id or mount name; nil when there is none.
func FindMount(project registry.Entry, target string) *registry.Mount {
	for i := range project.Mounts {
		m := project.Mounts[i]
		if m.ID == target || strings.EqualFold(m.Name, target) {
			return &m
		}
	}
	return nil
}

// Unmount removes the mount named by kb's id or the mount name from project's identity
// file and removes the symlink. The knowledge base is untouched. A real folder at the
// mount's path is refused before the identity file changes; the mount stays recorded.
func Unmount(project registry.Entry, target string, now time.Time) error {
	found := FindMount(project, target)
	if found == nil {
		return fmt.Errorf("%s has no mount named %q", project.Name, target)
	}
	path := project.KbDir(found.Name)
	info, lerr := os.Lstat(path)
	switch {
	case lerr != nil && !os.IsNotExist(lerr):
		return lerr
	case lerr == nil && info.Mode()&os.ModeSymlink == 0:
		return fmt.Errorf("%s is a folder, not a mount; leaving it in place", home.Display(path))
	}
	if err := vault.UpdateConfig(project.Path, "unmount "+found.Name, now, func(c *vault.Config) error {
		var keep []vault.Mount
		removed := false
		for _, m := range c.Mounts {
			if m.ID == target || strings.EqualFold(m.Name, target) {
				removed = true
				continue
			}
			keep = append(keep, m)
		}
		if !removed {
			return fmt.Errorf("%s has no mount named %q", project.Name, target)
		}
		c.Mounts = keep
		return nil
	}); err != nil {
		return err
	}
	if lerr != nil {
		// os.IsNotExist(lerr): nothing to remove.
		return nil
	}
	return os.Remove(path)
}

// Grant records project's access on kb: read or write. A knowledge base that is open
// stays open; the grant applies when it is guarded. It refuses a project entry as kb.
func Grant(kb, project registry.Entry, access string, now time.Time) error {
	if kb.Kind != vault.Knowledge {
		return fmt.Errorf("%s is not a knowledge base", kb.Name)
	}
	if project.Kind != vault.Project {
		return fmt.Errorf("%s is not a project", project.Name)
	}
	if !vault.ValidAccess(access, false) {
		return fmt.Errorf("access must be %s or %s, not %q", vault.AccessRead, vault.AccessWrite, access)
	}
	g := vault.Grant{ID: project.ID, Name: project.Name, Access: access}
	return vault.UpdateConfig(kb.Path, "grant "+project.Name+" "+access, now, func(c *vault.Config) error {
		for i := range c.Grants {
			if c.Grants[i].ID == project.ID {
				c.Grants[i] = g
				return nil
			}
		}
		c.Grants = append(c.Grants, g)
		return nil
	})
}

// Revoke removes project's grant from kb.
func Revoke(kb, project registry.Entry, now time.Time) error {
	has := false
	for _, g := range kb.Grants {
		if g.ID == project.ID {
			has = true
			break
		}
	}
	if !has {
		return fmt.Errorf("%s has no grant for %s", kb.Name, project.Name)
	}
	return vault.UpdateConfig(kb.Path, "revoke "+project.Name, now, func(c *vault.Config) error {
		var keep []vault.Grant
		removed := false
		for _, g := range c.Grants {
			if g.ID == project.ID {
				removed = true
				continue
			}
			keep = append(keep, g)
		}
		if !removed {
			return fmt.Errorf("%s has no grant for %s", kb.Name, project.Name)
		}
		c.Grants = keep
		return nil
	})
}

// MountRepair is what one run of EnsureMounts did to a project's mount symlinks: Created
// names a link it made, Repaired one that led elsewhere, Removed a link under kb/ that no
// mount names, and Missing a mount whose knowledge base the index does not hold.
type MountRepair struct {
	Created  []string
	Repaired []string
	Removed  []string
	Missing  []string
}

// EnsureMounts creates every symlink project's mounts need, repoints one that leads
// elsewhere, and removes symlinks under kb/ that no mount names. One mount it cannot fix
// does not stop the others; the first failure comes back once the project is done.
func EnsureMounts(project registry.Entry, ix *registry.Index) (MountRepair, error) {
	root := kbRoot(project)
	var rep MountRepair
	var firstErr error
	fail := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	// named compares a folder name to the mount names the way Mount does.
	named := func(dir string) bool {
		for _, m := range project.Mounts {
			if strings.EqualFold(m.Name, dir) {
				return true
			}
		}
		return false
	}
	wanted := map[string]string{}
	for _, m := range project.Mounts {
		kb := ix.ByID(m.ID)
		if kb == nil || kb.Kind != vault.Knowledge {
			rep.Missing = append(rep.Missing, m.Name)
			continue
		}
		wanted[m.Name] = kb.Wiki()
	}

	entries, rerr := os.ReadDir(root)
	if rerr != nil && !os.IsNotExist(rerr) {
		return rep, rerr
	}
	for _, entry := range entries {
		if named(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, lerr := os.Lstat(path)
		if lerr != nil {
			fail(lerr)
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if err := os.Remove(path); err != nil {
			fail(err)
			continue
		}
		rep.Removed = append(rep.Removed, entry.Name())
	}

	for _, m := range project.Mounts {
		target, ok := wanted[m.Name]
		if !ok {
			continue
		}
		path := filepath.Join(root, m.Name)
		before, lerr := os.Lstat(path)
		linked := lerr == nil && before.Mode()&os.ModeSymlink != 0
		if linked {
			if current, _ := os.Readlink(path); current == target {
				continue
			}
		}
		if err := symlinkTo(path, target); err != nil {
			fail(err)
			continue
		}
		if linked {
			rep.Repaired = append(rep.Repaired, m.Name)
		} else {
			rep.Created = append(rep.Created, m.Name)
		}
	}

	sort.Strings(rep.Created)
	sort.Strings(rep.Repaired)
	sort.Strings(rep.Removed)
	sort.Strings(rep.Missing)
	return rep, firstErr
}
