package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/mattn/go-runewidth"
)

const (
	taskIndent = "  "
	metaIndent = "     "
	maxRefCol  = 38
)

// renderPretty draws the report with lipgloss.
func renderPretty(w io.Writer, r Report, t Theme, showURLs bool) {
	fmt.Fprintln(w, header(r, t, commonOwner(allTasks(r))))
	fmt.Fprintln(w)
	fmt.Fprintln(w, statRow(r, t))
	fmt.Fprint(w, bodyPretty(r, t, showURLs))
	fmt.Fprintln(w)
	fmt.Fprintln(w, footer(r, t))
}

// bodyPretty renders the task sections. The header, stat row, and footer are
// drawn separately so the watch view can pin them above and below a viewport.
func bodyPretty(r Report, t Theme, showURLs bool) string {
	all := allTasks(r)
	owner := commonOwner(all)
	refW := refColumn(all, owner, t.RefCap)

	var b strings.Builder
	if len(r.Open) == 0 {
		b.WriteString("\n" + taskIndent + lipgloss.NewStyle().Foreground(t.Green).Render("✓ Nothing assigned to you is open.") + "\n")
	}

	for _, bucket := range bucketize(r.Open, r.Today) {
		b.WriteString("\n" + sectionHeader(bucket.Name, len(bucket.Tasks), t.BucketColor(bucket.Name), t) + "\n")
		for _, task := range bucket.Tasks {
			b.WriteString(taskBlock(task, t, r.Today, owner, refW, showURLs, false))
		}
	}

	if r.ShowDone {
		label := fmt.Sprintf("Recently completed · last %d days", r.DoneDays)
		b.WriteString("\n" + sectionHeader(label, len(r.Done), t.Green, t) + "\n")
		if len(r.Done) == 0 {
			b.WriteString(metaIndent + t.Faded.Render("nothing closed in this window") + "\n")
		}
		for _, task := range sortedDone(r) {
			b.WriteString(taskBlock(task, t, r.Today, owner, refW, showURLs, true))
		}
	}
	return b.String()
}

func allTasks(r Report) []Task {
	return append(append([]Task{}, r.Open...), r.Done...)
}

func sortedDone(r Report) []Task {
	done := append([]Task(nil), r.Done...)
	sort.SliceStable(done, func(i, j int) bool {
		return ptrTime(done[i].ClosedAt).After(ptrTime(done[j].ClosedAt))
	})
	return done
}

func header(r Report, t Theme, owner string) string {
	title := t.Title.Render("GitHub to-do")
	who := t.Subtitle.Render(r.User)
	if owner != "" {
		who += t.Faded.Render("  ·  ") + t.Subtitle.Render(owner)
	}
	when := t.Faded.Render(r.Today.Format("Monday, 2 January 2006"))

	left := lipgloss.JoinVertical(lipgloss.Left, title, who)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Accent).
		Padding(0, 2).
		Width(t.Width - 4)

	inner := box.GetWidth() - box.GetHorizontalPadding()
	gap := inner - lipgloss.Width(left) - lipgloss.Width(when)
	if gap < 1 {
		return box.Render(lipgloss.JoinVertical(lipgloss.Left, left, when))
	}
	right := lipgloss.NewStyle().Height(lipgloss.Height(left)).AlignVertical(lipgloss.Bottom).Render(when)
	return box.Render(lipgloss.JoinHorizontal(lipgloss.Bottom, left, strings.Repeat(" ", gap), right))
}

