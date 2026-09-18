// Package refresh derives state for every registry entry: the date and heat helpers this
// file keeps, and Derive, Registry, and Signals in derive.go.
package refresh

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/links"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

const (
	HotDays  = 7
	WarmDays = 30
)

var (
	logHeading = regexp.MustCompile(`(?m)^##\s+(\d{4}-\d{2}-\d{2})\b`)
	wikiLink   = regexp.MustCompile(`\[\[([^\]|]+)(?:\|([^\]]+))?\]\]`)
)

func ptr[T any](v T) *T { return &v }

// Heat maps a vault's age and idleness to new, hot, warm, or cold; unknown idleness gives "".
// A vault created within newDays is "new" whatever its activity, so a fresh, possibly
// empty vault is not mistaken for one with a long active history.
func Heat(daysIdle, daysOld *int, newDays int) string {
	switch {
	case daysIdle == nil:
		return ""
	case daysOld != nil && *daysOld < newDays:
		return "new"
	case *daysIdle < HotDays:
		return "hot"
	case *daysIdle < WarmDays:
		return "warm"
	default:
		return "cold"
	}
}

func parseDate(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	return t, err == nil
}

func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// NewestLogDate is the latest `## YYYY-MM-DD` heading in wiki/log.md.
func NewestLogDate(vault string) (time.Time, bool) {
	data, err := os.ReadFile(filepath.Join(vault, "wiki", "log.md"))
	if err != nil {
		return time.Time{}, false
	}
	var newest time.Time
	found := false
	for _, m := range logHeading.FindAllStringSubmatch(string(data), -1) {
		if t, ok := parseDate(m[1]); ok && (!found || t.After(newest)) {
			newest, found = t, true
		}
	}
	return newest, found
}

// CreatedDate is the day the vault was created: the identity file's date, or for a
// claude-obsidian vault the `created:` field its template wrote into wiki/index.md.
func CreatedDate(root string) (time.Time, bool) {
	if v, err := vault.Open(root); err == nil && v.Config.Created != "" {
		if t, ok := parseDate(v.Config.Created); ok {
			return t, true
		}
	}
	for _, name := range []string{"index.md", "overview.md", "log.md"} {
		data, err := os.ReadFile(filepath.Join(root, "wiki", name))
		if err != nil {
			continue
		}
		front, _, ok, err := vault.SplitFrontmatter(string(data))
		if err != nil || !ok {
			continue
		}
		if m := createdLine.FindStringSubmatch(front); m != nil {
			if t, ok := parseDate(m[1]); ok {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

var createdLine = regexp.MustCompile(`(?m)^created:\s*(\d{4}-\d{2}-\d{2})`)

// NewestWikiMtime is the latest modification date of any file under wiki/.
func NewestWikiMtime(vault string) (time.Time, bool) {
	var newest time.Time
	found := false
	filepath.WalkDir(filepath.Join(vault, "wiki"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !found || info.ModTime().After(newest) {
			newest, found = info.ModTime(), true
		}
		return nil
	})
	if !found {
		return time.Time{}, false
	}
	return dateOf(newest), true
}

// HotTopics lists the bullets under `## Active Threads` in wiki/hot.md. Prose, so best effort.
func HotTopics(vault string) []string {
	data, err := os.ReadFile(filepath.Join(vault, "wiki", "hot.md"))
	if err != nil {
		return nil
	}
	var threads []string
	inside := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			inside = strings.EqualFold(strings.TrimSpace(line[3:]), "active threads")
			continue
		}
		if !inside {
			continue
		}
		s := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* "):
			threads = append(threads, strings.TrimSpace(s[2:]))
		case s != "" && len(threads) > 0 && !strings.HasPrefix(s, "#"):
			threads[len(threads)-1] += " " + s
		}
	}
	return threads
}

// PlainText strips wikilinks; a link copied from another vault resolves to nothing in the atlas.
func PlainText(s string) string {
	return wikiLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := wikiLink.FindStringSubmatch(m)
		if sub[2] != "" {
			return sub[2]
		}
		return sub[1]
	})
}

// LinkSummary renders one repository link's derived facts on a line.
func LinkSummary(l links.Link) string {
	if !l.OK {
		return l.Error
	}
	var bits []string
	if l.Branch != "" {
		bits = append(bits, l.Branch)
	}
	if l.Dirty != nil {
		if *l.Dirty == 0 {
			bits = append(bits, "clean")
		} else {
			bits = append(bits, fmt.Sprintf("%d uncommitted", *l.Dirty))
		}
	}
	if l.LastCommit != "" {
		bits = append(bits, "last commit "+l.LastCommit)
	}
	if len(bits) == 0 {
		return "ok"
	}
	return strings.Join(bits, " · ")
}

// NowUTC is the timestamp format derived state records.
func NowUTC() string {
	return time.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
