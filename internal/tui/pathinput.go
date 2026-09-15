package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nathanaday/claude-atlas/internal/home"
)

// pathField is a text input that completes paths the way a shell does: the best match
// shows as ghost text, Tab accepts it, ↑↓ cycle the matches, and the matches show under
// the line. Names that are not paths, such as the folders a vault ingested from, can be
// offered too.
type pathField struct {
	input textinput.Model
	names []string
	last  string
}

const pathMatchesShown = 6

func newPathField(placeholder string, width int) pathField {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.CharLimit = 400
	input.Width = width
	input.ShowSuggestions = true
	return pathField{input: input}
}

func (f *pathField) focus() tea.Cmd {
	f.refresh()
	return f.input.Focus()
}

func (f *pathField) blur() { f.input.Blur() }

func (f *pathField) setValue(v string) {
	f.input.SetValue(v)
	f.input.CursorEnd()
	f.refresh()
}

func (f pathField) value() string { return strings.TrimSpace(f.input.Value()) }

// refresh recomputes the completions when the typed text changed.
func (f *pathField) refresh() {
	typed := f.input.Value()
	if typed == f.last && f.input.AvailableSuggestions() != nil {
		return
	}
	f.last = typed
	f.input.SetSuggestions(pathSuggestions(typed, f.names))
}

func (f pathField) update(msg tea.Msg) (pathField, tea.Cmd) {
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	f.refresh()
	return f, cmd
}

// view renders the input line, then the matches on one line below, indented by pad.
func (f pathField) view(pad string) string {
	out := f.input.View()
	matches := f.input.MatchedSuggestions()
	if f.input.Value() == "" && len(f.names) > 0 {
		matches = f.names
	}
	if len(matches) == 0 {
		return out
	}
	current := f.input.CurrentSuggestionIndex()
	var shown []string
	for i, m := range matches {
		if i == pathMatchesShown {
			shown = append(shown, "…")
			break
		}
		label := shortMatch(m, f.input.Value())
		if i == current && f.input.Value() != "" {
			label = cursorSt.Render(label)
		}
		shown = append(shown, label)
	}
	return out + "\n" + pad + dim.Render(strings.Join(shown, "  "))
}

// shortMatch shows a match by its last segment, so the line stays readable.
func shortMatch(match, typed string) string {
	trimmed := strings.TrimSuffix(match, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 && strings.Contains(typed, "/") {
		return match[i+1:]
	}
	return match
}

func pathHint() string { return "Tab complete · ↑↓ choose" }

// pathSuggestions lists what typed could continue as: entries of the directory it names
// so far, directories first, hidden ones only when asked for; and names when the text
// has no path separator yet.
func pathSuggestions(typed string, names []string) []string {
	var out []string
	if !strings.ContainsAny(typed, "/~") {
		for _, name := range names {
			if strings.HasPrefix(strings.ToLower(name), strings.ToLower(typed)) {
				out = append(out, name)
			}
		}
	}
	dir, prefix := "", typed
	if i := strings.LastIndex(typed, "/"); i >= 0 {
		dir, prefix = typed[:i+1], typed[i+1:]
	}
	lookup := home.Expand(dir)
	if lookup == "" {
		lookup = "."
	}
	if dir == "~" || (typed == "~" && dir == "") {
		return append(out, "~/")
	}
	entries, err := os.ReadDir(lookup)
	if err != nil {
		return out
	}
	var dirs, files []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			continue
		}
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(lookup, name)); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			dirs = append(dirs, dir+name+"/")
		} else {
			files = append(files, dir+name)
		}
	}
	sort.Strings(dirs)
	sort.Strings(files)
	return append(out, append(dirs, files...)...)
}
