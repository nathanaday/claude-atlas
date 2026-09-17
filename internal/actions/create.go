package actions

import (
	"fmt"
	"time"

	"github.com/nathanaday/claude-atlas/internal/console"
	"github.com/nathanaday/claude-atlas/internal/home"
	"github.com/nathanaday/claude-atlas/internal/registry"
	"github.com/nathanaday/claude-atlas/internal/vault"
	"github.com/nathanaday/claude-atlas/internal/vaults"
)

// createOrAdopt makes or adopts the knowledge base and registers it. The preview, when
// there is one, is the caller's: Create runs with confirm off.
func createOrAdopt(h home.Home, cfg *home.Config, c *console.Console, choice AddKnowledge) (string, error) {
	mode := vault.Generic
	if choice.Mode != "" {
		var err error
		if mode, err = vault.ParseMode(choice.Mode); err != nil {
			return "", err
		}
	}
	opts := vault.Options{Mode: mode, Name: choice.Name, Scope: choice.Scope}
	if choice.Adopt {
		if _, err := vault.Adopt(choice.Path, opts, time.Now()); err != nil {
			return "", err
		}
	} else if _, err := vaults.Create(choice.Path, opts, c, false); err != nil {
		return "", err
	}
	if _, err := vaults.Register(h, cfg, choice.Path); err != nil {
		return "", err
	}
	return choice.Path, nil
}

func errNoKnowledge(en registry.Entry) error {
	if en.Knowledge != nil && en.Knowledge.Error != "" {
		return fmt.Errorf("%s: %s", en.Name, en.Knowledge.Error)
	}
	return fmt.Errorf("%s uses no knowledge base; link one with `claude-atlas link KB`", en.Name)
}
