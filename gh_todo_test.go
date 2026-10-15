package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

func TestParseDueMarker(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Ship the thing due: 2026-10-20", "2026-10-20", true},
		{"deadline = 2026-01-02 for this", "2026-01-02", true},
		{"Due date: 2026-12-31", "2026-12-31", true},
		{"📅 2026-03-04", "2026-03-04", true},
		{"no date here", "", false},
		{"due: not-a-date", "", false},
	}
	for _, c := range cases {
		got, ok := parseDueMarker(c.in)
		if ok != c.ok {
			t.Errorf("parseDueMarker(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && got.Format("2006-01-02") != c.want {
			t.Errorf("parseDueMarker(%q) = %s, want %s", c.in, got.Format("2006-01-02"), c.want)
		}
	}
}

func TestBucketFor(t *testing.T) {
	today := date(2026, time.October, 14)
	mk := func(d *time.Time) Task { return Task{Due: d} }
	due := func(y int, m time.Month, d int) *time.Time { v := date(y, m, d); return &v }

	cases := []struct {
		task Task
		want string
	}{
		{mk(nil), bNone},
		{mk(due(2026, time.October, 13)), bOverdue},
		{mk(due(2026, time.October, 14)), bToday},
		{mk(due(2026, time.October, 15)), bTomorrow},
		{mk(due(2026, time.October, 21)), bWeek},
		{mk(due(2026, time.October, 22)), bLater},
	}
	for _, c := range cases {
		if got := bucketFor(c.task, today); got != c.want {
			t.Errorf("bucketFor(due=%v) = %s, want %s", c.task.Due, got, c.want)
		}
	}
}

func TestNormalizePrefersProjectDateOverMilestone(t *testing.T) {
	var n node
	n.Typename = "Issue"
	n.Number = 7
	n.Title = "thing"
	n.Repository.NameWithOwner = "acme/repo"
	n.Milestone = &struct {
		Title string `json:"title"`
		DueOn string `json:"dueOn"`
	}{Title: "M1", DueOn: "2026-11-01T00:00:00Z"}

	item := struct {
		Project struct {
			Title string `json:"title"`
		} `json:"project"`
		FieldValues struct {
			Nodes []struct {
				Typename string `json:"__typename"`
				Date     string `json:"date"`
				Name     string `json:"name"`
				Text     string `json:"text"`
				Field    struct {
					Name string `json:"name"`
				} `json:"field"`
			} `json:"nodes"`
		} `json:"fieldValues"`
	}{}
	item.Project.Title = "Roadmap"

	dateField := struct {
		Typename string `json:"__typename"`
		Date     string `json:"date"`
		Name     string `json:"name"`
		Text     string `json:"text"`
		Field    struct {
			Name string `json:"name"`
		} `json:"field"`
	}{Typename: "ProjectV2ItemFieldDateValue", Date: "2026-10-20"}
	dateField.Field.Name = "Due date"

	statusField := dateField
	statusField.Typename = "ProjectV2ItemFieldSingleSelectValue"
	statusField.Date = ""
	statusField.Name = "In progress"
	statusField.Field.Name = "Status"

	item.FieldValues.Nodes = append(item.FieldValues.Nodes, dateField, statusField)
	n.ProjectItems.Nodes = append(n.ProjectItems.Nodes, item)

	got := normalize(n)
	if got.Due == nil || got.Due.Format("2006-01-02") != "2026-10-20" {
		t.Fatalf("due = %v, want the project date field 2026-10-20", got.Due)
	}
	if got.Status != "In progress" {
		t.Errorf("status = %q, want %q", got.Status, "In progress")
	}
	if got.Milestone != "M1" {
		t.Errorf("milestone = %q, want M1", got.Milestone)
	}
}

func TestStartFieldIsNotADueDate(t *testing.T) {
	if dueFieldRe.MatchString("Start date") && !startFieldRe.MatchString("Start date") {
		t.Error("Start date must not be treated as a due date")
	}
	if !dueFieldRe.MatchString("Target date") {
		t.Error("Target date must be treated as a due date")
	}
}

func TestCommonOwner(t *testing.T) {
	same := []Task{{Repo: "acme/a"}, {Repo: "acme/b"}}
	if got := commonOwner(same); got != "acme" {
		t.Errorf("commonOwner = %q, want acme", got)
	}
	mixed := []Task{{Repo: "acme/a"}, {Repo: "other/b"}}
	if got := commonOwner(mixed); got != "" {
		t.Errorf("commonOwner = %q, want empty", got)
	}
}