func statRow(r Report, t Theme) string {
	stats := []struct {
		value int
		label string
		color lipgloss.AdaptiveColor
	}{
		{len(r.Open), "open", t.Accent},
		{countOverdue(r.Open, r.Today), "overdue", t.Red},
		{countDueIn(r.Open, r.Today, 0), "due today", t.Amber},
		{countDueWithin(r.Open, r.Today, 7), "next 7 days", t.Cyan},
	}
	if blocked := countBlocked(r.Open); blocked > 0 {
		stats = append(stats, struct {
			value int
			label string
			color lipgloss.AdaptiveColor
		}{blocked, "blocked", t.Muted})
	}
	if r.ShowDone {
		stats = append(stats, struct {
			value int
			label string
			color lipgloss.AdaptiveColor
		}{len(r.Done), "done", t.Green})
	}

	inner := 0
	for _, s := range stats {
		inner = max(inner, runewidth.StringWidth(s.label))
	}
	tile := t.Tile.Width(inner + t.Tile.GetHorizontalPadding())

	tiles := make([]string, 0, len(stats))
	for _, s := range stats {
		value := lipgloss.NewStyle().Bold(true).Foreground(s.color).Render(fmt.Sprintf("%d", s.value))
		label := t.Faded.Render(strings.ToUpper(s.label))
		tiles = append(tiles, tile.Render(lipgloss.JoinVertical(lipgloss.Center, value, label)))
	}

	return tileRows(tiles, t.Width-2)
}

