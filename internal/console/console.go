// Package console prints steps and asks questions on the terminal.
package console

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

var ErrNotInteractive = errors.New("stdin is not a terminal; pass --yes to run without prompts")

type Console struct {
	AssumeYes bool
	Out       io.Writer
	in        *bufio.Reader
	inIsTTY   bool
}

func New(assumeYes bool) *Console {
	tty := term.IsTerminal(os.Stdin.Fd())
	return &Console{AssumeYes: assumeYes, Out: os.Stdout, in: bufio.NewReader(os.Stdin), inIsTTY: tty}
}

// NewWith builds a console over explicit streams, for tests.
func NewWith(assumeYes bool, in io.Reader, out io.Writer, interactive bool) *Console {
	return &Console{AssumeYes: assumeYes, Out: out, in: bufio.NewReader(in), inIsTTY: interactive}
}

func (c *Console) Interactive() bool { return c.inIsTTY }

func (c *Console) Say(format string, args ...any) {
	fmt.Fprintf(c.Out, format+"\n", args...)
}

type Status int

const (
	OK Status = iota
	Skip
	Fail
)

func (c *Console) Step(status Status, label, detail string) {
	mark := map[Status]string{OK: "✓", Skip: "·", Fail: "✗"}[status]
	if detail == "" {
		fmt.Fprintf(c.Out, "  %s %s\n", mark, label)
		return
	}
	fmt.Fprintf(c.Out, "  %s %-16s %s\n", mark, label, detail)
}

func (c *Console) Confirm(prompt string, def bool) (bool, error) {
	if c.AssumeYes {
		return true, nil
	}
	if !c.inIsTTY {
		return false, ErrNotInteractive
	}
	suffix := "[Y/n]"
	if !def {
		suffix = "[y/N]"
	}
	for {
		fmt.Fprintf(c.Out, "%s %s ", prompt, suffix)
		line, err := c.in.ReadString('\n')
		if err != nil && line == "" {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}

func (c *Console) Ask(prompt, def string) string {
	if c.AssumeYes || !c.inIsTTY {
		return def
	}
	fmt.Fprintf(c.Out, "%s [%s]: ", prompt, def)
	line, _ := c.in.ReadString('\n')
	if answer := strings.TrimSpace(line); answer != "" {
		return answer
	}
	return def
}
