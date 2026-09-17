package claudecode

import (
	"errors"
	"os"
	"os/exec"

	"github.com/nathanaday/claude-atlas/internal/home"
)

// LaunchConfig is the claude_code section of config.json: Command (normally "claude"),
// Args placed before the prompt, an optional Prompt sent as the first message (for
// example "/claude-atlas:wiki"), and SessionContext, which lets the plugin's SessionStart
// hook hand Claude the vault's hot.md.
type LaunchConfig = home.LaunchConfig

var ErrNoClaude = errors.New("the `claude` command is not on PATH")

// EnvVault names the place, a knowledge base or a project's work folder, for the MCP
// server and hooks; EnvSessionContext turns the session-start context on ("1") or off
// ("0") for this launch.
const (
	EnvVault          = "CLAUDE_ATLAS_VAULT"
	EnvSessionContext = "CLAUDE_ATLAS_SESSION_CONTEXT"
)

// IngestPrompt is the first message that starts an ingest of the inbox.
const IngestPrompt = "/claude-atlas:wiki-ingest"

// DescribePrompt is the first message that writes the page describing a project.
const DescribePrompt = "/claude-atlas:describe"

// TaskPrompt is the first message that continues a task.
func TaskPrompt(taskID string) string { return "/claude-atlas:task-run " + taskID }

// LaunchCommand builds the process that runs Claude Code in a knowledge base or a
// project's work folder, with the place selected explicitly so the plugin never has to
// guess. prompt is the first message; empty means the configured one, if any.
func LaunchCommand(cfg LaunchConfig, place, prompt string) (*exec.Cmd, error) {
	return LaunchIn(cfg, place, place, prompt)
}

// LaunchIn is LaunchCommand with the session started in dir, while the place stays
// selected through the environment.
func LaunchIn(cfg LaunchConfig, place, dir, prompt string) (*exec.Cmd, error) {
	if dir == "" {
		dir = place
	}
	command := cfg.Command
	if command == "" {
		command = "claude"
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return nil, ErrNoClaude
	}
	args := append([]string{}, cfg.Args...)
	if prompt == "" {
		prompt = cfg.Prompt
	}
	if prompt != "" {
		args = append(args, prompt)
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	context := "0"
	if cfg.SessionContext {
		context = "1"
	}
	cmd.Env = append(os.Environ(), EnvVault+"="+place, EnvSessionContext+"="+context)
	return cmd, nil
}
