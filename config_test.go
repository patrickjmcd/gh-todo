package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Every flag should be documented in the starter config, so the file is a
// complete reference for what you can set.
func TestStarterConfigDocumentsEveryFlag(t *testing.T) {
	skip := map[string]bool{"config": true, "help": true, "version": true}

	newRootCmd().Flags().VisitAll(func(f *pflag.Flag) {
		if skip[f.Name] {
			return
		}
		if !strings.Contains(starterConfig, "# "+f.Name+":") {
			t.Errorf("starter config does not document the %q setting", f.Name)
		}
	})
}

func TestConfigDirPrefersXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	if got, want := configDir(), filepath.Join("/tmp/xdg", "gh-todo"); got != want {
		t.Errorf("configDir = %q, want %q", got, want)
	}
	if got, want := defaultConfigPath(), filepath.Join("/tmp/xdg", "gh-todo", "config.yaml"); got != want {
		t.Errorf("defaultConfigPath = %q, want %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, want := configDir(), filepath.Join(home, ".config", "gh-todo"); got != want {
		t.Errorf("configDir = %q, want %q", got, want)
	}
}

func TestOptsFromViperMapsEveryKey(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("org", []string{"acme"})
	viper.Set("repo", []string{"acme/tools"})
	viper.Set("completed", true)
	viper.Set("reviews", true)
	viper.Set("no-prs", true)
	viper.Set("due-only", true)
	viper.Set("urls", true)
	viper.Set("no-color", true)
	viper.Set("compact", true)
	viper.Set("no-links", true)
	viper.Set("watch", true)
	viper.Set("interval", "45s")
	viper.Set("days", 21)
	viper.Set("limit", 50)
	viper.Set("user", "octocat")
	viper.Set("format", "markdown")
	viper.Set("sort", "repo")

	o := optsFromViper()
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"orgs", strings.Join(o.orgs, ","), "acme"},
		{"repos", strings.Join(o.repos, ","), "acme/tools"},
		{"showDone", o.showDone, true},
		{"includeReviews", o.includeReviews, true},
		{"noPRs", o.noPRs, true},
		{"dueOnly", o.dueOnly, true},
		{"showURLs", o.showURLs, true},
		{"noColor", o.noColor, true},
		{"compact", o.compact, true},
		{"noLinks", o.noLinks, true},
		{"watch", o.watch, true},
		{"interval", o.interval, 45 * time.Second},
		{"doneDays", o.doneDays, 21},
		{"limit", o.limit, 50},
		{"user", o.user, "octocat"},
		{"format", o.format, "markdown"},
		{"sortBy", o.sortBy, "repo"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestEnvironmentOverridesConfigFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("days: 30\ncompact: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GH_TODO_DAYS", "3")
	cfgFile = path
	t.Cleanup(func() { cfgFile = "" })

	initConfig(pflag.NewFlagSet("test", pflag.ContinueOnError))
	if cfgErr != nil {
		t.Fatalf("initConfig: %v", cfgErr)
	}

	if got := viper.GetInt("days"); got != 3 {
		t.Errorf("days = %d, want 3 from the environment", got)
	}
	if !viper.GetBool("compact") {
		t.Error("compact must fall through to the config file")
	}
}

func TestMissingExplicitConfigIsAnError(t *testing.T) {
	viper.Reset()
	cfgErr = nil
	t.Cleanup(func() { viper.Reset(); cfgErr = nil; cfgFile = "" })

	cfgFile = filepath.Join(t.TempDir(), "absent.yaml")
	initConfig(pflag.NewFlagSet("test", pflag.ContinueOnError))

	if cfgErr == nil {
		t.Error("a missing --config file must produce an error")
	}
}

func TestLegacySingleDashFlagsAreRejected(t *testing.T) {
	cmd := newRootCmd()

	// Pre-v2 spellings must fail loudly rather than parse as shorthand clusters.
	for _, arg := range []string{"-completed", "-watch", "-no-color", "-format=json", "-days", "-urls"} {
		if err := legacyFlagError(cmd, []string{arg}); err == nil {
			t.Errorf("legacyFlagError(%q) = nil, want an error", arg)
		}
	}

	// Real shorthands, long flags, and anything after -- must pass through.
	for _, args := range [][]string{
		{"-c"}, {"-cw"}, {"--completed"}, {"--days", "7"}, {"-d", "7"},
		{"--", "-completed"}, {"config", "show"},
	} {
		if err := legacyFlagError(cmd, args); err != nil {
			t.Errorf("legacyFlagError(%v) = %v, want nil", args, err)
		}
	}
}

func TestGraphQLEndpoint(t *testing.T) {
	cases := map[string]string{
		"":                           "https://api.github.com/graphql",
		"github.com":                 "https://api.github.com/graphql",
		"github.example.com":         "https://github.example.com/api/graphql",
		"https://github.example.com": "https://github.example.com/api/graphql",
		"github.example.com/":        "https://github.example.com/api/graphql",
	}
	for host, want := range cases {
		if got := graphQLEndpoint(host); got != want {
			t.Errorf("graphQLEndpoint(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestConfigInitWritesToAMissingPath(t *testing.T) {
	viper.Reset()
	t.Cleanup(func() { viper.Reset(); cfgErr = nil; cfgFile = "" })

	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cmd := newRootCmd()
	cmd.SetArgs([]string{"config", "init", "--config", path})
	cmd.SetOut(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("config init: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the written config: %v", err)
	}
	if !strings.Contains(string(body), "your-org") {
		t.Error("the starter config must use a placeholder organization")
	}

	// A second run must refuse rather than clobber the file.
	cmd = newRootCmd()
	cmd.SetArgs([]string{"config", "init", "--config", path})
	cmd.SetOut(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Error("config init must refuse to overwrite an existing file")
	}
}