// tileRows lays tiles out left to right, wrapping to a new row when they no
// longer fit the terminal.
func tileRows(tiles []string, width int) string {
	if len(tiles) == 0 {
		return ""
	}
	perRow := max(1, (width+1)/(lipgloss.Width(tiles[0])+1))

	var rows []string
	for start := 0; start < len(tiles); start += perRow {
		chunk := tiles[start:min(start+perRow, len(tiles))]
		cells := make([]string, 0, len(chunk)*2)
		for i, tile := range chunk {
			if i > 0 {
				cells = append(cells, " ")
			}
			cells = append(cells, tile)
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}

	// Indent by hand: styling the block as a whole would pad every line out to
	// the width of the longest row.
	lines := strings.Split(strings.Join(rows, "\n"), "\n")
	for i, line := range lines {
		lines[i] = " " + strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

func sectionHeader(name string, count int, color lipgloss.AdaptiveColor, t Theme) string {
	bar := lipgloss.NewStyle().Foreground(color).Render("▌")
	label := lipgloss.NewStyle().Bold(true).Foreground(color).Render(strings.ToUpper(name))
	badge := lipgloss.NewStyle().
		Foreground(t.OnColor).Background(color).Bold(true).Padding(0, 1).
		Render(fmt.Sprintf("%d", count))

	line := fmt.Sprintf("%s %s %s ", bar, label, badge)
	rule := t.Width - lipgloss.Width(line) - 2
	if rule > 0 {
		line += t.Rule.Render(strings.Repeat("─", rule))
	}
	return line
}

func taskBlock(task Task, t Theme, today time.Time, owner string, refW int, showURLs, done bool) string {
	var b strings.Builder

	ref := refLabel(task, owner, t.RefCap)

	titleW := t.Width - len(taskIndent) - 2 - refW - 2
	title := truncateTo(task.Title, max(titleW, 20))

	b.WriteString(taskIndent)
	b.WriteString(marker(task, t, today, done))
	b.WriteString(" ")
	b.WriteString(t.LinkPadded(task.URL, ref, t.Ref, refW))
	b.WriteString("  ")
	b.WriteString(t.TaskName.Render(title))
	b.WriteString("\n")

	b.WriteString(metaIndent)
	b.WriteString(metaLine(task, t, today, done))
	b.WriteString("\n")

	if showURLs {
		b.WriteString(metaIndent)
		b.WriteString(t.Faded.Render(task.URL))
		b.WriteString("\n")
	}
	return b.String()
}

func marker(task Task, t Theme, today time.Time, done bool) string {
	style := lipgloss.NewStyle()
	switch {
	case done:
		return style.Foreground(t.Green).Render("✓")
	case task.Due != nil && daysUntil(*task.Due, today) < 0:
		return style.Foreground(t.Red).Render("●")
	case task.Due != nil && daysUntil(*task.Due, today) == 0:
		return style.Foreground(t.Amber).Render("◉")
	case task.Type == "pr":
		return style.Foreground(t.Cyan).Render("◆")
	default:
		return style.Foreground(t.Faint).Render("○")
	}
}

func metaLine(task Task, t Theme, today time.Time, done bool) string {
	parts := []string{t.Pill(task.Status, t.StatusColor(task.Status))}

	if task.Due != nil {
		d := daysUntil(*task.Due, today)
		text := fmt.Sprintf("◷ %s · %s", task.Due.Format("Jan 2"), relDays(d))
		switch {
		case done:
			parts = append(parts, t.Tag(text, t.Faint, false))
		case d < 0:
			parts = append(parts, t.Tag(text, t.Red, true))
		case d == 0:
			parts = append(parts, t.Tag(text, t.Amber, true))
		case d <= 7:
			parts = append(parts, t.Tag(text, t.Cyan, false))
		default:
			parts = append(parts, t.Tag(text, t.Muted, false))
		}
	}
	if done && task.ClosedAt != nil {
		parts = append(parts, t.Tag("✓ closed "+relDays(daysUntil(*task.ClosedAt, today)), t.Green, false))
	}

	line := strings.Join(parts, "  ")

	var trail []string
	if task.Milestone != "" {
		trail = append(trail, "◇ "+task.Milestone)
	}
	if task.Project != "" {
		trail = append(trail, "▸ "+task.Project)
	}
	if len(task.Labels) > 0 {
		trail = append(trail, strings.Join(task.Labels, ", "))
	}
	if len(trail) == 0 {
		return line
	}

	room := t.Width - len(metaIndent) - lipgloss.Width(line) - 3
	if room < 12 {
		return line
	}
	return line + "  " + t.Faded.Render(truncateTo(strings.Join(trail, "  "), room))
}

func footer(r Report, t Theme) string {
	overdue := countOverdue(r.Open, r.Today)
	summary := fmt.Sprintf("%d open · %d dated · %d overdue", len(r.Open), countWithDue(r.Open), overdue)
	if blocked := countBlocked(r.Open); blocked > 0 {
		summary += fmt.Sprintf(" · %d blocked", blocked)
	}

	hint := "next up: "
	switch {
	case overdue > 0:
		hint += fmt.Sprintf("%d overdue item(s)", overdue)
	case countDueIn(r.Open, r.Today, 0) > 0:
		hint += "work due today"
	case len(r.Open) > 0:
		hint += "nothing is late"
	default:
		hint = "inbox zero"
	}

	left := t.Faded.Render(summary)
	right := t.Dim.Render(hint)
	gap := t.Width - lipgloss.Width(left) - lipgloss.Width(right) - 3
	if gap < 2 {
		return " " + left
	}
	return " " + left + strings.Repeat(" ", gap) + right
}

// refLabel renders "repo#123", shortening the repository name to fit while
// always keeping the issue number intact.
func refLabel(t Task, owner string, cap int) string {
	name := shortRepo(t.Repo, owner)
	suffix := fmt.Sprintf("#%d", t.Number)
	avail := cap - runewidth.StringWidth(suffix)

	if runewidth.StringWidth(name) > avail {
		// The last dot-separated segment carries the distinctive part of the name.
		if i := strings.LastIndex(name, "."); i >= 0 && runewidth.StringWidth(name[i+1:])+1 <= avail {
			name = "…" + name[i+1:]
		} else {
			name = runewidth.Truncate(name, avail, "…")
		}
	}
	return name + suffix
}

func refColumn(ts []Task, owner string, cap int) int {
	w := 12
	for _, t := range ts {
		w = max(w, runewidth.StringWidth(refLabel(t, owner, cap)))
	}
	return min(w, cap)
}

func countDueIn(ts []Task, today time.Time, offset int) int {
	n := 0
	for _, t := range ts {
		if !isBlocked(t) && t.Due != nil && daysUntil(*t.Due, today) == offset {
			n++
		}
	}
	return n
}

func countDueWithin(ts []Task, today time.Time, days int) int {
	n := 0
	for _, t := range ts {
		if t.Due == nil || isBlocked(t) {
			continue
		}
		if d := daysUntil(*t.Due, today); d >= 0 && d <= days {
			n++
		}
	}
	return n
}

// ---- compact table mode ----

// renderCompact draws one row per task using lipgloss/table.
func renderCompact(w io.Writer, r Report, t Theme, showURLs bool) {
	fmt.Fprintln(w, header(r, t, commonOwner(allTasks(r))))
	fmt.Fprintln(w)
	fmt.Fprintln(w, statRow(r, t))
	fmt.Fprint(w, bodyCompact(r, t, showURLs))
	fmt.Fprintln(w)
	fmt.Fprintln(w, footer(r, t))
}

func bodyCompact(r Report, t Theme, _ bool) string {
	all := allTasks(r)
	owner := commonOwner(all)
	refW := refColumn(all, owner, t.RefCap)
	statusW := widestBy(all, func(task Task) string { return task.Status })
	dueW := widestBy(all, func(task Task) string { return dueText(task, r.Today) })

	var b strings.Builder
	for _, bucket := range bucketize(r.Open, r.Today) {
		b.WriteString("\n" + sectionHeader(bucket.Name, len(bucket.Tasks), t.BucketColor(bucket.Name), t) + "\n")
		b.WriteString(compactTable(bucket.Tasks, t, r.Today, owner, refW, statusW, dueW, false) + "\n")
	}

	if r.ShowDone {
		label := fmt.Sprintf("Recently completed · last %d days", r.DoneDays)
		b.WriteString("\n" + sectionHeader(label, len(r.Done), t.Green, t) + "\n")
		if len(r.Done) == 0 {
			b.WriteString(metaIndent + t.Faded.Render("nothing closed in this window") + "\n")
		} else {
			b.WriteString(compactTable(sortedDone(r), t, r.Today, owner, refW, statusW, dueW, true) + "\n")
		}
	}
	return b.String()
}

func compactTable(tasks []Task, t Theme, today time.Time, owner string, refW, statusW, dueW int, done bool) string {
	// Every cell carries one space of padding on each side. Cells are padded to a
	// fixed width so the columns line up across every section of the report.
	titleW := t.Width - 4 - (1 + 2) - (refW + 2) - (statusW + 2) - (dueW + 2)
	titleW = max(titleW, 24)

	rows := make([][]string, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, []string{
			marker(task, t, today, done),
			t.LinkPadded(task.URL, refLabel(task, owner, t.RefCap), t.Ref, refW),
			padTo(truncateTo(task.Title, titleW), titleW),
			lipgloss.NewStyle().Foreground(t.StatusColor(task.Status)).Render(padTo(task.Status, statusW)),
			dueCell(task, t, today, done, dueW),
		})
	}

	return table.New().
		Border(lipgloss.NormalBorder()).
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderRow(false).BorderHeader(true).
		BorderStyle(lipgloss.NewStyle().Foreground(t.Faint)).
		Headers("", "TASK", "TITLE", "STATUS", "DUE").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return style.Foreground(t.Faint).Bold(true)
			}
			return style
		}).
		Render()
}

