package main

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Theme holds every style the pretty renderer draws with. Colors are adaptive,
// so the same binary reads well on light and dark terminals.
type Theme struct {
	Width  int
	RefCap int
	Links  bool

	Accent  lipgloss.AdaptiveColor
	Red     lipgloss.AdaptiveColor
	Amber   lipgloss.AdaptiveColor
	Cyan    lipgloss.AdaptiveColor
	Green   lipgloss.AdaptiveColor
	Muted   lipgloss.AdaptiveColor
	Faint   lipgloss.AdaptiveColor
	OnColor lipgloss.AdaptiveColor

	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Dim      lipgloss.Style
	Faded    lipgloss.Style
	TaskName lipgloss.Style
	Ref      lipgloss.Style
	Tile     lipgloss.Style
	Header   lipgloss.Style
	Rule     lipgloss.Style
}

// NewTheme builds the theme and sizes it to the terminal.
func NewTheme(noColor, noLinks bool) Theme {
	if noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	width := 100
	tty := term.IsTerminal(int(os.Stdout.Fd()))
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 40 {
		width = min(w, 160)
	}

	t := Theme{
		Width:   width,
		RefCap:  max(14, min(maxRefCol, width/3)),
		Links:   tty && !noLinks && os.Getenv("NO_HYPERLINKS") == "",
		Accent:  lipgloss.AdaptiveColor{Light: "#5A32D6", Dark: "#A78BFA"},
		Red:     lipgloss.AdaptiveColor{Light: "#C2185B", Dark: "#FF6E9C"},
		Amber:   lipgloss.AdaptiveColor{Light: "#A85B00", Dark: "#FFB454"},
		Cyan:    lipgloss.AdaptiveColor{Light: "#00697F", Dark: "#5FD7FF"},
		Green:   lipgloss.AdaptiveColor{Light: "#1F7A4D", Dark: "#5FD787"},
		Muted:   lipgloss.AdaptiveColor{Light: "#5F5F5F", Dark: "#9E9E9E"},
		Faint:   lipgloss.AdaptiveColor{Light: "#8C8C8C", Dark: "#6C6C6C"},
		OnColor: lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#101014"},
	}

	return t.withStyles()
}

// withStyles fills in the derived styles for an already-coloured theme.
func (t Theme) withStyles() Theme {
	t.Title = lipgloss.NewStyle().Bold(true).Foreground(t.Accent)
	t.Subtitle = lipgloss.NewStyle().Foreground(t.Muted)
	t.Dim = lipgloss.NewStyle().Foreground(t.Muted)
	t.Faded = lipgloss.NewStyle().Foreground(t.Faint)
	t.TaskName = lipgloss.NewStyle()
	t.Ref = lipgloss.NewStyle().Foreground(t.Faint)
	t.Tile = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Faint).
		Padding(0, 2).
		Align(lipgloss.Center)
	t.Header = lipgloss.NewStyle().Bold(true)
	t.Rule = lipgloss.NewStyle().Foreground(t.Faint)
	return t
}

// Pill renders a filled badge, for statuses.
func (t Theme) Pill(text string, bg lipgloss.AdaptiveColor) string {
	return lipgloss.NewStyle().
		Foreground(t.OnColor).
		Background(bg).
		Bold(true).
		Padding(0, 1).
		Render(text)
}

// Tag renders an outlined badge, for due dates.
func (t Theme) Tag(text string, fg lipgloss.AdaptiveColor, bold bool) string {
	return lipgloss.NewStyle().Foreground(fg).Bold(bold).Render(text)
}

// Chip renders a faint label chip.
func (t Theme) Chip(text string) string {
	return lipgloss.NewStyle().
		Foreground(t.Faint).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(t.Faint).
		PaddingLeft(1).
		Render(text)
}

// StatusColor maps a status label to a color, whatever the project calls it.
func (t Theme) StatusColor(status string) lipgloss.AdaptiveColor {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "block"):
		return t.Red
	case strings.Contains(s, "changes requested"):
		return t.Red
	case strings.Contains(s, "progress"), strings.Contains(s, "doing"), strings.Contains(s, "active"):
		return t.Amber
	case strings.Contains(s, "review"):
		return t.Accent
	case strings.Contains(s, "done"), strings.Contains(s, "merged"), strings.Contains(s, "approved"), strings.Contains(s, "closed"):
		return t.Green
	case strings.Contains(s, "ready"), strings.Contains(s, "next"):
		return t.Cyan
	default: // Backlog, Todo, Draft, Open
		return t.Faint
	}
}

// BucketColor maps an urgency group to a color.
func (t Theme) BucketColor(name string) lipgloss.AdaptiveColor {
	switch name {
	case bOverdue:
		return t.Red
	case bToday:
		return t.Amber
	case bTomorrow, bWeek:
		return t.Cyan
	case bBlocked, bNone:
		return t.Faint
	default:
		return t.Accent
	}
}

// Resize returns a copy of the theme laid out for a new terminal width.
func (t Theme) Resize(width int) Theme {
	if width > 40 {
		t.Width = min(width, 160)
		t.RefCap = max(14, min(maxRefCol, t.Width/3))
	}
	return t
}

// Link wraps text in an OSC 8 hyperlink. Terminals that do not support OSC 8
// ignore the sequence, and it occupies no columns, so the layout is unaffected.
func (t Theme) Link(url, text string) string {
	if !t.Links || url == "" {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// LinkPadded links text and then pads to width, so the trailing run of spaces
// stays outside the clickable region.
func (t Theme) LinkPadded(url, text string, style lipgloss.Style, width int) string {
	pad := max(0, width-runewidth.StringWidth(text))
	return t.Link(url, style.Render(text)) + strings.Repeat(" ", pad)
}

// truncateTo shortens a string to a display width, adding an ellipsis.
func truncateTo(s string, width int) string {
	if width < 4 {
		return ""
	}
	return runewidth.Truncate(s, width, "…")
}

// padTo right-pads a string to a display width.
func padTo(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
