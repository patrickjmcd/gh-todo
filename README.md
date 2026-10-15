# gh-todo

Build a to-do list from the GitHub issues and pull requests assigned to you.
`gh-todo` groups your open work by due date, shows the status of each item, and
optionally lists what you closed recently. Output is styled with
[lipgloss](https://github.com/charmbracelet/lipgloss) and adapts to light and
dark terminals.

```
╭──────────────────────────────────────────────────────────────────────────╮
│  GitHub to-do                                                            │
│  octocat  ·  your-org                            Thursday, 15 October    │
╰──────────────────────────────────────────────────────────────────────────╯

 ╭───────────────╮ ╭───────────────╮ ╭───────────────╮ ╭───────────────╮
 │       12      │ │       2       │ │       1       │ │       4       │
 │     OPEN      │ │    OVERDUE    │ │   DUE TODAY   │ │  NEXT 7 DAYS  │
 ╰───────────────╯ ╰───────────────╯ ╰───────────────╯ ╰───────────────╯

▌ OVERDUE  2  ──────────────────────────────────────────────────────────────
  ● api-gateway#412    Rotate the signing keys before the audit
      In progress   ◷ Oct 9 · 6 days ago  ◇ Q4 hardening  ▸ Platform  security
  ● billing#88        Backfill the invoice totals
      Todo   ◷ Oct 13 · 2 days ago  ▸ Platform

▌ DUE TODAY  1  ────────────────────────────────────────────────────────────
  ◉ web#1204          Ship the new onboarding flow
      In review   ◷ Oct 15 · today  ◇ v3.2  ▸ Product
```

## Requirements

- Go 1.26 or newer, to build
- A GitHub token, from the [`gh` CLI](https://cli.github.com) or the environment

## Install

```sh
go install github.com/patrickjmcd/gh-todo@latest
```

Or build from a clone:

```sh
make install          # builds and copies to ~/.local/bin/gh-todo
```

To install somewhere else, set `PREFIX`:

```sh
make install PREFIX=/usr/local/bin
```

## Authenticate

`gh-todo` reads a token from `GITHUB_TOKEN`, then `GH_TOKEN`, then `gh auth token`.
If you already use the `gh` CLI, no setup is needed.

For GitHub Enterprise Server, set the host:

```sh
gh-todo --host github.example.com
```

The host also reads from `GH_TODO_HOST`, the `host` config key, or `GH_HOST`.
`gh-todo` derives the API URL from it: `https://api.github.com/graphql` for
github.com, `https://HOST/api/graphql` otherwise.

For due dates and statuses that live in GitHub Projects, the token needs the
`project` or `read:project` scope. Without it, `gh-todo` prints a warning and
falls back to milestones and body markers.

## Use

```sh
gh-todo                                   # open work, grouped by due date
gh-todo --completed                       # add what you closed in the last 7 days
gh-todo --completed --days 30             # widen the completed window
gh-todo --reviews                         # add PRs waiting on your review
gh-todo --org your-org                    # restrict to one organization
gh-todo --repo owner/name --repo owner/x  # restrict to specific repositories
gh-todo --due-only --sort due             # only dated work, soonest first
gh-todo --watch                           # stay open, refreshing every minute
gh-todo --compact                         # one line per task, as a table
gh-todo --format markdown > todo.md       # a Markdown checklist
gh-todo --format json | jq '.buckets'     # machine-readable output
```

Short forms: `-c` completed, `-d` days, `-f` format, `-o` org, `-r` repo,
`-s` sort, `-u` user, `-w` watch.

### Groups

Open work falls into one of seven groups: **Overdue**, **Due today**,
**Due tomorrow**, **Due this week**, **Later**, **No due date**, and **Blocked**.
Empty groups are skipped.

**Blocked** comes last and takes precedence over every due-date group, because
blocked work is not something you can act on. A task counts as blocked when its
Projects V2 status or one of its labels contains "blocked". Blocked tasks are
also left out of the overdue, due-today, and next-7-days counts, so the summary
tiles only ever count work you can pick up.

### Where due dates come from

GitHub issues have no native due date, so `gh-todo` checks three sources in order
and uses the first hit:

1. A Projects V2 date field whose name matches `due`, `deadline`, `target date`,
   `end date`, or `ship by`. Fields named `start` are ignored.
2. The milestone due date.
3. A `due: YYYY-MM-DD` or `📅 YYYY-MM-DD` marker in the title or body.

The JSON output records which source was used in `due_source`.

### Where status comes from

1. A Projects V2 single-select field named `Status`, such as `In progress` or `Blocked`.
2. Otherwise a derived value: `Draft`, `Needs review`, `Approved`,
   `Changes requested`, or `Merged` for pull requests; `In progress` or `Blocked`
   from labels for issues; `Open` otherwise.

## Configuration

Settings resolve in this order, highest first:

1. Flags
2. Environment variables, prefixed `GH_TODO_`
3. The config file
4. Built-in defaults

Every config key matches a flag name, so `--no-prs` is `no-prs` in the file and
`GH_TODO_NO_PRS` in the environment.

Write a commented starter file:

```sh
gh-todo config init          # writes ~/.config/gh-todo/config.yaml
gh-todo config path          # print the path in use
gh-todo config show          # print resolved settings and where each came from
```

The file lives at `$XDG_CONFIG_HOME/gh-todo/config.yaml`, falling back to
`~/.config/gh-todo/config.yaml`. A `config.yaml` in the working directory is
picked up too. Point somewhere else with `--config`:

```yaml
org:
  - your-org
completed: true
days: 14
compact: true
interval: 30s
```

A missing file is fine. A malformed one prints a warning and falls back to
defaults. A `--config` path that cannot be read is an error.

## Live view

`--watch` keeps the list on screen and refreshes it on an interval, default one
minute:

```sh
gh-todo --watch                   # refresh every minute
gh-todo --watch --interval 30s    # refresh twice a minute
gh-todo --watch --compact         # the table view, live
```

The live view runs in the alternate screen, so your scrollback is untouched when
you quit. Keys:

| Key | Action |
| --- | --- |
| `↑` `↓` `j` `k` | Scroll the list |
| `PgUp` `PgDn` | Scroll a page |
| `r` | Refresh now |
| `q` `Esc` `Ctrl-C` | Quit |

The status bar shows how long ago the list refreshed, the countdown to the next
refresh, and how far you have scrolled. Your scroll position survives a refresh.
Failed refreshes show the error in the status bar and keep the last good list on
screen. Hyperlinks stay clickable.

`--watch` needs a terminal and the text format; it refuses to run when output is
piped. The minimum interval is 10s, to stay well inside the GitHub API rate
limit.

## Hyperlinks

Each task reference is an OSC 8 hyperlink to the issue or pull request, so you
can click or ⌘-click it instead of copying a URL. iTerm2, WezTerm, Kitty,
Ghostty, and the VS Code terminal all support this; terminals that do not, such
as Apple Terminal, ignore the sequence and show plain text.

Links are emitted only when output goes to a terminal, so piped and redirected
output stays clean. Turn them off with `--no-links` or `NO_HYPERLINKS=1`. Use
`--urls` to print the full URL under each task instead.

## Views

The default view gives each task two lines: the reference and title, then a
status pill, a due-date tag, and the milestone, project, and labels.

`--compact` gives each task one line in a table — reference, title, status, and
due date — which scans faster when you have dozens of open items.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-c, --completed` | `false` | Include recently completed tasks |
| `-d, --days N` | `7` | Lookback window in days for completed tasks |
| `--reviews` | `false` | Also include pull requests waiting on your review |
| `--no-prs` | `false` | Issues only |
| `--due-only` | `false` | Only show tasks that have a due date |
| `-o, --org NAME` | — | Restrict to an organization; repeatable |
| `-r, --repo OWNER/NAME` | — | Restrict to repositories; repeatable |
| `-u, --user LOGIN` | `@me` | Build the list for another GitHub user |
| `-f, --format FMT` | `text` | `text`, `markdown`, or `json` |
| `-s, --sort KEY` | `due` | `due`, `updated`, or `repo` |
| `--limit N` | `200` | Maximum tasks to fetch per query |
| `-w, --watch` | `false` | Stay open and refresh on an interval |
| `--interval D` | `1m` | Refresh interval for `--watch`, minimum `10s` |
| `--compact` | `false` | One line per task, rendered as a table |
| `--urls` | `false` | Print the URL under each task |
| `--no-color` | `false` | Disable ANSI color; `NO_COLOR` also works |
| `--no-links` | `false` | Disable OSC 8 hyperlinks; `NO_HYPERLINKS` also works |
| `--host HOST` | `github.com` | GitHub host, for GitHub Enterprise Server |
| `--config PATH` | — | Use a specific config file |
| `--version` | — | Print the version and exit |

Color turns off automatically when output is piped to a file, and the layout
sizes itself to the terminal width.

## Shell completion

```sh
gh-todo completion zsh > "${fpath[1]}/_gh-todo"     # zsh
gh-todo completion bash > /usr/local/etc/bash_completion.d/gh-todo
gh-todo completion fish > ~/.config/fish/completions/gh-todo.fish
```

## Daily list

To see the list every time you open a shell, add this to `~/.zshrc`:

```sh
alias todo='gh-todo --completed --days 3'
alias todos='gh-todo --watch'
```

## Contributing

Issues and pull requests are welcome. Before opening a pull request, run:

```sh
gofmt -l .
go vet ./...
go test -race ./...
```

CI runs the same three checks on Linux and macOS.

## License

MIT. See [LICENSE](LICENSE).
