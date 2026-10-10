package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/0xTraceSentinel/MediaScraper/internal/app"
	"github.com/0xTraceSentinel/MediaScraper/internal/config"
	"github.com/0xTraceSentinel/MediaScraper/internal/output"
)

type scrapeOptions struct {
	input       string
	output      string
	format      string
	concurrency int
	depth       int
	maxPages    int
	sameHost    bool
	follow      []string
	rps         float64
	timeout     time.Duration
	retries     int
	userAgent   string
	proxies     []string
	proxyFile   string
	noRobots    bool
}

func newScrapeCommand(g *globalOptions) *cobra.Command {
	o := &scrapeOptions{}
	cmd := &cobra.Command{
		Use:   "scrape [url...]",
		Short: "Scrape one or more URLs",
		Example: `  # Scrape a single product page to stdout as JSON Lines
  mediascraper scrape https://shop.example.com/products/widget

  # Crawl a category page one level deep, following product links
  mediascraper scrape https://shop.example.com/c/widgets --depth 1 \
      --follow "a.product-card" -o products.csv -f csv

  # Read URLs from a file and route requests through rotating proxies
  mediascraper scrape -i urls.txt --proxy-file proxies.txt`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScrape(cmd, g, o, args)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&o.input, "input", "i", "", "file with one URL per line ('-' for stdin)")
	f.StringVarP(&o.output, "output", "o", "", "output file (default stdout)")
	f.StringVarP(&o.format, "format", "f", string(output.FormatJSONL), "output format: jsonl, json, csv")
	f.IntVar(&o.concurrency, "concurrency", 0, "number of pages processed in parallel")
	f.IntVarP(&o.depth, "depth", "d", 0, "link hops to follow from seed URLs")
	f.IntVar(&o.maxPages, "max-pages", 0, "maximum pages to fetch (0 = unlimited)")
	f.BoolVar(&o.sameHost, "same-host", true, "only follow links on the seed URLs' hosts")
	f.StringArrayVar(&o.follow, "follow", nil, "CSS selector for links to follow (repeatable)")
	f.Float64Var(&o.rps, "rps", 0, "max requests per second per host (0 = unlimited)")
	f.DurationVar(&o.timeout, "timeout", 0, "per-request timeout")
	f.IntVar(&o.retries, "retries", 0, "max attempts per request, including the first")
	f.StringVar(&o.userAgent, "user-agent", "", "fixed User-Agent (default: rotate browser UAs)")
	f.StringArrayVar(&o.proxies, "proxy", nil, "proxy URL, e.g. http://user:pass@host:port (repeatable)")
	f.StringVar(&o.proxyFile, "proxy-file", "", "file with one proxy URL per line")
	f.BoolVar(&o.noRobots, "ignore-robots", false, "do not check robots.txt (only for sites you are authorized to crawl)")
	return cmd
}

func (o *scrapeOptions) applyFlags(cmd *cobra.Command, cfg *config.Config) {
	f := cmd.Flags()
	if f.Changed("output") {
		cfg.Output.Path = o.output
	}
	if f.Changed("format") {
		cfg.Output.Format = output.Format(strings.ToLower(o.format))
	}
	if f.Changed("concurrency") {
		cfg.Scraper.Concurrency = o.concurrency
	}
	if f.Changed("depth") {
		cfg.Scraper.MaxDepth = o.depth
	}
	if f.Changed("max-pages") {
		cfg.Scraper.MaxPages = o.maxPages
	}
	if f.Changed("same-host") {
		cfg.Scraper.SameHost = o.sameHost
	}
	if f.Changed("follow") {
		cfg.Scraper.Follow = append(cfg.Scraper.Follow, o.follow...)
	}
	if f.Changed("rps") {
		cfg.HTTP.RequestsPerSecond = o.rps
	}
	if f.Changed("timeout") {
		cfg.HTTP.Timeout = o.timeout
	}
	if f.Changed("retries") {
		cfg.HTTP.Retry.MaxAttempts = o.retries
	}
	if f.Changed("user-agent") {
		cfg.HTTP.UserAgents = []string{o.userAgent}
	}
	if f.Changed("proxy") {
		cfg.Proxy.URLs = append(cfg.Proxy.URLs, o.proxies...)
	}
	if f.Changed("proxy-file") {
		cfg.Proxy.File = o.proxyFile
	}
	if f.Changed("ignore-robots") {
		cfg.HTTP.RespectRobots = !o.noRobots
	}
}

func runScrape(cmd *cobra.Command, g *globalOptions, o *scrapeOptions, args []string) error {
	cfg, err := g.loadConfig(cmd)
	if err != nil {
		return err
	}
	o.applyFlags(cmd, &cfg)

	urls, err := collectURLs(args, o.input, cmd.InOrStdin())
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return errors.New("no URLs given: pass them as arguments or with --input")
	}

	log := app.NewLogger(cfg.Log, cmd.ErrOrStderr())
	engine, err := app.NewEngine(cfg, log)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if cfg.Output.Path != "" && cfg.Output.Path != "-" {
		file, err := os.Create(cfg.Output.Path)
		if err != nil {
			return fmt.Errorf("create output: %w", err)
		}
		defer file.Close()
		w = file
	}
	bw := bufio.NewWriter(w)
	sink, err := output.New(cfg.Output.Format, bw)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	results, err := engine.Run(ctx, urls)
	if err != nil {
		return err
	}

	start := time.Now()
	var pages, failed, products int
	var writeErr error
	for r := range results {
		pages++
		products += len(r.Products)
		if !r.OK() {
			failed++
		}
		if writeErr == nil {
			if writeErr = sink.Write(r); writeErr == nil && cfg.Output.Format == output.FormatJSONL {
				writeErr = bw.Flush()
			}
		}
	}
	if err := sink.Close(); writeErr == nil {
		writeErr = err
	}
	if err := bw.Flush(); writeErr == nil {
		writeErr = err
	}
	if writeErr != nil {
		return fmt.Errorf("write output: %w", writeErr)
	}

	log.Info("scrape finished", "pages", pages, "failed", failed, "products", products, "elapsed", time.Since(start).Round(time.Millisecond))
	if ctx.Err() != nil {
		return fmt.Errorf("interrupted: %w", ctx.Err())
	}
	if pages > 0 && failed == pages {
		return fmt.Errorf("all %d pages failed", pages)
	}
	return nil
}

func collectURLs(args []string, input string, stdin io.Reader) ([]string, error) {
	urls := append([]string(nil), args...)
	if input == "" {
		return urls, nil
	}
	var r io.Reader = stdin
	if input != "-" {
		f, err := os.Open(input)
		if err != nil {
			return nil, fmt.Errorf("open input: %w", err)
		}
		defer f.Close()
		r = f
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}
	return urls, nil
}
