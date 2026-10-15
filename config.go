package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// starterConfig is written by `gh-todo config init`. Every key is commented out,
// so the file documents the options without changing any defaults.
const starterConfig = `# gh-todo configuration.
#
# Precedence, highest first: flags, environment (GH_TODO_*), this file, defaults.
# Every key matches a flag name: --no-prs is no-prs, --interval is interval.

# Restrict the search. Both accept a list.
# org:
#   - your-org
# repo:
#   - owner/name

# Include work you closed recently, and how far back to look.
# completed: true
# days: 7

# Also list pull requests waiting on your review.
# reviews: true

# Issues only, or dated work only.
# no-prs: false
# due-only: false

# Output. format is text, markdown, or json. sort is due, updated, or repo.
# format: text
# sort: due
# compact: false
# urls: false
# no-color: false
# no-links: false

# Live view.
# watch: false
# interval: 1m

# Build the list for someone else, and cap how much is fetched.
# user: "@me"
# limit: 200

# GitHub Enterprise Server. Leave unset for github.com.
# host: github.example.com
`

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and create the config file",
		Args:  cobra.NoArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newConfigInitCmd(), newConfigShowCmd(), newConfigPathCmd())
	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter config file",
		Long: "Write a commented starter config file. Every setting in it is commented\n" +
			"out, so the file changes nothing until you edit it.",
		Args: cobra.NoArgs,
		// A --config file that does not exist yet is the normal case here, so
		// skip the root command's check that it must be readable.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
		RunE: func(c *cobra.Command, _ []string) error {
			path := cfgFile
			if path == "" {
				path = defaultConfigPath()
			}

			if _, err := os.Stat(path); err == nil && !force {
				return fmt.Errorf("%s already exists; pass --force to overwrite it", path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(starterConfig), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "wrote %s\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config file")
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the resolved settings and where they came from",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			w := c.OutOrStdout()

			if used := viper.ConfigFileUsed(); used != "" {
				fmt.Fprintf(w, "config file: %s\n", used)
			} else {
				fmt.Fprintf(w, "config file: none (looked for %s)\n", defaultConfigPath())
			}
			fmt.Fprintf(w, "environment: %s_*\n\n", envPrefix)

			settings := viper.AllSettings()
			keys := make([]string, 0, len(settings))
			for k := range settings {
				if k != "config" {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)

			for _, k := range keys {
				fmt.Fprintf(w, "%-10s %v%s\n", k+":", settings[k], sourceNote(k))
			}
			return nil
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			path := viper.ConfigFileUsed()
			if path == "" {
				path = defaultConfigPath()
			}
			fmt.Fprintln(c.OutOrStdout(), path)
			return nil
		},
	}
}

// sourceNote reports where a non-default value came from.
func sourceNote(key string) string {
	env := envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
	if _, ok := os.LookupEnv(env); ok {
		return "   (from " + env + ")"
	}
	if viper.InConfig(key) {
		return "   (from config file)"
	}
	return ""
}
