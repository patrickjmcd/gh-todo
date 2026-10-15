package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// defaultHost is github.com. GitHub Enterprise Server installs put the API
// under https://HOST/api/graphql instead.
const defaultHost = "github.com"

// graphQLEndpoint derives the GraphQL URL for a host.
func graphQLEndpoint(host string) string {
	host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://"), "/")
	if host == "" || host == defaultHost {
		return "https://api." + defaultHost + "/graphql"
	}
	return "https://" + host + "/api/graphql"
}

// Task is a normalized GitHub issue or pull request.
type Task struct {
	Type       string     `json:"type"` // "issue" or "pr"
	Repo       string     `json:"repo"`
	Number     int        `json:"number"`
	Title      string     `json:"title"`
	URL        string     `json:"url"`
	State      string     `json:"state"`
	Status     string     `json:"status"`
	StatusSrc  string     `json:"status_source"`
	Due        *time.Time `json:"due,omitempty"`
	DueSrc     string     `json:"due_source,omitempty"`
	Milestone  string     `json:"milestone,omitempty"`
	Labels     []string   `json:"labels,omitempty"`
	Project    string     `json:"project,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
	CreatedAt  time.Time  `json:"created_at"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
	Merged     bool       `json:"merged,omitempty"`
	Draft      bool       `json:"draft,omitempty"`
	ReviewOnly bool       `json:"review_requested_only,omitempty"`
}

// Client talks to the GitHub GraphQL API.
type Client struct {
	token        string
	endpoint     string
	http         *http.Client
	withProjects bool
}

// NewClient resolves a token from GITHUB_TOKEN, GH_TOKEN, or the gh CLI, and
// points at the given host. An empty host means github.com.
func NewClient(host string) (*Client, error) {
	if host == "" {
		host = firstNonEmpty(os.Getenv("GH_HOST"), defaultHost)
	}

	token := firstNonEmpty(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"))
	if token == "" {
		out, err := exec.Command("gh", "auth", "token", "--hostname", host).Output()
		if err != nil {
			return nil, fmt.Errorf("no token for %s: set GITHUB_TOKEN or run `gh auth login --hostname %s`", host, host)
		}
		token = strings.TrimSpace(string(out))
	}
	if token == "" {
		return nil, fmt.Errorf("no token for %s: set GITHUB_TOKEN or run `gh auth login --hostname %s`", host, host)
	}
	return &Client{
		token:        token,
		endpoint:     graphQLEndpoint(host),
		http:         &http.Client{Timeout: 45 * time.Second},
		withProjects: true,
	}, nil
}

// Login returns the authenticated user's login.
func (c *Client) Login() (string, error) {
	var resp struct {
		Data struct {
			Viewer struct{ Login string } `json:"viewer"`
		} `json:"data"`
	}
	if err := c.do("query{viewer{login}}", nil, &resp); err != nil {
		return "", err
	}
	return resp.Data.Viewer.Login, nil
}

func (c *Client) do(query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequest("POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gh-todo")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github api returned %s: %s", resp.Status, truncate(string(raw), 300))
	}
	return json.Unmarshal(raw, out)
}

