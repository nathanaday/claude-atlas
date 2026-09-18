package threads

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/nathanaday/claude-atlas/internal/project"
	"github.com/nathanaday/claude-atlas/internal/vault"
)

var (
	legacyID      = regexp.MustCompile(`^task-(\d{8}-[0-9a-f]{4})$`)
	legacySection = regexp.MustCompile(`(?m)^##\s+(Idea|Plan|Progress|Outcome)\b[^\n]*\n`)
	legacyH1      = regexp.MustCompile(`(?m)\A\s*#\s[^\n]*\n`)
)

// legacyPages lists the task pages 2.x left under tasks/ and tasks/archive/.
func legacyPages(p *project.Project) []string {
	var out []string
	for _, dir := range []string{project.LegacyTasksDir, project.LegacyTasksDir + "/archive"} {
		files, _ := pages(p, dir)
		for _, rel := range files {
			if rel != project.LegacyTasksDir+"/tasks.md" {
				out = append(out, rel)
			}
		}
	}
	return out
}

// Legacy reports whether the project still holds task pages from 2.x.
func Legacy(p *project.Project) bool { return len(legacyPages(p)) > 0 }

// Migrate turns every task page of 2.x into a thread: Idea becomes the stub, Plan and
// Progress the plan, Outcome the receipt. A page it moved is removed; a page it cannot
// read stays where it is and is returned. It removes tasks/ once the folder is empty.
func Migrate(p *project.Project, now time.Time) (moved int, left []Problem, err error) {
	if err := p.EnsureFolders(); err != nil {
		return 0, nil, err
	}
	board, err := Load(p)
	if err != nil {
		return 0, nil, err
	}
	for _, rel := range legacyPages(p) {
		data, err := os.ReadFile(p.Path(rel))
		if err != nil {
			return moved, left, err
		}
		if err := migrateOne(p, board, rel, string(data), now); err != nil {
			left = append(left, Problem{Path: rel, Reason: err.Error()})
			continue
		}
		if err := os.Remove(p.Path(rel)); err != nil {
			return moved, left, err
		}
		moved++
	}
	if len(left) == 0 {
		os.Remove(p.Path(project.LegacyTasksDir + "/tasks.md"))
		os.Remove(p.Path(project.LegacyTasksDir + "/archive"))
		os.Remove(p.Path(project.LegacyTasksDir))
	}
	_, err = Sync(p, now)
	return moved, left, err
}

func migrateOne(p *project.Project, board *Board, rel, content string, now time.Time) error {
	fields, body, err := vault.Frontmatter(content)
	if err != nil {
		return err
	}
	if fields == nil || vault.StringField(fields, "type") != "task" {
		return fmt.Errorf("not a task page")
	}
	today := now.Format("2006-01-02")
	date := func(key string) string {
		if v := dateField(fields, key); datePattern.MatchString(v) {
			return v
		}
		return today
	}
	t := Thread{
		Title: strings.TrimSpace(vault.StringField(fields, "title")), Priority: vault.StringField(fields, "priority"),
		Phase: strings.TrimSpace(vault.StringField(fields, "phase")), Created: date("created"), Updated: date("updated"),
	}
	if t.Title == "" {
		t.Title = vault.PageTitle(rel)
	}
	if !contains(Priorities, t.Priority) {
		t.Priority = "normal"
	}
	if t.Phase != "" && board.Phase(t.Phase) == nil {
		t.Phase = ""
	}
	if m := legacyID.FindStringSubmatch(vault.StringField(fields, "task_id")); m != nil && board.Find("thr-"+m[1]) == nil {
		t.ID = "thr-" + m[1]
	} else {
		t.ID = NewID(now)
	}
	status := vault.StringField(fields, "status")
	if status == "blocked" {
		t.Blocked = "blocked as a task; the plan's progress says on what"
	}

	sections := map[string]string{}
	body = legacyH1.ReplaceAllString(body, "")
	marks := legacySection.FindAllStringSubmatchIndex(body, -1)
	preamble := body
	if len(marks) > 0 {
		preamble = body[:marks[0][0]]
	}
	for i, m := range marks {
		end := len(body)
		if i+1 < len(marks) {
			end = marks[i+1][0]
		}
		sections[body[m[2]:m[3]]] = strings.TrimSpace(body[m[1]:end])
	}
	stub := strings.TrimSpace(strings.TrimSpace(preamble) + "\n\n" + sections["Idea"])
	if stub == "" {
		stub = t.Title
	}
	plan := sections["Plan"]
	if progress := sections["Progress"]; progress != "" {
		plan = strings.TrimSpace(plan + "\n\n## Progress\n\n" + progress)
	}
	outcome := ""
	switch status {
	case "done":
		outcome = Completed
	case "cancelled":
		outcome = Killed
	}

	stem := freeStem(p, t.Title)
	t.Path = project.ThreadsDir + "/" + stem + ".md"
	docs := []struct {
		d    Doc
		text string
	}{{Doc{Stage: Stub, Created: t.Created}, stub}}
	if plan != "" {
		docs = append(docs, struct {
			d    Doc
			text string
		}{Doc{Stage: Plan, Created: t.Updated}, plan})
	}
	if outcome != "" {
		text := sections["Outcome"]
		if text == "" {
			text = "Closed as a task before the move to threads; the page recorded no outcome."
		}
		docs = append(docs, struct {
			d    Doc
			text string
		}{Doc{Stage: Receipt, Created: t.Updated, Outcome: outcome}, text})
	}
	if err := os.WriteFile(p.Path(t.Path), []byte(cardFront(t)), 0o644); err != nil {
		return err
	}
	for _, item := range docs {
		item.d.Path = path.Join(stageDir[item.d.Stage], stem+".md")
		if err := os.WriteFile(p.Path(item.d.Path), []byte(docPage(t, item.d, item.text)), 0o644); err != nil {
			return err
		}
	}
	board.Threads = append(board.Threads, t)
	return nil
}
