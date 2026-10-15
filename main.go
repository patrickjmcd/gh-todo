// gh-todo builds a to-do list from the GitHub issues and pull requests assigned to you.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

const version = "2.0.0"

// envPrefix namespaces the environment variables, so -interval reads
// GH_TODO_INTERVAL.
const envPrefix = "GH_TODO"

// Report is everything the renderers need.
type Report struct {
	User     string
	Today    time.Time
	Open     []Task
	Done     []Task
	ShowDone bool
	DoneDays int
}

type runOpts struct {
	orgs, repos                       []string
	showDone, includeReviews          bool
	noPRs, dueOnly, showURLs, noColor bool
	compact, noLinks, watch           bool
	interval                          time.Duration
	doneDays, limit                   int
	user, format, sortBy, host        string
}

var (
	cfgFile string
	cfgErr  error
)

func main() {
	root := newRootCmd()

	if err := legacyFlagError(root, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gh-todo: "+err.Error())
		os.Exit(1)
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gh-todo: "+err.Error())
		os.Exit(1)
	}
}

// legacyFlagError catches pre-v2 single-dash long flags. Left to pflag, "-completed"
// parses as the shorthand cluster "-c -o mpleted", which silently filters the search
// to an organization named "mpleted" and returns nothing.
func legacyFlagError(cmd *cobra.Command, args []string) error {
	names := map[string]bool{}
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names[f.Name] = true })

	for _, a := range args {
		if a == "--" {
			break
		}
		if len(a) <= 2 || !strings.HasPrefix(a, "-") || strings.HasPrefix(a, "--") {
			continue
		}
		if name, _, _ := strings.Cut(a[1:], "="); names[name] {
			return fmt.Errorf("unknown flag %q: flags take two dashes now, use --%s", a, name)
		}
	}
	return nil
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "gh-todo",
		Short:   "A to-do list from your assigned GitHub work",
		Version: version,
		Long: `gh-todo builds a to-do list from the GitHub issues and pull requests
assigned to you, grouped by due date and annotated with status.

Blocked work is grouped on its own at the end of the list and left out of the
overdue and due-soon counts. Task references are OSC 8 hyperlinks in terminals
that support them.

Due dates come from, in order: a Projects V2 date field named "Due" or
"Target date", the milestone due date, then a "due: YYYY-MM-DD" marker in the
title or body.

Settings resolve in this order, highest first: flags, environment variables
(GH_TODO_*), the config file, then defaults.`,
		Example: `  gh-todo                              open work assigned to you, grouped by due date
  gh-todo --completed --days 14        add everything you closed in the last two weeks
  gh-todo --org your-org --reviews     one org, plus PRs waiting on your review
  gh-todo --watch                      stay open, refreshing every minute
  gh-todo --compact                    one line per task, as a table
  gh-todo --format markdown > todo.md  a Markdown checklist`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		// An explicit --config that cannot be read is an error, not a fallback.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return cfgErr },
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(optsFromViper())
		},
	}

	// --config is persistent so the config subcommands can target a specific file.
	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is "+defaultConfigPath()+")")

	f := cmd.Flags()
	f.BoolP("completed", "c", false, "include recently completed tasks")
	f.IntP("days", "d", 7, "lookback window in days for completed tasks")
	f.StringP("user", "u", "@me", "GitHub login to build the list for")
	f.Bool("reviews", false, "also include pull requests waiting on your review")
	f.Bool("no-prs", false, "issues only; skip pull requests")
	f.Bool("due-only", false, "only show tasks that have a due date")
	f.StringP("format", "f", "text", "output format: text, markdown, or json")
	f.StringP("sort", "s", "due", "sort within groups: due, updated, or repo")
	f.Int("limit", 200, "maximum tasks to fetch per query")
	f.BoolP("watch", "w", false, "stay open and refresh on an interval")
	f.Duration("interval", time.Minute, "refresh interval for --watch")
	f.Bool("compact", false, "one line per task, rendered as a table")
	f.Bool("urls", false, "print the URL under each task")
	f.Bool("no-color", false, "disable ANSI color")
	f.Bool("no-links", false, "disable OSC 8 terminal hyperlinks")
	f.String("host", "", "GitHub host, for GitHub Enterprise Server (default github.com)")
	f.StringSliceP("org", "o", nil, "restrict to an organization (repeatable)")
	f.StringSliceP("repo", "r", nil, "restrict to OWNER/NAME (repeatable)")

	// Flags took one dash before v2. Say so rather than leaving people with
	// pflag's "unknown shorthand flag: 'a' in -atch".
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		if strings.Contains(err.Error(), "unknown shorthand flag") {
			return fmt.Errorf("%w\n\nflags take two dashes now: use --watch, not -watch", err)
		}
		return err
	})

	cobra.OnInitialize(func() { initConfig(f) })
	cmd.AddCommand(newConfigCmd())
	return cmd
}

// initConfig loads the config file and binds flags and environment variables.
// Precedence runs flags, then environment, then config file, then defaults.
func initConfig(flags *pflag.FlagSet) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(configDir())
		viper.AddConfigPath(".")
	}

	viper.SetEnvPrefix(envPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		switch {
		case cfgFile != "":
			cfgErr = fmt.Errorf("could not read config file %s: %w", cfgFile, err)
		case !errors.As(err, &notFound):
			// A config file exists but is malformed. Say so, then use defaults.
			warnf("ignoring %s: %v", viper.ConfigFileUsed(), err)
		}
	}
	if err := viper.BindPFlags(flags); err != nil {
		warnf("could not bind flags: %v", err)
	}
}

