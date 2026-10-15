package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Bucket groups tasks by urgency.
type Bucket struct {
	Name  string
	Tasks []Task
}

const (
	bOverdue  = "Overdue"
	bToday    = "Due today"
	bTomorrow = "Due tomorrow"
	bWeek     = "Due this week"
	bLater    = "Later"
	bNone     = "No due date"
	bBlocked  = "Blocked"
)

// Blocked sits last: it is a parking lot, not a deadline.
var bucketOrder = []string{bOverdue, bToday, bTomorrow, bWeek, bLater, bNone, bBlocked}

// isBlocked reports whether a task is waiting on something else. The status
// comes from a Projects V2 Status field or from a "blocked" label.
func isBlocked(t Task) bool {
	return strings.Contains(strings.ToLower(t.Status), "block")
}

func bucketFor(t Task, today time.Time) string {
	if isBlocked(t) {
		return bBlocked
	}
	if t.Due == nil {
		return bNone
	}
	days := daysUntil(*t.Due, today)
	switch {
	case days < 0:
		return bOverdue
	case days == 0:
		return bToday
	case days == 1:
		return bTomorrow
	case days <= 7:
		return bWeek
	default:
		return bLater
	}
}

func daysUntil(due, today time.Time) int {
	d := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.Local)
	return int(d.Sub(today).Hours() / 24)
}

func bucketize(tasks []Task, today time.Time) []Bucket {
	byName := map[string][]Task{}
	for _, t := range tasks {
		b := bucketFor(t, today)
		byName[b] = append(byName[b], t)
	}
	var out []Bucket
	for _, name := range bucketOrder {
		ts := byName[name]
		if len(ts) == 0 {
			continue
		}
		sortTasks(ts, "due")
		out = append(out, Bucket{Name: name, Tasks: ts})
	}
	return out
}

func sortTasks(ts []Task, by string) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		switch by {
		case "updated":
			return a.UpdatedAt.After(b.UpdatedAt)
		case "repo":
			if a.Repo != b.Repo {
				return a.Repo < b.Repo
			}
			return a.Number < b.Number
		default: // due
			switch {
			case a.Due != nil && b.Due == nil:
				return true
			case a.Due == nil && b.Due != nil:
				return false
			case a.Due != nil && b.Due != nil && !a.Due.Equal(*b.Due):
				return a.Due.Before(*b.Due)
			}
			if a.Repo != b.Repo {
				return a.Repo < b.Repo
			}
			return a.Number < b.Number
		}
	})
}

func relDays(d int) string {
	switch {
	case d == 0:
		return "today"
	case d == 1:
		return "tomorrow"
	case d == -1:
		return "yesterday"
	case d < 0:
		return fmt.Sprintf("%d days ago", -d)
	default:
		return fmt.Sprintf("in %d days", d)
	}
}

// ---- markdown output ----

func renderMarkdown(w io.Writer, r Report) {
	fmt.Fprintf(w, "# GitHub to-do — %s\n\n_%s_\n", r.User, r.Today.Format("Monday, 2006-01-02"))

	for _, b := range bucketize(r.Open, r.Today) {
		fmt.Fprintf(w, "\n## %s (%d)\n\n", b.Name, len(b.Tasks))
		for _, t := range b.Tasks {
			fmt.Fprintf(w, "- [ ] [%s#%d](%s) — %s\n", t.Repo, t.Number, t.URL, t.Title)
			fmt.Fprintf(w, "  - %s\n", strings.Join(mdMeta(t, r.Today), " · "))
		}
	}

	if r.ShowDone {
		fmt.Fprintf(w, "\n## Recently completed (last %d days) (%d)\n\n", r.DoneDays, len(r.Done))
		done := append([]Task(nil), r.Done...)
		sort.SliceStable(done, func(i, j int) bool {
			return ptrTime(done[i].ClosedAt).After(ptrTime(done[j].ClosedAt))
		})
		for _, t := range done {
			closed := ""
			if t.ClosedAt != nil {
				closed = " — closed " + t.ClosedAt.Local().Format("2006-01-02")
			}
			fmt.Fprintf(w, "- [x] [%s#%d](%s) — %s%s\n", t.Repo, t.Number, t.URL, t.Title, closed)
		}
	}
}

func mdMeta(t Task, today time.Time) []string {
	m := []string{"**" + t.Status + "**"}
	if t.Due != nil {
		m = append(m, fmt.Sprintf("due %s (%s)", t.Due.Format("2006-01-02"), relDays(daysUntil(*t.Due, today))))
	}
	if t.Milestone != "" {
		m = append(m, "milestone: "+t.Milestone)
	}
	if t.Project != "" {
		m = append(m, "project: "+t.Project)
	}
	if len(t.Labels) > 0 {
		m = append(m, "labels: "+strings.Join(t.Labels, ", "))
	}
	return m
}

// ---- json output ----

func renderJSON(w io.Writer, r Report) error {
	type jsonBucket struct {
		Name  string `json:"name"`
		Tasks []Task `json:"tasks"`
	}
	payload := struct {
		User      string       `json:"user"`
		Generated string       `json:"generated"`
		Buckets   []jsonBucket `json:"buckets"`
		Completed []Task       `json:"completed,omitempty"`
	}{User: r.User, Generated: r.Today.Format("2006-01-02")}

	for _, b := range bucketize(r.Open, r.Today) {
		payload.Buckets = append(payload.Buckets, jsonBucket{Name: b.Name, Tasks: b.Tasks})
	}
	if r.ShowDone {
		payload.Completed = r.Done
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func countWithDue(ts []Task) int {
	n := 0
	for _, t := range ts {
		if t.Due != nil {
			n++
		}
	}
	return n
}

func countOverdue(ts []Task, today time.Time) int {
	n := 0
	for _, t := range ts {
		if !isBlocked(t) && t.Due != nil && daysUntil(*t.Due, today) < 0 {
			n++
		}
	}
	return n
}

func countBlocked(ts []Task) int {
	n := 0
	for _, t := range ts {
		if isBlocked(t) {
			n++
		}
	}
	return n
}

// commonOwner returns the owner shared by every task, or "" if they differ.
func commonOwner(ts []Task) string {
	owner := ""
	for _, t := range ts {
		o, _, ok := strings.Cut(t.Repo, "/")
		if !ok {
			return ""
		}
		if owner == "" {
			owner = o
		} else if owner != o {
			return ""
		}
	}
	return owner
}

func shortRepo(repo, owner string) string {
	if owner != "" {
		return strings.TrimPrefix(repo, owner+"/")
	}
	return repo
}

func ptrTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
