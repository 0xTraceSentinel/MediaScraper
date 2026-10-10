package scraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/0xTraceSentinel/MediaScraper/internal/extract"
	"github.com/0xTraceSentinel/MediaScraper/internal/fetch"
	"github.com/0xTraceSentinel/MediaScraper/internal/model"
)

type Options struct {
	Concurrency int
	MaxDepth    int
	MaxPages    int
	SameHost    bool
}

type Result struct {
	URL        string          `json:"url"`
	FinalURL   string          `json:"final_url,omitempty"`
	Depth      int             `json:"depth"`
	StatusCode int             `json:"status_code,omitempty"`
	Profile    string          `json:"profile,omitempty"`
	Proxy      string          `json:"proxy,omitempty"`
	Attempts   int             `json:"attempts,omitempty"`
	FetchedAt  time.Time       `json:"fetched_at"`
	DurationMS int64           `json:"duration_ms"`
	Products   []model.Product `json:"products,omitempty"`
	LinksFound int             `json:"links_found,omitempty"`
	Error      string          `json:"error,omitempty"`
}

func (r *Result) OK() bool { return r.Error == "" }

type Processor interface {
	Process(pageURL *url.URL, body []byte) (*extract.Page, error)
}

type Engine struct {
	fetcher   fetch.Fetcher
	processor Processor
	opts      Options
	log       *slog.Logger
}

func New(f fetch.Fetcher, p Processor, opts Options, log *slog.Logger) *Engine {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Engine{fetcher: f, processor: p, opts: opts, log: log}
}

type task struct {
	url   string
	depth int
}

type outcome struct {
	links []string
	depth int
}

func (e *Engine) Run(ctx context.Context, seeds []string) (<-chan Result, error) {
	if len(seeds) == 0 {
		return nil, errors.New("scraper: no seed URLs")
	}
	allowedHosts := map[string]bool{}
	var queue []task
	seen := map[string]bool{}
	for _, s := range seeds {
		u, err := url.Parse(strings.TrimSpace(s))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("scraper: invalid seed URL %q", s)
		}
		allowedHosts[strings.ToLower(u.Hostname())] = true
		key := canonical(u)
		if !seen[key] {
			seen[key] = true
			queue = append(queue, task{url: u.String()})
		}
	}

	results := make(chan Result)
	jobs := make(chan task)
	done := make(chan outcome)

	for range e.opts.Concurrency {
		go func() {
			for t := range jobs {
				res, links := e.process(ctx, t)
				select {
				case results <- res:
				case <-ctx.Done():
				}
				done <- outcome{links: links, depth: t.depth}
			}
		}()
	}

	go func() {
		defer close(results)
		defer close(jobs)

		dispatched, inFlight := 0, 0
		cancelled := ctx.Done()
		for len(queue) > 0 || inFlight > 0 {
			var send chan task
			var next task
			if len(queue) > 0 && ctx.Err() == nil {
				send, next = jobs, queue[0]
			}
			select {
			case send <- next:
				queue = queue[1:]
				dispatched++
				inFlight++
				if e.opts.MaxPages > 0 && dispatched >= e.opts.MaxPages {
					queue = nil
				}
			case o := <-done:
				inFlight--
				if o.depth >= e.opts.MaxDepth || ctx.Err() != nil {
					continue
				}
				if e.opts.MaxPages > 0 && dispatched >= e.opts.MaxPages {
					continue
				}
				for _, link := range o.links {
					u, err := url.Parse(link)
					if err != nil {
						continue
					}
					if e.opts.SameHost && !allowedHosts[strings.ToLower(u.Hostname())] {
						continue
					}
					key := canonical(u)
					if seen[key] {
						continue
					}
					seen[key] = true
					queue = append(queue, task{url: link, depth: o.depth + 1})
				}
			case <-cancelled:
				queue = nil
				cancelled = nil
			}
		}
	}()

	return results, nil
}

func (e *Engine) process(ctx context.Context, t task) (res Result, links []string) {
	start := time.Now()
	res = Result{URL: t.url, Depth: t.depth, FetchedAt: start.UTC()}
	defer func() { res.DurationMS = time.Since(start).Milliseconds() }()

	resp, err := e.fetcher.Fetch(ctx, t.url)
	if resp != nil {
		res.StatusCode = resp.StatusCode
		res.FinalURL = resp.URL.String()
		res.Proxy = resp.Proxy
		res.Attempts = resp.Attempts
	}
	if err != nil {
		res.Error = err.Error()
		e.log.Warn("fetch failed", "url", t.url, "error", err)
		return res, nil
	}

	page, err := e.processor.Process(resp.URL, resp.Body)
	if page == nil {
		res.Error = fmt.Sprintf("process: %v", err)
		e.log.Warn("processing failed", "url", t.url, "error", err)
		return res, nil
	}
	if err != nil {
		e.log.Debug("extractor errors", "url", t.url, "error", err)
	}
	res.Products = page.Products
	res.Profile = page.Profile
	res.LinksFound = len(page.Links)
	e.log.Info("scraped page", "url", t.url, "status", res.StatusCode, "products", len(page.Products), "links", len(page.Links), "depth", t.depth)
	return res, page.Links
}

func canonical(u *url.URL) string {
	c := *u
	c.Scheme = strings.ToLower(c.Scheme)
	c.Host = strings.ToLower(c.Host)
	c.Fragment = ""
	c.RawFragment = ""
	if c.Path == "" {
		c.Path = "/"
	}
	return c.String()
}
