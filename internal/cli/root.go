package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/0xTraceSentinel/MediaScraper/internal/config"
	"github.com/0xTraceSentinel/MediaScraper/internal/version"
)

type globalOptions struct {
	configPath string
	logLevel   string
	logFormat  string
}

func (g *globalOptions) loadConfig(cmd *cobra.Command) (config.Config, error) {
	cfg := config.Default()
	if g.configPath != "" {
		var err error
		if cfg, err = config.Load(g.configPath); err != nil {
			return cfg, err
		}
	}
	flags := cmd.Flags()
	if flags.Changed("log-level") {
		cfg.Log.Level = g.logLevel
	}
	if flags.Changed("log-format") {
		cfg.Log.Format = g.logFormat
	}
	return cfg, nil
}

func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	g := &globalOptions{}
	root := &cobra.Command{
		Use:   "mediascraper",
		Short: "A configurable web scraper for e-commerce and beyond",
		Long: `mediascraper fetches web pages and extracts structured product data.

Products are found automatically via JSON-LD, microdata and OpenGraph
metadata. Site-specific CSS selector profiles can be added in a config
file for sites that need them.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	pf := root.PersistentFlags()
	pf.StringVarP(&g.configPath, "config", "c", "", "path to YAML config file")
	pf.StringVar(&g.logLevel, "log-level", "info", "log level: debug, info, warn, error")
	pf.StringVar(&g.logFormat, "log-format", "text", "log format: text, json")

	root.AddCommand(
		newScrapeCommand(g),
		newConfigCommand(g),
		newVersionCommand(),
	)
	return root
}

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := NewRootCommand(stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	return 0
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.String())
		},
	}
}