func optsFromViper() runOpts {
	return runOpts{
		orgs:           viper.GetStringSlice("org"),
		repos:          viper.GetStringSlice("repo"),
		showDone:       viper.GetBool("completed"),
		includeReviews: viper.GetBool("reviews"),
		noPRs:          viper.GetBool("no-prs"),
		dueOnly:        viper.GetBool("due-only"),
		showURLs:       viper.GetBool("urls"),
		noColor:        viper.GetBool("no-color"),
		compact:        viper.GetBool("compact"),
		noLinks:        viper.GetBool("no-links"),
		watch:          viper.GetBool("watch"),
		interval:       viper.GetDuration("interval"),
		doneDays:       viper.GetInt("days"),
		limit:          viper.GetInt("limit"),
		user:           viper.GetString("user"),
		format:         viper.GetString("format"),
		sortBy:         viper.GetString("sort"),
		host:           viper.GetString("host"),
	}
}

func configDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "gh-todo")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".config", "gh-todo")
}

func defaultConfigPath() string { return filepath.Join(configDir(), "config.yaml") }

func run(o runOpts) error {
	switch o.format {
	case "text", "markdown", "md", "json":
	default:
		return fmt.Errorf("unknown format %q: use text, markdown, or json", o.format)
	}
	switch o.sortBy {
	case "due", "updated", "repo":
	default:
		return fmt.Errorf("unknown sort %q: use due, updated, or repo", o.sortBy)
	}
	if o.doneDays < 1 {
		return fmt.Errorf("--days must be at least 1")
	}
	if o.watch && o.format != "text" {
		return fmt.Errorf("--watch works with the text format only")
	}
	if o.watch && o.interval < 10*time.Second {
		return fmt.Errorf("--interval must be at least 10s")
	}
	if o.watch && !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("--watch needs a terminal; drop it when piping or redirecting output")
	}

	client, err := NewClient(o.host)
	if err != nil {
		return err
	}

	login := o.user
	if login == "@me" || login == "" {
		login, err = client.Login()
		if err != nil {
			return fmt.Errorf("could not resolve the current user: %w", err)
		}
	}

	fetch := func() (Report, error) { return fetchReport(client, login, o) }

	if o.watch {
		return watchLoop(fetch, login, o)
	}

	report, err := fetch()
	if err != nil {
		return err
	}

	switch o.format {
	case "json":
		return renderJSON(os.Stdout, report)
	case "markdown", "md":
		renderMarkdown(os.Stdout, report)
	default:
		render := renderPretty
		if o.compact {
			render = renderCompact
		}
		render(os.Stdout, report, NewTheme(o.noColor, o.noLinks), o.showURLs)
	}
	return nil
}

// fetchReport runs every search the options call for and assembles the report.
func fetchReport(client *Client, login string, o runOpts) (Report, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	scope := scopeQualifiers(o.orgs, o.repos)
	typeQ := ""
	if o.noPRs {
		typeQ = " is:issue"
	}

	open, err := client.Search(fmt.Sprintf("assignee:%s is:open archived:false%s%s", login, typeQ, scope), o.limit)
	if err != nil {
		return Report{}, err
	}

	if o.includeReviews && !o.noPRs {
		reviews, err := client.Search(fmt.Sprintf("review-requested:%s is:open is:pr archived:false%s", login, scope), o.limit)
		if err != nil {
			return Report{}, err
		}
		for i := range reviews {
			reviews[i].ReviewOnly = true
			if reviews[i].StatusSrc == "derived" {
				reviews[i].Status = "Your review requested"
			}
		}
		open = dedupe(append(open, reviews...))
	}

	if o.dueOnly {
		open = filter(open, func(t Task) bool { return t.Due != nil })
	}
	sortTasks(open, o.sortBy)

	report := Report{User: login, Today: today, Open: open, ShowDone: o.showDone, DoneDays: o.doneDays}

	if o.showDone {
		since := today.AddDate(0, 0, -o.doneDays).Format("2006-01-02")
		done, err := client.Search(fmt.Sprintf("assignee:%s is:closed closed:>=%s archived:false%s%s", login, since, typeQ, scope), o.limit)
		if err != nil {
			return Report{}, err
		}
		report.Done = dedupe(done)
	}
	return report, nil
}

func scopeQualifiers(orgs, repos []string) string {
	var b strings.Builder
	for _, o := range orgs {
		b.WriteString(" org:" + o)
	}
	for _, r := range repos {
		b.WriteString(" repo:" + r)
	}
	return b.String()
}

func dedupe(ts []Task) []Task {
	seen := map[string]bool{}
	var out []Task
	for _, t := range ts {
		key := fmt.Sprintf("%s#%d", t.Repo, t.Number)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	sortTasks(out, "updated")
	return out
}

func filter(ts []Task, keep func(Task) bool) []Task {
	var out []Task
	for _, t := range ts {
		if keep(t) {
			out = append(out, t)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func warnf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "gh-todo: warning: "+format+"\n", a...)
}