func dueText(task Task, today time.Time) string {
	if task.Due == nil {
		return ""
	}
	return fmt.Sprintf("%s · %s", task.Due.Format("Jan 2"), relDaysShort(daysUntil(*task.Due, today)))
}

func dueCell(task Task, t Theme, today time.Time, done bool, width int) string {
	text := padTo(dueText(task, today), width)
	if task.Due == nil {
		return t.Faded.Render(padTo("—", width))
	}
	d := daysUntil(*task.Due, today)
	switch {
	case done:
		return t.Faded.Render(text)
	case d < 0:
		return lipgloss.NewStyle().Foreground(t.Red).Bold(true).Render(text)
	case d == 0:
		return lipgloss.NewStyle().Foreground(t.Amber).Bold(true).Render(text)
	case d <= 7:
		return lipgloss.NewStyle().Foreground(t.Cyan).Render(text)
	default:
		return t.Dim.Render(text)
	}
}

func relDaysShort(d int) string {
	switch {
	case d == 0:
		return "today"
	case d == 1:
		return "tomorrow"
	case d < 0:
		return fmt.Sprintf("%dd late", -d)
	default:
		return fmt.Sprintf("in %dd", d)
	}
}

func widestBy(ts []Task, f func(Task) string) int {
	w := 0
	for _, t := range ts {
		w = max(w, runewidth.StringWidth(f(t)))
	}
	return w
}
