package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// watchLoop runs the live view until you quit.
func watchLoop(fetch func() (Report, error), login string, o runOpts) error {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := watchModel{
		fetch:    fetch,
		login:    login,
		opts:     o,
		theme:    NewTheme(o.noColor, o.noLinks),
		spinner:  sp,
		loading:  true,
		now:      time.Now(),
		interval: o.interval,
	}
	m.spinner.Style = lipgloss.NewStyle().Foreground(m.theme.Accent)

	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

type watchModel struct {
	fetch func() (Report, error)
	login string
	opts  runOpts
	theme Theme

	report  Report
	hasData bool
	err     error
	loading bool

	last     time.Time
	next     time.Time
	now      time.Time
	interval time.Duration

	spinner  spinner.Model
	viewport viewport.Model
	ready    bool
}

type reportMsg struct {
	report Report
	err    error
}

type tickMsg time.Time

func (m watchModel) fetchCmd() tea.Cmd {
	fetch := m.fetch
	return func() tea.Msg {
		report, err := fetch()
		return reportMsg{report: report, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m watchModel) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), tick(), m.spinner.Tick)
}

func (m watchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r":
			if !m.loading {
				m.loading = true
				return m, tea.Batch(m.fetchCmd(), m.spinner.Tick)
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.theme = m.theme.Resize(msg.Width)
		chromeHeight := lipgloss.Height(m.chrome()) + lipgloss.Height(m.statusBar())
		bodyHeight := max(msg.Height-chromeHeight, 3)

		if !m.ready {
			m.viewport = viewport.New(msg.Width, bodyHeight)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = bodyHeight
		}
		m.refreshContent()
		return m, nil

	case reportMsg:
		m.loading = false
		m.now = time.Now()
		m.last = m.now
		m.next = m.now.Add(m.interval)
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			m.report = msg.report
			m.hasData = true
		}
		m.refreshContent()
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		if !m.loading && !m.next.IsZero() && m.now.After(m.next) {
			m.loading = true
			return m, tea.Batch(tick(), m.fetchCmd(), m.spinner.Tick)
		}
		return m, tick()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading {
			return m, cmd
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// refreshContent re-renders the scrollable body, holding the scroll position.
func (m *watchModel) refreshContent() {
	if !m.ready {
		return
	}
	offset := m.viewport.YOffset
	m.viewport.SetContent(m.body())
	m.viewport.SetYOffset(offset)
}

func (m watchModel) body() string {
	if !m.hasData {
		return "\n" + taskIndent + m.theme.Dim.Render("Loading your tasks…")
	}
	if m.opts.compact {
		return bodyCompact(m.report, m.theme, m.opts.showURLs)
	}
	return bodyPretty(m.report, m.theme, m.opts.showURLs)
}

// chrome is the fixed block above the scrolling body.
func (m watchModel) chrome() string {
	report := m.report
	if !m.hasData {
		report = Report{User: m.login, Today: m.now}
	}
	owner := commonOwner(allTasks(report))
	return header(report, m.theme, owner) + "\n\n" + statRow(report, m.theme) + "\n"
}

func (m watchModel) statusBar() string {
	t := m.theme

	var left string
	switch {
	case m.err != nil:
		left = lipgloss.NewStyle().Foreground(t.Red).Render("✕ " + truncateTo(m.err.Error(), max(m.theme.Width/2, 20)))
	case m.loading:
		left = m.spinner.View() + t.Dim.Render(" refreshing…")
	default:
		left = t.Faded.Render(fmt.Sprintf("updated %s · next in %s",
			agoText(m.now.Sub(m.last)), durationText(m.next.Sub(m.now))))
	}

	keys := t.Faded.Render("↑↓ scroll") + t.Dim.Render(" · ") +
		t.Faded.Render("r refresh") + t.Dim.Render(" · ") +
		t.Faded.Render("q quit")

	scroll := ""
	if m.ready && m.viewport.TotalLineCount() > m.viewport.Height {
		scroll = t.Faded.Render(fmt.Sprintf(" · %3.0f%%", m.viewport.ScrollPercent()*100))
	}

	right := keys + scroll
	gap := t.Width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 2 {
		return " " + left
	}
	return " " + left + strings.Repeat(" ", gap) + right
}

func (m watchModel) View() string {
	if !m.ready {
		return "\n" + taskIndent + "Starting…"
	}
	return m.chrome() + m.viewport.View() + "\n" + m.statusBar()
}

func agoText(d time.Duration) string {
	if d < 2*time.Second {
		return "just now"
	}
	return durationText(d) + " ago"
}

func durationText(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