type searchResponse struct {
	Data struct {
		Search struct {
			IssueCount int `json:"issueCount"`
			PageInfo   struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
			Nodes []node `json:"nodes"`
		} `json:"search"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"errors"`
}

type node struct {
	Typename  string `json:"__typename"`
	Number    int    `json:"number"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	State     string `json:"state"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	ClosedAt  string `json:"closedAt"`
	MergedAt  string `json:"mergedAt"`
	IsDraft   bool   `json:"isDraft"`
	Review    string `json:"reviewDecision"`

	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Milestone *struct {
		Title string `json:"title"`
		DueOn string `json:"dueOn"`
	} `json:"milestone"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	ProjectItems struct {
		Nodes []struct {
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
		} `json:"nodes"`
	} `json:"projectItems"`
}

const projectItemsFragment = `
    projectItems(first: 10, includeArchived: false) {
      nodes {
        project { title }
        fieldValues(first: 30) {
          nodes {
            __typename
            ... on ProjectV2ItemFieldDateValue { date field { ... on ProjectV2FieldCommon { name } } }
            ... on ProjectV2ItemFieldSingleSelectValue { name field { ... on ProjectV2FieldCommon { name } } }
            ... on ProjectV2ItemFieldTextValue { text field { ... on ProjectV2FieldCommon { name } } }
          }
        }
      }
    }`

const commonFields = `
    number title url state body createdAt updatedAt closedAt
    repository { nameWithOwner }
    milestone { title dueOn }
    labels(first: 20) { nodes { name } }`

func searchQuery(withProjects bool) string {
	pi := ""
	if withProjects {
		pi = projectItemsFragment
	}
	return `query($q: String!, $n: Int!, $after: String) {
  search(query: $q, type: ISSUE, first: $n, after: $after) {
    issueCount
    pageInfo { hasNextPage endCursor }
    nodes {
      __typename
      ... on Issue {` + commonFields + pi + `
      }
      ... on PullRequest {` + commonFields + `
        isDraft mergedAt reviewDecision` + pi + `
      }
    }
  }
}`
}

// Search runs a GitHub issue search and returns normalized tasks.
func (c *Client) Search(q string, limit int) ([]Task, error) {
	var tasks []Task
	var after *string
	for len(tasks) < limit {
		page := min(100, limit-len(tasks))
		vars := map[string]any{"q": q, "n": page, "after": after}

		var resp searchResponse
		if err := c.do(searchQuery(c.withProjects), vars, &resp); err != nil {
			return nil, err
		}
		if len(resp.Errors) > 0 {
			msg := resp.Errors[0].Message
			// Retry without project fields if the token lacks the project scope.
			if c.withProjects && mentionsProjectScope(msg) {
				c.withProjects = false
				warnf("token lacks the `read:project` scope; due dates fall back to milestones and body markers")
				continue
			}
			return nil, fmt.Errorf("graphql error: %s", msg)
		}
		for _, n := range resp.Data.Search.Nodes {
			if n.Number == 0 {
				continue
			}
			tasks = append(tasks, normalize(n))
		}
		if !resp.Data.Search.PageInfo.HasNextPage || len(resp.Data.Search.Nodes) == 0 {
			break
		}
		cur := resp.Data.Search.PageInfo.EndCursor
		after = &cur
	}
	return tasks, nil
}

func mentionsProjectScope(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "project") && (strings.Contains(m, "scope") || strings.Contains(m, "resource not accessible"))
}

var (
	dueFieldRe   = regexp.MustCompile(`(?i)\b(due|deadline|target\s*date|target|end\s*date|ship\s*by)\b`)
	startFieldRe = regexp.MustCompile(`(?i)\b(start|kickoff|begin)\b`)
	bodyDueRe    = regexp.MustCompile(`(?i)(?:due(?:\s*date)?|deadline)\s*[:=]?\s*(\d{4}-\d{2}-\d{2})`)
	emojiDueRe   = regexp.MustCompile(`\x{1F4C5}\s*(\d{4}-\d{2}-\d{2})`)
)

func normalize(n node) Task {
	t := Task{
		Repo:      n.Repository.NameWithOwner,
		Number:    n.Number,
		Title:     strings.TrimSpace(n.Title),
		URL:       n.URL,
		State:     strings.ToUpper(n.State),
		CreatedAt: parseTime(n.CreatedAt),
		UpdatedAt: parseTime(n.UpdatedAt),
	}
	if n.Typename == "PullRequest" {
		t.Type = "pr"
		t.Draft = n.IsDraft
		t.Merged = n.MergedAt != ""
	} else {
		t.Type = "issue"
	}
	if n.ClosedAt != "" {
		c := parseTime(n.ClosedAt)
		t.ClosedAt = &c
	}
	for _, l := range n.Labels.Nodes {
		t.Labels = append(t.Labels, l.Name)
	}

	// 1. Project V2 fields win: they carry both a real Status and a real due date.
	for _, item := range n.ProjectItems.Nodes {
		for _, fv := range item.FieldValues.Nodes {
			name := fv.Field.Name
			switch fv.Typename {
			case "ProjectV2ItemFieldDateValue":
				if fv.Date != "" && dueFieldRe.MatchString(name) && !startFieldRe.MatchString(name) && t.Due == nil {
					if d, err := time.ParseInLocation("2006-01-02", fv.Date, time.Local); err == nil {
						t.Due = &d
						t.DueSrc = item.Project.Title + " / " + name
					}
				}
			case "ProjectV2ItemFieldSingleSelectValue":
				if strings.EqualFold(name, "status") && t.Status == "" && fv.Name != "" {
					t.Status = fv.Name
					t.StatusSrc = item.Project.Title
				}
			}
		}
		if t.Project == "" {
			t.Project = item.Project.Title
		}
	}

	// 2. Milestone due date.
	if t.Due == nil && n.Milestone != nil && n.Milestone.DueOn != "" {
		d := parseTime(n.Milestone.DueOn).In(time.Local)
		d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.Local)
		t.Due = &d
		t.DueSrc = "milestone " + n.Milestone.Title
	}
	if n.Milestone != nil {
		t.Milestone = n.Milestone.Title
	}

	// 3. Due markers written into the title or body, e.g. "due: 2026-10-20" or "📅 2026-10-20".
	if t.Due == nil {
		if d, ok := parseDueMarker(n.Title); ok {
			t.Due = &d
			t.DueSrc = "title marker"
		} else if d, ok := parseDueMarker(truncate(n.Body, 4000)); ok {
			t.Due = &d
			t.DueSrc = "body marker"
		}
	}

	if t.Status == "" {
		t.Status = derivedStatus(t, n)
		t.StatusSrc = "derived"
	}
	return t
}

func parseDueMarker(s string) (time.Time, bool) {
	for _, re := range []*regexp.Regexp{bodyDueRe, emojiDueRe} {
		if m := re.FindStringSubmatch(s); len(m) == 2 {
			if d, err := time.ParseInLocation("2006-01-02", m[1], time.Local); err == nil {
				return d, true
			}
		}
	}
	return time.Time{}, false
}

func derivedStatus(t Task, n node) string {
	if t.Type == "pr" {
		switch {
		case t.Merged:
			return "Merged"
		case t.State == "CLOSED":
			return "Closed"
		case t.Draft:
			return "Draft"
		}
		switch n.Review {
		case "APPROVED":
			return "Approved"
		case "CHANGES_REQUESTED":
			return "Changes requested"
		case "REVIEW_REQUIRED":
			return "Needs review"
		}
		return "Open"
	}
	if t.State == "CLOSED" {
		return "Done"
	}
	for _, l := range t.Labels {
		low := strings.ToLower(l)
		if strings.Contains(low, "in progress") || strings.Contains(low, "wip") {
			return "In progress"
		}
		if strings.Contains(low, "blocked") {
			return "Blocked"
		}
	}
	return "Open"
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
