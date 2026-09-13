package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchCommandSetsVaultAndConsent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", dir)
	cmd, err := LaunchCommand(LaunchConfig{Command: "claude", Prompt: "/claude-atlas:wiki", Args: []string{"--model", "opus"}, SessionContext: true}, "/v/one", "")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "/v/one" || strings.Join(cmd.Args[1:], " ") != "--model opus /claude-atlas:wiki" {
		t.Fatalf("dir=%s args=%v", cmd.Dir, cmd.Args)
	}
	env := strings.Join(cmd.Env, "\n")
	for _, want := range []string{"CLAUDE_ATLAS_VAULT=/v/one", "CLAUDE_ATLAS_SESSION_CONTEXT=1"} {
		if !strings.Contains(env, want) {
			t.Errorf("missing %s", want)
		}
	}
	quiet, _ := LaunchCommand(LaunchConfig{SessionContext: false}, "/v/one", "")
	if !strings.Contains(strings.Join(quiet.Env, "\n"), "CLAUDE_ATLAS_SESSION_CONTEXT=0") {
		t.Fatal("session context should be off when not enabled")
	}
}

func TestLaunchCommandPromptOverride(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"), 0o755)
	t.Setenv("PATH", dir)
	cmd, _ := LaunchCommand(LaunchConfig{Prompt: "/claude-atlas:wiki"}, "/v", IngestPrompt)
	if strings.Join(cmd.Args[1:], " ") != IngestPrompt {
		t.Fatalf("args %v", cmd.Args)
	}
}

func TestLaunchCommandWithoutClaude(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := LaunchCommand(LaunchConfig{}, "/v", ""); err != ErrNoClaude {
		t.Fatalf("got %v", err)
	}
}