func TestDedupeKeepsFirst(t *testing.T) {
	ts := []Task{
		{Repo: "a/b", Number: 1, Status: "first", UpdatedAt: date(2026, time.October, 2)},
		{Repo: "a/b", Number: 1, Status: "second", UpdatedAt: date(2026, time.October, 1)},
		{Repo: "a/b", Number: 2, UpdatedAt: date(2026, time.October, 3)},
	}
	got := dedupe(ts)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestRefLabelKeepsTheIssueNumber(t *testing.T) {
	task := Task{Repo: "acme/very-long-repository-name-for-testing", Number: 1430}
	got := refLabel(task, "acme", 26)
	if !strings.HasSuffix(got, "#1430") {
		t.Errorf("refLabel = %q, want it to end with #1430", got)
	}
	if len([]rune(got)) > 26 {
		t.Errorf("refLabel = %q (%d runes), want at most 26", got, len([]rune(got)))
	}
}

func TestRefLabelLeavesShortNamesAlone(t *testing.T) {
	task := Task{Repo: "acme/tools", Number: 7}
	if got := refLabel(task, "acme", 26); got != "tools#7" {
		t.Errorf("refLabel = %q, want tools#7", got)
	}
}

func TestPadAndTruncate(t *testing.T) {
	if got := padTo("ab", 5); got != "ab   " {
		t.Errorf("padTo = %q", got)
	}
	if got := truncateTo("abcdefgh", 5); len([]rune(got)) != 5 {
		t.Errorf("truncateTo = %q, want 5 runes", got)
	}
}

func TestBlockedTakesPrecedenceOverDueDate(t *testing.T) {
	today := date(2026, time.October, 14)
	overdue := date(2026, time.August, 25)

	blocked := Task{Status: "Blocked", Due: &overdue}
	if got := bucketFor(blocked, today); got != bBlocked {
		t.Errorf("bucketFor(blocked, overdue) = %s, want %s", got, bBlocked)
	}

	open := Task{Status: "In progress", Due: &overdue}
	if got := bucketFor(open, today); got != bOverdue {
		t.Errorf("bucketFor(open, overdue) = %s, want %s", got, bOverdue)
	}
}

func TestBlockedIsLastInTheOrder(t *testing.T) {
	if bucketOrder[len(bucketOrder)-1] != bBlocked {
		t.Errorf("last bucket = %s, want %s", bucketOrder[len(bucketOrder)-1], bBlocked)
	}
}

func TestCountsExcludeBlocked(t *testing.T) {
	today := date(2026, time.October, 14)
	past := date(2026, time.October, 1)
	ts := []Task{
		{Status: "Blocked", Due: &past},
		{Status: "Todo", Due: &past},
		{Status: "blocked by #12", Due: &today},
	}
	if got := countOverdue(ts, today); got != 1 {
		t.Errorf("countOverdue = %d, want 1", got)
	}
	if got := countBlocked(ts); got != 2 {
		t.Errorf("countBlocked = %d, want 2", got)
	}
	if got := countDueIn(ts, today, 0); got != 0 {
		t.Errorf("countDueIn(today) = %d, want 0", got)
	}
}

func TestLinkWrapsOnlyWhenEnabled(t *testing.T) {
	off := Theme{Links: false}
	if got := off.Link("https://example.com", "text"); got != "text" {
		t.Errorf("Link with links off = %q, want %q", got, "text")
	}

	on := Theme{Links: true}
	got := on.Link("https://example.com", "text")
	want := "\x1b]8;;https://example.com\x1b\\text\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("Link = %q, want %q", got, want)
	}
	if on.Link("", "text") != "text" {
		t.Error("an empty URL must not produce a link")
	}
}

func TestLinkPaddedKeepsPaddingOutsideTheLink(t *testing.T) {
	th := Theme{Links: true}
	got := th.LinkPadded("https://example.com", "ab", lipgloss.NewStyle(), 5)
	if !strings.HasSuffix(got, "   ") {
		t.Errorf("LinkPadded = %q, want it to end with three spaces", got)
	}
	if ansiWidth(got) != 5 {
		t.Errorf("LinkPadded renders %d columns, want 5", ansiWidth(got))
	}
}

// ansiWidth measures display columns, ignoring escape sequences.
func ansiWidth(s string) int { return lipgloss.Width(s) }
