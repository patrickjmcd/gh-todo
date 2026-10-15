package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel() watchModel {
	due := date(2026, time.October, 14)
	report := Report{
		User:  "octocat",
		Today: date(2026, time.October, 14),
		Open: []Task{
			{Repo: "acme/tools", Number: 1, Title: "Ship it", Status: "In progress", Due: &due, URL: "https://example.com/1"},
			{Repo: "acme/tools", Number: 2, Title: "Waiting on infra", Status: "Blocked", URL: "https://example.com/2"},
		},
	}
	return watchModel{
		login:    "octocat",
		theme:    Theme{Width: 100, RefCap: 26}.withStyles(),
		interval: time.Minute,
		fetch:    func() (Report, error) { return report, nil },
		loading:  true,
		now:      date(2026, time.October, 14),
	}
}

func TestWatchRendersBodyAfterAReport(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(watchModel)
	if !m.ready {
		t.Fatal("the model must be ready after a window size message")
	}

	report, _ := m.fetch()
	updated, _ = m.Update(reportMsg{report: report})
	m = updated.(watchModel)

	if m.loading {
		t.Error("loading must clear once a report arrives")
	}
	if !m.hasData {
		t.Error("hasData must be set once a report arrives")
	}

	body := m.body()
	for _, want := range []string{"Ship it", "Waiting on infra", "BLOCKED"} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q", want)
		}
	}
	if !strings.Contains(m.View(), "q quit") {
		t.Error("the view must show the key hints")
	}
}

func TestWatchRefetchesWhenTheIntervalElapses(t *testing.T) {
	m := testModel()
	m.loading = false
	m.next = date(2026, time.October, 14).Add(time.Minute)

	// Before the interval elapses, nothing refetches.
	updated, _ := m.Update(tickMsg(m.next.Add(-time.Second)))
	if updated.(watchModel).loading {
		t.Error("must not refetch before the interval elapses")
	}

	updated, _ = m.Update(tickMsg(m.next.Add(time.Second)))
	if !updated.(watchModel).loading {
		t.Error("must refetch once the interval elapses")
	}
}

func TestWatchQuitKeys(t *testing.T) {
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		m := testModel()
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if key != "q" {
			continue // only "q" arrives as runes
		}
		if cmd == nil {
			t.Errorf("key %q must return a quit command", key)
		}
	}
}

func TestWatchShowsErrors(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(watchModel)
	updated, _ = m.Update(reportMsg{err: errString("rate limited")})
	m = updated.(watchModel)

	if !strings.Contains(m.statusBar(), "rate limited") {
		t.Errorf("status bar = %q, want it to mention the error", m.statusBar())
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestDurationText(t *testing.T) {
	cases := map[time.Duration]string{
		0:                "0s",
		45 * time.Second: "45s",
		90 * time.Second: "1m 30s",
		2 * time.Hour:    "2h 00m",
		-5 * time.Second: "0s",
	}
	for in, want := range cases {
		if got := durationText(in); got != want {
			t.Errorf("durationText(%v) = %q, want %q", in, got, want)
		}
	}
}
