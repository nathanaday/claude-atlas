package actions

import (
	"fmt"
	"time"

	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/tasks"
	"github.com/nathanaday/claude-atlas/internal/txn"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// createOrAdopt makes or adopts the vault, records the facts the template does not
// carry, registers it, and mounts or gathers what the caller chose. The preview, when
// there is one, is the caller's: Create runs with confirm off, and CreateIn with no
// console.
func createOrAdopt(h home.Home, cfg *home.Config, c *console.Console, choice AddVault) (string, error) {
	mode := vault.Generic
	if choice.Mode != "" {
		var err error
		if mode, err = vault.ParseMode(choice.Mode); err != nil {
			return "", err
		}
	}
	opts := vault.Options{Kind: choice.Kind, Mode: mode, Name: choice.Name}
	switch {
	case choice.Adopt:
		// An adopt with no kind keeps the vault's own, and recordFacts files the facts
		// under the kind the vault turned out to have.
		res, err := vault.Adopt(choice.Path, opts, time.Now())
		if err != nil {
			return "", err
		}
		choice.Kind = res.Kind
	case choice.InRepo != "":
		path, err := vaults.CreateIn(choice.InRepo, opts, nil)
		if err != nil {
			return "", err
		}
		choice.Path = path
	default:
		if _, err := vaults.Create(choice.Path, opts, c, false); err != nil {
			return "", err
		}
	}
	if err := recordFacts(h, cfg, choice); err != nil {
		return "", err
	}
	if _, err := vaults.Register(h, cfg, choice.Path); err != nil {
		return "", err
	}
	if err := mountChoice(cfg, choice); err != nil {
		return choice.Path, err
	}
	if err := memberChoice(cfg, choice); err != nil {
		return choice.Path, err
	}
	return choice.Path, nil
}

// recordFacts writes the identity fields the template does not carry: a project's tags,
// a knowledge base's scope and access.
func recordFacts(h home.Home, cfg *home.Config, choice AddVault) error {
	edit := vaults.Edit{}
	switch choice.Kind {
	case vault.Knowledge:
		if choice.Scope != "" {
			edit.Scope = &choice.Scope
		}
		if choice.Access != "" {
			edit.Access = &choice.Access
		}
	case vault.Project:
		if len(choice.Tags) > 0 {
			edit.Tags = &choice.Tags
		}
	}
	if edit == (vaults.Edit{}) {
		return nil
	}
	_, err := vaults.EditIdentity(h, cfg, registry.Entry{Path: choice.Path, Kind: choice.Kind}, edit, time.Now())
	return err
}

// mountChoice mounts the knowledge base a new project chose. The project is already
// written, so a failure here names the mount and leaves the vault.
func mountChoice(cfg *home.Config, choice AddVault) error {
	if choice.MountID == "" {
		return nil
	}
	ix, err := registry.Scan(cfg)
	if err != nil {
		return err
	}
	kb := ix.ByID(choice.MountID)
	if kb == nil {
		return fmt.Errorf("no knowledge base with id %s to mount", choice.MountID)
	}
	project := ix.ByPath(choice.Path)
	if project == nil {
		return fmt.Errorf("%s is not in the scan yet; mount %s by hand", choice.Name, kb.Name)
	}
	_, err = vaults.Mount(*project, *kb, vault.AccessWrite, "", time.Now())
	return err
}

// memberChoice records the knowledge bases a new cluster gathers. The cluster is already
// written, so a failure here names the member and leaves the vault.
func memberChoice(cfg *home.Config, choice AddVault) error {
	if len(choice.MemberIDs) == 0 {
		return nil
	}
	for _, id := range choice.MemberIDs {
		ix, err := registry.Scan(cfg)
		if err != nil {
			return err
		}
		cluster := ix.ByPath(choice.Path)
		if cluster == nil {
			return fmt.Errorf("%s is not in the scan yet; add its members by hand", choice.Name)
		}
		kb := ix.ByID(id)
		if kb == nil {
			return fmt.Errorf("no knowledge base with id %s to gather", id)
		}
		if err := vaults.AddMember(*cluster, *kb, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

// plantTask plants one task in a vault as a single operation.
func plantTask(v *vault.Vault, plant tasks.Plant) (txn.Planted, error) {
	now := time.Now()
	req, planted, err := txn.PlantRequest(v, plant, "", now)
	if err != nil {
		return txn.Planted{}, err
	}
	plan, err := txn.Prepare(v, req, now)
	if err != nil {
		return txn.Planted{}, err
	}
	if _, err := txn.Apply(v, plan, now); err != nil {
		return txn.Planted{}, err
	}
	return planted, nil
}
