package vaults

import (
	"fmt"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

// IsCluster reports whether a knowledge base gathers others. A knowledge base with no
// members is an ordinary one; gaining a member makes it a cluster and losing its last
// makes it ordinary again.
func IsCluster(e registry.Entry) bool { return e.Kind == vault.Knowledge && len(e.Members) > 0 }

// AddMember records kb in cluster's identity file. A project that mounts the cluster
// reaches the new member at its next refresh. It refuses a member that is not a knowledge
// base, a cluster as a member, a cluster that is itself a member, and a knowledge base the
// cluster already holds.
func AddMember(cluster, kb registry.Entry, now time.Time) error {
	if cluster.Kind != vault.Knowledge {
		return fmt.Errorf("%s is not a knowledge base", cluster.Name)
	}
	if kb.Kind != vault.Knowledge {
		return fmt.Errorf("%s is not a knowledge base", kb.Name)
	}
	if kb.ID == cluster.ID {
		return fmt.Errorf("%s cannot be its own member", cluster.Name)
	}
	if IsCluster(kb) {
		return fmt.Errorf("%s is a cluster; a cluster does not hold another cluster yet", kb.Name)
	}
	if len(cluster.Clusters) > 0 {
		return fmt.Errorf("%s is a member of %s; a cluster does not hold another cluster yet", cluster.Name, cluster.Clusters[0].Name)
	}
	for _, m := range cluster.Members {
		if m.ID == kb.ID {
			return fmt.Errorf("%s already holds %s", cluster.Name, kb.Name)
		}
	}
	return vault.UpdateConfig(cluster.Path, "member "+kb.Name, now, func(c *vault.Config) error {
		for _, m := range c.Members {
			if m.ID == kb.ID {
				return fmt.Errorf("%s already holds %s", cluster.Name, kb.Name)
			}
		}
		c.Members = append(c.Members, vault.Member{ID: kb.ID, Name: kb.Name})
		return nil
	})
}

// RemoveMember drops the member named by its id or its name from cluster's identity file.
// The knowledge base is untouched, and a project that mounts the cluster loses the mount
// at its next refresh.
func RemoveMember(cluster registry.Entry, target string, now time.Time) error {
	if cluster.Kind != vault.Knowledge {
		return fmt.Errorf("%s is not a knowledge base", cluster.Name)
	}
	return vault.UpdateConfig(cluster.Path, "drop member "+target, now, func(c *vault.Config) error {
		var keep []vault.Member
		removed := false
		for _, m := range c.Members {
			// Two members may share a display name. Drop the first match only.
			if !removed && (m.ID == target || strings.EqualFold(m.Name, target)) {
				removed = true
				continue
			}
			keep = append(keep, m)
		}
		if !removed {
			return fmt.Errorf("%s holds no member named %q", cluster.Name, target)
		}
		c.Members = keep
		return nil
	})
}
